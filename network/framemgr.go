package network

import (
	"container/list"
	"github.com/esrrhs/gohome/common"
	glist "github.com/esrrhs/gohome/list"
	"github.com/esrrhs/gohome/loggo"
	"google.golang.org/protobuf/proto"
	"strconv"
	"sync"
	"time"
)

/*
FrameMgr 实现了高效的帧管理系统，用于网络数据传输中的数据帧的发送、接收及管理。

该包定义了 FrameMgr 结构体，其中封装了用于控制网络数据传输的逻辑，包括数据帧的排序、重发机制、心跳检测、以及网络拥塞控制。

算法原理：

FrameMgr 采用了一种基于滑动窗口的机制来管理数据帧，具体流程如下：

1. 初始化状态：算法开始时，设置窗口大小、最大帧 ID 以及发送/接收缓冲区。同时会初始化状态追踪变量，用于监测网络状况。

2. 数据帧发送：通过 `WriteSendBuffer` 方法，将数据帧写入发送缓冲区并检查当前发送队列的状态。在达到小时队列限制前，智能地推送数据至发送窗口。

3. 数据接收处理：
   - 通过 `OnRecvFrame` 方法接收数据帧，并将其推入接收列表。此过程中，算法会根据帧类型（如 ACK、DATA、PING）进行不同的处理。
   - 确保对接收到的每个帧都进行有效性检查，并更新相应的状态统计。

4. 拥塞控制与心跳机制：
   - 通过 `ping` 和 `hb` 方法定期发送心跳检测包，以确保接收方存活，并检测网络延迟。
   - 根据接收到的 ACK 数据调整发送窗口大小，动态适应网络状况，避免网络拥塞。

5. 状态管理：使用状态机模式在不同网络条件下控制数据流的发送与接收，以实现高效的数据传输。

该算法旨在实现高可靠性和高效率的网络数据传输，有效减少延迟和数据丢失，提高整体的网络性能。
*/

type FrameStat struct {
	sendDataNum     int
	recvDataNum     int
	sendReqNum      int
	recvReqNum      int
	sendAckNum      int
	recvAckNum      int
	sendDataNumsMap map[int32]int
	recvDataNumsMap map[int32]int
	sendReqNumsMap  map[int32]int
	recvReqNumsMap  map[int32]int
	sendAckNumsMap  map[int32]int
	recvAckNumsMap  map[int32]int
	sendping        int
	sendpong        int
	recvping        int
	recvpong        int
	recvOldNum      int
	recvOutWinNum   int
}

const (
	hbTimeoutSecond = 10 // 心跳超时时间
)

type FrameMgr struct {
	frame_max_size int
	frame_max_id   int32
	sendb          *glist.RBuffergo
	recvb          *glist.RBuffergo

	sendblock sync.Locker
	recvblock sync.Locker

	recvlock      sync.Locker
	sendlock      sync.Locker
	windowsize    int32
	resend_timems int
	compress      int

	sendwin *glist.ROBuffergo
	sendq   []*Frame // staged outbound frames awaiting GetSendFrames; guarded by sendlock
	// sendspare is the recycled storage handed back by PutSendFrames; the next
	// sendFrame stages into it. Guarded by sendlock.
	sendspare []*Frame
	sendid    int32

	recvwin *glist.ROBuffergo
	recvq   []*Frame // staged inbound frames awaiting Update; guarded by recvlock
	recvid  int32

	close            bool
	remoteclosed     bool
	closesend        bool
	closesendreq     bool
	halfclosesend    bool
	remotesendclosed bool

	lastPingTime int64
	lastPongTime int64
	rttns        int64

	lastSendHBTime   int64
	lastRecvHBTime   int64
	lastRecvDataTime int64

	reqmap map[int32]int64

	connected bool

	fs            *FrameStat
	openstat      int
	lastPrintStat int64

	debugid string

	ct           Congestion
	ctLastSendId int32
}

func (fm *FrameMgr) SetDebugid(debugid string) {
	fm.debugid = debugid
}

func (fm *FrameMgr) SetCongestion(ct Congestion) {
	fm.ct = ct
	fm.ct.Init()
	if rttSetter, ok := fm.ct.(interface{ SetRtt(time.Duration) }); ok && fm.lastPongTime > 0 && fm.rttns > 0 {
		rttSetter.SetRtt(time.Duration(fm.rttns))
	}
}

func NewFrameMgr(frame_max_size int, frame_max_id int, buffersize int, windowsize int, resend_timems int, compress int, openstat int) *FrameMgr {

	// NewROBuffer returns nil when frame_max_id <= 0 or windowsize >=
	// frame_max_id, which would otherwise surface as a nil-pointer panic on
	// the first send/recv operation. Fail fast with a clear message instead.
	if frame_max_id <= 0 || windowsize <= 0 || windowsize >= frame_max_id {
		panic("invalid frame ring params: require windowsize > 0 and 0 < windowsize < frame_max_id")
	}

	sendb := glist.NewRBuffergo(buffersize, false)
	recvb := glist.NewRBuffergo(buffersize, false)

	fm := &FrameMgr{
		frame_max_size: frame_max_size, frame_max_id: int32(frame_max_id),
		sendb: sendb, recvb: recvb,
		sendblock: &sync.Mutex{}, recvblock: &sync.Mutex{},
		recvlock: &sync.Mutex{}, sendlock: &sync.Mutex{},
		windowsize: int32(windowsize), resend_timems: resend_timems, compress: compress,
		sendwin: glist.NewROBuffer(windowsize, 0, frame_max_id), sendid: 0,
		recvwin: glist.NewROBuffer(windowsize, 0, frame_max_id), recvid: 0,
		close: false, remoteclosed: false, closesend: false,
		lastPingTime: time.Now().UnixNano(), lastPongTime: 0,
		lastSendHBTime: time.Now().UnixNano(), lastRecvHBTime: time.Now().UnixNano(), lastRecvDataTime: time.Now().UnixNano(),
		rttns:     int64(resend_timems) * int64(time.Millisecond),
		reqmap:    make(map[int32]int64),
		connected: false, openstat: openstat, lastPrintStat: time.Now().UnixNano(),
		ctLastSendId: -1,
	}

	if openstat > 0 {
		fm.resetStat()
	}
	return fm
}

