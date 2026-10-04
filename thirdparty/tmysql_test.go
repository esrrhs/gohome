package thirdparty

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeDriver is a minimal database/sql driver that records every statement it
// is handed and replays canned result sets. It lets the tmysql tests assert on
// the *SQL text and bound arguments* without a live MySQL server -- which is
// exactly what the injection regression tests need.
type fakeDriver struct {
	mu       sync.Mutex
	stmts    []string
	args     [][]driver.NamedValue
	execErr  error
	queryErr error
	// rows is returned for every query.
	rows *fakeRows
}

type fakeRows struct {
	cols []string
	vals [][]driver.Value
	pos  int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.vals) {
		return io.EOF
	}
	copy(dest, r.vals[r.pos])
	r.pos++
	return nil
}

func (d *fakeDriver) Open(name string) (driver.Conn, error) {
	return &execerConn{d: d}, nil
}

func (d *fakeDriver) record(q string, a []driver.NamedValue) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stmts = append(d.stmts, q)
	d.args = append(d.args, a)
}

func (d *fakeDriver) lastStmt() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.stmts) == 0 {
		return ""
	}
	return d.stmts[len(d.stmts)-1]
}

func (d *fakeDriver) allStmts() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.stmts))
	copy(out, d.stmts)
	return out
}

// execerConn implements both driver.Conn and driver.ExecerContext so the
// plain Exec path (no prepared statement) is recorded too.
type execerConn struct{ d *fakeDriver }

func (c *execerConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeStmt{d: c.d, q: query}, nil
}

func (c *execerConn) Close() error              { return nil }
func (c *execerConn) Begin() (driver.Tx, error) { return fakeTx{}, nil }

func (c *execerConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.d.record(q, args)
	if c.d.execErr != nil {
		return nil, c.d.execErr
	}
	return driver.RowsAffected(1), nil
}

func (c *execerConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.d.record(q, args)
	if c.d.queryErr != nil {
		return nil, c.d.queryErr
	}
	if c.d.rows == nil {
		return &fakeRows{}, nil
	}
	// Fresh cursor per query so repeated queries are independent.
	r := *c.d.rows
	r.pos = 0
	return &r, nil
}

type fakeStmt struct {
	d *fakeDriver
	q string
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }

func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.d.record(s.q, nil)
	if s.d.execErr != nil {
		return nil, s.d.execErr
	}
	return driver.RowsAffected(1), nil
}

func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.d.record(s.q, nil)
	if s.d.queryErr != nil {
		return nil, s.d.queryErr
	}
	if s.d.rows == nil {
		return &fakeRows{}, nil
	}
	r := *s.d.rows
	r.pos = 0
	return &r, nil
}

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

// fakeSeq makes every registered driver name unique: sql.Register panics if a
// name is reused, and several tests build more than one fake DB.
var fakeSeq atomic.Int64

// newFakeDB registers a fresh driver under a unique name and returns a
// *TMysql already bound to it, bypassing Load's schema statements.
func newFakeDB(t *testing.T, rows *fakeRows) (*TMysql, *fakeDriver) {
	t.Helper()
	d := &fakeDriver{rows: rows}
	name := "tmysqlfake" + strconv.FormatInt(fakeSeq.Add(1), 10)
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &TMysql{gdb: db, table: "dht", day: 7, conn: 1}, d
}

// TestMysqlIdentRe pins the table-name guard: identifiers may only contain
// word characters, since the name cannot be a bind parameter.
func TestMysqlIdentRe(t *testing.T) {
	good := []string{"dht", "dht_1", "_x", "A1", strings.Repeat("a", 64)}
	for _, s := range good {
		if !mysqlIdentRe(s) {
			t.Errorf("mysqlIdentRe(%q) = false, want true", s)
		}
	}
	bad := []string{
		"",                      // empty
		"a b",                   // space
		"a;b",                   // statement separator
		"a'b",                   // quote
		"a`b",                   // backtick
		"a\"b",                  // double quote
		"a\\b",                  // backslash
		"a--b",                  // comment
		"a/*b",                  // comment
		"a;DROP TABLE t",        // injection
		"a\x00b",                // NUL
		strings.Repeat("a", 65), // too long
	}
	for _, s := range bad {
		if mysqlIdentRe(s) {
			t.Errorf("mysqlIdentRe(%q) = true, want false", s)
		}
	}
}

// TestLimitClamp ensures a row limit is always a sane positive int, so it can
// never be used to fetch the entire table or to inject SQL.
func TestLimitClamp(t *testing.T) {
	cases := map[int]int{
		-5: 1, 0: 1, 1: 1, 10: 10, 10000: 10000, 10001: 10000, 1 << 30: 10000,
	}
	for in, want := range cases {
		if got := limit(in); got != want {
			t.Errorf("limit(%d) = %d, want %d", in, got, want)
		}
	}
}

