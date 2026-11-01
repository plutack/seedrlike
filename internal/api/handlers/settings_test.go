package handlers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/plutack/seedrlike/internal/notifications"
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

func TestUnsetNotificationSettings(t *testing.T) {
	state := &settingsSQL{}
	db := sql.OpenDB(settingsConnector{state})
	t.Cleanup(func() { _ = db.Close() })
	h := NewSettingsHandler(db)
	csrf := strings.Repeat("A", 43)
	form := url.Values{"csrf": {csrf}, "action": {"unset"}, "email": {"not a valid email"}, "completed": {"on"}, "failed": {"on"}, "User_ID": {"other-owner"}}
	r := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "seedrlike_settings_csrf", Value: csrf})
	r = r.WithContext(context.WithValue(r.Context(), UserIDKey, "owner"))
	w := httptest.NewRecorder()
	h.Save(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Email unset. Notifications disabled.") {
		t.Fatalf("unset: status %d, body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(state.query, "INSERT INTO Notification_Settings") || !reflect.DeepEqual(state.args, []driver.NamedValue{{Ordinal: 1, Value: "owner"}, {Ordinal: 2, Value: ""}, {Ordinal: 3, Value: false}, {Ordinal: 4, Value: false}}) {
		t.Fatalf("unset must clear address and preferences through owner-scoped Save: %s, %+v", state.query, state.args)
	}
}

func TestUnsetRequiresCSRF(t *testing.T) {
	h := NewSettingsHandler(nil)
	r := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader("action=unset"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = r.WithContext(context.WithValue(r.Context(), UserIDKey, "owner"))
	w := httptest.NewRecorder()
	h.Save(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unset without CSRF: %d", w.Code)
	}
}

func TestSettingsRateLimitMessage(t *testing.T) {
	db := sql.OpenDB(settingsConnector{&settingsSQL{}})
	t.Cleanup(func() { _ = db.Close() })
	h := NewSettingsHandler(db)
	w := httptest.NewRecorder()
	h.deliveryError(w, httptest.NewRequest(http.MethodPost, "/settings", nil), "owner", notifications.ErrRateLimited)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" || !strings.Contains(w.Body.String(), "Too many requests. Try again shortly.") || strings.Contains(w.Body.String(), notifications.ErrRateLimited.Error()) {
		t.Fatalf("unexpected rate-limit response: %d %s", w.Code, w.Body.String())
	}
}

// A connector records Save arguments without requiring a database or sending mail.
type settingsSQL struct {
	query string
	args  []driver.NamedValue
}
type settingsConnector struct{ state *settingsSQL }
type settingsDriver struct{}
type settingsConn struct{ state *settingsSQL }
type settingsRows struct{}

func (c settingsConnector) Connect(context.Context) (driver.Conn, error) {
	return settingsConn{c.state}, nil
}
func (settingsConnector) Driver() driver.Driver         { return settingsDriver{} }
func (settingsDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (settingsConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (settingsConn) Close() error              { return nil }
func (settingsConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c settingsConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.query, c.state.args = query, append([]driver.NamedValue(nil), args...)
	return driver.RowsAffected(1), nil
}
func (settingsConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return settingsRows{}, nil
}
func (settingsRows) Columns() []string {
	return []string{"Email", "Verified_At", "Notify_Completed", "Notify_Failed"}
}
func (settingsRows) Close() error              { return nil }
func (settingsRows) Next([]driver.Value) error { return io.EOF }

func TestSettingsRejectWrongMethod(t *testing.T) {
	h := NewSettingsHandler(nil)
	w := httptest.NewRecorder()
	h.Test(w, httptest.NewRequest("GET", "/settings/test", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "POST" {
		t.Fatal(w.Code)
	}
}
