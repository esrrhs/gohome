package loggo

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestColor0001(t *testing.T) {
	fmt.Println(FgString("aaa", 255, 0, 0))
	fmt.Println(BgString("aaa", 255, 0, 0))
	fmt.Println(FgString("aaa", 0, 0, 0))
	fmt.Println(BgString("aaa", 0, 0, 0))
}

func TestColorString(t *testing.T) {
	input := "hello"
	result := String(input, 255, 0, 0, 0, 0, 255)
	if len(result) == 0 {
		t.Error("String() returned empty string")
	}
	if !strings.Contains(result, input) {
		t.Errorf("String() result %q does not contain input %q", result, input)
	}
	if len(result) <= len(input) {
		t.Errorf("String() result should be longer than input due to ANSI codes")
	}
}

func TestColorBytes(t *testing.T) {
	input := []byte("world")
	result := Bytes(input, 255, 0, 0, 0, 0, 255)
	if len(result) == 0 {
		t.Error("Bytes() returned empty slice")
	}
	if !bytes.Contains(result, input) {
		t.Errorf("Bytes() result does not contain input %q", input)
	}
	if len(result) <= len(input) {
		t.Errorf("Bytes() result should be longer than input due to ANSI codes")
	}
}

func TestColorFgBytes(t *testing.T) {
	input := []byte("foreground")
	result := FgBytes(input, 0, 255, 0)
	if len(result) == 0 {
		t.Error("FgBytes() returned empty slice")
	}
	if !bytes.Contains(result, input) {
		t.Errorf("FgBytes() result does not contain input %q", input)
	}
	if len(result) <= len(input) {
		t.Errorf("FgBytes() result should be longer than input due to ANSI codes")
	}
}

func TestColorBgBytes(t *testing.T) {
	input := []byte("background")
	result := BgBytes(input, 0, 0, 255)
	if len(result) == 0 {
		t.Error("BgBytes() returned empty slice")
	}
	if !bytes.Contains(result, input) {
		t.Errorf("BgBytes() result does not contain input %q", input)
	}
	if len(result) <= len(input) {
		t.Errorf("BgBytes() result should be longer than input due to ANSI codes")
	}
}

func TestColorFgByte(t *testing.T) {
	result := FgByte('X', 128, 128, 128)
	if len(result) == 0 {
		t.Error("FgByte() returned empty slice")
	}
	if !bytes.Contains(result, []byte{'X'}) {
		t.Errorf("FgByte() result does not contain input byte 'X'")
	}
}

func TestColorBgByte(t *testing.T) {
	result := BgByte('Z', 64, 64, 64)
	if len(result) == 0 {
		t.Error("BgByte() returned empty slice")
	}
	if !bytes.Contains(result, []byte{'Z'}) {
		t.Errorf("BgByte() result does not contain input byte 'Z'")
	}
}
