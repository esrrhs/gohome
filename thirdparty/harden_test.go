package thirdparty

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ===========================================================================
// 一、LoadGeoip2 端到端：最小 GeoLite2-Country mmdb 生成器（移植自
// dns/matcher 加固测试，country 记录额外带 names.en）
// ===========================================================================

const (
	hpTypeString = 2
	hpTypeUint16 = 5
	hpTypeUint32 = 6
	hpTypeMap    = 7
	hpTypeUint64 = 9
	hpTypeArray  = 11
)

func hpCtrl(t byte, size int) byte { return byte(t)<<5 | byte(size) }
func hpString(s string) []byte     { return append([]byte{hpCtrl(hpTypeString, len(s))}, s...) }

func hpUint(t byte, v uint64) []byte {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], v)
	i := 0
	for i < 7 && raw[i] == 0 {
		i++
	}
	payload := raw[i:]
	if t == hpTypeUint64 {
		return append([]byte{hpCtrl(0, len(payload)), hpTypeUint64 - 7}, payload...)
	}
	return append([]byte{hpCtrl(t, len(payload))}, payload...)
}

func hpMap(n int) []byte { return []byte{hpCtrl(hpTypeMap, n)} }
func hpArr(n int) []byte { return []byte{hpCtrl(0, n), hpTypeArray - 7} }

// hpCountry 编码 {"country":{"iso_code":code,"names":{"en":name}}}
func hpCountry(code, name string) []byte {
	var b []byte
	b = append(b, hpMap(1)...)
	b = append(b, hpString("country")...)
	b = append(b, hpMap(2)...)
	b = append(b, hpString("iso_code")...)
	b = append(b, hpString(code)...)
	b = append(b, hpString("names")...)
	b = append(b, hpMap(1)...)
	b = append(b, hpString("en")...)
	b = append(b, hpString(name)...)
	return b
}

type hpNode struct{ rec [2]uint32 }

func hpInsert(nodes []*hpNode, bits []bool, data uint32) []*hpNode {
	cur := uint32(0)
	for _, b := range bits[:len(bits)-1] {
		side := uint32(0)
		if b {
			side = 1
		}
		next := nodes[cur].rec[side]
		if next == 0 {
			next = uint32(len(nodes))
			nodes[cur].rec[side] = next
			nodes = append(nodes, &hpNode{})
		}
		cur = next
	}
	last := uint32(0)
	if bits[len(bits)-1] {
		last = 1
	}
	nodes[cur].rec[last] = data
	return nodes
}

func hpBits(a [4]byte, n int) []bool {
	bits := make([]bool, 0, n)
	for i := 0; i < n; i++ {
		bits = append(bits, a[i>>3]&(0x80>>uint(i%8)) != 0)
	}
	return bits
}

func hpWriteDB(t *testing.T, path string, records map[string][2]string) {
	t.Helper()
	type info struct {
		bits []bool
		off  uint32
	}
	var infos []info
	var dataSection []byte
	for cidr, rec := range records {
		ip, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatalf("cidr %q: %v", cidr, err)
		}
		var a [4]byte
		copy(a[:], ip.To4())
		pfx, _ := ipnet.Mask.Size()
		off := uint32(len(dataSection))
		dataSection = append(dataSection, hpCountry(rec[0], rec[1])...)
		infos = append(infos, info{bits: hpBits(a, pfx), off: off})
	}

	const base = uint32(0x40000000)
	nodes := []*hpNode{{}}
	for i, ri := range infos {
		nodes = hpInsert(nodes, ri.bits, base+uint32(i))
	}
	nodeCount := uint32(len(nodes))
	for _, n := range nodes {
		for s := 0; s < 2; s++ {
			switch {
			case n.rec[s] == 0:
				n.rec[s] = nodeCount
			case n.rec[s] >= base:
				idx := n.rec[s] - base
				n.rec[s] = nodeCount + 16 + infos[idx].off
			}
		}
	}

	tree := make([]byte, int(nodeCount)*6)
	for i, n := range nodes {
		b := i * 6
		for s := 0; s < 2; s++ {
			tree[b+s*3+0] = byte(n.rec[s] >> 16)
			tree[b+s*3+1] = byte(n.rec[s] >> 8)
			tree[b+s*3+2] = byte(n.rec[s])
		}
	}

	var meta []byte
	ap := func(b []byte) { meta = append(meta, b...) }
	ap2 := func(k string, v []byte) { ap(hpString(k)); ap(v) }
	ap(hpMap(9))
	ap2("binary_format_major_version", hpUint(hpTypeUint16, 2))
	ap2("binary_format_minor_version", hpUint(hpTypeUint16, 0))
	ap2("build_epoch", hpUint(hpTypeUint64, 0))
	ap2("database_type", hpString("GeoLite2-Country"))
	ap(hpString("description"))
	ap(hpMap(1))
	ap(hpString("en"))
	ap(hpString("harden db"))
	ap2("ip_version", hpUint(hpTypeUint16, 4))
	ap(hpString("languages"))
	ap(hpArr(1))
	ap(hpString("en"))
	ap2("node_count", hpUint(hpTypeUint32, uint64(nodeCount)))
	ap2("record_size", hpUint(hpTypeUint16, 24))

	var file []byte
	file = append(file, tree...)
	file = append(file, make([]byte, 16)...)
	file = append(file, dataSection...)
	file = append(file, []byte("\xAB\xCD\xEFMaxMind.com")...)
	file = append(file, meta...)
	if err := os.WriteFile(path, file, 0o600); err != nil {
		t.Fatal(err)
	}
}

