package skein256

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/esrrhs/gohome/crypto/cryptonight/inter/skein"
)

// KAT 取自 NIST Skein 候选官方 golden KAT（skein_golden_kat.txt，
// Hackage skein-1.0.9.4 镜像），并用独立的 RustCrypto skein crate
// （Rust，与本 Go 移植无共同代码血统）对每个向量重新计算核对。
func TestHardenKAT(t *testing.T) {
	const empty256 = "c8877087da56e072870daa843f176e9453115929094c3a40c463a196c29bf7ba"
	const block512 = "918a1b6d20015d0bf53cf4fdd39e28d8bc5504a96c1d310ad5adb15dcddea270" +
		"184f9467451cd97bc624d30883a006336457815a88a9feb449460e4b42d966ac"

	var out32 [32]byte
	Sum256(&out32, nil, nil)
	if got := hex.EncodeToString(out32[:]); got != empty256 {
		t.Fatalf("Sum256 empty = %s", got)
	}
	h := New256(nil)
	h.Write(nil)
	if got := hex.EncodeToString(h.Sum(nil)); got != empty256 {
		t.Fatalf("New256+Write(nil) empty = %s", got)
	}

	block := make([]byte, 128)
	var out64 [64]byte
	Sum512(&out64, block, nil)
	if got := hex.EncodeToString(out64[:]); got != block512 {
		t.Fatalf("Sum512 z128 = %s", got)
	}
	h = New512(nil)
	h.Write(block)
	if got := hex.EncodeToString(h.Sum(nil)); got != block512 {
		t.Fatalf("New512 z128 = %s", got)
	}
}

// hash.Hash 契约与 Write 各分支：块大小 32；边界 31/32/33/64/65；
// 缓冲跨块；Reset 复用；Sum 不改状态、可追加前缀（旧实现返回 b[:hashsize]
// 会丢掉前缀并裁掉摘要字节）；非法 hashsize 必须 panic。
func TestHardenHashInterface(t *testing.T) {
	h := New512(nil)
	if h.Size() != 64 {
		t.Fatalf("Size = %d", h.Size())
	}
	if h.BlockSize() != 32 {
		t.Fatalf("BlockSize = %d", h.BlockSize())
	}

	msg := bytes.Repeat([]byte{0x6C}, 200)
	oneShot := Sum(msg, 64, nil)

	h.Reset()
	// 先留 22 字节，再写 20 字节凑满一块还剩 10 字节，触发跨块缓冲分支
	h.Write(msg[:22])
	h.Write(msg[22:42])
	h.Write(msg[42:])
	if got := h.Sum(nil); !bytes.Equal(got, oneShot) {
		t.Fatal("split/cross-block write mismatch")
	}

	again := h.Sum(nil)
	if !bytes.Equal(again, oneShot) {
		t.Fatal("Sum must not mutate state")
	}
	prefixed := h.Sum([]byte("ZZ"))
	if len(prefixed) != 66 || !bytes.HasPrefix(prefixed, []byte("ZZ")) ||
		!bytes.Equal(prefixed[2:], oneShot) {
		t.Fatalf("Sum(prefix) broken: len=%d", len(prefixed))
	}
	h.Write([]byte("extra"))
	if bytes.Equal(h.Sum(nil), oneShot) {
		t.Fatal("Write after Sum must extend the stream")
	}

	// 各种边界长度一次/多次写入等价（含 hashsize 非整块倍数 48）
	for _, n := range []int{0, 1, 31, 32, 33, 63, 64, 65, 97} {
		m := msg[:n]
		want := Sum(m, 48, nil)
		hh := New(48, nil)
		hh.Write(m[:n/3])
		hh.Write(m[n/3 : 2*n/3])
		hh.Write(m[2*n/3:])
		if got := hh.Sum(nil); !bytes.Equal(got, want) {
			t.Fatalf("len %d split mismatch", n)
		}
		hp := hh.Sum([]byte("P"))
		if len(hp) != 49 || hp[0] != 'P' || !bytes.Equal(hp[1:], want) {
			t.Fatalf("len %d non-block-sized Sum(prefix) broken", n)
		}
	}

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

// 截断便捷函数与同 hashsize 的流式构造必须一致；Reset 恢复个性化状态。
func TestHardenTruncatedAndConfig(t *testing.T) {
	msg := []byte("the quick brown fox jumps")

	var out20 [20]byte
	Sum160(&out20, msg, nil)
	h20 := New(20, nil)
	h20.Write(msg)
	if got := h20.Sum(nil); !bytes.Equal(got, out20[:]) {
		t.Fatal("Sum160 / New(20) mismatch")
	}

	var out48 [48]byte
	Sum384(&out48, msg, nil)
	h48 := New(48, nil)
	h48.Write(msg)
	if got := h48.Sum(nil); !bytes.Equal(got, out48[:]) {
		t.Fatal("Sum384 / New(48) mismatch")
	}

	conf := &skein.Config{
		Key:       []byte("secret-key"),
		PublicKey: []byte("public-key"),
		KeyID:     []byte("key-id"),
		Nonce:     []byte("nonce123"),
		Personal:  []byte("gohome-test"),
	}
	hc := New(32, conf)
	hc.Write(msg)
	first := hc.Sum(nil)
	hc.Reset()
	hc.Write(msg)
	second := hc.Sum(nil)
	if !bytes.Equal(first, second) {
		t.Fatal("Reset must restore the personalized initial state")
	}
	if bytes.Equal(first, Sum(msg, 32, nil)) {
		t.Fatal("personalized hash must differ from unconfigured")
	}

	var out32 [32]byte
	Sum256(&out32, msg, conf.Key)
	hk := New256(conf.Key)
	hk.Write(msg)
	if !bytes.Equal(hk.Sum(nil), out32[:]) {
		t.Fatal("keyed New256 / Sum256 mismatch")
	}
}