func (fm *FrameMgr) GetSendBufferLeft() int {
	fm.sendblock.Lock()
	defer fm.sendblock.Unlock()
	left := fm.sendb.Capacity() - fm.sendb.Size()
	return left
}

// GetSendWinSize returns the number of unacknowledged user data frames in flight.
// It must be called from the same goroutine that drives Update: it only takes
// sendblock (the send-buffer lock), while processRecvList mutates sendwin
// without it.
func (fm *FrameMgr) GetSendWinSize() int {
	fm.sendblock.Lock()
	defer fm.sendblock.Unlock()
	count := 0
	// Bounded by the number of filled slots. Next() walks the ring one slot at
	// a time, so the walk must stop before requesting the element after the
	// last filled one — otherwise every call pays an O(windowsize) tail scan.
	// This runs on every conn loop iteration.
	e := fm.sendwin.FrontInter()
	remaining := fm.sendwin.Size()
	for e != nil && remaining > 0 {
		remaining--
		if f, ok := e.Value.(*Frame); ok && f != nil && f.Data != nil {
			if f.Data.Type == int32(FrameData_USER_DATA) && len(f.Data.Data) > 0 {
				count++
			}
		}
		if remaining <= 0 {
			break
		}
		e = e.Next()
	}
	return count
}

// GetLastRecvHBTime returns when the last heartbeat from the peer arrived.
func (fm *FrameMgr) GetLastRecvHBTime() int64 {
	return fm.lastRecvHBTime
}

func (fm *FrameMgr) WriteSendBuffer(data []byte) {
	fm.sendblock.Lock()
	defer fm.sendblock.Unlock()
	fm.sendb.Write(data)
	//loggo.Debug("debugid %v WriteSendBuffer %v %v", fm.debugid, fm.sendb.Size(), len(data))
}

func (fm *FrameMgr) Update() bool {
	cur := time.Now().UnixNano()

	fm.cutSendBufferToWindow(cur)

	rm := fm.preProcessRecvList()
	avtive := len(rm.req) + len(rm.ack) + len(rm.ackto)
	fm.processRecvList(rm)
	recvMapsPool.Put(rm)

	fm.combineWindowToRecvBuffer(cur)

	fm.calSendList(cur)

	fm.ping()
	fm.hb()

	fm.second(cur)

	return avtive > 0
}

func (fm *FrameMgr) cutSendBufferToWindow(cur int64) {

	fm.sendblock.Lock()
	defer fm.sendblock.Unlock()

	sendall := false

	if fm.sendb.Size() < fm.frame_max_size {
		sendall = true
	}

	for fm.sendb.Size() >= fm.frame_max_size && fm.sendwin.Size() < int(fm.windowsize) {
		fd := getSendFrame(fm.frame_max_size).Data
		fm.sendb.Read(fd.Data)

		if fm.compress > 0 && len(fd.Data) > fm.compress {
			newb := common.CompressDataZstd(fd.Data)
			if len(newb) < len(fd.Data) {
				fd.Data = newb
				fd.Compress = true
			}
		}

		f := &Frame{Type: (int32)(Frame_DATA),
			Id:   fm.sendid,
			Data: fd}

		fm.sendid++
		if fm.sendid >= fm.frame_max_id {
			fm.sendid = 0
		}

		err := fm.sendwin.Set(int(f.Id), f)
		if err != nil {
			loggo.Error("sendwin Set fail %v", err)
		}
		//loggo.Debug("debugid %v cut frame push to send win %v %v %v", fm.debugid, f.Id, fm.frame_max_size, fm.sendwin.Size())
	}

	if sendall && fm.sendb.Size() > 0 && fm.sendwin.Size() < int(fm.windowsize) {
		fd := getSendFrame(fm.sendb.Size()).Data
		fm.sendb.Read(fd.Data)

		if fm.compress > 0 && len(fd.Data) > fm.compress {
			newb := common.CompressDataZstd(fd.Data)
			if len(newb) < len(fd.Data) {
				fd.Data = newb
				fd.Compress = true
			}
		}

		f := &Frame{Type: (int32)(Frame_DATA),
			Id:   fm.sendid,
			Data: fd}

		fm.sendid++
		if fm.sendid >= fm.frame_max_id {
			fm.sendid = 0
		}

		err := fm.sendwin.Set(int(f.Id), f)
		if err != nil {
			loggo.Error("sendwin Set fail %v", err)
		}
		//loggo.Debug("debugid %v cut small frame push to send win %v %v %v", fm.debugid, f.Id, len(f.Data.Data), fm.sendwin.Size())
	}

	if fm.sendb.Empty() && fm.close && !fm.closesend && fm.sendwin.Size() < int(fm.windowsize) {
		fd := getSendFrame(0).Data
		fd.Type = (int32)(FrameData_CLOSE)

		f := &Frame{Type: (int32)(Frame_DATA),
			Id:   fm.sendid,
			Data: fd}

		fm.sendid++
		if fm.sendid >= fm.frame_max_id {
			fm.sendid = 0
		}

		err := fm.sendwin.Set(int(f.Id), f)
		if err != nil {
			loggo.Error("sendwin Set fail %v", err)
		}
		fm.closesend = true
		//loggo.Debug("debugid %v close frame push to send win %v %v", fm.debugid, f.Id, fm.sendwin.Size())
	}

	if fm.sendb.Empty() && fm.closesendreq && !fm.halfclosesend && fm.sendwin.Size() < int(fm.windowsize) {
		fd := getSendFrame(0).Data
		fd.Type = (int32)(FrameData_HALFCLOSE)

		f := &Frame{Type: (int32)(Frame_DATA),
			Id:   fm.sendid,
			Data: fd}

		fm.sendid++
		if fm.sendid >= fm.frame_max_id {
			fm.sendid = 0
		}

		err := fm.sendwin.Set(int(f.Id), f)
		if err != nil {
			loggo.Error("sendwin Set halfclose fail %v", err)
		}
		fm.halfclosesend = true
	}
}

