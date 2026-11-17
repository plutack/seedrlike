package handlers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/mux"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
)

type mediaTestDriver struct {
	file *database.File
	err  error
}
type mediaTestConn struct{ mediaTestDriver }

func (d mediaTestDriver) Open(string) (driver.Conn, error) { return mediaTestConn{d}, nil }
func (c mediaTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c mediaTestConn) Close() error              { return nil }
func (c mediaTestConn) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }
func (c mediaTestConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(q, "WHERE ID = ?") || len(args) != 1 || args[0].Value != "file-id" {
		return nil, errors.New("unscoped lookup")
	}
	return &mediaTestRows{file: c.file}, c.err
}

type mediaTestRows struct {
	file *database.File
	done bool
}

func (*mediaTestRows) Columns() []string {
	return []string{"id", "name", "folder_id", "size", "mimetype", "md5", "server", "date_added", "user_id"}
}
func (*mediaTestRows) Close() error { return nil }
func (r *mediaTestRows) Next(v []driver.Value) error {
	if r.done || r.file == nil {
		return io.EOF
	}
	r.done = true
	f := r.file
	copy(v, []driver.Value{f.ID, f.Name, f.FolderID, f.Size, f.Mimetype, f.Md5, f.Server, nil, nil})
	if f.UserID.Valid {
		v[8] = f.UserID.String
	}
	return nil
}

var mediaDriverID atomic.Uint64

func newTestMedia(t *testing.T, f *database.File, err error) *MediaHandler {
	t.Helper()
	name := fmt.Sprintf("media-test-%d", mediaDriverID.Add(1))
	sql.Register(name, mediaTestDriver{file: f, err: err})
	db, e := sql.Open(name, "")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewMediaHandler(database.New(db))
}
func mediaFile() *database.File {
	return &database.File{ID: "file-id", Name: "movie.mp4", Server: "store1", Mimetype: "text/html"}
}
func mediaRequest(method, user string) *http.Request {
	r := httptest.NewRequest(method, "/media/file-id/stream", nil)
	r = mux.SetURLVars(r, map[string]string{"ID": "file-id"})
	return r.WithContext(context.WithValue(r.Context(), UserIDKey, user))
}

type mediaRoundTrip func(*http.Request) (*http.Response, error)

func (f mediaRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMediaVisibilityAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name, user, owner, filename, server string
		private, missing                    bool
		err                                 error
		status                              int
	}{
		{name: "public guest", filename: "a.mp4", server: "store1", status: 200},
		{name: "public signed in", user: "any", filename: "a.mp3", server: "store1", status: 200},
		{name: "owner", user: "owner", owner: "owner", private: true, filename: "a.webm", server: "store1", status: 200},
		{name: "other user", user: "other", owner: "owner", private: true, status: 404},
		{name: "private guest", owner: "owner", private: true, status: 404},
		{name: "empty private owner", private: true, status: 404},
		{name: "missing", missing: true, status: 404},
		{name: "unsupported", filename: "a.mkv", server: "store1", status: 415},
		{name: "unsafe server", filename: "a.mp4", server: "evil.com", status: 502},
		{name: "database failure", err: errors.New("db unavailable"), status: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := mediaFile()
			f.Name = tc.filename
			f.Server = tc.server
			f.UserID = sql.NullString{String: tc.owner, Valid: tc.private}
			if tc.missing {
				f = nil
			}
			h := newTestMedia(t, f, tc.err)
			h.Client = &http.Client{Transport: mediaRoundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected upstream call"); return nil, nil })}
			for _, handler := range []http.HandlerFunc{h.Metadata, h.Stream} {
				w := httptest.NewRecorder()
				handler(w, mediaRequest("GET", tc.user))
				// Valid streams are exercised independently below.
				if tc.status == 200 && strings.Contains(w.Header().Get("Content-Type"), "application/json") {
					var m mediaMetadata
					if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
						t.Fatal(err)
					}
					if m.Name != tc.filename || m.StreamURL != "/media/file-id/stream" || !strings.HasPrefix(m.DownloadURL, "https://store1.gofile.io/") || m.MIME == "text/html" {
						t.Fatalf("bad metadata: %+v", m)
					}
					break
				}
				if w.Code != tc.status {
					t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body.String())
				}
			}
		})
	}
}
func TestMediaStreamRangeAndHead(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			h := newTestMedia(t, mediaFile(), nil)
			h.Client = &http.Client{Transport: mediaRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Method != method || r.Header.Get("Range") != "bytes=2-4" || r.Header.Get("If-Range") != `"etag"` {
					t.Fatalf("range lost: %s %+v", r.Method, r.Header)
				}
				for _, key := range []string{"Cookie", "Authorization", "Proxy-Authorization", "X-Forwarded-For"} {
					if r.Header.Get(key) != "" {
						t.Errorf("forwarded %s", key)
					}
				}
				return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": {"bytes 2-4/10"}, "Content-Length": {"3"}, "Accept-Ranges": {"bytes"}, "Etag": {`"etag"`}, "Set-Cookie": {"upstream=secret"}, "Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("abc"))}, nil
			})}
			r := mediaRequest(method, "")
			r.Header.Set("Range", "bytes=2-4")
			r.Header.Set("If-Range", `"etag"`)
			r.Header.Set("Cookie", "session=secret")
			r.Header.Set("Authorization", "Bearer secret")
			w := httptest.NewRecorder()
			h.Stream(w, r)
			if w.Code != 206 || w.Header().Get("Content-Range") != "bytes 2-4/10" || w.Header().Get("Content-Type") != "video/mp4" || w.Header().Get("Set-Cookie") != "" {
				t.Fatalf("bad proxy response: %d %+v", w.Code, w.Header())
			}
			want := "abc"
			if method == "HEAD" {
				want = ""
			}
			if w.Body.String() != want {
				t.Fatalf("body %q", w.Body.String())
			}
		})
	}
}

