package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/esrrhs/gohome/common"
	"github.com/esrrhs/gohome/loggo"
	"github.com/esrrhs/gohome/thread"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"google.golang.org/protobuf/proto"
)

// ICMP address families supported by RicmpConn.
const (
	icmpFamilyV4 = 4
	icmpFamilyV6 = 6
)

// Socket modes: privileged raw ICMP vs unprivileged datagram (ping) sockets.
const (
	icmpModeRaw   = "raw"
	icmpModeDgram = "dgram"
)

// icmpNetwork returns the x/net/icmp raw socket network for the given family.
func icmpNetwork(family int) string {
	if family == icmpFamilyV6 {
		return "ip6:icmp"
	}
	return "ip4:icmp"
}

// icmpDgramNetwork returns the unprivileged datagram network ("ping sockets"):
// no CAP_NET_RAW on Linux (subject to net.ipv4.ping_group_range) and no root
// on Darwin. Only echo requests can originate from these sockets, so they
// are usable on the Dial side; the listener side still needs raw sockets.
func icmpDgramNetwork(family int) string {
	if family == icmpFamilyV6 {
		return "udp6"
	}
	return "udp4"
}

// dialRicmpSocket opens an ICMP socket for the Dial side, preferring the
// privileged raw socket and transparently falling back to the unprivileged
// datagram socket when raw access is denied (EPERM/EACCES). forceDgram
// (used by tests) skips the raw attempt. For IPv6 datagram sockets the
// bind address is the kernel-selected source toward dst (see
// dgramBindAddress); raw sockets bind the wildcard.
func dialRicmpSocket(family int, forceDgram bool, dst *net.IPAddr) (*icmp.PacketConn, string, error) {
	dgramAddr := dgramBindAddress(family, dst)
	if !forceDgram {
		pc, err := icmp.ListenPacket(icmpNetwork(family), "")
		if err == nil {
			return pc, icmpModeRaw, nil
		}
		// Fall through to the datagram attempt; keep the raw error so a
		// total failure explains both the permission and the (possible)
		// missing kernel support (e.g. Linux ping_group_range).
		pc2, err2 := icmp.ListenPacket(icmpDgramNetwork(family), dgramAddr)
		if err2 != nil {
			return nil, "", fmt.Errorf("ricmp raw socket: %w; unprivileged dgram socket: %v", err, err2)
		}
		return pc2, icmpModeDgram, nil
	}
	pc, err := icmp.ListenPacket(icmpDgramNetwork(family), dgramAddr)
	if err != nil {
		return nil, "", err
	}
	return pc, icmpModeDgram, nil
}

// datagramEchoID returns the echo identifier a datagram socket must use.
// On Linux the kernel rewrites the outbound identifier to the socket's bound
// local port and filters inbound replies by it, so the id must equal that
// port. On Darwin the user-chosen id is preserved and LocalAddr reports port
// 0, in which case 0 tells the caller to keep a random id.
func datagramEchoID(conn *icmp.PacketConn) int {
	ua, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || ua == nil || ua.Port <= 0 {
		return 0
	}
	return ua.Port
}

// dgramBindAddress returns the local address an unprivileged datagram ICMP
// socket should bind to. The Linux ping-socket lookup table delivers an
// inbound ICMPv6 echo reply only when its destination address equals the
// socket's bound local address; a wildcard ("::") bind never matches a
// reply addressed to a concrete source such as ::1 (the IPv4 branch treats
// wildcard as match-any). Select the source the kernel routes toward the
// target with a short UDP probe (no packets are sent) and bind that
// address for IPv6. IPv4 keeps the wildcard bind.
func dgramBindAddress(family int, dst *net.IPAddr) string {
	if family != icmpFamilyV6 {
		return ""
	}
	probe, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: dst.IP, Zone: dst.Zone, Port: 9})
	if err != nil {
		return ""
	}
	defer probe.Close()
	if la, ok := probe.LocalAddr().(*net.UDPAddr); ok && la.IP != nil {
		return la.IP.String()
	}
	return ""
}

// familyFromIP classifies an IP as ICMPv4 or ICMPv6. IPv4-mapped IPv6
// addresses (e.g. ::ffff:1.2.3.4) are treated as IPv4 because ICMP raw
// sockets are not dual-stack.
func familyFromIP(ip net.IP) int {
	if ip.To4() != nil {
		return icmpFamilyV4
	}
	return icmpFamilyV6
}

// familyFromPacketConn reports whether an icmp.PacketConn carries ICMPv6.
func familyFromPacketConn(conn *icmp.PacketConn) int {
	if conn.IPv6PacketConn() != nil {
		return icmpFamilyV6
	}
	return icmpFamilyV4
}

