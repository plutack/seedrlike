package handlers

import (
	"encoding/json"
	"net/http"

	// "github.com/gorilla/mux"
	"github.com/plutack/seedrlike/internal/api/response"
	"github.com/plutack/seedrlike/internal/core/queue"
	// TODO: this might be conflicting with the anacrolix/torrent package
)

var downloadQueue = queue.New()

type downloadRequest struct {
	MagnetLink string `json:"magnet_link"`
}

type DownloadHandler struct {
	queue *queue.DownloadQueue
}

func NewDownloadHandler(q *queue.DownloadQueue) *DownloadHandler {
	return &DownloadHandler{
		queue: q,
	}
}

func sendResponse(w http.ResponseWriter, code int, msg interface{}) { // consider changing message type to an empty interface
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(msg); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func (d *DownloadHandler) CreateNewDownload(w http.ResponseWriter, r *http.Request) {
	//FIXME: right now 2nd requests hangs due to channel implementation.
	// first download needs to complete before 2nd request can be accepted which is not what I wwant
	var req downloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}
	if req.MagnetLink == "" {
		http.Error(w, "Magnetic link is required ", http.StatusBadRequest)
		return
	}

	d.queue.Add(req.MagnetLink)
	resp := "New downloaded added to queue"
	sendResponse(w, http.StatusOK, resp)
}

func GetDownloadsHandler(w http.ResponseWriter, _ *http.Request) {
	// TODO: logic to get all files in the server

	var resp response.GetDownloadsResponse

	sendResponse(w, http.StatusOK, resp)
}

// func StopDownloadTaskHandler(w http.ResponseWriter, r *http.Request) {
// 	vars := mux.Vars(r)
// 	torrentID := vars["torrentID"]

// 	// TODO: logic to terminate a running download
// 	var resp response.StopDownloadTaskResponse

// 	sendResponse(w, http.StatusOK, resp)
// }
