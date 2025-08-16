package aes

import (
	"testing"

	"github.com/esrrhs/gohome/common"
)

func TestCnRoundsGoSoft(t *testing.T) {
	var dst, src [2]uint64
	var rkeys [40]uint32
	src[0] = 0x0123456789abcdef
	src[1] = 0xfedcba9876543210
	for i := range rkeys {
		rkeys[i] = uint32(i + 1)
	}
	CnRoundsGoSoft(dst[:], src[:], &rkeys)
	if dst[0] != 0xbb69757f1833c1a3 || dst[1] != 0x94195ca67f338a90 {
		t.Errorf("got dst[0]=%x, dst[1]=%x", dst[0], dst[1])
	}
}

func TestCnRoundsGoSoft_BE(t *testing.T) {
	common.DebugSetBigEndian(true)
	defer common.DebugResetBigEndian()

	var dst, src [2]uint64
	var rkeys [40]uint32
	src[0] = 0x0123456789abcdef
	src[1] = 0xfedcba9876543210
	for i := range rkeys {
		rkeys[i] = uint32(i + 1)
	}
	CnRoundsGoSoft(dst[:], src[:], &rkeys)
	if dst[0] != 0xbb69757f1833c1a3 || dst[1] != 0x94195ca67f338a90 {
		t.Errorf("got dst[0]=%x, dst[1]=%x", dst[0], dst[1])
	}
}

func TestCnSingleRoundGoSoft(t *testing.T) {
	var dst, src [2]uint64
	var rkeys [2]uint64
	src[0] = 0x0123456789abcdef
	src[1] = 0xfedcba9876543210
	for i := range rkeys {
		rkeys[i] = uint64(i * 1000000000)
	}
	CnSingleRoundGoSoft(dst[:], src[:], &rkeys)
	if dst[0] != 0x6443f5555927d88c || dst[1] != 0x21ff754e10e42996 {
		t.Errorf("got dst[0]=%x, dst[1]=%x", dst[0], dst[1])
	}
}

func TestCnSingleRoundGoSoft_BE(t *testing.T) {
	common.DebugSetBigEndian(true)
	defer common.DebugResetBigEndian()

	var dst, src [2]uint64
	var rkeys [2]uint64
	src[0] = 0x0123456789abcdef
	src[1] = 0xfedcba9876543210
	for i := range rkeys {
		rkeys[i] = uint64(i * 1000000000)
	}
	CnSingleRoundGoSoft(dst[:], src[:], &rkeys)
	if dst[0] != 0x6443f5555927d88c || dst[1] != 0x21ff754e10e42996 {
		t.Errorf("got dst[0]=%x, dst[1]=%x", dst[0], dst[1])
	}
}

func TestCnSingleRoundHeavyGo(t *testing.T) {
	var dst, src [2]uint64
	var rkeys [2]uint64
	src[0] = 0x0123456789abcdef
	src[1] = 0xfedcba9876543210
	for i := range rkeys {
		rkeys[i] = uint64(i * 1000000000)
	}
	CnSingleRoundHeavyGo(dst[:], src[:], &rkeys)
	if dst[0] != 0xc963013a2b7ee396 || dst[1] != 0x25bb10be54f6768 {
		t.Errorf("got dst[0]=%x, dst[1]=%x", dst[0], dst[1])
	}
}

func TestCnSingleRoundHeavyGo_BE(t *testing.T) {
	common.DebugSetBigEndian(true)
	defer common.DebugResetBigEndian()

	var dst, src [2]uint64
	var rkeys [2]uint64
	src[0] = 0x0123456789abcdef
	src[1] = 0xfedcba9876543210
	for i := range rkeys {
		rkeys[i] = uint64(i * 1000000000)
	}
	CnSingleRoundHeavyGo(dst[:], src[:], &rkeys)
	if dst[0] != 0xc963013a2b7ee396 || dst[1] != 0x25bb10be54f6768 {
		t.Errorf("got dst[0]=%x, dst[1]=%x", dst[0], dst[1])
	}
}