func hpDBPath(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "country.mmdb")
	hpWriteDB(t, p, map[string][2]string{
		"1.1.1.0/24": {"US", "United States"},
		"2.2.2.2/32": {"CN", "China"},
	})
	return p
}

func TestHardenGeoipLookupSuccess(t *testing.T) {
	resetGeoip(t)
	if err := LoadGeoip2(hpDBPath(t)); err != nil {
		t.Fatalf("LoadGeoip2: %v", err)
	}

	code, err := GetGeoipCountryIsoCode("1.1.1.1")
	if err != nil || code != "US" {
		t.Fatalf("iso 1.1.1.1 = %q,%v", code, err)
	}
	name, err := GetGeoipCountryName("1.1.1.1")
	if err != nil || name != "United States" {
		t.Fatalf("name 1.1.1.1 = %q,%v", name, err)
	}
	if code, err := GetGeoipCountryIsoCode("2.2.2.2"); err != nil || code != "CN" {
		t.Fatalf("iso 2.2.2.2 = %q,%v", code, err)
	}

	// 树外地址：无错误、空记录
	if c, err := country("8.8.8.8"); err != nil || c.IsoCode != "" || c.Name != "" {
		t.Fatalf("out-of-tree = %+v,%v", c, err)
	}

	// 库已加载时坏 IP 走 ParseIP 报错分支（消息带地址）
	if _, err := GetGeoipCountryIsoCode("not-an-ip"); err == nil ||
		!strings.Contains(err.Error(), "not-an-ip") {
		t.Fatalf("bad ip err = %v", err)
	}

	// IPv6 地址查 IPv4-only 库：reader 返回错误而非空记录
	if _, err := GetGeoipCountryIsoCode("2001:db8::1"); err == nil ||
		!strings.Contains(err.Error(), "IPv6") {
		t.Fatalf("ipv6 lookup err = %v", err)
	}
}

func TestHardenGeoipReloadReplacesOld(t *testing.T) {
	resetGeoip(t)
	first := hpDBPath(t)
	second := hpDBPath(t)

	if err := LoadGeoip2(first); err != nil {
		t.Fatal(err)
	}
	// 再加载：旧 reader 必须在锁内被 Close 且不影响在途查询
	if err := LoadGeoip2(second); err != nil {
		t.Fatal(err)
	}
	if code, err := GetGeoipCountryIsoCode("1.1.1.1"); err != nil || code != "US" {
		t.Fatalf("after reload = %q,%v", code, err)
	}

	// 空文件名走默认路径，当前工作目录不存在该文件 → 报错且保留已加载库
	if err := LoadGeoip2(""); err == nil {
		t.Fatal("empty file must fall back to default path and fail here")
	}
	if _, err := GetGeoipCountryIsoCode("1.1.1.1"); err != nil {
		t.Fatalf("failed reload must keep previous reader: %v", err)
	}
}

