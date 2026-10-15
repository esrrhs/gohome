package network

import (
	"bytes"
	"errors"
	"net"
	"strconv"
	"testing"

	"github.com/esrrhs/gohome/common"
	"google.golang.org/protobuf/proto"
)

/*
本文件只针对 network 里自己手写的那部分逻辑做模糊测试：帧管理器的分帧/重组、
拥塞控制状态机、SOCKS5 的字节流解析。标准算法（zstd、rc4 等）不在范围内。

设计原则：
  1. 不给被测逻辑传时钟：FrameMgr 的重传和 REQ 节流都依赖 rttns/时间差，
     用例里直接把 rttns 置 0，让行为完全由输入决定，避免 fuzz 出"偶发"结果。
  2. 断言的是契约（顺序、不丢、不重、不越界、失败必须是干净失败），
     而不是重新实现一遍被测逻辑。
*/

// ---------- FrameMgr 接收侧：乱序 + 重复投递后的重组 ----------

const (
	fuzzRecvMaxId  = 64 // id 环大小
	fuzzRecvWin    = 16 // 接收窗口，必须 < maxId
	fuzzRecvBufCap = 4096
	fuzzRecvCut    = 32
	fuzzRecvMaxOps = 48 // 必须 < maxId：保证随机阶段最多消费不到一环，
	// 否则 recvid 会绕回 0，"补齐后正好等于一整圈"这条断言就不成立了。
)

// fuzzRecvPayload 给出 id 对应的确定性载荷：长度与内容都随 id 变化，
// 任何错位、丢失、重复都会在逐字节比对时暴露。
func fuzzRecvPayload(id int32) []byte {
	n := 1 + int(id%8)
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(int(id)*3 + i*7)
	}
	return p
}

func fuzzRecvFrame(id int32) *Frame {
	return &Frame{
		Type: int32(Frame_DATA),
		Id:   id,
		Data: &FrameData{Type: int32(FrameData_USER_DATA), Data: fuzzRecvPayload(id)},
	}
}

// FuzzFrameMgrRecvReassembly 先把 fuzz 给出的任意 id 序列（含乱序、越界、
// 重复）灌进接收窗口，再从当前 recvid 起按序补齐，最后要求：
//   - 落进接收缓冲区的字节流始终是"期望流"的严格前缀（不重、不乱序、不凭空产生）；
//   - 补齐后必须完整还原整圈数据（不乱丢、窗口判定不误杀在窗内的帧）。
func FuzzFrameMgrRecvReassembly(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{5, 4, 3, 2, 1, 0})
	f.Add([]byte{63, 0, 62, 1, 40, 41})
	f.Add([]byte{0, 0, 0, 1, 1, 1})
	f.Add([]byte{130, 131, 200})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, ops []byte) {
		if len(ops) > fuzzRecvMaxOps {
			ops = ops[:fuzzRecvMaxOps]
		}

		fm := NewFrameMgr(fuzzRecvCut, fuzzRecvMaxId, fuzzRecvBufCap, fuzzRecvWin, 10, 0, 0)
		// REQ 的节流基于 rttns；置 0 让本用例完全由输入决定，摆脱墙钟。
		fm.rttns = 0

		var want []byte
		for id := int32(0); id < fuzzRecvMaxId; id++ {
			want = append(want, fuzzRecvPayload(id)...)
		}

		var got []byte
		drain := func() {
			for {
				if fm.GetRecvBufferSize() <= 0 {
					return
				}
				buf := fm.GetRecvReadLineBuffer()
				if len(buf) == 0 {
					return
				}
				got = append(got, buf...)
				fm.SkipRecvBuffer(len(buf))
			}
		}
		deliver := func(id int32) {
			fm.OnRecvFrame(fuzzRecvFrame(id))
			fm.Update()
			drain()
		}

		for _, b := range ops {
			id := int32(b) % fuzzRecvMaxId
			deliver(id)
			if b >= 128 { // 同一个 id 连投两次，模拟重传
				deliver(id)
			}
			if len(got) > len(want) || !bytes.Equal(got, want[:len(got)]) {
				t.Fatalf("recv stream diverged after id %d: got %d bytes:\n got %v\nwant %v",
					id, len(got), got, want[:min(len(got), len(want))])
			}
			if n := fm.GetRecvBufferSize(); n > fuzzRecvBufCap {
				t.Fatalf("recv buffer over capacity: %d > %d", n, fuzzRecvBufCap)
			}
		}

		// 补齐阶段：从当前 recvid 起严格按序投递，直到 recvid 绕回 0（整圈补齐）。
		// 注意不能按"投递次数"计数：一次投递可能因为补上空洞而连续推进多个 id。
		for i := 0; i < fuzzRecvMaxId; i++ {
			if i > 0 && fm.recvid == 0 {
				break
			}
			deliver(fm.recvid)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("reassembly incomplete: got %d bytes want %d", len(got), len(want))
		}
		if fm.recvid != 0 {
			t.Fatalf("recvid did not wrap to 0 after a full ring: %d", fm.recvid)
		}
	})
}