// icmpEchoType maps the logical IcmpMsg ping/pong proto onto the concrete
// ICMP echo type of the address family: ICMPv4 uses 8/0, ICMPv6 uses 128/129.
func icmpEchoType(icmpProto int, family int) icmp.Type {
	if family == icmpFamilyV6 {
		if icmpProto == int(IcmpMsg_PING_PROTO) {
			return ipv6.ICMPTypeEchoRequest
		}
		return ipv6.ICMPTypeEchoReply
	}
	return ipv4.ICMPType(icmpProto)
}

// parseRicmpListenAddr resolves the Listen bind address into the set of ICMP
// families to open. Wildcard/unspecified addresses ("" / "0.0.0.0" / "::")
// bind both IPv4 and IPv6; an explicit IP literal binds only its own family.
// The returned bind address is always a literal (or "" for wildcard).
func parseRicmpListenAddr(dst string) (families []int, bind string, err error) {
	dst = strings.TrimSpace(dst)
	if dst == "" {
		return []int{icmpFamilyV4, icmpFamilyV6}, "", nil
	}
	ip := net.ParseIP(dst)
	if ip == nil {
		return nil, "", fmt.Errorf("ricmp listen: invalid bind address %q (host only, port is not allowed)", dst)
	}
	if ip.IsUnspecified() {
		return []int{icmpFamilyV4, icmpFamilyV6}, "", nil
	}
	return []int{familyFromIP(ip)}, dst, nil
}

/*
RicmpConn 实现了基于 可靠icmp 协议的Conn。
*/

type RicmpConfig struct {
	MaxPacketSize      int
	CutSize            int
	MaxId              int
	BufferSize         int
	MaxWin             int
	ResendTimems       int
	Compress           int
	Stat               int
	ConnectTimeoutMs   int
	CloseTimeoutMs     int
	CloseWaitTimeoutMs int
	AcceptChanLen      int
	Congestion         string
}

func DefaultRicmpConfig() *RicmpConfig {
	return &RicmpConfig{
		MaxPacketSize:      2048,
		CutSize:            1200,
		MaxId:              100000,
		BufferSize:         1024 * 1024,
		MaxWin:             10000,
		ResendTimems:       200,
		Compress:           0,
		Stat:               0,
		ConnectTimeoutMs:   10000,
		CloseTimeoutMs:     5000,
		CloseWaitTimeoutMs: 5000,
		AcceptChanLen:      128,
		Congestion:         "bb",
	}
}

type RicmpConn struct {
	id            string
	config        *RicmpConfig
	dialer        *ricmpConnDialer
	listenersonny *ricmpConnListenerSonny
	listener      *ricmpConnListener
	isclose       atomic.Bool
	closelock     sync.Mutex
	cfgMu         sync.RWMutex

	// srcIP caches the local source address selected toward the peer, used
	// to build the ICMPv6 pseudo-header checksum (raw v6 only).
	srcMu sync.Mutex
	srcIP net.IP
}

// sourceIPTowards returns the source address the kernel routes toward dst,
// discovered with an unconnected UDP probe (no packets are sent). The result
// is cached per conn because the peer address never changes.
func (c *RicmpConn) sourceIPTowards(dst net.IP, zone string) net.IP {
	c.srcMu.Lock()
	defer c.srcMu.Unlock()
	if c.srcIP != nil {
		return c.srcIP
	}
	probe, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: dst, Zone: zone, Port: 9})
	if err != nil {
		return nil
	}
	defer probe.Close()
	if la, ok := probe.LocalAddr().(*net.UDPAddr); ok {
		c.srcIP = la.IP
	}
	return c.srcIP
}

type ricmpConnDialer struct {
	serveraddr *net.IPAddr
	conn       *icmp.PacketConn
	fm         *FrameMgr
	wg         *thread.Group
	family     int    // icmpFamilyV4 or icmpFamilyV6
	mode       string // icmpModeRaw or icmpModeDgram
	icmpId     int
	icmpSeq    int32
	icmpProto  int
	icmpFlag   IcmpMsg_TYPE
}

type ricmpConnListenerSonny struct {
	dstaddr    net.Addr
	fatherconn *icmp.PacketConn
	listener   *ricmpConnListener
	fm         *FrameMgr
	wg         *thread.Group
	family     int // icmpFamilyV4 or icmpFamilyV6
	icmpId     int
	icmpSeq    int32 // written by recv loop, read by send loop
	icmpProto  int
	icmpFlag   IcmpMsg_TYPE
}

type ricmpConnListener struct {
	// One socket per address family: wildcard listen opens both IPv4 and
	// IPv6 because ICMP raw sockets are not dual-stack.
	listenerconns []*icmp.PacketConn
	wg            *thread.Group
	sonny         sync.Map
	accept        *common.Channel
}

