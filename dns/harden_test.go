package dns

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/esrrhs/gohome/dns/upstream"
	"github.com/miekg/dns"
)

// ===========================================================================
// 最小 GeoLite2-Country mmdb 生成器（dns 包内测试专用，零外部依赖）。
// 与 dns/matcher、thirdparty 测试中的生成器同源：record_size=24 的 IPv4-only
// 库，记录为 {"country":{"iso_code":code}}，足以驱动 resolver 的 GeoIP 分流。
// ===========================================================================

const (
	hrTypeString = 2
	hrTypeUint16 = 5
	hrTypeUint32 = 6
	hrTypeMap    = 7
	hrTypeUint64 = 9
	hrTypeArray  = 11
)

func hrCtrl(t byte, size int) byte { return byte(t)<<5 | byte(size) }

func hrString(s string) []byte {
	return append([]byte{hrCtrl(hrTypeString, len(s))}, s...)
}

func hrUint(t byte, v uint64) []byte {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], v)
	i := 0
	for i < 7 && raw[i] == 0 {
		i++
	}
	payload := raw[i:]
	if t == hrTypeUint64 {
		return append([]byte{hrCtrl(0, len(payload)), hrTypeUint64 - 7}, payload...)
	}
	return append([]byte{hrCtrl(t, len(payload))}, payload...)
}

func hrMapHeader(n int) []byte { return []byte{hrCtrl(hrTypeMap, n)} }

func hrArrayHeader(n int) []byte {
	return []byte{hrCtrl(0, n), hrTypeArray - 7}
}

func hrCountryRecord(code string) []byte {
	var b []byte
	b = append(b, hrMapHeader(1)...)
	b = append(b, hrString("country")...)
	b = append(b, hrMapHeader(1)...)
	b = append(b, hrString("iso_code")...)
	b = append(b, hrString(code)...)
	return b
}

type hrTreeNode struct{ rec [2]uint32 }

// 数据记录必须挂在最后一个前缀 bit 对应的记录槽上，不能再下钻新建节点，
// 否则 /32 会在 bit 耗尽后停在普通节点触发 invalid node 错误。
func hrInsert(nodes []*hrTreeNode, bits []bool, data uint32) []*hrTreeNode {
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
			nodes = append(nodes, &hrTreeNode{})
		}
		cur = next
	}
	lastSide := uint32(0)
	if bits[len(bits)-1] {
		lastSide = 1
	}
	nodes[cur].rec[lastSide] = data
	return nodes
}

func hrBitsPrefix(ipv4 [4]byte, prefixLen int) []bool {
	bits := make([]bool, 0, prefixLen)
	for i := 0; i < prefixLen; i++ {
		bits = append(bits, ipv4[i>>3]&(0x80>>uint(i%8)) != 0)
	}
	return bits
}

