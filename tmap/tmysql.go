package tmap

import (
	"database/sql"
	"github.com/esrrhs/gohome/loggo"
	"strconv"
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

func NewTMysql(dsn string, conn int, table string, day int) *TMysql {
	return &TMysql{dsn: dsn, conn: conn, table: table, day: day}
}

func (t *TMysql) Load() error {

	loggo.Info("mysql dht Load start")

	db, err := sql.Open("mysql", t.dsn)
	if err != nil {
		loggo.Error("TMysql Open fail %v", err)
		return err
	}
	t.gdb = db

	t.gdb.SetConnMaxLifetime(0)
	t.gdb.SetMaxIdleConns(t.conn)
	t.gdb.SetMaxOpenConns(t.conn)

	loggo.Info("mysql dht Load ok")

	_, err = t.gdb.Exec("CREATE DATABASE IF NOT EXISTS tmysql")
	if err != nil {
		loggo.Error("TMysql CREATE DATABASE fail %v", err)
		return err
	}

	_, err = t.gdb.Exec("CREATE TABLE IF NOT EXISTS tmysql." + t.table + "(" +
		"name VARCHAR(1000) NOT NULL," +
		"value VARCHAR(1000) NOT NULL," +
		"time DATETIME NOT NULL," +
		"PRIMARY KEY(name));")
	if err != nil {
		loggo.Error("TMysql CREATE TABLE fail %v", err)
		return err
	}

	num := t.GetSize()
	loggo.Info("TMysql size %v", num)

	return nil
}

func (t *TMysql) Insert(key string, value string) error {

	tx, err := t.gdb.Begin()
	if err != nil {
		loggo.Error("TMysql Begin fail %v", err)
		return err
	}
	stmt, err := tx.Prepare("insert IGNORE into tmysql." + t.table + "(name, value, time) values(?, ?, NOW())")
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

	t.gdb.Exec("delete from tmysql." + t.table + " where (TO_DAYS(NOW()) - TO_DAYS(time)) >= " + strconv.Itoa(t.day))

	num := t.GetSize()

	loggo.Info("TMysql InsertSpider ok %v %v %v %v", t.table, key, value, num)

	return nil
}

func (t *TMysql) GetSize() int {

	rows, err := t.gdb.Query("select count(*) from tmysql." + t.table)
	if err != nil {
		loggo.Error("TMysql Query fail %v", err)
		return 0
	}
	defer rows.Close()

	rows.Next()

	var num int
	err = rows.Scan(&num)
	if err != nil {
		loggo.Error("TMysql Scan fail %v", err)
		return 0
	}

	return num
}

func (t *TMysql) Has(key string) bool {

	rows, err := t.gdb.Query("select name, value from tmysql." + t.table + " where name='" + key + "'")
	if err != nil {
		loggo.Error("TMysql Query fail %v", err)
		return false
	}
	defer rows.Close()

	for rows.Next() {
		return true
	}

	return false
}

type TMysqlFindData struct {
	Name  string
	Value string
}

func (t *TMysql) Last(n int) []TMysqlFindData {
	var ret []TMysqlFindData

	rows, err := t.gdb.Query("select name, value from tmysql." + t.table + " order by time desc limit 0," + strconv.Itoa(n))
	if err != nil {
		loggo.Error("TMysql Query fail %v", err)
		return ret
	}
	defer rows.Close()

	for rows.Next() {

		var name string
		var value string
		err = rows.Scan(&name, &value)
		if err != nil {
			loggo.Error("TMysql Scan fail %v", err)
		}

		ret = append(ret, TMysqlFindData{name, value})
	}

	return ret
}

func (t *TMysql) FindValue(str string, max int) []TMysqlFindData {
	var ret []TMysqlFindData

	rows, err := t.gdb.Query("select name, value from tmysql." + t.table + " where value like '%" + str + "%' limit 0," + strconv.Itoa(max))
	if err != nil {
		loggo.Error("TMysql Query fail %v", err)
		return ret
	}
	defer rows.Close()

	for rows.Next() {

		var name string
		var value string
		err = rows.Scan(&name, &value)
		if err != nil {
			loggo.Error("Scan sqlite3 fail %v", err)
		}

		ret = append(ret, TMysqlFindData{name, value})
	}

	return ret
}
