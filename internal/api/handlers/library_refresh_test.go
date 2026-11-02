package handlers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plutack/seedrlike/internal/core/upload"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
)

type refreshConnector struct{ state *refreshState }
type refreshState struct {
	rootRead  bool
	lookupErr error
}
type refreshConn struct{ state *refreshState }

func (c refreshConnector) Connect(context.Context) (driver.Conn, error) {
	return &refreshConn{c.state}, nil
}
func (refreshConnector) Driver() driver.Driver { return staleCleanupDriver{} }
func (*refreshConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*refreshConn) Close() error              { return nil }
func (*refreshConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c *refreshConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "GetFolderByID") {
		if c.state.lookupErr != nil {
			return nil, c.state.lookupErr
		}
		return &staleCleanupRows{columns: []string{"ID"}}, nil
	}
	if strings.Contains(q, "SELECT type, ID, Name") {
		c.state.rootRead = true
		return &staleCleanupRows{columns: []string{"type", "ID", "Name", "Size", "Date_Added", "Server"}}, nil
	}
	return nil, errors.New("unexpected query")
}
func TestPostDeleteMissingFolderFallsBackToRoot(t *testing.T) {
	state := &refreshState{}
	db := sql.OpenDB(refreshConnector{state})
	defer db.Close()
	r := httptest.NewRequest("DELETE", "/contents?folder=deleted&q=sample&sort=name&order=asc", nil)
	r = r.WithContext(context.WithValue(r.Context(), UserIDKey, "owner"))
	w := httptest.NewRecorder()
	renderLibraryAfterDelete(w, r, database.New(db))
	if w.Code != 200 || !state.rootRead {
		t.Fatalf("expected successful root refresh: status=%d root=%v body=%s", w.Code, state.rootRead, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("HX-Replace-Url"), "q=sample") {
		t.Fatal("root URL should preserve search options")
	}
	if !strings.Contains(w.Body.String(), "/contents/"+upload.RootFolderPlaceholder) {
		t.Fatal("root not rendered")
	}
	if strings.Contains(w.Body.String(), "Folder not found") {
		t.Fatal("committed deletion reported failure")
	}
}
func TestPostDeleteFolderDatabaseErrorStillFails(t *testing.T) {
	state := &refreshState{lookupErr: errors.New("database unavailable")}
	db := sql.OpenDB(refreshConnector{state})
	defer db.Close()
	w := httptest.NewRecorder()
	renderLibraryAfterDelete(w, httptest.NewRequest("DELETE", "/contents?folder=existing", nil), database.New(db))
	if w.Code != 500 || state.rootRead {
		t.Fatalf("database error hidden: %d", w.Code)
	}
}
