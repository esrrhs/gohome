package network

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
)

func TestFrameMgrRTTInitUnit(t *testing.T) {
	fm := NewFrameMgr(100, 1000, 64*1024, 64, 200, 0, 0)
	want := int64(200) * int64(time.Millisecond)
	if fm.rttns != want {
		t.Fatalf("rttns init = %d, want %d (resend_timems as duration nanoseconds)", fm.rttns, want)
	}
}

func TestFrameMgrSendIdRankWrap(t *testing.T) {
	fm := NewFrameMgr(100, 100, 64*1024, 32, 50, 0, 0)
	if g, w := fm.sendIdRank(90, 5), int32(15); g != w {
		t.Fatalf("sendIdRank(90,5)=%d want %d", g, w)
	}
	if g, w := fm.sendIdRank(90, 95), int32(5); g != w {
		t.Fatalf("sendIdRank(90,95)=%d want %d", g, w)
	}
	if fm.sendIdRank(10, 10) != 0 {
		t.Fatal("sendIdRank same id should be 0")
	}
}

func TestFrameMgrReqMapUsesHoleId(t *testing.T) {
	// Tiny recv buffer so frame 0 cannot be drained; FrontInter stays valid and
	// holes 1,2 before frame 3 should all be REQ'd in one pass.
	fm := NewFrameMgr(64, 1000, 16, 64, 200, 0, 0)
	fm.rttns = int64(time.Second)

	f0 := &Frame{
		Type: int32(Frame_DATA),
		Id:   0,
		Data: &FrameData{Type: int32(FrameData_USER_DATA), Data: make([]byte, 64)},
	}
	f3 := &Frame{
		Type: int32(Frame_DATA),
		Id:   3,
		Data: &FrameData{Type: int32(FrameData_USER_DATA), Data: []byte("x")},
	}
	if err := fm.recvwin.Set(0, f0); err != nil {
		t.Fatal(err)
	}
	if err := fm.recvwin.Set(3, f3); err != nil {
		t.Fatal(err)
	}

	fm.combineWindowToRecvBuffer(time.Now().UnixNano())
	sl := fm.GetSendList()
	ids := map[int32]struct{}{}
	for e := sl.Front(); e != nil; e = e.Next() {
		fr := e.Value.(*Frame)
		if fr.Type == int32(Frame_REQ) {
			for _, id := range fr.Dataid {
				ids[id] = struct{}{}
			}
		}
	}
	for _, want := range []int32{1, 2} {
		if _, ok := ids[want]; !ok {
			t.Fatalf("missing REQ for hole %d, got %v", want, ids)
		}
	}
}

func TestFrameMgrReliableNoLoss(t *testing.T) {
	frameMgrReliableTransfer(t, frameMgrSimConfig{
		cutSize:     200,
		maxId:       10000,
		bufferSize:  1024 * 1024,
		maxWin:      256,
		resendMs:    20,
		lossProb:    0,
		totalBytes:  200 * 1024,
		useBB:       true,
		maxDuration: 30 * time.Second,
		writeChunk:  4096,
		seed:        1,
	})
}

func TestFrameMgrReliableRandomLoss(t *testing.T) {
	frameMgrReliableTransfer(t, frameMgrSimConfig{
		cutSize:     200,
		maxId:       10000,
		bufferSize:  1024 * 1024,
		maxWin:      256,
		resendMs:    20,
		lossProb:    0.05,
		totalBytes:  200 * 1024,
		useBB:       true,
		maxDuration: 60 * time.Second,
		writeChunk:  4096,
		seed:        42,
	})
}

func TestFrameMgrReliableHighLossWrap(t *testing.T) {
	// Tiny maxId forces sendid wrap; bb exercises wrap-aware ctLastSendId.
	frameMgrReliableTransfer(t, frameMgrSimConfig{
		cutSize:     64,
		maxId:       128,
		bufferSize:  512 * 1024,
		maxWin:      32,
		resendMs:    10,
		lossProb:    0.10,
		totalBytes:  256 * 1024,
		useBB:       true,
		maxDuration: 120 * time.Second,
		writeChunk:  1024,
		seed:        7,
	})
}

func TestFrameMgrReliableLongStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skip long stress in -short")
	}
	frameMgrReliableTransfer(t, frameMgrSimConfig{
		cutSize:     400,
		maxId:       1000,
		bufferSize:  2 * 1024 * 1024,
		maxWin:      128,
		resendMs:    15,
		lossProb:    0.08,
		totalBytes:  2 * 1024 * 1024,
		useBB:       true,
		maxDuration: 3 * time.Minute,
		writeChunk:  8192,
		seed:        99,
	})
}

