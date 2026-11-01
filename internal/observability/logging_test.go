package observability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

func capture(t *testing.T, level string) *bytes.Buffer {
	t.Helper()
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	var b bytes.Buffer
	if err := Configure(&b, level); err != nil {
		t.Fatal(err)
	}
	return &b
}
func TestConfigure(t *testing.T) {
	b := capture(t, "info")
	slog.Debug("hidden")
	slog.Info("visible", "stage", "uploading")
	var row map[string]any
	if err := json.Unmarshal(b.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["msg"] != "visible" || row["level"] != "INFO" {
		t.Fatal(row)
	}
	if Configure(io.Discard, "oops") == nil {
		t.Fatal("accepted invalid level")
	}
}
func TestHTTPLogsRouteWithoutSensitiveURL(t *testing.T) {
	b := capture(t, "info")
	router := mux.NewRouter()
	router.Use(HTTP)
	router.HandleFunc("/settings/verify", func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) == "" {
			t.Error("missing context id")
		}
		w.WriteHeader(202)
		fmt.Fprint(w, "ok")
	})
	r := httptest.NewRequest("GET", "/settings/verify?token=SECRET&email=private@example.com", nil)
	r.Header.Set("Authorization", "Bearer SECRET")
	r.Header.Set("X-Request-ID", "SECRET")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Header().Get("X-Request-ID") == "" || w.Header().Get("X-Request-ID") == "SECRET" {
		t.Fatal("request id not generated")
	}
	if strings.Contains(b.String(), "SECRET") || strings.Contains(b.String(), "private@example.com") {
		t.Fatal("sensitive request leaked")
	}
	var row map[string]any
	if err := json.Unmarshal(b.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["route"] != "/settings/verify" || row["status"] != float64(202) || row["bytes"] != float64(2) {
		t.Fatal(row)
	}
}
func TestHTTPPanicIsNotMisreportedAsSuccess(t *testing.T) {
	b := capture(t, "info")
	h := HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("SECRET") }))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("middleware changed panic behavior")
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/secret-path?token=SECRET", nil))
	}()
	if !strings.Contains(b.String(), "http_request_aborted") || strings.Contains(b.String(), "SECRET") || strings.Contains(b.String(), `"status":200`) {
		t.Fatal(b.String())
	}
}
func TestHTTPMiddlewarePreservesWebsocketHijacking(t *testing.T) {
	b := capture(t, "info")
	done := make(chan struct{})
	router := mux.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(done); HTTP(next).ServeHTTP(w, r) })
	})
	router.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		u := websocket.Upgrader{}
		c, err := u.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close()
		_ = c.WriteMessage(websocket.TextMessage, []byte("ok"))
	})
	server := httptest.NewServer(router)
	defer server.Close()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, msg, err := c.ReadMessage()
	if err != nil || string(msg) != "ok" {
		t.Fatalf("%s %v", msg, err)
	}
	<-done
	if !strings.Contains(b.String(), `"status":101`) {
		t.Fatal("websocket upgrade status not logged")
	}
}
