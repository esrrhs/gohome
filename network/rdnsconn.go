package network

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/esrrhs/gohome/common"
	"github.com/esrrhs/gohome/thread"
	"github.com/miekg/dns"
	"google.golang.org/protobuf/proto"
)

/*
RdnsConn implements a reliable DNS-tunnel Conn powered by FrameMgr ARQ.

Uplink frames are encoded into DNS query names under a configurable base
domain; downlink frames ride in TXT or NULL responses. DNS is strictly
request/response, so the client polls continuously (empty queries when idle).

Optional PSK enables AES-256-GCM for frame payloads. Cleartext DNS metadata
(QNAME labels, timing, sizes, server address) remains visible to on-path
observers — this is encrypt+bypass, not steganography.
*/

// RdnsConfig configures the rdns tunnel. Domain is required before Dial/Listen.
type RdnsConfig struct {
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

	// Domain is the base tunnel domain (e.g. "tunnel.example.com"). Required.
	Domain string
	// DNSAddr is an optional default DNS server for Dial when dst is empty.
	DNSAddr string
	// Upstream is reserved for future recursive forwarding of non-tunnel queries.
	Upstream string
	// PSK enables AES-GCM payload encryption when non-empty.
	PSK string
	// QueryType is "TXT" (default) or "NULL".
	QueryType string
	// PollIntervalMs is the client idle poll interval (default 50).
	PollIntervalMs int
	// EDNSUdpSize sets EDNS0 UDP payload size (default 1232).
	EDNSUdpSize uint16
}

// DefaultRdnsConfig returns DNS-friendly FrameMgr defaults (tiny CutSize).
func DefaultRdnsConfig() *RdnsConfig {
	return &RdnsConfig{
		MaxPacketSize:      1200,
		CutSize:            80,
		MaxId:              100000,
		BufferSize:         256 * 1024,
		MaxWin:             2000,
		ResendTimems:       800,
		Compress:           0,
		Stat:               0,
		ConnectTimeoutMs:   15000,
		CloseTimeoutMs:     5000,
		CloseWaitTimeoutMs: 5000,
		AcceptChanLen:      128,
		Congestion:         "bb",
		QueryType:          "TXT",
		PollIntervalMs:     50,
		EDNSUdpSize:        1232,
	}
}

// RdnsConn is the Conn implementation for proto "rdns".
type RdnsConn struct {
	id            string
	session       string
	config        *RdnsConfig
	dialer        *rdnsConnDialer
	listenersonny *rdnsConnListenerSonny
	listener      *rdnsConnListener
	isclose       atomic.Bool
	closelock     sync.Mutex
	cfgMu         sync.RWMutex
}

type rdnsConnDialer struct {
	dnsAddr string
	conn    *net.UDPConn
	fm      *FrameMgr
	wg      *thread.Group
	seq     uint32
}

type rdnsConnListenerSonny struct {
	session    string
	remote     *net.UDPAddr
	fatherconn *net.UDPConn
	listener   *rdnsConnListener
	fm         *FrameMgr
	wg         *thread.Group
	pending    [][]byte // downlink frames waiting to be carried in next responses
	pendMu     sync.Mutex
}

type rdnsConnListener struct {
	listenerconn *net.UDPConn
	wg           *thread.Group
	sonny        sync.Map // session -> *RdnsConn
	accept       *common.Channel
}

func (c *RdnsConn) Name() string {
	return "rdns"
}

