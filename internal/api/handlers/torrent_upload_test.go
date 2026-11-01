package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/plutack/seedrlike/internal/core/queue"
)

func TestUploadedTorrentNestedPreviewAndInvalidSelection(t *testing.T) {
	info := metainfo.Info{Name: "bundle", PieceLength: 16384, Pieces: make([]byte, 20), Files: []metainfo.FileInfo{{Length: 3, Path: []string{"folder", "deep", "wanted.txt"}}, {Length: 5, Path: []string{"folder", "deep", "skip.txt"}}}}
	raw, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: raw}
	var data bytes.Buffer
	mi.Write(&data)
	for _, tc := range []struct{ action, selection, index, want string }{{"preview", "", "", "folder/deep/wanted.txt"}, {"", "explicit", "9", "invalid file selection"}, {"", "explicit", "", "select at least one file"}} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("torrent-file", "nested.torrent")
		part.Write(data.Bytes())
		writer.WriteField("action", tc.action)
		writer.WriteField("selection", tc.selection)
		if tc.index != "" {
			writer.WriteField("files", tc.index)
		}
		writer.Close()
		r := httptest.NewRequest("POST", "/downloads/torrent", &body)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		r = r.WithContext(context.WithValue(r.Context(), UserIDKey, "owner"))
		w := httptest.NewRecorder()
		NewDownloadHandler(queue.New()).UploadTorrent(w, r)
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Fatalf("missing %q: %s", tc.want, w.Body.String())
		}
		if tc.action == "preview" {
			for _, want := range []string{`id="torrent-selection-file"`, `name="files" value="1"`, `hx-encoding="multipart/form-data"`, `name="selection" value="explicit"`} {
				if !strings.Contains(w.Body.String(), want) {
					t.Errorf("missing %s", want)
				}
			}
		}
		if w.Header().Get("HX-Trigger") != "" {
			t.Fatal("preview or invalid selection enqueued")
		}
	}
}

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
