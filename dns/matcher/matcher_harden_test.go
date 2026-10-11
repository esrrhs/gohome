package matcher

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ===========================================================================
// 最小 GeoLite2-Country mmdb 生成器（仅用于测试，零外部依赖）
//
// 文件布局：搜索树(record_size=24) + 16 字节分隔符 + 数据段 +
// 14 字节 metadata marker + metadata map。
// 语义依据 github.com/oschwald/maxminddb-golang v1.13 的读取实现核对：
//   - 节点记录：< nodeCount 为下一节点编号；== nodeCount 为空；
//     >= nodeCount+16 为数据段偏移（减去 nodeCount+16）
//   - 数据字段控制字节：高 3 位类型、低 5 位长度；类型>=8 用扩展转义
// ===========================================================================

const (
	mmTypeString = 2
	mmTypeUint16 = 5
	mmTypeUint32 = 6
	mmTypeMap    = 7
	mmTypeUint64 = 9  // 扩展类型，扩展字节 = 9-7 = 2
	mmTypeArray  = 11 // wire 类型 11（10 是 uint128），扩展字节 = 11-7 = 4
)

func mmCtrl(t byte, size int) byte { return byte(t)<<5 | byte(size) }

func mmString(s string) []byte {
	b := []byte{mmCtrl(mmTypeString, len(s))}
	return append(b, s...)
}

// mmUint 按最小大端字节数编码 uint16/32/64
func mmUint(t byte, v uint64) []byte {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], v)
	i := 0
	for i < 7 && raw[i] == 0 {
		i++
	}
	payload := raw[i:]
	if t == mmTypeUint64 {
		// 扩展类型：第一字节 type=0，低 5 位为长度，第二字节为真实类型-7
		return append([]byte{mmCtrl(0, len(payload)), mmTypeUint64 - 7}, payload...)
	}
	return append([]byte{mmCtrl(t, len(payload))}, payload...)
}

func mmMapHeader(n int) []byte { return []byte{mmCtrl(mmTypeMap, n)} }

func mmArrayHeader(n int) []byte {
	// array 是扩展类型 11 -> 第二字节 4
	return []byte{mmCtrl(0, n), mmTypeArray - 7}
}

// mmCountryRecord 编码 {"country":{"iso_code":code}}
func mmCountryRecord(code string) []byte {
	var b []byte
	b = append(b, mmMapHeader(1)...)
	b = append(b, mmString("country")...)
	b = append(b, mmMapHeader(1)...)
	b = append(b, mmString("iso_code")...)
	b = append(b, mmString(code)...)
	return b
}

type mmTreeNode struct{ rec [2]uint32 }

// mmBuildTree 向二叉 trie 插入前缀（bits 长度即前缀长度）。
// mmdb 查询在每个 bit 处读当前节点的记录槽：槽值 < nodeCount 才进下一节点。
// 因此数据记录必须直接挂在「最后一个前缀 bit 对应的记录槽」上，不能再下钻
// 新建节点——否则 /32 前缀会在 32 个 bit 耗尽后停在一个普通节点上，
// 触发 "invalid node in search tree"。测试用的前缀互不重叠。
func mmInsert(nodes []*mmTreeNode, bits []bool, data uint32) []*mmTreeNode {
	cur := uint32(0)
	for _, b := range bits[:len(bits)-1] {
		side := uint32(0)
		if b {
			side = 1
		}
		next := nodes[cur].rec[side]
		// 0 表示尚未连接（空终止值 nodeCount 由最后统一填充）
		if next == 0 {
			next = uint32(len(nodes))
			nodes[cur].rec[side] = next
			nodes = append(nodes, &mmTreeNode{})
		}
		cur = next
	}
	// 最后一个前缀 bit 的记录槽直接指向数据
	lastSide := uint32(0)
	if bits[len(bits)-1] {
		lastSide = 1
	}
	nodes[cur].rec[lastSide] = data
	return nodes
}

