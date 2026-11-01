package handlers

import (
	"context"
	"log/slog"
	"net"
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
				slog.Warn("request_origin_rejected", "reason", "fetch_site_cross_site")
				http.Error(w, "Cross-site request rejected", 403)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				if !requestOriginAllowed(r, origin) {
					// Serialized opaque origins ("Origin: null") reach the server
					// even for requests the browser itself initiated from this
					// site: hardened privacy modes, embedded previews, and
					// redirect chains can all serialize the initiator as null.
					// Fetch metadata is browser-set and unforgeable by web
					// content, so a same-origin attestation is a safe
					// substitute for the serialized origin. Null origins with
					// any other fetch site — including browsers old enough to
					// not send fetch metadata — stay rejected, preserving the
					// sandboxed-iframe CSRF defense.
					if !isNullOrigin(origin) || r.Header.Get("Sec-Fetch-Site") != "same-origin" {
						// Log all three values the policy can compare: the browser's
						// Origin, the administrator-configured public origin, and the
						// Host the proxy forwarded. Mismatches are config issues.
						slog.Warn("request_origin_rejected",
							"reason", "origin_mismatch",
							"origin", origin,
							"app_base_url", os.Getenv("APP_BASE_URL"),
							"host", r.Host)
						http.Error(w, "Cross-site request rejected", 403)
						return
					}
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
func loopbackOrigin(u *url.URL) bool {
	h := u.Hostname()
	if strings.EqualFold(h, "localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
func isNullOrigin(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), "null")
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
		if valid && sameOrigin(origin, public) {
			return true
		}
		// Local development: APP_BASE_URL must name the public origin (the
		// notifications feature requires it), so browsers running the app on
		// localhost would otherwise be locked out of every mutation. The
		// browser-controlled Origin header cannot be forged by web content and
		// a cross-site attacker page never carries a loopback origin, so a
		// loopback origin equal to the host the request actually reached is a
		// same-origin request from this machine.
		if !loopbackOrigin(origin) {
			return false
		}
		direct, valid := originURL(origin.Scheme + "://" + r.Host)
		return valid && sameOrigin(origin, direct)
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