func (c *RicmpConn) Name() string {
	return "ricmp"
}

func (c *RicmpConn) Read(p []byte) (n int, err error) {
	c.checkConfig()

	if c.isclose.Load() {
		return 0, errors.New("read closed conn")
	}

	if len(p) <= 0 {
		return 0, errors.New("read empty buffer")
	}

	var fm *FrameMgr
	var wg *thread.Group
	if c.dialer != nil {
		fm = c.dialer.fm
		wg = c.dialer.wg
	} else if c.listener != nil {
		return 0, errors.New("listener can not be read")
	} else if c.listenersonny != nil {
		fm = c.listenersonny.fm
		wg = c.listenersonny.wg
	} else {
		return 0, errors.New("empty conn")
	}

	for !c.isclose.Load() {
		if fm.GetRecvBufferSize() <= 0 {
			if wg != nil && wg.IsExit() {
				return 0, errors.New("closed conn")
			}
			time.Sleep(time.Millisecond * 100)
			continue
		}

		size := copy(p, fm.GetRecvReadLineBuffer())
		fm.SkipRecvBuffer(size)
		return size, nil
	}

	return 0, errors.New("read closed conn")
}

func (c *RicmpConn) Write(p []byte) (n int, err error) {
	c.checkConfig()

	if c.isclose.Load() {
		return 0, errors.New("write closed conn")
	}

	if len(p) <= 0 {
		return 0, errors.New("write empty data")
	}

	var fm *FrameMgr
	var wg *thread.Group
	if c.dialer != nil {
		fm = c.dialer.fm
		wg = c.dialer.wg
	} else if c.listener != nil {
		return 0, errors.New("listener can not be write")
	} else if c.listenersonny != nil {
		fm = c.listenersonny.fm
		wg = c.listenersonny.wg
	} else {
		return 0, errors.New("empty conn")
	}

	totalsize := len(p)
	cur := 0

	for !c.isclose.Load() {
		size := totalsize - cur
		svleft := fm.GetSendBufferLeft()
		if size > svleft {
			size = svleft
		}

		if size <= 0 {
			if wg != nil && wg.IsExit() {
				if cur > 0 {
					return cur, errors.New("closed conn")
				}
				return 0, errors.New("closed conn")
			}
			time.Sleep(time.Millisecond * 100)
			continue
		}

		fm.WriteSendBuffer(p[cur : cur+size])
		cur += size

		if cur >= totalsize {
			return totalsize, nil
		}

		time.Sleep(time.Millisecond * 100)
	}

	if cur > 0 {
		return cur, errors.New("write closed conn")
	}
	return 0, errors.New("write closed conn")
}

func (c *RicmpConn) Close() error {
	c.checkConfig()

	if c.isclose.Load() {
		return nil
	}

	c.closelock.Lock()
	defer c.closelock.Unlock()

	if c.isclose.Load() {
		return nil
	}

	// Mark closed first so Read/Write observe it before we tear down the socket.
	c.isclose.Store(true)

	if c.dialer != nil {
		if c.dialer.wg != nil {
			c.dialer.wg.Stop()
			// Join (not Wait): Wait returns as soon as Stop closes donech.
			_ = c.dialer.wg.Join()
		}
		if c.dialer.conn != nil {
			c.dialer.conn.Close()
		}
	} else if c.listener != nil {
		if c.listener.accept != nil {
			c.listener.accept.Close()
		}
		for _, lc := range c.listener.listenerconns {
			lc.Close()
		}
		c.listener.sonny.Range(func(key, value interface{}) bool {
			u := value.(*RicmpConn)
			_ = u.Close()
			return true
		})
		if c.listener.wg != nil {
			c.listener.wg.Stop()
			_ = c.listener.wg.Join()
		}
	} else if c.listenersonny != nil {
		if c.listenersonny.wg != nil {
			c.listenersonny.wg.Stop()
			_ = c.listenersonny.wg.Join()
		}
		if c.listenersonny.listener != nil {
			c.listenersonny.listener.sonny.Delete(c.id)
		}
	}

	return nil
}

func (c *RicmpConn) Info() string {
	c.checkConfig()

	if c.dialer != nil {
		return c.dialer.conn.LocalAddr().String() + "<--ricmp dialer " + c.id + "-->" + c.dialer.serveraddr.String()
	}
	if c.listener != nil {
		addrs := make([]string, 0, len(c.listener.listenerconns))
		for _, lc := range c.listener.listenerconns {
			addrs = append(addrs, lc.LocalAddr().String())
		}
		return "ricmp listener " + c.id + "--" + strings.Join(addrs, ",")
	}
	if c.listenersonny != nil {
		return c.listenersonny.fatherconn.LocalAddr().String() + "<--ricmp listenersonny " + c.id + "-->" + c.listenersonny.dstaddr.String()
	}
	return "empty ricmp conn"
}

