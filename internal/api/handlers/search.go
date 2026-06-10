package handlers

import (
	"net/http"
	"strings"

	"github.com/plutack/seedrlike/internal/core/search"
	"github.com/plutack/seedrlike/views/components"
)

// SearchHandler serves live torrent-search results by querying the external
// torrengo service and rendering them as an HTML fragment for HTMX.
type SearchHandler struct {
	client *search.Client
}

func NewSearchHandler(c *search.Client) *SearchHandler {
	return &SearchHandler{client: c}
}

// Search handles GET /search. The query arrives either as ?q= or, because the
// shared search input is named "magnet-link" for the magnet POST form, as
// ?magnet-link=. Magnet links and short queries render nothing.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		q = r.URL.Query().Get("magnet-link")
	}
	q = strings.TrimSpace(q)

	if len(q) < 3 || search.IsMagnet(q) {
		// Nothing to show: clear the results area.
		return
	}

	out, err := h.client.Search(r.Context(), q)
	if err != nil {
		components.SearchResults(nil, "", true).Render(r.Context(), w)
		return
	}
	components.SearchResults(out.Results, out.MatchedQuery, false).Render(r.Context(), w)
}
