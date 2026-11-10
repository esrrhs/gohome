package network

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"
)

// Scripted impairments in BOTH directions. Each forwarding loop owns all its
// state and scheduled packets; shutdown joins the loops before reading stats.
// No global hooks, privileged ports, random loss, or per-packet goroutines.
type rdnsFaultStats struct{ dropped, duplicated, delayed, reordered int }
type rdnsScheduledPacket struct {
	data []byte
	due  time.Time
	seq  int
}
type rdnsFaultProxy struct {
	front, back *net.UDPConn
	stop        chan struct{}
	once        sync.Once
	wg          sync.WaitGroup
	stats       [2]rdnsFaultStats
	mu          sync.Mutex
	client      *net.UDPAddr
}

func newRdnsFaultProxy(t *testing.T, upstream *net.UDPAddr) *rdnsFaultProxy {
	t.Helper()
	front, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	back, err := net.DialUDP("udp", nil, upstream)
	if err != nil {
		front.Close()
		t.Fatal(err)
	}
	p := &rdnsFaultProxy{front: front, back: back, stop: make(chan struct{})}
	p.wg.Add(2)
	for direction := 0; direction < 2; direction++ {
		go p.forward(direction)
	}
	t.Cleanup(p.close)
	return p
}
func (p *rdnsFaultProxy) close() {
	p.once.Do(func() { close(p.stop); p.front.Close(); p.back.Close(); p.wg.Wait() })
}
func (p *rdnsFaultProxy) forward(direction int) {
	defer p.wg.Done()
	src := p.front
	if direction == 1 {
		src = p.back
	}
	stats := &p.stats[direction]
	seq, maxSent := 0, 0
	var queue []rdnsScheduledPacket
	buf := make([]byte, 4096)
	send := func(packet rdnsScheduledPacket) {
		if packet.seq < maxSent {
			stats.reordered++
		}
		if packet.seq > maxSent {
			maxSent = packet.seq
		}
		if direction == 0 {
			_, _ = p.back.Write(packet.data)
		} else {
			p.mu.Lock()
			client := p.client
			p.mu.Unlock()
			if client != nil {
				_, _ = p.front.WriteToUDP(packet.data, client)
			}
		}
	}
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		now := time.Now()
		pending := queue[:0]
		for _, packet := range queue {
			if !now.Before(packet.due) {
				send(packet)
			} else {
				pending = append(pending, packet)
			}
		}
		queue = pending
		_ = src.SetReadDeadline(time.Now().Add(5 * time.Millisecond))
		n, addr, err := src.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		if direction == 0 {
			p.mu.Lock()
			p.client = addr
			p.mu.Unlock()
		}
		seq++
		if seq%29 == 0 {
			stats.dropped++
			continue
		}
		packet := rdnsScheduledPacket{data: append([]byte(nil), buf[:n]...), seq: seq}
		if seq%17 == 0 {
			packet.due = time.Now().Add(400 * time.Millisecond)
			queue = append(queue, packet)
			stats.delayed++
			continue
		}
		if seq%7 == 0 {
			packet.due = time.Now().Add(5 * time.Millisecond)
			queue = append(queue, packet)
			stats.delayed++
		} else {
			send(packet)
		}
		if seq%11 == 0 {
			queue = append(queue, rdnsScheduledPacket{data: packet.data, seq: seq, due: time.Now().Add(2 * time.Millisecond)})
			stats.duplicated++
		}
	}
}

