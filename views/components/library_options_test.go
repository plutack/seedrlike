package components

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestLibraryOptions(t *testing.T) {
	for _, tc := range []struct{ raw, sort, order, query string }{
		{"", "date", "desc", ""},
		{"q=+movie+&sort=name&order=asc", "name", "asc", "movie"},
		{"sort=DROP+TABLE&order=evil", "date", "desc", ""},
		{"sort=size&order=asc", "size", "asc", ""},
	} {
		v, _ := url.ParseQuery(tc.raw)
		got, err := ParseLibraryOptions(v)
		if err != nil || got.Sort != tc.sort || got.Order != tc.order || got.Query != tc.query {
			t.Fatalf("%q: %+v %v", tc.raw, got, err)
		}
	}
	if _, err := ParseLibraryOptions(url.Values{"q": {strings.Repeat("界", 201)}}); err == nil {
		t.Fatal("accepted oversized search")
	}
	if _, err := ParseLibraryOptions(url.Values{"q": {strings.Repeat("界", 200)}}); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryOptionsPreserved(t *testing.T) {
	options := LibraryOptions{Query: "A & B", Sort: "size", Order: "asc"}
	for _, raw := range []string{options.FolderURL("folder"), options.DeleteURL("file", "file", "folder"), options.StaleDeleteURL("folder")} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ParseLibraryOptions(u.Query())
		if err != nil || got != options {
			t.Fatalf("lost options: %s %+v", raw, got)
		}
	}
	ctx := WithLibraryOptions(context.Background(), options)
	if LibraryOptionsFromContext(ctx) != options {
		t.Fatal("lost context options")
	}
	if LibraryOptionsFromContext(context.Background()).Sort != "date" {
		t.Fatal("missing default")
	}
	u, _ := url.Parse(options.SortURL("folder", "size"))
	if u.Query().Get("order") != "desc" || u.Query().Get("q") != options.Query {
		t.Fatal("sort lost search or direction")
	}
}