func mmBitsPrefix(ipv4 [4]byte, prefixLen int) []bool {
	bits := make([]bool, 0, prefixLen)
	for i := 0; i < prefixLen; i++ {
		bits = append(bits, ipv4[i>>3]&(0x80>>uint(i%8)) != 0)
	}
	return bits
}

// mmWriteCountryDB 生成一个 IPv4-only 的 GeoLite2-Country 库：
// records 形如 map[CIDR]国家码，写入 path 并返回路径。
func mmWriteCountryDB(t *testing.T, path string, records map[string]string) {
	t.Helper()

	// 1) 先编码数据段（记录），得到各自偏移
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
		if pfx > 32 {
			t.Fatalf("only ipv4 prefixes supported, got %d", pfx)
		}

		off := uint32(len(dataSection))
		dataSection = append(dataSection, mmCountryRecord(code)...)
		infos = append(infos, recInfo{bits: mmBitsPrefix(a, pfx), off: off})
	}

	// 2) 建树：nodeCount 未知，数据记录先用高位占位，建完再回填
	const placeholderBase = uint32(0x40000000)
	nodes := []*mmTreeNode{{}}
	for i, ri := range infos {
		nodes = mmInsert(nodes, ri.bits, placeholderBase+uint32(i))
	}
	nodeCount := uint32(len(nodes))
	terminator := nodeCount
	// 回填：数据记录 = nodeCount + 16 + 数据段偏移；未连接记录 = 终止值
	for _, n := range nodes {
		for s := 0; s < 2; s++ {
			switch {
			case n.rec[s] == 0:
				n.rec[s] = terminator
			case n.rec[s] >= placeholderBase:
				idx := n.rec[s] - placeholderBase
				n.rec[s] = nodeCount + 16 + infos[idx].off
			}
		}
	}

	// 3) 序列化搜索树（record_size=24，每节点 6 字节）
	tree := make([]byte, int(nodeCount)*6)
	for i, n := range nodes {
		base := i * 6
		// 左记录 3 字节
		tree[base+0] = byte(n.rec[0] >> 16)
		tree[base+1] = byte(n.rec[0] >> 8)
		tree[base+2] = byte(n.rec[0])
		// 右记录 3 字节
		tree[base+3] = byte(n.rec[1] >> 16)
		tree[base+4] = byte(n.rec[1] >> 8)
		tree[base+5] = byte(n.rec[1])
	}

	// 4) metadata
	var meta []byte
	meta = append(meta, mmMapHeader(9)...)
	meta = append(meta, mmString("binary_format_major_version")...)
	meta = append(meta, mmUint(mmTypeUint16, 2)...)
	meta = append(meta, mmString("binary_format_minor_version")...)
	meta = append(meta, mmUint(mmTypeUint16, 0)...)
	meta = append(meta, mmString("build_epoch")...)
	meta = append(meta, mmUint(mmTypeUint64, 0)...)
	meta = append(meta, mmString("database_type")...)
	meta = append(meta, mmString("GeoLite2-Country")...)
	meta = append(meta, mmString("description")...)
	meta = append(meta, mmMapHeader(1)...)
	meta = append(meta, mmString("en")...)
	meta = append(meta, mmString("harden test db")...)
	meta = append(meta, mmString("ip_version")...)
	meta = append(meta, mmUint(mmTypeUint16, 4)...)
	meta = append(meta, mmString("languages")...)
	meta = append(meta, mmArrayHeader(1)...)
	meta = append(meta, mmString("en")...)
	meta = append(meta, mmString("node_count")...)
	meta = append(meta, mmUint(mmTypeUint32, uint64(nodeCount))...)
	meta = append(meta, mmString("record_size")...)
	meta = append(meta, mmUint(mmTypeUint16, 24)...)

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

func countryDBPath(t *testing.T, records map[string]string) string {
	p := filepath.Join(t.TempDir(), "country.mmdb")
	mmWriteCountryDB(t, p, records)
	return p
}

func mmTestRecords() map[string]string {
	return map[string]string{
		"1.0.0.0/8":   "CN",
		"2.0.0.0/8":   "US",
		"9.9.9.9/32":  "CN",
		"10.8.0.0/16": "JP",
	}
}