func rdnsBoundedTransfer(t *testing.T, client, server Conn, up, down []byte) {
	t.Helper()
	done := make(chan error, 4)
	write := func(c Conn, p []byte) {
		n, err := c.Write(p)
		if err == nil && n != len(p) {
			err = io.ErrShortWrite
		}
		if err != nil {
			err = fmt.Errorf("write %s: %w", c.Info(), err)
		}
		done <- err
	}
	read := func(c Conn, want []byte) {
		got := make([]byte, len(want))
		_, err := io.ReadFull(c, got)
		if err == nil && !bytes.Equal(got, want) {
			err = fmt.Errorf("payload corruption (%d bytes)", len(want))
		}
		if err != nil {
			err = fmt.Errorf("read %s: %w", c.Info(), err)
		}
		done <- err
	}
	go write(client, up)
	go write(server, down)
	go read(server, up)
	go read(client, down)
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	var firstErr error
	for completed := 0; completed < 4; completed++ {
		select {
		case err := <-done:
			if err != nil && firstErr == nil {
				firstErr = err
				_ = client.Close()
				_ = server.Close()
			}
		case <-timer.C:
			if firstErr == nil {
				firstErr = fmt.Errorf("weak-network transfer exceeded 45s")
			}
			_ = client.Close()
			_ = server.Close()
			// All four workers are joined, including on failure/timeout.
			for ; completed < 4; completed++ {
				<-done
			}
		}
	}
	if firstErr != nil {
		for _, conn := range []Conn{client, server} {
			r := conn.(*RdnsConn)
			_, wg, _ := r.fmAndWg()
			if wg != nil {
				_ = wg.Join()
				t.Logf("%s group error: %v", r.Info(), wg.Error())
			}
		}
		t.Fatal(firstErr)
	}

}
func TestRdnsWeakNetwork(t *testing.T) {
	for _, variant := range []struct{ name, query, psk string }{{"TXT", "TXT", ""}, {"NULL_PSK", "NULL", "weaknet-test-key"}} {
		t.Run(variant.name, func(t *testing.T) {
			cfg := testRdnsConfig("weak.rdns.test")
			cfg.QueryType = variant.query
			cfg.PSK = variant.psk
			cfg.PollIntervalMs = 5
			driver := &RdnsConn{}
			driver.SetConfig(cfg)
			l, err := driver.Listen("127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			proxy := newRdnsFaultProxy(t, l.(*RdnsConn).listener.listenerconn.LocalAddr().(*net.UDPAddr))
			dialer := &RdnsConn{}
			dialer.SetConfig(cloneRdnsConfig(cfg))
			var previous string
			// Reconnect is explicit: a fresh Dial creates a new session. Existing streams
			// recover transient loss using ARQ, but do not auto-redial after Close.
			for iteration := 0; iteration < 2; iteration++ {
				t.Logf("transfer iteration %d", iteration)
				c, err := dialer.Dial(proxy.front.LocalAddr().String())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = c.Close() })
				ch := make(chan Conn, 1)
				go func() { s, _ := l.Accept(); ch <- s }()
				var s Conn
				select {
				case s = <-ch:
					if s == nil {
						t.Fatal("accept failed")
					}
				case <-time.After(10 * time.Second):
					t.Fatal("accept timeout")
				}
				t.Cleanup(func() { _ = s.Close() })
				session := c.(*RdnsConn).session
				if session == previous {
					t.Fatal("redial reused session")
				}
				previous = session
				size := 16 * 1024
				if iteration == 1 {
					size = 1024
				}
				up, down := make([]byte, size), make([]byte, size+173)
				rng := rand.New(rand.NewSource(int64(92026 + iteration)))
				_, _ = rng.Read(up)
				_, _ = rng.Read(down)
				rdnsBoundedTransfer(t, c, s, up, down)
				_ = c.Close()
				_ = s.Close()
				if _, err := c.Write([]byte("closed")); err == nil {
					t.Fatal("closed stream auto-reconnected")
				}
			}
			proxy.close()
			for i, stats := range proxy.stats {
				t.Logf("direction %d: %+v", i, stats)
				if stats.dropped == 0 || stats.duplicated == 0 || stats.delayed == 0 || stats.reordered == 0 {
					t.Fatalf("missing impairment in direction %d: %+v", i, stats)
				}
			}
		})
	}
}
