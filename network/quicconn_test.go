package network

import (
	"context"
	"fmt"
	"github.com/esrrhs/gohome/loggo"
	"github.com/quic-go/quic-go"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func Test0001Quic(t *testing.T) {
	c, err := NewConn("quic")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	time.Sleep(time.Second)

	cc.Close()

	time.Sleep(time.Second)
}

func Test0002Quic(t *testing.T) {
	c, err := NewConn("quic")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		_, err := c.Dial("localhost:58081")
		fmt.Println(err)
	}()

	time.Sleep(time.Second)

	c.Close()

	time.Sleep(time.Second)
}

func TestQuicDialCancel(t *testing.T) {
	c, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Dial("203.0.113.1:1") // TEST-NET-3, blackhole
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Dial succeeded unexpectedly")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Dial was not canceled within 3s")
	}
}

func Test0003Quic(t *testing.T) {
	c, err := NewConn("quic")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		buf := make([]byte, 100)
		_, err := ccc.Read(buf)
		if err != nil {
			fmt.Println(err)
			return
		}
	}()

	time.Sleep(time.Second)

	cc.Close()
	ccc.Close()

	time.Sleep(time.Second)
}

func Test0004Quic(t *testing.T) {
	c, err := NewConn("quic")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		buf := make([]byte, 1000)
		for i := 0; i < 10000; i++ {
			_, err := ccc.Write(buf)
			if err != nil {
				fmt.Println(err)
				return
			}
		}
		fmt.Println("write done")
	}()

	time.Sleep(time.Second)

	cc.Close()
	ccc.Close()

	time.Sleep(time.Second)
}

func Test0005Quic(t *testing.T) {
	c, err := NewConn("quic")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	var exit atomic.Bool

	go func() {
		cc, err := cc.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		defer cc.Close()
		fmt.Println("accept done")
		buf := make([]byte, 10)
		for !exit.Load() {
			n, err := cc.Read(buf)
			if err != nil {
				fmt.Println(err)
				fmt.Println("Read done")
				return
			}
			fmt.Println(string(buf[0:n]))
			time.Sleep(time.Millisecond * 100)
		}
	}()

	ccc, err := c.Dial("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		for i := 0; i < 10000 && !exit.Load(); i++ {
			_, err := ccc.Write([]byte("hahaha" + strconv.Itoa(i)))
			if err != nil {
				fmt.Println(err)
				return
			}
		}
		fmt.Println("write done")
	}()

	time.Sleep(time.Second)

	cc.Close()
	ccc.Close()

	exit.Store(true)

	time.Sleep(time.Second)
}

func Test0005Quic1(t *testing.T) {
	c, err := NewConn("quic")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	var exit atomic.Bool

	go func() {
		cc, err := cc.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("accept done")
		for i := 0; i < 10000 && !exit.Load(); i++ {
			_, err := cc.Write([]byte("hahaha" + strconv.Itoa(i)))
			if err != nil {
				fmt.Println(err)
				return
			}
		}
		fmt.Println("write done")
	}()

	ccc, err := c.Dial("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		buf := make([]byte, 10)
		for !exit.Load() {
			n, err := ccc.Read(buf)
			if err != nil {
				fmt.Println(err)
				fmt.Println("Read done")
				return
			}
			fmt.Println(string(buf[0:n]))
			time.Sleep(time.Millisecond * 100)
		}
		fmt.Println("write done")
	}()

	time.Sleep(time.Second)

	cc.Close()
	ccc.Close()

	exit.Store(true)

	time.Sleep(time.Second)
}

func Test0006Quic(t *testing.T) {
	c, err := NewConn("quic")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc, err := cc.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		defer cc.Close()
		fmt.Println("accept done")
		buf := make([]byte, 10)
		_, err = cc.Read(buf)
		if err != nil {
			fmt.Println("Read " + err.Error())
			return
		}
		fmt.Println("Read done")

	}()

	ccc, err := c.Dial("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		time.Sleep(time.Second)
		ccc.Close()
		fmt.Println("client close")
	}()

	time.Sleep(time.Second * 3)

	cc.Close()
	ccc.Close()

	time.Sleep(time.Second)
}