// ===========================================================================
// GeoDB 成功路径：真实 mmdb 查找
// ===========================================================================

func TestHardenGeoDBLookups(t *testing.T) {
	db := NewGeoDB()
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Open(countryDBPath(t, mmTestRecords())); err != nil {
		t.Fatalf("Open generated mmdb: %v", err)
	}

	cases := []struct {
		ip   string
		code string
	}{
		{"1.1.1.1", "CN"},
		{"1.255.255.255", "CN"},
		{"2.0.0.1", "US"},
		{"9.9.9.9", "CN"},
		{"10.8.7.6", "JP"},
		{"8.8.8.8", ""},  // 无记录：空 code、无错误
		{"9.9.9.10", ""}, // 只注册了 /32
		{"10.9.0.1", ""}, // /16 外
	}
	for _, c := range cases {
		got, err := db.GetCountryCode(net.ParseIP(c.ip))
		if err != nil {
			t.Fatalf("GetCountryCode(%s): %v", c.ip, err)
		}
		if got != c.code {
			t.Errorf("GetCountryCode(%s) = %q, want %q", c.ip, got, c.code)
		}
		gotStr, err := db.GetCountryCodeStr(c.ip)
		if err != nil || gotStr != c.code {
			t.Errorf("GetCountryCodeStr(%s) = %q, %v", c.ip, gotStr, err)
		}
	}

	// IsCountry 命中/不命中
	if !db.IsCountry(net.ParseIP("1.2.3.4"), "CN") {
		t.Error("1.2.3.4 must be CN")
	}
	if db.IsCountry(net.ParseIP("1.2.3.4"), "US") {
		t.Error("1.2.3.4 must not be US")
	}
	// 无归属地 IP + 空 code：旧实现 ""=="" 误判 true
	if db.IsCountry(net.ParseIP("8.8.8.8"), "") {
		t.Error("empty target code must never match, even for empty-record IPs")
	}
	if db.IsCountry(net.ParseIP("1.1.1.1"), "") {
		t.Error("empty target code must never match")
	}

	// IPv6 在 IPv4-only 库中查询必须返回错误（db.Country 错误分支）
	if _, err := db.GetCountryCode(net.ParseIP("2001:db8::1")); err == nil {
		t.Error("IPv6 lookup in IPv4-only db must error")
	}
}