// sendIdRank returns the forward distance from origin to id in the id ring.
func (fm *FrameMgr) sendIdRank(origin, id int32) int32 {
	d := id - origin
	if d < 0 {
		d += fm.frame_max_id
	}
	return d
}

func (fm *FrameMgr) calSendList(cur int64) {
	cursorRank := int32(-1)
	var origin int32
	if fm.ct != nil && fm.ctLastSendId >= 0 {
		err, value := fm.sendwin.Get(int(fm.ctLastSendId))
		if err != nil || value == nil {
			fm.ctLastSendId = -1
		} else if front := fm.sendwin.FrontInter(); front != nil {
			origin = front.Value.(*Frame).Id
			cursorRank = fm.sendIdRank(origin, fm.ctLastSendId)
		}
	}

	// Same bound as GetSendWinSize: visit only the filled slots so the idle
	// tail of the ring is never scanned. This is the per-Update send walk.
	e := fm.sendwin.FrontInter()
	remaining := fm.sendwin.Size()
	for e != nil && remaining > 0 {
		remaining--
		f := e.Value.(*Frame)
		isNew := f.Sendtime == 0
		resendTimeout := int64(fm.resend_timems * (int)(time.Millisecond))
		if rttTimeout := fm.rttns * 2; rttTimeout > resendTimeout {
			resendTimeout = rttTimeout
		}
		isTimeout := !isNew && cur-f.Sendtime > resendTimeout
		minResendGap := fm.rttns / 2
		if fm.rttns > 0 && minResendGap < int64(time.Millisecond*50) {
			minResendGap = int64(time.Millisecond * 50)
		}
		isResend := !isNew && (f.Resend || isTimeout) && cur-f.Sendtime > minResendGap
		canSendFrame := !f.Acked && (isNew || isResend)

		if isResend || cursorRank < 0 || fm.sendIdRank(origin, f.Id) >= cursorRank {
			if canSendFrame {
				if fm.ct != nil && f.Data != nil && len(f.Data.Data) > 0 {
					if isNew {
						if !fm.ct.CanSend(int(f.Id), len(f.Data.Data)) {
							fm.ctLastSendId = f.Id
							return
						}
					} else {
						if resendChecker, ok := fm.ct.(interface{ CanSendResend(int, int) bool }); ok {
							if !resendChecker.CanSendResend(int(f.Id), len(f.Data.Data)) {
								fm.ctLastSendId = f.Id
								return
							}
						}
					}
				}
				f.Sendtime = cur
				fm.sendFrame(f)
				f.Resend = false
				if fm.openstat > 0 {
					fm.fs.sendDataNum++
					fm.fs.sendDataNumsMap[f.Id]++
				}
				//loggo.Debug("debugid %v push frame to sendlist %v %v", fm.debugid, f.Id, len(f.Data.Data))
			}
		}
		if remaining <= 0 {
			break
		}
		e = e.Next()
	}
	fm.ctLastSendId = -1
}

// GetSendFrames returns the outbound frames staged by sendFrame and empties
// the staging area. It returns nil whenever nothing is staged — including the
// empty-but-capped slice GetSendList leaves behind — so callers may treat a
// nil result as "no work this Update". The caller owns the slice until it
// hands it back once (PutSendFrames on this FrameMgr, or PutFrames for the
// shared pool) and must not read or append to it afterwards. Prefer this over
// GetSendList — it allocates no per-frame list nodes.
func (fm *FrameMgr) GetSendFrames() []*Frame {
	fm.sendlock.Lock()
	defer fm.sendlock.Unlock()
	if len(fm.sendq) == 0 {
		// Empty staging: report "no work" and keep any empty-but-capped slice
		// (GetSendList leaves one behind) as the spare. Never overwrite a spare
		// that is already parked here — doing so on every idle iteration throws
		// away the recycled storage and forces a fresh 512B slice next time.
		if fm.sendspare == nil {
			fm.sendspare = fm.sendq
		}
		fm.sendq = nil
		return nil
	}
	ret := fm.sendq
	fm.sendq = nil
	return ret
}

