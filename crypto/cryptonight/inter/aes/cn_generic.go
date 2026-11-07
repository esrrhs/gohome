//go:build !amd64 && !arm64
// +build !amd64,!arm64

package aes

func UseSoft(b bool) {
}

func CnRoundsGo(dst, src []uint64, rkeys *[40]uint32) {
	CnRoundsGoSoft(dst, src, rkeys)
}

func CnExpandKeyGo(key []uint64, rkeys *[40]uint32) {
	CnExpandKeyGoSoft(key, rkeys)
}

func CnSingleRoundGo(dst, src []uint64, rkey *[2]uint64) {
	CnSingleRoundGoSoft(dst, src, rkey)
}