func hrWriteCountryDB(t *testing.T, path string, records map[string]string) {
	t.Helper()

	type recInfo struct {
		bits []bool
		off  uint32
	}
	var infos []recInfo
	var dataSection []byte
	for cidr, code := range records {
		ip, ipnet, err := net.ParseCIDR(cidr)
		if err != nil || ip.To4() == nil {
			t.Fatalf("bad ipv4 cidr %q: %v", cidr, err)
		}
		var a [4]byte
		copy(a[:], ip.To4())
		pfx, _ := ipnet.Mask.Size()
		off := uint32(len(dataSection))
		dataSection = append(dataSection, hrCountryRecord(code)...)
		infos = append(infos, recInfo{bits: hrBitsPrefix(a, pfx), off: off})
	}

	const placeholderBase = uint32(0x40000000)
	nodes := []*hrTreeNode{{}}
	for i, ri := range infos {
		nodes = hrInsert(nodes, ri.bits, placeholderBase+uint32(i))
	}
	nodeCount := uint32(len(nodes))
	for _, n := range nodes {
		for s := 0; s < 2; s++ {
			switch {
			case n.rec[s] == 0:
				n.rec[s] = nodeCount
			case n.rec[s] >= placeholderBase:
				idx := n.rec[s] - placeholderBase
				n.rec[s] = nodeCount + 16 + infos[idx].off
			}
		}
	}

	tree := make([]byte, int(nodeCount)*6)
	for i, n := range nodes {
		base := i * 6
		tree[base+0] = byte(n.rec[0] >> 16)
		tree[base+1] = byte(n.rec[0] >> 8)
		tree[base+2] = byte(n.rec[0])
		tree[base+3] = byte(n.rec[1] >> 16)
		tree[base+4] = byte(n.rec[1] >> 8)
		tree[base+5] = byte(n.rec[1])
	}

	var meta []byte
	meta = append(meta, hrMapHeader(9)...)
	meta = append(meta, hrString("binary_format_major_version")...)
	meta = append(meta, hrUint(hrTypeUint16, 2)...)
	meta = append(meta, hrString("binary_format_minor_version")...)
	meta = append(meta, hrUint(hrTypeUint16, 0)...)
	meta = append(meta, hrString("build_epoch")...)
	meta = append(meta, hrUint(hrTypeUint64, 0)...)
	meta = append(meta, hrString("database_type")...)
	meta = append(meta, hrString("GeoLite2-Country")...)
	meta = append(meta, hrString("description")...)
	meta = append(meta, hrMapHeader(1)...)
	meta = append(meta, hrString("en")...)
	meta = append(meta, hrString("harden resolver db")...)
	meta = append(meta, hrString("ip_version")...)
	meta = append(meta, hrUint(hrTypeUint16, 4)...)
	meta = append(meta, hrString("languages")...)
	meta = append(meta, hrArrayHeader(1)...)
	meta = append(meta, hrString("en")...)
	meta = append(meta, hrString("node_count")...)
	meta = append(meta, hrUint(hrTypeUint32, uint64(nodeCount))...)
	meta = append(meta, hrString("record_size")...)
	meta = append(meta, hrUint(hrTypeUint16, 24)...)

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

func hrCountryDBPath(t *testing.T, records map[string]string) string {
	p := filepath.Join(t.TempDir(), "country.mmdb")
	hrWriteCountryDB(t, p, records)
	return p
}

// ===========================================================================
// ResolveAll / ResolveOne 残余分支
// ===========================================================================

// A 与 AAAA 都返回合法但无 Answer 的响应：ResolveAll 必须报"无 IP"，
// 且不能误带 aaaa 后缀（AAAA 本身没失败）。
func TestHardenResolveAll_NoAnswerFromBoth(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, func(req *dns.Msg) *dns.Msg {
		resp := new(dns.Msg)
		resp.SetReply(req)
		return resp
	})
	defer stop()
	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"}, func(c *Config) {
		// 直连白名单保证空应答原样返回，不进竞速丢弃逻辑
		c.DirectDomains = []string{"empty-both.test"}
	})

	_, err := r.ResolveAll(context.Background(), "empty-both.test")
	if err == nil {
		t.Fatal("expected no-IP error when both queries return empty answers")
	}
	if strings.Contains(err.Error(), "aaaa") {
		t.Fatalf("error must not blame aaaa on a successful empty reply: %v", err)
	}
	if !strings.Contains(err.Error(), "no IP answer") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A 查询的应答里只有 AAAA 记录（异常上游）：ResolveOne 无 IPv4 可选时
