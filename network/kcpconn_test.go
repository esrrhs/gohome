package network

import (
	"fmt"
	"github.com/esrrhs/gohome/loggo"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func Test0001KCP(t *testing.T) {
	c, err := NewConn("kcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1:58080")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		_, err := cc.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("accept done")
	}()

	time.Sleep(time.Second)

	fmt.Println("start close")
	cc.Close()

	time.Sleep(time.Second)
}

func Test0002KCP(t *testing.T) {
	c, err := NewConn("kcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		_, err := c.Dial("9.9.9.9127.0.0.1:58280")
		fmt.Println(err)
	}()

	time.Sleep(time.Second)

	c.Close()

	time.Sleep(time.Second)
}

func TestKcpDialCancel(t *testing.T) {
	c, err := NewConn("kcp")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		conn, err := c.Dial("203.0.113.1:1") // TEST-NET-3
		if conn != nil {
			_ = conn.Close()
		}
		done <- err
	}()

	time.Sleep(time.Millisecond)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-done:
		// KCP may succeed quickly (no handshake); Close must still return and
		// must not leave the factory dial abort state stuck.
	case <-time.After(3 * time.Second):
		t.Fatal("Dial did not return after Close")
	}
}

func Test0003KCP(t *testing.T) {
	c, err := NewConn("kcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1:58380")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial("127.0.0.1:58380")
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

func Test0004KCP(t *testing.T) {
	c, err := NewConn("kcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1:58480")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial("127.0.0.1:58480")
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

func Test0005KCP(t *testing.T) {
	c, err := NewConn("kcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1:58580")
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

	ccc, err := c.Dial("127.0.0.1:58580")
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

func Test0005KCP1(t *testing.T) {
	c, err := NewConn("kcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1:58680")
	if err != nil {
		fmt.Println(err)
		return
	}

	var exit atomic.Bool

	go func() {
		//fmt.Println("start Accept")
		cc, err := cc.Accept()
		if err != nil {
			fmt.Println("Accept " + err.Error())
			return
		}
		//fmt.Println("end Accept")
		for i := 0; i < 10000 && !exit.Load(); i++ {
			_, err := cc.Write([]byte("hahaha" + strconv.Itoa(i)))
			if err != nil {
				fmt.Println(err)
				return
			}
		}
		fmt.Println("write done")
	}()

	fmt.Println("start Dial")
	ccc, err := c.Dial("127.0.0.1:58680")
	if err != nil {
		fmt.Println("Dial " + err.Error())
		return
	}
	fmt.Println("end Dial")

	go func() {
		buf := make([]byte, 10)
		for !exit.Load() {
			//fmt.Println("start Read")
			n, err := ccc.Read(buf)
			//fmt.Println("end Read")
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

	fmt.Println("start close")
	cc.Close()
	ccc.Close()

	exit.Store(true)

	time.Sleep(time.Second)
}

func Test0006KCP(t *testing.T) {
	c, err := NewConn("kcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1:58780")
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

	ccc, err := c.Dial("127.0.0.1:58780")
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

func Test0008KCP(t *testing.T) {
	c, err := NewConn("kcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1:58880")
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
		data := make([]byte, 1024)
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

	ccc, err := c.Dial("127.0.0.1:58880")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		fmt.Println("start client")
		buf := make([]byte, 1024)
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

func Test0009KCP(t *testing.T) {
	c, err := NewConn("kcp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1:58980")
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
		data := make([]byte, 1024)
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

	ccc, err := c.Dial("127.0.0.1:58980")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		fmt.Println("start client")
		buf := make([]byte, 1024)
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

func TestKcpAcceptImmediateReadWrite(t *testing.T) {
	l, err := NewConn("kcp")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := l.Listen("127.0.0.1:58221")
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
		defer srv.Close()
		buf := make([]byte, 64)
		n, err := srv.Read(buf)
		if err != nil {
			errCh <- err
			return
		}
		_, err = srv.Write(buf[:n])
		errCh <- err
	}()

	d, err := NewConn("kcp")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := d.Dial("127.0.0.1:58221")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer cli.Close()

	msg := []byte("kcp-no-smux")
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
	if _, ok := cli.(*KcpConn); !ok {
		t.Fatal("expected *KcpConn")
	}
	if cli.(*KcpConn).sess == nil {
		t.Fatal("expected direct UDPSession (no smux)")
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

func TestKcpAcceptNotListen(t *testing.T) {
	c, err := NewConn("kcp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Accept(); err == nil {
		t.Fatal("Accept on non-listener should fail")
	}
}

func TestKcpDefaultConfigFecOff(t *testing.T) {
	cfg := DefaultKcpConfig()
	if d, p := cfg.fecParams(); d != 0 || p != 0 {
		t.Fatalf("default FEC = (%d,%d), want (0,0) for legacy wire compat", d, p)
	}
}

func TestKcpFecConfigValidation(t *testing.T) {
	cases := []struct {
		name        string
		cfg         *KcpConfig
		wantData    int
		wantParity  int
		fallbackOff bool
	}{
		{"enabled", &KcpConfig{DataShards: 10, ParityShards: 3}, 10, 3, false},
		{"disabled", &KcpConfig{DataShards: 0, ParityShards: 0}, 0, 0, false},
		{"parity without data", &KcpConfig{DataShards: 0, ParityShards: 3}, 0, 0, true},
		{"shards overflow", &KcpConfig{DataShards: 200, ParityShards: 200}, 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewConn("kcp")
			if err != nil {
				t.Fatal(err)
			}
			kc := c.(*KcpConn)
			kc.SetConfig(tc.cfg)
			d, p := kc.GetConfig().fecParams()
			if d != tc.wantData || p != tc.wantParity {
				t.Fatalf("fecParams = (%d,%d), want (%d,%d)", d, p, tc.wantData, tc.wantParity)
			}
			if tc.fallbackOff {
				got := kc.GetConfig()
				if got.DataShards != 0 || got.ParityShards != 0 {
					t.Fatalf("invalid config not reset to FEC-off default: %+v", got)
				}
			}
		})
	}
}

// kcpFecEcho boots a listener/dialer pair with the given configs and runs
// one echo round trip. KCP has no handshake, so a wire mismatch surfaces as
// a read timeout rather than a Dial error.
func kcpFecEcho(t *testing.T, srvCfg, cliCfg *KcpConfig, expectEcho bool) {
	t.Helper()
	l, err := NewConn("kcp")
	if err != nil {
		t.Fatal(err)
	}
	if srvCfg != nil {
		l.(*KcpConn).SetConfig(srvCfg)
	}
	ln, err := l.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.(*KcpConn).listener.Addr().String()

	go func() {
		srv, err := ln.Accept()
		if err != nil {
			return
		}
		defer srv.Close()
		buf := make([]byte, 128)
		n, err := srv.Read(buf)
		if err != nil {
			return
		}
		_, _ = srv.Write(buf[:n])
	}()

	time.Sleep(100 * time.Millisecond)

	d, err := NewConn("kcp")
	if err != nil {
		t.Fatal(err)
	}
	if cliCfg != nil {
		d.(*KcpConn).SetConfig(cliCfg)
	}
	cli, err := d.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer cli.Close()

	msg := []byte("kcp-fec-echo")
	if _, err := cli.Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_ = cli.(*KcpConn).sess.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 128)
	n, err := cli.Read(buf)
	if expectEcho {
		if err != nil {
			t.Fatalf("expected echo with matching FEC config, got: %v", err)
		}
		if string(buf[:n]) != string(msg) {
			t.Fatalf("echo mismatch: %q", buf[:n])
		}
	} else {
		if err == nil && string(buf[:n]) == string(msg) {
			t.Fatal("echo unexpectedly succeeded across mismatched FEC framing")
		}
	}
}

func TestKcpFecLoopback(t *testing.T) {
	cfg := &KcpConfig{DataShards: 10, ParityShards: 3}
	kcpFecEcho(t, cfg, cfg, true)
}

// FEC framing is not wire-compatible with a FEC-less peer: a 10+3 client
// against a legacy listener must not exchange data. This locks the default
// to (0,0): enabling FEC by default would silently break mixed-version
// deployments.
func TestKcpFecMismatchNoTraffic(t *testing.T) {
	kcpFecEcho(t, nil, &KcpConfig{DataShards: 10, ParityShards: 3}, false)
}
