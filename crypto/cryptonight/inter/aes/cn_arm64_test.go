//go:build arm64

package aes

import (
	"math/bits"
	"math/rand"
	"testing"
)

// 硬件轮密钥格式必须与软件轮密钥逐字节对应，只是每个 32 位字反转。
func TestCnExpandKeyGoAsmMatchesSoft(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	for n := 0; n < 2000; n++ {
		var key [4]uint64
		for i := range key {
			key[i] = rnd.Uint64()
		}
		var soft, asm [40]uint32
		CnExpandKeyGoSoft(key[:], &soft)
		CnExpandKeyGoAsm(key[:], &asm)
		for i := range soft {
			if asm[i] != bits.ReverseBytes32(soft[i]) {
				t.Fatalf("key %x round word %d: asm=%08x soft=%08x", key, i, asm[i], soft[i])
			}
		}
	}
}

// CnRoundsAsm 必须与软件实现逐位一致：随机输入 + 随机（合法）轮密钥。
func TestCnRoundsAsmMatchesSoft(t *testing.T) {
	rnd := rand.New(rand.NewSource(2))
	for n := 0; n < 2000; n++ {
		var key [4]uint64
		for i := range key {
			key[i] = rnd.Uint64()
		}
		var softKeys, asmKeys [40]uint32
		CnExpandKeyGoSoft(key[:], &softKeys)
		CnExpandKeyGoAsm(key[:], &asmKeys)

		var src, want, got [2]uint64
		src[0] = rnd.Uint64()
		src[1] = rnd.Uint64()
		want = src
		got = src
		CnRoundsGoSoft(want[:], want[:], &softKeys)
		CnRoundsAsm(&got[0], &got[0], &asmKeys)
		if want != got {
			t.Fatalf("rounds mismatch: src=%x key=%x soft=%x asm=%x", src, key, want, got)
		}
	}
}

// 非原地（dst != src）也必须一致。
func TestCnRoundsAsmNotInPlace(t *testing.T) {
	rnd := rand.New(rand.NewSource(3))
	for n := 0; n < 500; n++ {
		var key [4]uint64
		for i := range key {
			key[i] = rnd.Uint64()
		}
		var softKeys, asmKeys [40]uint32
		CnExpandKeyGoSoft(key[:], &softKeys)
		CnExpandKeyGoAsm(key[:], &asmKeys)

		var src, want, got [2]uint64
		src[0] = rnd.Uint64()
		src[1] = rnd.Uint64()
		CnRoundsGoSoft(want[:], src[:], &softKeys)
		CnRoundsAsm(&got[0], &src[0], &asmKeys)
		if want != got {
			t.Fatalf("rounds mismatch: src=%x key=%x soft=%x asm=%x", src, key, want, got)
		}
	}
}

// CnSingleRoundAsm 必须与软件实现逐位一致：随机输入 + 随机数据密钥。
func TestCnSingleRoundAsmMatchesSoft(t *testing.T) {
	rnd := rand.New(rand.NewSource(4))
	for n := 0; n < 2000; n++ {
		var src, rkey, want, got [2]uint64
		src[0] = rnd.Uint64()
		src[1] = rnd.Uint64()
		rkey[0] = rnd.Uint64()
		rkey[1] = rnd.Uint64()
		CnSingleRoundGoSoft(want[:], src[:], &rkey)
		CnSingleRoundAsm(&got[0], &src[0], &rkey)
		if want != got {
			t.Fatalf("single round mismatch: src=%x key=%x soft=%x asm=%x", src, rkey, want, got)
		}
	}
}

// 固定输入 + 固定密钥：与软件实现逐位一致（作为可重复的回归向量）。
func TestCnRoundsAsmFixedVector(t *testing.T) {
	var softKeys, asmKeys [40]uint32
	var key [4]uint64
	key[0] = 0x0123456789abcdef
	key[1] = 0xfedcba9876543210
	key[2] = 0x0f1e2d3c4b5a6978
	key[3] = 0x8796a5b4c3d2e1f0
	CnExpandKeyGoSoft(key[:], &softKeys)
	CnExpandKeyGoAsm(key[:], &asmKeys)

	var in, want, got [2]uint64
	in[0] = 0x0123456789abcdef
	in[1] = 0xfedcba9876543210
	CnRoundsGoSoft(want[:], in[:], &softKeys)
	CnRoundsAsm(&got[0], &in[0], &asmKeys)
	if want != got {
		t.Errorf("got %x, want %x", got, want)
	}
}

func TestCnSingleRoundAsmFixedVector(t *testing.T) {
	var in, rkey, want, got [2]uint64
	in[0] = 0x0123456789abcdef
	in[1] = 0xfedcba9876543210
	rkey[0] = 0x1122334455667788
	rkey[1] = 0x99aabbccddeeff00
	CnSingleRoundGoSoft(want[:], in[:], &rkey)
	CnSingleRoundAsm(&got[0], &in[0], &rkey)
	if want != got {
		t.Errorf("got %x, want %x", got, want)
	}
}
