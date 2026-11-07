//go:build arm64

package aes

import (
	"testing"
	"unsafe"
)

// roundsSoftNoBE 是 CnRoundsGoSoft 去掉每轮 common.IsBigEndian() 原子读之后的等价实现，
// 仅用于单独量化该开销，不参与生产路径。
func roundsSoftNoBE(dst, src []uint64, rkeys *[40]uint32) {
	src8 := (*[16]byte)(unsafe.Pointer(&src[0]))
	dst8 := (*[16]byte)(unsafe.Pointer(&dst[0]))

	var s0, s1, s2, s3, t0, t1, t2, t3 uint32
	s0 = uint32(src8[0])<<24 | uint32(src8[1])<<16 | uint32(src8[2])<<8 | uint32(src8[3])
	s1 = uint32(src8[4])<<24 | uint32(src8[5])<<16 | uint32(src8[6])<<8 | uint32(src8[7])
	s2 = uint32(src8[8])<<24 | uint32(src8[9])<<16 | uint32(src8[10])<<8 | uint32(src8[11])
	s3 = uint32(src8[12])<<24 | uint32(src8[13])<<16 | uint32(src8[14])<<8 | uint32(src8[15])

	for r := 0; r < 10; r++ {
		t0 = rkeys[4*r+0] ^ te0[uint8(s0>>24)] ^ te1[uint8(s1>>16)] ^ te2[uint8(s2>>8)] ^ te3[uint8(s3)]
		t1 = rkeys[4*r+1] ^ te0[uint8(s1>>24)] ^ te1[uint8(s2>>16)] ^ te2[uint8(s3>>8)] ^ te3[uint8(s0)]
		t2 = rkeys[4*r+2] ^ te0[uint8(s2>>24)] ^ te1[uint8(s3>>16)] ^ te2[uint8(s0>>8)] ^ te3[uint8(s1)]
		t3 = rkeys[4*r+3] ^ te0[uint8(s3>>24)] ^ te1[uint8(s0>>16)] ^ te2[uint8(s1>>8)] ^ te3[uint8(s2)]
		s0, s1, s2, s3 = t0, t1, t2, t3
	}

	dst8[0], dst8[1], dst8[2], dst8[3] = byte(s0>>24), byte(s0>>16), byte(s0>>8), byte(s0)
	dst8[4], dst8[5], dst8[6], dst8[7] = byte(s1>>24), byte(s1>>16), byte(s1>>8), byte(s1)
	dst8[8], dst8[9], dst8[10], dst8[11] = byte(s2>>24), byte(s2>>16), byte(s2>>8), byte(s2)
	dst8[12], dst8[13], dst8[14], dst8[15] = byte(s3>>24), byte(s3>>16), byte(s3>>8), byte(s3)
}

func BenchmarkCnRoundsSoftNoBigEndianCheck(b *testing.B) {
	src, softKeys, _, _ := benchSetup()
	dst := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		roundsSoftNoBE(dst[:], src[:], &softKeys)
	}
}

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