func (c *RicmpConn) Dial(dst string) (Conn, error) {
	return c.dial(dst, false)
}

// dial is Dial with a test-only switch to force the unprivileged datagram
// socket instead of first attempting the privileged raw socket.
func (c *RicmpConn) dial(dst string, forceDgram bool) (Conn, error) {
	c.checkConfig()

	addr, err := net.ResolveIPAddr("ip", dst)
	if err != nil {
		return nil, err
	}

	family := familyFromIP(addr.IP)
	conn, mode, err := dialRicmpSocket(family, forceDgram, addr)
	if err != nil {
		return nil, err
	}

	// Raw mode keeps a random id. Datagram mode must use the kernel-assigned
	// local port as the echo id where the platform rewrites/filters on it
	// (Linux); a 0 result (Darwin) keeps the random id which is preserved.
	icmpId := rand.Intn(math.MaxInt16)
	if mode == icmpModeDgram {
		if id := datagramEchoID(conn); id > 0 {
			icmpId = id
		}
	}

	id := common.Guid()
	fm := NewFrameMgr(c.config.CutSize, c.config.MaxId, c.config.BufferSize, c.config.MaxWin, c.config.ResendTimems, c.config.Compress, c.config.Stat)
	fm.SetDebugid(id + "-dialer")
	if c.config.Congestion == "bb" {
		fm.SetCongestion(&BBCongestion{})
	}

	dialer := &ricmpConnDialer{serveraddr: addr, conn: conn, fm: fm, family: family, mode: mode,
		icmpId: icmpId, icmpSeq: 0, icmpProto: int(IcmpMsg_PING_PROTO), icmpFlag: IcmpMsg_CLIENT_SEND_FLAG}

	if os.Getenv("RICMP_DEBUG") != "" {
		loggo.Info("RICMP_DEBUG dial start dst=%s family=%d mode=%s icmpId=%d localAddr=%s",
			dst, family, mode, icmpId, conn.LocalAddr())
	}

	u := &RicmpConn{id: id, config: c.config, dialer: dialer}

	//loggo.Debug("start connect remote ricmp %s %s", u.Info(), id)

	u.dialer.fm.Connect()

	startConnectTime := time.Now()
	buf := make([]byte, c.config.MaxPacketSize)
	for {
		if u.dialer.fm.IsConnected() {
			break
		}

		u.dialer.fm.Update()

		// send icmp
		sendlist := u.dialer.fm.GetSendList()
		for e := sendlist.Front(); e != nil; e = e.Next() {
			f := e.Value.(*Frame)
			mb, _ := u.dialer.fm.MarshalFrame(f)
			u.dialer.conn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))
			u.send_icmp(u.dialer.conn, u.dialer.family, u.dialer.mode, mb, u.dialer.serveraddr,
				u.id, u.dialer.icmpId, int(u.dialer.icmpSeq), u.dialer.icmpProto, u.dialer.icmpFlag)
			u.dialer.icmpSeq++
		}

		// recv icmp
		u.dialer.conn.SetReadDeadline(time.Now().Add(time.Millisecond * 100))
		n, _, _, id, echoId, _, echoFlag := u.recv_icmp(u.dialer.conn, buf)
		if os.Getenv("RICMP_DEBUG") != "" && n > 0 {
			loggo.Info("RICMP_DEBUG dial recv n=%d id=%q want=%q echoId=%d wantId=%d flag=%d wantFlag=%d",
				n, id, u.id, echoId, u.dialer.icmpId, echoFlag, int(IcmpMsg_SERVER_SEND_FLAG))
		}
		if n > 0 && id == u.id && echoId == u.dialer.icmpId && echoFlag == int(IcmpMsg_SERVER_SEND_FLAG) {
			f := &Frame{}
			err := proto.Unmarshal(buf[0:n], f)
			if err == nil {
				u.dialer.fm.OnRecvFrame(f)
			} else {
				//loggo.Error("%s %s Unmarshal fail %s", c.Info(), u.Info(), err)
				break
			}
		}

		if c.isclose.Load() {
			//loggo.Debug("can not connect remote ricmp %s", u.Info())
			break
		}

		// timeout
		now := time.Now()
		diffclose := now.Sub(startConnectTime)
		if diffclose > time.Millisecond*time.Duration(c.config.ConnectTimeoutMs) {
			//loggo.Debug("can not connect remote ricmp %s", u.Info())
			break
		}

		time.Sleep(time.Millisecond * 10)
	}

	if c.isclose.Load() {
		u.Close()
		return nil, errors.New("closed conn")
	}

	if u.isclose.Load() {
		return nil, errors.New("closed conn")
	}

	if !u.dialer.fm.IsConnected() {
		u.Close()
		return nil, errors.New("connect timeout")
	}

	//loggo.Debug("connect remote ok ricmp %s", u.Info())

	wg := thread.NewGroup("RicmpConn serveListenerSonny"+" "+u.Info(), nil, nil)

	u.dialer.wg = wg

	wg.Go("RicmpConn updateDialerSonny"+" "+u.Info(), func() error {
		return u.updateDialerSonny()
	})

	return u, nil
}

