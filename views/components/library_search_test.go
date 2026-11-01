package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestLibrarySearchIsDebouncedAndSortIsImmediate(t *testing.T) {
	var out bytes.Buffer
	if err := DownloadListWithOptions(false, nil, "folder", true, LibraryOptions{}).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{"input changed delay:500ms from:#library-query", "change[target.tagName", `hx-sync="this:replace"`, `hx-indicator="#library-search-indicator"`, `id="library-query"`, `type="search"`, `name="q"`, `name="sort"`, `name="order"`, `hx-push-url="true"`, `role="search"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing live search behavior %s", want)
		}
	}
	if strings.Contains(html, ">Apply</button>") {
		t.Fatal("obsolete Apply action remains")
	}
}