// 必须回退返回首个（v6）地址，而不是越界或空报。
func TestHardenResolveOne_V6OnlyAnswer(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, func(req *dns.Msg) *dns.Msg {
		resp := new(dns.Msg)
		resp.SetReply(req)
		if req.Question[0].Qtype == dns.TypeA {
			if rr, err := dns.NewRR(req.Question[0].Name + " 60 IN AAAA 2001:db8::9"); err == nil {
				resp.Answer = append(resp.Answer, rr)
			}
		}
		return resp
	})
	defer stop()
	r := newStubResolver(t, []string{addr}, []string{"127.0.0.1:1"}, func(c *Config) {
		c.DirectDomains = []string{"v6only.test"}
	})

	ip, err := r.ResolveOne(context.Background(), "v6only.test")
	if err != nil {
		t.Fatalf("ResolveOne v6-only failed: %v", err)
	}
	if ip == nil || ip.To4() != nil || ip.String() != "2001:db8::9" {
		t.Fatalf("expected the v6 address as fallback, got %v", ip)
	}
}

// ===========================================================================
// Exchange 空响应分支
// ===========================================================================

// hrEmptyUpstream 模拟"既无应答也无错误"的病态上游（旧实现会把 nil
// 塞进 fmt.Errorf("%w", nil)，对外吐出 "%!w(<nil>)" 垃圾错误串）。
type hrEmptyUpstream struct{}

func (hrEmptyUpstream) Address() string { return "stub-empty" }

func (hrEmptyUpstream) Exchange(context.Context, *dns.Msg) (*dns.Msg, error) {
	return nil, nil
}

