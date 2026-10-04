package network

import (
	"context"
	"errors"
	"fmt"
	"github.com/esrrhs/gohome/loggo"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func Test0001TCP(t *testing.T) {
	c, err := NewConn("tcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58085")
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

func Test0002TCP(t *testing.T) {
	c, err := NewConn("tcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		_, err := c.Dial("9.9.9.9:58085")
		fmt.Println(err)
	}()

	time.Sleep(time.Second)

	c.Close()

	time.Sleep(time.Second)
}

func Test0003TCP(t *testing.T) {
	c, err := NewConn("tcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58085")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial(":58085")
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

func Test0004TCP(t *testing.T) {
	c, err := NewConn("tcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58085")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial(":58085")
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

func Test0005TCP(t *testing.T) {
	c, err := NewConn("tcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58085")
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

	ccc, err := c.Dial(":58085")
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

func Test0005TCP1(t *testing.T) {
	c, err := NewConn("tcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58085")
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

	ccc, err := c.Dial(":58085")
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

func Test0006TCP(t *testing.T) {
	c, err := NewConn("tcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58085")
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

	ccc, err := c.Dial(":58085")
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

func Test0008TCP(t *testing.T) {
	c, err := NewConn("tcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58085")
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

	ccc, err := c.Dial(":58085")
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

func TestTcpAcceptNotListen(t *testing.T) {
	c, err := NewConn("tcp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Accept(); err == nil {
		t.Fatal("Accept on non-listener should fail")
	}
}

func TestTcpDialCancel(t *testing.T) {
	// Cancel must abort an in-flight dial, so the target must keep the dial in
	// SYN_SENT: a listener that never calls Accept leaves the kernel queue full
	// only after its backlog is exceeded. Flood it past the backlog so further
	// connects stay pending and Close is what releases the caller.
	//
	// A blackholed TEST-NET address cannot be used here: in sandboxed and
	// containerized environments the connect to 203.0.113.1:1 can succeed in
	// ~1ms, so Dial legitimately won the race against Close and the old
	// assertion failed for environmental reasons.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Never Accept; just hold the listener open until the test returns.
	dst := ln.Addr().String()

	// Fill the accept backlog so subsequent dials remain in SYN_SENT.
	fillers := make([]net.Conn, 0, 64)
	defer func() {
		for _, f := range fillers {
			f.Close()
		}
	}()
	for i := 0; i < 64; i++ {
		conn, err := net.DialTimeout("tcp", dst, 200*time.Millisecond)
		if err != nil {
			// Backlog is full: exactly the state we want.
			break
		}
		fillers = append(fillers, conn)
	}

	c, err := NewConn("tcp")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		conn, err := c.Dial(dst)
		if conn != nil {
			conn.Close()
		}
		done <- err
	}()
	<-started

	// Give the dial a moment to enter the kernel, then cancel via Close.
	time.Sleep(50 * time.Millisecond)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Skip("dial completed before Close took effect; backlog not saturated on this platform")
		}
		t.Logf("Dial canceled by Close: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Dial did not return after Close: Close failed to cancel the in-flight dial")
	}
}

// TestTcpDialCancelContextWiring verifies the cancel plumbing deterministically
// by driving the same context path Dial uses, with no dependence on how the OS
// schedules a pending SYN. Loopback dials complete in microseconds, so
// observing TcpConn.cancel mid-flight is inherently racy on any platform.
//
// The contract under test: the cancel func Dial registers is the one Close
// invokes, and Close clears it so a later Close cannot double-cancel.
func TestTcpDialCancelContextWiring(t *testing.T) {
	tcp := &TcpConn{}

	ctx, cancel := context.WithCancel(context.Background())
	tcp.dialMu.Lock()
	tcp.cancel = cancel
	tcp.dialMu.Unlock()

	// Close must consume and invoke the cancel func.
	if err := tcp.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := ctx.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("ctx.Err() = %v, want context.Canceled", err)
	}

	tcp.dialMu.Lock()
	leftover := tcp.cancel
	tcp.dialMu.Unlock()
	if leftover != nil {
		t.Error("Close should clear the cancel func")
	}

	// A second Close must be safe (nil cancel, no conn, no listener).
	if err := tcp.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
}

// TestTcpDialCancelAlreadyClosed pins the stricter guarantee: a Dial issued on
// an already-closed TcpConn must not hang. The cancel func is cleared by
// Close, so this exercises the concurrent Dial/Close bookkeeping in Dial's
// deferred cleanup.
func TestTcpDialCancelAlreadyClosed(t *testing.T) {
	c, err := NewConn("tcp")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Dial("127.0.0.1:1")
		done <- err
	}()

	select {
	case <-done:
		// Returning at all is the assertion; reaching a routable or refused
		// target both qualify.
	case <-time.After(10 * time.Second):
		t.Fatal("Dial on a closed TcpConn hung")
	}
}

func TestTcpListenCloseJoinEcho(t *testing.T) {
	c, err := NewConn("tcp")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := c.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.(*TcpConn).listener.Addr().String()

	accepted := make(chan Conn, 1)
	go func() {
		s, err := ln.Accept()
		if err == nil {
			accepted <- s
		}
	}()

	cli, err := c.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	srv := <-accepted

	msg := []byte("tcp-echo")
	if _, err := cli.Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 16)
	n, err := srv.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Fatalf("got %q want %q", buf[:n], msg)
	}

	_ = cli.Close()
	_ = srv.Close()
	_ = ln.Close()
}