func (c *RicmpConn) Listen(dst string) (Conn, error) {
	c.checkConfig()

	families, bind, err := parseRicmpListenAddr(dst)
	if err != nil {
		return nil, err
	}

	// Wildcard binds open both families; a failure on one family (e.g. the
	// host has no IPv6 stack) degrades to the other instead of failing.
	var conns []*icmp.PacketConn
	for _, family := range families {
		pc, perr := icmp.ListenPacket(icmpNetwork(family), bind)
		if perr != nil {
			if len(families) > 1 {
				loggo.Warn("ricmp listen: skip %s on %s: %s", icmpNetwork(family), bind, perr.Error())
				continue
			}
			return nil, perr
		}
		conns = append(conns, pc)
	}
	if len(conns) == 0 {
		return nil, errors.New("ricmp listen: no icmp socket available")
	}

	ch := common.NewChannel(c.config.AcceptChanLen)

	wg := thread.NewGroup("RicmpConn Listen"+" "+dst, nil, nil)

	listener := &ricmpConnListener{
		listenerconns: conns,
		wg:            wg,
		accept:        ch,
	}

	u := &RicmpConn{id: common.UniqueId(), config: c.config, listener: listener}
	for _, pc := range conns {
		pc := pc
		wg.Go("RicmpConn loopListenerRecv"+" "+dst, func() error {
			return u.loopListenerRecv(pc)
		})
	}

	return u, nil
}

func (c *RicmpConn) Accept() (Conn, error) {
	c.checkConfig()

	if c.listener == nil || c.listener.wg == nil {
		return nil, errors.New("not listen")
	}
	for !c.listener.wg.IsExit() {
		s := <-c.listener.accept.Ch()
		if s == nil {
			break
		}
		sonny := s.(*RicmpConn)
		_, ok := c.listener.sonny.Load(sonny.id)
		if !ok {
			continue
		}
		if sonny.isclose.Load() {
			continue
		}
		return sonny, nil
	}
	return nil, errors.New("listener close")
}

func (c *RicmpConn) checkConfig() {
	c.cfgMu.Lock()
	if c.config == nil {
		c.config = DefaultRicmpConfig()
	}
	c.cfgMu.Unlock()
}

func (c *RicmpConn) SetConfig(config *RicmpConfig) {
	c.cfgMu.Lock()
	c.config = config
	c.cfgMu.Unlock()
}

func (c *RicmpConn) GetConfig() *RicmpConfig {
	c.cfgMu.Lock()
	if c.config == nil {
		c.config = DefaultRicmpConfig()
	}
	cfg := c.config
	c.cfgMu.Unlock()
	return cfg
}