func TestHardenGeoDBHotReloadAndClose(t *testing.T) {
	db := NewGeoDB()
	t.Cleanup(func() { _ = db.Close() })

	p1 := countryDBPath(t, map[string]string{"1.0.0.0/8": "CN"})
	if err := db.Open(p1); err != nil {
		t.Fatal(err)
	}
	if code, _ := db.GetCountryCode(net.ParseIP("1.1.1.1")); code != "CN" {
		t.Fatalf("before reload = %q", code)
	}

	// 热替换为内容不同的新库：旧句柄必须被关闭、查询结果以新库为准
	p2 := countryDBPath(t, map[string]string{"1.0.0.0/8": "US", "3.0.0.0/8": "DE"})
	if err := db.Open(p2); err != nil {
		t.Fatalf("hot reload Open: %v", err)
	}
	if code, _ := db.GetCountryCode(net.ParseIP("1.1.1.1")); code != "US" {
		t.Fatalf("after reload = %q, want US", code)
	}
	if code, _ := db.GetCountryCode(net.ParseIP("3.3.3.3")); code != "DE" {
		t.Fatalf("new subnet after reload = %q", code)
	}

	// 损坏文件 Open 失败：旧库必须保持可用，不能被半初始化句柄替换
	bad := filepath.Join(t.TempDir(), "broken.mmdb")
	if err := os.WriteFile(bad, []byte("this is definitely not an mmdb file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.Open(bad); err == nil {
		t.Fatal("Open garbage file must fail")
	}
	if code, _ := db.GetCountryCode(net.ParseIP("1.1.1.1")); code != "US" {
		t.Fatalf("failed reload must keep previous db, got %q", code)
	}

	// 关闭已加载库
	if err := db.Close(); err != nil {
		t.Fatalf("Close loaded db: %v", err)
	}
	if _, err := db.GetCountryCode(net.ParseIP("1.1.1.1")); err == nil {
		t.Error("lookup after Close must error")
	}
	// 重复 Close 必须幂等
	if err := db.Close(); err != nil {
		t.Fatalf("double Close: %v", err)
	}
}

// TestHardenGeoDBConcurrentReloadStress 热替换与查询并发：
// linux/darwin 上 maxminddb 走 mmap，旧库 Close 即 munmap。
// 查询必须在 RLock 覆盖内完成、旧库必须在写锁内回收，
// 否则 -race 会报 r.buffer 读写竞争，最坏直接 SIGBUS。
func TestHardenGeoDBConcurrentReloadStress(t *testing.T) {
	db := NewGeoDB()
	t.Cleanup(func() { _ = db.Close() })

	pCN := countryDBPath(t, map[string]string{"1.0.0.0/8": "CN"})
	pUS := countryDBPath(t, map[string]string{"1.0.0.0/8": "US", "3.0.0.0/8": "DE"})
	if err := db.Open(pCN); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// 2 个热更新 goroutine 紧密交替加载两代库
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			paths := []string{pCN, pUS}
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				if err := db.Open(paths[(i+seed)%2]); err != nil {
					t.Errorf("concurrent Open: %v", err)
					return
				}
			}
		}(r)
	}

	// 8 个查询 goroutine：结果只可能来自当前有效两代库之一，绝不能报错/崩溃
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			one := net.ParseIP("1.1.1.1")
			three := net.ParseIP("3.3.3.3")
			for {
				select {
				case <-stop:
					return
				default:
				}
				code, err := db.GetCountryCode(one)
				if err != nil || (code != "CN" && code != "US") {
					t.Errorf("1.1.1.1 lookup during reload = %q, %v", code, err)
					return
				}
				// 3.0.0.0/8 在 CN 代无记录（""），US 代是 DE
				if code, err = db.GetCountryCode(three); err != nil || (code != "" && code != "DE") {
					t.Errorf("3.3.3.3 lookup during reload = %q, %v", code, err)
					return
				}
			}
		}()
	}

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()

	// 收敛后最后一代库必须仍然可用
	if code, err := db.GetCountryCode(net.ParseIP("1.1.1.1")); err != nil || (code != "CN" && code != "US") {
		t.Fatalf("final lookup = %q, %v", code, err)
	}
}

// ===========================================================================
// Trie / IPNetList 残余分支
// ===========================================================================

func TestHardenTrieEmptyLabelAndMalformedDnsmasq(t *testing.T) {
	trie := NewDomainTrie()

	// 连续点：空 label 被跳过，"a..b.com" 等价于 "a.b.com"
	trie.Add("a..b.com")
	if !trie.Has("x.a.b.com") {
		t.Fatal("empty labels in Add must be skipped")
	}
	trie.Reset([]string{"p..q.example", ""})
	if !trie.Has("x.p.q.example") {
		t.Fatal("empty labels in Reset must be skipped")
	}

	// 域名段为空的 dnsmasq 行不能落入普通域名解析产生垃圾节点
	n, err := trie.LoadFromReader(strings.NewReader("server=/\nipset=/\nserver=/ok.example/1.1.1.1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("only the valid dnsmasq line must count, got %d", n)
	}
	if !trie.Has("a.ok.example") {
		t.Fatal("valid dnsmasq domain must match")
	}
	if trie.Has("server=") || trie.Has("ipset=") {
		t.Fatal("malformed dnsmasq lines must not create junk domains")
	}
}

func TestHardenIPNetListLoadReaderError(t *testing.T) {
	list := NewIPNetList()
	// scanner 读取错误必须透出
	if _, err := list.LoadFromReader(errReader{}); err == nil {
		t.Fatal("reader error must be propagated")
	}
	// 错误后列表保持空（原子替换未发生）
	if list.Contains(net.ParseIP("1.1.1.1")) {
		t.Fatal("list must stay empty after a failed load")
	}
}
