package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/plutack/seedrlike/internal/auth"
)

func TestAuthCookiePersistenceAndLifetime(t *testing.T) {
	for _, remember := range []bool{false, true} {
		for _, transport := range []string{"http", "https", "proxy"} {
			t.Run(transport+map[bool]string{false: "/session", true: "/remember"}[remember], func(t *testing.T) {
				target := "http://example.com/login"
				if transport == "https" {
					target = "https://example.com/login"
				}
				r := httptest.NewRequest(http.MethodPost, target, nil)
				if transport == "proxy" {
					r.Header.Set("X-Forwarded-Proto", "https")
				}
				w := httptest.NewRecorder()
				if err := setAuthCookie(w, r, "user-123", remember); err != nil {
					t.Fatal(err)
				}
				cookies := w.Result().Cookies()
				if len(cookies) != 1 {
					t.Fatalf("got %d cookies", len(cookies))
				}
				cookie := cookies[0]
				if cookie.Name != "token" || cookie.Path != "/" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Secure != (transport != "http") {
					t.Fatalf("invalid cookie: %+v", cookie)
				}
				claims, err := auth.ValidateToken(cookie.Value)
				if err != nil {
					t.Fatal(err)
				}
				want := 24 * time.Hour
				if remember {
					want = 30 * 24 * time.Hour
					if cookie.MaxAge != int(want/time.Second) || !cookie.Expires.Equal(claims.ExpiresAt.Time) {
						t.Fatalf("persistent cookie does not match JWT: %+v", cookie)
					}
				} else if cookie.MaxAge != 0 || !cookie.Expires.IsZero() || strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age") || strings.Contains(w.Header().Get("Set-Cookie"), "Expires") {
					t.Fatalf("session cookie persisted: %s", w.Header().Get("Set-Cookie"))
				}
				if claims.UserID != "user-123" || claims.ExpiresAt.Sub(claims.IssuedAt.Time) != want {
					t.Fatal("incorrect token lifetime")
				}
			})
		}
	}
}

func TestAuthSuccessSupportsNativeAndHTMX(t *testing.T) {
	for _, hx := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodPost, "/login", nil)
		if hx {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		authSuccess(w, r)
		if hx {
			if w.Code != http.StatusOK || w.Header().Get("HX-Refresh") != "true" {
				t.Fatal("HTMX refresh absent")
			}
		} else if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" || w.Header().Get("HX-Refresh") != "" {
			t.Fatal("native POST must redirect")
		}
	}
}

func TestLogoutDeletesCookie(t *testing.T) {
	w := httptest.NewRecorder()
	NewAuthHandler(nil).Logout(w, httptest.NewRequest(http.MethodPost, "https://example.com/logout", nil))
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 || cookies[0].Value != "" || !cookies[0].Expires.Before(time.Now()) {
		t.Fatalf("cookie not deleted: %+v", cookies)
	}
}

func TestAuthMiddlewareDoesNotSlideExpiry(t *testing.T) {
	token, err := auth.GenerateTokenWithDuration("user-123", auth.RememberTokenDuration)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "token", Value: token})
	w := httptest.NewRecorder()
	AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(UserIDKey) != "user-123" {
			t.Fatal("user context missing")
		}
	})).ServeHTTP(w, r.WithContext(context.Background()))
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("middleware renewed token")
	}
}

func TestAuthErrorsRemainVisible(t *testing.T) {
	for _, hx := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader("username=&password="))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if hx {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		NewAuthHandler(nil).Register(w, r)
		if !strings.Contains(w.Body.String(), "Username and password required") || !strings.Contains(w.Body.String(), `id="auth-error"`) || !strings.Contains(w.Body.String(), `hx-swap-oob="true"`) {
			t.Fatal("visible OOB error missing")
		}
	}
}
