package jobs

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

// recordingDB is a strictly scripted, in-memory database/sql driver. Each call
// records and consumes an expected operation, including transaction boundaries.
// It never opens a socket or uses an external database.
type recordingDB struct {
	t     *testing.T
	steps []dbStep
	calls []string
}

type dbStep struct {
	op      string
	query   string
	args    []driver.Value
	rows    [][]driver.Value
	columns []string
	result  driver.Result
	err     error
}

func (r *recordingDB) take(op, query string, args []driver.NamedValue) dbStep {
	r.t.Helper()
	r.calls = append(r.calls, op)
	if len(r.steps) == 0 {
		r.t.Errorf("unexpected %s: %s", op, query)
		return dbStep{err: fmt.Errorf("unexpected %s", op)}
	}
	s := r.steps[0]
	r.steps = r.steps[1:]
	if s.op != op || s.query != query {
		r.t.Errorf("operation = (%q, %q), want (%q, %q)", op, query, s.op, s.query)
	}
	var values []driver.Value
	for i, arg := range args {
		if arg.Ordinal != i+1 || arg.Name != "" {
			r.t.Errorf("argument %d = %#v, want unnamed positional argument", i, arg)
		}
		value := arg.Value
		if b, ok := value.([]byte); ok {
			value = append([]byte(nil), b...)
		}
		values = append(values, value)
	}
	if !reflect.DeepEqual(values, s.args) {
		r.t.Errorf("%s arguments = %#v, want %#v", op, values, s.args)
	}
	return s
}

func (r *recordingDB) Connect(context.Context) (driver.Conn, error) { return r, nil }
func (r *recordingDB) Driver() driver.Driver                        { return recordingDriver{r} }
func (r *recordingDB) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare: context interfaces should be used")
}
func (r *recordingDB) Close() error { return nil }
func (r *recordingDB) Begin() (driver.Tx, error) {
	s := r.take("begin", "", nil)
	if s.err != nil {
		return nil, s.err
	}
	return recordingTx{r}, nil
}
func (r *recordingDB) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	s := r.take("exec", query, args)
	if s.result == nil {
		s.result = driver.RowsAffected(1)
	}
	return s.result, s.err
}
func (r *recordingDB) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	s := r.take("query", query, args)
	if s.err != nil {
		return nil, s.err
	}
	return &recordingRows{columns: s.columns, rows: s.rows}, nil
}

type recordingDriver struct{ recorder *recordingDB }

func (d recordingDriver) Open(string) (driver.Conn, error) { return d.recorder, nil }

type recordingTx struct{ recorder *recordingDB }

func (tx recordingTx) Commit() error   { return tx.recorder.take("commit", "", nil).err }
func (tx recordingTx) Rollback() error { return tx.recorder.take("rollback", "", nil).err }

type recordingRows struct {
	columns []string
	rows    [][]driver.Value
}

func (r *recordingRows) Columns() []string { return r.columns }
func (r *recordingRows) Close() error      { return nil }
func (r *recordingRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dest, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

func newRecordingStore(t *testing.T, steps ...dbStep) (*Store, *recordingDB) {
	t.Helper()
	r := &recordingDB{t: t, steps: steps}
	db := sql.OpenDB(r)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		if len(r.steps) != 0 {
			t.Errorf("%d unconsumed database operations: %#v", len(r.steps), r.steps)
		}
	})
	return New(db), r
}

const (
	createJobSQL = `INSERT INTO Download_Jobs (ID,Info_Hash,Magnet,Name,Status,Stage,User_ID,Zipped,Selected_Files,Size,Reserved) VALUES (?,?,?,?,?,?,?,?,?,?,?)`
	activeJobSQL = `SELECT ID,Info_Hash,Magnet,Name,Status,Stage,User_ID,Zipped,Selected_Files,Size,Reserved FROM Download_Jobs ORDER BY Created_At,ID`
	saveJobSQL   = `UPDATE Download_Jobs SET Name=?,Status=?,Stage=?,Selected_Files=?,Size=?,Reserved=? WHERE ID=?`
	existsJobSQL = `SELECT 1 FROM Download_Jobs WHERE ID=?`
	deleteJobSQL = `DELETE FROM Download_Jobs WHERE ID=?`
	metricJobSQL = `INSERT INTO Download_Metrics (Day,Status,Jobs,Bytes) VALUES (CURRENT_DATE(),?,1,?) ON DUPLICATE KEY UPDATE Jobs=Jobs+1, Bytes=Bytes+VALUES(Bytes)`
)

