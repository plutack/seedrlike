package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plutack/seedrlike/internal/core/search"
)

func TestSharedInputSearchRendersServiceResults(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("q") != "ubuntu" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[{"name":"Ubuntu test result","magnet":"magnet:?xt=urn:btih:0123456789012345678901234567890123456789","source":"tpb","seeders":12}]}`))
	}))
	defer server.Close()
	handler := NewSearchHandler(search.New(server.URL, "", ""))
	w := httptest.NewRecorder()
	handler.Search(w, httptest.NewRequest("GET", "/search?magnet-link=ubuntu", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Ubuntu test result") {
		t.Fatalf("results missing: %d %s", w.Code, w.Body.String())
	}
	for _, query := range []string{"ab", "magnet:%3Fxt=urn:btih:test"} {
		handler.Search(httptest.NewRecorder(), httptest.NewRequest("GET", "/search?magnet-link="+query, nil))
	}
	if calls != 1 {
		t.Fatalf("non-search input reached service: %d", calls)
	}
}
