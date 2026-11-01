package migrations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

// scriptedConnector keeps these tests entirely inside database/sql: no registered
// MySQL driver, network connection, or environment-based configuration is used.
type scriptedConnector struct {
	t        *testing.T
	steps    []queryStep
	beginErr error
	begins   int
}

type queryStep struct {
	query        string
	args         []driver.NamedValue
	value        driver.Value
	err          error
	checkContext func(context.Context)
}

func (s *scriptedConnector) Connect(context.Context) (driver.Conn, error) {
	return &scriptedConn{s}, nil
}
func (s *scriptedConnector) Driver() driver.Driver { return scriptedDriver{} }

type scriptedDriver struct{}

func (scriptedDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("scripted driver requires its connector")
}

type scriptedConn struct{ script *scriptedConnector }

func (c *scriptedConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (c *scriptedConn) Close() error { return nil }
func (c *scriptedConn) Begin() (driver.Tx, error) {
	c.script.begins++
	if c.script.beginErr != nil {
		return nil, c.script.beginErr
	}
	return nil, errors.New("unexpected Begin")
}
func (c *scriptedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	s := c.script
	if len(s.steps) == 0 {
		s.t.Errorf("unexpected query %q with args %v", query, args)
		return nil, errors.New("unexpected query")
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if query != step.query {
		s.t.Errorf("query = %q, want %q", query, step.query)
	}
	if len(args) != len(step.args) || (len(args) > 0 && !reflect.DeepEqual(args, step.args)) {
		s.t.Errorf("query %q args = %#v, want %#v", query, args, step.args)
	}
	if step.checkContext != nil {
		step.checkContext(ctx)
	}
	if step.err != nil {
		return nil, step.err
	}
	return &scriptedRows{value: step.value}, nil
}

type scriptedRows struct {
	value driver.Value
	done  bool
}

func (*scriptedRows) Columns() []string { return []string{"result"} }
func (*scriptedRows) Close() error      { return nil }
func (r *scriptedRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.value
	return nil
}

func scriptedDatabase(t *testing.T, steps ...queryStep) (*sql.DB, *scriptedConnector) {
	t.Helper()
	script := &scriptedConnector{t: t, steps: steps}
	db := sql.OpenDB(script)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
		if len(script.steps) != 0 {
			t.Errorf("%d scripted queries not executed", len(script.steps))
		}
	})
	return db, script
}

func scriptedConnection(t *testing.T, steps ...queryStep) *sql.Conn {
	t.Helper()
	db, _ := scriptedDatabase(t, steps...)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close connection: %v", err)
		}
	})
	return conn
}

func namedArgs(values ...driver.Value) []driver.NamedValue {
	args := make([]driver.NamedValue, len(values))
	for i, value := range values {
		args[i] = driver.NamedValue{Ordinal: i + 1, Value: value}
	}
	return args
}

func TestMySQLLockerDatabaseScopedStableNames(t *testing.T) {
	var names []string
	for _, database := range []string{"seedrlike", "seedrlike", "another_database", strings.Repeat("long_database_", 20)} {
		name := lockName(database)
		if len(name) == 0 || len(name) > 64 {
			t.Fatalf("lock name length = %d, want 1..64", len(name))
		}
		names = append(names, name)
		conn := scriptedConnection(t,
			queryStep{query: "SELECT DATABASE()", value: database},
			queryStep{query: "SELECT GET_LOCK(?, ?)", args: namedArgs(name, int64(60)), value: int64(1)},
			queryStep{query: "SELECT DATABASE()", value: database},
			queryStep{query: "SELECT RELEASE_LOCK(?)", args: namedArgs(name), value: int64(1)},
		)
		if err := (mysqlLocker{}).SessionLock(context.Background(), conn); err != nil {
			t.Fatal(err)
		}
		if err := (mysqlLocker{}).SessionUnlock(context.Background(), conn); err != nil {
			t.Fatal(err)
		}
	}
	if names[0] != names[1] {
		t.Error("same database produced different lock names")
	}
	if names[0] == names[2] || names[0] == names[3] || names[2] == names[3] {
		t.Error("different databases share a lock name")
	}
}