// PutFrames recycles a slice handed out by GetSendFrames. Optional —
// unreturned slices are simply garbage collected. The frames themselves are
// NOT freed here; only the slice storage is reused. Hot loops should prefer
// the FrameMgr method PutSendFrames, which reuses the storage locally instead
// of boxing a slice header into the shared pool on every call.
func PutFrames(fs []*Frame) {
	if cap(fs) == 0 || cap(fs) > 8192 {
		return
	}
	fs = fs[:0]
	frameSlicePool.Put(&fs)
}

// PutSendFrames recycles the slice handed out by GetSendFrames on this
// FrameMgr. The storage stays with the FrameMgr, so the staging round trip —
// which runs once per Update — neither touches the shared pool nor
// heap-allocates a slice header. Hand a slice back exactly once and only to
// one owner: returning it here and to PutFrames would leave two writers
// appending into the same array.
func (fm *FrameMgr) PutSendFrames(fs []*Frame) {
	if cap(fs) == 0 || cap(fs) > 8192 {
		return
	}
	fm.sendlock.Lock()
	fm.sendspare = fs[:0]
	fm.sendlock.Unlock()
}

var frameSlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]*Frame, 0, 64)
		return &s
	},
}

// recvMaps bundles the per-Update dedup/count scratch maps. Pooled because
// preProcessRecvList runs on every Update and three fresh maps per call
// dominated the allocator profile.
type recvMaps struct {
	req    map[int32]int
	ack    map[int32]int
	ackto  map[int32]*Frame
	cumAck int32
}

var recvMapsPool = sync.Pool{
	New: func() interface{} {
		return &recvMaps{
			req:    make(map[int32]int),
			ack:    make(map[int32]int),
			ackto:  make(map[int32]*Frame),
			cumAck: -1,
		}
	},
}

// sendFramePool recycles DATA frames popped from sendwin: each carries a
// payload buffer sized for a full frame, and cutting fresh Frame+FrameData
// per chunk dominated the send-side allocator profile. Frames are consumed
// synchronously by the emit side (marshal runs on the same goroutine that
// will next call Update), so recycling on the single ACK-pop path is safe.
var sendFramePool = sync.Pool{
	New: func() interface{} {
		return &Frame{Data: &FrameData{}}
	},
}

func getSendFrame(maxSize int) *Frame {
	f := sendFramePool.Get().(*Frame)
	fd := f.Data
	if cap(fd.Data) < maxSize {
		fd.Data = make([]byte, maxSize)
	}
	fd.Data = fd.Data[:maxSize]
	fd.Type = (int32)(FrameData_USER_DATA)
	fd.Compress = false
	f.Type = (int32)(Frame_DATA)
	f.Resend = false
	f.Sendtime = 0
	f.Dataid = nil
	f.Acked = false
	return f
}

func putSendFrame(f *Frame) {
	if f == nil {
		return
	}
	f.Dataid = nil
	sendFramePool.Put(f)
}

// GetSendList is the container/list flavor of GetSendFrames, kept for
// existing callers. New code should use GetSendFrames.
func (fm *FrameMgr) GetSendList() *list.List {
	fm.sendlock.Lock()
	defer fm.sendlock.Unlock()
	ret := list.New()
	for _, f := range fm.sendq {
		ret.PushBack(f)
	}
	fm.sendq = fm.sendq[:0]
	return ret
}

func (fm *FrameMgr) sendFrame(f *Frame) {
	fm.sendlock.Lock()
	defer fm.sendlock.Unlock()
	if fm.sendq == nil {
		fm.sendq = fm.sendspare
		fm.sendspare = nil
		if fm.sendq == nil {
			fm.sendq = make([]*Frame, 0, 64)
		}
	}
	fm.sendq = append(fm.sendq, f)
}

func (fm *FrameMgr) OnRecvFrame(f *Frame) {
	fm.recvlock.Lock()
	defer fm.recvlock.Unlock()
	fm.recvq = append(fm.recvq, f)
}

func (fm *FrameMgr) preProcessRecvList() *recvMaps {
	fm.recvlock.Lock()
	defer fm.recvlock.Unlock()

	rm := recvMapsPool.Get().(*recvMaps)
	clear(rm.req)
	clear(rm.ack)
	clear(rm.ackto)
	rm.cumAck = -1
	for _, f := range fm.recvq {
		if f.Type == (int32)(Frame_REQ) {
			for _, id := range f.Dataid {
				rm.req[id]++
				//loggo.Debug("debugid %v recv req %v %v", fm.debugid, f.Id, common.Int32ArrayToString(f.Dataid, ","))
			}
		} else if f.Type == (int32)(Frame_ACK) {
			for _, id := range f.Dataid {
				rm.ack[id]++
				//loggo.Debug("debugid %v recv ack %v %v", fm.debugid, f.Id, common.Int32ArrayToString(f.Dataid, ","))
			}
			if f.Id >= 0 && (rm.cumAck < 0 || fm.sendIdRank(rm.cumAck, f.Id) < fm.windowsize) {
				rm.cumAck = f.Id
			}
		} else if f.Type == (int32)(Frame_DATA) {
			rm.ackto[f.Id] = f
			if fm.openstat > 0 {
				fm.fs.recvDataNum++
				fm.fs.recvDataNumsMap[f.Id]++
			}
			//loggo.Debug("debugid %v recv data %v %v", fm.debugid, f.Id, len(f.Data.Data))
		} else if f.Type == (int32)(Frame_PING) {
			fm.processPing(f)
		} else if f.Type == (int32)(Frame_PONG) {
			fm.processPong(f)
		} else {
			loggo.Error("error frame type %v", f.Type)
		}
	}
	fm.recvq = fm.recvq[:0]
	return rm
}