func (c *RicmpConn) loopListenerRecv(listenerconn *icmp.PacketConn) error {
	c.checkConfig()

	family := familyFromPacketConn(listenerconn)

	buf := make([]byte, c.config.MaxPacketSize)
	for !c.listener.wg.IsExit() {
		listenerconn.SetReadDeadline(time.Now().Add(time.Millisecond * 100))
		n, srcaddr, err, cid, echoId, echoSeq, echoFlag := c.recv_icmp(listenerconn, buf)
		if os.Getenv("RICMP_DEBUG") != "" && n > 0 {
			loggo.Info("RICMP_DEBUG listener recv family=%d n=%d src=%v echoId=%d flag=%d",
				family, n, srcaddr, echoId, echoFlag)
		}
		if err != nil || echoFlag != int(IcmpMsg_CLIENT_SEND_FLAG) {
			continue
		}

		v, ok := c.listener.sonny.Load(cid)
		if !ok {
			fm := NewFrameMgr(c.config.CutSize, c.config.MaxId, c.config.BufferSize, c.config.MaxWin, c.config.ResendTimems, c.config.Compress, c.config.Stat)
			fm.SetDebugid(cid + "-listenersonny")
			if c.config.Congestion == "bb" {
				fm.SetCongestion(&BBCongestion{})
			}

			sonny := &ricmpConnListenerSonny{dstaddr: srcaddr, fatherconn: listenerconn, listener: c.listener, fm: fm, family: family,
				icmpId: echoId, icmpSeq: int32(echoSeq), icmpProto: int(IcmpMsg_PONG_PROTO), icmpFlag: IcmpMsg_SERVER_SEND_FLAG}

			u := &RicmpConn{id: cid, config: c.config, listenersonny: sonny}
			c.listener.sonny.Store(cid, u)

			// Feed the first packet (often CONNECT) into FrameMgr immediately;
			// previously it was dropped and relied solely on retransmission.
			f := &Frame{}
			if err := proto.Unmarshal(buf[0:n], f); err == nil {
				u.listenersonny.fm.OnRecvFrame(f)
			}

			c.listener.wg.Go("RicmpConn accept"+" "+u.Info(), func() error {
				return c.accept(u)
			})

			//loggo.Debug("start accept remote ricmp %s %s", u.Info(), cid)
		} else {
			u := v.(*RicmpConn)
			if u.isclose.Load() {
				c.listener.sonny.Delete(cid)
				continue
			}
			atomic.StoreInt32(&u.listenersonny.icmpSeq, int32(echoSeq))

			f := &Frame{}
			err := proto.Unmarshal(buf[0:n], f)
			if err == nil {
				u.listenersonny.fm.OnRecvFrame(f)
				//loggo.Debug("%s recv frame %d %v", u.Info(), f.Id, f.String())
			} else {
				//loggo.Error("%s %s Unmarshal fail %s", c.Info(), u.Info(), err)
			}
		}

		c.listener.sonny.Range(func(key, value interface{}) bool {
			u := value.(*RicmpConn)
			if u.isclose.Load() {
				c.listener.sonny.Delete(key)
				//loggo.Debug("delete sonny from map %s", u.Info())
			}
			return true
		})
	}
	return nil
}

func (c *RicmpConn) accept(u *RicmpConn) error {

	//loggo.Debug("server begin accept ricmp %s", u.Info())

	startConnectTime := time.Now()
	done := false
	for !c.listener.wg.IsExit() {

		if u.listenersonny.fm.IsConnected() {
			done = true
			break
		}

		u.listenersonny.fm.Update()

		// send icmp
		sendlist := u.listenersonny.fm.GetSendList()
		for e := sendlist.Front(); e != nil; e = e.Next() {
			f := e.Value.(*Frame)
			mb, err := u.listenersonny.fm.MarshalFrame(f)
			if err != nil {
				//loggo.Error("MarshalFrame fail %s", err)
				break
			}
			u.listenersonny.fatherconn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))
			u.send_icmp(u.listenersonny.fatherconn, u.listenersonny.family, icmpModeRaw, mb, u.listenersonny.dstaddr,
				u.id, u.listenersonny.icmpId, int(atomic.LoadInt32(&u.listenersonny.icmpSeq)), u.listenersonny.icmpProto, u.listenersonny.icmpFlag)
		}

		now := time.Now()
		diffclose := now.Sub(startConnectTime)
		if diffclose > time.Millisecond*time.Duration(c.config.ConnectTimeoutMs) {
			//loggo.Debug("can not connect by remote ricmp %s", u.Info())
			break
		}

		time.Sleep(time.Millisecond * 10)
	}

	if !done {
		u.Close()
		return nil
	}

	if c.listener.wg.IsExit() {
		u.Close()
		return nil
	}

	//loggo.Debug("server accept ricmp ok %s", u.Info())

	wg := thread.NewGroup("RicmpConn ListenerSonny"+" "+u.Info(), c.listener.wg, nil)
	u.listenersonny.wg = wg

	wg.Go("RicmpConn updateListenerSonny"+" "+u.Info(), func() error {
		return u.updateListenerSonny()
	})

	// Non-blocking accept enqueue: blocking holds the accept goroutine under backlog.
	if !c.listener.accept.WriteTimeout(u, 100) {
		u.Close()
		return nil
	}

	//loggo.Debug("accept ricmp finish %s", u.Info())

	return nil
}

func (c *RicmpConn) updateListenerSonny() error {
	defer func() {
		c.isclose.Store(true)
		if c.listenersonny != nil && c.listenersonny.listener != nil {
			c.listenersonny.listener.sonny.Delete(c.id)
		}
	}()
	return c.update_ricmp(c.listenersonny.wg, c.listenersonny.fm, c.listenersonny.fatherconn, c.listenersonny.family, icmpModeRaw, c.listenersonny.dstaddr, false,
		0, 0,
		c.id, c.listenersonny.icmpId, &c.listenersonny.icmpSeq, c.listenersonny.icmpProto, c.listenersonny.icmpFlag,
		false)
}

