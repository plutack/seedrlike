package handlers

import (
	"context"
	"net/http"
	"net/url"
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
				u, err := url.Parse(origin)
				if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) || (u.Scheme != "https" && u.Scheme != "http") {
					http.Error(w, "Cross-site request rejected", 403)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
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