// 白盒构造"两侧上游均为空"的 resolver（正常构造会被 initUpstreams 拒绝）：
// 竞速退化为纯直连时 queryDirect 必须给出明确错误，而不是 %w(nil) 垃圾串。
func TestHardenExchange_NoUpstreamsConfigured(t *testing.T) {
	r := newStubResolver(t, []string{"127.0.0.1:1"}, []string{"127.0.0.1:2"})
	r.mu.Lock()
	r.directUpstreams = nil
	r.remoteUpstreams = nil
	r.mu.Unlock()

	req := upstream.BuildQuery("nobody.home.test", dns.TypeA)
	resp, err := r.Exchange(context.Background(), req)
	if err == nil {
		t.Fatal("expected explicit error without any upstream, got nil error")
	}
	if resp != nil {
		t.Fatalf("nil response must not be returned as success: %v", resp)
	}
	if !strings.Contains(err.Error(), "no direct upstreams configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 病态上游返回 (nil,nil)：queryDirect/queryRemote 都必须返回可读错误，
// 不能让 "%!w(<nil>)" 漏给上层。
func TestHardenQuery_EmptyResponseUpstream(t *testing.T) {
	r := newStubResolver(t, []string{"127.0.0.1:1"}, []string{"127.0.0.1:2"})
	req := upstream.BuildQuery("empty-upstream.test", dns.TypeA)

	r.mu.Lock()
	r.directUpstreams = []upstream.Upstream{hrEmptyUpstream{}}
	r.mu.Unlock()
	if _, err := r.queryDirect(context.Background(), req); err == nil ||
		!strings.Contains(err.Error(), "empty response") {
		t.Fatalf("queryDirect err = %v, want empty response error", err)
	}

	r.mu.Lock()
	r.remoteUpstreams = []upstream.Upstream{hrEmptyUpstream{}}
	r.mu.Unlock()
	if _, err := r.queryRemote(context.Background(), req); err == nil ||
		!strings.Contains(err.Error(), "empty response") {
		t.Fatalf("queryRemote err = %v, want empty response error", err)
	}
}

// ===========================================================================
// GeoIP 驱动的竞速境外丢弃与 ShouldProxy 判定
// ===========================================================================

func newGeoIPStubResolver(t *testing.T, records map[string]string, direct, remote []string, extra ...func(*Config)) *StandardResolver {
	t.Helper()
	dbPath := hrCountryDBPath(t, records)
	opts := append([]func(*Config){func(c *Config) { c.GeoIPFile = dbPath }}, extra...)
	return newStubResolver(t, direct, remote, opts...)
}

// 竞速：直连快速返回境外公网 IP（GeoIP 库判为 US，RegionCode=CN），
// 必须丢弃直连结果（isAllDomestic=false），最终采纳远程应答。
func TestHardenParallel_ForeignDirectDiscarded(t *testing.T) {
	directAddr, _, stopDirect := startUDPHandlerStub(t, replyA("203.0.113.50"))
	defer stopDirect()
	remoteAddr, _, stopRemote := startUDPHandlerStub(t, func(req *dns.Msg) *dns.Msg {
		time.Sleep(100 * time.Millisecond)
		return replyA("198.51.100.50")(req)
	})
	defer stopRemote()

	r := newGeoIPStubResolver(t, map[string]string{
		"203.0.113.0/24": "US",
		"1.0.0.0/8":      "CN",
	}, []string{directAddr}, []string{remoteAddr})

	ips, err := r.Resolve(context.Background(), "foreign-direct.test")
	if err != nil {
		t.Fatalf("expected remote fallback after discarding foreign direct: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "198.51.100.50" {
		t.Fatalf("expected the remote answer, got %v", ips)
	}
	got := mustRoutedDomain(t, r, "foreign-direct.test")
	if !got.Proxy || got.Reason != RouteReasonParallelRemote {
		t.Fatalf("discarded direct must end as parallel_remote, got %+v", got)
	}
}

// 白盒直测 isAllDomestic 的境外返回 false 分支（竞速中已间接覆盖，
// 这里显式断言私有/CN/US/未知四类）。
func TestHardenIsAllDomestic_GeoIPClasses(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, replyA("10.0.0.1"))
	defer stop()
	r := newGeoIPStubResolver(t, map[string]string{
		"203.0.113.0/24": "US",
		"1.0.0.0/8":      "CN",
	}, []string{addr}, []string{"127.0.0.1:1"})

	cases := []struct {
		ip   string
		want bool
	}{
		{"10.5.5.5", true},      // 保留网段：直接放行
		{"1.1.1.1", true},       // GeoIP 判为本区域 CN
		{"203.0.113.50", false}, // GeoIP 判为境外 US
		{"8.8.8.8", true},       // 库中无记录：查不到国家码时放行（现有语义）
	}
	for _, c := range cases {
		if got := r.isAllDomestic([]net.IP{net.ParseIP(c.ip)}); got != c.want {
			t.Errorf("isAllDomestic(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

// ShouldProxy 的 GeoIP 分支：境外 IP 必代理；本区域 IP 直连；
// 未知归属地 IP 保持默认直连；Fake-IP 仍最高优先级代理。
func TestHardenShouldProxy_GeoIP(t *testing.T) {
	addr, _, stop := startUDPHandlerStub(t, replyA("10.0.0.1"))
	defer stop()
	r := newGeoIPStubResolver(t, map[string]string{
		"203.0.113.0/24": "US",
		"1.0.0.0/8":      "CN",
	}, []string{addr}, []string{"127.0.0.1:1"})

	if proxy, err := r.ShouldProxy("203.0.113.50"); err != nil || !proxy {
		t.Fatalf("US IP must proxy, got (%v,%v)", proxy, err)
	}
	if proxy, err := r.ShouldProxy("1.1.1.1"); err != nil || proxy {
		t.Fatalf("CN IP must be direct, got (%v,%v)", proxy, err)
	}
	if proxy, err := r.ShouldProxy("8.8.8.8"); err != nil || proxy {
		t.Fatalf("unknown-geolocation IP must stay direct, got (%v,%v)", proxy, err)
	}
}

// ===========================================================================
// Server 构造残余
// ===========================================================================

func TestHardenNewServer_DefaultAddr(t *testing.T) {
	if s := NewServer("", nil); s.Addr() != ":53" {
		t.Fatalf("empty addr must default to :53, got %q", s.Addr())
	}
	if s := NewServer("127.0.0.1:5353", nil); s.Addr() != "127.0.0.1:5353" {
		t.Fatalf("explicit addr must be kept, got %q", s.Addr())
	}
}
