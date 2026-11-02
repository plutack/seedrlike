package components

import (
	"bytes"
	"context"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
	"strings"
	"testing"
)

func TestFolderSearchResultNavigatesWithoutGlobalQuery(t *testing.T) {
	var out bytes.Buffer
	options := LibraryOptions{Query: "movie", Sort: "size", Order: "asc"}
	if err := DownloadListWithOptions(false, []database.GetFolderContentsRow{{ID: "child", Name: "Movie", Type: "folder"}}, "root", true, options).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	if strings.Contains(html, `/contents/child?order=asc&amp;q=movie`) {
		t.Fatal("folder navigation retained global search")
	}
	if !strings.Contains(html, `href="/contents/child?order=asc&amp;sort=size"`) {
		t.Fatal("folder navigation lost sort or did not clear query")
	}
	if !strings.Contains(html, `/contents/root?order=asc&amp;q=movie&amp;sort=size`) {
		t.Fatal("refresh lost search context")
	}
}
func TestBulkDeleteUsesInAppDialog(t *testing.T) {
	var out bytes.Buffer
	err := DownloadList(false, []database.GetFolderContentsRow{{ID: "one", Name: "Example", Type: "file"}}, "folder", true).Render(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, native := range []string{"confirm(", "alert(", "prompt(", "hx-confirm="} {
		if strings.Contains(html, native) {
			t.Fatalf("native dialog remains: %s", native)
		}
	}
	for _, want := range []string{"<dialog", `aria-labelledby="delete-selected-title"`, "showModal()", "could not be deleted", "response.ok", "x-on:htmx:after-request.camel", "x-text=\"mutationError\"", "Could not remove library records"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing deletion dialog behavior %s", want)
		}
	}
}
