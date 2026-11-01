package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plutack/seedrlike/internal/core/queue"
)

func TestParseSelection(t *testing.T) {
	for _, tt := range []struct {
		in    []string
		valid bool
		count int
	}{{nil, false, 0}, {[]string{"-1"}, false, 0}, {[]string{"oops"}, false, 0}, {[]string{"100001"}, false, 0}, {[]string{"0", "2", "2"}, true, 2}} {
		got, err := parseSelection(tt.in)
		if (err == nil) != tt.valid || tt.valid && len(got) != tt.count {
			t.Fatalf("selection %v: %v %v", tt.in, got, err)
		}
	}
}
func TestDownloadMutationsRequireAuthentication(t *testing.T) {
	d := NewDownloadHandler(queue.New())
	for _, handler := range []http.HandlerFunc{d.CreateNewDownload, d.UploadTorrent, d.Preview, d.StopDownload} {
		r := httptest.NewRequest("POST", "/downloads", strings.NewReader("magnet-link=test"))
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("got %d", w.Code)
		}
	}
}
func TestSameOriginMiddleware(t *testing.T) {
	h := SameOriginMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, tt := range []struct {
		method, origin, site string
		code                 int
	}{{"POST", "https://evil.example", "", 403}, {"POST", "https://seed.example", "", 204}, {"POST", "", "cross-site", 403}, {"GET", "https://evil.example", "cross-site", 204}, {"POST", "null", "", 403}} {
		r := httptest.NewRequest(tt.method, "https://seed.example/downloads", nil)
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("Sec-Fetch-Site", tt.site)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.code {
			t.Errorf("%+v got%d", tt, w.Code)
		}
	}
}