func TestHardenGeoipReloadRace(t *testing.T) {
	resetGeoip(t)
	p := hpDBPath(t)
	if err := LoadGeoip2(p); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				LoadGeoip2(p)
			}
		}()
	}
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = GetGeoipCountryIsoCode("1.1.1.1")
				_, _ = GetGeoipCountryName("8.8.8.8")
			}
		}()
	}
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// ===========================================================================
// 二、TMysql.Load 端到端：真实 mysql 驱动全仓无任何 import，
// 测试二进制里 "mysql" 驱动名空缺，用 fake 占用之（sync.Once 注册）。
// ===========================================================================

var registerMySQLFake sync.Once
var mysqlFake *hpMySQLDriver

type hpMySQLDriver struct {
	mu sync.Mutex
	// 行为开关
	openErr      error
	beginErr     error
	commitErr    error
	rollbackErr  error
	prepareErr   error
	stmtExecErr  error
	connExecErr  error
	queryErr     error
	countResult  int64
	countInvalid bool   // count(*) 返回无法 Scan 进 int 的类型
	rowsErr      error  // Next 耗尽数据行后返回的非 EOF 游标错误
	badScan      bool   // name/value 结果集返回 channel，convertAssign 必报错
	execOnly     string // 非空时只对包含该子串的 Exec 返回 connExecErr
	stmts        []string
}

type hpConn struct{ d *hpMySQLDriver }

func (c *hpConn) Prepare(query string) (driver.Stmt, error) {
	c.d.mu.Lock()
	err := c.d.prepareErr
	c.d.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &hpStmt{d: c.d, q: query}, nil
}
func (c *hpConn) Close() error { return nil }
func (c *hpConn) Begin() (driver.Tx, error) {
	if c.d.beginErr != nil {
		return nil, c.d.beginErr
	}
	return hpTx{c.d}, nil
}

func (c *hpConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	c.d.mu.Lock()
	c.d.stmts = append(c.d.stmts, q)
	err := c.d.connExecErr
	only := c.d.execOnly
	c.d.mu.Unlock()
	if err != nil && (only == "" || strings.Contains(q, only)) {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}

type hpRows struct {
	cols []string
	vals [][]driver.Value
	pos  int
	err  error
}

func (r *hpRows) Columns() []string { return r.cols }
func (r *hpRows) Close() error      { return nil }
func (r *hpRows) Err() error        { return r.err }
func (r *hpRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.vals) {
		// 现代 database/sql 在 EOF 时不主动调 driver.Rows.Err()，
		// 游标错误必须由 Next 直接以非 EOF 错误返回才会进 rows.Err()。
		if r.err != nil {
			return r.err
		}
		return io.EOF
	}
	copy(dest, r.vals[r.pos])
	r.pos++
	return nil
}

func (c *hpConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	c.d.stmts = append(c.d.stmts, q)
	if c.d.queryErr != nil {
		return nil, c.d.queryErr
	}
	if strings.Contains(q, "count(") {
		if c.d.rowsErr != nil {
			// 空结果集 + 游标错误：GetSize 的 !Next+rows.Err 分支
			return &hpRows{cols: []string{"count(*)"}, err: c.d.rowsErr}, nil
		}
		var v driver.Value = c.d.countResult
		if c.d.countInvalid {
			v = "not-an-int"
		}
		return &hpRows{cols: []string{"count(*)"}, vals: [][]driver.Value{{v}}}, nil
	}
	r := &hpRows{cols: []string{"name", "value"}, err: c.d.rowsErr}
	if c.d.badScan {
		r.vals = [][]driver.Value{{make(chan int), make(chan int)}}
	}
	return r, nil
}

type hpStmt struct {
	d *hpMySQLDriver
	q string
}

func (s *hpStmt) Close() error  { return nil }
func (s *hpStmt) NumInput() int { return -1 }
func (s *hpStmt) Exec(_ []driver.Value) (driver.Result, error) {
	s.d.mu.Lock()
	s.d.stmts = append(s.d.stmts, s.q)
	err := s.d.stmtExecErr
	s.d.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return driver.RowsAffected(1), nil
}
func (s *hpStmt) Query(_ []driver.Value) (driver.Rows, error) {
	return &hpRows{cols: []string{"name", "value"}}, nil
}