func (fm *FrameMgr) processRecvList(rm *recvMaps) {

	for id, num := range rm.req {
		err, value := fm.sendwin.Get(int(id))
		if err != nil {
			loggo.Error("sendwin get id fail %v %v", id, err)
			continue
		}
		if value == nil {
			continue
		}
		f := value.(*Frame)
		if f.Id == id {
			f.Resend = true
			//loggo.Debug("debugid %v choose resend win %v %v", fm.debugid, f.Id, len(f.Data.Data))
		} else {
			loggo.Error("sendwin get id diff %v %v", id, f.Id)
			continue
		}
		if fm.openstat > 0 {
			fm.fs.recvReqNum += num
			fm.fs.recvReqNumsMap[id] += num
		}
	}

	for id, num := range rm.ack {
		err, value := fm.sendwin.Get(int(id))
		if err != nil {
			continue
		}
		if value == nil {
			continue
		}
		f := value.(*Frame)
		if f.Id == id {
			f.Acked = true
			//loggo.Debug("debugid %v remove send win %v %v", fm.debugid, f.Id, len(f.Data.Data))
		} else {
			loggo.Error("sendwin get id diff %v %v", id, f.Id)
			continue
		}
		if fm.openstat > 0 {
			fm.fs.recvAckNum += num
			fm.fs.recvAckNumsMap[id] += num
		}
		if fm.ct != nil {
			fm.ct.RecvAck(int(id), len(f.Data.Data))
		}
	}

	if rm.cumAck >= 0 {
		e := fm.sendwin.FrontInter()
		remaining := fm.sendwin.Size()
		for e != nil && remaining > 0 {
			remaining--
			f := e.Value.(*Frame)
			d := fm.sendIdRank(f.Id, rm.cumAck)
			if d > 0 && d <= fm.windowsize {
				if !f.Acked {
					f.Acked = true
					if fm.ct != nil && f.Data != nil {
						fm.ct.RecvAck(int(f.Id), len(f.Data.Data))
					}
				}
			} else {
				break
			}
			e = e.Next()
		}
	}

	for !fm.sendwin.Empty() {
		err, value := fm.sendwin.Front()
		if err != nil {
			loggo.Error("sendwin get Front fail %s", err)
			break
		}
		if value == nil {
			break
		}
		f := value.(*Frame)
		if f.Acked {
			putSendFrame(f)
			err := fm.sendwin.PopFront()
			if err != nil {
				loggo.Error("sendwin PopFront fail ")
				break
			}
		} else {
			break
		}
	}

	if len(rm.ackto) > 0 {
		tmpsize := common.MinOfInt(len(rm.ackto), fm.frame_max_size/2/4)
		tmp := make([]int32, len(rm.ackto)) // bounded by window; not worth pooling
		index := 0
		for id, rf := range rm.ackto {
			if fm.addToRecvWin(rf) {
				tmp[index] = id
				index++
				if fm.openstat > 0 {
					fm.fs.sendAckNum++
					fm.fs.sendAckNumsMap[id]++
				}
				if index >= tmpsize {
					f := &Frame{Type: (int32)(Frame_ACK), Resend: false, Sendtime: 0,
						Id:     fm.recvid,
						Dataid: tmp[0:index]}
					fm.sendFrame(f)
					index = 0
					tmp = make([]int32, len(rm.ackto))
					//loggo.Debug("debugid %v send ack %v %v", fm.debugid, f.Id, common.Int32ArrayToString(f.Dataid, ","))
				}
				//loggo.Debug("debugid %v add data to win %v %v", fm.debugid, rf.Id, len(rf.Data.Data))
			}
		}
		if index > 0 {
			f := &Frame{Type: (int32)(Frame_ACK), Resend: false, Sendtime: 0,
				Id:     fm.recvid,
				Dataid: tmp[0:index]}
			fm.sendFrame(f)
			//loggo.Debug("debugid %v send ack %v %v", fm.debugid, f.Id, common.Int32ArrayToString(f.Dataid, ","))
		}
	}
}

func (fm *FrameMgr) addToRecvWin(rf *Frame) bool {

	if !fm.isIdInRange(rf.Id, fm.frame_max_id) {
		//loggo.Debug("debugid %v recv frame not in range %v %v", fm.debugid, rf.Id, fm.recvid)
		if fm.isIdOld(rf.Id, fm.frame_max_id) {
			if fm.openstat > 0 {
				fm.fs.recvOldNum++
			}
			return true
		}
		if fm.openstat > 0 {
			fm.fs.recvOutWinNum++
		}
		return false
	}

	err := fm.recvwin.Set(int(rf.Id), rf)
	if err != nil {
		loggo.Error("recvwin Set fail %v", err)
		return false
	}
	return true
}

