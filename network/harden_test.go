package network

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"github.com/esrrhs/gohome/common"
	"github.com/esrrhs/gohome/list"
	"github.com/esrrhs/gohome/thread"
	"google.golang.org/protobuf/proto"
)

// failRW is an in-memory io.ReadWriter whose Write fails on a given ordinal.
type failRW struct {
	in     []byte
	pos    int
	writes int
	failAt int
	err    error
}

func (f *failRW) Read(p []byte) (int, error) {
	if f.pos >= len(f.in) {
		return 0, io.EOF
	}
	n := copy(p, f.in[f.pos:])
	f.pos += n
	return n, nil
}

func (f *failRW) Write(p []byte) (int, error) {
	if f.writes == f.failAt {
		return 0, f.err
	}
	f.writes++
	return len(p), nil
}

var errFailRW = errors.New("failRW injected error")

// scriptedRW separates scripted input bytes from captured reply bytes.
type scriptedRW struct {
	in  *bytes.Buffer
	out *bytes.Buffer
}

func newScriptedRW(in []byte) *scriptedRW {
	return &scriptedRW{in: bytes.NewBuffer(in), out: &bytes.Buffer{}}
}

func (s *scriptedRW) Read(p []byte) (int, error)  { return s.in.Read(p) }
func (s *scriptedRW) Write(p []byte) (int, error) { return s.out.Write(p) }

// ---------- conn.go ----------

func TestHardenRegisterDialerController(t *testing.T) {
	old := gControlOnConnSetup
	t.Cleanup(func() { gControlOnConnSetup = old })

	RegisterDialerController(nil)
	if gControlOnConnSetup != nil {
		t.Fatal("expect nil controller after register nil")
	}
	ctrl := func(network, address string, c syscall.RawConn) error { return nil }
	RegisterDialerController(ctrl)
	if gControlOnConnSetup == nil {
		t.Fatal("expect controller registered")
	}
}

func TestHardenNewConnCaseAndUnknown(t *testing.T) {
	c, err := NewConn("TCP")
	if err != nil {
		t.Fatalf("NewConn(TCP): %v", err)
	}
	if _, ok := c.(*TcpConn); !ok {
		t.Fatalf("NewConn(TCP) = %T, want *TcpConn", c)
	}
	if _, err := NewConn("xxx-proto"); err == nil || !strings.Contains(err.Error(), "undefined proto") {
		t.Fatalf("NewConn(unknown) err = %v, want undefined proto", err)
	}
}

func TestHardenConnZeroValue(t *testing.T) {
	if _, err := (&TcpConn{}).Read(make([]byte, 1)); err == nil {
		t.Fatal("empty TcpConn.Read should fail")
	}
	if _, err := (&TcpConn{}).Write([]byte("x")); err == nil {
		t.Fatal("empty TcpConn.Write should fail")
	}
	if err := (&TcpConn{}).Close(); err != nil {
		t.Fatalf("empty TcpConn.Close: %v", err)
	}
	if got := (&TcpConn{}).Info(); got != "empty tcp conn" {
		t.Fatalf("empty TcpConn.Info = %q", got)
	}
	if _, err := (&TcpConn{}).Accept(); err == nil {
		t.Fatal("TcpConn.Accept without Listen should fail")
	}

	if d, p := (*KcpConfig)(nil).fecParams(); d != 0 || p != 0 {
		t.Fatalf("nil config fecParams = %d,%d", d, p)
	}
	if d, p := (&KcpConfig{DataShards: 0, ParityShards: 1}).fecParams(); d != 0 || p != 0 {
		t.Fatal("non-positive shards should yield 0,0")
	}
	if d, p := (&KcpConfig{DataShards: 200, ParityShards: 57}).fecParams(); d != 0 || p != 0 {
		t.Fatal("shards sum > 256 should yield 0,0")
	}
	if d, p := (&KcpConfig{DataShards: 3, ParityShards: 2}).fecParams(); d != 3 || p != 2 {
		t.Fatalf("valid fecParams = %d,%d", d, p)
	}
	kc := &KcpConn{}
	if _, err := kc.Read(make([]byte, 1)); err == nil {
		t.Fatal("empty KcpConn.Read should fail")
	}
	if _, err := kc.Write([]byte("x")); err == nil {
		t.Fatal("empty KcpConn.Write should fail")
	}
	if err := kc.Close(); err != nil {
		t.Fatalf("empty KcpConn.Close: %v", err)
	}
	if got := kc.Info(); got != "empty kcp conn" {
		t.Fatalf("empty KcpConn.Info = %q", got)
	}

	qc := &QuicConn{}
	if _, err := qc.Read(make([]byte, 1)); err == nil {
		t.Fatal("empty QuicConn.Read should fail")
	}
	if _, err := qc.Write([]byte("x")); err == nil {
		t.Fatal("empty QuicConn.Write should fail")
	}
	if err := qc.Close(); err != nil {
		t.Fatalf("empty QuicConn.Close: %v", err)
	}
	if got := qc.Info(); got != "empty quic conn" {
		t.Fatalf("empty QuicConn.Info = %q", got)
	}
	called := false
	fn := closerFunc(func() error { called = true; return nil })
	if err := fn.Close(); err != nil || !called {
		t.Fatal("closerFunc.Close did not run")
	}
	var nilOwned *ownedCloser
	if err := nilOwned.Close(); err != nil {
		t.Fatalf("nil ownedCloser.Close: %v", err)
	}
	if err := (&ownedCloser{}).Close(); err != nil {
		t.Fatalf("nil-inner ownedCloser.Close: %v", err)
	}

	uc := &UdpConn{}
	if _, err := uc.Read(make([]byte, 1)); err == nil {
		t.Fatal("empty UdpConn.Read should fail")
	}
	if _, err := uc.Write([]byte("x")); err == nil {
		t.Fatal("empty UdpConn.Write should fail")
	}
	if err := uc.Close(); err != nil {
		t.Fatalf("empty UdpConn.Close: %v", err)
	}
	if got := uc.Info(); got != "empty udp conn" {
		t.Fatalf("empty UdpConn.Info = %q", got)
	}
	uc.SetConfig(DefaultUdpConfig())
	if uc.GetConfig() == nil {
		t.Fatal("UdpConn.GetConfig after SetConfig nil")
	}

	if got := (&RudpConn{}).Info(); got != "empty rudp conn" {
		t.Fatalf("empty RudpConn.Info = %q", got)
	}
	if (&RudpConn{}).GetConfig() == nil {
		t.Fatal("RudpConn.GetConfig should default")
	}
	if got := (&RhttpConn{}).Info(); got != "empty http conn" {
		t.Fatalf("empty RhttpConn.Info = %q", got)
	}
	if (&RhttpConn{}).GetConfig() == nil {
		t.Fatal("RhttpConn.GetConfig should default")
	}
	if got := (&RicmpConn{id: "x"}).Info(); got != "empty ricmp conn" {
		t.Fatalf("empty RicmpConn.Info = %q", got)
	}
	if (&RicmpConn{}).GetConfig() == nil {
		t.Fatal("RicmpConn.GetConfig should default")
	}
}

func TestHardenTcpConnRoundTripAndController(t *testing.T) {
	nln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer nln.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		a, aerr := nln.Accept()
		if aerr == nil {
			accepted <- a
		}
	}()

	old := gControlOnConnSetup
	ctrlCalled := 0
	RegisterDialerController(func(network, address string, c syscall.RawConn) error {
		ctrlCalled++
		return nil
	})
	t.Cleanup(func() { gControlOnConnSetup = old })

	d := &TcpConn{}
	dc, err := d.Dial(nln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	if ctrlCalled == 0 {
		t.Fatal("dialer Control callback was not invoked")
	}
	if got := dc.Info(); !strings.Contains(got, "<--tcp-->") {
		t.Fatalf("dialed Info = %q", got)
	}

	srv := <-accepted
	defer srv.Close()
	if _, err := dc.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(srv, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("server read = %q err=%v", buf, err)
	}
	if _, err := srv.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(dc, buf); err != nil || string(buf) != "pong" {
		t.Fatalf("client read = %q err=%v", buf, err)
	}

	// Listener Info branch.
	lc, err := (&TcpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lc.Close()
	if got := lc.Info(); !strings.HasPrefix(got, "tcp--") {
		t.Fatalf("listener Info = %q", got)
	}
}

func TestHardenRhttpConnLocalEdges(t *testing.T) {
	if _, err := (&RhttpConn{}).Read(make([]byte, 1)); err == nil || err.Error() != "empty conn" {
		t.Fatalf("empty RhttpConn.Read err = %v", err)
	}
	if _, err := (&RhttpConn{}).Write([]byte("x")); err == nil || err.Error() != "empty conn" {
		t.Fatalf("empty RhttpConn.Write err = %v", err)
	}
	if _, err := (&RhttpConn{}).Read(nil); err == nil || err.Error() != "read empty buffer" {
		t.Fatalf("nil-buffer RhttpConn.Read err = %v", err)
	}
	if _, err := (&RhttpConn{}).Write(nil); err == nil || err.Error() != "write empty data" {
		t.Fatalf("nil-data RhttpConn.Write err = %v", err)
	}

	closed := &RhttpConn{}
	closed.isclose.Store(true)
	if _, err := closed.Read(make([]byte, 1)); err == nil || err.Error() != "read closed conn" {
		t.Fatalf("closed RhttpConn.Read err = %v", err)
	}
	if _, err := closed.Write([]byte("x")); err == nil || err.Error() != "write closed conn" {
		t.Fatalf("closed RhttpConn.Write err = %v", err)
	}

	lc := &RhttpConn{listener: &httpConnListener{}}
	if _, err := lc.Read(make([]byte, 1)); err == nil || err.Error() != "listener can not be read" {
		t.Fatalf("listener RhttpConn.Read err = %v", err)
	}
	if _, err := lc.Write([]byte("x")); err == nil || err.Error() != "listener can not be write" {
		t.Fatalf("listener RhttpConn.Write err = %v", err)
	}
}

// ---------- bbcongestion.go ----------

func TestHardenBBCPanics(t *testing.T) {
	bb := &BBCongestion{}
	bb.Init()

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("RecvAck(-1) should panic")
			}
		}()
		bb.RecvAck(0, -1)
	}()

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("CanSend(-1) should panic")
			}
		}()
		bb.CanSend(0, -1)
	}()

	bb.RecvAck(0, 1)
	bb.status = 9
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Update with bad status should panic")
			}
		}()
		bb.Update()
	}()
}

func TestHardenBBCStateMachine(t *testing.T) {
	bb := &BBCongestion{}
	bb.Init()

	// No acked data: Update is a no-op.
	bb.Update()
	if bb.status != bbc_status_init {
		t.Fatal("unexpected status change without flyed data")
	}

	bb.RecvAck(0, 1000)
	if !bb.CanSend(0, 500) {
		t.Fatal("CanSend under maxfly should be true")
	}
	grown := bb.maxfly
	bb.Update() // first sample beats lastflyeddata=0: grow, still init
	if bb.status != bbc_status_init || bb.maxfly <= grown {
		t.Fatalf("expect init grow, status=%d maxfly=%d", bb.status, bb.maxfly)
	}

	// Stalled acked data relative to previous window: shrink and enter prop.
	bb.lastflyeddata = 100 * 1024 * 1024
	bb.RecvAck(0, 1)
	bb.Update()
	if bb.status != bbc_status_prop {
		t.Fatalf("expect prop status, got %d", bb.status)
	}

	for i := 0; i < 6; i++ {
		bb.RecvAck(0, 1024)
		if !bb.CanSend(0, 1024) {
			t.Fatal("CanSend should be allowed after update reset flying")
		}
		bb.Update()
	}

	// prop branch: bandwidth product above maxfly raises the window.
	grow := &BBCongestion{}
	grow.Init()
	grow.status = bbc_status_prop
	grow.maxfly = 10
	grow.RecvAck(0, 100)
	if !grow.CanSend(0, 100) {
		t.Fatal("setup CanSend")
	}
	grow.Update()
	if grow.maxfly <= 10 {
		t.Fatalf("prop grow maxfly = %d, want > 10", grow.maxfly)
	}

	// prop branch: product equal to maxfly (NearlyEqual) replaces it.
	eq := &BBCongestion{}
	eq.Init()
	eq.status = bbc_status_prop
	eq.maxfly = 10
	eq.RecvAck(0, 10)
	if !eq.CanSend(0, 10) {
		t.Fatal("setup CanSend")
	}
	eq.Update()
	if eq.maxfly != 1024*1024 {
		t.Fatalf("prop equal then floor maxfly = %d", eq.maxfly)
	}

	// maxfly floor: prop result below 1MB must be clamped up.
	bb.maxfly = 500 * 1024
	bb.propindex = len(prop_seq) - 1
	bb.flyeddata = 1
	bb.flyingdata = 0
	bb.Update()
	if bb.maxfly != 1024*1024 {
		t.Fatalf("maxfly floor = %d, want 1MB", bb.maxfly)
	}
}

// ---------- framemgr.go statistics / branches ----------

func TestHardenFrameMgrStat(t *testing.T) {
	now := time.Now().UnixNano()
	fm := NewFrameMgr(64, 100, 4096, 16, 50, 0, 1)
	if fm.fs == nil || fm.fs.sendDataNumsMap == nil {
		t.Fatal("openstat=1 must allocate FrameStat")
	}

	// Put one data frame into the send window for REQ/ACK lookup.
	fm.WriteSendBuffer([]byte("hello"))
	fm.cutSendBufferToWindow(now)
	if got := fm.sendwin.Size(); got != 1 {
		t.Fatalf("sendwin size = %d, want 1", got)
	}

	fm.OnRecvFrame(&Frame{Type: int32(Frame_REQ), Dataid: []int32{0}})
	fm.OnRecvFrame(recvDataFrame(0, []byte("x")))
	fm.OnRecvFrame(&Frame{Type: int32(Frame_ACK), Dataid: []int32{0}})
	fm.OnRecvFrame(&Frame{Type: int32(Frame_PING), Sendtime: now - int64(time.Millisecond)})
	fm.OnRecvFrame(&Frame{Type: int32(Frame_PONG), Sendtime: now - int64(time.Millisecond)})
	fm.OnRecvFrame(&Frame{Type: 12345}) // unknown frame type
	// Out-of-order arrival after frame 0 is consumed produces REQ frames.
	fm.OnRecvFrame(recvDataFrame(3, []byte("yyy")))
	fm.Update()

	fs := fm.fs
	if fs.recvReqNum != 1 || fs.recvAckNum != 1 || fs.recvDataNum != 2 {
		t.Fatalf("recv counters req=%d ack=%d data=%d", fs.recvReqNum, fs.recvAckNum, fs.recvDataNum)
	}
	if fs.sendAckNum != 2 {
		t.Fatalf("sendAckNum = %d, want 2", fs.sendAckNum)
	}
	if fs.sendReqNum == 0 {
		t.Fatal("hole after frame 0 must trigger REQ")
	}
	if fs.recvping != 1 || fs.sendpong != 1 || fs.recvpong != 1 {
		t.Fatalf("ping/pong counters %d/%d/%d", fs.recvping, fs.sendpong, fs.recvpong)
	}

	// Old and out-of-window frames exercise addToRecvWin branches.
	fm2 := NewFrameMgr(64, 100, 4096, 16, 50, 0, 1)
	if !fm2.addToRecvWin(recvDataFrame(99, []byte("old"))) {
		t.Error("frame in old window should be acked (true)")
	}
	if fm2.fs.recvOldNum != 1 {
		t.Errorf("recvOldNum = %d", fm2.fs.recvOldNum)
	}
	if fm2.addToRecvWin(recvDataFrame(50, []byte("future"))) {
		t.Error("frame beyond recv window must be rejected")
	}
	if fm2.fs.recvOutWinNum != 1 {
		t.Errorf("recvOutWinNum = %d", fm2.fs.recvOutWinNum)
	}

	// second() prints stats, resets them and drives the congestion updater.
	bb := &BBCongestion{}
	fm.SetCongestion(bb)
	fm.lastPrintStat = 0
	fm.second(time.Now().UnixNano())
	if fm.fs.recvDataNum != 0 {
		t.Fatal("resetStat after print should zero counters")
	}

	if got := fm.printStatMap(&map[int32]int{}); got != "none" {
		t.Fatalf("empty stat map = %q, want none", got)
	}
	// Two ids with count 2, one id with count 1: histogram "1->1,2->2,".
	m := map[int32]int{1: 2, 3: 2, 9: 1}
	got := fm.printStatMap(&m)
	if !strings.Contains(got, "1->1,") || !strings.Contains(got, "2->2,") {
		t.Fatalf("printStatMap = %q", got)
	}
}

func TestHardenFrameMgrCompressCut(t *testing.T) {
	// Full-frame and tail-frame zstd compression branches.
	fm := NewFrameMgr(64, 100, 4096, 16, 50, 1, 0)
	fm.WriteSendBuffer(make([]byte, 128))
	fm.cutSendBufferToWindow(time.Now().UnixNano())

	var compressed int
	for e := fm.sendwin.FrontInter(); e != nil; e = e.Next() {
		f := e.Value.(*Frame)
		if f.Data.Compress {
			compressed++
		}
	}
	if compressed == 0 {
		t.Fatal("expected at least one compressed full frame")
	}

	fmTail := NewFrameMgr(64, 100, 4096, 16, 50, 1, 0)
	fmTail.WriteSendBuffer(make([]byte, 63))
	fmTail.cutSendBufferToWindow(time.Now().UnixNano())
	err, v := fmTail.sendwin.Front()
	if err != nil || v.(*Frame).Data.Compress != true {
		t.Fatalf("tail frame compression, err=%v", err)
	}
}

func TestHardenFrameMgrRecvFrameTypes(t *testing.T) {
	fm := NewFrameMgr(64, 100, 4096, 16, 50, 0, 0)

	// Corrupt compressed payload is rejected without touching recvb.
	bad := recvDataFrame(0, []byte("not-a-zstd-frame"))
	bad.Data.Compress = true
	if fm.processRecvFrame(bad) {
		t.Fatal("corrupt zstd frame must be rejected")
	}

	// Decompressed payload larger than the buffer left is rejected.
	small := NewFrameMgr(64, 100, 10, 16, 50, 0, 0)
	small.WriteSendBuffer(bytes.Repeat([]byte("a"), 9))
	zb := common.CompressDataZstd(bytes.Repeat([]byte("a"), 20))
	big := recvDataFrame(0, zb)
	big.Data.Compress = true
	if small.processRecvFrame(big) {
		t.Fatal("oversubscribed recv buffer after decompress must be rejected")
	}

	// Control frame types.
	if !fm.processRecvFrame(&Frame{Type: int32(Frame_DATA), Data: &FrameData{Type: int32(FrameData_CLOSE)}}) {
		t.Fatal("CLOSE frame should be accepted")
	}
	if !fm.IsRemoteClosed() {
		t.Fatal("CLOSE frame should mark remoteclosed")
	}
	if !fm.processRecvFrame(&Frame{Type: int32(Frame_DATA), Id: 7, Data: &FrameData{Type: int32(FrameData_CONN)}}) {
		t.Fatal("CONN frame should be accepted")
	}
	if !fm.IsConnected() {
		t.Fatal("CONN frame should connect and queue CONNRSP")
	}
	if !fm.processRecvFrame(&Frame{Type: int32(Frame_DATA), Data: &FrameData{Type: int32(FrameData_CONNRSP)}}) {
		t.Fatal("CONNRSP frame should be accepted")
	}
	if !fm.processRecvFrame(&Frame{Type: int32(Frame_DATA), Data: &FrameData{Type: int32(FrameData_HB)}}) {
		t.Fatal("HB frame should be accepted")
	}
	if fm.processRecvFrame(&Frame{Type: int32(Frame_DATA), Data: &FrameData{Type: 99}}) {
		t.Fatal("unknown data type must be rejected")
	}
}