func TestCreateSelectedEncoding(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selected []int
		json     string
	}{
		{name: "nil means all files", json: "null"},
		{name: "explicit empty selection", selected: []int{}, json: "[]"},
		{name: "explicit indexes", selected: []int{0, 3, 7}, json: "[0,3,7]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := Job{ID: "job-1", InfoHash: "hash", Magnet: "magnet", Name: "download", Status: "queued", Stage: "metadata", UserID: "user-1", Zipped: true, Selected: tc.selected, Size: 4096, Reserved: 8192}
			s, _ := newRecordingStore(t, dbStep{op: "exec", query: createJobSQL, args: []driver.Value{j.ID, j.InfoHash, j.Magnet, j.Name, j.Status, j.Stage, j.UserID, true, []byte(tc.json), j.Size, j.Reserved}})
			if err := s.Create(context.Background(), &j); err != nil {
				t.Fatalf("Create: %v", err)
			}
		})
	}
}

var activeColumns = []string{"ID", "Info_Hash", "Magnet", "Name", "Status", "Stage", "User_ID", "Zipped", "Selected_Files", "Size", "Reserved"}

func TestActiveParsesSelectedJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  driver.Value
		want []int
	}{
		{name: "SQL NULL", raw: nil},
		{name: "empty bytes", raw: []byte{}},
		{name: "JSON null", raw: []byte("null")},
		{name: "JSON empty array", raw: []byte("[]"), want: []int{}},
		{name: "JSON indexes", raw: []byte("[0,2,5]"), want: []int{0, 2, 5}},
		{name: "JSON string from driver", raw: "[1,4]", want: []int{1, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := Job{ID: "job-1", InfoHash: "hash", Magnet: "magnet", Name: "download", Status: "running", Stage: "download", UserID: "user-1", Zipped: true, Selected: tc.want, Size: 1234, Reserved: 5678}
			s, _ := newRecordingStore(t, dbStep{op: "query", query: activeJobSQL, columns: activeColumns, rows: [][]driver.Value{{want.ID, want.InfoHash, want.Magnet, want.Name, want.Status, want.Stage, want.UserID, true, tc.raw, want.Size, want.Reserved}}})
			got, err := s.Active(context.Background())
			if err != nil {
				t.Fatalf("Active: %v", err)
			}
			if !reflect.DeepEqual(got, []Job{want}) {
				t.Fatalf("Active = %#v, want %#v", got, []Job{want})
			}
		})
	}
}

func TestActiveRejectsMalformedSelection(t *testing.T) {
	for _, raw := range []string{"[", `["not-an-index"]`, `{"index":1}`} {
		t.Run(raw, func(t *testing.T) {
			s, _ := newRecordingStore(t, dbStep{op: "query", query: activeJobSQL, columns: activeColumns, rows: [][]driver.Value{{"broken-job", "hash", "magnet", "name", "running", "download", "user", false, []byte(raw), int64(1), int64(2)}}})
			got, err := s.Active(context.Background())
			if err == nil || !strings.Contains(err.Error(), "job broken-job selection:") {
				t.Fatalf("Active error = %v, want selection error identifying job", err)
			}
			if got != nil {
				t.Fatalf("Active = %#v on invalid JSON, want nil", got)
			}
		})
	}
}

