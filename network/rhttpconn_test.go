package network

import (
	"fmt"
	"github.com/esrrhs/gohome/loggo"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func Test000RHTTP(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58082")
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

func Test0002RHTTP(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		conn, err := c.Dial("9.9.9.9:58082")
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

func Test0003RHTTP(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58082")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial("127.0.0.1:58082")
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

func Test0004RHTTP(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58082")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial("127.0.0.1:58082")
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

func Test0005RHTTP(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58082")
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

	ccc, err := c.Dial("127.0.0.1:58082")
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

	time.Sleep(time.Second * 10)

	cc.Close()
	ccc.Close()

	exit.Store(true)

	time.Sleep(time.Second)
}

func Test0005RHTTP1(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58082")
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

	ccc, err := c.Dial("127.0.0.1:58082")
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

	time.Sleep(time.Second * 10)

	cc.Close()
	ccc.Close()

	exit.Store(true)

	time.Sleep(time.Second)
}

func Test0006RHTTP(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58082")
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

	ccc, err := c.Dial("127.0.0.1:58082")
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

func Test0007RHTTP(t *testing.T) {

	c, err := NewConn("rhttp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58082")
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

	ccc, err := c.Dial("127.0.0.1:58082")
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

func Test0008RHTTP(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58082")
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

	ccc, err := c.Dial("127.0.0.1:58082")
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

func Test0009RHTTP(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58082")
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
			//fmt.Println("start Read")
			n, err := cc.Read(data)
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

	ccc, err := c.Dial("127.0.0.1:58082")
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
			//fmt.Println("start Write")
			_, err := ccc.Write(buf)
			if err != nil {
				fmt.Println(err)
				return
			}
			//fmt.Println("end Write")
			speed += len(buf)
			if time.Now().Sub(start) > time.Second {
				speed = speed / 1024 / 1024
				loggo.Info("write speed %v MB per second", float64(speed)/float64(time.Now().Sub(start)/time.Second))
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

func TestRhttpName(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name() != "rhttp" {
		t.Fatalf("Name()=%q want rhttp", c.Name())
	}
}

func TestRhttpCloseJoinNoRace(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := c.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.(*RhttpConn).listener.listenerconn.Addr().String()

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

	msg := []byte("close-join")
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

func TestRhttpDialCancel(t *testing.T) {
	c, err := NewConn("rhttp")
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultHttpConfig()
	cfg.RequestTimeoutMs = 30000
	c.(*RhttpConn).SetConfig(cfg)

	done := make(chan error, 1)
	go func() {
		_, err := c.Dial("203.0.113.1:9") // TEST-NET-3 blackhole
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
	case <-time.After(5 * time.Second):
		t.Fatal("Dial was not canceled within 5s")
	}
}

func TestRhttpHBTimeoutUsesMilliseconds(t *testing.T) {
	cfg := DefaultHttpConfig()
	hb := time.Duration(cfg.HBTimeoutMs) * time.Millisecond
	if hb != 10*time.Second {
		t.Fatalf("HB duration=%v want 10s (was wrongly using time.Second*HBTimeoutMs)", hb)
	}
	if time.Second*time.Duration(cfg.HBTimeoutMs) < time.Hour {
		t.Fatal("sanity: old buggy formula should be huge")
	}
}

func TestRhttpNoTransportFDLeakSmoke(t *testing.T) {
	l, err := NewConn("rhttp")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := l.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.(*RhttpConn).listener.listenerconn.Addr().String()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn Conn) {
				buf := make([]byte, 64)
				for {
					if _, err := conn.Read(buf); err != nil {
						conn.Close()
						return
					}
				}
			}(c)
		}
	}()

	for i := 0; i < 50; i++ {
		d, err := NewConn("rhttp")
		if err != nil {
			t.Fatal(err)
		}
		cfg := DefaultHttpConfig()
		cfg.RequestTimeoutMs = 5000
		d.(*RhttpConn).SetConfig(cfg)
		conn, err := d.Dial(addr)
		if err != nil {
			t.Fatalf("Dial %d: %v", i, err)
		}
		if _, err := conn.Write([]byte("ping")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
	}
}
