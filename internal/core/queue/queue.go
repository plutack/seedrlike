// Package queue runs durable, isolated download jobs. Remote uploads are not
// idempotent: an interrupted uploading stage is failed on restart, never replayed.
package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/google/uuid"
	"github.com/plutack/go-gofile/api"
	"github.com/plutack/seedrlike/internal/core/upload"
	ws "github.com/plutack/seedrlike/internal/core/websocket"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
	"github.com/plutack/seedrlike/internal/jobs"
	"golang.org/x/sys/unix"
)

const (
	StatusNextPhase      = "Starting next phase"
	StatusPending        = "pending"
	StatusStarted        = "started"
	StatusTaskCompleted  = "completed"
	StatusUploading      = "uploading"
	StatusDownloading    = "downloading"
	StatusFailed         = "failed"
	StatusZipping        = "zipping"
	StatusStopped        = "stopped"
	maxQueueSize         = 10
	bytesPerGB           = 1 << 30
	defaultMaxActiveGB   = 50
	maxActiveDownloadEnv = "MAX_ACTIVE_DOWNLOAD_GB"
)

type DownloadRequest struct {
	MagnetLink string
	IsZipped   bool
	UserID     *string
	// nil selects every file; an explicit empty list is invalid.
	Selected []int
}

type jobStore interface {
	Create(context.Context, *jobs.Job) error
	Active(context.Context) ([]jobs.Job, error)
	Save(context.Context, jobs.Job) error
	Finish(context.Context, jobs.Job, string) error
}

type DownloadTask struct {
	job           jobs.Job
	running       bool
	reserved      bool
	completed     int64
	speed         float64
	failureReason string
}

type TaskSnapshot struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	UserID    string  `json:"user_id"`
	Size      int64   `json:"size"`
	Reserved  int64   `json:"reserved"`
	Completed int64   `json:"completed"`
	Speed     float64 `json:"speed"`
}

type QueueSnapshot struct {
	BudgetBytes   int64          `json:"budget_bytes"`
	ReservedBytes int64          `json:"reserved_bytes"`
	Tasks         []TaskSnapshot `json:"tasks"`
}

type DownloadQueue struct {
	mu             sync.Mutex
	tasks          []*DownloadTask
	maxActiveBytes int64
	activeBytes    int64
	store          jobStore
	dataDir        string
	notify         func(context.Context, string, string, string) error
	processing     bool
}

func maxActiveBytesFromEnv() int64 {
	gb, err := strconv.ParseInt(os.Getenv(maxActiveDownloadEnv), 10, 64)
	if err != nil || gb <= 0 || gb > math.MaxInt64/bytesPerGB {
		gb = defaultMaxActiveGB
	}
	return gb * bytesPerGB
}

func New() *DownloadQueue { return &DownloadQueue{maxActiveBytes: maxActiveBytesFromEnv()} }

// Configure must succeed before Add or ProcessTasks. Existing payloads are kept.
func (q *DownloadQueue) Configure(store *jobs.Store, dataDir string, notify func(context.Context, string, string, string) error) error {
	if store == nil {
		return errors.New("job store is required")
	}
	return q.configure(store, dataDir, notify)
}

