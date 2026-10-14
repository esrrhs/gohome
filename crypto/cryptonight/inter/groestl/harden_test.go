package groestl

import (
	"bytes"
	"encoding/binary"
	"hash"
	"math/rand/v2"
	"testing"

	"github.com/esrrhs/gohome/common"
)

// 编译期确认 New256 返回合规的 hash.Hash
var _ hash.Hash = New256()

// sumChunked 按给定分块逐段 Write 后 Sum。
func sumChunked(t *testing.T, msg []byte, chunks []int, bigEndian bool) []byte {
	t.Helper()
	if bigEndian {
		common.DebugSetBigEndian(true)
		defer common.DebugResetBigEndian()
	}
	h := New256()
	off := 0
	for _, c := range chunks {
		if off+c > len(msg) {
			c = len(msg) - off
		}
		n, err := h.Write(msg[off : off+c])
		if err != nil || n != c {
			t.Fatalf("Write(%d bytes) = (%d, %v), want (%d, nil)", c, n, err, c)
		}
		off += c
	}
	if off != len(msg) {
		t.Fatalf("chunks consumed %d bytes, want %d", off, len(msg))
	}
	return h.Sum(nil)
}

// chunkings 生成一条消息的多种分块方案，覆盖 0/55/56/57/63/64/65 等
// 单双块填充边界与逐字节写。
func chunkings(length int) [][]int {
	cands := [][]int{
		{length},
		{1, length - 1},
		{length / 2, length - length/2},
		{55, length - 55},
		{56, length - 56},
		{57, length - 57},
		{63, length - 63},
		{64, length - 64},
		{65, length - 65},
	}
	if length > 1 {
		per := make([]int, length)
		for i := range per {
			per[i] = 1
		}
		cands = append(cands, per)
	}
	var out [][]int
	for _, c := range cands {
		ok := true
		sum := 0
		for _, v := range c {
			if v < 0 {
				ok = false
				break
			}
			sum += v
		}
		if ok && sum == length {
			out = append(out, c)
		}
	}
	return out
}

// TestHardenSumDoesNotMutateState 是 Sum 污染状态缺陷的回归测试：
// 修复前连续两次 Sum 结果不同、Sum 后继续 Write 摘要错误。
func TestHardenSumDoesNotMutateState(t *testing.T) {
	msg := []byte("hello world")
	more := []byte(" and more")
	full := append(append([]byte{}, msg...), more...)

	for _, be := range []bool{false, true} {
		fresh := Sum256(msg)
		freshFull := Sum256(full)

		h := New256()
		h.Write(msg)
		first := h.Sum(nil)
		second := h.Sum(nil)
		if !bytes.Equal(first, fresh) {
			t.Fatalf("be=%v: first Sum %x, one-shot %x", be, first, fresh)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("be=%v: double Sum differs: %x vs %x", be, first, second)
		}

		// Sum 之后继续 Write，流不能被破坏
		h.Write(more)
		cont := h.Sum(nil)
		if !bytes.Equal(cont, freshFull) {
			t.Fatalf("be=%v: Sum-then-Write digest %x, want %x", be, cont, freshFull)
		}

		// 前缀语义：Sum(b) = b || digest
		prefixed := h.Sum([]byte("PRE"))
		if len(prefixed) != 3+32 {
			t.Fatalf("be=%v: prefixed len %d, want %d", be, len(prefixed), 3+32)
		}
		if !bytes.Equal(prefixed[:3], []byte("PRE")) {
			t.Fatalf("be=%v: prefix bytes corrupted: %x", be, prefixed[:3])
		}
		if !bytes.Equal(prefixed[3:], cont) {
			t.Fatalf("be=%v: prefixed digest %x, want %x", be, prefixed[3:], cont)
		}
	}
}

// TestHardenWriteChunkingEquivalence 验证任意分块写入与一次式等价，
// 覆盖 Write 的部分缓冲填充路径与 Sum 的单/双块填充分支（大小端）。
func TestHardenWriteChunkingEquivalence(t *testing.T) {
	lengths := []int{0, 1, 2, 31, 32, 33, 55, 56, 57, 63, 64, 65, 95, 96, 127, 128, 129, 191, 192, 200, 255, 256, 320}
	for _, L := range lengths {
		msg := make([]byte, L)
		for i := range msg {
			msg[i] = byte(i*7 + 13)
		}
		want := Sum256(msg)
		if len(want) != 32 {
			t.Fatalf("L=%d: digest len %d, want 32", L, len(want))
		}
		for _, chunks := range chunkings(L) {
			for _, be := range []bool{false, true} {
				got := sumChunked(t, msg, chunks, be)
				if !bytes.Equal(got, want) {
					t.Fatalf("L=%d chunks=%v be=%v: got %x, want %x", L, chunks, be, got, want)
				}
			}
		}
	}
}

