package common

import (
	"bytes"
	"net"
	"strings"
	"testing"
)

// FuzzCompressRoundTrip 对三种压缩实现做往返检查：任意字节序列压缩后再解压
// 必须还原成原串。历史上哈希与分块边界出过缺陷，压缩路径同样值得自动巡检。
func FuzzCompressRoundTrip(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("a"))
	f.Add([]byte("hello, world"))
	f.Add(bytes.Repeat([]byte{0x00}, 1024))
	f.Add(bytes.Repeat([]byte{0xFF}, 4096))
	f.Add([]byte("你好，世界"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// gzip
		gz := GzipString(string(data))
		if back := GunzipString(gz); back != string(data) {
			t.Fatalf("gzip round trip failed: len=%d got %q want %q", len(data), truncate(back), truncate(string(data)))
		}
		for name, fn := range map[string]func(string) string{
			"best-compression": GzipStringBestCompression,
			"best-speed":       GzipStringBestSpeed,
		} {
			if back := GunzipString(fn(string(data))); back != string(data) {
				t.Fatalf("gzip(%s) round trip failed: len=%d", name, len(data))
			}
		}

		// zlib / 通用压缩
		packed := CompressData(data)
		if packed == nil {
			t.Fatalf("CompressData returned nil for len=%d", len(data))
		}
		back, err := DeCompressData(packed)
		if err != nil {
			t.Fatalf("DeCompressData failed: %v (len=%d)", err, len(data))
		}
		if !bytes.Equal(back, data) {
			t.Fatalf("CompressData round trip mismatch: len=%d got %d", len(data), len(back))
		}

		// zstd
		zpacked := CompressDataZstd(data)
		if zpacked == nil {
			t.Fatalf("CompressDataZstd returned nil for len=%d", len(data))
		}
		zback, err := DeCompressDataZstd(zpacked)
		if err != nil {
			t.Fatalf("DeCompressDataZstd failed: %v (len=%d)", err, len(data))
		}
		if !bytes.Equal(zback, data) {
			t.Fatalf("zstd round trip mismatch: len=%d got %d", len(data), len(zback))
		}
	})
}

// FuzzGetRootDomain 对 eTLD+1 解析做 fuzzing。输入来自外部（DNS 查询名、HTTP
// Host 头），任意字节都不得 panic，且解析必须是确定性的。
func FuzzGetRootDomain(f *testing.F) {
	f.Add("www.example.com")
	f.Add("example.com")
	f.Add("com")
	f.Add("")
	f.Add(".")
	f.Add("..")
	f.Add("WWW.EXAMPLE.COM.")
	f.Add("xn--80ak6aa92e.com")
	f.Add("1.2.3.4")
	f.Add("a..b")
	f.Add(strings.Repeat("x.", 100) + "com")

	f.Fuzz(func(t *testing.T, host string) {
		root, err := GetRootDomain(host)
		if err != nil {
			return
		}
		// 解析必须是确定性的
		root2, err2 := GetRootDomain(host)
		if err2 != nil || root2 != root {
			t.Fatalf("GetRootDomain is not deterministic: %q vs %q", root, root2)
		}
		// 先剥离端口，再判断是否真的有主机名：GetRootDomain 对空主机名
		// 返回空是既定设计（无点主机名直接返回自身，见 net.go）。
		probe := host
		if h, _, err := net.SplitHostPort(host); err == nil {
			probe = h
		}
		norm := strings.ToLower(strings.Trim(probe, "."))
		if norm == "" {
			return
		}
		// 有主机名却返回空根域名：调用方会拿着空串继续匹配，属真实缺陷
		if root == "" {
			t.Fatalf("GetRootDomain(%q) returned an empty root without error", host)
		}
		// 根域名必须是输入（去端口、忽略大小写与尾点后）的后缀
		if norm != "" && !strings.HasSuffix(norm, strings.ToLower(strings.Trim(root, "."))) {
			t.Fatalf("root %q is not a suffix of host %q", root, host)
		}
	})
}

func truncate(s string) string {
	if len(s) > 32 {
		return s[:32] + "..."
	}
	return s
}