// ---------- FrameMgr 发送侧：任意分块写入后的切帧 ----------

// fuzzAckSent 把本轮发出的 DATA 帧标记为已确认并弹出发送窗口，
// 等价于对端全部回 ACK；这样发送侧可以脱离真实对端被单独驱动。
func fuzzAckSent(fm *FrameMgr) {
	for !fm.sendwin.Empty() {
		err, v := fm.sendwin.Front()
		if err != nil || v == nil {
			return
		}
		if !v.(*Frame).Acked {
			return
		}
		if err := fm.sendwin.PopFront(); err != nil {
			return
		}
	}
}

func FuzzFrameMgrCut(f *testing.F) {
	f.Add([]byte{1, 64, 3, 0, 200, 7}, byte(0))
	f.Add([]byte{64, 64, 64, 64, 64}, byte(0))
	f.Add([]byte{255, 1, 1, 1}, byte(1))
	f.Add([]byte{}, byte(1))
	f.Add([]byte{0, 0, 0}, byte(0))

	f.Fuzz(func(t *testing.T, chunks []byte, comp byte) {
		if len(chunks) > 64 {
			chunks = chunks[:64]
		}
		compress := 0
		if comp%2 == 1 {
			compress = 32
		}

		const cut = 64
		fm := NewFrameMgr(cut, 512, 16384, 32, 10, compress, 0)
		fm.rttns = 0

		var src []byte
		for _, b := range chunks {
			n := int(b) % 64
			for i := 0; i < n; i++ {
				src = append(src, byte(int(b)*7+i))
			}
		}
		fm.WriteSendBuffer(src)
		fm.Close()

		var got []byte
		collect := func() {
			sl := fm.GetSendList()
			for e := sl.Front(); e != nil; e = e.Next() {
				f := e.Value.(*Frame)
				if f.Type != int32(Frame_DATA) || f.Data == nil {
					continue
				}
				if f.Data.Type != int32(FrameData_USER_DATA) {
					continue // HB / CLOSE / CONN 帧不承载用户数据
				}
				// 同一次 Update 里同一帧不会被重复投递，但为稳妥起见先标记再收
				err, v := fm.sendwin.Get(int(f.Id))
				if err == nil && v != nil {
					v.(*Frame).Acked = true
				}
				payload := f.Data.Data
				if f.Data.Compress {
					old, err := common.DeCompressDataZstd(payload)
					if err != nil {
						t.Fatalf("frame %d failed to decompress: %v", f.Id, err)
					}
					payload = old
				}
				got = append(got, payload...)
			}
			fuzzAckSent(fm)
		}

		for i := 0; i < 512 && !fm.closesend; i++ {
			fm.Update()
			collect()
		}
		if !fm.closesend {
			t.Fatalf("close frame never sent: sendb=%d sendwin=%d", fm.GetSendBufferLeft(), fm.sendwin.Size())
		}

		if !bytes.Equal(got, src) {
			t.Fatalf("cut/reassembly mismatch: got %d bytes want %d", len(got), len(src))
		}
		if compress == 0 {
			// 关闭压缩时，除最后一片外每一片都必须切满 frame_max_size
			full := len(src) / cut
			for i := 0; i < full; i++ {
				if !bytes.Equal(got[i*cut:(i+1)*cut], src[i*cut:(i+1)*cut]) {
					t.Fatalf("frame %d not a full %d-byte slice", i, cut)
				}
			}
		}
	})
}

// ---------- FrameMgr 编组 ----------

