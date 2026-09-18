package network

import (
	"context"
	"fmt"
	"github.com/esrrhs/gohome/loggo"
	"strconv"
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

	exit := false

	go func() {
		cc, err := cc.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		defer cc.Close()
		fmt.Println("accept done")
		buf := make([]byte, 10)
		for !exit {
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
		for i := 0; i < 10000 && !exit; i++ {
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

	exit = true

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

	exit := false

	go func() {
		cc, err := cc.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("accept done")
		for i := 0; i < 10000 && !exit; i++ {
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
		for !exit {
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

	exit = true

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

	exit := false

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
		for !exit {
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
		for !exit {
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

	exit = true

	time.Sleep(time.Second)
}

func TestQuicCloseReleasesSession(t *testing.T) {
	l, err := NewConn("quic")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := l.Listen("127.0.0.1:58191")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

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
	client, err := d.Dial("127.0.0.1:58191")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	qc := client.(*QuicConn)
	if qc.qsession == nil || qc.session == nil {
		t.Fatal("missing quic/smux session after dial")
	}
	qsession := qc.qsession
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Closed quic session should reject new streams.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := qsession.OpenStreamSync(ctx); err == nil {
		t.Fatal("expected OpenStreamSync to fail after Close")
	}

	select {
	case ac := <-accepted:
		_ = ac.Close()
	case <-time.After(3 * time.Second):
	}
}
