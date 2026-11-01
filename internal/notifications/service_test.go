package notifications

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A recording SQL driver keeps security assertions hermetic and avoids a live
// database, a new test dependency, and all outbound SMTP.
type recordingDriver struct{ state *recordedSQL }
type recordedSQL struct {
	query    string
	args     []driver.NamedValue
	affected int64
}
type recordingConn struct{ state *recordedSQL }

func (d recordingDriver) Open(string) (driver.Conn, error) { return recordingConn{d.state}, nil }
func (c recordingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c recordingConn) Close() error              { return nil }
func (c recordingConn) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }
func (c recordingConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.state.query = q
	c.state.args = args
	return driver.RowsAffected(c.state.affected), nil
}
func (c recordingConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.query = q
	c.state.args = args
	return emptyRows{}, nil
}

type emptyRows struct{}

func (emptyRows) Columns() []string {
	return []string{"Email", "Verified_At", "Notify_Completed", "Notify_Failed"}
}
func (emptyRows) Close() error              { return nil }
func (emptyRows) Next([]driver.Value) error { return io.EOF }

var driverCounter atomic.Uint64

func recordedDB(t *testing.T) (*sql.DB, *recordedSQL) {
	t.Helper()
	state := &recordedSQL{affected: 1}
	name := "notification-recording-" + time.Now().Format("150405.000000000") + string(rune(driverCounter.Add(1)))
	sql.Register(name, recordingDriver{state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, state
}
func TestVerifyOwnerHashExpiryAndSingleUsePredicate(t *testing.T) {
	db, state := recordedDB(t)
	s := New(db)
	token, hash, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Verify(context.Background(), "owner-id", token); err != nil {
		t.Fatal(err)
	}
	for _, condition := range []string{"User_ID=?", "Verification_Hash=?", "Verification_Expires_At>?", "Verification_Hash=NULL", "Verification_Expires_At=NULL"} {
		if !strings.Contains(state.query, condition) {
			t.Fatal("missing secure verify predicate", condition)
		}
	}
	if state.args[1].Value != "owner-id" || string(state.args[2].Value.([]byte)) != string(hash) {
		t.Fatal("verification not scoped to owner and token hash")
	}
	if _, ok := state.args[3].Value.(time.Time); !ok {
		t.Fatal("expiry not bound to current time")
	}
	state.affected = 0
	if err = s.Verify(context.Background(), "different-owner", token); !errors.Is(err, ErrInvalidToken) {
		t.Fatal("unmatched/expired/consumed token accepted", err)
	}
	if err = s.Verify(context.Background(), "owner-id", "bad-token"); !errors.Is(err, ErrInvalidToken) {
		t.Fatal(err)
	}
}
func TestSaveResetsVerificationOnChangedEmailOnly(t *testing.T) {
	db, state := recordedDB(t)
	s := New(db)
	if err := s.Save(context.Background(), "owner", "new@example.com", true, false); err != nil {
		t.Fatal(err)
	}
	for _, condition := range []string{"Verified_At=IF(BINARY Email=BINARY VALUES(Email),Verified_At,NULL)", "Verification_Hash=IF(BINARY Email=BINARY VALUES(Email),Verification_Hash,NULL)", "Verification_Expires_At=IF(BINARY Email=BINARY VALUES(Email),Verification_Expires_At,NULL)"} {
		if !strings.Contains(state.query, condition) {
			t.Fatal("email changes do not invalidate verification", condition)
		}
	}
	if strings.Contains(state.query, "Last_Verification_At=") || strings.Contains(state.query, "Last_Test_At=") {
		t.Fatal("email change must not reset cooldown")
	}
	if state.args[0].Value != "owner" || state.args[1].Value != "new@example.com" {
		t.Fatal("save not owner-scoped")
	}
}
func TestServiceRejectsMissingOwner(t *testing.T) {
	s := New(nil)
	ctx := context.Background()
	if s.Save(ctx, "", "a@example.com", true, true) == nil {
		t.Fatal("save accepts guest")
	}
	if s.Verify(ctx, "", "invalid") == nil {
		t.Fatal("verify accepts guest")
	}
	if s.RequestVerification(ctx, "") == nil {
		t.Fatal("request accepts guest")
	}
	if s.Test(ctx, "") == nil {
		t.Fatal("test accepts guest")
	}
	if s.Notify(ctx, "", "file", "completed") != nil {
		t.Fatal("guest worker should be ignored")
	}
}