func (q *DownloadQueue) configure(store jobStore, dataDir string, notify func(context.Context, string, string, string) error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.store != nil || q.processing {
		return errors.New("queue already configured")
	}
	if store == nil || dataDir == "" {
		return errors.New("job store and data directory are required")
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	root := filepath.Join(abs, "jobs")
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if fi, e := os.Lstat(root); e != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe jobs directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	recovered, err := store.Active(ctx)
	if err != nil {
		return err
	}
	tasks := make([]*DownloadTask, 0, len(recovered))
	hashes := map[string]bool{}
	for _, j := range recovered {
		id, e := uuid.Parse(j.ID)
		if e != nil || id.String() != j.ID {
			return errors.New("invalid persisted job ID")
		}
		m, e := metainfo.ParseMagnetUri(j.Magnet)
		if e != nil || m.InfoHash.String() != j.InfoHash || hashes[j.InfoHash] {
			return errors.New("invalid or duplicate persisted job")
		}
		hashes[j.InfoHash] = true
		j.Selected = cloneSelection(j.Selected)
		recoveredStage := j.Stage
		// No retry after ambiguous remote side effects. An operator may reconcile
		// remote artifacts; a new submission is an explicit new upload attempt.
		if j.Stage == StatusUploading {
			j.Status = StatusFailed
			j.Stage = StatusFailed
		}
		if !terminal(j.Status) {
			j.Status = StatusPending
		}
		task := &DownloadTask{job: j}
		reason := "restart_resume"
		if j.Status == StatusFailed {
			reason = "recovered_terminal_failure"
			if recoveredStage == StatusUploading {
				reason = "interrupted_upload_not_replayed"
			}
			task.failureReason = reason
		}
		logJob(slog.LevelInfo, "recovered", j, reason, nil)
		tasks = append(tasks, task)
	}
	q.store = store
	q.dataDir = root
	q.notify = notify
	q.tasks = tasks
	return nil
}

func cloneSelection(s []int) []int {
	if s == nil {
		return nil
	}
	return append([]int{}, s...)
}

func (q *DownloadQueue) Add(r DownloadRequest) error {
	m, err := metainfo.ParseMagnetUri(r.MagnetLink)
	if err != nil {
		return errors.New("invalid magnet link")
	}
	if r.Selected != nil && len(r.Selected) == 0 {
		return errors.New("select at least one file")
	}
	seen := map[int]bool{}
	for _, i := range r.Selected {
		if i < 0 || seen[i] {
			return errors.New("invalid file selection")
		}
		seen[i] = true
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.store == nil {
		return errors.New("queue is not configured")
	}
	if len(q.tasks) >= maxQueueSize {
		return errors.New("download queue full")
	}
	for _, t := range q.tasks {
		if t.job.InfoHash == m.InfoHash.String() {
			return errors.New("torrent already active")
		}
	}
	j := jobs.Job{ID: uuid.NewString(), InfoHash: m.InfoHash.String(), Magnet: r.MagnetLink, Name: m.DisplayName, Status: StatusPending, Stage: StatusPending, Zipped: r.IsZipped, Selected: cloneSelection(r.Selected)}
	if r.UserID != nil {
		j.UserID = *r.UserID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = q.store.Create(ctx, &j); err != nil {
		return fmt.Errorf("persist job: %w", err)
	}
	q.tasks = append(q.tasks, &DownloadTask{job: j})
	logJob(slog.LevelInfo, "accepted", j, "submission_persisted", nil)
	return nil
}

// Stop is the trusted/internal cancellation API. HTTP callers must use StopOwned.
func (q *DownloadQueue) Stop(id string) error { return q.stop(id, "", false) }
func (q *DownloadQueue) StopOwned(id, userID string) error {
	if userID == "" {
		return errors.New("authentication required")
	}
	return q.stop(id, userID, true)
}
func (q *DownloadQueue) stop(id, user string, owned bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, t := range q.tasks {
		if t.job.ID != id || (owned && t.job.UserID != user) {
			continue
		}
		if terminal(t.job.Status) || t.job.Stage == StatusUploading || t.job.Stage == StatusZipping {
			return errors.New("job cannot be stopped in this stage")
		}
		j := t.job
		j.Status = StatusStopped
		j.Stage = StatusStopped
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := q.store.Save(ctx, j); err != nil {
			return err
		}
		t.job = j
		logJob(slog.LevelInfo, "stage_changed", j, "stop_requested", nil)
		return nil
	}
	return errors.New("job not found")
}
func terminal(s string) bool {
	return s == StatusStopped || s == StatusFailed || s == StatusTaskCompleted
}

func (q *DownloadQueue) Snapshot() QueueSnapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	s := QueueSnapshot{BudgetBytes: q.maxActiveBytes, ReservedBytes: q.activeBytes, Tasks: make([]TaskSnapshot, 0, len(q.tasks))}
	for _, t := range q.tasks {
		j := t.job
		s.Tasks = append(s.Tasks, TaskSnapshot{ID: j.ID, Name: j.Name, Status: j.Status, UserID: j.UserID, Size: j.Size, Reserved: j.Reserved, Completed: t.completed, Speed: t.speed})
	}
	return s
}

func ProcessTasks(c *torrent.Client, q *DownloadQueue, u *api.Api, root string, db *database.Queries, wm *ws.WebsocketManager) {
	q.mu.Lock()
	if q.store == nil || q.processing {
		q.mu.Unlock()
		slog.Error("queue_processor_failed", "reason", "queue_unconfigured_or_already_processing")
		return
	}
	q.processing = true
	q.mu.Unlock()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		q.mu.Lock()
		for _, t := range q.tasks {
			if !t.running {
				t.running = true
				go q.run(t, c, u, root, db, wm)
			}
		}
		q.mu.Unlock()
		<-ticker.C
	}
}

func (q *DownloadQueue) current(t *DownloadTask) jobs.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	return t.job
}
func (q *DownloadQueue) saveStage(t *DownloadTask, status string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if t.job.Status == StatusStopped {
		return errors.New("job stopped")
	}
	j := t.job
	j.Status = status
	j.Stage = status
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := q.store.Save(ctx, j); err != nil {
		return err
	}
	t.job = j
	logJob(slog.LevelInfo, "stage_changed", j, "stage_persisted", nil)
	t.speed = 0
	if status == StatusZipping || status == StatusUploading {
		t.completed = 0
	}
	return nil
}

