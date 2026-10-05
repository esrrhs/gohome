package matcher

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestDomainTrieEdgeCases(t *testing.T) {
	trie := NewDomainTrie()

	// 空输入不应产生任何节点，也不应把根标记为终点
	trie.Add("")
	trie.Add(".")
	trie.Add("   ")
	if trie.Has("") || trie.Has("example.com") {
		t.Fatalf("empty domains must not create matches")
	}

	trie.Add("Example.COM.")
	if !trie.Has("mail.example.com") {
		t.Fatalf("Add must normalize case and trailing dots")
	}

	// 父域不应被子域规则反向命中
	trie.Reset([]string{"mail.google.com"})
	if trie.Has("google.com") {
		t.Fatalf("a subdomain rule must not match its parent")
	}
	if !trie.Has("mail.google.com") {
		t.Fatalf("expected mail.google.com to match")
	}

	// Reset 之后旧规则必须全部失效
	trie.Add("old.example")
	trie.Reset([]string{"new.example"})
	if trie.Has("old.example") {
		t.Fatalf("Reset must drop previous domains")
	}
	if !trie.Has("new.example") {
		t.Fatalf("expected new.example to match")
	}
	if trie.Has("") {
		t.Fatalf("empty query must never match")
	}
}

func TestDomainTrieLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.txt")
	content := "# comment\n\nserver=/a.example/1.1.1.1\n  b.example  # trailing comment\nc.example\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	trie := NewDomainTrie()
	n, err := trie.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 domains, got %d", n)
	}
	for _, d := range []string{"a.example", "b.example", "c.example"} {
		if !trie.Has(d) {
			t.Fatalf("expected %s to match", d)
		}
	}

	if _, err := trie.LoadFromFile(filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatalf("expected an error for a missing file")
	}
	if _, err := trie.LoadFromReader(errReader{}); err == nil {
		t.Fatalf("expected a reader error to be propagated")
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errBoom }

var errBoom = &os.PathError{Op: "read", Err: os.ErrInvalid}

func TestIPNetListEdgeCases(t *testing.T) {
	list := NewIPNetList()

	// 空列表不应命中任何地址
	if list.Contains(net.ParseIP("1.2.3.4")) || list.Contains(nil) {
		t.Fatalf("an empty list must not match anything")
	}

	// 非法 CIDR 被跳过，返回值只统计成功解析的条目
	n := list.Reset([]string{"1.2.3.4", "2001:db8::1", "not-a-cidr", "", "  ", "10.0.0.0/8"})
	if n != 3 {
		t.Fatalf("expected 3 valid nets, got %d", n)
	}
	// 单个 IPv4 应自动补 /32
	if !list.Contains(net.ParseIP("1.2.3.4")) {
		t.Fatalf("expected bare IPv4 to be expanded to /32")
	}
	if list.Contains(net.ParseIP("1.2.3.5")) {
		t.Fatalf("a /32 must not match a neighbouring address")
	}
	// 单个 IPv6 应自动补 /128
	if !list.Contains(net.ParseIP("2001:db8::1")) {
		t.Fatalf("expected bare IPv6 to be expanded to /128")
	}
	if list.ContainsStr(" 10.1.2.3 ") != true {
		t.Fatalf("ContainsStr must trim its input")
	}
	if list.ContainsStr("garbage") {
		t.Fatalf("garbage input must not match")
	}

	// Reset 会整体替换而不是追加
	list.Reset([]string{"172.16.0.0/12"})
	if list.Contains(net.ParseIP("10.0.0.1")) {
		t.Fatalf("Reset must replace the previous list")
	}
}

func TestIPNetListLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cidr.txt")
	if err := os.WriteFile(path, []byte("# comment\n192.168.0.0/16\n\n203.0.113.0/24 extra column\n"), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	list := NewIPNetList()
	n, err := list.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 nets, got %d", n)
	}
	if !list.ContainsStr("192.168.1.1") || !list.ContainsStr("203.0.113.5") {
		t.Fatalf("expected loaded nets to match")
	}

	if _, err := list.LoadFromFile(filepath.Join(dir, "missing.txt")); err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}

func TestGeoDBWithoutDatabase(t *testing.T) {
	db := NewGeoDB()

	if _, err := db.GetCountryCode(nil); err == nil {
		t.Fatalf("nil IP must return an error")
	}
	// 未加载数据库时必须返回错误而不是 panic
	if _, err := db.GetCountryCode(net.ParseIP("1.1.1.1")); err == nil {
		t.Fatalf("expected an error when the database is not loaded")
	}
	if _, err := db.GetCountryCodeStr("not-an-ip"); err == nil {
		t.Fatalf("invalid IP string must return an error")
	}
	if db.IsCountry(net.ParseIP("1.1.1.1"), "CN") {
		t.Fatalf("IsCountry must be false without a database")
	}
	if err := db.Open(filepath.Join(t.TempDir(), "missing.mmdb")); err == nil {
		t.Fatalf("expected Open to fail on a missing file")
	}
	// 未加载时 Close 应当是安全的空操作
	if err := db.Close(); err != nil {
		t.Fatalf("Close on an unloaded db failed: %v", err)
	}
	// 加载失败后 db 仍应处于未加载状态，不能残留半初始化句柄
	if _, err := db.GetCountryCode(net.ParseIP("1.1.1.1")); err == nil {
		t.Fatalf("expected the db to stay unloaded after a failed Open")
	}
}

// TestGeoDBConcurrentAccess 保证热替换 GeoIP 时读写不冲突
func TestGeoDBConcurrentAccess(t *testing.T) {
	db := NewGeoDB()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_, _ = db.GetCountryCode(net.ParseIP("1.1.1.1"))
			_, _ = db.GetCountryCodeStr("8.8.8.8")
		}
	}()
	for i := 0; i < 20; i++ {
		// 文件不存在，Open 会失败；重点是并发路径不 panic / 不死锁
		_ = db.Open(filepath.Join(t.TempDir(), "missing.mmdb"))
	}
	<-done
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}
