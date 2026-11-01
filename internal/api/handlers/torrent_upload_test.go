package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plutack/seedrlike/internal/core/queue"
)

func TestTorrentUploadRejectsInvalidFiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{{"wrong.txt", []byte("bad")}, {"bad.torrent", []byte("not bencode")}, {"large.torrent", make([]byte, queue.MaxTorrentFileSize+1)}} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("torrent-file", tc.name)
			if err != nil {
				t.Fatal(err)
			}
			part.Write(tc.data)
			writer.Close()
			r := httptest.NewRequest("POST", "/downloads/torrent", &body)
			r.Header.Set("Content-Type", writer.FormDataContentType())
			r = r.WithContext(context.WithValue(r.Context(), UserIDKey, "owner"))
			w := httptest.NewRecorder()
			NewDownloadHandler(queue.New()).UploadTorrent(w, r)
			if !strings.Contains(w.Body.String(), "Could not add download") || w.Header().Get("HX-Trigger") != "" {
				t.Fatalf("invalid upload accepted: %s", w.Body.String())
			}
		})
	}
}
func TestTorrentUploadRequiresMultipartFile(t *testing.T) {
	r := httptest.NewRequest("POST", "/downloads/torrent", strings.NewReader("torrent-file=nope"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = r.WithContext(context.WithValue(r.Context(), UserIDKey, "owner"))
	w := httptest.NewRecorder()
	NewDownloadHandler(queue.New()).UploadTorrent(w, r)
	if !strings.Contains(w.Body.String(), "Choose a .torrent file") {
		t.Fatal(w.Body.String())
	}
}
