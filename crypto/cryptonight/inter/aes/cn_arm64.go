//go:build arm64

package aes

import (
	"math/bits"

	"golang.org/x/sys/cpu"
)

var hasARMAES = cpu.ARM64.HasAES

// UseSoft 与其它架构保持相同签名；arm64 上是否使用硬件 AES 由 CPU 特性决定。
func UseSoft(b bool) {
}

// CnExpandKeyGo 在支持 ARMv8 AES 扩展时生成可直接供 AESE 使用的轮密钥，
// 否则退回软件实现（此时轮密钥也必须使用软件格式，二者不可混用）。
func CnExpandKeyGo(key []uint64, rkeys *[40]uint32) {
	if !hasARMAES {
		CnExpandKeyGoSoft(key, rkeys)
		return
	}
	CnExpandKeyGoAsm(key, rkeys)
}

func CnRoundsGo(dst, src []uint64, rkeys *[40]uint32) {
	if !hasARMAES {
		CnRoundsGoSoft(dst, src, rkeys)
		return
	}
	CnRoundsAsm(&dst[0], &src[0], rkeys)
}

func CnSingleRoundGo(dst, src []uint64, rkey *[2]uint64) {
	if !hasARMAES {
		CnSingleRoundGoSoft(dst, src, rkey)
		return
	}
	CnSingleRoundAsm(&dst[0], &src[0], rkey)
}

// CnExpandKeyGoAsm 与 CnExpandKeyGoSoft 的展开过程数值上完全一致，区别只在
// 存储字节序：AESE 要求 16 字节轮密钥的 lane0 对应 AES 状态的 byte0，
// 而软件实现按 uint32 小端存的是 byte3..byte0，因此这里存的是每个 32 位字
// 按字节反转后的值。测试见 cn_arm64_test.go。
func CnExpandKeyGoAsm(key []uint64, rkeys *[40]uint32) {
	for i := 0; i < 4; i++ {
		rkeys[2*i] = uint32(key[i])
		rkeys[2*i+1] = uint32(key[i] >> 32)
	}

	for i := 8; i < 40; i++ {
		t := rkeys[i-1]
		if i%8 == 0 {
			// 软件格式按大端字存放，字节反转后 RotWord 变为循环右移，rcon 落在低字节。
			t = subw(rotrw(t)) ^ uint32(powx[i/8-1])
		} else if i%8 == 4 {
			t = subw(t)
		}
		rkeys[i] = rkeys[i-8] ^ t
	}
}

// rotrw 与 rotw 方向相反，见 CnExpandKeyGoAsm 中的说明。
func rotrw(w uint32) uint32 { return w>>8 | w<<24 }

// 供测试使用的格式转换：硬件格式轮密钥 = 软件格式轮密钥逐字字节反转。
func reverseKeyWords(dst *[40]uint32, src *[40]uint32) {
	for i := range dst {
		dst[i] = bits.ReverseBytes32(src[i])
	}
}

//go:noescape
func CnRoundsAsm(dst, src *uint64, rkeys *[40]uint32)

//go:noescape
func CnSingleRoundAsm(dst, src *uint64, rkey *[2]uint64)
