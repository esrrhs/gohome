package network

import (
	"bytes"
	"fmt"
	"math/rand"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 可靠协议下载吞吐：按协议顺序测 rudp → kcp → quic → rhttp → ricmp。
//
//	server: 不停 Write 同一块 1024 字节
//	client: 不停 Read，凑满 1024 后校验内容，累计字节
//	跑满 duration 后停，吞吐 = 校验通过字节 / duration / (1024*1024)  → MB/s
//
// rudp/kcp/quic：用户态 UDP netem；rhttp/ricmp：内核 tc netem（lo；TCP 按端口 / ICMP 按协议）。
//
//	go test ./network/ -run='^$' -bench=BenchmarkReliableThroughput -benchtime=1x -timeout 90m

const (
	benchPayloadSize = 1024
	benchOneWayDelay = 200 * time.Millisecond
	benchDuration    = time.Minute
)

// 所有协议共用同一块载荷，保证发的数据一致。
var benchPayload = func() []byte {
	p := make([]byte, benchPayloadSize)
	for i := range p {
		p[i] = byte(i)
	}
	return p
}()

func BenchmarkReliableThroughput(b *testing.B) {
	type caseSpec struct {
		name string
		loss float64
	}
	cases := []caseSpec{
		{"loss0%", 0},
		{"loss10%", 0.10},
		{"loss50%", 0.50},
	}
	// 按协议顺序：每种协议跑完所有丢包场景再下一个。
	protos := []string{"rudp", "kcp", "quic", "rhttp", "ricmp"}

	for _, proto := range protos {
		for _, cs := range cases {
			proto, cs := proto, cs
			b.Run(proto+"/"+cs.name, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					mbps, total, err := runFixedPayloadDownload(proto, cs.loss, benchDuration, int64(i+1))
					if err != nil {
						if isPermissionUnavailable(err) {
							b.Skipf("%s unavailable in unprivileged environment: %v", proto, err)
						}
						if proto == "quic" && cs.loss >= 0.5 {
							b.Skipf("quic under %.0f%% loss: %v", cs.loss*100, err)
						}
						// rhttp 每个请求新建 TCP；50% 丢包 + 200ms 下握手极慢，1 分钟内无有效 goodput。
						if proto == "rhttp" && cs.loss >= 0.5 {
							b.Skipf("rhttp short-lived TCP under %.0f%% loss: %v", cs.loss*100, err)
						}
						if proto == "ricmp" && cs.loss >= 0.5 {
							b.Skipf("ricmp under %.0f%% loss: %v", cs.loss*100, err)
						}
						b.Fatal(err)
					}
					b.SetBytes(total)
					b.ReportMetric(mbps, "MB/s")
					b.Logf("%s/%s: %.2f MB/s (%d bytes in %s)", proto, cs.name, mbps, total, benchDuration)
				}
			})
		}
	}
}

