//go:build amd64
// +build amd64

package aes

import (
	"golang.org/x/sys/cpu"
)

var (
	hasAES = cpu.X86.HasAES
)

func CnExpandKeyGo(key []uint64, rkeys *[40]uint32) {
	if !hasAES {
		CnExpandKeyGoSoft(key, rkeys)
		return
	}
	CnExpandKeyAsm(&key[0], rkeys)
}

func CnRoundsGo(dst, src []uint64, rkeys *[40]uint32) {
	if !hasAES {
		CnRoundsGoSoft(dst, src, rkeys)
		return
	}
	CnRoundsAsm(&dst[0], &src[0], &rkeys[0])
}

func CnSingleRoundGo(dst, src []uint64, rkey *[2]uint64) {
	if !hasAES {
		CnSingleRoundGoSoft(dst, src, rkey)
		return
	}
	CnSingleRoundAsm(&dst[0], &src[0], &rkey[0])
}

//go:noescape
func CnExpandKeyAsm(src *uint64, rkey *[40]uint32)

//go:noescape
func CnRoundsAsm(dst, src *uint64, rkeys *uint32)

//go:noescape
func CnSingleRoundAsm(dst, src *uint64, rkeys *uint64)
