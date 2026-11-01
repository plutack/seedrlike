package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNotificationEndpointsRequireOwner(t *testing.T) {
	h := NewSettingsHandler(nil)
	for _, tc := range []struct {
		method string
		fn     http.HandlerFunc
	}{{"GET", h.Settings}, {"POST", h.Save}, {"GET", h.Verify}, {"POST", h.Verify}, {"POST", h.Test}} {
		r := httptest.NewRequest(tc.method, "/settings", nil)
		w := httptest.NewRecorder()
		tc.fn(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s got %d", tc.method, w.Code)
		}
	}
}
func TestVerificationGetOnlyConfirmsAndEscapes(t *testing.T) {
	h := NewSettingsHandler(nil) // GET must never need a database.
	token := `"><script>alert(1)</script>`
	r := httptest.NewRequest("GET", "/settings/verify?token="+url.QueryEscape(token), nil)
	r = r.WithContext(context.WithValue(r.Context(), UserIDKey, "owner"))
	w := httptest.NewRecorder()
	h.Verify(w, r)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, token) || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("token not escaped")
	}
	if !strings.Contains(body, `method="post"`) || !strings.Contains(body, "Verify my email") {
		t.Fatal("confirmation form absent")
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("token privacy headers absent")
	}
}
func TestMutationsRequireCSRF(t *testing.T) {
	h := NewSettingsHandler(nil)
	for _, fn := range []http.HandlerFunc{h.Save, h.Test, h.Verify} {
		r := httptest.NewRequest("POST", "/settings", strings.NewReader("email=a%40example.com"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r = r.WithContext(context.WithValue(r.Context(), UserIDKey, "owner"))
		w := httptest.NewRecorder()
		fn(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatal(w.Code)
		}
	}
}
func TestSettingsCSRFCookieTransportSecurity(t *testing.T) {
	for _, tc := range []struct {
		name, target, forwarded string
		secure                  bool
	}{
		{"plain HTTP", "http://example.com/settings", "", false},
		{"direct HTTPS", "https://example.com/settings", "", true},
		{"proxy HTTPS", "http://example.com/settings", "https", true},
		{"proxy HTTP", "http://example.com/settings", "http", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.target, nil)
			if tc.forwarded != "" {
				r.Header.Set("X-Forwarded-Proto", tc.forwarded)
			}
			w := httptest.NewRecorder()
			token, err := settingsCSRF(w, r)
			if err != nil {
				t.Fatal(err)
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("got %d cookies", len(cookies))
			}
			c := cookies[0]
			if c.Name != "seedrlike_settings_csrf" || c.Value != token || !validCSRF(token) || c.Path != "/settings" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.MaxAge != 3600 || c.Secure != tc.secure {
				t.Fatalf("unexpected CSRF cookie: %+v", c)
			}
		})
	}
}

func TestSettingsRejectWrongMethod(t *testing.T) {
	h := NewSettingsHandler(nil)
	w := httptest.NewRecorder()
	h.Test(w, httptest.NewRequest("GET", "/settings/test", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "POST" {
		t.Fatal(w.Code)
	}
}