func TestHardenFrameMgrWindowFullAndWrap(t *testing.T) {
	fm := NewFrameMgr(64, 4, 4096, 2, 50, 0, 0)
	fm.Connect()
	fm.Connect()
	fm.Connect() // window full: error branch
	if fm.sendwin.Size() != 2 {
		t.Fatalf("sendwin size = %d, want 2", fm.sendwin.Size())
	}
	// Incoming CONN triggers sendConnectRsp, which also finds a full window.
	fm.processRecvFrame(&Frame{Type: int32(Frame_DATA), Id: 5, Data: &FrameData{Type: int32(FrameData_CONN)}})

	// sendid wrap on Connect / sendConnectRsp / hb. The ring begin must be
	// advanced (frames acked and popped) before id 7 is a valid window slot.
	advanceWindow := func() *FrameMgr {
		m := NewFrameMgr(10, 8, 4096, 4, 50, 0, 0)
		m.WriteSendBuffer(make([]byte, 40))
		m.cutSendBufferToWindow(time.Now().UnixNano())
		m.processRecvList(nil, map[int32]int{0: 1, 1: 1, 2: 1, 3: 1}, nil)
		if m.sendwin.Size() != 0 {
			t.Fatalf("setup: sendwin not drained, size %d", m.sendwin.Size())
		}
		return m
	}
	fw := advanceWindow()
	fw.sendid = 7
	fw.Connect()
	if fw.sendid != 0 {
		t.Fatalf("Connect wrap sendid = %d, want 0", fw.sendid)
	}
	fw = advanceWindow()
	fw.sendid = 7
	fw.sendConnectRsp()
	if fw.sendid != 0 {
		t.Fatalf("sendConnectRsp wrap sendid = %d", fw.sendid)
	}
	fw = advanceWindow()
	fw.sendid = 7
	fw.lastSendHBTime = 0
	fw.hb()
	if fw.sendid != 0 {
		t.Fatalf("hb wrap sendid = %d, want 0", fw.sendid)
	}

	// ping() with stale timestamp emits a PING and counts it under openstat.
	fs := NewFrameMgr(64, 8, 4096, 4, 50, 0, 1)
	fs.lastPingTime = 0
	fs.ping()
	if fs.fs.sendping != 1 {
		t.Fatalf("sendping = %d, want 1", fs.fs.sendping)
	}
}

func TestHardenFrameMgrCongestionSendList(t *testing.T) {
	cur := time.Now().UnixNano()

	// Congestion rejects the resend: frame id is parked as ctLastSendId.
	fm := NewFrameMgr(64, 100, 4096, 16, 50, 0, 1)
	fm.WriteSendBuffer([]byte("hello"))
	fm.cutSendBufferToWindow(cur)
	bb := &BBCongestion{}
	fm.SetCongestion(bb)
	bb.maxfly = 1
	bb.flyingdata = 2
	fm.calSendList(cur + int64(time.Second))
	if fm.ctLastSendId != 0 {
		t.Fatalf("ctLastSendId = %d, want parked 0", fm.ctLastSendId)
	}

	// Frame gets ACKed out of the window: parked cursor resets.
	fm.processRecvList(nil, map[int32]int{0: 1}, nil)
	fm.calSendList(cur + int64(2*time.Second))
	if fm.ctLastSendId != -1 {
		t.Fatalf("ctLastSendId = %d, want reset -1", fm.ctLastSendId)
	}

	// Congestion allows the resend: send statistic increments.
	fm2 := NewFrameMgr(64, 100, 4096, 16, 50, 0, 1)
	fm2.WriteSendBuffer([]byte("hello"))
	fm2.cutSendBufferToWindow(cur)
	fm2.SetCongestion(&BBCongestion{})
	fm2.calSendList(cur + int64(time.Second))
	if fm2.fs.sendDataNum != 1 {
		t.Fatalf("sendDataNum = %d, want 1", fm2.fs.sendDataNum)
	}
}

func TestHardenFrameMgrRejectsInvalidRingParams(t *testing.T) {
	// NewROBuffer returns nil when windowsize >= frame_max_id or maxid <= 0,
	// which used to surface as an obscure nil-pointer panic on the first
	// operation. Construction itself must reject these parameters.
	cases := []struct {
		maxID int
		win   int
	}{
		{0, 4},  // non-positive frame_max_id
		{-1, 4}, // negative frame_max_id
		{4, 4},  // window equal to id space
		{4, 5},  // window larger than id space
		{8, 0},  // non-positive window
		{8, -2}, // negative window
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("NewFrameMgr(maxid=%d, win=%d) should reject invalid ring params", tc.maxID, tc.win)
				}
			}()
			NewFrameMgr(64, tc.maxID, 4096, tc.win, 50, 0, 0)
		}()
	}
}

func TestHardenFrameMgrBufferAccessorsAndClose(t *testing.T) {
	fm := NewFrameMgr(64, 100, 4096, 16, 50, 0, 0)
	fm.SetDebugid("harden")
	fm.WriteSendBuffer([]byte("line1\nline2"))
	if left := fm.GetSendBufferLeft(); left != 4096-11 {
		t.Fatalf("send buffer left = %d", left)
	}

	fm.OnRecvFrame(recvDataFrame(0, []byte("abc\nxyz")))
	fm.Update()
	if size := fm.GetRecvBufferSize(); size != 7 {
		t.Fatalf("recv buffer size = %d, want 7", size)
	}
	line := fm.GetRecvReadLineBuffer()
	if string(line) != "abc\nxyz" {
		t.Fatalf("readline = %q", line)
	}
	fm.SkipRecvBuffer(4)
	if size := fm.GetRecvBufferSize(); size != 3 {
		t.Fatalf("recv buffer size after skip = %d, want 3", size)
	}

	fm.Close()
	if !fm.close {
		t.Fatal("Close should set close flag")
	}
	if fm.IsHBTimeout() {
		t.Fatal("fresh FrameMgr must not be HB timeout")
	}
	fm.lastRecvHBTime = 0
	fm.lastRecvDataTime = 0
	if !fm.IsHBTimeout() {
		t.Fatal("stale HB/data timestamps must be timeout")
	}

	// Trailing ACK flush (fewer accepted frames than the batch limit).
	fm2 := NewFrameMgr(256, 100, 4096, 16, 50, 0, 0)
	fm2.OnRecvFrame(recvDataFrame(0, []byte("a")))
	fm2.OnRecvFrame(recvDataFrame(1, []byte("bb")))
	fm2.Update()
	var ack *Frame
	for e := fm2.GetSendList().Front(); e != nil; e = e.Next() {
		if f := e.Value.(*Frame); f.Type == int32(Frame_ACK) {
			ack = f
		}
	}
	if ack == nil || len(ack.Dataid) != 2 {
		t.Fatalf("trailing ACK = %v", ack)
	}
}

func TestHardenFrameMgrCutWrapAndCloseFrame(t *testing.T) {
	cur := time.Now().UnixNano()

	// Full-frame cut wraps sendid; tail cut then wraps too.
	fm := NewFrameMgr(10, 8, 4096, 4, 50, 0, 0)
	fm.WriteSendBuffer(make([]byte, 40))
	fm.cutSendBufferToWindow(cur) // ids 0..3
	fm.processRecvList(nil, map[int32]int{0: 1, 1: 1, 2: 1, 3: 1}, nil)
	if fm.sendwin.Size() != 0 || fm.sendid != 4 {
		t.Fatalf("setup size=%d sendid=%d", fm.sendwin.Size(), fm.sendid)
	}
	fm.WriteSendBuffer(make([]byte, 30))
	fm.cutSendBufferToWindow(cur) // full frames 4,5,6 -> sendid 7
	fm.WriteSendBuffer(make([]byte, 5))
	fm.cutSendBufferToWindow(cur) // tail id 7, wraps sendid
	if fm.sendid != 0 {
		t.Fatalf("sendid after tail wrap = %d, want 0", fm.sendid)
	}

	// CLOSE frame is cut once the send buffer drains, also wrapping sendid.
	fc := NewFrameMgr(10, 8, 4096, 4, 50, 0, 0)
	fc.WriteSendBuffer(make([]byte, 40))
	fc.cutSendBufferToWindow(cur)
	fc.processRecvList(nil, map[int32]int{0: 1, 1: 1, 2: 1, 3: 1}, nil)
	fc.sendid = 7
	fc.Close()
	fc.cutSendBufferToWindow(cur)
	if !fc.closesend {
		t.Fatal("CLOSE frame should be queued and closesend set")
	}
	err, v := fc.sendwin.Get(7)
	if err != nil || v.(*Frame).Data.Type != int32(FrameData_CLOSE) {
		t.Fatalf("close frame at id7 err=%v", err)
	}
}

func TestHardenFrameMgrRecvCompressAndOverflow(t *testing.T) {
	// Valid compressed frame round-trips into the recv buffer.
	zb := common.CompressDataZstd([]byte("hello world"))
	f := recvDataFrame(0, zb)
	f.Data.Compress = true
	fm := NewFrameMgr(64, 100, 4096, 16, 50, 0, 0)
	if !fm.processRecvFrame(f) {
		t.Fatal("valid compressed frame should be accepted")
	}
	if fm.GetRecvBufferSize() != len("hello world") {
		t.Fatalf("recv buffer size = %d", fm.GetRecvBufferSize())
	}

	// Plain frame larger than the remaining buffer space is rejected.
	full := NewFrameMgr(64, 100, 10, 16, 50, 0, 0)
	full.recvb.Write(make([]byte, 10))
	if full.processRecvFrame(recvDataFrame(0, []byte("x"))) {
		t.Fatal("frame over full recv buffer must be rejected")
	}

	// REQ/ACK for ids absent from the send window are dropped without panic.
	fm.OnRecvFrame(&Frame{Type: int32(Frame_REQ), Dataid: []int32{40}})
	fm.OnRecvFrame(&Frame{Type: int32(Frame_ACK), Dataid: []int32{41}})
	fm.Update()
}

func TestHardenFrameMgrRecvRingWrap(t *testing.T) {
	cur := time.Now().UnixNano()
	fm := NewFrameMgr(64, 16, 4096, 8, 50, 0, 0)
	fm.rttns = int64(time.Millisecond)

	// Deliver one frame at a time so the recv window slides: consume 0..12.
	for i := int32(0); i < 13; i++ {
		fm.OnRecvFrame(recvDataFrame(i, []byte("x")))
		fm.Update()
	}
	if fm.recvid != 13 {
		t.Fatalf("recvid = %d, want 13", fm.recvid)
	}

	// Late arrival at id 0 wraps the window; holes 13,14,15 are REQ'd across
	// the ring boundary.
	if err := fm.recvwin.Set(0, recvDataFrame(0, []byte("x"))); err != nil {
		t.Fatal(err)
	}
	fm.combineWindowToRecvBuffer(cur + int64(time.Second))
	ids := reqIdsFromSendList(fm.GetSendList())
	for _, want := range []int32{13, 14, 15} {
		if _, ok := ids[want]; !ok {
			t.Fatalf("missing wrap-around REQ %d: %v", want, ids)
		}
	}

	// Arrival of 13,14,15 drains through, then recvid wraps consuming id 0.
	for i := int32(13); i < 16; i++ {
		fm.OnRecvFrame(recvDataFrame(i, []byte("x")))
	}
	fm.Update()
	if fm.recvid != 1 {
		t.Fatalf("recvid after full wrap drain = %d, want 1", fm.recvid)
	}

	// REQ budget exhaustion breaks the hole walk early.
	small := NewFrameMgr(8, 100, 4096, 16, 50, 0, 0)
	small.rttns = int64(time.Millisecond)
	if err := small.recvwin.Set(3, recvDataFrame(3, []byte("x"))); err != nil {
		t.Fatal(err)
	}
	small.combineWindowToRecvBuffer(cur)
	if ids := reqIdsFromSendList(small.GetSendList()); len(ids) != 1 {
		t.Fatalf("budget-limited REQs = %v, want exactly 1", ids)
	}
}

func TestHardenFrameMgrIsIdOldNonWrap(t *testing.T) {
	fm := NewFrameMgr(64, 100, 4096, 4, 50, 0, 0)
	fm.recvid = 8 // begin = recvid - windowsize = 4 < end: non-wrap branch
	if !fm.isIdOld(5, 100) {
		t.Fatal("id 5 should be old")
	}
	if fm.isIdOld(9, 100) {
		t.Fatal("id 9 must not be old")
	}
}

func TestHardenFrameMgrCongestionCursor(t *testing.T) {
	cur := time.Now().UnixNano()
	fm := NewFrameMgr(64, 100, 4096, 16, 50, 0, 1)
	fm.WriteSendBuffer(make([]byte, 128))
	fm.cutSendBufferToWindow(cur)
	err, v0 := fm.sendwin.Get(0)
	if err != nil {
		t.Fatal(err)
	}
	err, v1 := fm.sendwin.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	bb := &BBCongestion{}
	fm.SetCongestion(bb)
	bb.maxfly = 1
	bb.flyingdata = 2
	later := cur + int64(time.Second)
	// Frame 0 stamped "now" (not due yet), frame 1 old and blocked: park on 1.
	v0.(*Frame).Sendtime = later
	v1.(*Frame).Sendtime = 0
	fm.calSendList(later)
	if fm.ctLastSendId != 1 {
		t.Fatalf("parked id = %d, want 1", fm.ctLastSendId)
	}

	// Unblock: parked cursor skips frame 0 and resends frame 1.
	bb.maxfly = 1024 * 1024
	bb.flyingdata = 0
	fm.calSendList(later)
	if fm.ctLastSendId != -1 || fm.fs.sendDataNum != 1 {
		t.Fatalf("after unblock cursor=%d sendData=%d", fm.ctLastSendId, fm.fs.sendDataNum)
	}
}

func TestHardenFrameMgrMarshal(t *testing.T) {
	fm := NewFrameMgr(64, 100, 4096, 16, 50, 0, 0)
	f := &Frame{Type: int32(Frame_DATA), Resend: true, Sendtime: 12345, Id: 7,
		Data: &FrameData{Type: int32(FrameData_USER_DATA), Data: []byte("abc")}}
	mb, err := fm.MarshalFrame(f)
	if err != nil {
		t.Fatal(err)
	}
	var got Frame
	if err := proto.Unmarshal(mb, &got); err != nil {
		t.Fatal(err)
	}
	if got.Id != 7 || string(got.Data.Data) != "abc" {
		t.Fatalf("unmarshal mismatch: %v", &got)
	}
}

// ---------- socks5 server side ----------

func TestHardenSock5HandshakeByEdges(t *testing.T) {
	if err := Sock5HandshakeBy(nil, "", ""); err == nil || !strings.Contains(err.Error(), "nil conn") {
		t.Fatalf("nil conn err = %v", err)
	}

	// Truncated method header.
	if err := Sock5HandshakeBy(bytes.NewBuffer(nil), "", ""); err == nil {
		t.Fatal("truncated header should fail")
	}

	// NMETHODS=0: no method can be offered.
	rw := newScriptedRW([]byte{5, 0})
	if err := Sock5HandshakeBy(rw, "", ""); err != errMethod {
		t.Fatalf("nmethods=0 err = %v, want errMethod", err)
	}
	if !bytes.Equal(rw.out.Bytes(), []byte{5, 0xff}) {
		t.Fatalf("reply = %v, want FF", rw.out.Bytes())
	}

	// Declared methods truncated.
	if err := Sock5HandshakeBy(newScriptedRW([]byte{5, 2}), "u", "p"); err == nil {
		t.Fatal("truncated methods should fail")
	}
	// Auth method selected, then truncated sub-negotiation reads.
	if err := Sock5HandshakeBy(newScriptedRW([]byte{5, 1, 2}), "u", "p"); err == nil {
		t.Fatal("truncated auth head should fail")
	}
	if err := Sock5HandshakeBy(newScriptedRW([]byte{5, 1, 2, 0, 1, 'u', 1, 'p'}), "u", "p"); err == nil {
		t.Fatal("bad auth version should fail")
	}
	if err := Sock5HandshakeBy(newScriptedRW([]byte{5, 1, 2, 1, 2}), "u", "p"); err == nil {
		t.Fatal("truncated username should fail")
	}
	if err := Sock5HandshakeBy(newScriptedRW([]byte{5, 1, 2, 1, 0}), "u", "p"); err == nil {
		t.Fatal("truncated password length should fail")
	}
	if err := Sock5HandshakeBy(newScriptedRW([]byte{5, 1, 2, 1, 0, 1}), "u", "p"); err == nil {
		t.Fatal("truncated password should fail")
	}

	// Zero-length username from wire mismatches the configured username.
	if err := Sock5HandshakeBy(newScriptedRW([]byte{5, 1, 2, 1, 0, 1, 'p'}), "u", "p"); err != errAuth {
		t.Fatalf("empty wire user err = %v", err)
	}
	// Zero-length password from wire mismatches the configured password.
	if err := Sock5HandshakeBy(newScriptedRW([]byte{5, 1, 2, 1, 1, 'u', 0}), "u", "p"); err != errAuth {
		t.Fatalf("empty wire pass err = %v", err)
	}

	// Successful no-auth reply write failure propagates.
	if err := Sock5HandshakeBy(&failRW{in: []byte{5, 1, 0}, failAt: 0, err: errFailRW}, "", ""); !errors.Is(err, errFailRW) {
		t.Fatalf("no-auth write fail err = %v", err)
	}
	// Method selection reply write failure in userpass mode propagates.
	if err := Sock5HandshakeBy(&failRW{in: []byte{5, 1, 2}, failAt: 0, err: errFailRW}, "u", "p"); !errors.Is(err, errFailRW) {
		t.Fatalf("method write fail err = %v", err)
	}
	// Final auth-success reply write failure propagates.
	if err := Sock5HandshakeBy(&failRW{in: []byte{5, 1, 2, 1, 1, 'u', 1, 'p'}, failAt: 1, err: errFailRW}, "u", "p"); !errors.Is(err, errFailRW) {
		t.Fatalf("auth result write fail err = %v", err)
	}
}

func TestHardenSock5GetRequestEdges(t *testing.T) {
	if _, _, _, err := Sock5GetRequest(nil); err == nil || !strings.Contains(err.Error(), "nil conn") {
		t.Fatalf("nil conn err = %v", err)
	}
	if _, _, _, err := Sock5GetRequest(bytes.NewBuffer(nil)); err == nil {
		t.Fatal("truncated header should fail")
	}
	if _, _, _, err := Sock5GetRequest(bytes.NewBuffer([]byte{5, 1, 0, 1, 1, 2, 3})); err == nil {
		t.Fatal("truncated ipv4 addr should fail")
	}
	if _, _, _, err := Sock5GetRequest(bytes.NewBuffer(append([]byte{5, 1, 0, 4}, make([]byte, 10)...))); err == nil {
		t.Fatal("truncated ipv6 addr should fail")
	}
	if _, _, _, err := Sock5GetRequest(bytes.NewBuffer([]byte{5, 1, 0, 3})); err == nil {
		t.Fatal("missing domain length should fail")
	}
	if _, _, _, err := Sock5GetRequest(bytes.NewBuffer([]byte{5, 1, 0, 3, 0})); err == nil {
		t.Fatal("zero-length domain should fail")
	}
	if _, _, _, err := Sock5GetRequest(bytes.NewBuffer([]byte{5, 1, 0, 3, 3, 'a', 'b'})); err == nil {
		t.Fatal("truncated domain body should fail")
	}
	if _, _, _, err := Sock5GetRequest(bytes.NewBuffer([]byte{5, 1, 0, 9})); err != errAddrType {
		t.Fatalf("unknown atyp err = %v, want errAddrType", err)
	}
	if _, _, _, err := Sock5GetRequest(bytes.NewBuffer([]byte{5, 1, 0, 1, 1, 2, 3, 4})); err == nil {
		t.Fatal("missing port should fail")
	}

	// IPv6 happy path with full address + port.
	wire := append([]byte{5, 1, 0, 4}, make([]byte, 16)...)
	wire = append(wire, 0x1f, 0x90)
	cmd, raw, host, err := Sock5GetRequest(bytes.NewBuffer(wire))
	if err != nil || cmd != Socks5CmdConnect || host != "[::]:8080" {
		t.Fatalf("ipv6 request cmd=%d host=%q err=%v", cmd, host, err)
	}
	if len(raw) != 19 || raw[0] != Socks5AtypIP6 {
		t.Fatalf("ipv6 rawaddr len=%d: %v", len(raw), raw)
	}

	// Domain happy path.
	wire = []byte{5, 1, 0, 3, 3, 'f', 'o', 'o', 0x00, 0x50}
	_, raw, host, err = Sock5GetRequest(bytes.NewBuffer(wire))
	if err != nil || host != "foo:80" || string(raw[2:5]) != "foo" {
		t.Fatalf("domain request host=%q raw=%v err=%v", host, raw, err)
	}
}

// TestHardenSock5DomainHostMustBeDialable 是 fuzz 抓到的缺陷的回归用例：
// 域名里含 ']' 时 net.JoinHostPort 会产出 "0]0:12336" 这种再也无法被
// net.SplitHostPort 解析的串，调用方拿着它只会得到莫名的地址错误。
func TestHardenSock5DomainHostMustBeDialable(t *testing.T) {
	for _, bad := range []string{"0]0", "a:b", "a[b", "a\x00b"} {
		wire := []byte{5, 1, 0, Socks5AtypDomain, byte(len(bad))}
		wire = append(wire, bad...)
		wire = append(wire, 0, 80)
		if _, _, host, err := Sock5GetRequest(bytes.NewBuffer(wire)); err == nil {
			t.Fatalf("domain %q accepted by request parser, host=%q", bad, host)
		}
		addr := []byte{Socks5AtypDomain, byte(len(bad))}
		addr = append(addr, bad...)
		addr = append(addr, 0, 80)
		if h, _, _, err := decodeSocksAddr(addr); err == nil {
			t.Fatalf("domain %q accepted by udp parser, host=%q", bad, h)
		}
	}

	// 合法域名必须照常放行，且返回的 host 一定可再解析。
	for _, good := range []string{"foo", "a.b.c", "xn--fiqs8s", "a-b.c", "a_b"} {
		wire := []byte{5, 1, 0, Socks5AtypDomain, byte(len(good))}
		wire = append(wire, good...)
		wire = append(wire, 0, 80)
		_, _, host, err := Sock5GetRequest(bytes.NewBuffer(wire))
		if err != nil {
			t.Fatalf("domain %q rejected: %v", good, err)
		}
		h, p, err := net.SplitHostPort(host)
		if err != nil || h != good || p != "80" {
			t.Fatalf("domain %q produced undialable host %q: h=%q p=%q err=%v", good, host, h, p, err)
		}
	}
}

