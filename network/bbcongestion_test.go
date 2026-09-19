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

func TestBBCongestionAckedUpdateClearsFlying(t *testing.T) {
	bb := &BBCongestion{}
	bb.Init()

	pkt := 1024
	for bb.CanSend(0, pkt) {
	}
	bb.RecvAck(0, pkt)
	bb.Update()
	if bb.flyingdata != 0 || bb.flyeddata != 0 {
		t.Fatalf("after acked Update want zeros, flying=%d flyed=%d", bb.flyingdata, bb.flyeddata)
	}
	if !bb.CanSend(1, pkt) {
		t.Fatal("CanSend should work after acked Update cleared flyingdata")
	}
}
