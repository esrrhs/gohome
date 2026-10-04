package thirdparty

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/esrrhs/gohome/loggo"
)

/*
Package tmap 提供了一个用于操作MySQL数据库的结构体TMysql。
该结构体支持连接数据库、创建数据库和表、插入记录、查询记录及查询特定条件的记录。
功能包括：

- 初始化MySQL连接
- 加载数据库和表结构
- 插入数据并管理数据的保留策略
- 获取记录总数
- 检查特定记录是否存在
- 获取最近的记录
- 根据条件查找记录
*/

type TMysql struct {
	gdb   *sql.DB
	dsn   string
	table string
	day   int
	conn  int
}

// ErrNotLoaded is returned by every method when Load has not succeeded yet.
// Without it the queries would nil-panic on the zero *sql.DB.
var ErrNotLoaded = errors.New("tmysql: Load() must be called first")

// mysqlIdentRe guards the table name, which cannot be passed as a bind
// parameter. Anything outside [A-Za-z0-9_] is rejected instead of quoted so
// that a hostile table name can never reach the SQL text.
var mysqlIdentRe = func(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// limit clamps a caller-supplied row limit into a sane positive range so it
// can be interpolated safely (and cannot be used to request the whole table).
func limit(n int) int {
	if n <= 0 {
		return 1
	}
	if n > 10000 {
		return 10000
	}
	return n
}

// tableRef validates the table name and returns the qualified reference used
// in every query.
func (t *TMysql) tableRef() (string, error) {
	if t.gdb == nil {
		return "", ErrNotLoaded
	}
	if !mysqlIdentRe(t.table) {
		return "", fmt.Errorf("tmysql: invalid table name %q", t.table)
	}
	return "tmysql." + t.table, nil
}

func NewTMysql(dsn string, conn int, table string, day int) *TMysql {
	if conn <= 0 {
		conn = 1
	}
	return &TMysql{dsn: dsn, conn: conn, table: table, day: day}
}

func (t *TMysql) Load() error {

	if !mysqlIdentRe(t.table) {
		return fmt.Errorf("tmysql: invalid table name %q", t.table)
	}

	loggo.Info("mysql dht Load start")

	db, err := sql.Open("mysql", t.dsn)
	if err != nil {
		loggo.Error("TMysql Open fail %v", err)
		return err
	}

	db.SetConnMaxLifetime(0)
	db.SetMaxIdleConns(t.conn)
	db.SetMaxOpenConns(t.conn)

	loggo.Info("mysql dht Load ok")

	_, err = db.Exec("CREATE DATABASE IF NOT EXISTS tmysql")
	if err != nil {
		loggo.Error("TMysql CREATE DATABASE fail %v", err)
		db.Close()
		return err
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS tmysql." + t.table + "(" +
		"name VARCHAR(1000) NOT NULL," +
		"value VARCHAR(1000) NOT NULL," +
		"time DATETIME NOT NULL," +
		"PRIMARY KEY(name));")
	if err != nil {
		loggo.Error("TMysql CREATE TABLE fail %v", err)
		db.Close()
		return err
	}

	// Only publish the handle once every schema statement succeeded, so a
	// failed Load leaves the value unusable instead of half-initialized.
	t.gdb = db

	num := t.GetSize()
	loggo.Info("TMysql size %v", num)

	return nil
}

// Close releases the underlying connection pool.
func (t *TMysql) Close() error {
	if t.gdb == nil {
		return nil
	}
	err := t.gdb.Close()
	t.gdb = nil
	return err
}

// purge drops rows older than the retention window. Failures are logged but
// never fail the caller's insert: retention is best-effort housekeeping.
func (t *TMysql) purge() {
	if t.gdb == nil {
		return
	}
	if _, err := t.gdb.Exec("delete from tmysql."+t.table+" where (TO_DAYS(NOW()) - TO_DAYS(time)) >= ?", t.day); err != nil {
		loggo.Error("TMysql purge fail %v", err)
	}
}

func (t *TMysql) Insert(key string, value string) error {

	tbl, err := t.tableRef()
	if err != nil {
		return err
	}

	tx, err := t.gdb.Begin()
	if err != nil {
		loggo.Error("TMysql Begin fail %v", err)
		return err
	}
	// Rolls back unless the commit below succeeds; a bare Commit error would
	// otherwise leave the transaction and its connection dangling.
	defer tx.Rollback()

	stmt, err := tx.Prepare("insert IGNORE into " + tbl + "(name, value, time) values(?, ?, NOW())")
	if err != nil {
		loggo.Error("TMysql Prepare fail %v", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(key, value)
	if err != nil {
		loggo.Error("TMysql insert fail %v", err)
		return err
	}
	err = tx.Commit()
	if err != nil {
		loggo.Error("TMysql Commit fail %v", err)
		return err
	}

	t.purge()

	num := t.GetSize()

	loggo.Info("TMysql InsertSpider ok %v %v %v %v", t.table, key, value, num)

	return nil
}

func (t *TMysql) GetSize() int {

	tbl, err := t.tableRef()
	if err != nil {
		loggo.Error("TMysql GetSize fail %v", err)
		return 0
	}

	rows, err := t.gdb.Query("select count(*) from " + tbl)
	if err != nil {
		loggo.Error("TMysql Query fail %v", err)
		return 0
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			loggo.Error("TMysql Scan fail %v", err)
		}
		return 0
	}

	var num int
	if err := rows.Scan(&num); err != nil {
		loggo.Error("TMysql Scan fail %v", err)
		return 0
	}

	return num
}

func (t *TMysql) Has(key string) bool {

	tbl, err := t.tableRef()
	if err != nil {
		loggo.Error("TMysql Has fail %v", err)
		return false
	}

	// Parameterized: key is caller-supplied and must never reach the SQL text.
	rows, err := t.gdb.Query("select name, value from "+tbl+" where name = ? limit 1", key)
	if err != nil {
		loggo.Error("TMysql Query fail %v", err)
		return false
	}
	defer rows.Close()

	for rows.Next() {
		return true
	}
	if err := rows.Err(); err != nil {
		loggo.Error("TMysql Has rows fail %v", err)
	}

	return false
}

type TMysqlFindData struct {
	Name  string
	Value string
}

// scanFind drains a two-column result set into TMysqlFindData values.
func scanFind(rows *sql.Rows) ([]TMysqlFindData, error) {
	var ret []TMysqlFindData
	for rows.Next() {
		var d TMysqlFindData
		if err := rows.Scan(&d.Name, &d.Value); err != nil {
			return ret, err
		}
		ret = append(ret, d)
	}
	return ret, rows.Err()
}

func (t *TMysql) Last(n int) []TMysqlFindData {

	tbl, err := t.tableRef()
	if err != nil {
		loggo.Error("TMysql Last fail %v", err)
		return nil
	}

	rows, err := t.gdb.Query("select name, value from "+tbl+" order by time desc limit ?", limit(n))
	if err != nil {
		loggo.Error("TMysql Query fail %v", err)
		return nil
	}
	defer rows.Close()

	ret, err := scanFind(rows)
	if err != nil {
		loggo.Error("TMysql Scan fail %v", err)
	}

	return ret
}

func (t *TMysql) FindValue(str string, max int) []TMysqlFindData {

	tbl, err := t.tableRef()
	if err != nil {
		loggo.Error("TMysql FindValue fail %v", err)
		return nil
	}

	// The pattern is bound as a parameter; the surrounding % wildcards stay
	// in the SQL text so the LIKE semantics are unchanged.
	rows, err := t.gdb.Query("select name, value from "+tbl+" where value like ? limit ?", "%"+str+"%", limit(max))
	if err != nil {
		loggo.Error("TMysql Query fail %v", err)
		return nil
	}
	defer rows.Close()

	ret, err := scanFind(rows)
	if err != nil {
		loggo.Error("TMysql Scan fail %v", err)
	}

	return ret
}