func TestHardenSock5ReplyEdges(t *testing.T) {
	if err := Sock5SendConnectReply(nil, 0, "1.2.3.4:80"); err == nil || !strings.Contains(err.Error(), "nil conn") {
		t.Fatalf("nil writer err = %v", err)
	}

	zeroBnd := []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	for _, bnd := range []string{"bad", "host:abc", "host:70000", ":80", strings.Repeat("a", 256) + ":80"} {
		buf := &bytes.Buffer{}
		if err := Sock5SendConnectReply(buf, 0, bnd); err != nil {
			t.Fatalf("bnd %q: %v", bnd, err)
		}
		if !bytes.Equal(buf.Bytes(), zeroBnd) {
			t.Fatalf("bnd %q reply = %v, want zero ipv4 bnd", bnd, buf.Bytes())
		}
	}

	// IPv6 bind address.
	buf := &bytes.Buffer{}
	if err := Sock5SendConnectReply(buf, 0, "[::1]:80"); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	if len(got) != 22 || got[3] != Socks5AtypIP6 || got[21] != 80 {
		t.Fatalf("ipv6 reply len=%d atyp=%d: %v", len(got), got[3], got)
	}

	// Domain bind address.
	buf = &bytes.Buffer{}
	if err := Sock5SendConnectReply(buf, 0, "example.com:443"); err != nil {
		t.Fatal(err)
	}
	got = buf.Bytes()
	if got[3] != Socks5AtypDomain || got[4] != 11 || string(got[5:16]) != "example.com" {
		t.Fatalf("domain reply: %v", got)
	}

	// Writer failures on both the fallback and the normal paths propagate.
	if err := Sock5SendConnectReply(&failRW{failAt: 0, err: errFailRW}, 0, "bad"); !errors.Is(err, errFailRW) {
		t.Fatalf("fallback write fail err = %v", err)
	}
	if err := Sock5SendConnectReply(&failRW{failAt: 0, err: errFailRW}, 0, "1.2.3.4:80"); !errors.Is(err, errFailRW) {
		t.Fatalf("normal write fail err = %v", err)
	}
}

func TestHardenSock5AddrCodec(t *testing.T) {
	v6, err := encodeSocksAddr("::1", 80)
	if err != nil || v6[0] != Socks5AtypIP6 || len(v6) != 19 {
		t.Fatalf("encode ipv6: %v %v", v6, err)
	}
	if _, err := encodeSocksAddr("", 80); err == nil {
		t.Fatal("empty host should fail")
	}
	if _, err := encodeSocksAddr(strings.Repeat("a", 256), 80); err == nil {
		t.Fatal("256 byte host should fail")
	}

	if _, _, _, err := decodeSocksAddr(nil); err == nil {
		t.Fatal("empty addr should fail")
	}
	if _, _, _, err := decodeSocksAddr([]byte{Socks5AtypIP4, 1, 2, 3}); err == nil {
		t.Fatal("truncated ipv4 should fail")
	}
	if _, _, _, err := decodeSocksAddr(append([]byte{Socks5AtypIP6}, make([]byte, 10)...)); err == nil {
		t.Fatal("truncated ipv6 should fail")
	}
	if _, _, _, err := decodeSocksAddr([]byte{Socks5AtypDomain}); err == nil {
		t.Fatal("missing domain length should fail")
	}
	if _, _, _, err := decodeSocksAddr([]byte{Socks5AtypDomain, 0}); err == nil {
		t.Fatal("zero-length domain should fail")
	}
	if _, _, _, err := decodeSocksAddr([]byte{Socks5AtypDomain, 3, 'a'}); err == nil {
		t.Fatal("truncated domain should fail")
	}
	if _, _, _, err := decodeSocksAddr([]byte{9}); err != errAddrType {
		t.Fatalf("unknown atyp err = %v", err)
	}

	// IPv4 and IPv6 happy paths.
	h, p, _, err := decodeSocksAddr([]byte{Socks5AtypIP4, 1, 2, 3, 4, 0, 80})
	if err != nil || h != "1.2.3.4" || p != 80 {
		t.Fatalf("decode ipv4 host=%q port=%d err=%v", h, p, err)
	}
	h, p, _, err = decodeSocksAddr(append([]byte{Socks5AtypIP6}, append(make([]byte, 16), 0, 80)...))
	if err != nil || h != "::" || p != 80 {
		t.Fatalf("decode ipv6 host=%q port=%d err=%v", h, p, err)
	}

	// Domain with trailing payload is returned as rest.
	b := []byte{Socks5AtypDomain, 3, 'f', 'o', 'o', 0, 80, 'h', 'i'}
	host, port, rest, err := decodeSocksAddr(b)
	if err != nil || host != "foo" || port != 80 || string(rest) != "hi" {
		t.Fatalf("decode domain host=%q port=%d rest=%q err=%v", host, port, rest, err)
	}
}

func TestHardenSock5UDPEdges(t *testing.T) {
	if _, err := Sock5PackUDP("1.2.3.4", -1, nil); err == nil {
		t.Fatal("negative port should fail")
	}
	if _, err := Sock5PackUDP("", 80, nil); err == nil {
		t.Fatal("empty host should fail")
	}

	// IPv6 packing.
	pkt, err := Sock5PackUDP("::1", 53, []byte("x"))
	if err != nil || pkt[3] != Socks5AtypIP6 {
		t.Fatalf("ipv6 pack: %v err=%v", pkt, err)
	}

	// Domain round trip.
	pkt, err = Sock5PackUDP("foo.com", 53, []byte("hi"))
	if err != nil {
		t.Fatal(err)
	}
	host, port, data, err := Sock5UnpackUDP(pkt)
	if err != nil || host != "foo.com" || port != 53 || string(data) != "hi" {
		t.Fatalf("domain unpack host=%q port=%d data=%q err=%v", host, port, data, err)
	}

	if _, _, _, err := Sock5UnpackUDP([]byte{0, 0, 0}); err == nil {
		t.Fatal("short packet should fail")
	}
	if _, _, _, err := Sock5UnpackUDP([]byte{0, 0, 1, Socks5AtypIP4, 1, 2, 3, 4, 0, 80}); err != errUDPFrag {
		t.Fatalf("frag err = %v, want errUDPFrag", err)
	}
	if _, _, _, err := Sock5UnpackUDP([]byte{0, 0, 0, 9}); err == nil {
		t.Fatal("unknown atyp should fail")
	}
}

// ---------- socks5 client side ----------

func TestHardenSock5ClientReaders(t *testing.T) {
	if h, err := readSocksHost(bytes.NewReader([]byte{1, 2, 3, 4}), Socks5AtypIP4); err != nil || h != "1.2.3.4" {
		t.Fatalf("ipv4 reader host=%q err=%v", h, err)
	}
	if h, err := readSocksHost(bytes.NewReader(make([]byte, 16)), Socks5AtypIP6); err != nil || h != "::" {
		t.Fatalf("ipv6 reader host=%q err=%v", h, err)
	}
	if h, err := readSocksHost(bytes.NewReader([]byte{3, 'f', 'o', 'o'}), Socks5AtypDomain); err != nil || h != "foo" {
		t.Fatalf("domain reader host=%q err=%v", h, err)
	}
	if _, err := readSocksHost(bytes.NewReader([]byte{1, 2}), Socks5AtypIP4); err == nil {
		t.Fatal("truncated ipv4 should fail")
	}
	if _, err := readSocksHost(bytes.NewReader(make([]byte, 3)), Socks5AtypIP6); err == nil {
		t.Fatal("truncated ipv6 should fail")
	}
	if _, err := readSocksHost(bytes.NewReader(nil), Socks5AtypDomain); err == nil {
		t.Fatal("missing domain length should fail")
	}
	if _, err := readSocksHost(bytes.NewReader([]byte{0}), Socks5AtypDomain); err == nil {
		t.Fatal("zero-length domain should fail")
	}
	if _, err := readSocksHost(bytes.NewReader([]byte{3, 'a'}), Socks5AtypDomain); err == nil {
		t.Fatal("truncated domain should fail")
	}
	if _, err := readSocksHost(bytes.NewReader(nil), 9); err == nil {
		t.Fatal("unknown atyp should fail")
	}
	if p, err := readSocksPort(bytes.NewReader([]byte{0x1f, 0x90})); err != nil || p != 8080 {
		t.Fatalf("port = %d err=%v", p, err)
	}
	if _, err := readSocksPort(bytes.NewReader([]byte{0})); err == nil {
		t.Fatal("truncated port should fail")
	}
}

func TestHardenSock5ClientNilAndValidate(t *testing.T) {
	if err := Sock5Handshake(nil, 0, "", ""); err == nil || !strings.Contains(err.Error(), "nil conn") {
		t.Fatalf("nil handshake err = %v", err)
	}
	if err := Sock5SetRequest(nil, "1.2.3.4", 80, 0); err == nil || !strings.Contains(err.Error(), "nil conn") {
		t.Fatalf("nil setrequest err = %v", err)
	}
	if _, err := Sock5SetUDPRequest(nil, "1.2.3.4", 80, 0); err == nil || !strings.Contains(err.Error(), "nil conn") {
		t.Fatalf("nil setudp err = %v", err)
	}
}

// scriptedTCPProxy starts a local TCP listener whose accepted connection is
// driven by h; the dialed *net.TCPConn is returned for the client under test.
func scriptedTCPProxy(t *testing.T, h func(net.Conn)) *net.TCPConn {
	t.Helper()
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			h(c)
			<-done
			c.Close()
		}
	}()
	c, err := net.DialTCP("tcp", nil, ln.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		close(done)
		_ = c.Close()
		_ = ln.Close()
	})
	return c
}

func readFull(t *testing.T, c net.Conn, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := io.ReadFull(c, b); err != nil {
		t.Fatalf("scripted proxy read %d: %v", n, err)
	}
	return b
}

func TestHardenSock5ClientHandshakeErrors(t *testing.T) {
	longName := strings.Repeat("u", 256)
	c := scriptedTCPProxy(t, func(net.Conn) {})
	if err := Sock5Handshake(c, 1000, longName, "p"); err == nil || !strings.Contains(err.Error(), "longer than 255") {
		t.Fatalf("long username err = %v", err)
	}

	// Closed connection: greeting write fails.
	c = scriptedTCPProxy(t, func(net.Conn) {})
	_ = c.Close()
	if err := Sock5Handshake(c, 1000, "", ""); err == nil || !strings.Contains(err.Error(), "failed to write greeting") {
		t.Fatalf("closed greeting err = %v", err)
	}

	// Server rejects every method.
	c = scriptedTCPProxy(t, func(s net.Conn) {
		_, _ = s.Write([]byte{5, 0xff})
	})
	if err := Sock5Handshake(c, 1000, "", ""); err == nil || !strings.Contains(err.Error(), "rejected all authentication") {
		t.Fatalf("no acceptable err = %v", err)
	}

	// Bad version in method selection.
	c = scriptedTCPProxy(t, func(s net.Conn) {
		_, _ = s.Write([]byte{4, 0})
	})
	if err := Sock5Handshake(c, 1000, "", ""); err == nil || !strings.Contains(err.Error(), "unexpected version") {
		t.Fatalf("bad version err = %v", err)
	}

	// Client wants userpass but server selects no-auth.
	c = scriptedTCPProxy(t, func(s net.Conn) {
		readFull(t, s, 3)
		_, _ = s.Write([]byte{5, 0})
	})
	if err := Sock5Handshake(c, 1000, "u", "p"); err == nil || !strings.Contains(err.Error(), "selected method") {
		t.Fatalf("method mismatch err = %v", err)
	}

	// Authentication failure reply.
	c = scriptedTCPProxy(t, func(s net.Conn) {
		readFull(t, s, 3)
		_, _ = s.Write([]byte{5, 2})
		readFull(t, s, 5)
		_, _ = s.Write([]byte{1, 1})
	})
	if err := Sock5Handshake(c, 1000, "u", "p"); err == nil || !strings.Contains(err.Error(), "fail authentication") {
		t.Fatalf("auth failure err = %v", err)
	}

	// Server goes silent after greeting: read deadline surfaces as error.
	c = scriptedTCPProxy(t, func(s net.Conn) {
		readFull(t, s, 3)
		time.Sleep(2 * time.Second)
	})
	if err := Sock5Handshake(c, 300, "", ""); err == nil || !strings.Contains(err.Error(), "failed to read greeting") {
		t.Fatalf("greeting timeout err = %v", err)
	}

	// Server goes silent after method selection while authenticating.
	c = scriptedTCPProxy(t, func(s net.Conn) {
		readFull(t, s, 3)
		_, _ = s.Write([]byte{5, 2})
		time.Sleep(2 * time.Second)
	})
	if err := Sock5Handshake(c, 300, "u", "p"); err == nil || !strings.Contains(err.Error(), "failed to read authentication") {
		t.Fatalf("auth timeout err = %v", err)
	}
}

func TestHardenSock5ClientRequestErrors(t *testing.T) {
	// Invalid port and oversized host are rejected before touching the wire.
	c := scriptedTCPProxy(t, func(net.Conn) {})
	if err := Sock5SetRequest(c, "1.2.3.4", 70000, 1000); err == nil || !strings.Contains(err.Error(), "invalid destination port") {
		t.Fatalf("bad port err = %v", err)
	}
	c = scriptedTCPProxy(t, func(net.Conn) {})
	if err := Sock5SetRequest(c, strings.Repeat("a", 256), 80, 1000); err == nil || !strings.Contains(err.Error(), "hostname too long") {
		t.Fatalf("long host err = %v", err)
	}

	// Closed connection: request write fails.
	c = scriptedTCPProxy(t, func(net.Conn) {})
	_ = c.Close()
	if err := Sock5SetRequest(c, "1.2.3.4", 80, 1000); err == nil || !strings.Contains(err.Error(), "failed to write connect") {
		t.Fatalf("closed request err = %v", err)
	}

	// Server silent after request: reply header read times out.
	c = scriptedTCPProxy(t, func(s net.Conn) {
		readFull(t, s, 10)
		time.Sleep(2 * time.Second)
	})
	if err := Sock5SetRequest(c, "1.2.3.4", 80, 300); err == nil || !strings.Contains(err.Error(), "failed to read connect reply") {
		t.Fatalf("reply timeout err = %v", err)
	}

	// Bad reply version.
	c = scriptedTCPProxy(t, func(s net.Conn) {
		readFull(t, s, 10)
		_, _ = s.Write([]byte{4, 0, 0, 1, 0, 0, 0, 0, 0, 80})
	})
	if err := Sock5SetRequest(c, "1.2.3.4", 80, 1000); err == nil || !strings.Contains(err.Error(), "unexpected version") {
		t.Fatalf("bad reply version err = %v", err)
	}

	// Unknown bnd address type.
	c = scriptedTCPProxy(t, func(s net.Conn) {
		readFull(t, s, 10)
		_, _ = s.Write([]byte{5, 0, 0, 9})
	})
	if err := Sock5SetRequest(c, "1.2.3.4", 80, 1000); err == nil || !strings.Contains(err.Error(), "bnd host") {
		t.Fatalf("bad bnd atyp err = %v", err)
	}

	// Truncated bnd port.
	c = scriptedTCPProxy(t, func(s net.Conn) {
		readFull(t, s, 10)
		_, _ = s.Write([]byte{5, 0, 0, 1, 1, 2, 3, 4})
	})
	if err := Sock5SetRequest(c, "1.2.3.4", 80, 1000); err == nil || !strings.Contains(err.Error(), "bnd port") {
		t.Fatalf("truncated bnd port err = %v", err)
	}

	// REP failure codes map through socks5Errors; unknown codes are reported raw.
	cases := []struct {
		rep  byte
		want string
	}{
		{1, "general failure"},
		{8, "address type not supported"},
		{45, "reply code 45"},
	}
	for _, tc := range cases {
		rep := tc.rep
		c := scriptedTCPProxy(t, func(s net.Conn) {
			readFull(t, s, 10)
			_, _ = s.Write([]byte{5, rep, 0, 1, 0, 0, 0, 0, 0, 80})
		})
		err := Sock5SetRequest(c, "1.2.3.4", 80, 1000)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("rep=%d err = %v, want %q", rep, err, tc.want)
		}
	}

	// Success reply returns the bnd address.
	c = scriptedTCPProxy(t, func(s net.Conn) {
		readFull(t, s, 10)
		_, _ = s.Write([]byte{5, 0, 0, 1, 1, 2, 3, 4, 0x1f, 0x90})
	})
	bnd, err := Sock5SetUDPRequest(c, "1.2.3.4", 80, 1000)
	if err != nil || bnd != "1.2.3.4:8080" {
		t.Fatalf("bnd = %q err = %v", bnd, err)
	}
}

// ---------- kcp loopback ----------

// hardenPtrCloser is a pointer-receiver io.Closer: KcpConn.finishDial compares
// dialOwned via interface equality, which panics on func-typed closers (the
// production Dial wraps its resource in a pointer for exactly this reason).
type hardenPtrCloser struct{ closed int32 }

func (h *hardenPtrCloser) Close() error {
	atomic.AddInt32(&h.closed, 1)
	return nil
}

// hardenRegisterController installs a dialer Control callback and returns a
// pointer to its invocation counter plus the restore cleanup.
func hardenRegisterController() (*int32, func()) {
	old := gControlOnConnSetup
	var n int32
	RegisterDialerController(func(network, address string, c syscall.RawConn) error {
		atomic.AddInt32(&n, 1)
		return nil
	})
	return &n, func() { gControlOnConnSetup = old }
}

func hardenKcpListen(t *testing.T) Conn {
	t.Helper()
	l, err := NewConn("kcp")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := l.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("kcp listen: %v", err)
	}
	return ln
}

// hardenKcpServe accepts one session on ln and echoes exactly one message.
func hardenKcpServe(t *testing.T, ln Conn, done chan<- struct{}) {
	t.Helper()
	go func() {
		srv, err := ln.Accept()
		if err != nil {
			return
		}
		defer srv.Close()
		buf := make([]byte, 256)
		n, err := io.ReadFull(srv, buf[:16])
		if err != nil {
			t.Errorf("kcp server read: %v", err)
			return
		}
		if _, err := srv.Write(buf[:n]); err != nil {
			t.Errorf("kcp server write: %v", err)
			return
		}
		time.Sleep(100 * time.Millisecond)
		close(done)
	}()
}