// TestHardenRandomChunkingEquivalence 固定种子随机消息 + 随机分块，
// 与一次式 Sum256 对账（大小端各一轮）。
func TestHardenRandomChunkingEquivalence(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261013, 9721))
	for iter := 0; iter < 300; iter++ {
		L := rng.IntN(321) // 0..320
		msg := make([]byte, L)
		for i := range msg {
			msg[i] = byte(rng.IntN(256))
		}
		want := Sum256(msg)

		var chunks []int
		remain := L
		for remain > 0 {
			c := rng.IntN(remain) + 1
			chunks = append(chunks, c)
			remain -= c
		}
		for _, be := range []bool{false, true} {
			got := sumChunked(t, msg, chunks, be)
			if !bytes.Equal(got, want) {
				t.Fatalf("iter=%d L=%d chunks=%v be=%v: got %x, want %x", iter, L, chunks, be, got, want)
			}
		}
	}
}

// TestHardenPoolReusePattern 模拟 cryptonight final_hash.go 的
// Get/Reset/Write/Sum/Put 循环：同一对象反复复用必须与一次式一致。
func TestHardenPoolReusePattern(t *testing.T) {
	h := New256()
	rounds := [][]byte{
		[]byte("test"),
		{},
		make([]byte, 200), // 生产 finalHash 固定写 200 字节
		make([]byte, 56),
		make([]byte, 57),
		make([]byte, 64),
	}
	for i := range rounds[2] {
		rounds[2][i] = byte(i * 11)
	}
	for round, msg := range rounds {
		h.Reset()
		// 分两段写，顺带覆盖部分缓冲路径
		half := len(msg) / 2
		if _, err := h.Write(msg[:half]); err != nil {
			t.Fatalf("round %d: first Write: %v", round, err)
		}
		if _, err := h.Write(msg[half:]); err != nil {
			t.Fatalf("round %d: second Write: %v", round, err)
		}
		got := h.Sum(nil)
		want := Sum256(msg)
		if !bytes.Equal(got, want) {
			t.Fatalf("round %d: pooled digest %x, want %x", round, got, want)
		}
	}
}

// TestHardenResetSizeBlockSize 覆盖 Reset/Size/BlockSize（既有测试零覆盖），
// 并用白盒状态比对确认 Reset 恢复到的就是全新状态。
func TestHardenResetSizeBlockSize(t *testing.T) {
	h := New256()
	if h.Size() != 32 {
		t.Errorf("Size() = %d, want 32", h.Size())
	}
	if h.BlockSize() != 64 {
		t.Errorf("BlockSize() = %d, want 64", h.BlockSize())
	}

	// 污染后 Reset，再写的摘要必须等于一次式
	h.Write([]byte("garbage data"))
	_ = h.Sum(nil)
	h.Reset()
	h.Write([]byte("test"))
	if got := h.Sum(nil); !bytes.Equal(got, Sum256([]byte("test"))) {
		t.Errorf("after Reset: got %x, want %x", got, Sum256([]byte("test")))
	}

	// 重复 Reset 幂等
	h.Reset()
	h.Reset()
	h.Write([]byte("test"))
	if got := h.Sum(nil); !bytes.Equal(got, Sum256([]byte("test"))) {
		t.Errorf("after double Reset: got %x, want %x", got, Sum256([]byte("test")))
	}

	// 白盒：Reset 后的 state 与全新 state 逐字段一致
	s := New256().(*state)
	s.Write([]byte("garbage data"))
	s.Sum(nil)
	s.Reset()
	fresh := New256().(*state)
	if *s != *fresh {
		t.Errorf("Reset state %+v != fresh state %+v", *s, *fresh)
	}
}

