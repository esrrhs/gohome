package loggo

import (
	"fmt"
	"testing"
)

func TestColor0001(t *testing.T) {
	fmt.Println(FgString("aaa", 255, 0, 0))
	fmt.Println(BgString("aaa", 255, 0, 0))
	fmt.Println(FgString("aaa", 0, 0, 0))
	fmt.Println(BgString("aaa", 0, 0, 0))
}
