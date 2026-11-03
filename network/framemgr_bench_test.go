package network

import (
	"testing"
)

// Microbenchmarks for FrameMgr hot paths (cut → stage → GetSendFrames) and
// BBCongestion. Compare with benchstat:
//
//	go test ./network/ -run='^$' -bench='BenchmarkFrameMgr|BenchmarkBBCongestion' \
//	  -benchmem -count=6 -timeout 10m | tee old.txt
//	# after a change:
//	go test ./network/ -run='^$' -bench='BenchmarkFrameMgr|BenchmarkBBCongestion' \
//	  -benchmem -count=6 -timeout 10m | tee new.txt
//	benchstat old.txt new.txt

func BenchmarkFrameMgrWriteUpdateDrain(b *testing.B) {
	const chunk = 1024
	payload := make([]byte, chunk)
	for i := range payload {
		payload[i] = byte(i)
	}

	b.ReportAllocs()
	b.SetBytes(chunk)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Fresh mgr each iter keeps the send window from filling forever
		// without needing a peer ACK path in the hot loop.
		b.StopTimer()
		fm := NewFrameMgr(chunk, 100000, 4*1024*1024, 1024, 50, 0, 0)
		b.StartTimer()

		fm.WriteSendBuffer(payload)
		fm.Update()
		frames := fm.GetSendFrames()
		PutFrames(frames)
	}
}

func BenchmarkFrameMgrMarshalFrame(b *testing.B) {
	fm := NewFrameMgr(128, 1000, 64*1024, 64, 50, 0, 0)
	f := &Frame{
		Type: int32(Frame_DATA),
		Id:   1,
		Data: &FrameData{
			Type: int32(FrameData_USER_DATA),
			Data: make([]byte, 128),
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fm.MarshalFrame(f); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBBCongestionCanSendUpdate(b *testing.B) {
	bb := &BBCongestion{}
	bb.Init()
	const pkt = 1024
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !bb.CanSend(i, pkt) {
			bb.RecvAck(i, pkt)
			bb.Update()
			if !bb.CanSend(i, pkt) {
				b.Fatal("CanSend false after Update")
			}
		}
		if i%8 == 0 {
			bb.RecvAck(i, pkt)
			bb.Update()
		}
	}
}