func (fm *FrameMgr) processRecvFrame(f *Frame) bool {
	if f.Data.Type == (int32)(FrameData_USER_DATA) {
		fm.recvblock.Lock()
		defer fm.recvblock.Unlock()

		left := fm.recvb.Capacity() - fm.recvb.Size()
		if left >= len(f.Data.Data) {
			src := f.Data.Data
			if f.Data.Compress {
				old, err := common.DeCompressDataZstd(src)
				if err != nil {
					loggo.Error("recv frame DeCompressDataZstd error %v", f.Id)
					return false
				}
				if left < len(old) {
					return false
				}
				//loggo.Debug("debugid %v DeCompressDataZstd recv frame %v %v %v", fm.debugid, f.Id, len(src), len(old))
				src = old
			}

			fm.lastRecvDataTime = time.Now().UnixNano()

			fm.recvb.Write(src)
			//loggo.Debug("debugid %v combined recv frame to recv buffer %v %v", fm.debugid, f.Id, len(src))
			return true
		}
		return false
	} else if f.Data.Type == (int32)(FrameData_CLOSE) {
		fm.remoteclosed = true
		//loggo.Debug("debugid %v recv remote close frame %v", fm.debugid, f.Id)
		return true
	} else if f.Data.Type == (int32)(FrameData_CONN) {
		fm.sendConnectRsp()
		fm.connected = true
		//loggo.Debug("debugid %v recv remote conn frame %v", fm.debugid, f.Id)
		return true
	} else if f.Data.Type == (int32)(FrameData_CONNRSP) {
		fm.connected = true
		//loggo.Debug("debugid %v recv remote conn rsp frame %v", fm.debugid, f.Id)
		return true
	} else if f.Data.Type == (int32)(FrameData_HB) {
		fm.lastRecvHBTime = time.Now().UnixNano()
		//loggo.Debug("debugid %v recv remote hb frame %v", fm.debugid, f.Id)
		return true
	} else if f.Data.Type == (int32)(FrameData_HALFCLOSE) {
		fm.remotesendclosed = true
		//loggo.Debug("debugid %v recv remote halfclose frame %v", fm.debugid, f.Id)
		return true
	} else {
		loggo.Error("recv frame type error %v", f.Data.Type)
		return false
	}
}

func (fm *FrameMgr) combineWindowToRecvBuffer(cur int64) {

	for {
		done := false
		err, value := fm.recvwin.Front()
		if err == nil {
			f := value.(*Frame)
			if f.Id == fm.recvid {
				delete(fm.reqmap, f.Id)
				if fm.processRecvFrame(f) {
					err := fm.recvwin.PopFront()
					if err != nil {
						loggo.Error("recvwin PopFront fail %v ", err)
					}
					done = true
					//loggo.Debug("debugid %v process recv frame ok %v %v", fm.debugid, f.Id, len(f.Data.Data))
				}
			}
		}
		if !done {
			//loggo.Debug("debugid %v combined need recvid %v ", fm.debugid, fm.recvid)
			break
		} else {
			fm.recvid++
			if fm.recvid >= fm.frame_max_id {
				fm.recvid = 0
			}
			//loggo.Debug("debugid %v combined ok add recvid %v ", fm.debugid, fm.recvid)
		}
	}

	// Walk arrived frames in ring order starting at recvid, even when the
	// begin slot (recvid itself) is a hole: FrontInter() returns nil in that
	// case, which used to suppress every REQ until the sender's timeout
	// retransmission happened. Holes between the cursor and the next arrived
	// frame are all REQ'd in one pass.
	maxReq := int(fm.windowsize)
	if n := fm.frame_max_size / 2 / 4; n < maxReq {
		maxReq = n
	}
	// Created on the first hole only: in-order delivery (the steady state)
	// never REQs anything, and this walk runs on every Update.
	var reqtmp map[int32]int
	id := fm.recvid
	// Slot scans are O(window) and recvwin is mostly empty: skip the walk when
	// nothing arrived out of order, and stop once every arrived frame has been
	// visited instead of scanning the empty tail of the ring.
	remaining := fm.recvwin.Size()
	var e *glist.ROBuffergoInter
	if remaining > 0 {
		e = fm.recvwin.FrontInterFromBegin()
	}
	for e != nil && len(reqtmp) < maxReq {
		fid := int32(e.Id())
		for id != fid && len(reqtmp) < maxReq {
			oldReq := fm.reqmap[id]
			reqInterval := fm.rttns * 3 / 4
			if fm.rttns > 0 && reqInterval < int64(time.Millisecond*50) {
				reqInterval = int64(time.Millisecond * 50)
			}
			if cur-oldReq > reqInterval {
				if reqtmp == nil {
					reqtmp = make(map[int32]int)
				}
				reqtmp[id]++
				fm.reqmap[id] = cur
			}
			id++
			if id >= fm.frame_max_id {
				id = 0
			}
		}
		if id != fid {
			// Stopped only because the REQ budget was exhausted.
			break
		}
		// fid has arrived; advance the cursor past it.
		id++
		if id >= fm.frame_max_id {
			id = 0
		}
		remaining--
		if remaining <= 0 {
			break
		}
		e = e.Next()
	}

	if len(reqtmp) > 0 {
		f := &Frame{Type: (int32)(Frame_REQ), Resend: false, Sendtime: 0,
			Id:     0,
			Dataid: make([]int32, len(reqtmp))}
		index := 0
		for id := range reqtmp {
			f.Dataid[index] = id
			index++
			if fm.openstat > 0 {
				fm.fs.sendReqNum++
				fm.fs.sendReqNumsMap[id]++
			}
		}
		fm.sendFrame(f)
		fm.sendFrame(f)
		//loggo.Debug("debugid %v send req %v %v", fm.debugid, f.Id, common.Int32ArrayToString(f.Dataid, ","))
	}
}

