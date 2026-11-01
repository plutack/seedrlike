package queue

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func torrentFixture(t *testing.T, mutate func(*metainfo.Info)) []byte {
	t.Helper()
	private := true
	info := metainfo.Info{Name: "example.txt", Length: 1, PieceLength: 16384, Pieces: make([]byte, 20), Private: &private}
	if mutate != nil {
		mutate(&info)
	}
	raw, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: raw, Announce: "https://tracker.example/announce", AnnounceList: [][]string{{"https://tracker.example/announce"}}}
	var buf bytes.Buffer
	if err = mi.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func TestTorrentUploadPersistsOriginalMetadataAndRecovers(t *testing.T) {
	s := &fakeStore{}
	q := configured(t, s)
	data := torrentFixture(t, nil)
	owner := "owner"
	if err := q.Add(DownloadRequest{TorrentData: data, UserID: &owner, IsZipped: true}); err != nil {
		t.Fatal(err)
	}
	j := s.created[0]
	if j.UserID != owner || !j.Zipped || j.Name != "example.txt" {
		t.Fatalf("bad job: %+v", j)
	}
	dir := filepath.Join(q.dataDir, j.ID)
	spec, err := loadSpec(dir, j)
	if err != nil || len(spec.InfoBytes) == 0 {
		t.Fatalf("lost metadata: %v", err)
	}
	mi, err := metainfo.LoadFromFile(filepath.Join(dir, "uploaded.torrent"))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := mi.UnmarshalInfo()
	if info.Private == nil || !*info.Private || mi.Announce != "https://tracker.example/announce" {
		t.Fatal("original private metadata lost")
	}
	if err = q.Add(DownloadRequest{MagnetLink: j.Magnet}); err == nil {
		t.Fatal("magnet duplicate accepted")
	}
	recovered := New()
	s.active = s.created
	if err = recovered.configure(s, filepath.Dir(q.dataDir), nil); err != nil {
		t.Fatal(err)
	}
	if len(recovered.Snapshot().Tasks) != 1 {
		t.Fatal("upload not recovered")
	}
}
func TestTorrentMetadataCleanupPreservesPayloadAndAllowsMagnetRecovery(t *testing.T) {
	s := &fakeStore{}
	q := configured(t, s)
	if err := q.Add(DownloadRequest{TorrentData: torrentFixture(t, nil)}); err != nil {
		t.Fatal(err)
	}
	j := s.created[0]
	dir := filepath.Join(q.dataDir, j.ID)
	for _, name := range []string{"metadata.torrent", "metadata.tmp", "payload.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep payload"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeTorrentMetadata(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"uploaded.torrent", "metadata.torrent", "metadata.tmp"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("metadata remains: %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "payload.txt")); err != nil {
		t.Fatal("payload removed", err)
	}
	spec, err := loadSpec(dir, j)
	if err != nil || len(spec.InfoBytes) != 0 {
		t.Fatalf("expected magnet fallback: %v", err)
	}
	if err := removeTorrentMetadata(dir); err != nil {
		t.Fatal("cleanup must be idempotent", err)
	}
}

func TestTorrentFileValidation(t *testing.T) {
	cases := [][]byte{nil, []byte("not bencode"), make([]byte, MaxTorrentFileSize+1), torrentFixture(t, func(i *metainfo.Info) { i.Name = "../escape" }), torrentFixture(t, func(i *metainfo.Info) { i.PieceLength = 0 }), torrentFixture(t, func(i *metainfo.Info) { i.Pieces = nil }), torrentFixture(t, func(i *metainfo.Info) { i.Length = -1 }), torrentFixture(t, func(i *metainfo.Info) {
		i.Length = 0
		i.Files = []metainfo.FileInfo{{Length: 1, Path: []string{"..", "escape"}}}
	})}
	for n, data := range cases {
		if _, _, err := ParseTorrentFile(data); err == nil {
			t.Errorf("invalid case %d accepted", n)
		}
	}
}
func TestTorrentMetadataRetainedOnAmbiguousJobCommit(t *testing.T) {
	s := &fakeStore{createErr: errors.New("commit response lost")}
	q := configured(t, s)
	if err := q.Add(DownloadRequest{TorrentData: torrentFixture(t, nil)}); err == nil {
		t.Fatal("expected error")
	}
	entries, err := os.ReadDir(q.dataDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("metadata must survive ambiguous commit: %v", err)
	}
	if len(q.Snapshot().Tasks) != 0 {
		t.Fatal("failed submission exposed in memory")
	}
}
