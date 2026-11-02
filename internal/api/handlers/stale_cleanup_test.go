package handlers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/plutack/go-gofile/model"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
)

type staleDeleteResult struct {
	response model.DeleteContentResponse
	err      error
}

type staleContentDeleter struct {
	results []staleDeleteResult
	calls   [][]string
}

func (d *staleContentDeleter) DeleteContent(ids ...string) (model.DeleteContentResponse, error) {
	d.calls = append(d.calls, append([]string(nil), ids...))
	if len(d.calls) > len(d.results) {
		return model.DeleteContentResponse{}, errors.New("unexpected remote deletion")
	}
	result := d.results[len(d.calls)-1]
	return result.response, result.err
}

func staleDeleteResponse(status string, items map[string]string) model.DeleteContentResponse {
	response := model.DeleteContentResponse{Status: status}
	if items != nil {
		response.Data = make(map[string]struct {
			Status string `json:"status"`
		}, len(items))
		for id, status := range items {
			response.Data[id] = struct {
				Status string `json:"status"`
			}{Status: status}
		}
	}
	return response
}

// Each failure must leave every local reference intact, even if the remote API
// returned nil error or confirmed only some of the requested IDs.
func staleDeleteFailures(first, second string) []struct {
	name   string
	result staleDeleteResult
} {
	return []struct {
		name   string
		result staleDeleteResult
	}{
		{"transport error", staleDeleteResult{err: errors.New("remote unavailable")}},
		{"top-level error", staleDeleteResult{response: staleDeleteResponse("error", map[string]string{first: "ok", second: "ok"})}},
		{"empty top-level status", staleDeleteResult{response: staleDeleteResponse("", map[string]string{first: "ok", second: "ok"})}},
		{"per-item error after success", staleDeleteResult{response: staleDeleteResponse("ok", map[string]string{first: "ok", second: "error-permissionDenied"})}},
		{"not-found is not confirmed", staleDeleteResult{response: staleDeleteResponse("ok", map[string]string{first: "ok", second: "error-notFound"})}},
		{"missing requested ID", staleDeleteResult{response: staleDeleteResponse("ok", map[string]string{first: "ok", "unrequested": "ok"})}},
		{"nil data", staleDeleteResult{response: staleDeleteResponse("ok", nil)}},
		{"empty data", staleDeleteResult{response: staleDeleteResponse("ok", map[string]string{})}},
		{"malformed item status", staleDeleteResult{response: staleDeleteResponse("ok", map[string]string{first: "ok", second: ""})}},
	}
}

func TestDeleteRemoteContentConfirmed(t *testing.T) {
	ids := []string{"file-a", "file-b"}
	for _, tc := range staleDeleteFailures(ids[0], ids[1]) {
		t.Run(tc.name, func(t *testing.T) {
			remote := &staleContentDeleter{results: []staleDeleteResult{tc.result}}
			if err := deleteRemoteContentConfirmed(remote, ids); err == nil {
				t.Fatal("unconfirmed remote deletion succeeded")
			}
			if !reflect.DeepEqual(remote.calls, [][]string{ids}) {
				t.Fatalf("remote calls = %v, want [%v]", remote.calls, ids)
			}
		})
	}
	t.Run("all requested IDs confirmed", func(t *testing.T) {
		remote := &staleContentDeleter{results: []staleDeleteResult{{response: staleDeleteResponse("ok", map[string]string{
			"file-a": "ok", "file-b": "ok", "extra": "error",
		})}}}
		if err := deleteRemoteContentConfirmed(remote, ids); err != nil {
			t.Fatalf("confirmed deletion failed: %v", err)
		}
		if !reflect.DeepEqual(remote.calls, [][]string{ids}) {
			t.Fatalf("remote calls = %v, want [%v]", remote.calls, ids)
		}
	})
	t.Run("empty lists make no remote request", func(t *testing.T) {
		for _, ids := range [][]string{nil, {}} {
			// A nil client would panic if an unnecessary request were attempted.
			if err := deleteRemoteContentConfirmed(nil, ids); err != nil {
				t.Fatalf("empty deletion failed: %v", err)
			}
		}
	})
}

func TestDeleteStaleContentRemoteFailureStillAttemptsBothBatchesAndLocalCleanup(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "cleanup-admin")
	files, folders := []string{"file-a", "file-b"}, []string{"folder-a", "folder-b"}
	for _, failure := range staleDeleteFailures(files[0], files[1]) {
		t.Run(failure.name, func(t *testing.T) {
			state, db := newStaleCleanupDB(t, files, folders)
			remote := &staleContentDeleter{results: []staleDeleteResult{failure.result, failure.result}}
			w := httptest.NewRecorder()
			DeleteStaleContentFromDB(database.New(db), remote, db)(w, staleCleanupRequest())
			// The test driver refuses Begin. Reaching it proves remote errors no
			// longer block the existing transactional local cleanup path.
			if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to start transaction") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if !reflect.DeepEqual(remote.calls, [][]string{files, folders}) {
				t.Fatalf("remote requests skipped: %v", remote.calls)
			}
			assertStaleCleanupReads(t, state)
			if state.begins != 1 {
				t.Fatalf("local cleanup not attempted: %d", state.begins)
			}
		})
	}
}