func TestFrameMgrReliableMultiSeedLoss(t *testing.T) {
	losses := []float64{0.01, 0.05, 0.15}
	seeds := []int64{1, 2, 3, 5, 8, 13, 21, 34}
	for _, loss := range losses {
		for _, seed := range seeds {
			loss, seed := loss, seed
			name := fmt.Sprintf("loss%.2f_seed%d", loss, seed)
			t.Run(name, func(t *testing.T) {
				frameMgrReliableTransfer(t, frameMgrSimConfig{
					cutSize:     128,
					maxId:       256,
					bufferSize:  512 * 1024,
					maxWin:      64,
					resendMs:    10,
					lossProb:    loss,
					totalBytes:  128 * 1024,
					useBB:       true,
					maxDuration: 90 * time.Second,
					writeChunk:  2048,
					seed:        seed,
				})
			})
		}
	}
}

func TestFrameMgrReliableBidirectional(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	a := NewFrameMgr(128, 512, 1024*1024, 64, 15, 0, 0)
	b := NewFrameMgr(128, 512, 1024*1024, 64, 15, 0, 0)
	a.SetCongestion(&BBCongestion{})
	b.SetCongestion(&BBCongestion{})
	if err := frameMgrConnect(a, b, 0.05, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	const n = 64 * 1024
	srcA := make([]byte, n)
	srcB := make([]byte, n)
	for i := 0; i < n; i++ {
		srcA[i] = byte(rng.Intn(256))
		srcB[i] = byte(rng.Intn(256))
	}
	var gotA, gotB bytes.Buffer
	offA, offB := 0, 0
	deadline := time.Now().Add(60 * time.Second)
	loss := 0.05

	for gotA.Len() < n || gotB.Len() < n {
		if time.Now().After(deadline) {
			t.Fatalf("bidi timeout gotA=%d gotB=%d", gotA.Len(), gotB.Len())
		}
		for offA < n {
			left := a.GetSendBufferLeft()
			if left <= 0 {
				break
			}
			m := n - offA
			if m > left {
				m = left
			}
			if m > 2048 {
				m = 2048
			}
			a.WriteSendBuffer(srcA[offA : offA+m])
			offA += m
		}
		for offB < n {
			left := b.GetSendBufferLeft()
			if left <= 0 {
				break
			}
			m := n - offB
			if m > left {
				m = left
			}
			if m > 2048 {
				m = 2048
			}
			b.WriteSendBuffer(srcB[offB : offB+m])
			offB += m
		}
		a.Update()
		b.Update()
		frameMgrExchange(a, b, rng, loss)
		frameMgrExchange(b, a, rng, loss)
		for a.GetRecvBufferSize() > 0 && gotA.Len() < n {
			buf := a.GetRecvReadLineBuffer()
			if len(buf) == 0 {
				break
			}
			m := len(buf)
			if m > n-gotA.Len() {
				m = n - gotA.Len()
			}
			gotA.Write(buf[:m])
			a.SkipRecvBuffer(m)
		}
		for b.GetRecvBufferSize() > 0 && gotB.Len() < n {
			buf := b.GetRecvReadLineBuffer()
			if len(buf) == 0 {
				break
			}
			m := len(buf)
			if m > n-gotB.Len() {
				m = n - gotB.Len()
			}
			gotB.Write(buf[:m])
			b.SkipRecvBuffer(m)
		}
		if offA >= n && offB >= n {
			time.Sleep(time.Millisecond)
		}
	}
	if !bytes.Equal(gotA.Bytes(), srcB) {
		t.Fatal("A did not receive B payload")
	}
	if !bytes.Equal(gotB.Bytes(), srcA) {
		t.Fatal("B did not receive A payload")
	}
}

type frameMgrSimConfig struct {
	cutSize     int
	maxId       int
	bufferSize  int
	maxWin      int
	resendMs    int
	lossProb    float64
	totalBytes  int
	useBB       bool
	maxDuration time.Duration
	writeChunk  int
	seed        int64
}

func frameMgrReliableTransfer(t *testing.T, cfg frameMgrSimConfig) {
	t.Helper()
	rng := rand.New(rand.NewSource(cfg.seed))

	a := NewFrameMgr(cfg.cutSize, cfg.maxId, cfg.bufferSize, cfg.maxWin, cfg.resendMs, 0, 0)
	b := NewFrameMgr(cfg.cutSize, cfg.maxId, cfg.bufferSize, cfg.maxWin, cfg.resendMs, 0, 0)
	if cfg.useBB {
		a.SetCongestion(&BBCongestion{})
		b.SetCongestion(&BBCongestion{})
	}
	a.SetDebugid("a")
	b.SetDebugid("b")

	if err := frameMgrConnect(a, b, cfg.lossProb, 5*time.Second); err != nil {
		t.Fatalf("connect: %v", err)
	}

	src := make([]byte, cfg.totalBytes)
	for i := range src {
		src[i] = byte(rng.Intn(256))
	}

	var got bytes.Buffer
	got.Grow(cfg.totalBytes)
	writeOff := 0
	deadline := time.Now().Add(cfg.maxDuration)
	idleRounds := 0

	for got.Len() < cfg.totalBytes {
		if time.Now().After(deadline) {
			t.Fatalf("timeout after %v: wrote=%d/%d got=%d/%d loss=%.2f maxId=%d",
				cfg.maxDuration, writeOff, cfg.totalBytes, got.Len(), cfg.totalBytes, cfg.lossProb, cfg.maxId)
		}

		progress := false

		for writeOff < cfg.totalBytes {
			left := a.GetSendBufferLeft()
			if left <= 0 {
				break
			}
			n := cfg.writeChunk
			if n > left {
				n = left
			}
			if n > cfg.totalBytes-writeOff {
				n = cfg.totalBytes - writeOff
			}
			a.WriteSendBuffer(src[writeOff : writeOff+n])
			writeOff += n
			progress = true
		}

		a.Update()
		b.Update()

		if frameMgrExchange(a, b, rng, cfg.lossProb) {
			progress = true
		}
		if frameMgrExchange(b, a, rng, cfg.lossProb) {
			progress = true
		}

		for b.GetRecvBufferSize() > 0 {
			buf := b.GetRecvReadLineBuffer()
			if len(buf) == 0 {
				break
			}
			need := cfg.totalBytes - got.Len()
			if need <= 0 {
				break
			}
			n := len(buf)
			if n > need {
				n = need
			}
			got.Write(buf[:n])
			b.SkipRecvBuffer(n)
			progress = true
		}

		if writeOff >= cfg.totalBytes && !a.close {
			a.Close()
		}

		if !progress {
			idleRounds++
			if idleRounds > 100000 {
				t.Fatalf("no progress: wrote=%d got=%d", writeOff, got.Len())
			}
			time.Sleep(time.Millisecond)
		} else {
			idleRounds = 0
		}
	}

	if !bytes.Equal(got.Bytes(), src) {
		gb := got.Bytes()
		n := len(gb)
		if n > len(src) {
			n = len(src)
		}
		idx := 0
		for idx < n && gb[idx] == src[idx] {
			idx++
		}
		t.Fatalf("payload mismatch at %d (gotLen=%d wantLen=%d)", idx, got.Len(), len(src))
	}

	t.Logf("ok bytes=%d loss=%.2f maxId=%d approxDataFrames=%d",
		cfg.totalBytes, cfg.lossProb, cfg.maxId, cfg.totalBytes/cfg.cutSize)
}

func frameMgrConnect(a, b *FrameMgr, loss float64, timeout time.Duration) error {
	rng := rand.New(rand.NewSource(1))
	a.Connect()
	deadline := time.Now().Add(timeout)
	for !a.IsConnected() || !b.IsConnected() {
		if time.Now().After(deadline) {
			return fmt.Errorf("connect timeout a=%v b=%v", a.IsConnected(), b.IsConnected())
		}
		a.Update()
		b.Update()
		frameMgrExchange(a, b, rng, loss)
		frameMgrExchange(b, a, rng, loss)
		time.Sleep(time.Millisecond)
	}
	return nil
}

func frameMgrExchange(from, to *FrameMgr, rng *rand.Rand, loss float64) bool {
	sl := from.GetSendList()
	if sl.Len() == 0 {
		return false
	}
	delivered := false
	for e := sl.Front(); e != nil; e = e.Next() {
		f := e.Value.(*Frame)
		if loss > 0 && rng.Float64() < loss {
			continue
		}
		deliverFrame(to, f)
		delivered = true
	}
	return delivered
}

func deliverFrame(to *FrameMgr, f *Frame) {
	cp, ok := proto.Clone(f).(*Frame)
	if !ok || cp == nil {
		cp = cloneFrame(f)
	}
	to.OnRecvFrame(cp)
}

func cloneFrame(f *Frame) *Frame {
	cp := &Frame{
		Type:     f.Type,
		Resend:   f.Resend,
		Sendtime: f.Sendtime,
		Id:       f.Id,
	}
	if f.Dataid != nil {
		cp.Dataid = append([]int32(nil), f.Dataid...)
	}
	if f.Data != nil {
		cp.Data = &FrameData{
			Type:     f.Data.Type,
			Compress: f.Data.Compress,
		}
		if f.Data.Data != nil {
			cp.Data.Data = append([]byte(nil), f.Data.Data...)
		}
	}
	return cp
}