type hpTx struct{ d *hpMySQLDriver }

func (x hpTx) Commit() error   { return x.d.commitErr }
func (x hpTx) Rollback() error { return x.d.rollbackErr }

func (d *hpMySQLDriver) Open(_ string) (driver.Conn, error) {
	if d.openErr != nil {
		return nil, d.openErr
	}
	return &hpConn{d: d}, nil
}

func (d *hpMySQLDriver) resetLocked() {
	d.openErr = nil
	d.beginErr = nil
	d.commitErr = nil
	d.rollbackErr = nil
	d.prepareErr = nil
	d.stmtExecErr = nil
	d.connExecErr = nil
	d.queryErr = nil
	d.countResult = 0
	d.countInvalid = false
	d.rowsErr = nil
	d.badScan = false
	d.execOnly = ""
	d.stmts = nil
}

func resetMySQLFake(t *testing.T) {
	t.Helper()
	registerMySQLFake.Do(func() {
		mysqlFake = &hpMySQLDriver{}
		sql.Register("mysql", mysqlFake)
	})
	mysqlFake.mu.Lock()
	mysqlFake.resetLocked()
	mysqlFake.mu.Unlock()
	t.Cleanup(func() {
		mysqlFake.mu.Lock()
		mysqlFake.resetLocked()
		mysqlFake.mu.Unlock()
	})
}

func (d *hpMySQLDriver) snapshot() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.stmts...)
}

