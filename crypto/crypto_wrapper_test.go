package crypto

import (
	"encoding/hex"
	"testing"

	"github.com/esrrhs/gohome/crypto/cryptonight"
)

// TestNewCrypto 校验构造器对各 family 的封装语义：
// 空串与 "cryptonight" 初始化底层 CryptoNight，其余 family 返回未初始化对象。
func TestNewCrypto(t *testing.T) {
	if c := NewCrypto(""); c == nil || c.cn == nil {
		t.Fatalf("NewCrypto(\"\") did not initialize cryptonight: %+v", c)
	}
	if c := NewCrypto("cryptonight"); c == nil || c.cn == nil {
		t.Fatalf("NewCrypto(\"cryptonight\") did not initialize cryptonight: %+v", c)
	}
	if c := NewCrypto("unknown-family"); c == nil || c.cn != nil {
		t.Fatalf("NewCrypto(\"unknown-family\") should leave cn uninitialized: %+v", c)
	}
}

// TestCryptoSumKnownVectors 通过封装层驱动 cn/0，断言与上游标准向量一致，
// 且返回值固定为 32 字节。
func TestCryptoSumKnownVectors(t *testing.T) {
	c := NewCrypto("")
	vectors := []struct {
		input string
		want  string
	}{
		{"", "eb14e8a833fac6fe9a43b57b336789c46ffe93f2868452240720607b14387e11"},
		{"This is a test", "a084f01d1437a09c6985401b60d43554ae105802c5f5d8a9b3253649c0be6605"},
	}
	for _, v := range vectors {
		got := c.Sum([]byte(v.input), "cn/0", 0)
		if len(got) != 32 {
			t.Fatalf("Sum(%q) returned %d bytes, want 32", v.input, len(got))
		}
		if hex.EncodeToString(got) != v.want {
			t.Errorf("Sum(%q, cn/0) = %s, want %s", v.input, hex.EncodeToString(got), v.want)
		}
	}
}

// TestCryptoSumDeterministic 同一输入的哈希应稳定，且不同实例间结果一致、
// 不同输入结果不同；同时覆盖显式 "cryptonight" family 构造路径。
func TestCryptoSumDeterministic(t *testing.T) {
	c1 := NewCrypto("cryptonight")
	c2 := NewCrypto("")
	input := []byte("deterministic-input")

	first := c1.Sum(input, "cn/0", 0)
	second := c1.Sum(input, "cn/0", 0)
	other := c2.Sum(input, "cn/0", 0)
	diff := c1.Sum([]byte("another-input"), "cn/0", 0)

	if hex.EncodeToString(first) != hex.EncodeToString(second) {
		t.Error("Sum is not deterministic across calls on the same instance")
	}
	if hex.EncodeToString(first) != hex.EncodeToString(other) {
		t.Error("Sum differs between independent instances")
	}
	if hex.EncodeToString(first) == hex.EncodeToString(diff) {
		t.Error("Sum returned the same digest for different inputs")
	}
}

// TestCryptoSumUnknownAlgo 未识别的 algo 由底层返回 nil。
func TestCryptoSumUnknownAlgo(t *testing.T) {
	c := NewCrypto("")
	if got := c.Sum([]byte("data"), "not-an-algo", 0); got != nil {
		t.Errorf("Sum with unknown algo = %x, want nil", got)
	}
}

// TestAlgoList 封装层的 Algo 直接返回底层算法列表。
func TestAlgoList(t *testing.T) {
	algos := Algo()
	underlying := cryptonight.Algo()
	if len(algos) == 0 {
		t.Fatal("Algo() returned empty list")
	}
	if len(algos) != len(underlying) {
		t.Fatalf("Algo() has %d entries, want %d", len(algos), len(underlying))
	}
	found := false
	for _, a := range algos {
		if a == "cn/0" {
			found = true
		}
	}
	if !found {
		t.Error("Algo() does not contain cn/0")
	}
	for i := range algos {
		if algos[i] != underlying[i] {
			t.Errorf("Algo()[%d] = %q, want %q", i, algos[i], underlying[i])
		}
	}
}

// TestTestSumWrapper 覆盖封装层 TestSum 的两个方向：已知算法向量通过、
// 未知算法返回 false。cn/0 向量轻量，适合单测。
func TestTestSumWrapper(t *testing.T) {
	if !TestSum("cn/0") {
		t.Error("TestSum(\"cn/0\") = false, want true")
	}
	if TestSum("not-an-algo") {
		t.Error("TestSum(\"not-an-algo\") = true, want false")
	}
}
