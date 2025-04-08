package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	// "github.com/gorilla/mux"
	"github.com/gorilla/mux"
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

func (d *DownloadHandler) StopDownload(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	torrentID := vars["ID"]

	var resp response.StopDownloadResponse
	if torrentID == "" {
		log.Println("Stop request failed: Missing torrent ID in URL")
		sendResponse(w, http.StatusBadRequest, "Missing torrent ID in URL path")
		return
	} // TODO: logic to terminate a running download
	log.Printf("Received stop request for torrent ID: %s", torrentID)

	err := d.queue.Stop(torrentID)

	if err != nil {
		log.Printf("Error processing stop request for torrent %s: %v", torrentID, err)
		errMsg := err.Error()
		if errMsg == fmt.Sprintf("torrent with ID %s not found", torrentID) {
			sendResponse(w, http.StatusNotFound, errMsg)
		} else if errMsg == fmt.Sprintf("torrent %s already stopping/stopped", torrentID) || errMsg == fmt.Sprintf("torrent %s already completed/failed", torrentID) {
			sendResponse(w, http.StatusConflict, errMsg)
		} else {
			sendResponse(w, http.StatusInternalServerError, "Failed to stop download torrent")
		}
		return
	}
	log.Printf("Successfully processed stop request for torrent ID: %s", torrentID)
	sendResponse(w, http.StatusOK, resp)
}
