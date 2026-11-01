package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	case "invalid magnet link", "select at least one file", "invalid file selection", "download queue full", "torrent already active":
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
	userID := requireDownloadUser(w, r)
	if userID == "" {
		return
	}
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
	components.DownloadFeedback("Download added to the queue.", false).Render(r.Context(), w)
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
func (d *DownloadHandler) Batch(w http.ResponseWriter, r *http.Request) {
	userID := requireDownloadUser(w, r)
	if userID == "" {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid batch", 400)
		return
	}
	lines := strings.Split(strings.ReplaceAll(r.FormValue("magnets"), "\r\n", "\n"), "\n")
	if len(lines) > 50 {
		components.DownloadFeedback("Import up to 50 links at a time.", true).Render(r.Context(), w)
		return
	}
	var results []components.BatchResult
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		err := d.queue.Add(queue.DownloadRequest{MagnetLink: line, IsZipped: r.FormValue("is-zipped") == "on", UserID: &userID})
		message := "Added to queue"
		if err != nil {
			message = publicQueueError(err)
		}
		results = append(results, components.BatchResult{Line: i + 1, Message: message, Failed: err != nil})
	}
	components.BatchFeedback(results).Render(r.Context(), w)
}
func (d *DownloadHandler) Preview(w http.ResponseWriter, r *http.Request) {
	if requireDownloadUser(w, r) == "" {
		return
	}
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
