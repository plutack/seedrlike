package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	database "github.com/plutack/seedrlike/internal/database/sqlc"
)

type adminLookupStub struct {
	user  database.User
	err   error
	calls int
	id    string
}

func (s *adminLookupStub) GetUserByID(_ context.Context, id string) (database.User, error) {
	s.calls++
	s.id = id
	return s.user, s.err
}

func TestAuthorizeAdmin(t *testing.T) {
	tests := []struct {
		name, configured, username string
		identity                   any
		err                        error
		want, calls                int
	}{
		{name: "guest", configured: "operator", want: http.StatusUnauthorized},
		{name: "wrong context type", configured: "operator", identity: 123, want: http.StatusUnauthorized},
		{name: "empty identity", configured: "operator", identity: "", want: http.StatusUnauthorized},
		{name: "nonadmin", configured: "operator", identity: "u1", username: "reader", want: http.StatusForbidden, calls: 1},
		{name: "admin", configured: "operator", identity: "u1", username: "operator", want: http.StatusOK, calls: 1},
		{name: "unset admin disables access", identity: "u1", username: "operator", want: http.StatusForbidden, calls: 1},
		{name: "empty names do not grant access", identity: "u1", want: http.StatusForbidden, calls: 1},
		{name: "case sensitive configuration", configured: "Operator", identity: "u1", username: "operator", want: http.StatusForbidden, calls: 1},
		{name: "deleted user", configured: "operator", identity: "u1", err: sql.ErrNoRows, want: http.StatusUnauthorized, calls: 1},
		{name: "database unavailable", configured: "operator", identity: "u1", err: errors.New("offline"), want: http.StatusServiceUnavailable, calls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ADMIN_USERNAME", tt.configured)
			lookup := &adminLookupStub{user: database.User{Username: tt.username}, err: tt.err}
			ctx := context.Background()
			if tt.identity != nil {
				ctx = context.WithValue(ctx, UserIDKey, tt.identity)
			}
			if got := authorizeAdmin(ctx, lookup); got != tt.want {
				t.Fatalf("status = %d; want %d", got, tt.want)
			}
			if lookup.calls != tt.calls {
				t.Fatalf("lookup calls = %d; want %d", lookup.calls, tt.calls)
			}
			if tt.calls > 0 && lookup.id != "u1" {
				t.Fatalf("lookup identity = %q", lookup.id)
			}
		})
	}
}

func TestDashboardRejectsSpoofedIdentityBeforeSensitiveReads(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "operator")
	for _, authenticated := range []bool{false, true} {
		t.Run(map[bool]string{false: "guest", true: "nonadmin"}[authenticated], func(t *testing.T) {
			lookup := &adminLookupStub{user: database.User{Username: "reader"}}
			// Missing dependencies must never be touched for rejected callers.
			h := &AdminHandler{queries: lookup, dataDir: "/this/path/must/not/be/read"}
			request := httptest.NewRequest(http.MethodGet, "/admin?username=operator&userID=admin&admin=true", nil)
			request.Header.Set("X-User-ID", "admin")
			request.Header.Set("X-Admin", "true")
			request.Header.Set("X-Username", "operator")
			request.Header.Set("HX-Request", "true")
			want := http.StatusUnauthorized
			if authenticated {
				request = request.WithContext(context.WithValue(request.Context(), UserIDKey, "reader-id"))
				want = http.StatusForbidden
			}
			response := httptest.NewRecorder()
			h.Dashboard(response, request)
			if response.Code != want {
				t.Fatalf("status = %d; want %d", response.Code, want)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("sensitive route must not be cached")
			}
		})
	}
}

func TestAdminHandlerMissingDatabaseFailsClosed(t *testing.T) {
	h := NewAdminHandler(nil, nil, nil, "")
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request = request.WithContext(context.WithValue(request.Context(), UserIDKey, "u1"))
	response := httptest.NewRecorder()
	h.Dashboard(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestDashboardAdminUnavailableDependencies(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "operator")
	h := &AdminHandler{queries: &adminLookupStub{user: database.User{Username: "operator"}}}
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request = request.WithContext(context.WithValue(request.Context(), UserIDKey, "u1"))
	response := httptest.NewRecorder()
	h.Dashboard(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal("missing HTML content type")
	}
	for _, message := range []string{"Disk capacity unavailable", "Queue capacity unavailable", "Outcome counters unavailable"} {
		if !strings.Contains(response.Body.String(), message) {
			t.Errorf("missing %q", message)
		}
	}
}

func TestDashboardRejectsAdminWrites(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "operator")
	h := &AdminHandler{queries: &adminLookupStub{user: database.User{Username: "operator"}}}
	request := httptest.NewRequest(http.MethodPost, "/admin", nil)
	request = request.WithContext(context.WithValue(request.Context(), UserIDKey, "u1"))
	response := httptest.NewRecorder()
	h.Dashboard(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", response.Code)
	}
	if response.Header().Get("Allow") != http.MethodGet {
		t.Fatal("expected Allow: GET")
	}
}

func TestDashboardReadsActualFilesystem(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "operator")
	h := &AdminHandler{queries: &adminLookupStub{user: database.User{Username: "operator"}}, dataDir: t.TempDir()}
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request = request.WithContext(context.WithValue(request.Context(), UserIDKey, "u1"))
	response := httptest.NewRecorder()
	h.Dashboard(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if strings.Contains(response.Body.String(), "Disk capacity unavailable") {
		t.Fatal("existing filesystem should be measurable")
	}
	if !strings.Contains(response.Body.String(), "Disk available") || !strings.Contains(response.Body.String(), " total</p>") {
		t.Fatal("missing actual capacity")
	}
}
