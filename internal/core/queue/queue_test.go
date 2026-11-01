package queue

import (
	"bytes"
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/plutack/seedrlike/internal/jobs"
)

type fakeStore struct {
	active                          []jobs.Job
	created                         []jobs.Job
	saved                           []jobs.Job
	finished                        []jobs.Job
	createErr, errorFinish, saveErr error
}

func (s *fakeStore) Create(_ context.Context, j *jobs.Job) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.created = append(s.created, *j)
	return nil
}
func (s *fakeStore) Active(context.Context) ([]jobs.Job, error) { return s.active, nil }
func (s *fakeStore) Save(_ context.Context, j jobs.Job) error {
	s.saved = append(s.saved, j)
	return s.saveErr
}
func (s *fakeStore) Finish(_ context.Context, j jobs.Job, _ string) error {
	if s.errorFinish != nil {
		return s.errorFinish
	}
	s.finished = append(s.finished, j)
	return nil
}

const testMagnet = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789&dn=test"

func configured(t *testing.T, s *fakeStore) *DownloadQueue {
	t.Helper()
	q := New()
	if err := q.configure(s, t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
	return q
}
func TestFinishReconcilesAmbiguousCommit(t *testing.T) {
	s := &fakeStore{errorFinish: errors.New("commit response lost")}
	q := configured(t, s)
	if err := q.Add(DownloadRequest{MagnetLink: testMagnet}); err != nil {
		t.Fatal(err)
	}
	task := q.tasks[0]
	task.job.Reserved = 123
	task.reserved = true
	q.activeBytes = 123
	q.finish(task, StatusTaskCompleted, nil)
	if len(q.tasks) != 1 || q.activeBytes != 123 {
		t.Fatal("failed commit must retain task and reservation")
	}
	s.errorFinish = nil
	s.saveErr = sql.ErrNoRows
	q.finish(task, StatusTaskCompleted, nil)
	if len(q.tasks) != 0 || q.activeBytes != 0 || len(s.finished) != 1 {
		t.Fatal("missing row after ambiguous commit was not reconciled")
	}
}
func TestFinishRetainsSaveBarrierForOtherErrors(t *testing.T) {
	s := &fakeStore{}
	q := configured(t, s)
	if err := q.Add(DownloadRequest{MagnetLink: testMagnet}); err != nil {
		t.Fatal(err)
	}
	s.saveErr = errors.New("offline")
	q.finish(q.tasks[0], StatusTaskCompleted, nil)
	if len(s.finished) != 0 || len(q.tasks) != 1 {
		t.Fatal("Finish bypassed terminal save barrier")
	}
}
func TestAddRequiresPersistenceAndCanonicalDedup(t *testing.T) {
	if err := New().Add(DownloadRequest{MagnetLink: testMagnet}); err == nil {
		t.Fatal("unconfigured queue accepted job")
	}
	s := &fakeStore{createErr: errors.New("offline")}
	q := configured(t, s)
	if err := q.Add(DownloadRequest{MagnetLink: testMagnet}); err == nil || len(q.Snapshot().Tasks) != 0 {
		t.Fatal("failed persistence accepted")
	}
	s.createErr = nil
	selection := []int{0}
	owner := "alice"
	if err := q.Add(DownloadRequest{MagnetLink: testMagnet, Selected: selection, UserID: &owner}); err != nil {
		t.Fatal(err)
	}
	selection[0] = 9
	owner = "mallory"
	snap := q.Snapshot()
	if _, err := uuid.Parse(snap.Tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	if q.tasks[0].job.Selected[0] != 0 || snap.Tasks[0].UserID != "alice" {
		t.Fatal("request retained caller memory")
	}
	if err := q.Add(DownloadRequest{MagnetLink: testMagnet + "&tr=http://other"}); err == nil {
		t.Fatal("same hash accepted")
	}
}
func TestSelectionValidation(t *testing.T) {
	for _, s := range [][]int{{}, {-1}, {2}, {0, 0}} {
		if _, err := selectedIndexes(s, 2); err == nil {
			t.Fatalf("accepted %v", s)
		}
	}
	all, err := selectedIndexes(nil, 2)
	if err != nil || len(all) != 2 {
		t.Fatal("nil must select all")
	}
	q := configured(t, &fakeStore{})
	if err = q.Add(DownloadRequest{MagnetLink: testMagnet, Selected: []int{}}); err == nil {
		t.Fatal("explicit empty accepted")
	}
}
func TestStopOwnedDoesNotPermitOtherOwnerOrGuest(t *testing.T) {
	s := &fakeStore{}
	q := configured(t, s)
	owner := "alice"
	if err := q.Add(DownloadRequest{MagnetLink: testMagnet, UserID: &owner}); err != nil {
		t.Fatal(err)
	}
	id := q.Snapshot().Tasks[0].ID
	for _, user := range []string{"", "bob"} {
		if err := q.StopOwned(id, user); err == nil {
			t.Fatal("unauthorized stop")
		}
	}
	if err := q.StopOwned(id, "alice"); err != nil {
		t.Fatal(err)
	}
	if s.saved[0].Status != StatusStopped {
		t.Fatal("stop not durable")
	}
}
func TestRecoveryNeverReplaysAmbiguousUpload(t *testing.T) {
	s := &fakeStore{active: []jobs.Job{{ID: uuid.NewString(), InfoHash: "0123456789012345678901234567890123456789", Magnet: testMagnet, Status: StatusUploading, Stage: StatusUploading}}}
	q := configured(t, s)
	if q.Snapshot().Tasks[0].Status != StatusFailed {
		t.Fatal("interrupted upload replayed")
	}
	q.finish(q.tasks[0], StatusFailed, nil)
	if len(s.finished) != 1 || len(q.Snapshot().Tasks) != 0 {
		t.Fatal("terminal recovery not finished")
	}
}
func TestRecoveryTerminalStatesNeverStartTorrent(t *testing.T) {
	for _, status := range []string{StatusTaskCompleted, StatusFailed, StatusStopped} {
		t.Run(status, func(t *testing.T) {
			s := &fakeStore{active: []jobs.Job{{ID: uuid.NewString(), InfoHash: "0123456789012345678901234567890123456789", Magnet: testMagnet, Status: status, Stage: status}}}
			q := configured(t, s)
			// nil clients deliberately panic if recovery attempts network work.
			q.run(q.tasks[0], nil, nil, "", nil, nil)
			if len(s.finished) != 1 || s.finished[0].Status != status {
				t.Fatal("terminal recovery replayed work")
			}
		})
	}
}
func TestFinishDatabaseFailureRetainsTerminalJobAndArtifacts(t *testing.T) {
	s := &fakeStore{errorFinish: errors.New("offline")}
	q := configured(t, s)
	if err := q.Add(DownloadRequest{MagnetLink: testMagnet}); err != nil {
		t.Fatal(err)
	}
	task := q.tasks[0]
	dir, err := q.jobPath(task.job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	q.finish(task, StatusTaskCompleted, nil)
	if len(q.tasks) != 1 || task.job.Status != StatusTaskCompleted || task.running {
		t.Fatal("failed finish discarded job")
	}
	if _, err = os.Stat(dir); err != nil {
		t.Fatal("removed artifacts before transaction")
	}
	s.errorFinish = nil
	q.finish(task, StatusTaskCompleted, nil)
	if len(q.tasks) != 0 {
		t.Fatal("finish retry not removed")
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("successful finish not cleaned")
	}
}
func TestStagingOnlySelectedFilesAndRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "torrent", "name")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"wanted", "boundary"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := stagePaths(root, []string{"name/wanted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "payload", "name", "boundary")); !os.IsNotExist(err) {
		t.Fatal("unselected boundary staged")
	}
	src, _ := os.Stat(filepath.Join(dir, "wanted"))
	dst, _ := os.Stat(filepath.Join(root, "payload", "name", "wanted"))
	if !os.SameFile(src, dst) {
		t.Fatal("staging copied rather than linked")
	}
	if err := os.Symlink("wanted", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := stagePaths(root, []string{"name/link"}); err == nil {
		t.Fatal("symlink accepted")
	}
}
func TestPhysicalAllocationDeduplicatesStagingLinks(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "data")
	if err := os.WriteFile(file, make([]byte, 8192), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := allocatedBytes(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Link(file, filepath.Join(root, "staged")); err != nil {
		t.Fatal(err)
	}
	after, err := allocatedBytes(root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("hardlink double counted: %d -> %d", before, after)
	}
	if remainingAllocation(50, 40) != 10 || remainingAllocation(40, 50) != 0 {
		t.Fatal("partial recovery allocation counted twice")
	}
}
func TestUnsafePathsAndReservations(t *testing.T) {
	for _, path := range []string{"", "../escape", "/absolute", "a/../escape", "a//b", "a\\b", "./a"} {
		if _, err := safeRelative(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	unzipped, err := reservation(1000, 10, false, 2)
	if err != nil || unzipped < 1000 {
		t.Fatal("full torrent not reserved")
	}
	zipped, err := reservation(1000, 10, true, 2)
	if err != nil || zipped < unzipped+10 {
		t.Fatal("zip not reserved")
	}
	if _, err := reservation(math.MaxInt64, 1, true, 1); err == nil {
		t.Fatal("overflow accepted")
	}
	if returnPercentageCompleted(0, 0) != 0 {
		t.Fatal("zero total must not produce NaN")
	}
}

// One selected and one unwanted file share the first piece. The unwanted
// file's last piece is absent, so torrent completion is false while selected
// completion must be true. No sockets, trackers, DHT, or remote API are used.
func TestLocalTorrentSelectedCompletionAndBoundaryStaging(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "torrent")
	if err := os.MkdirAll(filepath.Join(dataDir, "bundle", "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "bundle", "dir", "wanted"), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	// Only the boundary byte from the unwanted file is present.
	if err := os.WriteFile(filepath.Join(dataDir, "bundle", "unwanted"), []byte("d"), 0600); err != nil {
		t.Fatal(err)
	}
	p1 := sha1.Sum([]byte("abcd"))
	p2 := sha1.Sum([]byte("efgh"))
	info := metainfo.Info{Name: "bundle", PieceLength: 4, Pieces: append(p1[:], p2[:]...), Files: []metainfo.FileInfo{{Length: 3, Path: []string{"dir", "wanted"}}, {Length: 5, Path: []string{"unwanted"}}}}
	raw, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = t.TempDir()
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.DisablePEX = true
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	cfg.DisableWebtorrent = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableWebseeds = true
	client, err := torrent.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	disk := storage.NewFile(dataDir)
	defer disk.Close()
	spec := &torrent.TorrentSpec{InfoHash: metainfo.HashBytes(raw), InfoBytes: raw, Storage: disk, DisallowDataDownload: true, DisallowDataUpload: true}
	tor, _, err := client.AddTorrentSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	dropped := false
	defer func() {
		if !dropped {
			tor.Drop()
		}
	}()
	mi := tor.Metainfo()
	if err := persistMetadata(root, &mi); err != nil {
		t.Fatal(err)
	}
	recovered, err := loadSpec(root, jobs.Job{InfoHash: mi.HashInfoBytes().String(), Magnet: testMagnet})
	if err != nil || len(recovered.InfoBytes) == 0 {
		t.Fatalf("offline metadata recovery failed: %v", err)
	}
	if _, err := loadSpec(root, jobs.Job{InfoHash: "wrong"}); err == nil {
		t.Fatal("wrong metadata hash accepted")
	}
	files := tor.Files()
	chosen := []*torrent.File{files[0]}
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		completed, done := selectedProgress(tor, chosen)
		if done {
			if completed != 3 {
				t.Fatalf("selected bytes = %d", completed)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("selected file did not verify")
		case <-ticker.C:
		}
	}
	if tor.Complete().Bool() {
		t.Fatal("whole torrent unexpectedly complete")
	}
	// Drop before linking, just as the production worker does.
	tor.Drop()
	dropped = true
	if err := stageSelected(root, chosen); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "payload", "bundle", "dir", "wanted"))
	if err != nil || string(got) != "abc" {
		t.Fatalf("selected payload %q: %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "payload", "bundle", "unwanted")); !os.IsNotExist(err) {
		t.Fatal("unwanted partial boundary file leaked to payload")
	}
}
func TestJobPathRejectsEscapes(t *testing.T) {
	q := configured(t, &fakeStore{})
	if _, err := q.jobPath("../escape"); err == nil {
		t.Fatal("non-UUID path accepted")
	}
	id := uuid.NewString()
	if err := os.Symlink(t.TempDir(), filepath.Join(q.dataDir, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.jobPath(id); err == nil {
		t.Fatal("symlink job root accepted")
	}
}

// These tests intentionally use the production default logger, without a DB,
// tracker, remote API, or network client.
func captureQueueLogs(t *testing.T) (*bytes.Buffer, func() []map[string]any) {
	t.Helper()
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &output, func() []map[string]any {
		t.Helper()
		var events []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
			if line == "" {
				continue
			}
			var event map[string]any
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
		}
		return events
	}
}

func assertQueueEvent(t *testing.T, events []map[string]any, event, id string) map[string]any {
	t.Helper()
	for _, entry := range events {
		if entry["msg"] == event && entry["job_id"] == id {
			for _, key := range []string{"stage", "status", "selected_bytes", "reserved_bytes", "reason"} {
				if _, ok := entry[key]; !ok {
					t.Fatalf("%s missing %s: %v", event, key, entry)
				}
			}
			if entry["reason"] == "" {
				t.Fatalf("%s has no reason", event)
			}
			return entry
		}
	}
	t.Fatalf("missing %s for job %s: %v", event, id, events)
	return nil
}

func TestStructuredQueueLifecycleExcludesSensitiveFields(t *testing.T) {
	output, events := captureQueueLogs(t)
	s := &fakeStore{}
	q := configured(t, s)
	owner := "private-user@example.invalid"
	magnet := testMagnet + "-private-name&tr=https://tracker.invalid/private-token"
	if err := q.Add(DownloadRequest{MagnetLink: magnet, UserID: &owner}); err != nil {
		t.Fatal(err)
	}
	task := q.tasks[0]
	task.job.Size, task.job.Reserved = 123, 456
	for _, stage := range []string{StatusStarted, StatusDownloading, StatusZipping, StatusUploading} {
		if err := q.saveStage(task, stage); err != nil {
			t.Fatal(err)
		}
	}
	q.finishWithReason(task, StatusTaskCompleted, nil, "upload_completed")
	got := events()
	assertQueueEvent(t, got, "accepted", task.job.ID)
	count := 0
	for _, entry := range got {
		if entry["msg"] == "stage_changed" {
			count++
			if entry["level"] != "INFO" || entry["job_id"] != task.job.ID {
				t.Fatalf("bad transition: %v", entry)
			}
		}
	}
	if count != 4 {
		t.Fatalf("stage transitions = %d", count)
	}
	finished := assertQueueEvent(t, got, "job_finished", task.job.ID)
	if finished["status"] != StatusTaskCompleted || finished["selected_bytes"] != float64(123) || finished["reserved_bytes"] != float64(456) {
		t.Fatalf("bad final fields: %v", finished)
	}
	for _, secret := range []string{"magnet:", "private-name", "private-token", owner, q.dataDir} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("sensitive value logged: %q", secret)
		}
	}
}

func TestRunEarlyFailuresHaveActionableSanitizedReasons(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		prepare      func(*testing.T, *DownloadQueue, *DownloadTask)
	}{
		{"unsafe_job_directory", "job_directory_invalid", func(t *testing.T, q *DownloadQueue, task *DownloadTask) {
			if err := os.Symlink(t.TempDir(), filepath.Join(q.dataDir, task.job.ID)); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink_in_storage", "job_storage_validation_failed", func(t *testing.T, q *DownloadQueue, task *DownloadTask) {
			dir := filepath.Join(q.dataDir, task.job.ID)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("sensitive-target", filepath.Join(dir, "private-name")); err != nil {
				t.Fatal(err)
			}
		}},
		{"torrent_directory", "torrent_directory_create_failed", func(t *testing.T, q *DownloadQueue, task *DownloadTask) {
			dir := filepath.Join(q.dataDir, task.job.ID)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "torrent"), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"metadata", "torrent_metadata_load_failed", func(t *testing.T, q *DownloadQueue, task *DownloadTask) {
			dir := filepath.Join(q.dataDir, task.job.ID)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "metadata.torrent"), []byte("private-token corrupt metadata"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, events := captureQueueLogs(t)
			q := configured(t, &fakeStore{})
			if err := q.Add(DownloadRequest{MagnetLink: testMagnet + "-private-name"}); err != nil {
				t.Fatal(err)
			}
			task := q.tasks[0]
			tc.prepare(t, q, task)
			// No torrent client: these failures must occur before any network work.
			q.run(task, nil, nil, "", nil, nil)
			got := events()
			assertQueueEvent(t, got, "job_started", task.job.ID)
			failed := assertQueueEvent(t, got, "failed", task.job.ID)
			if failed["reason"] != tc.reason || failed["level"] != "ERROR" || failed["status"] != StatusFailed {
				t.Fatalf("bad failure: %v", failed)
			}
			assertQueueEvent(t, got, "job_finished", task.job.ID)
			for _, secret := range []string{"magnet:", "private-name", "sensitive-target", "private-token", q.dataDir} {
				if strings.Contains(output.String(), secret) {
					t.Fatalf("sensitive value logged: %q", secret)
				}
			}
		})
	}
}

func TestRecoveryAndPersistenceFailuresLogSafeContext(t *testing.T) {
	output, events := captureQueueLogs(t)
	id := uuid.NewString()
	s := &fakeStore{active: []jobs.Job{{ID: id, InfoHash: "0123456789012345678901234567890123456789", Magnet: testMagnet, Name: "private-name", UserID: "secret@example.invalid", Status: StatusUploading, Stage: StatusUploading}}, errorFinish: errors.New("remote token=private-token path=/private-name secret@example.invalid")}
	q := configured(t, s)
	q.run(q.tasks[0], nil, nil, "", nil, nil)
	got := events()
	recovered := assertQueueEvent(t, got, "recovered", id)
	if recovered["reason"] != "interrupted_upload_not_replayed" {
		t.Fatalf("bad recovery: %v", recovered)
	}
	failed := assertQueueEvent(t, got, "failed", id)
	if failed["reason"] != "interrupted_upload_not_replayed" {
		t.Fatalf("lost recovery reason: %v", failed)
	}
	retry := assertQueueEvent(t, got, "job_finish_retry", id)
	if retry["reason"] != "terminal_persist_failed" || retry["error_type"] != "*errors.errorString" {
		t.Fatalf("unsafe or missing error context: %v", retry)
	}
	for _, secret := range []string{"magnet:", "private-name", "private-token", "secret@example.invalid", q.dataDir} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("sensitive value logged: %q", secret)
		}
	}
}