func (q *DownloadQueue) run(task *DownloadTask, c *torrent.Client, u *api.Api, remoteRoot string, db *database.Queries, wm *ws.WebsocketManager) {
	j := q.current(task)
	logJob(slog.LevelInfo, "job_started", j, "worker_started", nil)
	if terminal(j.Status) {
		q.finish(task, j.Status, wm)
		return
	}
	q.emit(task, wm)
	status := StatusFailed
	reason := "job_directory_invalid"
	defer func() {
		if q.current(task).Status == StatusStopped {
			status = StatusStopped
			reason = "stop_requested"
		}
		q.finishWithReason(task, status, wm, reason)
	}()
	jobDir, err := q.jobPath(j.ID)
	if err != nil {
		return
	}
	reason = "job_directory_create_failed"
	if err = os.MkdirAll(jobDir, 0700); err != nil {
		return
	}
	// Refuse every symlink in recovered storage before handing it to a backend
	// that uses ordinary filesystem opens. The job tree is private (0700).
	reason = "job_storage_validation_failed"
	if err = rejectSymlinks(jobDir); err != nil {
		return
	}
	torrentDir := filepath.Join(jobDir, "torrent")
	reason = "torrent_directory_create_failed"
	if err = os.MkdirAll(torrentDir, 0700); err != nil {
		return
	}
	disk := storage.NewFile(torrentDir)
	reason = "torrent_metadata_load_failed"
	spec, err := loadSpec(jobDir, j)
	if err != nil {
		disk.Close()
		return
	}
	spec.Storage = disk
	spec.DisallowDataUpload = true
	spec.DisallowDataDownload = true
	reason = "isolated_torrent_create_failed"
	t, fresh, err := c.AddTorrentSpec(spec)
	if err != nil || !fresh {
		disk.Close()
		return
	}
	closed := false
	defer func() {
		if !closed {
			t.Drop()
			if e := disk.Close(); e != nil {
				logJob(slog.LevelWarn, "job_storage_close_failed", q.current(task), "torrent_storage_close_failed", e)
			}
		}
	}()
	reason = "started_stage_persist_failed"
	if err = q.saveStage(task, StatusStarted); err != nil {
		return
	}
	infoTimeout := time.NewTimer(time.Minute)
	defer infoTimeout.Stop()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
infoWait:
	for {
		select {
		case <-t.GotInfo():
			break infoWait
		case <-infoTimeout.C:
			reason = "metadata_timeout"
			return
		case <-poll.C:
			if q.current(task).Status == StatusStopped {
				return
			}
		}
	}
	mi := t.Metainfo()
	reason = "metadata_persist_failed"
	if err = persistMetadata(jobDir, &mi); err != nil {
		return
	}
	files := t.Files()
	reason = "file_selection_invalid"
	indexes, err := selectedIndexes(j.Selected, len(files))
	if err != nil {
		return
	}
	chosen := make([]*torrent.File, 0, len(indexes))
	var selectedSize int64
	for _, f := range files {
		f.SetPriority(torrent.PiecePriorityNone)
	}
	// Validate all metadata paths, including unselected boundary files.
	reason = "torrent_path_unsafe"
	for _, f := range files {
		if _, err = safeRelative(f.Path()); err != nil {
			return
		}
		fi := f.FileInfo()
		if strings.Contains(fi.Attr, "l") || len(fi.SymlinkPath) != 0 {
			reason = "torrent_symlink_rejected"
			return
		}
		for _, part := range fi.BestPath() {
			if _, err = safeRelative(part); err != nil || strings.Contains(part, "/") {
				return
			}
		}
	}
	name := t.Name()
	reason = "torrent_name_unsafe"
	if _, err = safeRelative(name); err != nil || filepath.Base(name) != name {
		return
	}
	reason = "selected_size_invalid"
	for _, i := range indexes {
		f := files[i]
		if f.Length() < 0 || selectedSize > math.MaxInt64-f.Length() {
			return
		}
		selectedSize += f.Length()
		chosen = append(chosen, f)
	}
	reason = "reservation_invalid"
	reserve, err := reservation(t.Length(), selectedSize, j.Zipped, len(files))
	if err != nil {
		return
	}
	q.mu.Lock()
	task.job.Name = t.Name()
	task.job.Size = selectedSize
	task.job.Reserved = reserve
	q.mu.Unlock()
	if reserve > q.maxActiveBytes {
		reason = "reservation_exceeds_budget"
		return
	}
	// Recovery always rehashes partial data, rather than trusting completion DB.
	t.VerifyData()
	waitLogged := false
	reason = "capacity_check_failed"
	for {
		if q.current(task).Status == StatusStopped {
			return
		}
		q.mu.Lock()
		var stat unix.Statfs_t
		err = unix.Statfs(q.dataDir, &stat)
		reason = "filesystem_capacity_check_failed"
		free := uint64(stat.Bavail) * uint64(stat.Bsize)
		// Existing partial data already reduces Statfs free space. Reserve only
		// outstanding allocations here, while retaining full logical budget.
		need := int64(0)
		if err == nil {
			for _, other := range q.tasks {
				if other != task && !other.reserved {
					continue
				}
				allocated, e := allocatedBytes(filepath.Join(q.dataDir, other.job.ID))
				if e != nil {
					reason = "allocated_storage_inspection_failed"
					err = e
					break
				}
				remaining := remainingAllocation(other.job.Reserved, allocated)
				if need > math.MaxInt64-remaining {
					reason = "disk_obligation_overflow"
					err = errors.New("disk obligation overflow")
					break
				}
				need += remaining
			}
		}
		if err == nil && q.activeBytes <= q.maxActiveBytes-reserve && uint64(need) <= free {
			q.activeBytes += reserve
			task.reserved = true
			q.mu.Unlock()
			break
		}
		q.mu.Unlock()
		if err != nil {
			return
		}
		if !waitLogged {
			logJob(slog.LevelInfo, "capacity_wait", q.current(task), "waiting_for_capacity", nil)
			waitLogged = true
		}
		<-poll.C
	}
	reason = "downloading_stage_persist_failed"
	if err = q.saveStage(task, StatusDownloading); err != nil {
		return
	}
	for _, f := range chosen {
		f.SetPriority(torrent.PiecePriorityNormal)
	}
	t.AllowDataDownload()
	previous, _ := selectedProgress(t, chosen)
	sampled := time.Now()
	for {
		if q.current(task).Status == StatusStopped {
			return
		}
		completed, done := selectedProgress(t, chosen)
		now := time.Now()
		speed := float64(completed-previous) / now.Sub(sampled).Seconds()
		if speed < 0 {
			speed = 0
		}
		q.mu.Lock()
		task.completed = completed
		task.speed = speed
		q.mu.Unlock()
		q.emit(task, wm)
		if done {
			break
		}
		previous = completed
		sampled = now
		<-poll.C
	}
	// Drop before closing storage or staging: no writer can mutate upload payloads.
	t.Drop()
	closed = true
	reason = "torrent_storage_close_failed"
	if err = disk.Close(); err != nil {
		return
	}
	payload := filepath.Join(jobDir, "payload")
	reason = "selected_payload_stage_failed"
	if err = stageSelected(jobDir, chosen); err != nil {
		return
	}
	// Files preserve the torrent root name inside the allowlist payload. Upload
	// that named root rather than the internal staging directory.
	uploadPath := filepath.Join(payload, name)
	if j.Zipped {
		reason = "zipping_stage_persist_failed"
		if err = q.saveStage(task, StatusZipping); err != nil {
			return
		}
		q.emit(task, wm)
		uploadPath = filepath.Join(jobDir, name+".zip")
		reason = "payload_zip_failed"
		if err = upload.ZipFolder(filepath.Join(payload, name), uploadPath, wm, func(done, total int64) { q.mu.Lock(); task.completed = done; q.mu.Unlock(); q.emit(task, wm) }); err != nil {
			return
		}
	}
	// Persist before the FIRST remote action, including folder creation. A crash
	// after this point has ambiguous remote effects and will not be retried.
	reason = "uploading_stage_persist_failed"
	if err = q.saveStage(task, StatusUploading); err != nil {
		return
	}
	q.emit(task, wm)
	reason = "upload_server_lookup_failed"
	servers, err := u.GetAvailableServers("eu")
	if err != nil || len(servers.Data.Servers) == 0 {
		if err == nil {
			reason = "upload_servers_unavailable"
		}
		return
	}
	j = q.current(task)
	var user *string
	if j.UserID != "" {
		owner := j.UserID
		user = &owner
	}
	reason = "remote_upload_failed"
	if err = upload.SendTorrentToServerWithProgress(uploadPath, u, remoteRoot, servers.Data.Servers[0].Name, j.ID, db, wm, j.Name, user); err != nil {
		return
	}
	status = StatusTaskCompleted
	reason = "upload_completed"
}

