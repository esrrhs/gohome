package jh

import (
	"fmt"
	"testing"

	"github.com/esrrhs/gohome/common"
)

func TestJH(t *testing.T) {
	dst := Sum256([]byte("test"))
	fmt.Println(dst)
	// [129 180 125 98 167 71 101 50 112 40 110 224 20 80 180 219 38 53 20 234 153 172 142 100 208 245 249 53 229 101 103 197]
	expected := []byte{129, 180, 125, 98, 167, 71, 101, 50, 112, 40, 110, 224, 20, 80, 180, 219, 38, 53, 20, 234, 153, 172, 142, 100, 208, 245, 249, 53, 229, 101, 103, 197}
	if len(dst) != len(expected) {
		t.Errorf("expected length %d, got %d", len(expected), len(dst))
	}
	for i := range dst {
		if dst[i] != expected[i] {
			t.Errorf("expected byte %d to be %d, got %d", i, expected[i], dst[i])
		}
	}
}

func TestJH_BE(t *testing.T) {
	common.DebugSetBigEndian(true)
	defer common.DebugResetBigEndian()

	dst := Sum256([]byte("test"))
	fmt.Println(dst)
	// [129 180 125 98 167 71 101 50 112 40 110 224 20 80 180 219 38 53 20 234 153 172 142 100 208 245 249 53 229 101 103 197]
	expected := []byte{129, 180, 125, 98, 167, 71, 101, 50, 112, 40, 110, 224, 20, 80, 180, 219, 38, 53, 20, 234, 153, 172, 142, 100, 208, 245, 249, 53, 229, 101, 103, 197}
	if len(dst) != len(expected) {
		t.Errorf("expected length %d, got %d", len(expected), len(dst))
	}
	for i := range dst {
		if dst[i] != expected[i] {
			t.Errorf("expected byte %d to be %d, got %d", i, expected[i], dst[i])
		}
	}
}