func TestDeleteStaleContentConfirmedBatchesReachTransaction(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "cleanup-admin")
	files, folders := []string{"file-a", "file-b"}, []string{"folder-a", "folder-b"}
	state, db := newStaleCleanupDB(t, files, folders)
	remote := &staleContentDeleter{results: []staleDeleteResult{
		{response: staleDeleteResponse("ok", map[string]string{files[0]: "ok", files[1]: "ok"})},
		{response: staleDeleteResponse("ok", map[string]string{folders[0]: "ok", folders[1]: "ok"})},
	}}
	w := httptest.NewRecorder()
	DeleteStaleContentFromDB(database.New(db), remote, db)(w, staleCleanupRequest())
	// The driver deliberately refuses Begin, stopping before the unrelated SQL
	// subtree-deletion/rendering code while proving both confirmations passed.
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "Failed to start transaction") {
		t.Fatalf("status = %d, body = %s; want transaction-start failure", w.Code, w.Body.String())
	}
	if !reflect.DeepEqual(remote.calls, [][]string{files, folders}) {
		t.Fatalf("remote calls = %v, want both batches", remote.calls)
	}
	assertStaleCleanupReads(t, state)
	if state.begins != 1 || len(state.execs) != 0 {
		t.Fatalf("Begin=%d, SQL writes=%v; want one attempted transaction and no writes", state.begins, state.execs)
	}
}

func staleCleanupRequest() *http.Request {
	r := httptest.NewRequest(http.MethodDelete, "/stale", nil)
	return r.WithContext(context.WithValue(r.Context(), UserIDKey, "admin-id"))
}

// This database/sql driver supplies the exact columns scanned by sqlc, records
// every query/write/transaction attempt, and cannot contact a live database.
// OpenDB avoids global driver registration and keeps each subtest isolated.
type staleCleanupSQL struct {
	files, folders []string
	queries, execs []string
	begins         int
}

type staleCleanupConnector struct{ state *staleCleanupSQL }
type staleCleanupDriver struct{}
type staleCleanupConn struct{ state *staleCleanupSQL }

func newStaleCleanupDB(t *testing.T, files, folders []string) (*staleCleanupSQL, *sql.DB) {
	t.Helper()
	state := &staleCleanupSQL{files: files, folders: folders}
	db := sql.OpenDB(staleCleanupConnector{state: state})
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close scripted database: %v", err)
		}
	})
	return state, db
}

func (c staleCleanupConnector) Connect(context.Context) (driver.Conn, error) {
	return &staleCleanupConn{state: c.state}, nil
}
func (staleCleanupConnector) Driver() driver.Driver { return staleCleanupDriver{} }
func (staleCleanupDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use the isolated connector")
}
func (*staleCleanupConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (*staleCleanupConn) Close() error { return nil }
func (c *staleCleanupConn) Begin() (driver.Tx, error) {
	c.state.begins++
	return nil, errors.New("scripted transaction boundary")
}
func (c *staleCleanupConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (c *staleCleanupConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.state.execs = append(c.state.execs, query)
	return nil, errors.New("unexpected database write")
}
func (c *staleCleanupConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.queries = append(c.state.queries, query)
	switch strings.Join(strings.Fields(query), " ") {
	case "-- name: GetUserByID :one SELECT id, password_hash, created_at, username FROM Users WHERE ID = ?":
		if len(args) != 1 || args[0].Value != "admin-id" {
			return nil, fmt.Errorf("unexpected user lookup arguments: %v", args)
		}
		return &staleCleanupRows{
			columns: []string{"id", "password_hash", "created_at", "username"},
			values:  [][]driver.Value{{"admin-id", "unused-hash", time.Unix(0, 0), "cleanup-admin"}},
		}, nil
	case "-- name: GetOldFiles :many SELECT ID FROM Files WHERE Date_Added < NOW() - INTERVAL 7 DAY":
		return staleCleanupIDRows(c.state.files), nil
	case "-- name: GetOldFolders :many SELECT ID FROM Folders WHERE Date_Added < NOW() - INTERVAL 7 DAY AND ID != '00000000-0000-0000-0000-000000000000'":
		return staleCleanupIDRows(c.state.folders), nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

func assertStaleCleanupReads(t *testing.T, state *staleCleanupSQL) {
	t.Helper()
	want := []string{"-- name: GetUserByID :one\n", "-- name: GetOldFiles :many\n", "-- name: GetOldFolders :many\n"}
	if len(state.queries) != len(want) {
		t.Fatalf("queries = %v, want only admin and both stale-list reads", state.queries)
	}
	for i, prefix := range want {
		if !strings.HasPrefix(state.queries[i], prefix) {
			t.Errorf("query %d = %s, want prefix %q", i, state.queries[i], prefix)
		}
	}
}

type staleCleanupRows struct {
	columns []string
	values  [][]driver.Value
	next    int
}

func staleCleanupIDRows(ids []string) *staleCleanupRows {
	rows := &staleCleanupRows{columns: []string{"ID"}}
	for _, id := range ids {
		rows.values = append(rows.values, []driver.Value{id})
	}
	return rows
}
func (r *staleCleanupRows) Columns() []string { return r.columns }
func (*staleCleanupRows) Close() error        { return nil }
func (r *staleCleanupRows) Next(dest []driver.Value) error {
	if r.next == len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.next])
	r.next++
	return nil
}