// Full local loopback round trip: listener/dialer/session Info strings, the
// registered dialer Control callback, and a bidirectional echo.
func TestHardenKcpLoopbackEchoAndInfo(t *testing.T) {
	if (&KcpConn{}).Name() != "kcp" {
		t.Fatal("KcpConn.Name")
	}
	ctrl, restore := hardenRegisterController()
	defer restore()

	ln := hardenKcpListen(t)
	defer ln.Close()
	addr := ln.(*KcpConn).listener.Addr().String()
	if got := ln.Info(); got != "kcp--"+addr {
		t.Fatalf("listener Info = %q", got)
	}

	done := make(chan struct{})
	hardenKcpServe(t, ln, done)
	time.Sleep(100 * time.Millisecond)

	d, err := NewConn("kcp")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := d.Dial(addr)
	if err != nil {
		t.Fatalf("kcp dial: %v", err)
	}
	defer cli.Close()
	if atomic.LoadInt32(ctrl) == 0 {
		t.Fatal("dialer Control callback not invoked for kcp Dial")
	}
	if got := cli.Info(); !strings.Contains(got, "<--kcp-->") {
		t.Fatalf("dialed Info = %q", got)
	}

	msg := []byte("kcp-loopback-016")
	if _, err := cli.Write(msg); err != nil {
		t.Fatalf("kcp write: %v", err)
	}
	_ = cli.(*KcpConn).sess.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(cli, buf); err != nil {
		t.Fatalf("kcp echo read: %v", err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("kcp echo mismatch: %q", buf)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("kcp server goroutine did not finish")
	}
}

func TestHardenKcpListenAndDialErrors(t *testing.T) {
	// Port out of range: ListenWithOptions fails to bind.
	if _, err := (&KcpConn{}).Listen("127.0.0.1:99999"); err == nil {
		t.Fatal("kcp Listen on invalid port should fail")
	}
	// Port out of range fails address resolution before any socket binds.
	if _, err := (&KcpConn{}).Dial("127.0.0.1:99999"); err == nil {
		t.Fatal("kcp Dial to invalid port should fail")
	}
	// Accept after listener Close must surface the listener error.
	ln := hardenKcpListen(t)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ln.Accept(); err == nil {
		t.Fatal("kcp Accept after Close should fail")
	}
}

// White-box coverage for the dial-abort ownership state machine that cannot
// be driven deterministically over the wire:
//   - Close while a Dial is in flight cancels its context and closes the
//     in-flight owned resource (kcpconn.go Close branches);
//   - finishDial only succeeds with a live context while ownership is held.
func TestHardenKcpCloseAbortAndFinishDial(t *testing.T) {
	canceled := false
	owned := &hardenPtrCloser{}
	c := &KcpConn{
		cancel:    func() { canceled = true },
		dialOwned: owned,
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close with in-flight dial state: %v", err)
	}
	if !canceled || atomic.LoadInt32(&owned.closed) != 1 {
		t.Fatalf("Close must cancel in-flight dial and close its resource: %v/%d", canceled, owned.closed)
	}

	fd := &KcpConn{}

	// Ownership stolen by Close: finishDial fails and must not close the
	// resource (Close already did).
	want := &hardenPtrCloser{}
	fd.dialOwned = &hardenPtrCloser{}
	if fd.finishDial(context.Background(), want) {
		t.Fatal("finishDial must fail when dialOwned was stolen")
	}
	if atomic.LoadInt32(&want.closed) != 0 {
		t.Fatal("finishDial must not close a resource it no longer owns")
	}

	// Context canceled while still owning: fail and close the resource.
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	self := &hardenPtrCloser{}
	fd.dialOwned = self
	if fd.finishDial(ctx2, self) {
		t.Fatal("finishDial must fail on canceled context")
	}
	if atomic.LoadInt32(&self.closed) != 1 {
		t.Fatal("finishDial must close the owned resource on cancel")
	}

	// Happy path: live context, still owner -> success, state cleared, and
	// the resource stays open for the delivered conn.
	happy := &hardenPtrCloser{}
	fd.dialOwned = happy
	if !fd.finishDial(context.Background(), happy) {
		t.Fatal("finishDial must succeed on the happy path")
	}
	if fd.dialOwned != nil || fd.cancel != nil {
		t.Fatal("finishDial must clear dial state")
	}
	if atomic.LoadInt32(&happy.closed) != 0 {
		t.Fatal("happy path must hand the open resource to the delivered conn")
	}
}

// A Close landing while Dial is parked inside the UDP bind deterministically
// hits the post-bind cancel window: Go's ListenConfig keeps running the
// Control callback after the dial context is canceled (a canceled listener
// bind still succeeds), so the abort must be caught by Dial's own ctx.Err()
// check right after setDialOwned, and Dial's deferred cleanup -- not Close,
// which ran while dialOwned was still nil -- has to close the new socket.
func TestHardenKcpDialAbortDuringBind(t *testing.T) {
	old := gControlOnConnSetup
	defer func() { gControlOnConnSetup = old }()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	RegisterDialerController(func(network, address string, c syscall.RawConn) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	})

	c := &KcpConn{}
	type dialResult struct {
		conn Conn
		err  error
	}
	result := make(chan dialResult, 1)
	go func() {
		conn, err := c.Dial("127.0.0.1:9")
		result <- dialResult{conn, err}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("Control callback not entered within 2s")
	}
	// Dial is parked in bind: cancel is installed but dialOwned is still nil.
	if err := c.Close(); err != nil {
		t.Fatalf("close during bind: %v", err)
	}
	close(release)
	select {
	case r := <-result:
		if r.err == nil {
			_ = r.conn.Close()
			t.Fatal("Dial must fail when aborted during bind")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Dial did not return within 2s after bind release")
	}
}

// A dialer Control callback that rejects the socket (firewall-style policy
// failure) must surface from ListenPacket: Dial returns the bind error before
// any dial-owned resource is registered, so no resource leaks and no abort.
func TestHardenKcpDialControllerRejectsBind(t *testing.T) {
	old := gControlOnConnSetup
	defer func() { gControlOnConnSetup = old }()
	RegisterDialerController(func(network, address string, c syscall.RawConn) error {
		return errors.New("harden control reject")
	})

	conn, err := (&KcpConn{}).Dial("127.0.0.1:9")
	if err == nil {
		_ = conn.Close()
		t.Fatal("Dial must fail when the Control callback rejects the bind")
	}
	if conn != nil {
		t.Fatal("rejected bind must return a nil conn")
	}
}

// The narrow cancel windows around ResolveUDPAddr/NewConn/finishDial have no
// blocking hook, so spinning Close callers race a strictly sequential stream
// of Dials. Aborting an in-flight Dial is the documented contract guarded by
// dialMu, hence every outcome is legal: the test only asserts termination and
// cleanup (and is race-clean) while best-effort covering the canceled
// branches. kcp-go NewConn never blocks on a handshake, so a dead remote is
// cheap and deterministic.
func TestHardenKcpDialCancelWindowHammer(t *testing.T) {
	const dialCount = 4000
	const closerCount = 6
	c := &KcpConn{}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < closerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = c.Close()
				}
			}
		}()
	}
	var aborted, delivered int
	for i := 0; i < dialCount; i++ {
		conn, err := c.Dial("127.0.0.1:9")
		if err != nil {
			aborted++
			continue
		}
		delivered++
		_ = conn.Close() // io.ErrClosedPipe if Close stole it meanwhile: ignored
	}
	close(stop)
	wg.Wait()
	t.Logf("kcp cancel hammer: %d dials, %d delivered, %d aborted",
		dialCount, delivered, aborted)
	if delivered+aborted != dialCount {
		t.Fatal("every Dial must finish exactly once")
	}
}

// ---------- quic loopback ----------

func hardenQuicListen(t *testing.T) Conn {
	t.Helper()
	l, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := l.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("quic listen: %v", err)
	}
	return ln
}

// hardenQuicEvict forces the shared client session for key out of the
// process cache so a test leaves no warm session behind.
func hardenQuicEvict(key string) {
	if v, ok := quicClientSessionCache.Load(key); ok {
		v.(*quicClientSession).idleEvict()
		quicClientSessionCache.CompareAndDelete(key, v)
	}
}

// hardenQuicEchoServer accepts up to n conns and echoes one 16-byte message
// on each.
func hardenQuicEchoServer(t *testing.T, ln Conn, n int) {
	t.Helper()
	go func() {
		for i := 0; i < n; i++ {
			srv, err := ln.Accept()
			if err != nil {
				return
			}
			go func(srv Conn) {
				defer srv.Close()
				buf := make([]byte, 16)
				if _, err := io.ReadFull(srv, buf); err != nil {
					return
				}
				_, _ = srv.Write(buf)
				time.Sleep(100 * time.Millisecond)
			}(srv)
		}
	}()
}

