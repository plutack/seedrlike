package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/plutack/seedrlike/internal/api/response"
)

type downloadRequest struct {
	MagnetLink string `json:"magnet_link"`
}

func sendResponse(w http.ResponseWriter, code int, msg fmt.Stringer) { // consider changing message type to an empty interface
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(msg); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func CreateNewDownloadHandler(w http.ResponseWriter, r *http.Request) {
	var req downloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}
	if req.MagnetLink == "" {
		http.Error(w, "Magnetic link is required ", http.StatusBadRequest)
		return
	}
	// TODO: call start downlaod function here

	var resp response.DownloadResponse

	sendResponse(w, http.StatusOK, resp)
}

func GetDownloadsHandler(w http.ResponseWriter, _ *http.Request) {
	// TODO: logic to get all files in the server

	var resp response.GetDownloadsResponse

	sendResponse(w, http.StatusOK, resp)
}

func StopDownloadTaskHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	torrentID := vars["torrentID"]

	// TODO: logic to terminate a running download
	var resp response.StopDownloadTaskResponse

	sendResponse(w, http.StatusOK, resp)
}