func (q *DownloadQueue) emit(t *DownloadTask, wm *ws.WebsocketManager) {
	if wm == nil {
		return
	}
	q.mu.Lock()
	j := t.job
	done, speed := t.completed, t.speed
	q.mu.Unlock()
	var user *string
	if j.UserID != "" {
		owner := j.UserID
		user = &owner
	}
	wm.SendProgress(ws.TorrentUpdate{Type: "torrent update", ID: j.ID, Name: j.Name, Status: j.Status, Progress: returnPercentageCompleted(done, j.Size), Speed: formatSpeed(speed), ETA: etaFromRate(done, j.Size, speed), TotalSize: j.Size, BytesCompleted: done, UserID: user})
}

func (q *DownloadQueue) finish(t *DownloadTask, status string, wm *ws.WebsocketManager) {
	q.finishWithReason(t, status, wm, "terminal_state_recovered")
}

func (q *DownloadQueue) finishWithReason(t *DownloadTask, status string, wm *ws.WebsocketManager, reason string) {
	q.mu.Lock()
	if status == StatusFailed {
		if t.failureReason == "" {
			t.failureReason = reason
		}
		reason = t.failureReason
		failedJob := t.job
		failedJob.Status = StatusFailed
		logJob(slog.LevelError, "failed", failedJob, reason, nil)
	}
	t.job.Status = status
	t.job.Stage = status
	t.speed = 0
	if status == StatusTaskCompleted {
		t.completed = t.job.Size
	}
	j := t.job
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	// Retain terminal state in memory and retry Finish on DB failure. Saving the
	// terminal state also prevents replay if the transaction fails then we crash.
	saveErr := q.store.Save(ctx, j)
	err := saveErr
	// A prior Finish commit can succeed on MySQL even when its response is lost.
	// Save then sees a missing row. Re-enter idempotent Finish to reconcile that
	// outcome; it only adds metrics when its DELETE actually removes a row.
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		err = q.store.Finish(ctx, j, status)
	}
	cancel()
	if err != nil {
		t.running = false
		q.mu.Unlock()
		logJob(slog.LevelError, "job_finish_retry", j, "terminal_persist_failed", err)
		return
	}
	for i, candidate := range q.tasks {
		if candidate == t {
			q.tasks = append(q.tasks[:i], q.tasks[i+1:]...)
			break
		}
	}
	if t.reserved {
		q.activeBytes -= j.Reserved
		t.reserved = false
	}
	notify := q.notify
	q.mu.Unlock()
	// Keep failed job artifacts for manual inspection, especially ambiguous
	// interrupted remote uploads. They are not automatically uploaded again.
	if path, e := q.jobPath(j.ID); status != StatusFailed && e == nil {
		// jobPath validates the canonical UUID and the resolved parent. Root removal
		// cannot follow a swapped symlink outside the dedicated jobs directory.
		root, e := os.OpenRoot(q.dataDir)
		if e == nil {
			e = root.RemoveAll(filepath.Base(path))
			root.Close()
		}
		if e != nil {
			logJob(slog.LevelWarn, "job_cleanup_failed", j, "artifact_cleanup_failed", e)
		}
	}
	logJob(slog.LevelInfo, "job_finished", j, reason, nil)
	q.emit(t, wm)
	if status == StatusTaskCompleted && wm != nil {
		wm.SendProgress(ws.RefreshUpdate{Type: "upload refresh", Message: "content uploaded on gofile"})
	}
	if notify != nil && j.UserID != "" && (status == StatusTaskCompleted || status == StatusFailed) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := notify(ctx, j.UserID, j.Name, status); err != nil {
				logJob(slog.LevelWarn, "job_notification_failed", j, "notification_delivery_failed", err)
			}
		}()
	}
}