// FuzzFrameMgrMarshal 保证帧的线上编解码不丢字段：ACK/REQ 这类 Data 为 nil
// 的帧、负 id、大 Sendtime 都必须原样往返，且 MarshalFrame 不得改动入参。
func FuzzFrameMgrMarshal(f *testing.F) {
	f.Add(int32(0), int32(0), false, int64(0), []byte("payload"), false, int32(0), []byte{1, 2, 3})
	f.Add(int32(2), int32(-1), true, int64(1<<40), []byte{}, true, int32(3), []byte{})
	f.Add(int32(3), int32(7), false, int64(-1), []byte(nil), false, int32(-1), []byte{255, 0})

	f.Fuzz(func(t *testing.T, typ int32, id int32, resend bool, sendtime int64,
		data []byte, compress bool, dtype int32, ids []byte) {
		if len(data) > 4096 {
			data = data[:4096]
		}
		if len(ids) > 64 {
			ids = ids[:64]
		}
		dataid := make([]int32, 0, len(ids))
		for _, b := range ids {
			dataid = append(dataid, int32(int8(b)))
		}

		fm := NewFrameMgr(1024, 1024, 8192, 64, 10, 0, 0)
		src := &Frame{
			Type:     typ,
			Resend:   resend,
			Sendtime: sendtime,
			Id:       id,
			Dataid:   dataid,
		}
		if dtype >= 0 {
			src.Data = &FrameData{Type: dtype, Compress: compress, Data: data}
		}

		mb, err := fm.MarshalFrame(src)
		if err != nil {
			t.Fatalf("MarshalFrame: %v", err)
		}
		if src.Resend != resend || src.Sendtime != sendtime || src.Id != id {
			t.Fatal("MarshalFrame mutated its argument")
		}

		var dst Frame
		if err := proto.Unmarshal(mb, &dst); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if !proto.Equal(src, &dst) {
			t.Fatalf("frame round trip mismatch:\n src %v\n dst %v", src, &dst)
		}
	})
}

// ---------- BBCongestion ----------

// FuzzBBCongestion 用"先 CanSend 再 RecvAck"的成对流量驱动状态机，
// 断言：Update 之后飞行量归零、窗口下限仍在、控制器不会被永久卡死，
// 且重复 Update 是无副作用的（flyeddata 为 0 时必须立即返回）。
func FuzzBBCongestion(f *testing.F) {
	f.Add([]byte{0, 1, 2})
	f.Add([]byte{64, 65, 66, 128, 128, 128})
	f.Add([]byte{192, 192, 192, 1, 65, 129, 193})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, ops []byte) {
		if len(ops) > 512 {
			ops = ops[:512]
		}
		bb := &BBCongestion{}
		bb.Init()

		outstanding := 0
		for _, b := range ops {
			op := b >> 6
			size := int(b&0x3f) * 4096
			switch op {
			case 0, 3:
				if bb.CanSend(int(b), size) {
					outstanding += size
				}
			case 1:
				if size > outstanding {
					size = outstanding
				}
				bb.RecvAck(int(b), size)
				outstanding -= size
			case 2:
				if bb.flyeddata <= 0 {
					// 一个 ACK 都没收到时 Update 必须立即返回，
					// 既不能改窗口，也不能动飞行量。
					maxfly, flying := bb.maxfly, bb.flyingdata
					bb.Update()
					if bb.maxfly != maxfly || bb.flyingdata != flying {
						t.Fatalf("Update changed state without feedback: maxfly %d->%d flying %d->%d",
							maxfly, bb.maxfly, flying, bb.flyingdata)
					}
					continue
				}

				bb.Update()
				outstanding = 0

				if bb.flyingdata != 0 || bb.flyeddata != 0 {
					t.Fatalf("Update left flying=%d flyed=%d", bb.flyingdata, bb.flyeddata)
				}
				if bb.maxfly < 1024*1024 {
					t.Fatalf("maxfly below floor after Update: %d", bb.maxfly)
				}
				if bb.status != bbc_status_init && bb.status != bbc_status_prop {
					t.Fatalf("unknown status %d", bb.status)
				}
				if !bb.CanSend(0, 1) {
					t.Fatal("controller permanently blocked right after Update")
				}
				// 归零后再 Update 同样必须完全无副作用
				maxfly := bb.maxfly
				bb.Update()
				if bb.maxfly != maxfly {
					t.Fatalf("idle Update changed maxfly: %d -> %d", maxfly, bb.maxfly)
				}
				info := bb.Info()
				if bytes.Contains([]byte(info), []byte("NaN")) ||
					bytes.Contains([]byte(info), []byte("Inf")) {
					t.Fatalf("Info() produced non-finite numbers: %s", info)
				}
			}
		}
	})
}

// ---------- SOCKS5 字节流解析 ----------