// The next read must observe the preceding chunk already written downstream.
// This fails if the handler reads the entire body before writing it.
type mediaIncrementalBody struct {
	t      *testing.T
	output *httptest.ResponseRecorder
	reads  int
}

func (b *mediaIncrementalBody) Read(p []byte) (int, error) {
	b.reads++
	if b.reads == 1 {
		return copy(p, "first"), nil
	}
	if b.output.Body.String() != "first" {
		b.t.Fatal("stream buffered before forwarding")
	}
	return 0, io.EOF
}
func (*mediaIncrementalBody) Close() error { return nil }
func TestMediaStreamOwnerAndIncrementalCopy(t *testing.T) {
	f := mediaFile()
	f.UserID = sql.NullString{String: "owner", Valid: true}
	h := newTestMedia(t, f, nil)
	w := httptest.NewRecorder()
	h.Client = &http.Client{Transport: mediaRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: &mediaIncrementalBody{t: t, output: w}}, nil
	})}
	h.Stream(w, mediaRequest("GET", "owner"))
	if w.Code != 200 || w.Body.String() != "first" {
		t.Fatalf("stream failed: %d %q", w.Code, w.Body.String())
	}
}
func TestMediaUpstreamErrorsAndRedirects(t *testing.T) {
	for _, status := range []int{200, 403, 404, 429, 500, 416} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			h := newTestMedia(t, mediaFile(), nil)
			h.Client = &http.Client{Transport: mediaRoundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Range": {"bytes */10"}}, Body: io.NopCloser(strings.NewReader("upstream-secret"))}, nil
			})}
			w := httptest.NewRecorder()
			h.Stream(w, mediaRequest("GET", ""))
			want := 502
			if status == 200 || status == 416 {
				want = status
			}
			if w.Code != want {
				t.Fatalf("got %d want %d", w.Code, want)
			}
			if status != 200 && strings.Contains(w.Body.String(), "upstream-secret") {
				t.Fatal("leaked upstream error")
			}
		})
	}
	for _, target := range []string{"https://127.0.0.1/secret", "http://store1.gofile.io/a", "https://gofile.io.evil.example/a", "https://store-eu-par-1.gofileusercontent.com/file"} {
		t.Run(target, func(t *testing.T) {
			h := newTestMedia(t, mediaFile(), nil)
			calls := 0
			h.Client = &http.Client{Transport: mediaRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": {target}}, Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
			})}
			w := httptest.NewRecorder()
			h.Stream(w, mediaRequest("GET", ""))
			safe := strings.Contains(target, "gofileusercontent.com")
			if safe && (w.Code != 200 || calls != 2) {
				t.Fatalf("safe redirect failed: %d %d", w.Code, calls)
			}
			if !safe && (w.Code != 502 || calls != 1) {
				t.Fatalf("unsafe redirect followed: %d %d", w.Code, calls)
			}
		})
	}
	h := newTestMedia(t, mediaFile(), nil)
	h.Client = &http.Client{Transport: mediaRoundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("timeout secret") })}
	w := httptest.NewRecorder()
	h.Stream(w, mediaRequest("GET", ""))
	if w.Code != 502 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("bad network failure response")
	}
}
