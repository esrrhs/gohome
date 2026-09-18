package network

import (
	"fmt"
	"github.com/esrrhs/gohome/loggo"
	"strconv"
	"testing"
	"time"
)

func Test000UDP(t *testing.T) {
	c, err := NewConn("udp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58086")
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

func Test0002UDP(t *testing.T) {
	c, err := NewConn("udp")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		conn, err := c.Dial("9.9.9.9:58086")
		if err != nil {
			fmt.Println(err)
		} else {
			fmt.Println(conn.Info())
		}

	}()

	time.Sleep(time.Second)

	c.Close()

	time.Sleep(time.Second)
}

func Test0003UDP(t *testing.T) {
	c, err := NewConn("udp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58086")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial(":58086")
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

func Test0004UDP(t *testing.T) {
	c, err := NewConn("udp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58086")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial(":58086")
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

func Test0005UDP(t *testing.T) {
	c, err := NewConn("udp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58086")
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

	ccc, err := c.Dial(":58086")
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

func Test0005UDP1(t *testing.T) {
	c, err := NewConn("udp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58086")
	if err != nil {
		fmt.Println(err)
		return
	}

	exit := false

	go func() {
		fmt.Println("start Accept")
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

	fmt.Println("start Dial")
	ccc, err := c.Dial(":58086")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Dial done")

	go func() {
		buf := make([]byte, 10)
		ccc.Write([]byte("hahaha"))
		ccc.Write([]byte("hahaha"))
		ccc.Write([]byte("hahaha"))
		for {
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

func Test0008UDP(t *testing.T) {
	c, err := NewConn("udp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58086")
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
		data := make([]byte, 500)
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

	ccc, err := c.Dial(":58086")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		fmt.Println("start client")
		ccc.Write([]byte("hahaha"))
		ccc.Write([]byte("hahaha"))
		ccc.Write([]byte("hahaha"))
		buf := make([]byte, 500)
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

func TestUdpAcceptNotListen(t *testing.T) {
	c, err := NewConn("udp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Accept(); err == nil {
		t.Fatal("Accept on non-listener should fail")
	}
}

func TestUdpCloseJoinEcho(t *testing.T) {
	c, err := NewConn("udp")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := c.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.(*UdpConn).listener.listenerconn.LocalAddr().String()

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

	msg := []byte("udp-close-join")
	if _, err := cli.Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var srv Conn
	select {
	case srv = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("Accept timed out")
	}

	buf := make([]byte, 64)
	n, err := srv.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Fatalf("got %q want %q", buf[:n], msg)
	}

	if _, err := srv.Write([]byte("pong")); err != nil {
		t.Fatalf("srv Write: %v", err)
	}
	n, err = cli.Read(buf)
	if err != nil {
		t.Fatalf("cli Read: %v", err)
	}
	if string(buf[:n]) != "pong" {
		t.Fatalf("got %q want pong", buf[:n])
	}

	_ = cli.Close()
	_ = srv.Close()
	_ = ln.Close()
}
