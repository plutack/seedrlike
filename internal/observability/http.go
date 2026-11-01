package observability

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type responseRecorder struct {
	http.ResponseWriter
	status, bytes int
}

func (w *responseRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseRecorder) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseRecorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}
func (w *responseRecorder) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, r, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.status = 101
	}
	return c, r, err
}
func (w *responseRecorder) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{w}, r)
}

// HTTP logs route templates only, never request URLs, query strings, headers,
// bodies, IP addresses, credentials, or user-provided identifiers.
func HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := uuid.NewString()
		w.Header().Set("X-Request-ID", id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		rw := &responseRecorder{ResponseWriter: w}
		completed := false
		defer func() {
			route := "unmatched"
			if matched := mux.CurrentRoute(r); matched != nil {
				if template, err := matched.GetPathTemplate(); err == nil {
					route = template
				}
			}
			level := slog.LevelInfo
			if route == "/health" || route == "/live" || strings.HasPrefix(route, "/assets/") {
				level = slog.LevelDebug
			}
			status := rw.status
			if status == 0 {
				status = 200
			}
			if !completed {
				slog.Error("http_request_aborted", "request_id", id, "route", route)
				return
			}
			if status >= 500 {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			}
			slog.Log(r.Context(), level, "http_request", "request_id", id, "method", r.Method, "route", route, "status", status, "bytes", rw.bytes, "duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(rw, r)
		completed = true
	})
}