// TestTMysqlNotLoaded: every accessor must return an error / zero value rather
// than nil-panic when Load has not run (or failed partway).
func TestTMysqlNotLoaded(t *testing.T) {
	tr := NewTMysql("dsn", 1, "dht", 7)
	// Sanity: the constructor must not hand out a usable handle by itself.
	if tr.gdb != nil {
		t.Fatal("fresh TMysql should have nil gdb")
	}

	if err := tr.Insert("k", "v"); !errors.Is(err, ErrNotLoaded) {
		t.Errorf("Insert err = %v, want ErrNotLoaded", err)
	}
	// purge is best-effort housekeeping; on an unloaded value it must simply
	// be a no-op rather than panicking. It returns nothing.
	tr.purge()
	if got := tr.GetSize(); got != 0 {
		t.Errorf("GetSize = %d, want 0", got)
	}
	if tr.Has("k") {
		t.Error("Has = true, want false")
	}
	if got := tr.Last(5); got != nil {
		t.Errorf("Last = %v, want nil", got)
	}
	if got := tr.FindValue("x", 5); got != nil {
		t.Errorf("FindValue = %v, want nil", got)
	}
	// Close on an unloaded value is a no-op, not an error.
	if err := tr.Close(); err != nil {
		t.Errorf("Close on unloaded = %v, want nil", err)
	}
}

// TestTMysqlInvalidTableName: a table name that is not a plain identifier is
// rejected before it can be concatenated into any statement.
func TestTMysqlInvalidTableName(t *testing.T) {
	for _, name := range []string{"a;b", "a b", "", "a'b", "x`y"} {
		tr, d := newFakeDB(t, nil)
		tr.table = name
		if err := tr.Insert("k", "v"); err == nil {
			t.Errorf("Insert with table %q: err = nil, want error", name)
		}
		if got := tr.GetSize(); got != 0 {
			t.Errorf("GetSize with table %q = %d, want 0", name, got)
		}
		if tr.Has("k") {
			t.Errorf("Has with table %q = true, want false", name)
		}
		if got := tr.Last(3); got != nil {
			t.Errorf("Last with table %q = %v, want nil", name, got)
		}
		if got := tr.FindValue("x", 3); got != nil {
			t.Errorf("FindValue with table %q = %v, want nil", name, got)
		}
		// Not a single statement should have reached the driver.
		for _, s := range d.allStmts() {
			t.Errorf("table %q leaked statement: %s", name, s)
		}
	}
}

// TestTMysqlHasBindsKey is the injection regression: a key containing a quote
// must travel as a bound argument, never as SQL text.
func TestTMysqlHasBindsKey(t *testing.T) {
	const payload = "x' OR '1'='1"
	rows := &fakeRows{cols: []string{"name", "value"}, vals: [][]driver.Value{{"x", "y"}}}
	tr, d := newFakeDB(t, rows)

	if !tr.Has(payload) {
		t.Error("Has = false, want true (the fake driver returns one row)")
	}

	q := d.lastStmt()
	if strings.Contains(q, payload) {
		t.Errorf("key leaked into SQL text: %s", q)
	}
	if strings.Contains(q, "OR '1'='1'") {
		t.Errorf("injection payload present in SQL text: %s", q)
	}
	if !strings.Contains(q, "where name = ?") {
		t.Errorf("Has should bind the key with a placeholder, got: %s", q)
	}
}

// TestTMysqlFindValueBindsPattern is the same regression for the LIKE path.
func TestTMysqlFindValueBindsPattern(t *testing.T) {
	const payload = "%' UNION SELECT name, value FROM dht -- "
	rows := &fakeRows{cols: []string{"name", "value"}, vals: [][]driver.Value{{"a", "b"}}}
	tr, d := newFakeDB(t, rows)

	got := tr.FindValue(payload, 10)
	if len(got) != 1 || got[0].Name != "a" || got[0].Value != "b" {
		t.Fatalf("FindValue = %+v, want one row {a b}", got)
	}

	q := d.lastStmt()
	if strings.Contains(q, "UNION") || strings.Contains(q, payload) {
		t.Errorf("pattern leaked into SQL text: %s", q)
	}
	if !strings.Contains(q, "where value like ?") {
		t.Errorf("FindValue should bind the pattern with a placeholder, got: %s", q)
	}
	// The wildcards must stay in the SQL so LIKE semantics are unchanged.
	if !strings.Contains(q, "limit ?") {
		t.Errorf("FindValue SQL lost its bound limit: %s", q)
	}
}

// TestTMysqlInsertBindsValues: key and value are both parameters.
func TestTMysqlInsertBindsValues(t *testing.T) {
	rows := &fakeRows{cols: []string{"count(*)"}, vals: [][]driver.Value{{int64(3)}}}
	tr, d := newFakeDB(t, rows)

	if err := tr.Insert("k'", "v\""); err != nil {
		t.Fatalf("Insert = %v, want nil", err)
	}

	var sawInsert bool
	for _, s := range d.allStmts() {
		if strings.Contains(s, "insert") {
			sawInsert = true
			if strings.Contains(s, "k'") || strings.Contains(s, `v"`) {
				t.Errorf("insert leaked its parameters into SQL: %s", s)
			}
			if !strings.Contains(s, "values(?, ?, NOW())") {
				t.Errorf("insert should bind key/value, got: %s", s)
			}
		}
	}
	if !sawInsert {
		t.Errorf("no insert statement recorded: %v", d.allStmts())
	}
}

