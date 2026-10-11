package jh

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/esrrhs/gohome/common"
)

// KAT 来自与本实现无关的 Rust `jh` crate（RustCrypto 生态）交叉核对，
// 其中 "test" 的摘要与仓库原有硬编码值逐字节一致，证明两边实现同源正确。
// 长度选择覆盖：0/3 字节（不足一块）、63/64/65（块边界两侧）、
// 128/129（多块后恰在边界/多 1 字节）、1000（长消息多块）。
func TestHardenJH256_KAT(t *testing.T) {
	z := func(n int) []byte { return make([]byte, n) }
	cases := []struct {
		name string
		msg  []byte
		want string
	}{
		{"empty", nil, "46e64619c18bb0a92a5e87185a47eef83ca747b8fcc8e1412921357e326df434"},
		{"abc", []byte("abc"), "924bc82f24a76d519d4f69493da7fa70dc88bdb6016b6d1cc1dcf7def15e9cdd"},
		{"z63", z(63), "a604a7a85252df7692beb59815be3001bea14254ac96c8f534c17e4bb91e01d7"},
		{"z64", z(64), "afa70dd00e126b01caa323a98dc59a4e19280e083eb50486ff5091b009df0448"},
		{"z65", z(65), "e592e4309b70a64828efd573f335f15d21f7a84ab31bee8209ab8e3395e5d37b"},
		{"z128", z(128), "a1190884c3f6da622c4d87993c6cb1ff841219859e9e22facf53384938501b43"},
		{"z129", z(129), "4b0b376f460f5f0769316ab27ba5ad793a02a93ad59b2d8ff9941f1c25e1d561"},
		{"A1000", bytes.Repeat([]byte("A"), 1000), "ef7e7abcdf3cdd91b73a9fffda7948fd51ad6db067733b5548e731f6e80c884a"},
	}
	for _, c := range cases {
		got := hex.EncodeToString(Sum256(c.msg))
		if got != c.want {
			t.Errorf("Sum256(%s) = %s, want %s", c.name, got, c.want)
		}
	}
}

// hash.Hash 接口契约：Size/BlockSize、Reset 可复用、
// 分块写入（含跨 64 字节边界的缓冲拼接）必须与一次性写入等价、
// Sum 不改变内部状态可连续取值、Sum(prefix) 把摘要追加到 prefix 后。
func TestHardenJH256_HashInterface(t *testing.T) {
	h := New256()
	if h.Size() != 32 {
		t.Fatalf("Size = %d, want 32", h.Size())
	}
	if h.BlockSize() != 64 {
		t.Fatalf("BlockSize = %d, want 64", h.BlockSize())
	}

	msg := bytes.Repeat([]byte{0x5A}, 200)
	want := Sum256(msg)

	// 分块：先留 30 字节在缓冲区，再用足以填满并溢出的数据触发
	// "缓冲 + 新数据凑满一块"分支
	h.Reset()
	if n, err := h.Write(msg[:30]); err != nil || n != 30 {
		t.Fatalf("Write first chunk = (%d,%v)", n, err)
	}
	if n, err := h.Write(msg[30:100]); err != nil || n != 70 {
		t.Fatalf("Write second chunk = (%d,%v)", n, err)
	}
	if n, err := h.Write(msg[100:]); err != nil || n != 100 {
		t.Fatalf("Write third chunk = (%d,%v)", n, err)
	}
	if got := h.Sum(nil); !bytes.Equal(got, want) {
		t.Fatalf("split write mismatch:\n got %x\nwant %x", got, want)
	}

	// Sum 不修改状态：紧接着第二次 Sum 结果一致，且可追加前缀
	again := h.Sum(nil)
	if !bytes.Equal(again, want) {
		t.Fatal("Sum must not mutate the hasher state")
	}
	prefixed := h.Sum([]byte("PRE"))
	if !bytes.HasPrefix(prefixed, []byte("PRE")) ||
		!bytes.Equal(prefixed[3:], want) {
		t.Fatalf("Sum(prefix) broken: %x", prefixed)
	}

	// Reset 后必须能重新独立使用
	h.Reset()
	h.Write(msg)
	if got := h.Sum(nil); !bytes.Equal(got, want) {
		t.Fatal("Reset did not restore a clean hasher")
	}

	// Sum 之后继续 Write：流必须接上，等价于一次性写完整消息
	h.Reset()
	h.Write(msg[:40])
	if got := h.Sum(nil); !bytes.Equal(got, Sum256(msg[:40])) {
		t.Fatal("mid-stream Sum mismatch")
	}
	h.Write(msg[40:])
	if got := h.Sum(nil); !bytes.Equal(got, want) {
		t.Fatal("Write after Sum must continue the same stream")
	}

	// 缓冲不足一块、直接 Sum（覆盖 Write 的"缓冲但不满"分支）
	h.Reset()
	h.Write(msg[:10])
	if got := h.Sum(nil); !bytes.Equal(got, Sum256(msg[:10])) {
		t.Fatal("buffered partial write mismatch")
	}
}

// 调试态大端开关下的边界长度也要与一次性路径一致（该开关改变 f8 字节序）。
func TestHardenJH256_BigEndianMode(t *testing.T) {
	common.DebugSetBigEndian(true)
	defer common.DebugResetBigEndian()

	for _, n := range []int{0, 63, 64, 65, 128} {
		msg := make([]byte, n)
		for i := range msg {
			msg[i] = byte(i)
		}
		h := New256()
		h.Write(msg[:n/2])
		h.Write(msg[n/2:])
		got := h.Sum(nil)
		want := Sum256(msg)
		if !bytes.Equal(got, want) {
			t.Errorf("BE mode len %d: split %x != oneshot %x", n, got, want)
		}
	}
}
