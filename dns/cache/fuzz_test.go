package cache

import (
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// fuzzCapacity 把 fuzz 给的容量夹到合法范围（构造器会把 <=0 夹成 2000）。
func fuzzCapacity(capacity uint16) int {
	n := int(capacity)
	if n < 1 {
		n = 1
	}
	if n > 64 {
		n = 64
	}
	return n
}

// newAnswerMsg 构造一条带 Answer 的消息。Set 对 msg==nil 或 Answer 为空会直接
// 忽略（见 cache.go），因此缓存测试必须用真实应答。
func newAnswerMsg(t *testing.T) *dns.Msg {
	t.Helper()
	rr, err := dns.NewRR("example.com. 60 IN A 1.2.3.4")
	if err != nil {
		t.Fatalf("dns.NewRR: %v", err)
	}
	msg := new(dns.Msg)
	msg.Answer = []dns.RR{rr}
	return msg
}

func asciiUpper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 'a' - 'A'
		}
	}
	return string(b)
}

// FuzzKeyForMsg 检查缓存 key 的归一化契约：确定性、大小写不敏感、尾点不影响。
// 查询名来自外部报文，任一性质被破坏都会造成同名查询重复回源。
func FuzzKeyForMsg(f *testing.F) {
	f.Add("example.com", uint16(dns.TypeA), uint16(dns.ClassINET))
	f.Add("", uint16(dns.TypeAAAA), uint16(dns.ClassINET))
	f.Add(".", uint16(0), uint16(0))
	f.Add("WWW.Example.COM.", uint16(dns.TypeMX), uint16(dns.ClassINET))
	f.Add("a..b", uint16(dns.TypeTXT), uint16(dns.ClassINET))

	f.Fuzz(func(t *testing.T, qName string, qType uint16, qClass uint16) {
		key := KeyForMsg(qName, qType, qClass)

		if again := KeyForMsg(qName, qType, qClass); again != key {
			t.Fatalf("KeyForMsg is not deterministic: %q vs %q", key, again)
		}
		if upper := KeyForMsg(asciiUpper(qName), qType, qClass); upper != key {
			t.Fatalf("KeyForMsg must be case-insensitive: %q vs %q", key, upper)
		}
		if dotted := KeyForMsg(qName+".", qType, qClass); dotted != key {
			t.Fatalf("trailing dot must not change the key: %q vs %q", key, dotted)
		}

		// 类型与类别必须参与 key，否则 A 与 AAAA 记录会互相串味
		if qType != ^uint16(0) {
			if other := KeyForMsg(qName, qType+1, qClass); qType+1 != qType && other == key {
				t.Fatalf("qType must be part of the key: %q", key)
			}
		}
		if qClass != ^uint16(0) {
			if other := KeyForMsg(qName, qType, qClass+1); qClass+1 != qClass && other == key {
				t.Fatalf("qClass must be part of the key: %q", key)
			}
		}
	})
}

// FuzzDNSCache 检查 DNS 缓存的不变量：写入即可读、条目数不超过容量、
// 命中/未命中计数与实际情况一致。
func FuzzDNSCache(f *testing.F) {
	f.Add(uint16(2), "example.com", uint16(dns.TypeA), uint16(dns.ClassINET))
	f.Add(uint16(0), "", uint16(dns.TypeAAAA), uint16(dns.ClassINET))
	f.Add(uint16(1), "a..b", uint16(0), uint16(0))

	f.Fuzz(func(t *testing.T, capacity uint16, qName string, qType uint16, qClass uint16) {
		cap := fuzzCapacity(capacity)
		c := NewDNSCache(cap)

		key := KeyForMsg(qName, qType, qClass)
		msg := newAnswerMsg(t)

		hits, misses := c.Hits(), c.Misses()

		c.Set(key, msg)

		// 1. 写入后必须能读到（TTL 至少 5s，不会瞬时过期）
		got, ok := c.Get(key)
		if !ok {
			t.Fatalf("cap=%d: Get(%q) missed right after Set", cap, key)
		}
		if got == nil {
			t.Fatalf("cap=%d: Get(%q) returned a nil msg", cap, key)
		}
		if len(got.Answer) != len(msg.Answer) {
			t.Fatalf("cap=%d: Get returned %d answers, want %d", cap, len(got.Answer), len(msg.Answer))
		}

		// 2. 命中计数必须增加，未命中计数不变
		if c.Hits() != hits+1 {
			t.Fatalf("cap=%d: Hits()=%d, want %d", cap, c.Hits(), hits+1)
		}
		if c.Misses() != misses {
			t.Fatalf("cap=%d: Misses()=%d, want %d", cap, c.Misses(), misses)
		}

		// 3. 不存在的 key 必须未命中并计入 miss
		absent := key + "-absent"
		if _, ok := c.Get(absent); ok {
			t.Fatalf("cap=%d: Get(%q) must miss", cap, absent)
		}
		if c.Misses() != misses+1 {
			t.Fatalf("cap=%d: Misses()=%d, want %d", cap, c.Misses(), misses+1)
		}

		// 4. 条目数不得超过容量
		if c.Len() > cap {
			t.Fatalf("Len()=%d exceeds capacity %d", c.Len(), cap)
		}

		// 5. nil 与空应答不得入库（Set 会直接忽略，避免缓存毒化）
		before := c.Len()
		c.Set("nil-msg", nil)
		c.Set("empty-msg", new(dns.Msg))
		if c.Len() != before {
			t.Fatalf("Set(nil/empty) must not add entries: %d -> %d", before, c.Len())
		}

		// 6. 满容写入后条目数仍受控
		for i := 0; i < cap*2+4; i++ {
			c.Set(KeyForMsg("bulk"+strings.Repeat("x", i%8), uint16(i%65536), dns.ClassINET), msg)
			if c.Len() > cap {
				t.Fatalf("Len()=%d exceeds capacity %d during bulk insert", c.Len(), cap)
			}
		}

		c.Clear()
		if c.Len() != 0 {
			t.Fatalf("Len()=%d after Clear(), want 0", c.Len())
		}
	})
}
