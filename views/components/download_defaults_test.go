package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/plutack/seedrlike/internal/core/search"
)

func TestDownloadAllIsDefaultUIAction(t *testing.T) {
	var out bytes.Buffer
	if err := Download().Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	if !strings.Contains(html, `action="/downloads" hx-post="/downloads"`) || !strings.Contains(html, `>Download magnet</button>`) {
		t.Fatal("magnet form must submit whole torrent by default")
	}
	if !strings.Contains(html, `hx-post="/downloads/preview"`) {
		t.Fatal("optional selection missing")
	}
	out.Reset()
	if err := SearchResults([]search.Result{{Name: "Example", Magnet: "magnet:?xt=test"}}, "", false).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html = out.String()
	all := strings.Index(html, `hx-post="/downloads"`)
	preview := strings.Index(html, `hx-post="/downloads/preview"`)
	if all < 0 || preview < all || !strings.Contains(html, "Download all") {
		t.Fatal("whole torrent must be primary search-result action")
	}
}