func TestSaveUnchangedRow(t *testing.T) {
	queryErr := errors.New("existence lookup failed")
	for _, tc := range []struct {
		name string
		rows [][]driver.Value
		err  error
		want error
	}{
		{name: "existing unchanged row", rows: [][]driver.Value{{int64(1)}}},
		{name: "missing row", want: sql.ErrNoRows},
		{name: "lookup error", err: queryErr, want: queryErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := Job{ID: "job-1", Name: "name", Status: "running", Stage: "download", Selected: []int{1, 3}, Size: 100, Reserved: 200}
			s, _ := newRecordingStore(t,
				dbStep{op: "exec", query: saveJobSQL, args: []driver.Value{j.Name, j.Status, j.Stage, []byte("[1,3]"), j.Size, j.Reserved, j.ID}, result: driver.RowsAffected(0)},
				dbStep{op: "query", query: existsJobSQL, args: []driver.Value{j.ID}, columns: []string{"1"}, rows: tc.rows, err: tc.err},
			)
			if err := s.Save(context.Background(), j); !errors.Is(err, tc.want) {
				t.Fatalf("Save error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSaveChangedRowDoesNotCheckExistence(t *testing.T) {
	j := Job{ID: "job-1", Name: "name", Status: "running", Stage: "download", Size: 100, Reserved: 200}
	s, _ := newRecordingStore(t, dbStep{op: "exec", query: saveJobSQL, args: []driver.Value{j.Name, j.Status, j.Stage, []byte("null"), j.Size, j.Reserved, j.ID}, result: driver.RowsAffected(1)})
	if err := s.Save(context.Background(), j); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestFinishCommitsDeletionAndAggregate(t *testing.T) {
	for _, status := range []string{"completed", "failed", "stopped"} {
		t.Run(status, func(t *testing.T) {
			j := Job{ID: "job-1", Size: 9876}
			s, r := newRecordingStore(t,
				dbStep{op: "begin"},
				dbStep{op: "exec", query: deleteJobSQL, args: []driver.Value{j.ID}},
				dbStep{op: "exec", query: metricJobSQL, args: []driver.Value{status, j.Size}},
				dbStep{op: "commit"},
				// A retry sees no unfinished row and must not count it twice.
				dbStep{op: "begin"},
				dbStep{op: "exec", query: deleteJobSQL, args: []driver.Value{j.ID}, result: driver.RowsAffected(0)},
				dbStep{op: "commit"},
			)
			for attempt := 0; attempt < 2; attempt++ {
				if err := s.Finish(context.Background(), j, status); err != nil {
					t.Fatalf("Finish attempt %d: %v", attempt+1, err)
				}
			}
			want := []string{"begin", "exec", "exec", "commit", "begin", "exec", "commit"}
			if !reflect.DeepEqual(r.calls, want) {
				t.Fatalf("transaction operations = %v, want %v", r.calls, want)
			}
		})
	}
}

func TestFinishRollsBackOnError(t *testing.T) {
	deleteErr := errors.New("delete failed")
	metricErr := errors.New("aggregate failed")
	rollbackErr := errors.New("rollback failed")
	for _, tc := range []struct {
		name        string
		deleteErr   error
		metricErr   error
		rollbackErr error
		want        error
	}{
		{name: "delete error", deleteErr: deleteErr, want: deleteErr},
		{name: "aggregate error", metricErr: metricErr, want: metricErr},
		{name: "rollback error preserves aggregate error", metricErr: metricErr, rollbackErr: rollbackErr, want: metricErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := Job{ID: "job-1", Size: 1234}
			steps := []dbStep{
				{op: "begin"},
				{op: "exec", query: deleteJobSQL, args: []driver.Value{j.ID}, err: tc.deleteErr},
			}
			if tc.deleteErr == nil {
				steps = append(steps, dbStep{op: "exec", query: metricJobSQL, args: []driver.Value{"completed", j.Size}, err: tc.metricErr})
			}
			steps = append(steps, dbStep{op: "rollback", err: tc.rollbackErr})
			s, _ := newRecordingStore(t, steps...)
			if err := s.Finish(context.Background(), j, "completed"); !errors.Is(err, tc.want) {
				t.Fatalf("Finish error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestFinishRejectsNonterminalStatusBeforeTransaction(t *testing.T) {
	for _, status := range []string{"", "queued", "running", "COMPLETED"} {
		t.Run(status, func(t *testing.T) {
			s, _ := newRecordingStore(t)
			if err := s.Finish(context.Background(), Job{ID: "job-1"}, status); err == nil {
				t.Fatal("Finish accepted nonterminal status")
			}
		})
	}
}