// fuzzSocksPktMax 是喂给 SOCKS5 解析器的报文上限：一个合法报文最长只有
// 4+1+255+2 字节，留一点余量足够。别调大——Go 的 fuzzer 在最小化大 []byte
// 输入时会耗掉整个 -fuzzminimizetime（默认 60s），吞吐会直接掉到 0。
const fuzzSocksPktMax = 320

// fuzzSocksAddrLen 独立地按 ATYP 算出 ATYP+ADDR+PORT 的长度，
// 用来验证 Sock5UnpackUDP 既没有多读也没有少读载荷。
func fuzzSocksAddrLen(b []byte) int {
	if len(b) < 1 {
		return -1
	}
	switch b[0] {
	case Socks5AtypIP4:
		return 1 + net.IPv4len + 2
	case Socks5AtypIP6:
		return 1 + net.IPv6len + 2
	case Socks5AtypDomain:
		if len(b) < 2 {
			return -1
		}
		return 1 + 1 + int(b[1]) + 2
	}
	return -1
}

// FuzzSock5UnpackUDP 断言 UDP 报文解析的三条性质：
//   - 失败必须是干净的失败（不返回半成品）；
//   - 成功时载荷必须正好是地址之后的全部剩余字节（不多不少）；
//   - 用解出的 host:port 重新打包再解包，三元组必须完全不变（幂等）。
func FuzzSock5UnpackUDP(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1, 1, 2, 3, 4, 0, 80, 'h', 'i'})
	f.Add([]byte{0, 0, 0, 3, 3, 'a', 'b', 'c', 0, 80})
	f.Add([]byte{0, 0, 0, 4, 0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 2})
	f.Add([]byte{0, 0, 0, 3, 0, 0, 80})
	f.Add([]byte{0, 0, 1, 1, 1, 2, 3, 4, 0, 80})
	f.Add([]byte{0, 0, 0, 9, 1, 2, 3, 4, 0, 80})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, pkt []byte) {
		if len(pkt) > fuzzSocksPktMax {
			pkt = pkt[:fuzzSocksPktMax]
		}
		host, port, data, err := Sock5UnpackUDP(pkt)
		if err != nil {
			if host != "" || port != 0 || data != nil {
				t.Fatalf("error path returned partial result: host=%q port=%d data=%v", host, port, data)
			}
			return
		}
		if host == "" {
			t.Fatal("empty host accepted")
		}
		if port < 0 || port > 65535 {
			t.Fatalf("port out of range: %d", port)
		}

		n := fuzzSocksAddrLen(pkt[3:])
		if n < 0 {
			t.Fatalf("accepted packet with unsupported ATYP %d", pkt[3])
		}
		if len(pkt)-3 < n {
			t.Fatalf("accepted truncated address: wire %d bytes, need %d", len(pkt)-3, n)
		}
		if len(data) != len(pkt)-3-n {
			t.Fatalf("payload length mismatch: got %d want %d", len(data), len(pkt)-3-n)
		}
		if !bytes.Equal(data, pkt[3+n:]) {
			t.Fatal("payload is not the exact trailing bytes")
		}

		rep, err := Sock5PackUDP(host, port, data)
		if err != nil {
			t.Fatalf("repack failed for accepted host %q: %v", host, err)
		}
		h2, p2, d2, err := Sock5UnpackUDP(rep)
		if err != nil {
			t.Fatalf("repacked packet rejected: %v", err)
		}
		if h2 != host || p2 != port || !bytes.Equal(d2, data) {
			t.Fatalf("round trip mismatch: %q:%d vs %q:%d", h2, p2, host, port)
		}
	})
}

