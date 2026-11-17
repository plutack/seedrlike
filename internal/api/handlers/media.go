package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/mux"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
	"github.com/plutack/seedrlike/internal/media"
)

// MediaHandler serves metadata and authenticated-by-visibility streaming. Client
// may be replaced before serving requests for hermetic tests. Its cookie jar and
// redirect policy are never used; only its transport and timeout are retained.
type MediaHandler struct {
	queries *database.Queries
	Client  *http.Client
}

func NewMediaHandler(q *database.Queries) *MediaHandler {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = 15 * time.Second
	transport.IdleConnTimeout = 90 * time.Second
	transport.DisableCompression = true
	return &MediaHandler{queries: q, Client: &http.Client{Transport: transport, Timeout: 6 * time.Hour}}
}

type mediaMetadata struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	MIME        string `json:"mime"`
	StreamURL   string `json:"streamURL"`
	DownloadURL string `json:"downloadURL"`
}

func (h *MediaHandler) resolve(w http.ResponseWriter, r *http.Request) (mediaMetadata, bool) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	id := mux.Vars(r)["ID"]
	if id == "" {
		http.NotFound(w, r)
		return mediaMetadata{}, false
	}
	file, err := h.queries.GetFileByID(r.Context(), id)
	userID, _ := r.Context().Value(UserIDKey).(string)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && file.UserID.Valid && (userID == "" || file.UserID.String != userID)) {
		http.NotFound(w, r)
		return mediaMetadata{}, false
	}
	if err != nil {
		http.Error(w, "Failed to resolve media", http.StatusInternalServerError)
		return mediaMetadata{}, false
	}
	kind, supported := media.Detect(file.Name)
	if !supported {
		http.Error(w, "Unsupported media container", http.StatusUnsupportedMediaType)
		return mediaMetadata{}, false
	}
	download, err := media.DownloadURL(file.Server, file.ID, file.Name)
	if err != nil {
		http.Error(w, "Invalid media download location", http.StatusBadGateway)
		return mediaMetadata{}, false
	}
	return mediaMetadata{Name: file.Name, Kind: kind.Kind, MIME: kind.MIME, StreamURL: "/media/" + url.PathEscape(file.ID) + "/stream", DownloadURL: download}, true
}

// Metadata returns the same-origin stream URL and a validated direct download URL.
func (h *MediaHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	metadata, ok := h.resolve(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(metadata)
}

// Stream forwards only range validators, never caller credentials. Response
// bodies are copied incrementally with bounded memory and cancellation on idle.
func (h *MediaHandler) Stream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	metadata, ok := h.resolve(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Hour)
	defer cancel()
	upstream, err := http.NewRequestWithContext(ctx, r.Method, metadata.DownloadURL, nil)
	if err != nil {
		http.Error(w, "Invalid media request", http.StatusBadGateway)
		return
	}
	for _, key := range []string{"Range", "If-Range"} {
		if value := r.Header.Get(key); value != "" {
			upstream.Header.Set(key, value)
		}
	}
	upstream.Header.Set("Accept-Encoding", "identity")
	client := http.Client{Timeout: 6 * time.Hour}
	if h.Client != nil {
		client.Transport = h.Client.Transport
		if h.Client.Timeout > 0 && h.Client.Timeout < client.Timeout {
			client.Timeout = h.Client.Timeout
		}
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !media.SafeDownloadURL(req.URL) {
			return errors.New("unsafe media redirect")
		}
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")
		req.Header.Del("Proxy-Authorization")
		req.Header.Del("Referer")
		return nil
	}
	response, err := client.Do(upstream)
	if err != nil {
		http.Error(w, "Media upstream unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK, http.StatusPartialContent, http.StatusRequestedRangeNotSatisfiable:
	default:
		http.Error(w, "Media upstream returned an error", http.StatusBadGateway)
		return
	}
	for _, key := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if value := response.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	if response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		// Preserve the unsatisfied range, not a remote HTML/error body or its length.
		w.Header().Set("Content-Length", "0")
	}
	// A multipart range response needs its boundary; never trust arbitrary upstream
	// MIME types (HTML/error documents must not become active same-origin content).
	contentType := metadata.MIME
	if parsed, params, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err == nil && parsed == "multipart/byteranges" && response.StatusCode == http.StatusPartialContent {
		contentType = mime.FormatMediaType(parsed, params)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": metadata.Name}))
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(response.StatusCode)
	if r.Method == http.MethodHead || response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return
	}
	idle := time.AfterFunc(60*time.Second, cancel)
	defer idle.Stop()
	_, _ = io.Copy(w, mediaProgressReader{Reader: response.Body, idle: idle})
}

type mediaProgressReader struct {
	io.Reader
	idle *time.Timer
}

func (r mediaProgressReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.idle.Reset(60 * time.Second)
	}
	return n, err
}
