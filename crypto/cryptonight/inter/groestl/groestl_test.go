package groestl

import (
	"fmt"
	"testing"
)

func TestGroestl(t *testing.T) {
	dst := Sum256([]byte("test"))
	fmt.Println(dst)
	// [70 66 23 246 116 157 209 218 148 85 117 146 208 153 23 176 121 243 74 41 148 149 34 171 63 27 61 100 156 8 4 249]
	expected := []byte{70, 66, 23, 246, 116, 157, 209, 218, 148, 85, 117, 146, 208, 153, 23, 176, 121, 243, 74, 41, 148, 149, 34, 171, 63, 27, 61, 100, 156, 8, 4, 249}
	if len(dst) != len(expected) {
		t.Errorf("expected length %d, got %d", len(expected), len(dst))
	}
	for i := range dst {
		if dst[i] != expected[i] {
			t.Errorf("expected byte %d to be %d, got %d", i, expected[i], dst[i])
		}
	}
}