func hardenQuicEcho(t *testing.T, cli Conn) {
	t.Helper()
	msg := []byte("quic-loopback!!!")
	if _, err := cli.Write(msg); err != nil {
		t.Fatalf("quic write: %v", err)
	}
	_ = cli.(*QuicConn).stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(cli, buf); err != nil {
		t.Fatalf("quic echo read: %v", err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("quic echo mismatch: %q", buf)
	}
}

func TestHardenQuicLoopbackEchoAndInfo(t *testing.T) {
	if (&QuicConn{}).Name() != "quic" {
		t.Fatal("QuicConn.Name")
	}
	ctrl, restore := hardenRegisterController()
	defer restore()

	ln := hardenQuicListen(t)
	defer ln.Close()
	addr := ln.(*QuicConn).listener.Addr().String()
	if got := ln.Info(); got != "quic--"+addr {
		t.Fatalf("listener Info = %q", got)
	}
	hardenQuicEchoServer(t, ln, 1)
	time.Sleep(100 * time.Millisecond)

	d, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := d.Dial(addr)
	if err != nil {
		t.Fatalf("quic dial: %v", err)
	}
	defer func() {
		_ = cli.Close()
		hardenQuicEvict(addr)
	}()
	if atomic.LoadInt32(ctrl) == 0 {
		t.Fatal("dialer Control callback not invoked for quic Dial")
	}
	if got := cli.Info(); !strings.Contains(got, "<--quic-->") {
		t.Fatalf("dialed Info = %q", got)
	}
	hardenQuicEcho(t, cli)
}

// Closing the last conn keeps the session warm (idle timer armed); the next
// Dial must reuse that session, stop the idle timer, and carry data on a new
// stream without a second handshake.
func TestHardenQuicWarmSessionReuse(t *testing.T) {
	ln := hardenQuicListen(t)
	defer ln.Close()
	addr := ln.(*QuicConn).listener.Addr().String()
	hardenQuicEchoServer(t, ln, 2)
	time.Sleep(100 * time.Millisecond)

	d1, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	cli1, err := d1.Dial(addr)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	qc1 := cli1.(*QuicConn)
	cs := qc1.clientSess
	sharedSession := qc1.qsession
	hardenQuicEcho(t, cli1)
	if err := cli1.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	cs.mu.Lock()
	if cs.refs != 0 || cs.idleTimer == nil || cs.closed {
		cs.mu.Unlock()
		t.Fatal("warm session must have refs=0 with idle timer armed after Close")
	}
	cs.mu.Unlock()

	d2, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	cli2, err := d2.Dial(addr)
	if err != nil {
		t.Fatalf("reuse dial: %v", err)
	}
	defer func() {
		_ = cli2.Close()
		hardenQuicEvict(addr)
	}()
	qc2 := cli2.(*QuicConn)
	if qc2.qsession != sharedSession || qc2.clientSess != cs {
		t.Fatal("second Dial must reuse the cached QUIC session")
	}
	cs.mu.Lock()
	if cs.refs != 1 || cs.idleTimer != nil || cs.closed {
		cs.mu.Unlock()
		t.Fatalf("reused session refs/timer/closed = %d/%v/%v, want 1/nil/false", cs.refs, cs.idleTimer, cs.closed)
	}
	cs.mu.Unlock()
	hardenQuicEcho(t, cli2)
}

// Immediate release (dial failure/abort path) shuts the session and its UDP
// socket down right away and evicts the cache entry.
func TestHardenQuicImmediateRelease(t *testing.T) {
	ln := hardenQuicListen(t)
	defer ln.Close()
	addr := ln.(*QuicConn).listener.Addr().String()
	hardenQuicEchoServer(t, ln, 1)
	time.Sleep(100 * time.Millisecond)

	d, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := d.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	qc := cli.(*QuicConn)
	cs := qc.clientSess
	pconn := cs.pconn

	cs.release(true)
	if !cs.closed {
		t.Fatal("immediate release must mark the session closed")
	}
	if _, ok := quicClientSessionCache.Load(addr); ok {
		t.Fatal("immediate release must evict the cache entry")
	}
	if _, err := pconn.WriteTo([]byte("z"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}); err == nil {
		t.Fatal("immediate release must close the dial-side PacketConn")
	}
	// Closing the delivered conn afterwards must stay safe: refs clamp at 0
	// and no second shutdown runs.
	if err := cli.Close(); err != nil {
		t.Fatalf("close after immediate release: %v", err)
	}
}

// Legacy/defensive close path for a non-cached direct conn owns its session
// and UDP socket and must close both.
func TestHardenQuicLegacyClosePath(t *testing.T) {
	ln := hardenQuicListen(t)
	defer ln.Close()
	addr := ln.(*QuicConn).listener.Addr().String()
	hardenQuicEchoServer(t, ln, 1)
	time.Sleep(100 * time.Millisecond)

	d, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := d.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	qc := cli.(*QuicConn)
	cs := qc.clientSess
	session := qc.qsession
	pconn := cs.pconn

	// Detach from the cache-backed ownership model: the cache entry goes
	// away and the conn itself takes ownership of session + socket.
	quicClientSessionCache.CompareAndDelete(addr, cs)
	qc.clientSess = nil
	qc.pconn = pconn

	if err := cli.Close(); err != nil {
		t.Fatalf("legacy close: %v", err)
	}
	if session.Context().Err() == nil {
		t.Fatal("legacy close must shut the QUIC session down")
	}
	if _, err := pconn.WriteTo([]byte("z"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}); err == nil {
		t.Fatal("legacy close must close the owned PacketConn")
	}

	// Error arms of the legacy teardown: the already-dead session is closed
	// once more and a PacketConn whose Close fails surfaces as the result.
	again := &QuicConn{qsession: session, pconn: hardenFailPacketConn{}}
	if err := again.Close(); err == nil {
		t.Fatal("legacy close with failing resources must return an error")
	}
	// Nil session + failing PacketConn: the socket error is reported directly.
	onlyPconn := &QuicConn{pconn: hardenFailPacketConn{}}
	if err := onlyPconn.Close(); err == nil || err.Error() != "harden fail close" {
		t.Fatalf("legacy close pconn error = %v", err)
	}
}

// hardenFailPacketConn is a PacketConn stub whose every op fails, used to
// exercise legacy teardown error propagation.
type hardenFailPacketConn struct{}

func (hardenFailPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	return 0, nil, errors.New("harden fail read")
}
func (hardenFailPacketConn) WriteTo([]byte, net.Addr) (int, error) {
	return 0, errors.New("harden fail write")
}
func (hardenFailPacketConn) Close() error                     { return errors.New("harden fail close") }
func (hardenFailPacketConn) LocalAddr() net.Addr              { return nil }
func (hardenFailPacketConn) SetDeadline(time.Time) error      { return nil }
func (hardenFailPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (hardenFailPacketConn) SetWriteDeadline(time.Time) error { return nil }

func TestHardenQuicListenAndDialErrors(t *testing.T) {
	// TLS config is generated first (cannot fail locally); binding an
	// out-of-range port must surface the quic listen error.
	if _, err := (&QuicConn{}).Listen("127.0.0.1:99999"); err == nil {
		t.Fatal("quic Listen on invalid port should fail")
	}
	// Address resolution fails before any handshake.
	if _, err := (&QuicConn{}).Dial("127.0.0.1:99999"); err == nil {
		t.Fatal("quic Dial to invalid port should fail")
	}
	// Accept after listener Close must return the close error.
	ln := hardenQuicListen(t)
	go func() { _, _ = ln.Accept() }()
	time.Sleep(50 * time.Millisecond)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ln.Accept(); err == nil {
		t.Fatal("quic Accept after Close should fail")
	}
}

// White-box: Close during an in-flight Dial cancels and closes the owned
// abort closer; newOwnedCloser wraps real closers (not just nil receivers).
func TestHardenQuicCloseInflightAndOwnedCloser(t *testing.T) {
	canceled := false
	ownedClosed := false
	c := &QuicConn{
		cancel:    func() { canceled = true },
		dialOwned: newOwnedCloser(closerFunc(func() error { ownedClosed = true; return nil })),
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close with in-flight dial state: %v", err)
	}
	if !canceled || !ownedClosed {
		t.Fatalf("Close must cancel in-flight dial and close its resource: %v/%v", canceled, ownedClosed)
	}

	realClosed := false
	o := newOwnedCloser(closerFunc(func() error { realClosed = true; return nil }))
	if err := o.Close(); err != nil || !realClosed {
		t.Fatalf("ownedCloser with real closer: closed=%v err=%v", realClosed, err)
	}
}

// When OpenStreamSync cannot complete because the server-side bidirectional
// stream limit is saturated on the shared session and the session dies
// underneath the Dial, Dial returns the open error and its own deferred
// cleanup must invoke the abort closer (release(true)) instead of leaking
// the reference; release(true) on the sole owner shuts down and evicts the
// cache entry immediately.
func TestHardenQuicDialAbortedAtStreamOpen(t *testing.T) {
	ctrl, restore := hardenRegisterController()
	defer restore()
	ln := hardenQuicListen(t)
	defer ln.Close()
	addr := ln.(*QuicConn).listener.Addr().String()
	defer hardenQuicEvict(addr)

	cli, err := (&QuicConn{}).Dial(addr)
	if err != nil {
		t.Fatalf("warm dial: %v", err)
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if atomic.LoadInt32(ctrl) == 0 {
		t.Fatal("dialer Control callback not invoked")
	}
	v, ok := quicClientSessionCache.Load(udpAddr.String())
	if !ok {
		t.Fatal("warm session not cached")
	}
	cs := v.(*quicClientSession)
	// Saturate the server-advertised bidirectional stream limit adaptively:
	// open until the next grant does not arrive (quic-go reserves one slot,
	// so the exact count is version-dependent).
	var opened []io.Closer
	for {
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		s, oerr := cs.session.OpenStreamSync(probeCtx)
		probeCancel()
		if oerr != nil {
			if !errors.Is(oerr, context.DeadlineExceeded) {
				t.Fatalf("unexpected saturation error: %v", oerr)
			}
			break
		}
		opened = append(opened, s)
	}
	if len(opened) == 0 {
		t.Fatal("expected to open at least one stream")
	}
	defer func() {
		for _, s := range opened {
			_ = s.Close()
		}
	}()
	// Drop the warm conn: refs -> 0 and the idle timer arms while the session
	// (with its saturated streams) stays cached for reuse.
	if err := cli.Close(); err != nil {
		t.Fatalf("warm conn close: %v", err)
	}

	c := &QuicConn{}
	done := make(chan struct{})
	var dconn Conn
	var derr error
	go func() {
		dconn, derr = c.Dial(addr)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	// Kill the shared session underneath the blocked Dial (server restart,
	// idle eviction, etc.): OpenStreamSync fails on its own, so Dial's own
	// deferred cleanup -- not Close() -- must close the abort closer.
	cs.shutdown()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream-open dial did not return after session shutdown")
	}
	// Dial already cleared its in-flight state; Close afterwards is a no-op.
	if err := c.Close(); err != nil {
		t.Fatalf("close after finished dial: %v", err)
	}
	if derr == nil {
		_ = dconn.Close()
		t.Fatal("dial blocked on OpenStreamSync must fail after session shutdown")
	}
	if dconn != nil {
		t.Fatal("failed dial must return nil conn")
	}
	if _, ok := quicClientSessionCache.Load(udpAddr.String()); ok {
		t.Fatal("aborted sole-owner dial must release(true) and evict the session")
	}
	cs.mu.Lock()
	closed := cs.closed
	refs := cs.refs
	cs.mu.Unlock()
	if !closed || refs != 0 {
		t.Fatalf("session after abort: closed=%v refs=%d", closed, refs)
	}
}

// A Dial stuck in the QUIC handshake against a non-QUIC UDP peer is aborted
// by Close: acquireQuicSession returns the dial error, closes the dialing
// PacketConn, and never caches a session.
func TestHardenQuicDialHandshakeAborted(t *testing.T) {
	// Local UDP blackhole: accepts and discards datagrams, never speaks QUIC.
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("blackhole listen: %v", err)
	}
	defer pc.Close()
	addr := pc.LocalAddr().String()
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, rerr := pc.ReadFromUDP(buf); rerr != nil {
				return
			}
		}
	}()

	c := &QuicConn{}
	done := make(chan struct{})
	var conn Conn
	var dialErr error
	go func() {
		conn, dialErr = c.Dial(addr)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	if err := c.Close(); err != nil {
		t.Fatalf("close during dial: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("aborted QUIC dial did not return within 5s")
	}
	if dialErr == nil {
		_ = conn.Close()
		t.Fatal("aborted QUIC dial must fail")
	}
	if conn != nil {
		t.Fatal("failed dial must return a nil conn")
	}
	if _, ok := quicClientSessionCache.Load(addr); ok {
		t.Fatal("dead session must not be cached")
	}
}

// ---------- tcp harden ----------

// Name/Info/Read/Write/Close on a bare TcpConn and deterministic error
// branches of Listen/Dial without touching the network.
func TestHardenTcpEmptyAndErrors(t *testing.T) {
	c := &TcpConn{}
	if c.Name() != "tcp" {
		t.Fatal("TcpConn.Name")
	}
	if c.Info() != "empty tcp conn" {
		t.Fatalf("empty Info = %q", c.Info())
	}
	if _, err := c.Read(make([]byte, 4)); err == nil || err.Error() != "empty conn" {
		t.Fatalf("empty Read err = %v", err)
	}
	if _, err := c.Write([]byte("x")); err == nil || err.Error() != "empty conn" {
		t.Fatalf("empty Write err = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("empty Close: %v", err)
	}

	// Address resolution failures surface before any socket op.
	if _, err := (&TcpConn{}).Dial(":::::not-an-addr"); err == nil {
		t.Fatal("Dial with unparsable address must fail")
	}
	if _, err := (&TcpConn{}).Listen(":::::not-an-addr"); err == nil {
		t.Fatal("Listen with unparsable address must fail")
	}

	// Bind failure while resolution succeeds: a second listener on an
	// already-owned loopback port gets EADDRINUSE from ListenTCP.
	first, err := (&TcpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("first listen: %v", err)
	}
	defer first.Close()
	port := first.(*TcpConn).listener.Addr().(*net.TCPAddr).Port
	if _, err := (&TcpConn{}).Listen("127.0.0.1:" + strconv.Itoa(port)); err == nil {
		t.Fatal("second Listen on the same port must fail")
	}

	// Connection refused is a deterministic dial error on loopback.
	refused, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	closedAddr := refused.Addr().String()
	_ = refused.Close()
	if conn, err := (&TcpConn{}).Dial(closedAddr); err == nil {
		_ = conn.Close()
		t.Fatal("Dial to a closed loopback port must fail")
	}

	// Accept after Close returns the listener error; bare Accept fails too.
	if _, err := c.Accept(); err == nil || err.Error() != "not listen" {
		t.Fatalf("bare Accept err = %v", err)
	}
	go func() {
		if _, err := first.Accept(); err == nil {
			t.Errorf("Accept after Close must fail")
		}
	}()
	_ = first.Close()
}

// Full loopback echo plus listener/dialer Info strings and Control callback.
func TestHardenTcpLoopbackInfo(t *testing.T) {
	ctrl, restore := hardenRegisterController()
	defer restore()
	ln, err := (&TcpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	laddr := ln.(*TcpConn).listener.Addr().String()
	if got := ln.Info(); got != "tcp--"+laddr {
		t.Fatalf("listener Info = %q", got)
	}
	accepted := make(chan Conn, 1)
	go func() {
		s, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		accepted <- s
	}()

	cli, err := (&TcpConn{}).Dial(laddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	if atomic.LoadInt32(ctrl) == 0 {
		t.Fatal("dialer Control callback not invoked")
	}
	if !strings.Contains(cli.Info(), "<--tcp-->") {
		t.Fatalf("dialer Info = %q", cli.Info())
	}
	srv := <-accepted
	defer srv.Close()

	msg := []byte("tcp-loopback-16")
	if _, err := cli.Write(msg); err != nil {
		t.Fatalf("client write: %v", err)
	}
	echo := make([]byte, len(msg))
	if _, err := io.ReadFull(srv, echo); err != nil {
		t.Fatalf("server read: %v", err)
	}
	if string(echo) != string(msg) {
		t.Fatalf("server got %q", echo)
	}
	if _, err := srv.Write(echo); err != nil {
		t.Fatalf("server write: %v", err)
	}
	back := make([]byte, len(msg))
	if _, err := io.ReadFull(cli, back); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(back) != string(msg) {
		t.Fatalf("echo mismatch %q", back)
	}
}

// The post-connect cancel window (ctx canceled between DialContext success
// and Dial's own ctx.Err check) has no blocking hook: loopback TCP connects
// in microseconds. Two complementary best-effort drivers are used, both
// asserting only termination/per-conn cleanup (every outcome is legal):
//
//  1. Timed aborts: the Control hook runs immediately before the kernel
//     connect, so a Close scheduled a few hundred microseconds later lands
//     right when the handshake completes. Inside net.Dialer the conn-ready
//     and ctx-done signals are a randomized select, so a successful connect
//     racing a canceled ctx surfaces here as DialContext success followed by
//     the post-connect cancel check.
//  2. A stream of Dials with spinning Close callers covers the same window
//     under a different interleaving.
func TestHardenTcpDialCancelWindowHammer(t *testing.T) {
	ln, err := (&TcpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.(*TcpConn).listener.Addr().String()
	go func() {
		for {
			s, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(s Conn) {
				io.Copy(io.Discard, s)
				_ = s.Close()
			}(s)
		}
	}()

	old := gControlOnConnSetup
	defer func() { gControlOnConnSetup = old }()
	for attempt := 0; attempt < 600; attempt++ {
		c := &TcpConn{}
		delay := time.Duration(50+attempt%400) * time.Microsecond
		RegisterDialerController(func(network, address string, raw syscall.RawConn) error {
			go func() {
				time.Sleep(delay)
				_ = c.Close()
			}()
			return nil
		})
		if conn, derr := c.Dial(addr); derr == nil {
			_ = conn.Close()
		}
	}

	RegisterDialerController(func(network, address string, raw syscall.RawConn) error { return nil })
	const dialCount = 3000
	c := &TcpConn{}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = c.Close()
				}
			}
		}()
	}
	var delivered, aborted int
	for i := 0; i < dialCount; i++ {
		conn, derr := c.Dial(addr)
		if derr != nil {
			aborted++
			continue
		}
		delivered++
		_ = conn.Close()
	}
	close(stop)
	wg.Wait()
	t.Logf("tcp cancel hammer: %d stream dials, %d delivered, %d aborted",
		dialCount, delivered, aborted)
	if delivered+aborted != dialCount {
		t.Fatal("every Dial must finish exactly once")
	}
}

// ---------- udp harden ----------

// Bare/listener/dialer error branches that need no peer traffic.
func TestHardenUdpEmptyAndErrors(t *testing.T) {
	c := &UdpConn{}
	if c.Name() != "udp" {
		t.Fatal("UdpConn.Name")
	}
	if c.Info() != "empty udp conn" {
		t.Fatalf("empty Info = %q", c.Info())
	}
	if _, err := c.Read(make([]byte, 4)); err == nil || err.Error() != "empty conn" {
		t.Fatalf("empty Read err = %v", err)
	}
	if _, err := c.Write([]byte("x")); err == nil || err.Error() != "empty conn" {
		t.Fatalf("empty Write err = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("empty Close: %v", err)
	}
	// checkConfig must auto-install defaults on a bare conn.
	cfg := c.GetConfig()
	if cfg == nil || cfg.MaxPacketSize != 10240 {
		t.Fatalf("auto default config = %+v", cfg)
	}
	custom := DefaultUdpConfig()
	custom.MaxPacketSize = 1234
	c.SetConfig(custom)
	if c.GetConfig().MaxPacketSize != 1234 {
		t.Fatal("SetConfig/GetConfig")
	}

	// Resolution failure and double-bind failure of Listen.
	if _, err := (&UdpConn{}).Listen(":::::not-an-addr"); err == nil {
		t.Fatal("Listen with unparsable address must fail")
	}
	ln0, err := (&UdpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("first listen: %v", err)
	}
	port := ln0.(*UdpConn).listener.listenerconn.LocalAddr().(*net.UDPAddr).Port
	if _, err := (&UdpConn{}).Listen("127.0.0.1:" + strconv.Itoa(port)); err == nil {
		t.Fatal("second Listen on the same port must fail")
	}

	// A listener itself can neither Read nor Write.
	if _, err := ln0.Read(make([]byte, 4)); err == nil || err.Error() != "listener can not be read" {
		t.Fatalf("listener Read err = %v", err)
	}
	if _, err := ln0.Write([]byte("x")); err == nil || err.Error() != "listener can not be write" {
		t.Fatalf("listener Write err = %v", err)
	}
	// Bare Accept fails; Accept on a closed listener fails as well.
	if _, err := (&UdpConn{}).Accept(); err == nil || err.Error() != "not listen" {
		t.Fatal("bare Accept must fail with not listen")
	}
	go func() {
		if _, err := ln0.Accept(); err == nil {
			t.Errorf("Accept after Close must fail")
		}
	}()
	_ = ln0.Close()

	// Dial resolution failure.
	if conn, err := (&UdpConn{}).Dial(":::::not-an-addr"); err == nil {
		_ = conn.Close()
		t.Fatal("Dial with unparsable address must fail")
	}
}

// A rejecting Control callback surfaces from DialContext (bind/connect
// failure), and a Close landing while Dial is parked inside the callback
// cancels the dial through Close's cancel branch.
func TestHardenUdpDialControllerPaths(t *testing.T) {
	old := gControlOnConnSetup
	defer func() { gControlOnConnSetup = old }()

	RegisterDialerController(func(network, address string, raw syscall.RawConn) error {
		return errors.New("harden control reject")
	})
	if conn, err := (&UdpConn{}).Dial("127.0.0.1:9"); err == nil {
		_ = conn.Close()
		t.Fatal("Dial must fail when Control rejects")
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	RegisterDialerController(func(network, address string, raw syscall.RawConn) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	})
	c := &UdpConn{}
	type result struct {
		conn Conn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		conn, err := c.Dial("127.0.0.1:9")
		done <- result{conn, err}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("Control callback not entered")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close during dial: %v", err)
	}
	close(release)
	select {
	case r := <-done:
		if r.err == nil {
			_ = r.conn.Close()
			t.Fatal("Dial parked in Control must fail once Close cancels it")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Dial did not return after Control release")
	}
}

// End-to-end listener/sonny lifecycle: Info strings, oversized read buffer,
// read/write after sonny close, stale-sonny eviction on late packets, and a
// fresh sonny accepted from the same source afterwards.
func TestHardenUdpSonnyLifecycle(t *testing.T) {
	ln, err := (&UdpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	luc := ln.(*UdpConn)
	addr := luc.listener.listenerconn.LocalAddr().String()
	if got := ln.Info(); got != "udp--"+addr {
		t.Fatalf("listener Info = %q", got)
	}

	cli, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("raw udp client: %v", err)
	}
	defer cli.Close()
	accepted := make(chan Conn, 1)
	go func() {
		s, aerr := ln.Accept()
		if aerr == nil {
			accepted <- s
		}
	}()
	if _, err := cli.Write([]byte("udp-sonny-16!!!")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	var srv Conn
	select {
	case srv = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("Accept timeout")
	}
	defer srv.Close()
	if !strings.Contains(srv.Info(), "<--udp-->") {
		t.Fatalf("sonny Info = %q", srv.Info())
	}

	// An undersized read consumes the queued datagram and reports the size
	// mismatch (production behavior), so resend before doing a full read.
	msg := []byte("udp-sonny-16!!!")
	small := make([]byte, 8)
	if _, err := srv.Read(small); err == nil || err.Error() != "read buffer too small" {
		t.Fatalf("oversized Read err = %v", err)
	}
	if _, err := cli.Write(msg); err != nil {
		t.Fatalf("resend: %v", err)
	}
	buf := make([]byte, 64)
	n, err := srv.Read(buf)
	if err != nil || string(buf[:n]) != string(msg) {
		t.Fatalf("sonny Read n=%d err=%v", n, err)
	}
	if _, err := srv.Write([]byte("pong-udp")); err != nil {
		t.Fatalf("sonny Write: %v", err)
	}
	rbuf := make([]byte, 16)
	if rn, rerr := cli.Read(rbuf); rerr != nil || string(rbuf[:rn]) != "pong-udp" {
		t.Fatalf("client reply n=%d err=%v", rn, rerr)
	}

	// Close the sonny: further Read/Write fail immediately.
	if err := srv.Close(); err != nil {
		t.Fatalf("sonny close: %v", err)
	}
	if _, err := srv.Read(buf); err == nil || err.Error() != "read closed conn" {
		t.Fatalf("closed sonny Read err = %v", err)
	}
	if _, err := srv.Write([]byte("x")); err == nil || err.Error() != "write closed conn" {
		t.Fatalf("closed sonny Write err = %v", err)
	}
	// A packet arriving while a closed sonny is still registered must evict
	// it from the map and be dropped. Close already deletes the entry, so
	// re-register the closed sonny under the client's source key to
	// deterministically drive the stale-entry branch.
	srcKey := cli.LocalAddr().String()
	luc.listener.sonny.Store(srcKey, srv)
	if _, err := cli.Write([]byte("late")); err != nil {
		t.Fatalf("late write: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := luc.listener.sonny.Load(srcKey); !ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := luc.listener.sonny.Load(srcKey); ok {
		t.Fatal("stale closed sonny must be evicted on the next packet")
	}
	// The next packet creates a brand-new sonny and enqueues it.
	if _, err := cli.Write([]byte("next-udp-packet!")); err != nil {
		t.Fatalf("next write: %v", err)
	}
	srv2, err := ln.Accept()
	if err != nil {
		t.Fatalf("re-accept after stale eviction: %v", err)
	}
	defer srv2.Close()
	n, err = srv2.Read(buf)
	if err != nil || string(buf[:n]) != "next-udp-packet!" {
		t.Fatalf("new sonny read n=%d err=%v", n, err)
	}

	// Closing the listener stops the recv group and closes every sonny.
	_ = ln.Close()
	if _, err := srv2.Read(buf); err == nil {
		t.Fatal("sonny must be closed when the listener closes")
	}
}

// Defensive channel-closed-without-flag branch of sonny Read.
func TestHardenUdpSonnyReadClosedChannel(t *testing.T) {
	ch := common.NewChannel(1)
	ch.Close()
	u := &UdpConn{
		config: DefaultUdpConfig(),
		listenersonny: &udpConnListenerSonny{
			recvch: ch,
		},
	}
	if _, err := u.Read(make([]byte, 4)); err == nil || err.Error() != "read closed conn" {
		t.Fatalf("closed-channel Read err = %v", err)
	}
}

// Unbuffered recv channels plus an unread accept channel force every
// loopRecv push to time out: new-sonny recv push, the accept-backlog reject
// (which closes the rejected sonny), and the live-sonny recv push.
func TestHardenUdpLoopRecvBackpressure(t *testing.T) {
	cfg := &UdpConfig{
		MaxPacketSize:       1024,
		RecvChanLen:         0, // unbuffered: first push has no reader -> timeout
		AcceptChanLen:       1,
		RecvChanPushTimeout: 30,
	}
	factory := &UdpConn{}
	factory.SetConfig(cfg)
	ln, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.(*UdpConn).listener.listenerconn.LocalAddr().String()

	c1, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("client1: %v", err)
	}
	defer c1.Close()
	if _, err := c1.Write([]byte("a")); err != nil {
		t.Fatalf("c1 write: %v", err)
	}
	c2, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("client2: %v", err)
	}
	defer c2.Close()
	// c2's recv push times out (unbuffered), the accept push then fills the
	// one-slot accept channel (c1 occupies it) and times out too: loopRecv
	// closes c2's rejected sonny itself.
	if _, err := c2.Write([]byte("b")); err != nil {
		t.Fatalf("c2 write: %v", err)
	}
	// c1's live sonny also has an unbuffered, unread recv channel.
	if _, err := c1.Write([]byte("c")); err != nil {
		t.Fatalf("c1 second write: %v", err)
	}
	// Worst case: three 30ms push timeouts plus scheduling slack.
	time.Sleep(300 * time.Millisecond)
	luc := ln.(*UdpConn)
	if n := lenSyncMap(&luc.listener.sonny); n != 1 {
		t.Fatalf("rejected c2 sonny must self-close/evict, map len = %d", n)
	}
}

// Accept must skip stale entries (missing from the map or already closed)
// and return the next live sonny; RecvChanPushTimeout<=0 selects the
// non-blocking push path.
func TestHardenUdpAcceptSkipsStale(t *testing.T) {
	cfg := &UdpConfig{
		MaxPacketSize:       1024,
		RecvChanLen:         16,
		AcceptChanLen:       16,
		RecvChanPushTimeout: 0, // non-blocking push branch
	}
	factory := &UdpConn{}
	factory.SetConfig(cfg)
	ln, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	luc := ln.(*UdpConn)
	addr := luc.listener.listenerconn.LocalAddr().String()

	missingDst := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 59999}
	missing := &UdpConn{config: cfg, listenersonny: &udpConnListenerSonny{dstaddr: missingDst, recvch: common.NewChannel(1)}}
	luc.listener.accept.Write(missing)

	storedDst := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 59998}
	stored := &UdpConn{config: cfg, listenersonny: &udpConnListenerSonny{dstaddr: storedDst, isclose: 1, recvch: common.NewChannel(1)}}
	luc.listener.sonny.Store(storedDst.String(), stored)
	luc.listener.accept.Write(stored)

	got := make(chan Conn, 1)
	go func() {
		s, aerr := ln.Accept()
		if aerr == nil {
			got <- s
		}
	}()

	cli, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer cli.Close()
	if _, err := cli.Write([]byte("live-udp-packet")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case srv := <-got:
		defer srv.Close()
		buf := make([]byte, 32)
		n, rerr := srv.Read(buf)
		if rerr != nil || string(buf[:n]) != "live-udp-packet" {
			t.Fatalf("live sonny read n=%d err=%v", n, rerr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Accept must skip stale entries and return the live sonny")
	}
}

// GetConfig lazily installs defaults when no config was ever set.
func TestHardenUdpGetConfigLazyDefault(t *testing.T) {
	c := &UdpConn{}
	if c.GetConfig().RecvChanLen != 128 {
		t.Fatal("GetConfig on a zero-value UdpConn must install defaults")
	}
}

// Closing the receive socket out from under a running loopRecv surfaces the
// read error while the group has not been stopped.
func TestHardenUdpLoopRecvSocketError(t *testing.T) {
	ln, err := (&UdpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	luc := ln.(*UdpConn)
	_ = luc.listener.listenerconn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if luc.listener.wg.IsExit() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = ln.Close()
}

// The post-connect cancel window of UDP Dial is a few-instruction race with
// no blocking hook (connected UDP setup is instant); spinning Close callers
// against a live listener exercise it best-effort while asserting
// termination and cleanup.
func TestHardenUdpDialCancelWindowHammer(t *testing.T) {
	ln, err := (&UdpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.(*UdpConn).listener.listenerconn.LocalAddr().String()
	go func() {
		for {
			if _, aerr := ln.Accept(); aerr != nil {
				return
			}
		}
	}()

	const dialCount = 3000
	c := &UdpConn{}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = c.Close()
				}
			}
		}()
	}
	var delivered, aborted int
	for i := 0; i < dialCount; i++ {
		conn, derr := c.Dial(addr)
		if derr != nil {
			aborted++
			continue
		}
		delivered++
		_ = conn.Close()
	}
	close(stop)
	wg.Wait()
	t.Logf("udp cancel hammer: %d dials, %d delivered, %d aborted",
		dialCount, delivered, aborted)
	if delivered+aborted != dialCount {
		t.Fatal("every Dial must finish exactly once")
	}
}

func lenSyncMap(m *sync.Map) int {
	n := 0
	m.Range(func(_, _ interface{}) bool { n++; return true })
	return n
}

// A connected UDP dialer round-trips through the listener: covers dialer
// Read/Write and the live-sonny push path.
func TestHardenUdpDialerRoundTrip(t *testing.T) {
	ln, err := (&UdpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.(*UdpConn).listener.listenerconn.LocalAddr().String()
	accepted := make(chan Conn, 1)
	go func() {
		s, aerr := ln.Accept()
		if aerr == nil {
			accepted <- s
		}
	}()
	cli, err := (&UdpConn{}).Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	if !strings.Contains(cli.Info(), "<--udp-->") {
		t.Fatalf("dialer Info = %q", cli.Info())
	}
	if _, err := cli.Write([]byte("udp-dialer-16!")); err != nil {
		t.Fatalf("dialer write: %v", err)
	}
	srv := <-accepted
	defer srv.Close()
	buf := make([]byte, 32)
	n, err := srv.Read(buf)
	if err != nil || string(buf[:n]) != "udp-dialer-16!" {
		t.Fatalf("server read n=%d err=%v", n, err)
	}
	if _, err := srv.Write(buf[:n]); err != nil {
		t.Fatalf("server echo: %v", err)
	}
	echo := make([]byte, 32)
	if en, eerr := cli.Read(echo); eerr != nil || string(echo[:en]) != "udp-dialer-16!" {
		t.Fatalf("dialer echo n=%d err=%v", en, eerr)
	}
}

// ---------- rhttp harden ----------

// Bare/listener error branches that need no peer traffic.
func TestHardenRhttpEmptyAndErrors(t *testing.T) {
	c := &RhttpConn{}
	if got := c.Info(); got != "empty http conn" {
		t.Fatalf("empty Info=%q", got)
	}
	if _, err := c.Read(nil); err == nil || !strings.Contains(err.Error(), "read empty buffer") {
		t.Fatalf("nil Read err=%v", err)
	}
	if _, err := c.Write(nil); err == nil || !strings.Contains(err.Error(), "write empty data") {
		t.Fatalf("nil Write err=%v", err)
	}
	if _, err := c.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "empty conn") {
		t.Fatalf("bare Read err=%v", err)
	}
	if _, err := c.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "empty conn") {
		t.Fatalf("bare Write err=%v", err)
	}
	if _, err := c.Accept(); err == nil || !strings.Contains(err.Error(), "not listen") {
		t.Fatalf("bare Accept err=%v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("bare Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := c.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "read closed conn") {
		t.Fatalf("closed Read err=%v", err)
	}
	if _, err := c.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "write closed conn") {
		t.Fatalf("closed Write err=%v", err)
	}

	fac := &RhttpConn{}
	ln, err := fac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	lc := ln.(*RhttpConn)
	if got := lc.Info(); !strings.Contains(got, "rhttp listener") {
		t.Fatalf("listener Info=%q", got)
	}
	if _, err := lc.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "listener can not be read") {
		t.Fatalf("listener Read err=%v", err)
	}
	if _, err := lc.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "listener can not be write") {
		t.Fatalf("listener Write err=%v", err)
	}
	addr := lc.listener.listenerconn.Addr().String()
	if _, err := (&RhttpConn{}).Listen(addr); err == nil {
		t.Fatal("rebind same addr must fail")
	}
	if _, err := (&RhttpConn{}).Listen(":::::not-an-addr"); err == nil {
		t.Fatal("resolve garbage must fail")
	}

	accErr := make(chan error, 1)
	go func() {
		_, e := lc.Accept()
		accErr <- e
	}()
	time.Sleep(100 * time.Millisecond)
	if err := lc.Close(); err != nil {
		t.Fatalf("listener Close: %v", err)
	}
	select {
	case err := <-accErr:
		if err == nil {
			t.Fatal("Accept after close must fail")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Accept did not unblock on listener close")
	}
}