// FuzzSock5GetRequest 断言请求解析的确定性，并做一次交叉校验：
// 解析出的 rawaddr 交给 UDP 路径再解一遍，必须得到同一个 host:port 且无残留。
// 两条手写解析路径一旦对 ATYP/长度的理解出现分歧，这里就会炸。
func FuzzSock5GetRequest(f *testing.F) {
	f.Add([]byte{5, 1, 0, 1, 1, 2, 3, 4, 0, 80})
	f.Add([]byte{5, 1, 0, 3, 3, 'a', 'b', 'c', 0, 80})
	f.Add([]byte{5, 3, 0, 4, 0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 80})
	f.Add([]byte{5, 2, 0, 1, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{4, 1, 0, 1, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{5, 1, 0, 3, 0, 0, 80})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > fuzzSocksPktMax {
			raw = raw[:fuzzSocksPktMax]
		}
		run := func() (byte, []byte, string, error) {
			return Sock5GetRequest(newScriptedRW(append([]byte(nil), raw...)))
		}
		cmd1, rawaddr1, host1, err1 := run()
		cmd2, rawaddr2, host2, err2 := run()

		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("non-deterministic error: %v vs %v", err1, err2)
		}
		if cmd1 != cmd2 || host1 != host2 || !bytes.Equal(rawaddr1, rawaddr2) {
			t.Fatalf("non-deterministic parse: %v/%q/%v vs %v/%q/%v",
				cmd1, host1, rawaddr1, cmd2, host2, rawaddr2)
		}
		if err1 != nil {
			return
		}
		if cmd1 != Socks5CmdConnect && cmd1 != Socks5CmdUDPAssociate {
			t.Fatalf("accepted unsupported cmd %d", cmd1)
		}
		h, pstr, err := net.SplitHostPort(host1)
		if err != nil {
			t.Fatalf("accepted unparsable host %q: %v", host1, err)
		}

		pkt := make([]byte, 0, 3+len(rawaddr1))
		pkt = append(pkt, 0, 0, 0)
		pkt = append(pkt, rawaddr1...)
		uh, up, udata, err := Sock5UnpackUDP(pkt)
		if err != nil {
			t.Fatalf("rawaddr %v rejected by UDP decoder: %v", rawaddr1, err)
		}
		if len(udata) != 0 {
			t.Fatalf("rawaddr left %d trailing bytes", len(udata))
		}
		if uh != h || strconv.Itoa(up) != pstr {
			t.Fatalf("cross-check mismatch: udp %q:%d vs request %q:%s", uh, up, h, pstr)
		}
	})
}

// FuzzSock5HandshakeAuth 用 fuzz 提供的方法清单和口令拼出一条格式正确的
// 客户端字节流，再断言握手的准入判定：
//   - 清单里有需要的方法 + 口令正确 → 必须接受，且回包符合 RFC；
//   - 清单里没有需要的方法 → 必须是 errMethod（不能因为别的原因"侥幸"通过）；
//   - 口令不对 → 必须是 errAuth（杜绝鉴权绕过）。
func FuzzSock5HandshakeAuth(f *testing.F) {
	f.Add([]byte{NoAuth}, "u", "p")
	f.Add([]byte{UserPassAuth}, "u", "p")
	f.Add([]byte{NoAuth, UserPassAuth}, "u", "p")
	f.Add([]byte{NoAuth, UserPassAuth}, "", "")
	f.Add([]byte{0x7f, 0x80}, "u", "p")
	f.Add([]byte{}, "u", "p")

	f.Fuzz(func(t *testing.T, methods []byte, user string, pass string) {
		if len(methods) > 255 {
			methods = methods[:255]
		}
		if len(user) > 200 {
			user = user[:200]
		}
		if len(pass) > 200 {
			pass = pass[:200]
		}
		stream := []byte{socksVer5, byte(len(methods))}
		stream = append(stream, methods...)
		stream = append(stream, userAuthVersion, byte(len(user)))
		stream = append(stream, user...)
		stream = append(stream, byte(len(pass)))
		stream = append(stream, pass...)

		needAuth := user != "" || pass != ""
		want := NoAuth
		if needAuth {
			want = UserPassAuth
		}
		offered := methodOffered(methods, want)

		rw := newScriptedRW(stream)
		err := Sock5HandshakeBy(rw, user, pass)
		if !offered {
			if err == nil {
				t.Fatalf("handshake accepted without required method %d (methods=%v)", want, methods)
			}
			if !errors.Is(err, errMethod) {
				t.Fatalf("want errMethod, got %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("well-formed handshake rejected: %v", err)
		}
		out := rw.out.Bytes()
		if needAuth {
			if !bytes.Equal(out, []byte{socksVer5, UserPassAuth, userAuthVersion, authSuccess}) {
				t.Fatalf("unexpected auth reply %v", out)
			}
		} else {
			if !bytes.Equal(out, []byte{socksVer5, NoAuth}) {
				t.Fatalf("unexpected noauth reply %v", out)
			}
		}

		if !needAuth {
			return
		}
		wrong := "z"
		if user == "z" {
			wrong = "y"
		}
		if err := Sock5HandshakeBy(newScriptedRW(stream), wrong, pass); err == nil {
			t.Fatal("handshake accepted a wrong username")
		} else if !errors.Is(err, errAuth) {
			t.Fatalf("want errAuth, got %v", err)
		}
	})
}