// TestHardenBlockCounterWrap 白盒把 blockCounter1 顶到 0xFFFFFFFF，
// 确定性命中 transform 与 Sum 两处计数器回绕分支（真实场景需 256GiB
// 消息才能走到），并顺带断言 Sum 不碰接收者计数器。
func TestHardenBlockCounterWrap(t *testing.T) {
	// transform 内回绕：写满一块即 +1
	s := New256().(*state)
	s.blockCounter1 = 0xFFFFFFFF
	n, err := s.Write(make([]byte, 64))
	if n != 64 || err != nil {
		t.Fatalf("Write = (%d, %v), want (64, nil)", n, err)
	}
	if s.blockCounter1 != 0 || s.blockCounter2 != 1 {
		t.Fatalf("transform wrap: bc1=%d bc2=%d, want 0/1", s.blockCounter1, s.blockCounter2)
	}
	first := s.Sum(nil)
	second := s.Sum(nil)
	if len(first) != 32 {
		t.Fatalf("wrapped digest len %d, want 32", len(first))
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("wrapped double Sum differs: %x vs %x", first, second)
	}

	// Sum 内 length padding 回绕：顶到最大值后直接 Sum
	s2 := New256().(*state)
	s2.Write([]byte("test"))
	s2.blockCounter1 = 0xFFFFFFFF
	a := s2.Sum(nil)
	b := s2.Sum(nil)
	if len(a) != 32 || !bytes.Equal(a, b) {
		t.Fatalf("Sum wrap: len=%d equal=%v", len(a), bytes.Equal(a, b))
	}
	if s2.blockCounter1 != 0xFFFFFFFF || s2.blockCounter2 != 0 {
		t.Fatalf("Sum mutated receiver counters: bc1=%d bc2=%d", s2.blockCounter1, s2.blockCounter2)
	}
}

// TestHardenByteSwapHelpers 直接调用三个字节序转换函数：既覆盖
// 正常往返，也覆盖尺寸非法的 panic 防御分支（调用点全是编译期常量，
// 不走正常路径）。
func TestHardenByteSwapHelpers(t *testing.T) {
	a := make([]byte, 64)
	for i := range a {
		a[i] = byte(i*3 + 5)
	}

	w := u8_u32_be(a, 0, 64)
	for i := 0; i < 16; i++ {
		if want := binary.LittleEndian.Uint32(a[i*4 : i*4+4]); w[i] != want {
			t.Fatalf("u8_u32_be word %d = %08x, want %08x", i, w[i], want)
		}
	}

	b32 := u32_u8_be_32(w[:], 8, 16)
	if !bytes.Equal(b32[:], a[32:64]) {
		t.Fatalf("u32_u8_be_32 roundtrip: %x, want %x", b32[:], a[32:64])
	}

	b64 := u32_u8_be(w[:], 0, 16)
	if !bytes.Equal(b64[:], a) {
		t.Fatalf("u32_u8_be roundtrip: %x, want %x", b64[:], a)
	}

	assertPanic(t, "u8_u32_be invalid size", func() { u8_u32_be(a, 0, 8) })
	assertPanic(t, "u32_u8_be_32 invalid size", func() { u32_u8_be_32(w[:], 0, 4) })
	assertPanic(t, "u32_u8_be invalid size", func() { u32_u8_be(w[:], 0, 8) })
}

func assertPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s: expected panic, got none", name)
		}
	}()
	f()
}

// TestHardenKnownVectors 固定上游移植自带向量 + 边界长度自洽对账。
func TestHardenKnownVectors(t *testing.T) {
	want := []byte{70, 66, 23, 246, 116, 157, 209, 218, 148, 85, 117, 146, 208, 153, 23, 176,
		121, 243, 74, 41, 148, 149, 34, 171, 63, 27, 61, 100, 156, 8, 4, 249}
	if got := Sum256([]byte("test")); !bytes.Equal(got, want) {
		t.Fatalf("Sum256(\"test\") = %x, want %x", got, want)
	}

	// 边界长度（两种填充分支 + 整块边界）与一次式对账
	for _, L := range []int{0, 55, 56, 63, 64, 65, 200} {
		msg := make([]byte, L)
		for i := range msg {
			msg[i] = byte(i)
		}
		h := New256()
		h.Write(msg)
		if got := h.Sum(nil); !bytes.Equal(got, Sum256(msg)) {
			t.Fatalf("L=%d: %x, want %x", L, got, Sum256(msg))
		}
	}
}