// Server-side Read polls an empty recvb until data arrives (100ms sleep
// branch) and the dialer data loop round-trips the message.
func TestHardenRhttpReadWaitsForData(t *testing.T) {
	fac := &RhttpConn{}
	ln, err := fac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.(*RhttpConn).listener.listenerconn.Addr().String()

	got := make(chan []byte, 1)
	go func() {
		s, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer s.Close()
		buf := make([]byte, 64)
		n, rerr := s.Read(buf)
		if rerr == nil {
			got <- buf[:n]
		}
	}()

	cli, err := (&RhttpConn{}).Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	// Give the server-side Read time to enter the empty-buffer poll sleep.
	time.Sleep(250 * time.Millisecond)
	if _, err := cli.Write([]byte("rhttp-wait-9!")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case b := <-got:
		if string(b) != "rhttp-wait-9!" {
			t.Fatalf("server got %q", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server Read never got data")
	}
}

// Whitebox sendb/recvb backpressure against crafted dialer groups:
//   - stopped wg: Write queues what fits then returns the partial count with
//     "closed conn" (cur>0), and again returns zero once full (cur==0);
//     Read on an empty recvb with exited wg returns "closed conn";
//   - live wg: Close landing while Write is blocked with/without queued bytes
//     returns the partial/zero counts with "write closed conn".
func TestHardenRhttpWriteBackpressureAndClose(t *testing.T) {
	cfg4 := DefaultHttpConfig()
	cfg4.BufferSize = 4
	stopped := func() *RhttpConn {
		wg := thread.NewGroup("harden rhttp stopped", nil, nil)
		wg.Stop()
		return &RhttpConn{
			config: cfg4,
			dialer: &httpConnDialer{wg: wg},
			sendb:  list.NewRBuffergo(cfg4.BufferSize, true),
			recvb:  list.NewRBuffergo(cfg4.BufferSize, true),
		}
	}

	u := stopped()
	n, err := u.Write([]byte("abcd1234"))
	if n != 4 || err == nil {
		t.Fatalf("partial blocked Write n=%d err=%v", n, err)
	}
	n, err = u.Write([]byte("zz"))
	if n != 0 || err == nil {
		t.Fatalf("full-buffer Write n=%d err=%v", n, err)
	}
	if _, err := u.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "closed conn") {
		t.Fatalf("empty read with exited wg err=%v", err)
	}

	live := func(capSize int) *RhttpConn {
		return &RhttpConn{
			config: DefaultHttpConfig(),
			dialer: &httpConnDialer{wg: thread.NewGroup("harden rhttp live", nil, nil)},
			sendb:  list.NewRBuffergo(capSize, true),
			recvb:  list.NewRBuffergo(capSize, true),
		}
	}
	waitWrite := func(c *RhttpConn, p []byte) (int, error) {
		type res struct {
			n   int
			err error
		}
		ch := make(chan res, 1)
		go func() {
			wN, wErr := c.Write(p)
			ch <- res{wN, wErr}
		}()
		// Ensure Write has entered the blocked sleep at least once.
		time.Sleep(250 * time.Millisecond)
		if cerr := c.Close(); cerr != nil {
			t.Fatalf("Close: %v", cerr)
		}
		select {
		case r := <-ch:
			return r.n, r.err
		case <-time.After(3 * time.Second):
			t.Fatal("Write did not unblock on Close")
			return 0, nil
		}
	}

	// Cap 8 prefilled with 6: two more bytes fit before Close (partial).
	pc := live(8)
	pc.sendb.Write([]byte("123456"))
	if n, err := waitWrite(pc, []byte("ABCD")); n != 2 || err == nil {
		t.Fatalf("partial close Write n=%d err=%v", n, err)
	}
	// Cap 4 already full: zero bytes queued before Close.
	zc := live(4)
	zc.sendb.Write([]byte("1234"))
	if n, err := waitWrite(zc, []byte("ABCD")); n != 0 || err == nil {
		t.Fatalf("zero close Write n=%d err=%v", n, err)
	}
}

// A listener Close with a factory cancel still attached must invoke it.
func TestHardenRhttpListenerCancelOnClose(t *testing.T) {
	ln, err := (&RhttpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	lc := ln.(*RhttpConn)
	cancelled := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	lc.cancel = func() {
		cancel()
		close(cancelled)
	}
	if err := lc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("listener cancel was not invoked")
	}
	_ = ctx
}

// Concurrent Close callers race the first isclose check; the loser rechecks
// under closelock and returns early. Outcomes are idempotent, the test only
// asserts no panic/deadlock and best-effort covers the locked recheck.
func TestHardenRhttpCloseDoubleHammer(t *testing.T) {
	fac := &RhttpConn{}
	ln, err := fac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.(*RhttpConn).listener.listenerconn.Addr().String()
	go func() {
		for {
			s, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(s Conn) {
				buf := make([]byte, 64)
				for {
					if _, rerr := s.Read(buf); rerr != nil {
						return
					}
				}
			}(s)
		}
	}()
	for i := 0; i < 200; i++ {
		cli, derr := (&RhttpConn{}).Dial(addr)
		if derr != nil {
			t.Fatalf("dial %d: %v", i, derr)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for j := 0; j < 16; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_ = cli.Close()
			}()
		}
		close(start)
		wg.Wait()
	}
}

// The dialer drain loop (after updateDialerSonny exits) is only entered on a
// natural loop exit: Close() flips wg.IsExit before Join, so a close-driven
// exit skips the loop. Retry exhaustion against a scripted server reaches it:
// with queued recvb data the loop polls (10ms sleeps) until CloseWaitTimeoutMs
// and breaks on the timeout; with an empty recvb it breaks immediately. In
// both cases the self-initiated ProtoClose runs because Close() never did.
func TestHardenRhttpDialerDrainAndSelfClose(t *testing.T) {
	newScripted := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("type") {
			case ProtoConnnect, ProtoClose:
				w.WriteHeader(ProtoCodeOK)
			default:
				w.WriteHeader(ProtoCodeFail)
				_, _ = w.Write([]byte("injected data fail"))
			}
		}))
	}
	dialOne := func(t *testing.T, ts *httptest.Server, seedRecv bool, waitMs int) *RhttpConn {
		cfg := DefaultHttpConfig()
		cfg.MaxRetryNum = 0
		cfg.CloseWaitTimeoutMs = waitMs
		self := &RhttpConn{}
		self.SetConfig(cfg)
		conn, err := self.Dial(ts.URL)
		if err != nil {
			t.Fatalf("dial scripted: %v", err)
		}
		sc := conn.(*RhttpConn)
		if seedRecv && !sc.recvb.Write([]byte("x")) {
			t.Fatal("recvb seed write failed")
		}
		return sc
	}
	waitExit := func(t *testing.T, sc *RhttpConn, minD time.Duration) {
		start := time.Now()
		deadline := start.Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if sc.dialer.wg.IsExit() {
				if d := time.Since(start); d < minD {
					t.Fatalf("drain took %v, want >= %v", d, minD)
				}
				if sc.isclose.Load() {
					t.Fatal("natural exit must stay unclosed; self ProtoClose path requires it")
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("data loop did not exit after retry exhaustion + self close")
	}

	ts1 := newScripted()
	sc1 := dialOne(t, ts1, true, 50)
	waitExit(t, sc1, 45*time.Millisecond)
	ts1.Close()

	ts2 := newScripted()
	sc2 := dialOne(t, ts2, false, 5000)
	waitExit(t, sc2, 0)
	ts2.Close()
}

// Accept skips queued entries whose sonny is missing from the map or already
// closed, then returns the next live sonny pushed by a real Connect.
func TestHardenRhttpAcceptSkipsStale(t *testing.T) {
	cfg := DefaultHttpConfig()
	cfg.AcceptChanLen = 4
	fac := &RhttpConn{}
	fac.SetConfig(cfg)
	ln, err := fac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	lc := ln.(*RhttpConn)
	addr := lc.listener.listenerconn.Addr().String()

	// checkSonnyClose scans the map and dereferences listenersonny, so the
	// injected closed entry needs a real (empty) sonny shell.
	dead := &RhttpConn{
		id:            "/dead-id",
		config:        cfg,
		listenersonny: &httpConnListenerSonny{listener: lc.listener},
	}
	dead.isclose.Store(true)
	lc.listener.sonny.Store(dead.id, dead)
	if !lc.listener.accept.WriteTimeout(dead, 100) {
		t.Fatal("queue dead failed")
	}
	ghost := &RhttpConn{id: "/ghost-id", config: cfg}
	if !lc.listener.accept.WriteTimeout(ghost, 100) {
		t.Fatal("queue ghost failed")
	}

	got := make(chan Conn, 1)
	go func() {
		s, aerr := lc.Accept()
		if aerr == nil {
			got <- s
		}
	}()
	// Let Accept drain the two stale entries and park on the channel.
	time.Sleep(200 * time.Millisecond)

	cli, err := (&RhttpConn{}).Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	select {
	case s := <-got:
		if s.(*RhttpConn).id == dead.id || s.(*RhttpConn).id == ghost.id {
			t.Fatalf("Accept returned stale sonny %s", s.(*RhttpConn).id)
		}
		_ = s.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("live sonny was not accepted")
	}
}

// hardenRawHTTP speaks a hand-written HTTP/1.1 request; when readResp is
// false it returns after a short wait (used for connection-cut cases).
func hardenRawHTTP(t *testing.T, addr, req string, readResp bool) string {
	t.Helper()
	cn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("raw dial: %v", err)
	}
	defer cn.Close()
	if _, err := cn.Write([]byte(req)); err != nil {
		t.Fatalf("raw write: %v", err)
	}
	if !readResp {
		time.Sleep(150 * time.Millisecond)
		return ""
	}
	_ = cn.SetReadDeadline(time.Now().Add(3 * time.Second))
	b, err := io.ReadAll(cn)
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	return string(b)
}

// Server-side protocol error branches: malformed URL/params/index, stale
// closed sonny, body size limits, mid-body connection cuts, and accept
// backlog rejection.
func TestHardenRhttpServerProtocolErrors(t *testing.T) {
	newL := func(cfg *HttpConfig) (*RhttpConn, string) {
		f := &RhttpConn{}
		f.SetConfig(cfg)
		ln, err := f.Listen("127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		return ln.(*RhttpConn), ln.(*RhttpConn).listener.listenerconn.Addr().String()
	}
	expectCode := func(name string, code, want int) {
		if code != want {
			t.Fatalf("%s: code=%d want %d", name, code, want)
		}
	}

	lc, addr := newL(DefaultHttpConfig())
	defer lc.Close()
	base := "http://" + addr
	post := func(target string, body []byte) (int, string) {
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		resp, err := http.Post(base+target, "application/octet-stream", rdr)
		if err != nil {
			t.Fatalf("POST %s: %v", target, err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	// url.Parse failure on the request target.
	// "//[" is a legal HTTP request target but url.Parse rejects the
	// unterminated IPv6 host literal; the handler answers ProtoCodeFail.
	r := hardenRawHTTP(t, addr,
		"POST //[ HTTP/1.1\r\nHost: h\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", true)
	if !strings.Contains(r, "404") || !strings.Contains(r, "url Parse fail") {
		t.Fatalf("bad url resp=%q", r)
	}

	// A live sonny exercised through every parameter/body validation.
	expectCode("connect", mustCode(post("/e1?type=connect", nil)), ProtoCodeOK)
	expectCode("no type", mustCode(post("/e1", nil)), ProtoCodeFail)
	expectCode("bad type", mustCode(post("/e1?type=nope", nil)), ProtoCodeFail)
	expectCode("no index", mustCode(post("/e1?type=data", nil)), ProtoCodeFail)
	expectCode("bad index", mustCode(post("/e1?type=data&index=abc", nil)), ProtoCodeFail)
	// index == MaxMsgIndex-1 wraps to expectIndex 0: treated as a duplicate
	// of a not-yet-received frame, answered from lastSend (empty).
	expectCode("dup wrap", mustCode(post("/e1?type=data&index=99", nil)), ProtoCodeOK)
	expectCode("index diff", mustCode(post("/e1?type=data&index=5", nil)), ProtoCodeFail)
	expectCode("unknown id", mustCode(post("/missing?type=data&index=0", nil)), ProtoCodeFail)
	expectCode("close", mustCode(post("/e1?type=close", nil)), ProtoCodeOK)

	// Body over the default MaxPacketSize cap is rejected.
	expectCode("connect big", mustCode(post("/big?type=connect", nil)), ProtoCodeOK)
	expectCode("too large",
		mustCode(post("/big?type=data&index=0", make([]byte, 100*1024+1))), ProtoCodeFail)

	// An existing-but-closed sonny is refused before any parsing.
	dead := &RhttpConn{
		id:            "/e2",
		config:        lc.config,
		listenersonny: &httpConnListenerSonny{listener: lc.listener, lastRecvTime: time.Now()},
	}
	dead.isclose.Store(true)
	lc.listener.sonny.Store(dead.id, dead)
	expectCode("closed sonny", mustCode(post("/e2?type=data&index=0", nil)), ProtoCodeFail)
	lc.listener.sonny.Delete(dead.id)

	// MaxPacketSize 0 falls back to the 100KB default; a small body works.
	zeroCfg := DefaultHttpConfig()
	zeroCfg.MaxPacketSize = 0
	lcZero, addrZero := newL(zeroCfg)
	defer lcZero.Close()
	zResp, err := http.Post("http://"+addrZero+"/z1?type=connect", "application/octet-stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	zResp.Body.Close()
	zResp2, err := http.Post("http://"+addrZero+"/z1?type=data&index=0",
		"application/octet-stream", bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	zb, _ := io.ReadAll(zResp2.Body)
	zResp2.Body.Close()
	expectCode("default body limit", zResp2.StatusCode, ProtoCodeOK)
	_ = zb
	// Server-side body read error: declare 100 bytes, send 5, cut the TCP.
	hardenRawHTTP(t, addrZero,
		"POST /z1?type=data&index=1 HTTP/1.1\r\nHost: h\r\nContent-Length: 100\r\n\r\n12345",
		false)

	// Accept backlog with RecvChanPushTimeout 0 (defaults to 100ms): the
	// second Connect fails pushing and is answered ProtoCodeFull; a real
	// Dial there takes the client-side code!=OK failure path.
	fullCfg := DefaultHttpConfig()
	fullCfg.AcceptChanLen = 1
	fullCfg.RecvChanPushTimeout = 0
	lcFull, addrFull := newL(fullCfg)
	defer lcFull.Close()
	expectCode("f1", mustCode(postFull(addrFull, "/f1?type=connect")), ProtoCodeOK)
	expectCode("f2 rejected", mustCode(postFull(addrFull, "/f2?type=connect")), ProtoCodeFull)
	if _, err := (&RhttpConn{}).Dial(addrFull); err == nil {
		t.Fatal("Dial against full accept queue must fail with ProtoCodeFull")
	}
}

// Deterministic coverage of the under-lock isclose recheck: hold closelock
// externally so every contender passes the first check and blocks on the
// mutex; once released, all waiters after the winner hit the locked recheck.
func TestHardenRhttpCloseLockedRecheck(t *testing.T) {
	fac := &RhttpConn{}
	ln, err := fac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.(*RhttpConn).listener.listenerconn.Addr().String()
	go func() {
		for {
			s, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(s Conn) {
				buf := make([]byte, 64)
				for {
					if _, rerr := s.Read(buf); rerr != nil {
						return
					}
				}
			}(s)
		}
	}()
	cli, err := (&RhttpConn{}).Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	rc := cli.(*RhttpConn)
	rc.closelock.Lock()
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = rc.Close()
		}()
	}
	// All contenders pass the first isclose check and park on the mutex.
	close(start)
	time.Sleep(150 * time.Millisecond)
	rc.closelock.Unlock()
	wg.Wait()
}

func mustCode(code int, _ string) int { return code }

func postFull(addr, target string) (int, string) {
	resp, err := http.Post("http://"+addr+target, "application/octet-stream", nil)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// Client-side edge paths: refused Dial, one-off postData transport with nil
// context / non-positive timeout, malformed request URL, truncated response
// body, gControl hooks for both transports, and a dialer whose ctx was
// detached (nil fallback).
func TestHardenRhttpClientEdgePaths(t *testing.T) {
	// Refused Dial: error path + takeCancel.
	ll, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddr := ll.Addr().String()
	_ = ll.Close()
	if _, err := (&RhttpConn{}).Dial(closedAddr); err == nil {
		t.Fatal("dial to closed port must fail")
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(ProtoCodeOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	cfg := DefaultHttpConfig()
	cfg.RequestTimeoutMs = -1 // exercises the <=0 default branch
	bare := &RhttpConn{}
	bare.SetConfig(cfg)
	// 刻意传 nil：覆盖 postData 里 ctx == nil 回退到 Background 的分支
	code, body, err := bare.postData(nil, ts.URL, []byte("x")) //nolint:staticcheck // nil ctx is intentional here
	if err != nil || code != ProtoCodeOK || string(body) != "ok" {
		t.Fatalf("one-off post code=%d body=%q err=%v", code, body, err)
	}
	if _, _, err := bare.postData(context.Background(), "://bad url", nil); err == nil {
		t.Fatal("malformed URL must fail request construction")
	}

	// Truncated response body surfaces ReadAll failure.
	tsCut := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(ProtoCodeOK)
		_, _ = w.Write([]byte("x"))
	}))
	defer tsCut.Close()
	if _, _, err := bare.postData(context.Background(), tsCut.URL, nil); err == nil {
		t.Fatal("truncated body must return read error")
	}

	// gControlOnConnSetup hooks for h1 and h2c dial transports.
	ln, err := (&RhttpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.(*RhttpConn).listener.listenerconn.Addr().String()
	go func() {
		for {
			s, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(s Conn) {
				buf := make([]byte, 64)
				for {
					if _, rerr := s.Read(buf); rerr != nil {
						return
					}
				}
			}(s)
		}
	}()
	old := gControlOnConnSetup
	RegisterDialerController(func(network, address string, raw syscall.RawConn) error { return nil })
	c1, err := (&RhttpConn{}).Dial(addr)
	if err != nil {
		t.Fatalf("h1 dial with control: %v", err)
	}
	c2, err := (&RhttpConn{}).Dial("h2c://" + addr)
	if err != nil {
		t.Fatalf("h2c dial with control: %v", err)
	}

	_ = c1.Close()
	_ = c2.Close()
	gControlOnConnSetup = old

	// Detached dialer ctx: a dialer whose ctx is nil before its worker
	// starts makes its data POSTs against context.Background. Setting the
	// field after startup would race with updateDialerSonny, so the whole
	// conn is constructed race-free and driven against a local server.
	var hits int32
	tsBg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(ProtoCodeOK)
	}))
	defer tsBg.Close()
	bgWg := thread.NewGroup("harden rhttp nil-ctx dialer", nil, nil)
	bgCfg := DefaultHttpConfig()
	bgConn := &RhttpConn{
		id:     "harden-nilctx",
		config: bgCfg,
		dialer: &httpConnDialer{wg: bgWg, url: tsBg.URL + "/x", retry: 0, ctx: nil, rt: nil},
		sendb:  list.NewRBuffergo(bgCfg.BufferSize, true),
		recvb:  list.NewRBuffergo(bgCfg.BufferSize, true),
	}
	bgWg.Go("nilctx updateDialerSonny", bgConn.updateDialerSonny)
	bgDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(bgDeadline) && atomic.LoadInt32(&hits) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	bgWg.Stop()
	_ = bgWg.Join()
	if atomic.LoadInt32(&hits) == 0 {
		t.Fatal("nil-ctx dialer never posted")
	}
}

// Whitebox lifecycle: GetConfig lazy default, loopRecv nil-server failure,
// Serve error after a raw listener close, sonny Info and sonny-side cancel,
// and the HBTimeout<=0 default inside checkSonnyClose.
func TestHardenRhttpWhiteboxLifecycle(t *testing.T) {
	if cfg := (&RhttpConn{}).GetConfig(); cfg == nil || cfg.MaxPacketSize == 0 {
		t.Fatalf("GetConfig lazy default: %+v", cfg)
	}

	// loopRecv with no http.Server returns an error and marks the group exit.
	wg := thread.NewGroup("harden rhttp nil srv", nil, nil)
	lcNil := &RhttpConn{config: DefaultHttpConfig(), listener: &httpConnListener{wg: wg}}
	wg.Go("loopRecv", lcNil.loopRecv)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !wg.IsExit() {
		time.Sleep(5 * time.Millisecond)
	}
	if !wg.IsExit() {
		t.Fatal("loopRecv nil server must exit the group")
	}

	// Raw listener close (not Shutdown) makes Serve return a non-shutdown
	// error which loopRecv propagates.
	fac := &RhttpConn{}
	ln, err := fac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lc := ln.(*RhttpConn)
	_ = lc.listener.listenerconn.Close()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !lc.listener.wg.IsExit() {
		time.Sleep(5 * time.Millisecond)
	}
	if !lc.listener.wg.IsExit() {
		t.Fatal("Serve error must mark listener group exit")
	}
	if err := lc.Close(); err != nil {
		t.Fatalf("cleanup close: %v", err)
	}

	// Sonny Info + sonny-side cancel on Close.
	ln2, err := (&RhttpConn{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()
	addr2 := ln2.(*RhttpConn).listener.listenerconn.Addr().String()
	accepted := make(chan Conn, 1)
	go func() {
		s, aerr := ln2.Accept()
		if aerr == nil {
			accepted <- s
		}
	}()
	cli, err := (&RhttpConn{}).Dial(addr2)
	if err != nil {
		t.Fatal(err)
	}
	srv := <-accepted
	if !strings.Contains(srv.Info(), "listenersonny") {
		t.Fatalf("sonny Info=%q", srv.Info())
	}
	cancelled := make(chan struct{})
	ctx2, cancel2 := context.WithCancel(context.Background())
	srv.(*RhttpConn).cancel = func() {
		cancel2()
		close(cancelled)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("sonny close: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("sonny cancel not invoked")
	}
	_ = ctx2
	_ = cli.Close()

	// HBTimeoutMs 0 -> default heart beat inside checkSonnyClose scan.
	hbCfg := DefaultHttpConfig()
	hbCfg.HBTimeoutMs = 0
	hbFac := &RhttpConn{}
	hbFac.SetConfig(hbCfg)
	ln3, err := hbFac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln3.Close()
	addr3 := ln3.(*RhttpConn).listener.listenerconn.Addr().String()
	cli3, err := (&RhttpConn{}).Dial(addr3)
	if err != nil {
		t.Fatal(err)
	}
	defer cli3.Close()
	// checkSonnyClose scans once per second.
	time.Sleep(1100 * time.Millisecond)
}

// ProtoCodeFull stuck longer than the heart-beat limit breaks the data loop.
// With HBTimeoutMs<=0 the limit defaults to 10s, so this test takes ~10s;
// it uniquely covers the defensive default and the timeout break.
func TestHardenRhttpFullStuckBreaksLoop(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("type") {
		case ProtoConnnect, ProtoClose:
			w.WriteHeader(ProtoCodeOK)
		default:
			w.WriteHeader(ProtoCodeFull)
		}
	}))
	defer ts.Close()
	cfg := DefaultHttpConfig()
	cfg.MaxRetryNum = 100000
	cfg.HBTimeoutMs = 0
	self := &RhttpConn{}
	self.SetConfig(cfg)
	conn, err := self.Dial(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	sc := conn.(*RhttpConn)
	start := time.Now()
	deadline := start.Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if sc.dialer.wg.IsExit() {
			if d := time.Since(start); d < 9500*time.Millisecond {
				t.Fatalf("full-stuck broke after %v, want ~10s", d)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("full-stuck loop did not break")
}

// ---------- rudp harden ----------

// Bare/error branches: name/info, empty buffers, empty/listener/closed
// Read+Write, resolve/listen failures, lazy config and Set/GetConfig.
func TestHardenRudpEmptyAndErrors(t *testing.T) {
	c := &RudpConn{}
	if c.Name() != "rudp" {
		t.Fatalf("Name=%q", c.Name())
	}
	if c.Info() != "empty rudp conn" {
		t.Fatalf("Info=%q", c.Info())
	}
	if _, err := c.Read(nil); err == nil || !strings.Contains(err.Error(), "read empty buffer") {
		t.Fatalf("nil Read err=%v", err)
	}
	if _, err := c.Write(nil); err == nil || !strings.Contains(err.Error(), "write empty data") {
		t.Fatalf("nil Write err=%v", err)
	}
	if _, err := c.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "empty conn") {
		t.Fatalf("empty Read err=%v", err)
	}
	if _, err := c.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "empty conn") {
		t.Fatalf("empty Write err=%v", err)
	}
	lsh := &RudpConn{listener: &rudpConnListener{}}
	if _, err := lsh.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "listener can not be read") {
		t.Fatalf("listener Read err=%v", err)
	}
	if _, err := lsh.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "listener can not be write") {
		t.Fatalf("listener Write err=%v", err)
	}
	if _, err := c.Dial("127.0.0.1:70000"); err == nil {
		t.Fatal("dial with invalid port must fail")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "read closed conn") {
		t.Fatalf("closed Read err=%v", err)
	}
	if _, err := c.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "write closed conn") {
		t.Fatalf("closed Write err=%v", err)
	}
	if cfg := c.GetConfig(); cfg == nil || cfg.MaxPacketSize == 0 {
		t.Fatalf("GetConfig lazy default %+v", cfg)
	}
	cfg2 := DefaultRudpConfig()
	cfg2.MaxPacketSize = 4242
	c.SetConfig(cfg2)
	if c.GetConfig().MaxPacketSize != 4242 {
		t.Fatal("SetConfig/GetConfig mismatch")
	}

	fac := &RudpConn{}
	ln, err := fac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lc := ln.(*RudpConn)
	if !strings.HasPrefix(lc.Info(), "rudp--") {
		t.Fatalf("listener Info=%q", lc.Info())
	}
	addr := lc.listener.listenerconn.LocalAddr().String()
	if _, err := (&RudpConn{}).Listen(addr); err == nil {
		t.Fatal("rebind must fail")
	}
	if _, err := (&RudpConn{}).Listen(":::::not-an-addr"); err == nil {
		t.Fatal("bad resolve must fail")
	}
	accErr := make(chan error, 1)
	go func() { _, e := lc.Accept(); accErr <- e }()
	time.Sleep(100 * time.Millisecond)
	if err := lc.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-accErr:
		if err == nil {
			t.Fatal("Accept must fail after close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Accept did not unblock")
	}
}

