package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/plutack/seedrlike/internal/auth"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
	"github.com/plutack/seedrlike/views/components"
)

const AdminKey contextKey = "isAdmin"

// Browser mutations must originate from this application's origin. SameSite
// cookies provide an additional boundary; neither relies on hidden UI controls.
func SameOriginMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "Cross-site request rejected", 403)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				if !requestOriginAllowed(r, origin) {
					slog.Warn("request_origin_rejected", "reason", "origin_mismatch")
					http.Error(w, "Cross-site request rejected", 403)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// originURL accepts serialized origins only, never URLs with credentials/paths.
func originURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || (u.Scheme != "https" && u.Scheme != "http") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, false
	}
	return u, true
}
func sameOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}
func requestOriginAllowed(r *http.Request, raw string) bool {
	origin, ok := originURL(raw)
	if !ok {
		return false
	}
	// Trust the administrator-configured public URL, not attacker-supplied
	// Forwarded/X-Forwarded-Host headers. Proxies may rewrite r.Host internally.
	if configured := os.Getenv("APP_BASE_URL"); configured != "" {
		public, valid := originURL(configured)
		return valid && sameOrigin(origin, public)
	}
	// Compatibility for installations without a configured public origin.
	// Preserve host-based checks (including default-port normalization).
	direct, valid := originURL(origin.Scheme + "://" + r.Host)
	return valid && sameOrigin(origin, direct)
}
func AdminContextMiddleware(q *database.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := r.Context().Value(UserIDKey).(string)
			if id != "" {
				if u, err := q.GetUserByID(r.Context(), id); err == nil {
					r = r.WithContext(components.WithAdmin(context.WithValue(r.Context(), AdminKey, auth.IsAdmin(u.Username)), auth.IsAdmin(u.Username)))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
