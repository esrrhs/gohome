package matcher

import (
	"net"
	"strings"
	"testing"
)

func TestDomainTrie(t *testing.T) {
	trie := NewDomainTrie()
	trie.Add("google.com")
	trie.Add("baidu.com")
	trie.Add(".cn")

	if !trie.Has("google.com") {
		t.Fatalf("expected google.com true")
	}
	if !trie.Has("mail.google.com") {
		t.Fatalf("expected mail.google.com true")
	}
	if !trie.Has("a.b.c.baidu.com") {
		t.Fatalf("expected a.b.c.baidu.com true")
	}
	if !trie.Has("taobao.cn") {
		t.Fatalf("expected taobao.cn true")
	}
	if trie.Has("example.org") {
		t.Fatalf("expected example.org false")
	}

	// 测试 dnsmasq 格式解析
	rules := `
# some comments
server=/qq.com/114.114.114.114
ipset=/jd.com/china
zhihu.com
`
	n, err := trie.LoadFromReader(strings.NewReader(rules))
	if err != nil || n != 3 {
		t.Fatalf("LoadFromReader failed: n=%d err=%v", n, err)
	}
	if !trie.Has("v.qq.com") || !trie.Has("item.jd.com") || !trie.Has("zhihu.com") {
		t.Fatalf("expected loaded domains to match")
	}
}

func TestIPNetList(t *testing.T) {
	list := NewIPNetList()
	list.Reset([]string{"192.168.0.0/16", "10.0.0.0/8", "1.1.1.1"})

	if !list.Contains(net.ParseIP("192.168.1.1")) {
		t.Fatalf("expected 192.168.1.1 in list")
	}
	if !list.Contains(net.ParseIP("10.254.1.2")) {
		t.Fatalf("expected 10.254.1.2 in list")
	}
	if !list.Contains(net.ParseIP("1.1.1.1")) {
		t.Fatalf("expected 1.1.1.1 in list")
	}
	if list.Contains(net.ParseIP("8.8.8.8")) {
		t.Fatalf("expected 8.8.8.8 not in list")
	}
}