// Whitebox FrameMgr backpressure against crafted dialer groups: stopped wg
// returns partial/zero "closed conn" counts; Close landing during a blocked
// Write (full or partly-full buffer) returns the closed-conn counts.
func TestHardenRudpBufferLifecycle(t *testing.T) {
	cfg := DefaultRudpConfig()
	cfg.BufferSize = 256
	newFM := func() *FrameMgr {
		return NewFrameMgr(cfg.CutSize, cfg.MaxId, cfg.BufferSize, cfg.MaxWin,
			cfg.ResendTimems, cfg.Compress, cfg.Stat)
	}
	stoppedGroup := func() *thread.Group {
		wg := thread.NewGroup("harden rudp stopped", nil, nil)
		wg.Stop()
		return wg
	}

	u := &RudpConn{config: cfg, dialer: &rudpConnDialer{fm: newFM(), wg: stoppedGroup()}}
	if _, err := u.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "closed conn") {
		t.Fatalf("empty read exited wg err=%v", err)
	}
	n, err := u.Write(make([]byte, 1024))
	if n != 256 || err == nil {
		t.Fatalf("partial blocked write n=%d err=%v", n, err)
	}
	n, err = u.Write(make([]byte, 8))
	if n != 0 || err == nil {
		t.Fatalf("full buffer write n=%d err=%v", n, err)
	}

	blockedClose := func(prefill int, p []byte) int {
		wg := thread.NewGroup("harden rudp live", nil, nil)
		live := &RudpConn{config: cfg, dialer: &rudpConnDialer{fm: newFM(), wg: wg}}
		if prefill > 0 {
			live.dialer.fm.WriteSendBuffer(make([]byte, prefill))
		}
		type res struct{ n int }
		ch := make(chan res, 1)
		go func() { wN, _ := live.Write(p); ch <- res{wN} }()
		time.Sleep(250 * time.Millisecond)
		if err := live.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		select {
		case r := <-ch:
			return r.n
		case <-time.After(3 * time.Second):
			t.Fatal("blocked write did not unblock")
			return -1
		}
	}
	// Empty buffer: writer fills 256, then Close -> partial count.
	if n := blockedClose(0, make([]byte, 1024)); n != 256 {
		t.Fatalf("partial close write n=%d", n)
	}
	// Pre-full buffer: Close before any byte -> zero.
	if n := blockedClose(256, make([]byte, 16)); n != 0 {
		t.Fatalf("zero close write n=%d", n)
	}
}

// Deterministic locked double-close recheck (same hold-the-mutex technique):
// contenders pass the first check while the test holds closelock, then the
// waiters after the winner recheck under the lock.
func TestHardenRudpCloseLockedRecheck(t *testing.T) {
	fac := &RudpConn{}
	ln, err := fac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.(*RudpConn).listener.listenerconn.LocalAddr().String()
	go func() {
		for {
			s, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_ = s
		}
	}()
	cli, err := (&RudpConn{}).Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	rc := cli.(*RudpConn)
	rc.closelock.Lock()
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = rc.Close()
		}()
	}
	close(start)
	time.Sleep(150 * time.Millisecond)
	rc.closelock.Unlock()
	wg.Wait()
}

// A Dial through a registered Control hook covers the non-nil controller
// branch for the UDP dialer.
func TestHardenRudpDialControlHook(t *testing.T) {
	fac := &RudpConn{}
	ln, err := fac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.(*RudpConn).listener.listenerconn.LocalAddr().String()
	go func() {
		for {
			if _, aerr := ln.Accept(); aerr != nil {
				return
			}
		}
	}()
	old := gControlOnConnSetup
	RegisterDialerController(func(network, address string, raw syscall.RawConn) error { return nil })
	cli, err := (&RudpConn{}).Dial(addr)
	if err != nil {
		t.Fatalf("dial with control: %v", err)
	}
	gControlOnConnSetup = old
	_ = cli.Close()

	RegisterDialerController(func(network, address string, raw syscall.RawConn) error {
		return errors.New("control denied")
	})
	if _, err := (&RudpConn{}).Dial(addr); err == nil {
		t.Fatal("dial with failing control must fail")
	}
	gControlOnConnSetup = old
}

// Best-effort cancel-window hammer for the UDP DialContext: the Control hook
// runs right before the kernel connect, so a scheduled Close lands around
// socket setup (post-connect ctx check) or in the handshake loop (factory
// closed). All outcomes are legal; the test asserts termination only.
func TestHardenRudpDialCancelWindowHammer(t *testing.T) {
	fac := &RudpConn{}
	ln, err := fac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.(*RudpConn).listener.listenerconn.LocalAddr().String()
	go func() {
		for {
			if _, aerr := ln.Accept(); aerr != nil {
				return
			}
		}
	}()
	old := gControlOnConnSetup
	defer func() { gControlOnConnSetup = old }()
	for attempt := 0; attempt < 1200; attempt++ {
		c := &RudpConn{}
		delay := time.Duration(attempt%1000) * time.Microsecond
		RegisterDialerController(func(network, address string, raw syscall.RawConn) error {
			go func() {
				time.Sleep(delay)
				_ = c.Close()
			}()
			return nil
		})
		if conn, derr := c.Dial(addr); derr == nil {
			_ = conn.Close()
		}
	}
	RegisterDialerController(func(network, address string, raw syscall.RawConn) error { return nil })
}

func hardenListenUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	a, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	co, err := net.ListenUDP("udp", a)
	if err != nil {
		t.Fatal(err)
	}
	return co
}

// Dial-side failures: garbage handshake reply hits the protobuf failure
// break; a silent peer hits the connect-timeout break. Listener-side: an
// unsolicited datagram starts a sonny whose handshake times out and closes,
// and stale map entries (packet from a closed sonny; closed entry swept
// during another packet's scan) are evicted.
func TestHardenRudpGarbageAndStale(t *testing.T) {
	// Garbage reply during Dial connect loop.
	garbageSrv := hardenListenUDP(t)
	defer garbageSrv.Close()
	go func() {
		buf := make([]byte, 2048)
		for {
			n, sa, err := garbageSrv.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_ = n
			_, _ = garbageSrv.WriteToUDP([]byte("this-is-not-a-rudp-frame!!"), sa)
		}
	}()
	gcfg := DefaultRudpConfig()
	gcfg.ConnectTimeoutMs = 30000
	gfac := &RudpConn{}
	gfac.SetConfig(gcfg)
	if _, err := gfac.Dial(garbageSrv.LocalAddr().String()); err == nil {
		t.Fatal("dial with garbage reply must fail")
	}

	// Silent peer -> connect timeout.
	silent := hardenListenUDP(t)
	defer silent.Close()
	scfg := DefaultRudpConfig()
	scfg.ConnectTimeoutMs = 200
	sfac := &RudpConn{}
	sfac.SetConfig(scfg)
	start := time.Now()
	if _, err := sfac.Dial(silent.LocalAddr().String()); err == nil {
		t.Fatal("dial to silent peer must time out")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("connect timeout took too long")
	}

	// Listener: unsolicited datagram -> sonny handshake timeout self-close.
	lcfg := DefaultRudpConfig()
	lcfg.ConnectTimeoutMs = 200
	lfac := &RudpConn{}
	lfac.SetConfig(lcfg)
	ln, err := lfac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	lc := ln.(*RudpConn)
	sender := hardenListenUDP(t)
	defer sender.Close()
	dst, _ := net.ResolveUDPAddr("udp", lc.listener.listenerconn.LocalAddr().String())
	if _, err := sender.WriteToUDP([]byte("unsolicited"), dst); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	// The sonny map is populated asynchronously by the recv loop; wait until
	// the entry appears before waiting for the timeout self-close.
	for time.Now().Before(deadline) && lenSyncMap(&lc.listener.sonny) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	for time.Now().Before(deadline) && lenSyncMap(&lc.listener.sonny) != 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if lenSyncMap(&lc.listener.sonny) != 0 {
		t.Fatal("timed-out sonny must remove itself")
	}

	// Closed sonny still keyed under the sender address: a packet from it is
	// dropped via the load-closed branch.
	closed := &RudpConn{
		config:        lcfg,
		listenersonny: &rudpConnListenerSonny{listener: lc.listener, dstaddr: sender.LocalAddr().(*net.UDPAddr)},
	}
	closed.isclose.Store(true)
	lc.listener.sonny.Store(sender.LocalAddr().String(), closed)
	if _, err := sender.WriteToUDP([]byte("from-closed"), dst); err != nil {
		t.Fatal(err)
	}
	if _, err := sender.WriteToUDP([]byte("from-closed-2"), dst); err != nil {
		t.Fatal(err)
	}

	// A closed entry for a silent address is swept during the next packet's
	// sonny Range (packet from a fresh sender creates its own sonny).
	silentKey := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9999}
	stale := &RudpConn{
		config:        lcfg,
		listenersonny: &rudpConnListenerSonny{listener: lc.listener, dstaddr: silentKey},
	}
	stale.isclose.Store(true)
	lc.listener.sonny.Store(silentKey.String(), stale)
	sender2 := hardenListenUDP(t)
	defer sender2.Close()
	if _, err := sender2.WriteToUDP([]byte("fresh"), dst); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := lc.listener.sonny.Load(silentKey.String()); !ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := lc.listener.sonny.Load(silentKey.String()); ok {
		t.Fatal("closed stale sonny must be swept")
	}
	if err := lc.Close(); err != nil {
		t.Fatal(err)
	}
}

