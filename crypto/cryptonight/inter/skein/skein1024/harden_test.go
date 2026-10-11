package skein1024

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/esrrhs/gohome/crypto/cryptonight/inter/skein"
)

// KAT 取自 NIST Skein 候选官方 golden KAT（skein_golden_kat.txt，
// Hackage skein-1.0.9.4 镜像），并用独立的 RustCrypto skein crate
// （Rust，与本 Go 移植无共同代码血统）对每个向量重新计算核对：
//   - Skein-1024-512("") / Skein-1024-256("") 为 crate 输出；
//     注意 Skein 不同输出长度对应不同配置块，摘要互不为前缀；
//   - 128 个 0x00（恰好一个 1024-bit 块）的 256/512 输出与官方原文一致。
func TestHardenKAT_TruncatedOutputs(t *testing.T) {
	const empty512 = "e2943eb0bc0efabd49503a76edf7cfcf072db25bad94ed44fe537284163f3119" +
		"c47ac6f78699b4272255966e0aba65c75a0a64bd23df6996d1bc3174afd9fa8b"
	const empty256 = "0e5203ea7b10d1e3320f0b015eb852b82619ad599880bcf156ed2e75c59bf1fc"
	const block256 = "35eb69e504af454e834af1a473418d69b72638dd7568957539ab6a154839f4e9"
	const block512 = "2df89e1021071c136ca68c020d0a670d980dc7750d23bb084d7bff10ca2f2f51" +
		"fa1e584da858df1fc58287b7c6f2bec2c48dcaafccd35f4682e68759b62b6a70"

	// 空消息：便捷函数（内部显式 finalize）必须等价于 New 后 Write(nil)
	var out64 [64]byte
	Sum512(&out64, nil, nil)
	if got := hex.EncodeToString(out64[:]); got != empty512 {
		t.Fatalf("Sum512 empty = %s", got)
	}
	h := New512(nil)
	h.Write(nil)
	if got := hex.EncodeToString(h.Sum(nil)); got != empty512 {
		t.Fatalf("New512+Write(nil) empty = %s", got)
	}

	var out32 [32]byte
	Sum256(&out32, nil, nil)
	if got := hex.EncodeToString(out32[:]); got != empty256 {
		t.Fatalf("Sum256 empty = %s", got)
	}
	h = New256(nil)
	h.Write(nil)
	if got := hex.EncodeToString(h.Sum(nil)); got != empty256 {
		t.Fatalf("New256+Write(nil) empty = %s", got)
	}

	// 单块（128 字节）消息，256/512 两种输出
	block := make([]byte, 128)
	Sum256(&out32, block, nil)
	if hex.EncodeToString(out32[:]) != block256 {
		t.Fatalf("Sum256 block = %x", out32)
	}
	Sum512(&out64, block, nil)
	if hex.EncodeToString(out64[:]) != block512 {
		t.Fatalf("Sum512 block = %x", out64)
	}
	h = New512(nil)
	h.Write(block)
	if got := hex.EncodeToString(h.Sum(nil)); got != block512 {
		t.Fatalf("New512 block mismatch: %s", got)
	}
}

// hash.Hash 契约与 Write 各分支：
// 块大小 128；边界 127/128/129/256/257；缓冲跨块；Reset 复用；
// Sum 不改状态、可追加前缀；非法 hashsize 必须 panic。
func TestHardenHashInterface(t *testing.T) {
	h := New512(nil)
	if h.Size() != 64 {
		t.Fatalf("Size = %d", h.Size())
	}
	if h.BlockSize() != 128 {
		t.Fatalf("BlockSize = %d", h.BlockSize())
	}

	// 跨块缓冲：先留 118 字节，再写 30 字节（凑满一块后还剩 20 字节）
	msg := bytes.Repeat([]byte{0x7B}, 400)
	oneShot := Sum(msg, 64, nil)

	h.Reset()
	if n, _ := h.Write(msg[:118]); n != 118 {
		t.Fatalf("Write n=%d", n)
	}
	if n, _ := h.Write(msg[118:148]); n != 30 {
		t.Fatalf("Write n=%d", n)
	}
	h.Write(msg[148:])
	if got := h.Sum(nil); !bytes.Equal(got, oneShot) {
		t.Fatal("split/cross-block write mismatch")
	}

	// Sum 幂等、可追加前缀、之后可继续 Write
	again := h.Sum(nil)
	if !bytes.Equal(again, oneShot) {
		t.Fatal("Sum must not mutate state")
	}
	prefixed := h.Sum([]byte("ZZ"))
	if !bytes.HasPrefix(prefixed, []byte("ZZ")) || !bytes.Equal(prefixed[2:], oneShot) {
		t.Fatalf("Sum(prefix) broken: len=%d", len(prefixed))
	}
	if len(prefixed) != 66 {
		t.Fatalf("Sum(prefix) length = %d, want 66", len(prefixed))
	}
	h.Write([]byte("extra"))
	cont := h.Sum(nil)
	if bytes.Equal(cont, oneShot) {
		t.Fatal("Write after Sum must extend the stream")
	}

	// 各种边界长度一次/多次写入等价（含 hashsize 非整块倍数 48）
	for _, n := range []int{0, 1, 127, 128, 129, 255, 256, 257, 400} {
		m := msg[:n]
		want := Sum(m, 48, nil)
		hh := New(48, nil)
		hh.Write(m[:n/3])
		hh.Write(m[n/3 : 2*n/3])
		hh.Write(m[2*n/3:])
		if got := hh.Sum(nil); !bytes.Equal(got, want) {
			t.Fatalf("len %d split mismatch", n)
		}
		// 非整块 hashsize 下 Sum(prefix) 同样必须正确
		hp := hh.Sum([]byte("P"))
		if len(hp) != 49 || string(hp[0]) != "P" || !bytes.Equal(hp[1:], want) {
			t.Fatalf("len %d non-block-sized Sum(prefix) broken", n)
		}
	}

	// 非法 hashsize
	for _, bad := range []int{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("New(%d) must panic", bad)
				}
			}()
			New(bad, nil)
		}()
	}
}

// 个性化配置的各字段（Key/PublicKey/KeyID/Nonce/Personal）初始化后
// Reset 必须恢复到带配置的初始状态；带密钥输出必须与无密钥不同。
func TestHardenConfigFields(t *testing.T) {
	conf := &skein.Config{
		Key:       []byte("secret-key"),
		PublicKey: []byte("public-key"),
		KeyID:     []byte("key-id"),
		Nonce:     []byte("nonce123"),
		Personal:  []byte("gohome-test"),
	}
	msg := []byte("configured message")

	h := New(32, conf)
	h.Write(msg)
	first := h.Sum(nil)
	h.Reset()
	h.Write(msg)
	second := h.Sum(nil)
	if !bytes.Equal(first, second) {
		t.Fatal("Reset must restore the personalized initial state")
	}
	if bytes.Equal(first, Sum(msg, 32, nil)) {
		t.Fatal("personalized hash must differ from unconfigured")
	}

	// New256(key) 与 Sum256 便捷函数必须一致（MAC 路径）
	var out [32]byte
	Sum256(&out, msg, conf.Key)
	hk := New256(conf.Key)
	hk.Write(msg)
	if !bytes.Equal(hk.Sum(nil), out[:]) {
		t.Fatal("keyed New256 / Sum256 mismatch")
	}
}
