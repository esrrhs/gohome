package network

import (
	"fmt"
	"sync/atomic"
	"github.com/esrrhs/gohome/loggo"
	"strconv"
	"testing"
	"time"
)

func Test000RUDP(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58084")
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

func Test0002RUDP(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		conn, err := c.Dial("9.9.9.9:58084")
		fmt.Println("Dial return")
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Println(conn.Info())
		}

	}()

	time.Sleep(time.Second)

	c.Close()
	fmt.Println("closed")

	time.Sleep(time.Second)
}

func Test0003RUDP(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58084")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial(":58084")
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

	time.Sleep(time.Second * 5)
	fmt.Println("start close listener")
	cc.Close()
	fmt.Println("close listener ok")
	fmt.Println("start close client")
	ccc.Close()
	fmt.Println("close client ok")

	time.Sleep(time.Second)
}

func Test0004RUDP(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58084")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial(":58084")
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

func Test0005RUDP(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58084")
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

	ccc, err := c.Dial(":58084")
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

func Test0005RUDP1(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58084")
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

	ccc, err := c.Dial(":58084")
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

func Test0006RUDP(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58084")
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

	ccc, err := c.Dial(":58084")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		time.Sleep(time.Second)
		ccc.Close()
		fmt.Println("client close")
	}()

	time.Sleep(time.Second * 20)

	fmt.Println("start close")
	cc.Close()
	ccc.Close()

	time.Sleep(time.Second)
}

func Test0007RUDP(t *testing.T) {

	c, err := NewConn("rudp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58084")
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

	ccc, err := c.Dial(":58084")
	if err != nil {
		fmt.Println(err)
		return
	}

	time.Sleep(time.Second * 5)

	fmt.Println("start close")
	cc.Close()
	ccc.Close()

	time.Sleep(time.Second)
}

func Test0008RUDP(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58084")
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

	ccc, err := c.Dial(":58084")
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

func TestRudpAcceptUnblocksOnClose(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := c.Listen("127.0.0.1:58201")
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
			t.Fatal("Accept should fail after Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Accept blocked after Close")
	}
}

func TestRudpAcceptImmediateReadWrite(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := c.Listen("127.0.0.1:58211")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srvReady := make(chan Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		srv, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		srvReady <- srv
		buf := make([]byte, 64)
		n, err := srv.Read(buf)
		if err != nil {
			errCh <- err
			return
		}
		_, err = srv.Write(buf[:n])
		errCh <- err
	}()

	time.Sleep(100 * time.Millisecond)
	cli, err := c.Dial("127.0.0.1:58211")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer cli.Close()

	var srv Conn
	select {
	case srv = <-srvReady:
	case err := <-errCh:
		t.Fatalf("Accept: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Accept timed out")
	}
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	msg := []byte("rudp-accept-ready")
	if _, err := cli.Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 64)
		var got []byte
		for {
			n, err := cli.Read(buf)
			if err != nil {
				readDone <- err
				return
			}
			got = append(got, buf[:n]...)
			if string(got) == string(msg) {
				readDone <- nil
				return
			}
		}
	}()

	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("client Read: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client Read timed out")
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server side timed out")
	}
}

func TestRudpAcceptNotListen(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Accept(); err == nil {
		t.Fatal("Accept on non-listener should fail")
	}
}

func TestRudpCloseJoinEcho(t *testing.T) {
	c, err := NewConn("rudp")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := c.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.(*RudpConn).listener.listenerconn.LocalAddr().String()

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

	msg := []byte("rudp-close-join")
	if _, err := cli.Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 64)
	deadline := time.After(5 * time.Second)
	var got []byte
	for {
		n, err := srv.Read(buf)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		got = append(got, buf[:n]...)
		if string(got) == string(msg) {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("incomplete read: %q", got)
		default:
		}
	}

	if err := cli.Close(); err != nil {
		t.Fatalf("cli Close: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("srv Close: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("ln Close: %v", err)
	}
}