// Accept skips queued closed/missing sonnies, and a full accept channel makes
// a completed handshake self-close instead of being delivered.
func TestHardenRudpAcceptStaleAndBacklog(t *testing.T) {
	rudpCfg := DefaultRudpConfig()
	rudpCfg.AcceptChanLen = 4
	fac := &RudpConn{}
	fac.SetConfig(rudpCfg)
	ln, err := fac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	lc := ln.(*RudpConn)
	addr := lc.listener.listenerconn.LocalAddr().String()

	deadAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}
	dead := &RudpConn{
		config:        rudpCfg,
		listenersonny: &rudpConnListenerSonny{listener: lc.listener, dstaddr: deadAddr},
	}
	dead.isclose.Store(true)
	lc.listener.sonny.Store(deadAddr.String(), dead)
	lc.listener.accept.WriteTimeout(dead, 100)
	ghostAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2}
	ghost := &RudpConn{
		config:        rudpCfg,
		listenersonny: &rudpConnListenerSonny{listener: lc.listener, dstaddr: ghostAddr},
	}
	lc.listener.accept.WriteTimeout(ghost, 100)

	got := make(chan Conn, 1)
	go func() {
		s, aerr := lc.Accept()
		if aerr == nil {
			got <- s
		}
	}()
	time.Sleep(200 * time.Millisecond)
	cli, err := (&RudpConn{}).Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	select {
	case s := <-got:
		if s == dead || s == ghost {
			t.Fatal("accepted stale sonny")
		}
		_ = s.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("live sonny not accepted")
	}

	// Backlog: capacity-1 channel, no Accept consumer; the second completed
	// handshake fails the 100ms push and self-closes server-side.
	bcfg := DefaultRudpConfig()
	bcfg.AcceptChanLen = 1
	bfac := &RudpConn{}
	bfac.SetConfig(bcfg)
	bln, err := bfac.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer bln.Close()
	baddr := bln.(*RudpConn).listener.listenerconn.LocalAddr().String()
	c1, err := (&RudpConn{}).Dial(baddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	time.Sleep(200 * time.Millisecond)
	c2, err := (&RudpConn{}).Dial(baddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	// Give the accept goroutine time to hit push timeout and self-close c2.
	time.Sleep(500 * time.Millisecond)
}

// Natural (non-Close) loop exits via the peer CLOSE frame. A CLOSE frame is
// injected whitebox into the server FrameMgr (in-order id after handshake);
// the server exits the main loop on IsRemoteClosed and runs its close stage
// via WriteToUDP, and the outgoing CLOSE then drives the dialer down the same
// path via conn.Write. A buffered-but-unread byte exercises the end-wait
// timeout in the third pair.
func TestHardenRudpRemoteCloseFlows(t *testing.T) {
	cfg := DefaultRudpConfig()
	cfg.CloseTimeoutMs = 300
	cfg.CloseWaitTimeoutMs = 60

	newPair := func(t *testing.T) (*RudpConn, *RudpConn) {
		f := &RudpConn{}
		f.SetConfig(cfg)
		ln, err := f.Listen("127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		accepted := make(chan Conn, 1)
		go func() {
			s, aerr := ln.Accept()
			if aerr == nil {
				accepted <- s
			}
		}()
		d := &RudpConn{}
		d.SetConfig(cfg)
		cli, err := d.Dial(ln.(*RudpConn).listener.listenerconn.LocalAddr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cli.Close() })
		srv := <-accepted
		t.Cleanup(func() { _ = srv.Close() })
		return cli.(*RudpConn), srv.(*RudpConn)
	}
	waitWg := func(ok func() bool, what string, d time.Duration) {
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			if ok() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s did not exit on remote close", what)
	}
	injectClose := func(fm *FrameMgr, id int32) {
		fm.OnRecvFrame(&Frame{
			Type: int32(Frame_DATA),
			Id:   id,
			Data: &FrameData{Type: int32(FrameData_CLOSE)},
		})
	}

	// Server observes injected CLOSE; dialer observes the server CLOSE.
	cli, srv := newPair(t)
	injectClose(srv.listenersonny.fm, 1)
	waitWg(func() bool { return srv.listenersonny.wg.IsExit() }, "server sonny loop", 5*time.Second)
	waitWg(func() bool { return cli.dialer.wg.IsExit() }, "dialer loop", 5*time.Second)

	// Buffered recv data makes the server end-wait sleep until its timeout.
	cli2, srv2 := newPair(t)
	if _, err := cli2.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	waitWg(func() bool { return srv2.listenersonny.fm.GetRecvBufferSize() > 0 },
		"data arrival", 5*time.Second)
	injectClose(srv2.listenersonny.fm, 2)
	start := time.Now()
	waitWg(func() bool { return srv2.listenersonny.wg.IsExit() },
		"buffered server sonny loop", 5*time.Second)
	if d := time.Since(start); d < 50*time.Millisecond {
		t.Fatalf("end-wait returned after %v, want >= CloseWaitTimeout", d)
	}
}

// A peer that goes silent (its UDP socket is closed without the rudp CLOSE
// handshake) makes the live side exit naturally on the heart-beat timeout,
// then run its close stage. This takes ~10s (hardcoded hbTimeoutSecond).
func TestHardenRudpHBTimeoutNaturalExit(t *testing.T) {
	cfg := DefaultRudpConfig()
	cfg.CloseTimeoutMs = 100
	f := &RudpConn{}
	f.SetConfig(cfg)
	ln, err := f.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan Conn, 1)
	go func() {
		s, aerr := ln.Accept()
		if aerr == nil {
			accepted <- s
		}
	}()
	d := &RudpConn{}
	d.SetConfig(cfg)
	cli, err := d.Dial(ln.(*RudpConn).listener.listenerconn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	srv := <-accepted
	rsrv := srv.(*RudpConn)
	// Abort the client socket directly: no CLOSE frame is sent; the server
	// only sees silence and must time the heart beat out on its own.
	if err := cli.(*RudpConn).dialer.conn.Close(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	deadline := start.Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if rsrv.listenersonny.wg.IsExit() {
			if elapsed := time.Since(start); elapsed < 9500*time.Millisecond {
				t.Fatalf("hb timeout exit after %v, want ~10s", elapsed)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server sonny did not exit on heart-beat timeout")
}

// A garbled datagram arriving on a connected dialer socket is dropped by
// the recv goroutine's Unmarshal error branch.
func TestHardenRudpDialerGarbagePacket(t *testing.T) {
	f := &RudpConn{}
	ln, err := f.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan Conn, 1)
	go func() {
		s, aerr := ln.Accept()
		if aerr == nil {
			got <- s
		}
	}()
	cli, err := (&RudpConn{}).Dial(ln.(*RudpConn).listener.listenerconn.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	srv := <-got
	defer srv.Close()

	sender := hardenListenUDP(t)
	defer sender.Close()
	dst, _ := net.ResolveUDPAddr("udp", cli.(*RudpConn).dialer.conn.LocalAddr().String())
	for i := 0; i < 3; i++ {
		if _, err := sender.WriteToUDP([]byte("garbled-dialer-packet"), dst); err != nil {
			t.Fatal(err)
		}
	}
	// Let the recv goroutine service the packets.
	time.Sleep(250 * time.Millisecond)
}

// Race hammer for the listener-exit recheck right after the accept handshake
// completes: a CONN datagram arrives, then the listener is stopped on a
// microsecond sweep so shutdown occasionally lands between the connected
// break and the wg recheck. All outcomes are legal; coverage is observed via
// the cover profile.
func TestHardenRudpAcceptExitRaceHammer(t *testing.T) {
	sender := hardenListenUDP(t)
	defer sender.Close()
	cfg := DefaultRudpConfig()
	cfg.ConnectTimeoutMs = 10000
	fm := NewFrameMgr(cfg.CutSize, cfg.MaxId, cfg.BufferSize, cfg.MaxWin,
		cfg.ResendTimems, cfg.Compress, cfg.Stat)
	mb, err := fm.MarshalFrame(&Frame{
		Type: int32(Frame_DATA),
		Id:   0,
		Data: &FrameData{Type: int32(FrameData_CONN)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 60; attempt++ {
		ff := &RudpConn{}
		ff.SetConfig(cfg)
		l, lerr := ff.Listen("127.0.0.1:0")
		if lerr != nil {
			t.Fatal(lerr)
		}
		lc := l.(*RudpConn)
		dst, _ := net.ResolveUDPAddr("udp", lc.listener.listenerconn.LocalAddr().String())
		if _, werr := sender.WriteToUDP(mb, dst); werr != nil {
			t.Fatal(werr)
		}
		time.Sleep(time.Duration(attempt%40) * 50 * time.Microsecond)
		_ = lc.Close()
	}
}

// ---------- ricmp harden ----------

// Pure state/error branches that need no raw socket: name/info, empty and
// listener Read/Write rejects, closed rejects, lazy config, bad dial/listen
// addresses and double Close.
func TestHardenRicmpEmptyAndErrors(t *testing.T) {
	c := &RicmpConn{}
	if c.Name() != "ricmp" {
		t.Fatalf("Name=%q", c.Name())
	}
	if c.Info() != "empty ricmp conn" {
		t.Fatalf("Info=%q", c.Info())
	}
	if _, err := c.Read(nil); err == nil || !strings.Contains(err.Error(), "read empty buffer") {
		t.Fatalf("nil Read err=%v", err)
	}
	if _, err := c.Write(nil); err == nil || !strings.Contains(err.Error(), "write empty data") {
		t.Fatalf("nil Write err=%v", err)
	}
	if _, err := c.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "empty conn") {
		t.Fatalf("empty Read err=%v", err)
	}
	if _, err := c.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "empty conn") {
		t.Fatalf("empty Write err=%v", err)
	}
	lsh := &RicmpConn{listener: &ricmpConnListener{}}
	if _, err := lsh.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "listener can not be read") {
		t.Fatalf("listener Read err=%v", err)
	}
	if _, err := lsh.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "listener can not be write") {
		t.Fatalf("listener Write err=%v", err)
	}
	// Listener Info with no open sockets must not dereference anything.
	if s := lsh.Info(); !strings.Contains(s, "ricmp listener") {
		t.Fatalf("listener Info=%q", s)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "read closed conn") {
		t.Fatalf("closed Read err=%v", err)
	}
	if _, err := c.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "write closed conn") {
		t.Fatalf("closed Write err=%v", err)
	}
	if cfg := c.GetConfig(); cfg == nil || cfg.MaxPacketSize == 0 {
		t.Fatal("lazy GetConfig failed")
	}
	c.SetConfig(DefaultRicmpConfig())
	if _, err := (&RicmpConn{}).Dial("127.0.0.1:70000"); err == nil {
		t.Fatal("dial invalid address must fail")
	}
	if _, err := (&RicmpConn{}).Listen("host:1234"); err == nil {
		t.Fatal("listen with host:port must fail")
	}
}

// Locked recheck in Close: contenders pass the first isclose check and block
// on closelock, so the second in-lock check is reached when they wake.
func TestHardenRicmpCloseLockedRecheck(t *testing.T) {
	cfg := DefaultRicmpConfig()
	rc := &RicmpConn{id: "recheck", config: cfg,
		listenersonny: &ricmpConnListenerSonny{listener: &ricmpConnListener{}}}
	rc.closelock.Lock()
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = rc.Close()
		}()
	}
	close(start)
	time.Sleep(150 * time.Millisecond)
	rc.closelock.Unlock()
	wg.Wait()
}

// Whitebox FrameMgr Read/Write/Close behavior against in-memory FrameMgr
// instances, mirroring the rudp lifecycle test without any ICMP socket.
func TestHardenRicmpBufferLifecycle(t *testing.T) {
	cfg := DefaultRicmpConfig()
	cfg.BufferSize = 256
	newFM := func() *FrameMgr {
		return NewFrameMgr(cfg.CutSize, cfg.MaxId, cfg.BufferSize, cfg.MaxWin,
			cfg.ResendTimems, cfg.Compress, cfg.Stat)
	}
	stoppedGroup := func() *thread.Group {
		wg := thread.NewGroup("harden ricmp stopped", nil, nil)
		wg.Stop()
		return wg
	}

	u := &RicmpConn{config: cfg, dialer: &ricmpConnDialer{fm: newFM(), wg: stoppedGroup()}}
	if _, err := u.Read(make([]byte, 4)); err == nil || !strings.Contains(err.Error(), "closed conn") {
		t.Fatalf("empty read exited wg err=%v", err)
	}
	n, err := u.Write(make([]byte, 1024))
	if n != 256 || err == nil {
		t.Fatalf("partial blocked write n=%d err=%v", n, err)
	}
	n, err = u.Write(make([]byte, 8))
	if n != 0 || err == nil {
		t.Fatalf("full buffer write n=%d err=%v", n, err)
	}

	// Buffered data is copied out through Read, then the stopped group
	// surfaces as "closed conn".
	fm := newFM()
	fm.OnRecvFrame(&Frame{
		Type: int32(Frame_DATA),
		Id:   0,
		Data: &FrameData{Type: int32(FrameData_USER_DATA), Data: []byte("ping")},
	})
	fm.Update()
	rd := &RicmpConn{config: cfg, dialer: &ricmpConnDialer{fm: fm, wg: stoppedGroup()}}
	buf := make([]byte, 4)
	if rn, rerr := rd.Read(buf); rerr != nil || string(buf[:rn]) != "ping" {
		t.Fatalf("buffered read n=%d err=%v", rn, rerr)
	}
	if _, rerr := rd.Read(buf); rerr == nil {
		t.Fatal("drained read must report closed wg")
	}

	blockedClose := func(prefill int, p []byte) int {
		wg := thread.NewGroup("harden ricmp live", nil, nil)
		live := &RicmpConn{config: cfg, dialer: &ricmpConnDialer{fm: newFM(), wg: wg}}
		if prefill > 0 {
			live.dialer.fm.WriteSendBuffer(make([]byte, prefill))
		}
		ch := make(chan int, 1)
		go func() { wN, _ := live.Write(p); ch <- wN }()
		time.Sleep(250 * time.Millisecond)
		if err := live.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		select {
		case wn := <-ch:
			return wn
		case <-time.After(3 * time.Second):
			t.Fatal("blocked write did not unblock")
			return -1
		}
	}
	if n := blockedClose(0, make([]byte, 1024)); n != 256 {
		t.Fatalf("partial close write n=%d", n)
	}
	if n := blockedClose(256, make([]byte, 16)); n != 0 {
		t.Fatalf("zero close write n=%d", n)
	}

	// listenersonny-side Read/Write use the same FrameMgr plumbing.
	sfm := newFM()
	sfm.OnRecvFrame(&Frame{
		Type: int32(Frame_DATA),
		Id:   0,
		Data: &FrameData{Type: int32(FrameData_USER_DATA), Data: []byte("pong")},
	})
	sfm.Update()
	sonny := &RicmpConn{id: "sonny", config: cfg,
		listenersonny: &ricmpConnListenerSonny{fm: sfm, wg: stoppedGroup()}}
	sbuf := make([]byte, 4)
	if rn, rerr := sonny.Read(sbuf); rerr != nil || string(sbuf[:rn]) != "pong" {
		t.Fatalf("sonny buffered read n=%d err=%v", rn, rerr)
	}
	if wn, werr := sonny.Write([]byte("ab")); werr != nil || wn != 2 {
		t.Fatalf("sonny small write n=%d err=%v", wn, werr)
	}

	// A live listenersonny Read sleeps on an empty recv buffer until Close.
	efm := newFM()
	ewg := thread.NewGroup("harden ricmp sonny live", nil, nil)
	emptySonny := &RicmpConn{id: "esonny", config: cfg,
		listenersonny: &ricmpConnListenerSonny{fm: efm, wg: ewg, listener: &ricmpConnListener{}}}
	errc := make(chan error, 1)
	go func() { _, e := emptySonny.Read(make([]byte, 4)); errc <- e }()
	time.Sleep(250 * time.Millisecond)
	if err := emptySonny.Close(); err != nil {
		t.Fatalf("sonny close: %v", err)
	}
	select {
	case e := <-errc:
		if e == nil || !strings.Contains(e.Error(), "read closed conn") {
			t.Fatalf("sonny read after close err=%v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sonny blocked read did not unblock")
	}

	// Listener-side Close closes the accept channel, all tracked sockets
	// (none here) and every tracked sonny, then stops the group.
	lwg := thread.NewGroup("harden ricmp listener", nil, nil)
	lc := &RicmpConn{id: "lc", config: cfg, listener: &ricmpConnListener{
		accept: common.NewChannel(2), wg: lwg,
	}}
	s1 := &RicmpConn{id: "s1", config: cfg, listenersonny: &ricmpConnListenerSonny{listener: lc.listener}}
	lc.listener.sonny.Store("s1", s1)
	if err := lc.Close(); err != nil {
		t.Fatalf("listener close: %v", err)
	}
	if _, aerr := lc.Accept(); aerr == nil {
		t.Fatal("Accept after listener close must fail")
	}
}

// Accept pops closed/missing entries and finally surfaces the listener close.
func TestHardenRicmpAcceptSkipsStale(t *testing.T) {
	cfg := DefaultRicmpConfig()
	wg := thread.NewGroup("harden ricmp accept", nil, nil)
	lc := &RicmpConn{id: "lc", config: cfg, listener: &ricmpConnListener{
		accept: common.NewChannel(4), wg: wg,
	}}
	closed := &RicmpConn{id: "s1", config: cfg,
		listenersonny: &ricmpConnListenerSonny{listener: lc.listener}}
	closed.isclose.Store(true)
	lc.listener.sonny.Store("s1", closed)
	lc.listener.accept.Write(closed)
	lc.listener.accept.Write(&RicmpConn{id: "ghost", config: cfg,
		listenersonny: &ricmpConnListenerSonny{listener: lc.listener}})
	live := &RicmpConn{id: "s3", config: cfg,
		listenersonny: &ricmpConnListenerSonny{listener: lc.listener}}
	lc.listener.sonny.Store("s3", live)
	lc.listener.accept.Write(live)

	got := make(chan Conn, 1)
	go func() {
		s, err := lc.Accept()
		if err == nil {
			got <- s
		}
	}()
	select {
	case s := <-got:
		if s != live {
			t.Fatal("accepted stale sonny")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("live sonny not accepted")
	}
	if err := lc.Close(); err != nil {
		t.Fatal(err)
	}
}

// Unprivileged paths only: dgram ping socket (Darwin/Linux when permitted),
// source-IP probe, packet-conn family detection and Info formatting from a
// real socket. All failures to open the dgram socket are environment skips.
func TestHardenRicmpDgramSocketsAndHelpers(t *testing.T) {
	pc, mode, err := dialRicmpSocket(icmpFamilyV4, true)
	if err != nil {
		t.Skipf("unprivileged icmp dgram socket unavailable: %v", err)
	}
	defer pc.Close()
	if mode != icmpModeDgram {
		t.Fatalf("mode=%q want dgram", mode)
	}
	if fam := familyFromPacketConn(pc); fam != icmpFamilyV4 {
		t.Fatalf("packet conn family=%d want v4", fam)
	}
	if id := datagramEchoID(pc); id < 0 {
		t.Fatalf("datagram echo id %d", id)
	}
	addr := &net.IPAddr{IP: net.ParseIP("127.0.0.1")}
	d := &RicmpConn{id: "dialer-info", config: DefaultRicmpConfig(),
		dialer: &ricmpConnDialer{conn: pc, serveraddr: addr}}
	if s := d.Info(); !strings.Contains(s, "ricmp dialer") {
		t.Fatalf("dialer Info=%q", s)
	}
	s := &RicmpConn{id: "sonny-info", config: DefaultRicmpConfig(),
		listenersonny: &ricmpConnListenerSonny{fatherconn: pc, dstaddr: addr}}
	if si := s.Info(); !strings.Contains(si, "listenersonny") {
		t.Fatalf("sonny Info=%q", si)
	}

	// Source-IP probe uses an unconnected UDP socket: loopback is always
	// routable and the result is cached.
	probe := &RicmpConn{}
	if ip := probe.sourceIPTowards(net.ParseIP("127.0.0.1"), ""); ip == nil {
		t.Fatal("source IP toward loopback is nil")
	}
	if ip := probe.sourceIPTowards(net.ParseIP("127.0.0.1"), ""); ip == nil {
		t.Fatal("cached source IP is nil")
	}

	// IPv6 dgram is unsupported on Darwin; elsewhere it may or may not
	// exist. Only assert that both code paths (error string / success)
	// return consistently.
	if pc6, m6, err6 := dialRicmpSocket(icmpFamilyV6, true); err6 == nil {
		defer pc6.Close()
		if m6 != icmpModeDgram {
			t.Fatalf("v6 mode=%q", m6)
		}
		if fam := familyFromPacketConn(pc6); fam != icmpFamilyV6 {
			t.Fatalf("v6 packet conn family=%d want v6", fam)
		}
	}
}

// Two unprivileged dgram ping sockets on loopback: a malformed ICMP echo
// (valid echo header, garbage protobuf body) reaches recv_icmp and must take
// the decode-error return. If the platform does not deliver dgram-to-dgram
// echoes locally, the test skips.
func TestHardenRicmpDgramRecvDecodeError(t *testing.T) {
	a, _, err := dialRicmpSocket(icmpFamilyV4, true)
	if err != nil {
		t.Skipf("unprivileged icmp dgram socket unavailable: %v", err)
	}
	defer a.Close()
	b, _, err := dialRicmpSocket(icmpFamilyV4, true)
	if err != nil {
		t.Skipf("second dgram socket unavailable: %v", err)
	}
	defer b.Close()
	pb := b.LocalAddr().(*net.UDPAddr)
	msg, merr := (&icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   pb.Port,
			Seq:  1,
			Data: []byte("this-is-not-an-icmpmsg-protobuf!!"),
		},
	}).Marshal(nil)
	if merr != nil {
		t.Fatal(merr)
	}
	if _, werr := a.WriteTo(msg, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: pb.Port}); werr != nil {
		t.Skipf("dgram write failed: %v", werr)
	}
	buf := make([]byte, 2048)
	b.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
	n, _, rerr, _, _, _, _ := (&RicmpConn{}).recv_icmp(b, buf)
	if rerr != nil {
		return // expected decode error
	}
	if n == 0 {
		t.Skip("platform did not deliver dgram-to-dgram echo")
	}
	t.Fatalf("malformed payload decoded without error, n=%d", n)
}

// A forced dgram dial against loopback with no raw listener cannot complete
// the FrameMgr handshake (the kernel may answer the echo itself, but that
// reply carries the client flag, not the server flag), so the connect loop
// must hit its timeout; closing the factory mid-handshake returns the closed
// error. Everything stays on loopback.
func TestHardenRicmpDgramDialFailures(t *testing.T) {
	if _, _, err := dialRicmpSocket(icmpFamilyV4, true); err != nil {
		t.Skipf("unprivileged icmp dgram socket unavailable: %v", err)
	}
	cfg := DefaultRicmpConfig()
	cfg.ConnectTimeoutMs = 200
	c1 := &RicmpConn{}
	c1.SetConfig(cfg)
	start := time.Now()
	if _, err := c1.dial("127.0.0.1", true); err == nil {
		t.Fatal("dgram dial with no server must fail")
	} else if time.Since(start) > 5*time.Second {
		t.Fatalf("connect timeout took too long: %v", err)
	}

	c2 := &RicmpConn{}
	c2.SetConfig(cfg)
	errc := make(chan error, 1)
	go func() {
		_, err := c2.dial("127.0.0.1", true)
		errc <- err
	}()
	time.Sleep(80 * time.Millisecond)
	if err := c2.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "closed conn") {
			t.Fatalf("mid-handshake close err=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mid-handshake dial did not return")
	}
}

func TestHardenFrameMgrInBandHeartbeatBackwardsCompatibility(t *testing.T) {
	fm := NewFrameMgr(64, 100, 4096, 16, 50, 0, 1)

	// Step 1: Remote connects
	fm.OnRecvFrame(&Frame{
		Type: int32(Frame_DATA),
		Id:   0,
		Data: &FrameData{Type: int32(FrameData_CONN)},
	})
	fm.Update()
	if !fm.IsConnected() {
		t.Fatal("expected connected after FrameData_CONN")
	}
	if fm.recvid != 1 {
		t.Fatalf("expected recvid=1, got %d", fm.recvid)
	}

	// Step 2: Remote peer is an older version and sends an in-band heartbeat
	// with a stream sequence ID (Id: 1)
	hbFrame := &Frame{
		Type: int32(Frame_DATA),
		Id:   1,
		Data: &FrameData{Type: int32(FrameData_HB)},
	}
	fm.OnRecvFrame(hbFrame)
	fm.Update()

	if fm.recvid != 2 {
		t.Fatalf("in-band heartbeat must advance recvid to 2 to avoid stream hole, got %d", fm.recvid)
	}
	if fm.GetLastRecvHBTime() == 0 {
		t.Fatal("in-band heartbeat must record lastRecvHBTime")
	}

	// Step 3: Remote sends user data at Id: 2
	dataPayload := []byte("stream-data-after-hb")
	fm.OnRecvFrame(recvDataFrame(2, dataPayload))
	fm.Update()

	if fm.recvid != 3 {
		t.Fatalf("stream data after in-band heartbeat must advance recvid to 3, got %d", fm.recvid)
	}

	// Verify data is deliverable to application
	if fm.GetRecvBufferSize() != len(dataPayload) {
		t.Fatalf("recv buffer size = %d, want %d", fm.GetRecvBufferSize(), len(dataPayload))
	}
	buf := fm.GetRecvReadLineBuffer()
	if string(buf) != string(dataPayload) {
		t.Fatalf("read %s, want %s", string(buf), string(dataPayload))
	}
}
