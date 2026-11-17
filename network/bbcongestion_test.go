package network

import (
	"strings"
	"testing"
	"time"
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

// TestBBCongestionRandomLossCompensation ensures that 15-20% random cross-ocean
// packet loss without queue buildup (queueDelay < 70ms) does not cause
// pacingRate to decay to the 4 MB/s floor.
func TestBBCongestionRandomLossCompensation(t *testing.T) {
	bb := &BBCongestion{}
	bb.Init()
	bb.SetRtt(200 * time.Millisecond)

	// Simulate 6 seconds of 17% random packet loss with healthy queue (RTT=215ms, minRTT=200ms).
	simRate := 6 * 1024 * 1024 // start at 6 MB/s wire rate
	for sec := 0; sec < 6; sec++ {
		bb.last = bb.last.Add(-time.Second)
		bb.SetRtt(215 * time.Millisecond)
		if int(bb.pacingRate) > simRate {
			simRate = int(bb.pacingRate)
		}
		acked := int(float64(simRate) * 0.83) // 17% random loss
		bb.totalsent = simRate
		bb.flyingdata = simRate - acked
		bb.flyeddata = acked
		bb.Update()
	}

	const minExpectedPacing = 8 * 1024 * 1024 // must climb past 8 MB/s, never decay to 4 MB/s
	if bb.pacingRate < float64(minExpectedPacing) {
		t.Fatalf("pacingRate decayed under random loss: got %.2f MB/s, want >= %.2f MB/s",
			bb.pacingRate/(1024*1024), float64(minExpectedPacing)/(1024*1024))
	}
}