func (c *RicmpConn) updateDialerSonny() error {
	defer func() {
		c.isclose.Store(true)
		if c.dialer != nil && c.dialer.conn != nil {
			c.dialer.conn.Close()
		}
	}()
	return c.update_ricmp(c.dialer.wg, c.dialer.fm, c.dialer.conn, c.dialer.family, c.dialer.mode, c.dialer.serveraddr, true,
		c.dialer.icmpId, int(IcmpMsg_SERVER_SEND_FLAG),
		c.id, c.dialer.icmpId, &c.dialer.icmpSeq, c.dialer.icmpProto, c.dialer.icmpFlag,
		true)
}

func (c *RicmpConn) update_ricmp(wg *thread.Group, fm *FrameMgr, conn *icmp.PacketConn, family int, mode string, dstaddr net.Addr, readconn bool,
	recvCheckEchoId int, recvCheckEchoFlag int, id string, icmpId int, icmpSeq *int32, icmpProto int, icmpFlag IcmpMsg_TYPE, addIcmpSeq bool) error {

	//loggo.Debug("start ricmp conn %s", c.Info())

	const (
		stageOpen      int32 = 0
		stageClose     int32 = 1
		stageCloseWait int32 = 2
	)
	var stage atomic.Int32
	stage.Store(stageOpen)

	if readconn {
		wg.Go("RicmpConn update_ricmp recv"+" "+c.Info(), func() error {
			bytes := make([]byte, c.config.MaxPacketSize)
			for !wg.IsExit() && stage.Load() != stageCloseWait {
				// recv icmp
				conn.SetReadDeadline(time.Now().Add(time.Millisecond * 100))
				n, _, _, id, echoId, _, echoFlag := c.recv_icmp(conn, bytes)
				if n > 0 && id == c.id && echoId == recvCheckEchoId && echoFlag == recvCheckEchoFlag {
					f := &Frame{}
					err := proto.Unmarshal(bytes[0:n], f)
					if err == nil {
						fm.OnRecvFrame(f)
						//loggo.Debug("%s recv frame %d %v", c.Info(), f.Id, f.String())
					} else {
						//loggo.Error("Unmarshal fail from %s %s", c.Info(), err)
					}
				}
			}

			return nil
		})
	}

	reason := ""

	for !wg.IsExit() {

		avctive := fm.Update()

		// send icmp
		sendlist := fm.GetSendList()
		for e := sendlist.Front(); e != nil; e = e.Next() {
			f := e.Value.(*Frame)
			mb, err := fm.MarshalFrame(f)
			if err != nil {
				//loggo.Error("MarshalFrame fail %s", err)
				reason = "MarshalFrame"
				break
			}
			conn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))
			seq := int(atomic.LoadInt32(icmpSeq))
			c.send_icmp(conn, family, mode, mb, dstaddr, id, icmpId, seq, icmpProto, icmpFlag)
			if addIcmpSeq {
				atomic.AddInt32(icmpSeq, 1)
			}
			//loggo.Debug("%s send frame to %s %d %v", c.Info(), dstaddr, f.Id, f.String())
		}

		if reason == "MarshalFrame" {
			break
		}

		// timeout
		if fm.IsHBTimeout() {
			reason = "HBTimeout"
			//loggo.Debug("close inactive conn %s", c.Info())
			break
		}

		if fm.IsRemoteClosed() {
			reason = "RemoteClose"
			//loggo.Debug("closed by remote conn %s", c.Info())
			break
		}

		if !avctive && sendlist.Len() <= 0 {
			time.Sleep(time.Millisecond * 10)
		}
	}

	stage.Store(stageClose)
	fm.Close()
	//loggo.Debug("close ricmp conn fm %s", c.Info())

	startCloseTime := time.Now()
	for !wg.IsExit() {
		now := time.Now()

		fm.Update()

		// send icmp
		sendlist := fm.GetSendList()
		for e := sendlist.Front(); e != nil; e = e.Next() {
			f := e.Value.(*Frame)
			mb, err := fm.MarshalFrame(f)
			if err != nil {
				//loggo.Error("MarshalFrame fail %s", err)
				break
			}
			conn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))
			seq := int(atomic.LoadInt32(icmpSeq))
			c.send_icmp(conn, family, mode, mb, dstaddr, id, icmpId, seq, icmpProto, icmpFlag)
			if addIcmpSeq {
				atomic.AddInt32(icmpSeq, 1)
			}
			//loggo.Debug("%s send frame to %s %d", c.Info(), dstaddr, f.Id)
		}

		diffclose := now.Sub(startCloseTime)
		if diffclose > time.Millisecond*time.Duration(c.config.CloseTimeoutMs) {
			//loggo.Debug("close conn had timeout %s", c.Info())
			break
		}

		remoteclosed := fm.IsRemoteClosed()
		if remoteclosed {
			//loggo.Debug("remote conn had closed %s", c.Info())
			break
		}

		time.Sleep(time.Millisecond * 10)
	}

	stage.Store(stageCloseWait)
	//loggo.Debug("close ricmp conn update %s", c.Info())

	startEndTime := time.Now()
	for !wg.IsExit() {
		now := time.Now()

		diffclose := now.Sub(startEndTime)
		if diffclose > time.Millisecond*time.Duration(c.config.CloseWaitTimeoutMs) {
			//loggo.Debug("close wait conn had timeout %s", c.Info())
			break
		}

		if fm.GetRecvBufferSize() <= 0 {
			//loggo.Debug("conn had no data %s", c.Info())
			break
		}

		time.Sleep(time.Millisecond * 10)
	}

	//loggo.Debug("close ricmp conn %s", c.Info())

	return errors.New("closed " + reason)
}