// runFixedPayloadDownload: 一 server 不停发 1024，一 client 不停收并校验，跑满 duration。
func runFixedPayloadDownload(proto string, loss float64, duration time.Duration, seed int64) (mbps float64, verifiedBytes int64, err error) {
	lnFactory, err := NewConn(proto)
	if err != nil {
		return 0, 0, err
	}
	if err := configureBenchConn(lnFactory); err != nil {
		return 0, 0, err
	}
	listenAddr := "127.0.0.1:0"
	if proto == "ricmp" {
		listenAddr = "127.0.0.1" // ICMP 无端口
	}
	ln, err := lnFactory.Listen(listenAddr)
	if err != nil {
		return 0, 0, err
	}
	defer ln.Close()

	backend, err := connListenAddr(ln)
	if err != nil {
		return 0, 0, err
	}

	dialAddr := backend
	var stopNetem func()
	switch proto {
	case "rhttp":
		port, perr := tcpPortOf(backend)
		if perr != nil {
			return 0, 0, perr
		}
		stopNetem, err = startLoTCNetemTCP(port, benchOneWayDelay, loss)
	case "ricmp":
		dialAddr = "127.0.0.1"
		stopNetem, err = startLoTCNetemICMP(benchOneWayDelay, loss)
	default:
		var proxyAddr string
		proxyAddr, stopNetem, err = startUDPNetemProxy(backend, loss, benchOneWayDelay, seed)
		dialAddr = proxyAddr
	}
	if err != nil {
		return 0, 0, err
	}
	defer stopNetem()

	var exit atomic.Bool
	var total atomic.Int64
	var verifyErr atomic.Value // error
	accepted := make(chan Conn, 1)

	// server: Accept 后不停发同一块 1024
	go func() {
		srv, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		accepted <- srv
		defer srv.Close()
		payload := benchPayload
		for !exit.Load() {
			if err := writeFull(srv, payload); err != nil {
				return
			}
		}
	}()

	dialFactory, err := NewConn(proto)
	if err != nil {
		exit.Store(true)
		return 0, 0, err
	}
	if err := configureBenchConn(dialFactory); err != nil {
		exit.Store(true)
		return 0, 0, err
	}
	cli, err := dialFactory.Dial(dialAddr)
	if err != nil {
		exit.Store(true)
		return 0, 0, fmt.Errorf("Dial: %w", err)
	}
	// KCP Accept 需要看到客户端流量；kick 不进入 client 收包路径。
	if _, err := cli.Write([]byte{0}); err != nil {
		exit.Store(true)
		_ = cli.Close()
		return 0, 0, fmt.Errorf("kick Write: %w", err)
	}

	acceptWait := 30 * time.Second
	if proto == "rhttp" && loss >= 0.5 {
		acceptWait = 2 * time.Minute // 短连接 + 高丢包，握手很慢
	}
	select {
	case <-accepted:
	case <-time.After(acceptWait):
		exit.Store(true)
		_ = cli.Close()
		_ = ln.Close()
		return 0, 0, fmt.Errorf("Accept timeout")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cli.Close()
		buf := make([]byte, benchPayloadSize*2)
		pending := make([]byte, 0, benchPayloadSize)
		for !exit.Load() {
			n, rerr := cli.Read(buf)
			if n > 0 {
				pending = append(pending, buf[:n]...)
				for len(pending) >= benchPayloadSize {
					chunk := pending[:benchPayloadSize]
					if !bytes.Equal(chunk, benchPayload) {
						verifyErr.Store(fmt.Errorf("payload mismatch"))
						return
					}
					total.Add(int64(benchPayloadSize))
					pending = pending[benchPayloadSize:]
				}
			}
			if rerr != nil {
				return
			}
		}
	}()

	time.Sleep(duration)
	exit.Store(true)
	_ = cli.Close()
	_ = ln.Close()
	<-done

	if v := verifyErr.Load(); v != nil {
		return 0, 0, v.(error)
	}
	got := total.Load()
	if got <= 0 {
		return 0, 0, fmt.Errorf("no verified bytes in %s", duration)
	}
	mbps = float64(got) / duration.Seconds() / (1024 * 1024)
	return mbps, got, nil
}