func Test0008Quic(t *testing.T) {
	c, err := NewConn("quic")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	var exit atomic.Bool

	go func() {
		cc, err := cc.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("accept done")
		data := make([]byte, 1024*1024)
		start := time.Now()
		speed := 0
		for !exit.Load() {
			//fmt.Println("start Write")
			_, err := cc.Write(data)
			if err != nil {
				fmt.Println(err)
				return
			}
			//fmt.Println("end Write")
			speed += len(data)
			if time.Now().Sub(start) > time.Second {
				speed = speed / 1024 / 1024
				loggo.Info("write speed %v MB per second", float64(speed)/float64(time.Now().Sub(start)/time.Second))
				speed = 0
				start = time.Now()
			}
		}
		fmt.Println("write done")
	}()

	ccc, err := c.Dial("localhost:58081")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		fmt.Println("start client")
		buf := make([]byte, 1024*1024)
		start := time.Now()
		speed := 0
		for !exit.Load() {
			//fmt.Println("start Read")
			n, err := ccc.Read(buf)
			//fmt.Println("start Read")
			if err != nil {
				fmt.Println(err)
				fmt.Println("Read done")
				return
			}
			speed += n
			if time.Now().Sub(start) > time.Second {
				speed = speed / 1024 / 1024
				loggo.Info("read speed %v MB per second", float64(speed)/float64(time.Now().Sub(start)/time.Second))
				speed = 0
				start = time.Now()
			}
		}
		fmt.Println("write done")
	}()

	time.Sleep(time.Second * 10)

	cc.Close()
	ccc.Close()

	exit.Store(true)

	time.Sleep(time.Second)
}

func TestQuicCloseReleasesSession(t *testing.T) {
	l, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := l.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.(*QuicConn).listener.Addr().String()

	accepted := make(chan Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()

	d, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	client, err := d.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	qc := client.(*QuicConn)
	if qc.qsession == nil || qc.stream == nil {
		t.Fatal("missing quic session/stream after dial")
	}
	if qc.clientSess == nil || qc.clientSess.pconn == nil {
		t.Fatal("missing cached session/dial-side PacketConn after dial")
	}
	qsession := qc.qsession
	cs := qc.clientSess
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close releases the reference; the session stays warm in the cache for
	// fast reconnect (idle timer armed, refs zero).
	cs.mu.Lock()
	if cs.refs != 0 {
		t.Fatalf("refs after Close = %d, want 0", cs.refs)
	}
	if cs.idleTimer == nil {
		t.Fatal("idle shutdown timer not armed after Close")
	}
	cs.mu.Unlock()

	// Once idle-evicted the session must reject new streams.
	cs.idleEvict()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := qsession.OpenStreamSync(ctx); err == nil {
		t.Fatal("expected OpenStreamSync to fail after idle eviction")
	}

	select {
	case ac := <-accepted:
		_ = ac.Close()
	case <-time.After(3 * time.Second):
	}
}

func TestQuicCloseFreesPacketConn(t *testing.T) {
	l, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := l.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.(*QuicConn).listener.Addr().String()

	go func() {
		c, err := ln.Accept()
		if err == nil {
			buf := make([]byte, 8)
			_, _ = c.Read(buf)
			_ = c.Close()
		}
	}()

	d, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := d.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	qc := cli.(*QuicConn)
	cs := qc.clientSess
	if cs == nil || cs.pconn == nil {
		t.Fatal("expected cached session with dial-side PacketConn")
	}
	pconn := cs.pconn
	_, _ = cli.Write([]byte("x"))
	if err := cli.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// While warm-idle the shared socket remains reusable; once the cached
	// session is evicted the underlying UDP socket must be closed (not
	// waiting for GC) and reject I/O.
	cs.idleEvict()
	if _, err := pconn.WriteTo([]byte("z"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}); err == nil {
		t.Fatal("expected WriteTo on closed PacketConn to fail")
	}
}

func TestQuicAcceptImmediateReadWrite(t *testing.T) {
	l, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := l.Listen("127.0.0.1:58193")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		srv, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		buf := make([]byte, 64)
		n, err := srv.Read(buf)
		if err != nil {
			_ = srv.Close()
			errCh <- err
			return
		}
		_, err = srv.Write(buf[:n])
		// Keep session open briefly so the client can read the echo.
		time.Sleep(200 * time.Millisecond)
		_ = srv.Close()
		errCh <- err
	}()

	time.Sleep(50 * time.Millisecond)
	d, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := d.Dial("127.0.0.1:58193")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer cli.Close()

	msg := []byte("quic-no-smux")
	if _, err := cli.Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 64)
	deadline := time.Now().Add(5 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		n, err := cli.Read(buf)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		got = append(got, buf[:n]...)
		if string(got) == string(msg) {
			break
		}
	}
	if string(got) != string(msg) {
		t.Fatalf("echo mismatch: %q", got)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server side timed out")
	}
}

func TestQuicAcceptNotListen(t *testing.T) {
	c, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Accept(); err == nil {
		t.Fatal("Accept on non-listener should fail")
	}
}

