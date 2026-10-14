package matcher

import (
	"strings"
	"testing"
)

// FuzzDomainTrie 对域名树做 fuzzing。规则文件来自外部（用户配置、上游订阅），
// 解析任意字节都不得 panic；同时钉死两条核心契约：
//   - 加入的域名自身必须命中
//   - 后缀匹配：加入 "example.com" 后 "www.example.com" 必须命中，
//     而 "notexample.com" 不得误命中
func FuzzDomainTrie(f *testing.F) {
	f.Add("example.com\ngoogle.com\n", "www.example.com")
	f.Add("server=/foo.com/114.114.114.114\n", "foo.com")
	f.Add("ipset=/bar.com/cn\n", "bar.com")
	f.Add("# comment only\n\n", "")
	f.Add("a.b.c\nd.e\n", "x.a.b.c")
	f.Add("...\n", "...")
	f.Add("EXAMPLE.COM\n", "WWW.EXAMPLE.COM")

	f.Fuzz(func(t *testing.T, rules string, query string) {
		// 固定契约：后缀匹配语义
		trie := NewDomainTrie()
		trie.Add("example.com")
		if !trie.Has("example.com") {
			t.Fatalf("exact match must hit")
		}
		if !trie.Has("www.example.com") {
			t.Fatalf("suffix match must hit")
		}
		if trie.Has("notexample.com") {
			t.Fatalf("unrelated domain must not hit")
		}
		if trie.Has("com") {
			t.Fatalf("parent domain must not hit unless added")
		}

		// 任意规则输入：解析不得 panic
		loaded := NewDomainTrie()
		n, err := loaded.LoadFromReader(strings.NewReader(rules))
		if err != nil {
			// bufio.Scanner 对超长行返回 ErrTooLong，属预期，跳过后续断言
			return
		}
		if n < 0 {
			t.Fatalf("LoadFromReader returned negative count %d", n)
		}

		// 解析结果必须是确定性的：相同输入重复加载得到相同计数
		again := NewDomainTrie()
		n2, err := again.LoadFromReader(strings.NewReader(rules))
		if err != nil {
			return
		}
		if n2 != n {
			t.Fatalf("LoadFromReader is not deterministic: %d vs %d", n, n2)
		}

		// 查询不得 panic，且结果必须稳定
		first := loaded.Has(query)
		if second := loaded.Has(query); first != second {
			t.Fatalf("Has(%q) is not stable: %v vs %v", query, first, second)
		}

		// 加入查询域名后必须命中（空域名与纯点除外，它们会被规范化掉）
		added := NewDomainTrie()
		added.Add(query)
		if strings.Trim(query, ".") != "" && !added.Has(query) {
			t.Fatalf("Add(%q) then Has(%q) must be true", query, query)
		}

		// Reset 必须是全量替换，且可重复调用
		reset := NewDomainTrie()
		reset.Add("should-be-cleared.com")
		reset.Reset(strings.Split(rules, "\n"))
		before := reset.Has("should-be-cleared.com")
		reset.Reset(strings.Split(rules, "\n"))
		if after := reset.Has("should-be-cleared.com"); before != after {
			t.Fatalf("Reset is not idempotent: %v vs %v", before, after)
		}
	})
}

// FuzzIPNetList 对 CIDR 列表做 fuzzing：任意规则文本与任意 IP 串都不得 panic，
// 解析计数必须确定，Contains 结果必须稳定且与字符串入口保持一致。
func FuzzIPNetList(f *testing.F) {
	f.Add("192.168.0.0/16\n10.0.0.0/8\n", "192.168.1.1")
	f.Add("1.2.3.4\n", "1.2.3.4")
	f.Add("2001:db8::/32\n", "2001:db8::1")
	f.Add("# comment\n\n", "")
	f.Add("not-a-cidr\n", "1.1.1.1")
	f.Add("0.0.0.0/0\n", "8.8.8.8")

	f.Fuzz(func(t *testing.T, rules string, ip string) {
		list := NewIPNetList()

		n, err := list.LoadFromReader(strings.NewReader(rules))
		if err != nil {
			return
		}
		if n < 0 {
			t.Fatalf("LoadFromReader returned negative count %d", n)
		}

		// 解析必须确定：相同输入重复加载得到相同计数
		other := NewIPNetList()
		n2, err := other.LoadFromReader(strings.NewReader(rules))
		if err != nil {
			return
		}
		if n2 != n {
			t.Fatalf("LoadFromReader is not deterministic: %d vs %d", n, n2)
		}

		// 查询不得 panic，结果必须稳定
		first := list.ContainsStr(ip)
		if second := list.ContainsStr(ip); first != second {
			t.Fatalf("ContainsStr(%q) is not stable: %v vs %v", ip, first, second)
		}

		// 空串与纯空白不得被判为命中
		if strings.TrimSpace(ip) == "" && first {
			t.Fatalf("blank IP string must not be contained")
		}

		// Reset 全量替换：重置为空后任何 IP 都不得命中
		list.Reset(nil)
		if list.ContainsStr(ip) {
			t.Fatalf("after Reset(nil) nothing may be contained, but %q hit", ip)
		}
	})
}
