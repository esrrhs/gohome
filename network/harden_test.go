package network

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/esrrhs/gohome/common"
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
	var ctrl func(network, address string, c syscall.RawConn) error
	ctrl = func(network, address string, c syscall.RawConn) error { return nil }
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
	cmd, raw, host, err = Sock5GetRequest(bytes.NewBuffer(wire))
	if err != nil || host != "foo:80" || string(raw[2:5]) != "foo" {
		t.Fatalf("domain request host=%q raw=%v err=%v", host, raw, err)
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