func (fm *FrameMgr) GetRecvBufferSize() int {
	fm.recvblock.Lock()
	defer fm.recvblock.Unlock()

	return fm.recvb.Size()
}

func (fm *FrameMgr) GetRecvReadLineBuffer() []byte {
	fm.recvblock.Lock()
	defer fm.recvblock.Unlock()

	ret := fm.recvb.GetReadLineBuffer()
	//loggo.Debug("debugid %v GetRecvReadLineBuffer %v %v", fm.debugid, fm.recvb.Size(), len(ret))
	return ret
}

func (fm *FrameMgr) SkipRecvBuffer(size int) {
	fm.recvblock.Lock()
	defer fm.recvblock.Unlock()

	fm.recvb.SkipRead(size)
	//loggo.Debug("debugid %v SkipRead %v %v", fm.debugid, fm.recvb.Size(), size)
}

func (fm *FrameMgr) Close() {
	fm.close = true
}

func (fm *FrameMgr) IsRemoteClosed() bool {
	return fm.remoteclosed
}

// CloseSend half-closes the send side: after the send buffer drains, a
// HALFCLOSE frame is delivered and the recv direction stays usable.
func (fm *FrameMgr) CloseSend() {
	fm.closesendreq = true
}

func (fm *FrameMgr) IsRemoteSendClosed() bool {
	return fm.remotesendclosed
}

// CloseFlushed reports that every frame this side sent — including its own
// close/halfclose marker — has been acknowledged by the peer.
func (fm *FrameMgr) CloseFlushed() bool {
	if !fm.closesend && !fm.halfclosesend {
		return false
	}
	return fm.sendb.Empty() && fm.sendwin.Size() == 0
}

func (fm *FrameMgr) ping() {
	cur := time.Now().UnixNano()
	if cur-fm.lastPingTime > (int64)(time.Millisecond * 200) {
		fm.lastPingTime = cur
		f := &Frame{Type: (int32)(Frame_PING), Resend: false, Sendtime: cur,
			Id: 0}
		fm.sendFrame(f)
		//loggo.Debug("debugid %v send ping %v", fm.debugid, cur)
		if fm.openstat > 0 {
			fm.fs.sendping++
		}
	}
}

func (fm *FrameMgr) hb() {
	cur := time.Now().UnixNano()
	if cur-fm.lastSendHBTime > (int64)(time.Second) && fm.sendwin.Size() < int(fm.windowsize) {
		fm.lastSendHBTime = cur

		fd := &FrameData{Type: (int32)(FrameData_HB)}

		f := &Frame{Type: (int32)(Frame_DATA),
			Id:   fm.sendid,
			Data: fd}

		fm.sendid++
		if fm.sendid >= fm.frame_max_id {
			fm.sendid = 0
		}

		err := fm.sendwin.Set(int(f.Id), f)
		if err != nil {
			loggo.Error("sendwin Set fail %v", err)
		}
		//loggo.Debug("debugid %v send hb %v", fm.debugid, f.Id)
	}
}

func (fm *FrameMgr) processPing(f *Frame) {
	rf := &Frame{Type: (int32)(Frame_PONG), Resend: false, Sendtime: f.Sendtime,
		Id: 0}
	fm.sendFrame(rf)
	if fm.openstat > 0 {
		fm.fs.recvping++
		fm.fs.sendpong++
	}
	//loggo.Debug("debugid %v recv ping %v", fm.debugid, f.Sendtime)
}

func (fm *FrameMgr) processPong(f *Frame) {
	cur := time.Now().UnixNano()
	if cur > f.Sendtime {
		rtt := cur - f.Sendtime
		if fm.lastPongTime == 0 {
			fm.rttns = rtt
		} else {
			fm.rttns = (fm.rttns*7 + rtt) / 8
		}
		if fm.ct != nil {
			if rttSetter, ok := fm.ct.(interface{ SetRtt(time.Duration) }); ok {
				rttSetter.SetRtt(time.Duration(fm.rttns))
			}
		}
		if fm.openstat > 0 {
			fm.fs.recvpong++
		}
		fm.lastPongTime = cur
		//loggo.Debug("debugid %v recv pong %v %dms", fm.debugid, rtt, fm.rttns/1000/1000)
	}
}

func (fm *FrameMgr) isIdInRange(id int32, maxid int32) bool {
	begin := fm.recvid
	end := fm.recvid + fm.windowsize
	if end >= maxid {
		if id >= 0 && id < end-maxid {
			return true
		}
		end = maxid
	}
	if id >= begin && id < end {
		return true
	}
	return false
}

func (fm *FrameMgr) isIdOld(id int32, maxid int32) bool {
	begin := fm.recvid - fm.windowsize
	if begin < 0 {
		begin += maxid
	}
	end := fm.recvid

	if begin < end {
		return id >= begin && id < end
	} else {
		return (id >= begin && id < maxid) || (id >= 0 && id < end)
	}
}

func (fm *FrameMgr) IsConnected() bool {
	return fm.connected
}

