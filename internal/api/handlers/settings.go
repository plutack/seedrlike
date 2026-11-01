package handlers

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/plutack/seedrlike/internal/notifications"
	"github.com/plutack/seedrlike/views/components"
	"github.com/plutack/seedrlike/views/layouts"
)

type SettingsHandler struct{ service *notifications.Service }

func NewSettingsHandler(db *sql.DB) *SettingsHandler {
	return &SettingsHandler{service: notifications.New(db)}
}
func settingsOwner(w http.ResponseWriter, r *http.Request) string {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	id, _ := r.Context().Value(UserIDKey).(string)
	if id == "" {
		http.Error(w, "Sign in to manage your notification settings.", http.StatusUnauthorized)
	}
	return id
}
func settingsMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	return false
}
func settingsCSRF(w http.ResponseWriter, r *http.Request) (string, error) {
	if c, err := r.Cookie("seedrlike_settings_csrf"); err == nil && validCSRF(c.Value) {
		return c.Value, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	// Strict cookies plus a random double-submit token protect every mutation.
	http.SetCookie(w, &http.Cookie{Name: "seedrlike_settings_csrf", Value: token, Path: "/settings", HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode, MaxAge: 3600})
	return token, nil
}
func validCSRF(token string) bool {
	b, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(b) == 32
}
func settingsPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "Invalid request origin", http.StatusForbidden)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return false
	}
	c, err := r.Cookie("seedrlike_settings_csrf")
	token := r.PostForm.Get("csrf")
	if err != nil || !validCSRF(token) || subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) != 1 {
		http.Error(w, "Your form expired. Reload settings and try again.", http.StatusForbidden)
		return false
	}
	return true
}
func (h *SettingsHandler) render(w http.ResponseWriter, r *http.Request, id, message string, isError bool, status int) {
	s, err := h.service.Settings(r.Context(), id)
	if err != nil {
		slog.Error("Notification settings unavailable", "operation", "load_notification_settings", "reason", "database_query_failed", "error_type", fmt.Sprintf("%T", err))
		http.Error(w, "Unable to load notification settings", http.StatusInternalServerError)
		return
	}
	csrf, err := settingsCSRF(w, r)
	if err != nil {
		http.Error(w, "Unable to secure form", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err = layouts.SettingsPage("Email notifications", true, components.NotificationSettings(s, csrf, message, isError)).Render(r.Context(), w); err != nil {
		slog.Error("Notification settings render failed", "operation", "render_notification_settings", "reason", "render_failed", "error_type", fmt.Sprintf("%T", err))
	}
}
func (h *SettingsHandler) Settings(w http.ResponseWriter, r *http.Request) {
	if !settingsMethod(w, r, http.MethodGet) {
		return
	}
	id := settingsOwner(w, r)
	if id == "" {
		return
	}
	h.render(w, r, id, "", false, http.StatusOK)
}
func (h *SettingsHandler) Save(w http.ResponseWriter, r *http.Request) {
	if !settingsMethod(w, r, http.MethodPost) {
		return
	}
	id := settingsOwner(w, r)
	if id == "" || !settingsPost(w, r) {
		return
	}
	email := r.PostForm.Get("email")
	if err := h.service.Save(r.Context(), id, email, r.PostForm.Get("completed") == "on", r.PostForm.Get("failed") == "on"); err != nil {
		slog.Error("Notification settings save failed", "operation", "save_notification_settings", "reason", "settings_save_failed", "error_type", fmt.Sprintf("%T", err))
		h.render(w, r, id, "Unable to save. Enter a valid email address and try again.", true, http.StatusBadRequest)
		return
	}
	message := "Your notification preferences have been saved."
	if r.PostForm.Get("action") == "verify" {
		if err := h.service.RequestVerification(r.Context(), id); err != nil {
			h.deliveryError(w, r, id, err)
			return
		}
		message = "Preferences saved. Check your inbox for a verification link; it expires in 30 minutes."
	}
	h.render(w, r, id, message, false, http.StatusOK)
}
func (h *SettingsHandler) Verify(w http.ResponseWriter, r *http.Request) {
	if !settingsMethod(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	id := settingsOwner(w, r)
	if id == "" {
		return
	}
	if r.Method == http.MethodGet {
		// Deliberately no service call or token consumption: email scanners cannot verify.
		token := r.URL.Query().Get("token")
		if len(token) > 128 || token == "" {
			http.Error(w, "Missing or invalid verification token", http.StatusBadRequest)
			return
		}
		csrf, err := settingsCSRF(w, r)
		if err != nil {
			http.Error(w, "Unable to secure form", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err = layouts.SettingsPage("Confirm email", true, components.ConfirmNotificationEmail(token, csrf)).Render(r.Context(), w); err != nil {
			slog.Error("Verification page render failed", "operation", "render_email_verification", "reason", "render_failed", "error_type", fmt.Sprintf("%T", err))
		}
		return
	}
	if !settingsPost(w, r) {
		return
	}
	if err := h.service.Verify(r.Context(), id, r.PostForm.Get("token")); err != nil {
		if errors.Is(err, notifications.ErrInvalidToken) {
			h.render(w, r, id, err.Error(), true, http.StatusBadRequest)
		} else {
			slog.Error("Email verification failed", "operation", "verify_email", "reason", "verification_failed", "error_type", fmt.Sprintf("%T", err))
			h.render(w, r, id, "Unable to verify your email. Please try again.", true, http.StatusInternalServerError)
		}
		return
	}
	h.render(w, r, id, "Email verified. Your selected download updates are now enabled.", false, http.StatusOK)
}
func (h *SettingsHandler) Test(w http.ResponseWriter, r *http.Request) {
	if !settingsMethod(w, r, http.MethodPost) {
		return
	}
	id := settingsOwner(w, r)
	if id == "" || !settingsPost(w, r) {
		return
	}
	if err := h.service.Test(r.Context(), id); err != nil {
		h.deliveryError(w, r, id, err)
		return
	}
	h.render(w, r, id, "Test email sent. Check your inbox (and your spam folder).", false, http.StatusOK)
}
func (h *SettingsHandler) deliveryError(w http.ResponseWriter, r *http.Request, id string, err error) {
	if errors.Is(err, notifications.ErrRateLimited) {
		w.Header().Set("Retry-After", "60")
		h.render(w, r, id, err.Error(), true, http.StatusTooManyRequests)
		return
	}
	if errors.Is(err, notifications.ErrUnverified) {
		h.render(w, r, id, err.Error(), true, http.StatusBadRequest)
		return
	}
	slog.Error("Notification delivery failed", "operation", "send_notification", "reason", "delivery_failed", "error_type", fmt.Sprintf("%T", err))
	h.render(w, r, id, "Preferences are saved, but the email could not be sent. Check your address or contact your administrator, then try again in a minute.", true, http.StatusServiceUnavailable)
}
