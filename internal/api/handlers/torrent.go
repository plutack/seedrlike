package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	// "github.com/gorilla/mux"
	"github.com/plutack/seedrlike/internal/api/response"
	"github.com/plutack/seedrlike/internal/core/queue"
	"github.com/plutack/seedrlike/views/components"
	// TODO: this might be conflicting with the anacrolix/torrent package
)

type DownloadRequest struct {
	MagnetLink string `json:"magnet_link"`
	IsZipped   bool   `json:"zipped"`
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
	var err error
	if err = r.ParseForm(); err != nil {
		log.Println("something happened")
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}
	magnetLink := r.FormValue("magnet-link")
	isZipped := r.FormValue("is-zipped") == "on"
	if magnetLink == "" {
		http.Error(w, "Magnetic link is required", http.StatusBadRequest)
		return
	}

	payload := queue.DownloadRequest{
		MagnetLink: magnetLink,
		IsZipped:   isZipped,
	}

	err = d.queue.Add(payload)
	if err != nil {
		sendResponse(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	components.QueueSuccess().Render(r.Context(), w)
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
