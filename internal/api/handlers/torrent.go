package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/gorilla/mux"
	"github.com/plutack/seedrlike/internal/core/queue"
	"github.com/plutack/seedrlike/views/components"
)

type DownloadHandler struct {
	queue     *queue.DownloadQueue
	preview   *torrent.Client
	previewMu sync.Mutex
}

func NewDownloadHandler(q *queue.DownloadQueue) *DownloadHandler { return &DownloadHandler{queue: q} }
func (d *DownloadHandler) SetPreviewClient(c *torrent.Client)    { d.preview = c }
func sendResponse(w http.ResponseWriter, code int, msg interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(msg)
}
func requireDownloadUser(w http.ResponseWriter, r *http.Request) string {
	u, _ := r.Context().Value(UserIDKey).(string)
	if u == "" {
		http.Error(w, "Sign in to add or manage downloads.", http.StatusUnauthorized)
	}
	return u
}
func publicQueueError(err error) string {
	switch err.Error() {
	case "invalid torrent file", "v2-only torrent files are not supported", "invalid magnet link", "select at least one file", "invalid file selection", "download queue full", "torrent already active":
		return err.Error()
	default:
		slog.Error("Queue request failed", "operation", "enqueue_download", "reason", "queue_unavailable", "error_type", fmt.Sprintf("%T", err))
		return "The download queue is unavailable. Please try again shortly."
	}
}
func parseSelection(values []string) ([]int, error) {
	if len(values) == 0 {
		return nil, errors.New("select at least one file")
	}
	if len(values) > 10000 {
		return nil, errors.New("too many files")
	}
	out := make([]int, 0, len(values))
	seen := map[int]bool{}
	for _, v := range values {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 100000 {
			return nil, errors.New("invalid file selection")
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}
func (d *DownloadHandler) CreateNewDownload(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(UserIDKey).(string)
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", 400)
		return
	}
	var selected []int
	var err error
	if r.FormValue("selection") == "explicit" {
		selected, err = parseSelection(r.Form["files"])
		if err != nil {
			components.DownloadFeedback(err.Error(), true).Render(r.Context(), w)
			return
		}
	}
	err = d.queue.Add(queue.DownloadRequest{MagnetLink: strings.TrimSpace(r.FormValue("magnet-link")), IsZipped: r.FormValue("is-zipped") == "on", UserID: &userID, Selected: selected})
	if err != nil {
		components.DownloadFeedback(publicQueueError(err), true).Render(r.Context(), w)
		return
	}
	w.Header().Set("HX-Retarget", "#download-response")
	w.Header().Set("HX-Trigger", "download-queued")
	components.DownloadFeedback("Added to queue.", false).Render(r.Context(), w)
}
func (d *DownloadHandler) StopDownload(w http.ResponseWriter, r *http.Request) {
	userID := requireDownloadUser(w, r)
	if userID == "" {
		return
	}
	if err := d.queue.StopOwned(mux.Vars(r)["ID"], userID); err != nil {
		sendResponse(w, http.StatusConflict, "Download unavailable or cannot be stopped at this stage.")
		return
	}
	sendResponse(w, 200, "Download cancelled")
}

// UploadTorrent accepts one bounded multipart file; filename and MIME type are
// only UI hints. The bencoded content is validated again at the queue boundary.
func (d *DownloadHandler) UploadTorrent(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(UserIDKey).(string)
	fail := func(message string) { components.DownloadFeedback(message, true).Render(r.Context(), w) }
	r.Body = http.MaxBytesReader(w, r.Body, queue.MaxTorrentFileSize+(64<<10))
	if err := r.ParseMultipartForm(queue.MaxTorrentFileSize + (64 << 10)); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		fail("Choose a .torrent file up to 4 MB.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File["torrent-file"]) != 1 {
		fail("Choose one .torrent file.")
		return
	}
	file, header, err := r.FormFile("torrent-file")
	if err != nil {
		fail("Choose a .torrent file.")
		return
	}
	defer file.Close()
	if !strings.EqualFold(filepath.Ext(header.Filename), ".torrent") || header.Size > queue.MaxTorrentFileSize {
		fail("Choose a .torrent file up to 4 MB.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, queue.MaxTorrentFileSize+1))
	if err != nil || len(data) > queue.MaxTorrentFileSize {
		fail("Could not read the torrent file (4 MB maximum).")
		return
	}
	mi, _, err := queue.ParseTorrentFile(data)
	if err != nil {
		fail(publicQueueError(err))
		return
	}
	info, _ := mi.UnmarshalInfo()
	files := info.UpvertedFiles()
	if r.FormValue("action") == "preview" {
		items := make([]components.SelectableFile, 0, len(files))
		for i, f := range files {
			items = append(items, components.SelectableFile{Index: i, Name: f.DisplayPath(&info), Size: f.Length})
		}
		components.UploadedFileSelection(info.BestName(), r.FormValue("is-zipped") == "on", items).Render(r.Context(), w)
		return
	}
	var selected []int
	if r.FormValue("selection") == "explicit" {
		selected, err = parseSelection(r.MultipartForm.Value["files"])
		if err != nil {
			fail(err.Error())
			return
		}
		for _, index := range selected {
			if index >= len(files) {
				fail("invalid file selection")
				return
			}
		}
	}
	err = d.queue.Add(queue.DownloadRequest{TorrentData: data, IsZipped: r.FormValue("is-zipped") == "on", UserID: &userID, Selected: selected})
	if err != nil {
		fail(publicQueueError(err))
		return
	}
	w.Header().Set("HX-Retarget", "#torrent-upload-response")
	w.Header().Set("HX-Trigger", "torrent-uploaded, download-queued")
	components.DownloadFeedback("Added to queue.", false).Render(r.Context(), w)
}

func (d *DownloadHandler) Preview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid magnet", 400)
		return
	}
	magnet := strings.TrimSpace(r.FormValue("magnet-link"))
	spec, err := torrent.TorrentSpecFromMagnetUri(magnet)
	if err != nil || len(magnet) > 8192 {
		components.DownloadFeedback("Enter a valid magnet link.", true).Render(r.Context(), w)
		return
	}
	if d.preview == nil {
		http.Error(w, "Metadata preview unavailable", 503)
		return
	}
	// Limit metadata discovery concurrency and keep it separate from active torrent handles.
	if !d.previewMu.TryLock() {
		components.DownloadFeedback("Another file preview is loading. Try again shortly.", true).Render(r.Context(), w)
		return
	}
	defer d.previewMu.Unlock()
	spec.DisallowDataDownload = true
	spec.DisallowDataUpload = true
	t, _, err := d.preview.AddTorrentSpec(spec)
	if err != nil {
		components.DownloadFeedback("Could not load torrent metadata.", true).Render(r.Context(), w)
		return
	}
	defer t.Drop()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		components.DownloadFeedback("Metadata timed out. Check that this torrent has available peers.", true).Render(r.Context(), w)
		return
	case <-t.GotInfo():
	}
	files := t.Files()
	if len(files) > 10000 {
		components.DownloadFeedback("This torrent has too many files to preview.", true).Render(r.Context(), w)
		return
	}
	items := make([]components.SelectableFile, 0, len(files))
	for i, f := range files {
		items = append(items, components.SelectableFile{Index: i, Name: f.DisplayPath(), Size: f.Length()})
	}
	components.FileSelection(magnet, t.Name(), r.FormValue("is-zipped") == "on", items).Render(r.Context(), w)
}