func TestMySQLLockerSessionLockFailures(t *testing.T) {
	queryErr := errors.New("scripted query failure")
	for _, tc := range []struct {
		name  string
		steps []queryStep
		want  string
		cause error
	}{
		{name: "false means timeout", steps: []queryStep{{query: "SELECT DATABASE()", value: "seedrlike"}, {query: "SELECT GET_LOCK(?, ?)", args: namedArgs(lockName("seedrlike"), int64(60)), value: int64(0)}}, want: "within 60 seconds"},
		{name: "null means timeout", steps: []queryStep{{query: "SELECT DATABASE()", value: "seedrlike"}, {query: "SELECT GET_LOCK(?, ?)", args: namedArgs(lockName("seedrlike"), int64(60)), value: nil}}, want: "within 60 seconds"},
		{name: "lock query error", steps: []queryStep{{query: "SELECT DATABASE()", value: "seedrlike"}, {query: "SELECT GET_LOCK(?, ?)", args: namedArgs(lockName("seedrlike"), int64(60)), err: queryErr}}, want: "acquire migration lock", cause: queryErr},
		{name: "database query error", steps: []queryStep{{query: "SELECT DATABASE()", err: queryErr}}, cause: queryErr},
		{name: "missing database", steps: []queryStep{{query: "SELECT DATABASE()", value: nil}}, want: "database must be selected"},
		{name: "empty database", steps: []queryStep{{query: "SELECT DATABASE()", value: ""}}, want: "database must be selected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (mysqlLocker{}).SessionLock(context.Background(), scriptedConnection(t, tc.steps...))
			if err == nil {
				t.Fatal("SessionLock succeeded, want error")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err, tc.want)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Errorf("error = %v, want wrapped %v", err, tc.cause)
			}
		})
	}
}

func TestMySQLLockerSessionUnlockUsesFreshDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checkContext := func(ctx context.Context) {
		if err := ctx.Err(); err != nil {
			t.Errorf("unlock context already canceled: %v", err)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second {
			t.Errorf("unlock deadline = %v, want fresh deadline within five seconds", deadline)
		}
	}
	conn := scriptedConnection(t,
		queryStep{query: "SELECT DATABASE()", value: "seedrlike", checkContext: checkContext},
		queryStep{query: "SELECT RELEASE_LOCK(?)", args: namedArgs(lockName("seedrlike")), value: int64(1), checkContext: checkContext},
	)
	if err := (mysqlLocker{}).SessionUnlock(ctx, conn); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLLockerSessionUnlockFailures(t *testing.T) {
	queryErr := errors.New("scripted unlock query failure")
	for _, tc := range []struct {
		name  string
		value driver.Value
		err   error
	}{
		{name: "not held", value: int64(0)},
		{name: "null", value: nil},
		{name: "query error", err: queryErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := scriptedConnection(t,
				queryStep{query: "SELECT DATABASE()", value: "seedrlike"},
				queryStep{query: "SELECT RELEASE_LOCK(?)", args: namedArgs(lockName("seedrlike")), value: tc.value, err: tc.err},
			)
			err := (mysqlLocker{}).SessionUnlock(context.Background(), conn)
			if err == nil {
				t.Fatal("SessionUnlock succeeded, want error")
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Errorf("error = %v, want %v", err, tc.err)
			}
			if tc.err == nil && !strings.Contains(err.Error(), "not released") {
				t.Errorf("unexpected release error: %v", err)
			}
		})
	}
}

func TestEmbeddedMigrationsGooseProvider(t *testing.T) {
	files, err := fs.Glob(Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	const wantCount = 6
	if len(files) != wantCount {
		t.Fatalf("embedded SQL files = %d, want %d", len(files), wantCount)
	}
	db, script := scriptedDatabase(t)
	provider, err := goose.NewProvider(goose.DialectMySQL, db, Files,
		goose.WithDisableGlobalRegistry(true), goose.WithDisableVersioning(true))
	if err != nil {
		t.Fatalf("initialize provider: %v", err)
	}
	sources := provider.ListSources()
	if len(sources) != wantCount {
		t.Fatalf("provider sources = %d, want %d", len(sources), wantCount)
	}
	for i, source := range sources {
		if source.Path != files[i] {
			t.Errorf("source %d = %q, want %q", i, source.Path, files[i])
		}
		if source.Type != goose.TypeSQL {
			t.Errorf("source %q type = %v, want SQL", source.Path, source.Type)
		}
		if i > 0 && source.Version <= sources[i-1].Version {
			t.Errorf("source versions not strictly ascending: %v", sources)
		}
		content, err := Files.ReadFile(source.Path)
		if err != nil || len(content) == 0 {
			t.Errorf("read embedded source %q: %v (bytes %d)", source.Path, err, len(content))
		}
	}
	// Goose parses SQL lazily, not in NewProvider/ListSources. With version
	// tracking disabled, Up parses every source (both directions) before its
	// first Begin. Stop there so this validates parsing without executing SQL.
	stopBeforeSQL := fmt.Errorf("stop after parsing, before executing migrations")
	script.beginErr = stopBeforeSQL
	if _, err := provider.Up(context.Background()); !errors.Is(err, stopBeforeSQL) {
		t.Fatalf("parse embedded migrations: got %v, want execution barrier %v", err, stopBeforeSQL)
	}
	if script.begins != 1 {
		t.Errorf("Begin calls = %d, want 1", script.begins)
	}
}

func TestUpRejectsNilDatabase(t *testing.T) {
	if err := Up(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("Up(nil) = %v, want database required error", err)
	}
}
