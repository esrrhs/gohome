//go:build arm64

package aes

import (
	"testing"
)

func benchSetup() (src [2]uint64, softKeys, asmKeys [40]uint32, rkey [2]uint64) {
	key := [4]uint64{0x0123456789abcdef, 0xfedcba9876543210, 0x0f1e2d3c4b5a6978, 0x8796a5b4c3d2e1f0}
	src[0] = 0xdeadbeefcafebabe
	src[1] = 0x1234567890abcdef
	rkey[0] = 0x1122334455667788
	rkey[1] = 0x99aabbccddeeff00
	CnExpandKeyGoSoft(key[:], &softKeys)
	CnExpandKeyGoAsm(key[:], &asmKeys)
	return
}

func BenchmarkCnRoundsSoft(b *testing.B) {
	src, softKeys, _, _ := benchSetup()
	dst := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CnRoundsGoSoft(dst[:], src[:], &softKeys)
	}
}

func BenchmarkCnRoundsAsm(b *testing.B) {
	src, _, asmKeys, _ := benchSetup()
	dst := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CnRoundsAsm(&dst[0], &src[0], &asmKeys)
	}
}

func BenchmarkCnSingleRoundSoft(b *testing.B) {
	src, _, _, rkey := benchSetup()
	dst := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CnSingleRoundGoSoft(dst[:], src[:], &rkey)
	}
}

func BenchmarkCnSingleRoundAsm(b *testing.B) {
	src, _, _, rkey := benchSetup()
	dst := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CnSingleRoundAsm(&dst[0], &src[0], &rkey)
	}
}

func BenchmarkCnExpandKeySoft(b *testing.B) {
	key := [4]uint64{0x0123456789abcdef, 0xfedcba9876543210, 0x0f1e2d3c4b5a6978, 0x8796a5b4c3d2e1f0}
	var keys [40]uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CnExpandKeyGoSoft(key[:], &keys)
	}
}

func BenchmarkCnExpandKeyAsm(b *testing.B) {
	key := [4]uint64{0x0123456789abcdef, 0xfedcba9876543210, 0x0f1e2d3c4b5a6978, 0x8796a5b4c3d2e1f0}
	var keys [40]uint32
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CnExpandKeyGoAsm(key[:], &keys)
	}
}