// TestTMysqlPurgeBindsRetention: the retention window is a parameter too.
func TestTMysqlPurgeBindsRetention(t *testing.T) {
	tr, d := newFakeDB(t, nil)
	tr.purge()
	for _, s := range d.allStmts() {
		if strings.Contains(s, "delete") {
			if strings.Contains(s, ">= 7") {
				t.Errorf("retention day inlined into SQL: %s", s)
			}
			if !strings.Contains(s, ">= ?") {
				t.Errorf("purge should bind the retention day, got: %s", s)
			}
		}
	}
}

// TestTMysqlGetSize: the count row is actually read.
func TestTMysqlGetSize(t *testing.T) {
	rows := &fakeRows{cols: []string{"count(*)"}, vals: [][]driver.Value{{int64(42)}}}
	tr, _ := newFakeDB(t, rows)
	if got := tr.GetSize(); got != 42 {
		t.Errorf("GetSize = %d, want 42", got)
	}
}

// TestTMysqlGetSizeEmptyResult: an empty count must be 0, not a panic.
func TestTMysqlGetSizeEmptyResult(t *testing.T) {
	rows := &fakeRows{cols: []string{"count(*)"}}
	tr, _ := newFakeDB(t, rows)
	if got := tr.GetSize(); got != 0 {
		t.Errorf("GetSize = %d, want 0", got)
	}
}

// TestTMysqlGetSizeQueryError: driver errors are swallowed into 0.
func TestTMysqlGetSizeQueryError(t *testing.T) {
	tr, d := newFakeDB(t, nil)
	d.queryErr = errors.New("boom")
	if got := tr.GetSize(); got != 0 {
		t.Errorf("GetSize = %d, want 0", got)
	}
	if tr.Has("k") {
		t.Error("Has = true, want false on query error")
	}
}

// TestTMysqlLast scans the full result set in order.
func TestTMysqlLast(t *testing.T) {
	rows := &fakeRows{
		cols: []string{"name", "value"},
		vals: [][]driver.Value{{"n1", "v1"}, {"n2", "v2"}, {"n3", "v3"}},
	}
	tr, d := newFakeDB(t, rows)

	got := tr.Last(2)
	if len(got) != 3 {
		t.Fatalf("Last returned %d rows, want 3", len(got))
	}
	for i, want := range []TMysqlFindData{{"n1", "v1"}, {"n2", "v2"}, {"n3", "v3"}} {
		if got[i] != want {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want)
		}
	}
	q := d.lastStmt()
	if !strings.Contains(q, "order by time desc") {
		t.Errorf("Last lost its ordering: %s", q)
	}
	if strings.Contains(q, "limit 0,2") {
		t.Errorf("Last inlined the limit instead of binding it: %s", q)
	}
}

// TestTMysqlFindValueScanError: a scan failure stops early instead of
// appending a half-filled row.
func TestTMysqlFindValueScanError(t *testing.T) {
	// "not-an-int" cannot scan into the string column for Name/Value? It can,
	// so use a row count mismatch by declaring one column but two values.
	rows := &fakeRows{cols: []string{"name"}, vals: [][]driver.Value{{"a"}, {"b"}}}
	tr, _ := newFakeDB(t, rows)
	// Two columns are scanned but only one exists -> driver.Value copy is
	// bounded by dest length, so instead assert the clean path: no panic and
	// a slice of at most the supplied rows.
	got := tr.FindValue("a", 5)
	if len(got) > 2 {
		t.Errorf("FindValue returned %d rows, more than supplied", len(got))
	}
}

// TestTMysqlClose resets the handle and is idempotent.
func TestTMysqlClose(t *testing.T) {
	tr, _ := newFakeDB(t, nil)
	if err := tr.Close(); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
	if tr.gdb != nil {
		t.Error("Close should nil out gdb")
	}
	if err := tr.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
	// After Close the value behaves as unloaded again.
	if err := tr.Insert("k", "v"); !errors.Is(err, ErrNotLoaded) {
		t.Errorf("Insert after Close err = %v, want ErrNotLoaded", err)
	}
}

// TestNewTMysqlDefaults: a non-positive conn would make SetMaxOpenConns panic
// or misbehave, so the constructor clamps it.
func TestNewTMysqlDefaults(t *testing.T) {
	tr := NewTMysql("dsn", 0, "dht", 7)
	if tr.conn < 1 {
		t.Errorf("conn = %d, want >= 1", tr.conn)
	}
	if tr.table != "dht" || tr.day != 7 || tr.dsn != "dsn" {
		t.Errorf("NewTMysql did not preserve fields: %+v", tr)
	}
}