func TestHardenTMysqlLoadSuccess(t *testing.T) {
	resetMySQLFake(t)
	mysqlFake.countResult = 0
	tr := NewTMysql("user:pass@tcp(127.0.0.1:3306)/", 4, "dht_test", 7)

	if err := tr.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tr.gdb == nil {
		t.Fatal("Load must publish the handle")
	}
	stmts := mysqlFake.snapshot()
	joined := strings.Join(stmts, "\n")
	if !strings.Contains(joined, "CREATE DATABASE IF NOT EXISTS tmysql") {
		t.Fatalf("missing CREATE DATABASE: %s", joined)
	}
	if !strings.Contains(joined, "tmysql.dht_test") {
		t.Fatalf("missing qualified CREATE TABLE: %s", joined)
	}
	// Load 末尾做了一次 GetSize
	if got := tr.GetSize(); got != 0 {
		t.Fatalf("GetSize = %d, want 0", got)
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHardenTMysqlLoadInvalidTable(t *testing.T) {
	resetMySQLFake(t)
	tr := NewTMysql("dsn", 1, "a;DROP TABLE x", 7)
	if err := tr.Load(); err == nil {
		t.Fatal("Load with hostile table must fail")
	}
	if tr.gdb != nil {
		t.Fatal("failed Load must not publish a handle")
	}
	if len(mysqlFake.snapshot()) != 0 {
		t.Fatal("no statement should reach the driver")
	}
}

func TestHardenTMysqlLoadSchemaErrors(t *testing.T) {
	boom := errors.New("create db boom")

	// CREATE DATABASE 失败
	resetMySQLFake(t)
	mysqlFake.connExecErr = boom
	mysqlFake.execOnly = "CREATE DATABASE"
	tr := NewTMysql("dsn", 1, "dht_x", 7)
	if err := tr.Load(); !errors.Is(err, boom) {
		t.Fatalf("Load = %v, want create-db error", err)
	}
	if tr.gdb != nil {
		t.Fatal("handle must stay nil after create-db failure")
	}

	// CREATE TABLE 失败
	resetMySQLFake(t)
	mysqlFake.connExecErr = errors.New("create table boom")
	mysqlFake.execOnly = "CREATE TABLE"
	tr = NewTMysql("dsn", 1, "dht_x", 7)
	if err := tr.Load(); err == nil {
		t.Fatal("Load must fail when CREATE TABLE fails")
	}
	if tr.gdb != nil {
		t.Fatal("handle must stay nil after create-table failure")
	}
}

func mustLoad(t *testing.T, table string) *TMysql {
	t.Helper()
	resetMySQLFake(t)
	tr := NewTMysql("dsn", 2, table, 7)
	if err := tr.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return tr
}

func TestHardenTMysqlInsertTxErrors(t *testing.T) {
	// Begin 失败
	tr := mustLoad(t, "dht_begin")
	mysqlFake.beginErr = errors.New("begin boom")
	if err := tr.Insert("k", "v"); err == nil {
		t.Fatal("Insert must fail when Begin fails")
	}

	// Prepare 失败
	tr = mustLoad(t, "dht_prep")
	mysqlFake.prepareErr = errors.New("prepare boom")
	if err := tr.Insert("k", "v"); err == nil {
		t.Fatal("Insert must fail when Prepare fails")
	}

	// stmt Exec 失败（defer Rollback 兜底，不能 panic）
	tr = mustLoad(t, "dht_exec")
	mysqlFake.stmtExecErr = errors.New("exec boom")
	if err := tr.Insert("k", "v"); err == nil {
		t.Fatal("Insert must fail when stmt Exec fails")
	}

	// Commit 失败
	tr = mustLoad(t, "dht_commit")
	mysqlFake.commitErr = errors.New("commit boom")
	if err := tr.Insert("k", "v"); err == nil {
		t.Fatal("Insert must fail when Commit fails")
	}
}

func TestHardenTMysqlInsertHappyWithPurge(t *testing.T) {
	tr := mustLoad(t, "dht_ok")
	mysqlFake.countResult = 1
	if err := tr.Insert("k1", "v1"); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	joined := strings.Join(mysqlFake.snapshot(), "\n")
	if !strings.Contains(joined, "insert IGNORE into tmysql.dht_ok") {
		t.Fatalf("insert statement missing: %s", joined)
	}
	if !strings.Contains(joined, "delete from tmysql.dht_ok") {
		t.Fatalf("post-insert purge missing: %s", joined)
	}

	// purge 删除失败是 best-effort：Insert 仍成功
	tr = mustLoad(t, "dht_purge")
	mysqlFake.connExecErr = errors.New("delete boom")
	mysqlFake.execOnly = "delete"
	if err := tr.Insert("k", "v"); err != nil {
		t.Fatalf("purge failure must not fail Insert: %v", err)
	}
}

// TestHardenTMysqlResultErrorPaths 结果集层面的三类错误都被吞成零值，
// 不能 panic、不能返回半截数据。
func TestHardenTMysqlResultErrorPaths(t *testing.T) {
	tr := mustLoad(t, "dht_rows")

	// count(*) 值无法 Scan 进 int → 0
	mysqlFake.countInvalid = true
	if got := tr.GetSize(); got != 0 {
		t.Fatalf("GetSize with bad scan = %d, want 0", got)
	}
	mysqlFake.countInvalid = false

	// 游标错误：GetSize 首行都拿不到 → 0；Has → false；Last/FindValue → 空
	mysqlFake.rowsErr = errors.New("cursor boom")
	if got := tr.GetSize(); got != 0 {
		t.Fatalf("GetSize with cursor error = %d, want 0", got)
	}
	if tr.Has("k") {
		t.Fatal("Has must be false on rows.Err")
	}
	if got := tr.Last(5); len(got) != 0 {
		t.Fatalf("Last with rows.Err = %v, want empty", got)
	}
	if got := tr.FindValue("k", 5); len(got) != 0 {
		t.Fatalf("FindValue with rows.Err = %v, want empty", got)
	}
	mysqlFake.rowsErr = nil

	// 查询直接失败：Last/FindValue 返回空（GetSize/Has 的同类分支已有用例）
	mysqlFake.queryErr = errors.New("query boom")
	if got := tr.Last(5); got != nil {
		t.Fatalf("Last with query error = %v, want nil", got)
	}
	if got := tr.FindValue("k", 5); got != nil {
		t.Fatalf("FindValue with query error = %v, want nil", got)
	}
	mysqlFake.queryErr = nil

	// 行值类型不在 driver.Value 支持集（chan）：convertAssign 报错，
	// scanFind 必须在第一行就停下，不追加半行
	mysqlFake.rowsErr = nil
	mysqlFake.badScan = true
	if got := tr.Last(5); len(got) != 0 {
		t.Fatalf("Last with scan error = %v, want empty", got)
	}
	if got := tr.FindValue("k", 5); len(got) != 0 {
		t.Fatalf("FindValue with scan error = %v, want empty", got)
	}
}
