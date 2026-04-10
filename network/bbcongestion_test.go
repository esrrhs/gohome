package network

import (
	"strings"
	"testing"
)

func TestBBCongestionInfo(t *testing.T) {
	bb := &BBCongestion{}
	bb.Init()

	info := bb.Info()
	if info == "" {
		t.Fatal("Info() returned empty string")
	}

	expectedFields := []string{"status", "maxfly", "flyeddata", "lastratewin", "lastflyedwin"}
	for _, field := range expectedFields {
		if !strings.Contains(info, field) {
			t.Errorf("Info() = %q, missing expected field %q", info, field)
		}
	}
}
