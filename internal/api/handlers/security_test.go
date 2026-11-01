package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMutationOriginBehindProxy(t *testing.T) {
	for _, tc := range []struct {
		name, base, host, origin, site, forwarded string
		want                                      int
	}{
		{"rewritten proxy host", "https://seed.example", "seed-container:3000", "https://seed.example", "same-origin", "", 204},
		{"default https port", "https://seed.example", "internal:3000", "https://SEED.example:443", "same-origin", "", 204},
		{"foreign origin", "https://seed.example", "internal:3000", "https://evil.example", "", "seed.example", 403},
		{"scheme mismatch", "https://seed.example", "seed.example", "http://seed.example", "", "", 403},
		{"port mismatch", "https://seed.example", "internal:3000", "https://seed.example:444", "", "", 403},
		{"credentials rejected", "https://seed.example", "seed.example", "https://evil@seed.example", "", "", 403},
		{"path rejected", "https://seed.example", "seed.example", "https://seed.example/evil", "", "", 403},
		{"null rejected", "https://seed.example", "seed.example", "null", "", "", 403},
		{"fetch cross site rejected", "https://seed.example", "internal:3000", "https://seed.example", "cross-site", "", 403},
		{"bad configured origin fails closed", "https://seed.example/path", "seed.example", "https://seed.example", "", "", 403},
		{"legacy direct host", "", "localhost:3000", "http://localhost:3000", "same-origin", "", 204},
		{"legacy foreign host", "", "localhost:3000", "https://evil.example", "", "localhost:3000", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_BASE_URL", tc.base)
			r := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/settings", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("X-Forwarded-Host", tc.forwarded)
			w := httptest.NewRecorder()
			SameOriginMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
		})
	}
}
