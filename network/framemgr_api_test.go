package network

import (
	"math/rand"
	"testing"
	"time"
)

// Focused coverage for the FrameMgr API surface that recently landed
// (GetSendFrames/PutFrames, GetSendWinSize, half-close) without pulling in
// the long reliable-loss suite.

func TestFrameMgrGetSendFramesAndPut(t *testing.T) {
	fm := NewFrameMgr(64, 1000, 64*1024, 64, 50, 0, 0)
	fm.WriteSendBuffer([]byte("hello-send-frames"))
	fm.Update()

	frames := fm.GetSendFrames()
	if len(frames) == 0 {
		t.Fatal("expected staged outbound frames after WriteSendBuffer+Update")
	}
	// Second drain must be empty — GetSendFrames swaps the staging slice out.
	if again := fm.GetSendFrames(); len(again) != 0 {
		t.Fatalf("second GetSendFrames returned %d frames, want 0", len(again))
	}
	PutFrames(frames)
	PutFrames(nil)                     // no-op path
	PutFrames(make([]*Frame, 0))       // cap==0 early return
	PutFrames(make([]*Frame, 0, 8193)) // cap>8192 early return
}

func TestFrameMgrGetSendWinSizeCountsUserData(t *testing.T) {
	fm := NewFrameMgr(64, 1000, 64*1024, 64, 50, 0, 0)
	if n := fm.GetSendWinSize(); n != 0 {
		t.Fatalf("empty win size = %d, want 0", n)
	}
	payload := make([]byte, 64)
	for i := range payload {
		payload[i] = byte(i)
	}
	fm.WriteSendBuffer(payload)
	fm.WriteSendBuffer(payload)
	fm.Update()
	if n := fm.GetSendWinSize(); n != 2 {
		t.Fatalf("GetSendWinSize after 2 full frames = %d, want 2", n)
	}
	// Heartbeat frames are FrameData_HB with empty Data — must not count.
	fm.lastSendHBTime = 0
	fm.hb()
	if n := fm.GetSendWinSize(); n != 2 {
		t.Fatalf("GetSendWinSize after HB = %d, want 2 (HB must not count)", n)
	}
}

func TestFrameMgrHalfCloseAndCloseFlushed(t *testing.T) {
	a := NewFrameMgr(64, 1000, 64*1024, 64, 50, 0, 0)
	b := NewFrameMgr(64, 1000, 64*1024, 64, 50, 0, 0)
	rng := rand.New(rand.NewSource(1))

	if a.CloseFlushed() {
		t.Fatal("CloseFlushed before CloseSend must be false")
	}
	if a.IsRemoteSendClosed() || b.IsRemoteSendClosed() {
		t.Fatal("neither side should see remote half-close yet")
	}

	if err := frameMgrConnect(a, b, 0, 5*time.Second); err != nil {
		t.Fatalf("connect: %v", err)
	}

	a.WriteSendBuffer([]byte("halfclose-payload"))
	a.CloseSend()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.Update()
		b.Update()
		frameMgrExchange(a, b, rng, 0)
		frameMgrExchange(b, a, rng, 0)
		if b.IsRemoteSendClosed() && a.CloseFlushed() {
			break
		}
		time.Sleep(time.Millisecond)
	}

	if !b.IsRemoteSendClosed() {
		t.Fatal("peer did not observe HALFCLOSE (IsRemoteSendClosed=false)")
	}
	if !a.CloseFlushed() {
		t.Fatal("sender CloseFlushed still false after HALFCLOSE ACKed")
	}
	// Recv side must still accept data after half-close.
	b.WriteSendBuffer([]byte("still-open-recv"))
	b.Update()
	if sl := b.GetSendList(); sl.Len() == 0 {
		t.Fatal("half-closed peer should still be able to send")
	}
}
