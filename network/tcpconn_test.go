package network

import (
	"fmt"
	"github.com/esrrhs/gohome/loggo"
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
	c, err := NewConn("tcp")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := c.Dial("203.0.113.1:1")
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Dial succeeded unexpectedly after Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Dial did not return after Close")
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