func (c *RicmpConn) send_icmp(conn *icmp.PacketConn, family int, mode string, data []byte, dst net.Addr, id string, icmpId int, icmpSeq int, icmpProto int, icmpFlag IcmpMsg_TYPE) {

	m := &IcmpMsg{
		Id:    id,
		Data:  data,
		Magic: IcmpMsg_MAGIC,
		Flag:  icmpFlag,
	}

	mb, err := proto.Marshal(m)
	if err != nil {
		//loggo.Error("sendICMP Marshal MyMsg error %s %s", c.Info(), err)
		return
	}

	body := &icmp.Echo{
		ID:   icmpId,
		Seq:  icmpSeq,
		Data: mb,
	}

	msg := &icmp.Message{
		Type: icmpEchoType(icmpProto, family),
		Code: 0,
		Body: body,
	}

	// Checksum handling differs by family/mode:
	//   - ICMPv4 raw sockets: the kernel fills the checksum on output.
	//   - datagram sockets (both families): the kernel computes it.
	//   - ICMPv6 RAW sockets: the kernel does NOT compute the checksum, so a
	//     zero checksum packet is dropped by the peer's icmpv6 input before
	//     it reaches unprivileged ping sockets (raw receivers don't verify,
	//     which is why raw-to-raw worked). Compute it here over the IPv6
	//     pseudo-header using the source routed toward the destination.
	var psh []byte
	if family == icmpFamilyV6 && mode == icmpModeRaw {
		if ipa, ok := dst.(*net.IPAddr); ok && ipa != nil {
			if src := c.sourceIPTowards(ipa.IP, ipa.Zone); src != nil {
				psh = icmp.IPv6PseudoHeader(src, ipa.IP)
			}
		}
	}
	bytes, err := msg.Marshal(psh)
	if err != nil {
		//loggo.Error("sendICMP Marshal error %s %s", c.Info(), err)
		return
	}

	// Datagram endpoints require *net.UDPAddr destinations; raw endpoints
	// use *net.IPAddr.
	if ipa, ok := dst.(*net.IPAddr); ok && mode == icmpModeDgram {
		dst = &net.UDPAddr{IP: ipa.IP, Zone: ipa.Zone}
	}
	conn.WriteTo(bytes, dst)
}

func (c *RicmpConn) recv_icmp(conn *icmp.PacketConn, bytes []byte) (int, net.Addr, error, string, int, int, int) {
	n, srcaddr, err := conn.ReadFrom(bytes)
	if err != nil {
		return 0, srcaddr, err, "", 0, 0, 0
	}

	payload, id, echoId, echoSeq, flag, err := decodeIcmpPacketPayload(bytes[:n])
	if err != nil {
		return 0, srcaddr, err, "", 0, 0, 0
	}
	copied := copy(bytes, payload)
	return copied, srcaddr, nil, id, echoId, echoSeq, flag
}

// decodeIcmpPacketPayload parses an ICMP echo message buffer (header + IcmpMsg protobuf).
// packet must start at the ICMP header (type/code/checksum/id/seq).
func decodeIcmpPacketPayload(packet []byte) (payload []byte, id string, echoId, echoSeq, flag int, err error) {
	// ICMP echo header is 8 bytes (type/code/checksum/id/seq).
	if len(packet) < 8 {
		return nil, "", 0, 0, 0, errors.New("icmp packet too short")
	}

	echoId = int(binary.BigEndian.Uint16(packet[4:6]))
	echoSeq = int(binary.BigEndian.Uint16(packet[6:8]))

	my := &IcmpMsg{}
	if err = proto.Unmarshal(packet[8:], my); err != nil {
		return nil, "", 0, 0, 0, err
	}
	if my.Magic != IcmpMsg_MAGIC {
		return nil, "", 0, 0, 0, errors.New("magic error")
	}
	return my.Data, my.Id, echoId, echoSeq, int(my.Flag), nil
}