// logJob deliberately allowlists operational fields. Error messages (including
// filesystem and remote API errors) may contain paths, credentials, or user data.
func logJob(level slog.Level, event string, j jobs.Job, reason string, err error) {
	attrs := []any{"job_id", j.ID, "stage", j.Stage, "status", j.Status,
		"selected_bytes", j.Size, "reserved_bytes", j.Reserved, "reason", reason}
	if err != nil {
		attrs = append(attrs, "error_type", fmt.Sprintf("%T", err))
	}
	slog.Log(context.Background(), level, event, attrs...)
}

func (q *DownloadQueue) jobPath(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return "", errors.New("invalid job directory ID")
	}
	parent, err := filepath.EvalSymlinks(q.dataDir)
	if err != nil || parent != q.dataDir {
		return "", errors.New("unsafe job root")
	}
	target := filepath.Join(parent, id)
	if fi, e := os.Lstat(target); e == nil && (!fi.IsDir() || fi.Mode()&os.ModeSymlink != 0) {
		return "", errors.New("unsafe job directory")
	} else if e != nil && !os.IsNotExist(e) {
		return "", e
	}
	return target, nil
}

func safeRelative(path string) (string, error) {
	if path == "" || strings.Contains(path, "\\") || strings.ContainsRune(path, 0) || !filepath.IsLocal(path) {
		return "", errors.New("unsafe torrent path")
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("unsafe torrent path component")
		}
	}
	return filepath.FromSlash(path), nil
}