func (c *RdnsConn) Read(p []byte) (n int, err error) {
	c.checkConfig()

	if c.isclose.Load() {
		return 0, errors.New("read closed conn")
	}
	if len(p) <= 0 {
		return 0, errors.New("read empty buffer")
	}

	fm, wg, err := c.fmAndWg()
	if err != nil {
		return 0, err
	}
	if c.listener != nil {
		return 0, errors.New("listener can not be read")
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

func (c *RdnsConn) Write(p []byte) (n int, err error) {
	c.checkConfig()

	if c.isclose.Load() {
		return 0, errors.New("write closed conn")
	}
	if len(p) <= 0 {
		return 0, errors.New("write empty data")
	}

	fm, wg, err := c.fmAndWg()
	if err != nil {
		return 0, err
	}
	if c.listener != nil {
		return 0, errors.New("listener can not be write")
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

func (c *RdnsConn) fmAndWg() (*FrameMgr, *thread.Group, error) {
	if c.dialer != nil {
		return c.dialer.fm, c.dialer.wg, nil
	}
	if c.listenersonny != nil {
		return c.listenersonny.fm, c.listenersonny.wg, nil
	}
	if c.listener != nil {
		return nil, nil, nil
	}
	return nil, nil, errors.New("empty conn")
}

func (c *RdnsConn) Close() error {
	c.checkConfig()
	if c.isclose.Load() {
		return nil
	}
	c.closelock.Lock()
	defer c.closelock.Unlock()
	if c.isclose.Load() {
		return nil
	}
	c.isclose.Store(true)

	if c.dialer != nil {
		if c.dialer.wg != nil {
			c.dialer.wg.Stop()
			_ = c.dialer.wg.Join()
		}
		if c.dialer.conn != nil {
			c.dialer.conn.Close()
		}
	} else if c.listener != nil {
		if c.listener.accept != nil {
			c.listener.accept.Close()
		}
		if c.listener.listenerconn != nil {
			c.listener.listenerconn.Close()
		}
		c.listener.sonny.Range(func(key, value interface{}) bool {
			u := value.(*RdnsConn)
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
		if c.listenersonny.listener != nil && c.session != "" {
			c.listenersonny.listener.sonny.Delete(c.session)
		}
	}
	return nil
}

func (c *RdnsConn) Info() string {
	c.checkConfig()
	domain := rdnsNormalizeDomain(c.config.Domain)
	if c.dialer != nil {
		return c.dialer.conn.LocalAddr().String() + "<--rdns " + c.session + " " + domain + "-->" + c.dialer.dnsAddr
	}
	if c.listener != nil {
		return "rdns--" + c.listener.listenerconn.LocalAddr().String() + " domain=" + domain
	}
	if c.listenersonny != nil {
		remote := ""
		if c.listenersonny.remote != nil {
			remote = c.listenersonny.remote.String()
		}
		return c.listenersonny.fatherconn.LocalAddr().String() + "<--rdns " + c.session + "-->" + remote
	}
	return "empty rdns conn"
}

func (c *RdnsConn) checkConfig() {
	c.cfgMu.Lock()
	if c.config == nil {
		c.config = DefaultRdnsConfig()
	}
	c.cfgMu.Unlock()
}

// SetConfig replaces the connection config. Call before Dial/Listen.
func (c *RdnsConn) SetConfig(config *RdnsConfig) {
	c.cfgMu.Lock()
	c.config = config
	c.cfgMu.Unlock()
}

// GetConfig returns the current config, initializing defaults if needed.
func (c *RdnsConn) GetConfig() *RdnsConfig {
	c.cfgMu.Lock()
	if c.config == nil {
		c.config = DefaultRdnsConfig()
	}
	cfg := c.config
	c.cfgMu.Unlock()
	return cfg
}

func (c *RdnsConn) requireDomain() (string, error) {
	c.checkConfig()
	d := rdnsNormalizeDomain(c.config.Domain)
	if d == "" {
		return "", errors.New("rdns: Domain required in SetConfig (e.g. tunnel.example.com)")
	}
	return d, nil
}

func (c *RdnsConn) newFrameMgr(debugSuffix string) *FrameMgr {
	fm := NewFrameMgr(c.config.CutSize, c.config.MaxId, c.config.BufferSize, c.config.MaxWin, c.config.ResendTimems, c.config.Compress, c.config.Stat)
	fm.SetDebugid(c.id + debugSuffix)
	if c.config.Congestion == "bb" {
		fm.SetCongestion(&BBCongestion{})
	}
	return fm
}

func (c *RdnsConn) Dial(dst string) (Conn, error) {
	c.checkConfig()
	domain, err := c.requireDomain()
	if err != nil {
		return nil, err
	}
	dnsAddr := strings.TrimSpace(dst)
	if dnsAddr == "" {
		dnsAddr = strings.TrimSpace(c.config.DNSAddr)
	}
	if dnsAddr == "" {
		return nil, errors.New("rdns: Dial needs dns server address (dst) or config.DNSAddr")
	}
	raddr, err := net.ResolveUDPAddr("udp", dnsAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, err
	}

	id := common.Guid()
	session := rdnsSessionID(id)
	fm := c.newFrameMgr("-dialer")
	// Clamp CutSize so uplink frames fit in a QNAME after seal+base32.
	maxUp := rdnsMaxUplinkPayload(domain, session)
	if c.config.PSK != "" {
		maxUp -= rdnsGCMNonceSize + 16 // nonce + GCM tag
	}
	maxUp -= 2 // length prefix in pack
	if maxUp < 16 {
		conn.Close()
		return nil, fmt.Errorf("rdns: domain too long for tunnel payloads (max uplink ~%d)", maxUp)
	}
	if c.config.CutSize > maxUp {
		// Recreate FrameMgr with clamped cut size.
		cut := maxUp
		fm = NewFrameMgr(cut, c.config.MaxId, c.config.BufferSize, c.config.MaxWin, c.config.ResendTimems, c.config.Compress, c.config.Stat)
		fm.SetDebugid(id + "-dialer")
		if c.config.Congestion == "bb" {
			fm.SetCongestion(&BBCongestion{})
		}
	}

	dialer := &rdnsConnDialer{dnsAddr: raddr.String(), conn: conn, fm: fm}
	u := &RdnsConn{id: id, session: session, config: c.config, dialer: dialer}

	u.dialer.fm.Connect()

	startConnectTime := time.Now()
	for {
		if u.dialer.fm.IsConnected() {
			break
		}
		u.dialer.fm.Update()
		if err := u.clientExchange(); err != nil {
			// transient UDP errors are fine during handshake
		}
		if c.isclose.Load() {
			break
		}
		if time.Since(startConnectTime) > time.Millisecond*time.Duration(c.config.ConnectTimeoutMs) {
			break
		}
		time.Sleep(time.Millisecond * 20)
	}

	if c.isclose.Load() || u.isclose.Load() {
		u.Close()
		return nil, errors.New("closed conn")
	}
	if !u.dialer.fm.IsConnected() {
		u.Close()
		return nil, errors.New("connect timeout")
	}

	wg := thread.NewGroup("RdnsConn Dialer"+" "+u.Info(), nil, nil)
	u.dialer.wg = wg
	wg.Go("RdnsConn updateDialerSonny"+" "+u.Info(), func() error {
		return u.updateDialerSonny()
	})
	return u, nil
}

func (c *RdnsConn) Listen(dst string) (Conn, error) {
	c.checkConfig()
	if _, err := c.requireDomain(); err != nil {
		return nil, err
	}
	bind := strings.TrimSpace(dst)
	if bind == "" {
		bind = strings.TrimSpace(c.config.DNSAddr)
	}
	if bind == "" {
		bind = ":53"
	}
	addr, err := net.ResolveUDPAddr("udp", bind)
	if err != nil {
		return nil, err
	}
	listenerconn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	ch := common.NewChannel(c.config.AcceptChanLen)
	wg := thread.NewGroup("RdnsConn Listen"+" "+bind, nil, nil)
	listener := &rdnsConnListener{
		listenerconn: listenerconn,
		wg:           wg,
		accept:       ch,
	}
	u := &RdnsConn{id: common.Guid(), config: c.config, listener: listener}
	wg.Go("RdnsConn loopListenerRecv"+" "+bind, func() error {
		return u.loopListenerRecv()
	})
	return u, nil
}

func (c *RdnsConn) Accept() (Conn, error) {
	c.checkConfig()
	if c.listener == nil || c.listener.wg == nil {
		return nil, errors.New("not listen")
	}
	for !c.listener.wg.IsExit() {
		s := <-c.listener.accept.Ch()
		if s == nil {
			break
		}
		sonny := s.(*RdnsConn)
		_, ok := c.listener.sonny.Load(sonny.session)
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

func (c *RdnsConn) clientExchange() error {
	cfg := c.config
	domain := rdnsNormalizeDomain(cfg.Domain)
	fm := c.dialer.fm

	// One FrameMgr packet per DNS query keeps QNAMEs under the 255-byte limit.
	var frames [][]byte
	sendlist := fm.GetSendList()
	if e := sendlist.Front(); e != nil {
		f := e.Value.(*Frame)
		if mb, err := fm.MarshalFrame(f); err == nil {
			frames = append(frames, mb)
		}
		// Re-queue leftovers immediately so the next poll can carry them
		// without waiting for the ARQ resend timer.
		for e = e.Next(); e != nil; e = e.Next() {
			fm.sendFrame(e.Value.(*Frame))
		}
	}
	blob := rdnsPackFrames(frames)
	sealed, err := rdnsSeal(cfg.PSK, blob)
	if err != nil {
		return err
	}
	qname, err := rdnsBuildQName(domain, c.session, sealed)
	if err != nil {
		// Fall back to an empty poll so the connection stays alive.
		sealed, _ = rdnsSeal(cfg.PSK, nil)
		qname, err = rdnsBuildQName(domain, c.session, sealed)
		if err != nil {
			return err
		}
	}

	msg := new(dns.Msg)
	qtype := rdnsQueryType(cfg.QueryType)
	msg.SetQuestion(qname, qtype)
	msg.RecursionDesired = false
	udpSize := cfg.EDNSUdpSize
	if udpSize == 0 {
		udpSize = 1232
	}
	msg.SetEdns0(udpSize, false)
	// Mix a seq into the DNS id for basic demux of late replies.
	seq := atomic.AddUint32(&c.dialer.seq, 1)
	msg.Id = uint16(seq)

	wire, err := msg.Pack()
	if err != nil {
		return err
	}
	_ = c.dialer.conn.SetWriteDeadline(time.Now().Add(time.Millisecond * 200))
	if _, err := c.dialer.conn.Write(wire); err != nil {
		return err
	}

	buf := make([]byte, int(udpSize)+512)
	_ = c.dialer.conn.SetReadDeadline(time.Now().Add(time.Millisecond * 300))
	n, err := c.dialer.conn.Read(buf)
	if err != nil || n <= 0 {
		return err
	}
	resp := new(dns.Msg)
	if err := resp.Unpack(buf[:n]); err != nil {
		return err
	}
	down, err := rdnsExtractAnswer(resp, cfg.QueryType)
	if err != nil || len(down) == 0 {
		return err
	}
	plain, err := rdnsOpen(cfg.PSK, down)
	if err != nil {
		return err
	}
	parts, err := rdnsUnpackFrames(plain)
	if err != nil {
		return err
	}
	for _, p := range parts {
		f := &Frame{}
		if err := proto.Unmarshal(p, f); err == nil {
			fm.OnRecvFrame(f)
		}
	}
	return nil
}

func rdnsExtractAnswer(resp *dns.Msg, queryType string) ([]byte, error) {
	if resp == nil {
		return nil, errors.New("rdns: nil response")
	}
	wantTXT := rdnsIsTXT(queryType)
	for _, rr := range resp.Answer {
		if wantTXT {
			if txt, ok := rr.(*dns.TXT); ok {
				return rdnsDecodePayload(strings.Join(txt.Txt, ""))
			}
		} else if nrr, ok := rr.(*dns.NULL); ok {
			return []byte(nrr.Data), nil
		}
	}
	// Interop fallback: accept the other RR type if present.
	for _, rr := range resp.Answer {
		if txt, ok := rr.(*dns.TXT); ok {
			return rdnsDecodePayload(strings.Join(txt.Txt, ""))
		}
		if nrr, ok := rr.(*dns.NULL); ok {
			return []byte(nrr.Data), nil
		}
	}
	return nil, nil
}

func (c *RdnsConn) updateDialerSonny() error {
	defer func() {
		c.isclose.Store(true)
		if c.dialer != nil && c.dialer.conn != nil {
			c.dialer.conn.Close()
		}
	}()
	return c.update_rdns_client()
}

func (c *RdnsConn) update_rdns_client() error {
	fm := c.dialer.fm
	wg := c.dialer.wg
	reason := ""
	poll := time.Duration(c.config.PollIntervalMs) * time.Millisecond
	if poll <= 0 {
		poll = 50 * time.Millisecond
	}

	for !wg.IsExit() {
		active := fm.Update()
		_ = c.clientExchange()

		if fm.IsHBTimeout() {
			reason = "HBTimeout"
			break
		}
		if fm.IsRemoteClosed() {
			reason = "RemoteClose"
			break
		}
		if !active {
			time.Sleep(poll)
		} else {
			time.Sleep(time.Millisecond * 10)
		}
	}

	fm.Close()
	startCloseTime := time.Now()
	for !wg.IsExit() {
		fm.Update()
		_ = c.clientExchange()
		if time.Since(startCloseTime) > time.Millisecond*time.Duration(c.config.CloseTimeoutMs) {
			break
		}
		if fm.IsRemoteClosed() {
			break
		}
		time.Sleep(time.Millisecond * 20)
	}

	startEndTime := time.Now()
	for !wg.IsExit() {
		if time.Since(startEndTime) > time.Millisecond*time.Duration(c.config.CloseWaitTimeoutMs) {
			break
		}
		if fm.GetRecvBufferSize() <= 0 {
			break
		}
		time.Sleep(time.Millisecond * 10)
	}
	return errors.New("closed " + reason)
}

func (c *RdnsConn) loopListenerRecv() error {
	c.checkConfig()
	domain := rdnsNormalizeDomain(c.config.Domain)
	udpSize := c.config.EDNSUdpSize
	if udpSize == 0 {
		udpSize = 1232
	}
	buf := make([]byte, int(udpSize)+512)

	for !c.listener.wg.IsExit() {
		_ = c.listener.listenerconn.SetReadDeadline(time.Now().Add(time.Millisecond * 100))
		n, srcaddr, err := c.listener.listenerconn.ReadFromUDP(buf)
		if err != nil || n <= 0 {
			continue
		}
		req := new(dns.Msg)
		if err := req.Unpack(buf[:n]); err != nil || len(req.Question) == 0 {
			continue
		}
		q := req.Question[0]
		session, uplink, ok, perr := rdnsParseQName(q.Name, domain)
		if perr != nil || !ok {
			// Not our tunnel query — REFUSED.
			c.writeDNSError(req, srcaddr, dns.RcodeRefused)
			continue
		}

		v, loaded := c.listener.sonny.Load(session)
		var u *RdnsConn
		if !loaded {
			id := common.Guid()
			fm := NewFrameMgr(c.config.CutSize, c.config.MaxId, c.config.BufferSize, c.config.MaxWin, c.config.ResendTimems, c.config.Compress, c.config.Stat)
			fm.SetDebugid(id + "-listener")
			if c.config.Congestion == "bb" {
				fm.SetCongestion(&BBCongestion{})
			}
			sonny := &rdnsConnListenerSonny{
				session:    session,
				remote:     srcaddr,
				fatherconn: c.listener.listenerconn,
				listener:   c.listener,
				fm:         fm,
			}
			u = &RdnsConn{id: id, session: session, config: c.config, listenersonny: sonny}
			c.listener.sonny.Store(session, u)

			if len(uplink) > 0 {
				c.feedUplink(u, uplink)
			}
			c.listener.wg.Go("RdnsConn accept"+" "+u.Info(), func() error {
				return c.accept(u)
			})
		} else {
			u = v.(*RdnsConn)
			if u.isclose.Load() {
				c.listener.sonny.Delete(session)
				c.writeDNSError(req, srcaddr, dns.RcodeServerFailure)
				continue
			}
			u.listenersonny.remote = srcaddr
			if len(uplink) > 0 {
				c.feedUplink(u, uplink)
			}
		}

		// Build downlink from FrameMgr send queue / pending.
		down := c.collectDownlink(u)
		resp := c.buildDNSResponse(req, down)
		wire, err := resp.Pack()
		if err != nil {
			continue
		}
		_ = c.listener.listenerconn.SetWriteDeadline(time.Now().Add(time.Millisecond * 200))
		_, _ = c.listener.listenerconn.WriteToUDP(wire, srcaddr)

		c.listener.sonny.Range(func(key, value interface{}) bool {
			su := value.(*RdnsConn)
			if su.isclose.Load() {
				c.listener.sonny.Delete(key)
			}
			return true
		})
	}
	return nil
}

func (c *RdnsConn) feedUplink(u *RdnsConn, sealed []byte) {
	plain, err := rdnsOpen(c.config.PSK, sealed)
	if err != nil {
		return
	}
	parts, err := rdnsUnpackFrames(plain)
	if err != nil {
		return
	}
	for _, p := range parts {
		f := &Frame{}
		if err := proto.Unmarshal(p, f); err == nil {
			u.listenersonny.fm.OnRecvFrame(f)
		}
	}
}

func (c *RdnsConn) collectDownlink(u *RdnsConn) []byte {
	fm := u.listenersonny.fm
	fm.Update()

	var frames [][]byte
	u.listenersonny.pendMu.Lock()
	if len(u.listenersonny.pending) > 0 {
		frames = append(frames, u.listenersonny.pending...)
		u.listenersonny.pending = nil
	}
	u.listenersonny.pendMu.Unlock()

	sendlist := fm.GetSendList()
	for e := sendlist.Front(); e != nil; e = e.Next() {
		f := e.Value.(*Frame)
		mb, err := fm.MarshalFrame(f)
		if err != nil {
			continue
		}
		frames = append(frames, mb)
		if len(frames) >= 8 {
			break
		}
	}
	blob := rdnsPackFrames(frames)
	sealed, err := rdnsSeal(c.config.PSK, blob)
	if err != nil {
		return nil
	}
	// Keep response reasonably small for UDP DNS.
	maxDown := 1000
	if len(sealed) > maxDown && len(frames) > 1 {
		// Put overflow back as pending for next poll.
		keep := frames[:1]
		rest := frames[1:]
		u.listenersonny.pendMu.Lock()
		u.listenersonny.pending = append(rest, u.listenersonny.pending...)
		u.listenersonny.pendMu.Unlock()
		blob = rdnsPackFrames(keep)
		sealed, _ = rdnsSeal(c.config.PSK, blob)
	}
	return sealed
}

func (c *RdnsConn) buildDNSResponse(req *dns.Msg, payload []byte) *dns.Msg {
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Authoritative = true
	if len(req.Question) == 0 {
		return resp
	}
	q := req.Question[0]
	name := q.Name
	useTXT := q.Qtype == dns.TypeTXT || (q.Qtype != dns.TypeNULL && rdnsIsTXT(c.config.QueryType))
	if q.Qtype == dns.TypeNULL {
		useTXT = false
	}
	if useTXT {
		enc := ""
		if len(payload) > 0 {
			enc = strings.ToLower(rdnsB32.EncodeToString(payload))
		} else {
			enc = rdnsEmptyLabel
		}
		// Split into 255-char TXT strings.
		var chunks []string
		for i := 0; i < len(enc); i += 255 {
			end := i + 255
			if end > len(enc) {
				end = len(enc)
			}
			chunks = append(chunks, enc[i:end])
		}
		resp.Answer = append(resp.Answer, &dns.TXT{
			Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 0},
			Txt: chunks,
		})
	} else {
		data := string(payload)
		resp.Answer = append(resp.Answer, &dns.NULL{
			Hdr:  dns.RR_Header{Name: name, Rrtype: dns.TypeNULL, Class: dns.ClassINET, Ttl: 0},
			Data: data,
		})
	}
	return resp
}

func (c *RdnsConn) writeDNSError(req *dns.Msg, addr *net.UDPAddr, rcode int) {
	resp := new(dns.Msg)
	resp.SetRcode(req, rcode)
	wire, err := resp.Pack()
	if err != nil {
		return
	}
	_ = c.listener.listenerconn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))
	_, _ = c.listener.listenerconn.WriteToUDP(wire, addr)
}

func (c *RdnsConn) accept(u *RdnsConn) error {
	startConnectTime := time.Now()
	done := false
	for !c.listener.wg.IsExit() {
		if u.listenersonny.fm.IsConnected() {
			done = true
			break
		}
		u.listenersonny.fm.Update()
		// Downlink frames produced here wait in GetSendList until the next
		// client poll picks them up via collectDownlink.
		if time.Since(startConnectTime) > time.Millisecond*time.Duration(c.config.ConnectTimeoutMs) {
			break
		}
		time.Sleep(time.Millisecond * 20)
	}
	if !done || c.listener.wg.IsExit() {
		u.Close()
		return nil
	}

	wg := thread.NewGroup("RdnsConn ListenerSonny"+" "+u.Info(), c.listener.wg, nil)
	u.listenersonny.wg = wg
	wg.Go("RdnsConn updateListenerSonny"+" "+u.Info(), func() error {
		return u.updateListenerSonny()
	})
	if !c.listener.accept.WriteTimeout(u, 100) {
		u.Close()
		return nil
	}
	return nil
}

func (c *RdnsConn) updateListenerSonny() error {
	defer func() {
		c.isclose.Store(true)
		if c.listenersonny != nil && c.listenersonny.listener != nil && c.session != "" {
			c.listenersonny.listener.sonny.Delete(c.session)
		}
	}()
	fm := c.listenersonny.fm
	wg := c.listenersonny.wg
	reason := ""
	for !wg.IsExit() {
		active := fm.Update()
		if fm.IsHBTimeout() {
			reason = "HBTimeout"
			break
		}
		if fm.IsRemoteClosed() {
			reason = "RemoteClose"
			break
		}
		if !active {
			time.Sleep(time.Millisecond * 50)
		} else {
			time.Sleep(time.Millisecond * 10)
		}
	}
	fm.Close()
	startCloseTime := time.Now()
	for !wg.IsExit() {
		fm.Update()
		if time.Since(startCloseTime) > time.Millisecond*time.Duration(c.config.CloseTimeoutMs) {
			break
		}
		if fm.IsRemoteClosed() {
			break
		}
		time.Sleep(time.Millisecond * 20)
	}
	startEndTime := time.Now()
	for !wg.IsExit() {
		if time.Since(startEndTime) > time.Millisecond*time.Duration(c.config.CloseWaitTimeoutMs) {
			break
		}
		if fm.GetRecvBufferSize() <= 0 {
			break
		}
		time.Sleep(time.Millisecond * 10)
	}
	return errors.New("closed " + reason)
}