// TestQuicMultiStreamSessionReuse opens two Dials to the same server and
// verifies that (a) both are accepted as independent conns, (b) both carry
// data correctly, (c) the client reused one QUIC session (two streams) and
// the server sees both streams on that same session.
func TestQuicMultiStreamSessionReuse(t *testing.T) {
	l, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := l.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.(*QuicConn).listener.Addr().String()

	const n = 2
	srvConns := make(chan Conn, n)
	srvErr := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			s, err := ln.Accept()
			if err != nil {
				srvErr <- err
				return
			}
			srvConns <- s
		}()
	}
	// Per-connection echo goroutines.
	go func() {
		for i := 0; i < n; i++ {
			s := <-srvConns
			go func(s Conn) {
				buf := make([]byte, 128)
				nr, err := s.Read(buf)
				if err != nil {
					_ = s.Close()
					srvErr <- err
					return
				}
				if _, err := s.Write(buf[:nr]); err != nil {
					srvErr <- err
				}
				// Let the reply reach the client before sending FIN.
				time.Sleep(200 * time.Millisecond)
				_ = s.Close()
			}(s)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	d, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	var clients [n]Conn
	for i := 0; i < n; i++ {
		cli, err := d.Dial(addr)
		if err != nil {
			t.Fatalf("Dial %d: %v", i, err)
		}
		clients[i] = cli
	}
	defer func() {
		for _, cli := range clients {
			if cli != nil {
				_ = cli.Close()
			}
		}
	}()

	var sharedSession *quic.Conn
	for i, cli := range clients {
		qc := cli.(*QuicConn)
		if qc.clientSess == nil {
			t.Fatalf("client %d not backed by session cache", i)
		}
		if i == 0 {
			sharedSession = qc.qsession
		} else if qc.qsession != sharedSession {
			t.Fatalf("client %d opened a different QUIC session; expected reuse", i)
		}

		msg := []byte("quic multi-stream " + strconv.Itoa(i))
		if _, err := cli.Write(msg); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
		buf := make([]byte, 128)
		_ = cli.(*QuicConn).stream.SetReadDeadline(time.Now().Add(5 * time.Second))
		nr, err := cli.Read(buf)
		if err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
		if string(buf[:nr]) != string(msg) {
			t.Fatalf("echo mismatch %d: %q", i, buf[:nr])
		}
	}

	// Both clients share one session: the cache refcount is 2 while alive.
	if cs := clients[0].(*QuicConn).clientSess; cs.refs != n {
		t.Fatalf("session refs = %d, want %d", cs.refs, n)
	}
}

// TestQuicSessionCacheConcurrentStress hammers one server address with many
// concurrent Dial/echo/Close rounds. Run with -race: the shared session
// cache refcounting and idle-timer transitions must stay race-free, every
// echo must succeed via the cached session, and the entry must evict cleanly
// afterwards.
func TestQuicSessionCacheConcurrentStress(t *testing.T) {
	l, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := l.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.(*QuicConn).listener.Addr().String()

	go func() {
		for {
			s, err := ln.Accept()
			if err != nil {
				return
			}
			go func(s Conn) {
				defer s.Close()
				buf := make([]byte, 64)
				for {
					n, err := s.Read(buf)
					if err != nil {
						return
					}
					if _, err := s.Write(buf[:n]); err != nil {
						return
					}
				}
			}(s)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	const goroutines = 8
	const rounds = 5
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*rounds)
	var sessions sync.Map // *quic.Conn -> struct{}
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := NewConn("quic")
			if err != nil {
				errCh <- err
				return
			}
			for r := 0; r < rounds; r++ {
				cli, err := d.Dial(addr)
				if err != nil {
					errCh <- err
					return
				}
				sessions.Store(cli.(*QuicConn).qsession, struct{}{})
				msg := []byte("stress round " + strconv.Itoa(r))
				if _, err := cli.Write(msg); err != nil {
					errCh <- err
					_ = cli.Close()
					return
				}
				buf := make([]byte, 64)
				_ = cli.(*QuicConn).stream.SetReadDeadline(time.Now().Add(5 * time.Second))
				n, err := cli.Read(buf)
				if err != nil {
					errCh <- fmt.Errorf("read round %d: %w", r, err)
					_ = cli.Close()
					return
				}
				if string(buf[:n]) != string(msg) {
					errCh <- fmt.Errorf("echo mismatch round %d: %q", r, buf[:n])
					_ = cli.Close()
					return
				}
				_ = cli.Close()
			}
		}()
	}
	wg.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}

	// Every dial reused the same cached session (one handshake total).
	count := 0
	sessions.Range(func(_, _ any) bool { count++; return true })
	if count != 1 {
		t.Fatalf("concurrent dials used %d QUIC sessions, want 1 cached session", count)
	}

	// After all refs drop, force eviction and confirm the cache releases it.
	if v, ok := quicClientSessionCache.Load(addr); ok {
		v.(*quicClientSession).idleEvict()
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := quicClientSessionCache.Load(addr); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cached QUIC session entry leaked after eviction")
}

func TestQuicListenCloseUnblocksAccept(t *testing.T) {
	c, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := c.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := ln.Accept()
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	if err := ln.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Accept should fail after listener Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Accept not unblocked by listener Close")
	}
}
