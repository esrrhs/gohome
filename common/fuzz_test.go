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

// FuzzRc4RoundTrip 检查 RC4 的对称性：Rc4 每次调用都从初始 S-box 重新起步
// （见 alg.go 的 "each call starts keystream from 0"），因此对同一 key 连续两次
// 异或必须还原原文。这条性质一旦被状态复用破坏，加解密就会静默失配。
func FuzzRc4RoundTrip(f *testing.F) {
	f.Add("key", []byte(""))
	f.Add("key", []byte("hello"))
	f.Add(strings.Repeat("k", 256), []byte("max length key"))
	f.Add("k", []byte("min length key"))
	f.Add("中文密钥", []byte("payload"))

	f.Fuzz(func(t *testing.T, key string, src []byte) {
		if len(key) < 1 || len(key) > 256 {
			return
		}

		once, err := Rc4(key, src)
		if err != nil {
			t.Fatalf("Rc4 failed: %v", err)
		}
		if len(once) != len(src) {
			t.Fatalf("Rc4 changed length: %d != %d", len(once), len(src))
		}

		twice, err := Rc4(key, once)
		if err != nil {
			t.Fatalf("Rc4 (second pass) failed: %v", err)
		}
		if !bytes.Equal(twice, src) {
			t.Fatalf("Rc4 is not an involution: len=%d", len(src))
		}

		// 同 key 同明文必须得到同密文（keystream 每次从 0 起步）
		again, err := Rc4(key, src)
		if err != nil {
			t.Fatalf("Rc4 (repeat) failed: %v", err)
		}
		if !bytes.Equal(again, once) {
			t.Fatalf("Rc4 is not deterministic for the same key")
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