func rejectSymlinks(root string) error {
	return filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink in job data")
		}
		return nil
	})
}

// stageSelected hardlinks ONLY the explicit allowlist. Unwanted boundary pieces
// may exist in torrent/ but can never enter payload/ or the ZIP.
func stageSelected(jobDir string, files []*torrent.File) error {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path())
	}
	return stagePaths(jobDir, paths)
}
func stagePaths(jobDir string, paths []string) error {
	if err := rejectSymlinks(jobDir); err != nil {
		return err
	}
	root, err := os.OpenRoot(jobDir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.RemoveAll("payload"); err != nil {
		return err
	}
	if err = root.Mkdir("payload", 0700); err != nil {
		return err
	}
	for _, path := range paths {
		rel, e := safeRelative(path)
		if e != nil {
			return e
		}
		src := filepath.Join("torrent", rel)
		dst := filepath.Join("payload", rel)
		fi, e := root.Lstat(src)
		if e != nil {
			return e
		}
		if !fi.Mode().IsRegular() {
			return errors.New("selected source is not a regular file")
		}
		if e = root.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return e
		}
		if e = root.Link(src, dst); e != nil {
			return e
		}
	}
	return nil
}

// Cached metadata makes recovery independent of peer availability. A cache
// with the wrong canonical hash is rejected rather than used for another job.
func loadSpec(jobDir string, j jobs.Job) (*torrent.TorrentSpec, error) {
	root, err := os.OpenRoot(jobDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open("metadata.torrent")
	if os.IsNotExist(err) {
		return torrent.TorrentSpecFromMagnetUri(j.Magnet)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	mi, err := metainfo.Load(f)
	if err != nil {
		return nil, err
	}
	if mi.HashInfoBytes().String() != j.InfoHash {
		return nil, errors.New("cached metadata hash mismatch")
	}
	return torrent.TorrentSpecFromMetaInfoErr(mi)
}
func persistMetadata(jobDir string, mi *metainfo.MetaInfo) error {
	root, err := os.OpenRoot(jobDir)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := root.OpenFile("metadata.tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	err = mi.Write(f)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = root.Rename("metadata.tmp", "metadata.torrent"); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func remainingAllocation(reserved, allocated int64) int64 {
	if allocated >= reserved {
		return 0
	}
	return reserved - allocated
}

// Count physical blocks, not logical sparse length, and deduplicate hardlinks
// shared between torrent storage and the selected upload staging tree.
func allocatedBytes(root string) (int64, error) {
	type inode struct {
		dev uint64
		ino uint64
	}
	seen := map[inode]bool{}
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink in job data")
		}
		var st unix.Stat_t
		if err = unix.Lstat(path, &st); err != nil {
			return err
		}
		key := inode{uint64(st.Dev), st.Ino}
		if seen[key] {
			return nil
		}
		seen[key] = true
		if st.Blocks < 0 || st.Blocks > (math.MaxInt64-total)/512 {
			return errors.New("allocated size overflow")
		}
		total += st.Blocks * 512
		return nil
	})
	return total, err
}

