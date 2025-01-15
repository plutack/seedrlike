package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/plutack/seedrlike/internal/api/response"
)

type downloadRequest struct {
	MagnetLink string `json:"magnet_link"`
}

func sendResponse(w http.ResponseWriter, msg fmt.Stringer) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
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
	// call start downlaod function here

	var resp response.DownloadResponse

	sendResponse(w, resp)
}

func getDownloadsHandler(w http.ResponseWriter, r *http.Request) {
	// logic to get all files in the server

	var resp response.GetDownloadsResponse

	sendResponse(w, resp)
}