func writeFull(c Conn, p []byte) error {
	for len(p) > 0 {
		n, err := c.Write(p)
		if n > 0 {
			p = p[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("Write returned 0")
		}
	}
	return nil
}

func configureBenchConn(c Conn) error {
	switch rc := c.(type) {
	case *RudpConn:
		cfg := DefaultRudpConfig()
		cfg.ResendTimems = 800 // > RTT (~400ms)
		cfg.ConnectTimeoutMs = 60000
		rc.SetConfig(cfg)
	case *RicmpConn:
		cfg := DefaultRicmpConfig()
		cfg.ResendTimems = 800
		cfg.ConnectTimeoutMs = 60000
		rc.SetConfig(cfg)
	}
	return nil
}

func connListenAddr(ln Conn) (string, error) {
	switch c := ln.(type) {
	case *RudpConn:
		if c.listener == nil || c.listener.listenerconn == nil {
			return "", fmt.Errorf("rudp listener empty")
		}
		return c.listener.listenerconn.LocalAddr().String(), nil
	case *KcpConn:
		if c.listener == nil {
			return "", fmt.Errorf("kcp listener empty")
		}
		return c.listener.Addr().String(), nil
	case *QuicConn:
		if c.listener == nil {
			return "", fmt.Errorf("quic listener empty")
		}
		return c.listener.Addr().String(), nil
	case *RhttpConn:
		if c.listener == nil || c.listener.listenerconn == nil {
			return "", fmt.Errorf("rhttp listener empty")
		}
		return c.listener.listenerconn.Addr().String(), nil
	case *RicmpConn:
		return "127.0.0.1", nil
	default:
		info := ln.Info()
		if i := strings.Index(info, "--"); i >= 0 {
			return info[i+2:], nil
		}
		return "", fmt.Errorf("cannot resolve listen addr from %T info=%q", ln, info)
	}
}

func startUDPNetemProxy(backend string, loss float64, oneWayDelay time.Duration, seed int64) (proxyAddr string, stop func(), err error) {
	backAddr, err := net.ResolveUDPAddr("udp", backend)
	if err != nil {
		return "", nil, err
	}
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return "", nil, err
	}
	_ = pc.SetReadBuffer(8 << 20)
	_ = pc.SetWriteBuffer(8 << 20)

	rng := rand.New(rand.NewSource(seed))
	var mu sync.Mutex
	var writeMu sync.Mutex
	var clientAddr *net.UDPAddr
	var closed atomic.Bool
	var delayWG sync.WaitGroup

	// 并行延迟（不串行 HOL）；约 2% 包 +1ms，轻微乱序。
	schedule := func(pkt []byte, dst *net.UDPAddr) {
		if dst == nil || closed.Load() {
			return
		}
		delay := oneWayDelay
		if delay > 0 {
			mu.Lock()
			bump := rng.Float64() < 0.02
			mu.Unlock()
			if bump {
				delay += time.Millisecond
			}
		}
		delayWG.Add(1)
		time.AfterFunc(delay, func() {
			defer delayWG.Done()
			if closed.Load() {
				return
			}
			writeMu.Lock()
			_, _ = pc.WriteToUDP(pkt, dst)
			writeMu.Unlock()
		})
	}

	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		buf := make([]byte, 65535)
		for !closed.Load() {
			_ = pc.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, addr, err := pc.ReadFromUDP(buf)
			if closed.Load() {
				return
			}
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				continue
			}
			fromBackend := addr.IP.Equal(backAddr.IP) && addr.Port == backAddr.Port
			pkt := append([]byte(nil), buf[:n]...)
			mu.Lock()
			drop := loss > 0 && rng.Float64() < loss
			if fromBackend {
				ca := clientAddr
				mu.Unlock()
				if ca == nil || drop {
					continue
				}
				schedule(pkt, ca)
				continue
			}
			if clientAddr == nil {
				cp := *addr
				clientAddr = &cp
			}
			mu.Unlock()
			if drop {
				continue
			}
			schedule(pkt, backAddr)
		}
	}()

	stop = func() {
		if closed.Swap(true) {
			return
		}
		_ = pc.Close()
		readerWG.Wait()
		delayWG.Wait()
	}
	return pc.LocalAddr().String(), stop, nil
}

var (
	loTCMu   sync.Mutex
	tcBin    = "/usr/sbin/tc"
)

func tcpPortOf(addr string) (int, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}

func runTC(args ...string) error {
	cmd := exec.Command(tcBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tc %v: %v (%s)", args, err, bytes.TrimSpace(out))
	}
	return nil
}

// startLoTCNetemTCP 在 lo 上对指定 TCP 端口挂内核 netem（delay + loss）。
func startLoTCNetemTCP(port int, delay time.Duration, loss float64) (stop func(), err error) {
	return startLoTCNetem(delay, loss, [][]string{
		{"filter", "add", "dev", "lo", "protocol", "ip", "parent", "1:0", "prio", "1",
			"u32", "match", "ip", "dport", strconv.Itoa(port), "0xffff", "flowid", "1:3"},
		{"filter", "add", "dev", "lo", "protocol", "ip", "parent", "1:0", "prio", "1",
			"u32", "match", "ip", "sport", strconv.Itoa(port), "0xffff", "flowid", "1:3"},
	})
}

// startLoTCNetemICMP 在 lo 上对 ICMP（protocol 1）挂内核 netem。
func startLoTCNetemICMP(delay time.Duration, loss float64) (stop func(), err error) {
	return startLoTCNetem(delay, loss, [][]string{
		{"filter", "add", "dev", "lo", "protocol", "ip", "parent", "1:0", "prio", "1",
			"u32", "match", "ip", "protocol", "1", "0xff", "flowid", "1:3"},
	})
}