func selectedIndexes(selection []int, count int) ([]int, error) {
	if selection == nil {
		all := make([]int, count)
		for i := range all {
			all[i] = i
		}
		if count == 0 {
			return nil, errors.New("torrent has no files")
		}
		return all, nil
	}
	if len(selection) == 0 {
		return nil, errors.New("empty selection")
	}
	seen := map[int]bool{}
	result := cloneSelection(selection)
	for _, i := range result {
		if i < 0 || i >= count || seen[i] {
			return nil, errors.New("invalid file selection")
		}
		seen[i] = true
	}
	return result, nil
}

func selectedProgress(t *torrent.Torrent, files []*torrent.File) (completed int64, done bool) {
	done = true
	for _, f := range files {
		completed += f.BytesCompleted()
		for p := f.BeginPieceIndex(); p < f.EndPieceIndex(); p++ {
			s := t.PieceState(p)
			if !s.Complete || s.Checking || s.Hashing || s.QueuedForHash || s.Marking {
				done = false
			}
		}
	}
	return
}

// Reserve the full torrent, not just selected files: adjacent unselected files
// may be sparsely allocated by shared pieces. ZIP worst-case expansion and per
// file filesystem/ZIP metadata are additional, with overflow rejection.
func reservation(full, selected int64, zipped bool, count int) (int64, error) {
	if full < 0 || selected < 0 || selected > full || count < 0 {
		return 0, errors.New("invalid torrent size")
	}
	extra := int64(16 << 20)
	if int64(count) > (math.MaxInt64-extra)/8192 {
		return 0, errors.New("reservation overflow")
	}
	extra += int64(count) * 8192
	if zipped {
		if selected > math.MaxInt64-extra-selected/20 {
			return 0, errors.New("reservation overflow")
		}
		extra += selected + selected/20
	}
	if full > math.MaxInt64-extra {
		return 0, errors.New("reservation overflow")
	}
	return full + extra, nil
}

func returnPercentageCompleted(c, t int64) float64 {
	if t <= 0 {
		return 0
	}
	return math.Round(math.Max(0, math.Min(100, float64(c)*100/float64(t)))*100) / 100
}
func formatSpeed(rate float64) string {
	if rate < 1024 {
		return fmt.Sprintf("%.2f B/s", rate)
	}
	if rate < 1024*1024 {
		return fmt.Sprintf("%.2f KB/s", rate/1024)
	}
	return fmt.Sprintf("%.2f MB/s", rate/(1024*1024))
}
func etaFromRate(completed, total int64, rate float64) string {
	if total <= 0 || rate <= 0 {
		return "calculating..."
	}
	if completed >= total {
		return "complete"
	}
	return formatDuration(time.Duration(float64(total-completed)/rate) * time.Second)
}
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}
