// Package search is seedrlike's client for the external torrengo search
// microservice. seedrlike never scrapes torrent sites itself; it asks the
// torrengo service for magnets and renders them.
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result mirrors one hit from the torrengo service's JSON response. seedrlike
// only needs Name + Magnet to enqueue a download; Seeders/Leechers/Size/Source
// drive the UI.
type Result struct {
	Name      string `json:"name"`
	Magnet    string `json:"magnet"`
	Size      string `json:"size"`
	SizeBytes int64  `json:"size_bytes"`
	Seeders   int    `json:"seeders"`
	Leechers  int    `json:"leechers"`
	Source    string `json:"source"`
}

// response is the torrengo /search envelope.
type response struct {
	Results      []Result `json:"results"`
	MatchedQuery string   `json:"matched_query"`
}

// SearchResponse is what Search returns: the hits plus, when the service
// broadened a half-typed query, the query it actually matched.
type SearchResponse struct {
	Results      []Result
	MatchedQuery string
}

// Client talks to the torrengo search service over HTTP.
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

// New builds a client. If baseURL is empty it defaults to localhost:8080, the
// torrengo service's default port. username/password, when set, are sent as
// HTTP Basic Auth and must match the credentials configured on the torrengo
// service.
func New(baseURL, username, password string) *Client {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		http:     &http.Client{Timeout: 25 * time.Second},
	}
}

// Search asks the torrengo service for results matching query. Results arrive
// already sorted by seeder/leecher health.
func (c *Client) Search(ctx context.Context, query string) (SearchResponse, error) {
	endpoint := c.baseURL + "/search?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("build request: %w", err)
	}
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("search service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return SearchResponse{}, fmt.Errorf("search service rejected credentials (401)")
	}
	if resp.StatusCode != http.StatusOK {
		return SearchResponse{}, fmt.Errorf("search service status %d", resp.StatusCode)
	}

	var out response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return SearchResponse{}, fmt.Errorf("decode search response: %w", err)
	}
	return SearchResponse{Results: out.Results, MatchedQuery: out.MatchedQuery}, nil
}

// IsMagnet reports whether s is a magnet link (anchored prefix, mirroring the
// front-end check) so the search handler can skip querying for magnets.
func IsMagnet(s string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "magnet:?")
}