func (fm *FrameMgr) Connect() {
	if fm.sendwin.Size() < int(fm.windowsize) {
		fd := &FrameData{Type: (int32)(FrameData_CONN)}

		f := &Frame{Type: (int32)(Frame_DATA),
			Id:   fm.sendid,
			Data: fd}

		fm.sendid++
		if fm.sendid >= fm.frame_max_id {
			fm.sendid = 0
		}

		err := fm.sendwin.Set(int(f.Id), f)
		if err != nil {
			loggo.Error("sendwin Set fail %v", err)
		}
		//loggo.Debug("debugid %v start connect", fm.debugid)
	} else {
		loggo.Error("Connect fail ")
	}
}

func (fm *FrameMgr) sendConnectRsp() {
	if fm.sendwin.Size() < int(fm.windowsize) {
		fd := &FrameData{Type: (int32)(FrameData_CONNRSP)}

		f := &Frame{Type: (int32)(Frame_DATA),
			Id:   fm.sendid,
			Data: fd}

		fm.sendid++
		if fm.sendid >= fm.frame_max_id {
			fm.sendid = 0
		}

		err := fm.sendwin.Set(int(f.Id), f)
		if err != nil {
			loggo.Error("sendwin Set fail %v", err)
		}
		//loggo.Debug("debugid %v send connect rsp", fm.debugid)
	} else {
		loggo.Error("sendConnectRsp fail ")
	}
}

func (fm *FrameMgr) resetStat() {
	fm.fs = &FrameStat{}
	fm.fs.sendDataNumsMap = make(map[int32]int)
	fm.fs.recvDataNumsMap = make(map[int32]int)
	fm.fs.sendReqNumsMap = make(map[int32]int)
	fm.fs.recvReqNumsMap = make(map[int32]int)
	fm.fs.sendAckNumsMap = make(map[int32]int)
	fm.fs.recvAckNumsMap = make(map[int32]int)
}

func (fm *FrameMgr) second(cur int64) {
	if cur-fm.lastPrintStat > (int64)(time.Second) {
		fm.lastPrintStat = cur

		ctinfo := ""
		if fm.ct != nil {
			ctinfo = fm.ct.Info()
		}

		if fm.openstat > 0 {
			fs := fm.fs
			loggo.Info("\nsendDataNum %v\nrecvDataNum %v\nsendReqNum %v\nrecvReqNum %v\nsendAckNum %v\nrecvAckNum %v\n"+
				"sendDataNumsMap %v\nrecvDataNumsMap %v\nsendReqNumsMap %v\nrecvReqNumsMap %v\nsendAckNumsMap %v\nrecvAckNumsMap %v\n"+
				"sendping %v\nrecvping %v\nsendpong %v\nrecvpong %v\n"+
				"sendwin %v\nrecvwin %v\n"+
				"recvOldNum %v\nrecvOutWinNum %v\n"+
				"rtt %v\n"+
				"ct %v\n",
				fs.sendDataNum, fs.recvDataNum,
				fs.sendReqNum, fs.recvReqNum,
				fs.sendAckNum, fs.recvAckNum,
				fm.printStatMap(&fs.sendDataNumsMap), fm.printStatMap(&fs.recvDataNumsMap),
				fm.printStatMap(&fs.sendReqNumsMap), fm.printStatMap(&fs.recvReqNumsMap),
				fm.printStatMap(&fs.sendAckNumsMap), fm.printStatMap(&fs.recvAckNumsMap),
				fs.sendping, fs.recvping,
				fs.sendpong, fs.recvpong,
				fm.sendwin.Size(), fm.recvwin.Size(),
				fs.recvOldNum, fs.recvOutWinNum,
				time.Duration(fm.rttns).String(),
				ctinfo)
			fm.resetStat()
		}

		if fm.ct != nil {
			if rttSetter, ok := fm.ct.(interface{ SetRtt(time.Duration) }); ok && fm.rttns > 0 {
				rttSetter.SetRtt(time.Duration(fm.rttns))
			}
			fm.ct.Update()
		}

	}
}

func (fm *FrameMgr) printStatMap(m *map[int32]int) string {
	tmp := make(map[int]int)
	for _, v := range *m {
		tmp[v]++
	}
	max := 0
	for k := range tmp {
		if k > max {
			max = k
		}
	}
	var ret string
	for i := 1; i <= max; i++ {
		ret += strconv.Itoa(i) + "->" + strconv.Itoa(tmp[i]) + ","
	}
	if len(ret) <= 0 {
		ret = "none"
	}
	return ret
}

func (fm *FrameMgr) MarshalFrame(f *Frame) ([]byte, error) {
	resend := f.Resend
	sendtime := f.Sendtime
	mb, err := proto.Marshal(f)
	f.Resend = resend
	f.Sendtime = sendtime
	return mb, err
}

// MarshalFrameAppend serializes f into dst and returns the extended slice,
// letting callers that recycle their buffer avoid the allocation MarshalFrame
// makes for every frame. Resend/Sendtime are restored after marshaling, same
// as MarshalFrame.
func (fm *FrameMgr) MarshalFrameAppend(dst []byte, f *Frame) ([]byte, error) {
	resend := f.Resend
	sendtime := f.Sendtime
	mb, err := proto.MarshalOptions{}.MarshalAppend(dst, f)
	f.Resend = resend
	f.Sendtime = sendtime
	return mb, err
}

func (fm *FrameMgr) IsHBTimeout() bool {
	now := time.Now().UnixNano()
	if now-fm.lastRecvHBTime > int64(time.Second)*hbTimeoutSecond && now-fm.lastRecvDataTime > int64(time.Second)*hbTimeoutSecond {
		return true
	}
	return false
}