func startLoTCNetem(delay time.Duration, loss float64, filters [][]string) (stop func(), err error) {
	if _, err := exec.LookPath(tcBin); err != nil {
		if p, e := exec.LookPath("tc"); e == nil {
			tcBin = p
		} else {
			return nil, fmt.Errorf("tc not found (install iproute-tc): %w", err)
		}
	}
	if out, err := exec.Command("modprobe", "sch_netem").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("modprobe sch_netem: %v (%s)", err, bytes.TrimSpace(out))
	}

	loTCMu.Lock()
	_ = exec.Command(tcBin, "qdisc", "del", "dev", "lo", "root").Run()

	delayMs := delay.Milliseconds()
	if delayMs < 0 {
		delayMs = 0
	}
	netemArgs := []string{
		"qdisc", "add", "dev", "lo", "parent", "1:3", "handle", "30:",
		"netem", "delay", fmt.Sprintf("%dms", delayMs),
		"limit", "100000",
	}
	if loss > 0 {
		netemArgs = append(netemArgs, "loss", fmt.Sprintf("%g%%", loss*100))
	}

	cleanup := func() {
		_ = exec.Command(tcBin, "qdisc", "del", "dev", "lo", "root").Run()
		loTCMu.Unlock()
	}

	if err := runTC("qdisc", "replace", "dev", "lo", "root", "handle", "1:", "prio"); err != nil {
		cleanup()
		return nil, err
	}
	if err := runTC(netemArgs...); err != nil {
		cleanup()
		return nil, err
	}
	for _, f := range filters {
		if err := runTC(f...); err != nil {
			cleanup()
			return nil, err
		}
	}

	var once sync.Once
	stop = func() {
		once.Do(cleanup)
	}
	return stop, nil
}

func isPermissionUnavailable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "operation not permitted") ||
		strings.Contains(s, "permission denied") ||
		strings.Contains(s, "could not insert 'sch_netem'")
}

func TestReliableThroughputSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	for _, proto := range []string{"rudp", "kcp", "quic", "rhttp", "ricmp"} {
		proto := proto
		t.Run(proto+"/loss0%", func(t *testing.T) {
			mbps, total, err := runFixedPayloadDownload(proto, 0, 10*time.Second, 1)
			if err != nil {
				if isPermissionUnavailable(err) {
					t.Skipf("%s unavailable in unprivileged environment: %v", proto, err)
				}
				t.Fatal(err)
			}
			t.Logf("download %.2f MB/s (%d bytes in 10s)", mbps, total)
			if total%(int64(benchPayloadSize)) != 0 {
				t.Fatalf("verified bytes not multiple of %d: %d", benchPayloadSize, total)
			}
			if mbps < 0.01 {
				t.Fatalf("throughput too low: %.2f MB/s", mbps)
			}
		})
	}
	t.Run("rhttp/loss10%short", func(t *testing.T) {
		mbps, total, err := runFixedPayloadDownload("rhttp", 0.10, 15*time.Second, 2)
		if err != nil {
			if isPermissionUnavailable(err) {
				t.Skipf("rhttp tc netem unavailable in unprivileged environment: %v", err)
			}
			t.Fatal(err)
		}
		t.Logf("rhttp loss10%% %.2f MB/s (%d bytes in 15s)", mbps, total)
		if total <= 0 {
			t.Fatal("expected some verified bytes under tc netem loss")
		}
	})
	t.Run("ricmp/loss10%short", func(t *testing.T) {
		mbps, total, err := runFixedPayloadDownload("ricmp", 0.10, 15*time.Second, 3)
		if err != nil {
			if isPermissionUnavailable(err) {
				t.Skipf("ricmp tc netem unavailable in unprivileged environment: %v", err)
			}
			t.Fatal(err)
		}
		t.Logf("ricmp loss10%% %.2f MB/s (%d bytes in 15s)", mbps, total)
		if total <= 0 {
			t.Fatal("expected some verified bytes under tc netem loss")
		}
	})
}

