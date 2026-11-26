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

func TestFrameMgrNilDataFrameDoesNotPanic(t *testing.T) {
	fm := NewFrameMgr(64, 1000, 64*1024, 64, 50, 0, 0)
	// Empty wire payload decodes to Frame{Type: Frame_DATA, Id: 0, Data: nil}.
	fm.OnRecvFrame(nil)
	fm.OnRecvFrame(&Frame{Type: int32(Frame_DATA), Id: 0, Data: nil})
	fm.Update()
	if fm.GetRecvBufferSize() != 0 {
		t.Fatalf("expected 0 recv bytes after nil Data frame, got %d", fm.GetRecvBufferSize())
	}
	// Subsequent valid frame id=0 must still be accepted cleanly.
	fm.OnRecvFrame(&Frame{
		Type: int32(Frame_DATA),
		Id:   0,
		Data: &FrameData{Type: int32(FrameData_USER_DATA), Data: []byte("ok")},
	})
	fm.Update()
	if fm.GetRecvBufferSize() != 2 {
		t.Fatalf("expected 2 recv bytes after valid frame, got %d", fm.GetRecvBufferSize())
	}
}

func TestQuicConnCloseUnblocksConcurrentRead(t *testing.T) {
	srvDriver, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := srvDriver.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	accepted := make(chan Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			accepted <- c
		}
	}()

	cliDriver, _ := NewConn("quic")
	cli, err := cliDriver.Dial(ln.(*QuicConn).listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	if _, err := cli.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}

	srv := <-accepted
	buf := make([]byte, 16)
	if _, err := srv.Read(buf); err != nil {
		t.Fatal(err)
	}

	// Server stream blocks on Read while client keeps session open;
	// srv.Close() must cancel the stream's read side and unblock promptly.
	readDone := make(chan error, 1)
	go func() {
		_, rerr := srv.Read(buf)
		readDone <- rerr
	}()
	time.Sleep(30 * time.Millisecond)
	_ = srv.Close()

	select {
	case rerr := <-readDone:
		if rerr == nil {
			t.Fatal("expected blocked Read to return error on Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("QuicConn.Close failed to unblock concurrent Read")
	}
}
