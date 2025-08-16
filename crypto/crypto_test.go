package crypto

import (
	"fmt"
	"testing"

	"github.com/esrrhs/gohome/common"
)

func Test0001(t *testing.T) {
	fmt.Println(Algo())
	if !TestAllSum() {
		t.Error("TestSum fail")
	}
}

func Test0002(t *testing.T) {
	common.DebugSetBigEndian(true)
	defer common.DebugResetBigEndian()

	fmt.Println(Algo())
	if !TestAllSum() {
		t.Error("TestSum fail")
	}
}
