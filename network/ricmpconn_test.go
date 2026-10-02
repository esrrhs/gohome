package network

import (
	"fmt"
	"net"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/esrrhs/gohome/loggo"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"google.golang.org/protobuf/proto"
)

func Test000RICMP(t *testing.T) {
	c, err := NewConn("ricmp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen(":58083")
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

func Test0002RICMP(t *testing.T) {
	c, err := NewConn("ricmp")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		conn, err := c.Dial("9.9.9.9")
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

func Test0003RICMP(t *testing.T) {
	c, err := NewConn("ricmp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial("127.0.0.1")
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

func Test0004RICMP(t *testing.T) {
	c, err := NewConn("ricmp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		cc.Accept()
		fmt.Println("accept done")
	}()

	ccc, err := c.Dial("127.0.0.1")
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

func Test0005RICMP(t *testing.T) {
	c, err := NewConn("ricmp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1")
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

	ccc, err := c.Dial("127.0.0.1")
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

	time.Sleep(time.Second * 5)

	cc.Close()
	ccc.Close()

	exit.Store(true)

	time.Sleep(time.Second)
}

func Test0005RICMP1(t *testing.T) {
	c, err := NewConn("ricmp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1")
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

	ccc, err := c.Dial("127.0.0.1")
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

func Test0006RICMP(t *testing.T) {
	c, err := NewConn("ricmp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1")
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

	ccc, err := c.Dial("127.0.0.1")
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

func Test0007RICMP(t *testing.T) {

	c, err := NewConn("ricmp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("127.0.0.1")
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

	ccc, err := c.Dial("127.0.0.1")
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

func Test0008RICMP(t *testing.T) {
	c, err := NewConn("ricmp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("0.0.0.0")
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

	ccc, err := c.Dial("127.0.0.1")
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
			if err != nil {
				fmt.Println(err)
				fmt.Println("Read done")
				return
			}
			//fmt.Println("end Read")
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

func Test0009RICMP(t *testing.T) {
	c, err := NewConn("ricmp")
	if err != nil {
		fmt.Println(err)
		return
	}

	cc, err := c.Listen("0.0.0.0")
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
		buf := make([]byte, 1024*1024)
		start := time.Now()
		speed := 0
		for !exit.Load() {
			//fmt.Println("start Read")
			n, err := cc.Read(buf)
			if err != nil {
				fmt.Println(err)
				fmt.Println("Read done")
				return
			}
			//fmt.Println("end Read")
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

	ccc, err := c.Dial("127.0.0.1")
	if err != nil {
		fmt.Println(err)
		return
	}

	go func() {
		fmt.Println("start client")
		data := make([]byte, 1024*1024)
		start := time.Now()
		speed := 0
		for !exit.Load() {
			//fmt.Println("start Write")
			_, err := ccc.Write(data)
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

	time.Sleep(time.Second * 10)

	cc.Close()
	ccc.Close()

	exit.Store(true)

	time.Sleep(time.Second)
}

func TestDecodeIcmpPacketPayloadShort(t *testing.T) {
	for _, n := range []int{0, 1, 7} {
		_, _, _, _, _, err := decodeIcmpPacketPayload(make([]byte, n))
		if err == nil {
			t.Fatalf("len=%d: expected error", n)
		}
	}
}

func TestDecodeIcmpPacketPayloadOK(t *testing.T) {
	msg := &IcmpMsg{
		Id:    "conn-id-1",
		Data:  []byte("frame-bytes"),
		Magic: IcmpMsg_MAGIC,
		Flag:  IcmpMsg_CLIENT_SEND_FLAG,
	}
	body, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, 8+len(body))
	packet[4], packet[5] = 0x12, 0x34 // echo id
	packet[6], packet[7] = 0x00, 0x09 // echo seq
	copy(packet[8:], body)

	payload, id, echoId, echoSeq, flag, err := decodeIcmpPacketPayload(packet)
	if err != nil {
		t.Fatal(err)
	}
	if id != "conn-id-1" || string(payload) != "frame-bytes" {
		t.Fatalf("id/payload mismatch: %q %q", id, payload)
	}
	if echoId != 0x1234 || echoSeq != 9 || flag != int(IcmpMsg_CLIENT_SEND_FLAG) {
		t.Fatalf("header mismatch: id=%d seq=%d flag=%d", echoId, echoSeq, flag)
	}
}

func TestDecodeIcmpPacketPayloadBadMagic(t *testing.T) {
	msg := &IcmpMsg{Id: "x", Data: []byte("y"), Magic: 0, Flag: IcmpMsg_CLIENT_SEND_FLAG}
	body, _ := proto.Marshal(msg)
	packet := make([]byte, 8+len(body))
	copy(packet[8:], body)
	if _, _, _, _, _, err := decodeIcmpPacketPayload(packet); err == nil {
		t.Fatal("expected magic error")
	}
}

func TestRicmpAcceptImmediateReadWrite(t *testing.T) {
	c, err := NewConn("ricmp")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := c.Listen("127.0.0.1")
	if err != nil {
		// CI runners usually lack CAP_NET_RAW for ip4:icmp.
		t.Skipf("ricmp listen unavailable: %v", err)
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
	cli, err := c.Dial("127.0.0.1")
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

	msg := []byte("ricmp-accept-ready")
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

func TestRicmpAcceptNotListen(t *testing.T) {
	c, err := NewConn("ricmp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Accept(); err == nil {
		t.Fatal("Accept on non-listener should fail")
	}
}

func TestRicmpFamilyHelpers(t *testing.T) {
	if got := familyFromIP(net.ParseIP("127.0.0.1")); got != icmpFamilyV4 {
		t.Fatalf("127.0.0.1 family=%d want v4", got)
	}
	if got := familyFromIP(net.ParseIP("::1")); got != icmpFamilyV6 {
		t.Fatalf("::1 family=%d want v6", got)
	}
	// IPv4-mapped IPv6 must be carried over an ICMPv4 raw socket.
	if got := familyFromIP(net.ParseIP("::ffff:1.2.3.4")); got != icmpFamilyV4 {
		t.Fatalf("::ffff:1.2.3.4 family=%d want v4", got)
	}

	if got := icmpNetwork(icmpFamilyV4); got != "ip4:icmp" {
		t.Fatalf("v4 network=%q", got)
	}
	if got := icmpNetwork(icmpFamilyV6); got != "ip6:icmp" {
		t.Fatalf("v6 network=%q", got)
	}

	cases := []struct {
		icmpProto int
		family    int
		want      icmpTypeLike
	}{
		{int(IcmpMsg_PING_PROTO), icmpFamilyV4, icmpTypeLike{"ipv4", int(ipv4.ICMPTypeEcho)}},
		{int(IcmpMsg_PONG_PROTO), icmpFamilyV4, icmpTypeLike{"ipv4", int(ipv4.ICMPTypeEchoReply)}},
		{int(IcmpMsg_PING_PROTO), icmpFamilyV6, icmpTypeLike{"ipv6", int(ipv6.ICMPTypeEchoRequest)}},
		{int(IcmpMsg_PONG_PROTO), icmpFamilyV6, icmpTypeLike{"ipv6", int(ipv6.ICMPTypeEchoReply)}},
	}
	for _, tc := range cases {
		got := icmpEchoType(tc.icmpProto, tc.family)
		var gotKind string
		switch got.(type) {
		case ipv4.ICMPType:
			gotKind = "ipv4"
		case ipv6.ICMPType:
			gotKind = "ipv6"
		}
		if gotKind != tc.want.kind {
			t.Fatalf("proto=%d family=%d kind=%s want %s", tc.icmpProto, tc.family, gotKind, tc.want.kind)
		}
		if typeCode(got) != tc.want.code {
			t.Fatalf("proto=%d family=%d type=%d want %d", tc.icmpProto, tc.family, typeCode(got), tc.want.code)
		}
	}
}

type icmpTypeLike struct {
	kind string
	code int
}

func typeCode(t icmp.Type) int {
	switch v := t.(type) {
	case ipv4.ICMPType:
		return int(v)
	case ipv6.ICMPType:
		return int(v)
	}
	return -1
}

func TestParseRicmpListenAddr(t *testing.T) {
	cases := []struct {
		dst       string
		wantFam   []int
		wantBind  string
		wantError bool
	}{
		{"", []int{icmpFamilyV4, icmpFamilyV6}, "", false},
		{"  ", []int{icmpFamilyV4, icmpFamilyV6}, "", false},
		{"0.0.0.0", []int{icmpFamilyV4, icmpFamilyV6}, "", false},
		{"::", []int{icmpFamilyV4, icmpFamilyV6}, "", false},
		{"127.0.0.1", []int{icmpFamilyV4}, "127.0.0.1", false},
		{"::1", []int{icmpFamilyV6}, "::1", false},
		{":8888", nil, "", true},          // ricmp has no port
		{"127.0.0.1:8888", nil, "", true}, // host:port is invalid
		{"example.com", nil, "", true},    // hostnames are not allowed at listen
	}
	for _, tc := range cases {
		fams, bind, err := parseRicmpListenAddr(tc.dst)
		if tc.wantError {
			if err == nil {
				t.Fatalf("dst=%q expected error, got families=%v bind=%q", tc.dst, fams, bind)
			}
			continue
		}
		if err != nil {
			t.Fatalf("dst=%q unexpected error: %v", tc.dst, err)
		}
		if !reflect.DeepEqual(fams, tc.wantFam) {
			t.Fatalf("dst=%q families=%v want %v", tc.dst, fams, tc.wantFam)
		}
		if bind != tc.wantBind {
			t.Fatalf("dst=%q bind=%q want %q", tc.dst, bind, tc.wantBind)
		}
	}
}

func TestRicmpAcceptImmediateReadWriteIPv6(t *testing.T) {
	if _, err := net.ResolveIPAddr("ip6", "::1"); err != nil {
		t.Skipf("IPv6 unavailable: %v", err)
	}
	c, err := NewConn("ricmp")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := c.Listen("::1")
	if err != nil {
		// Raw ip6:icmp requires root/CAP_NET_RAW (or a permissive stack).
		t.Skipf("ricmp ipv6 listen unavailable: %v", err)
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
	cli, err := c.Dial("::1")
	if err != nil {
		t.Fatalf("Dial ::1: %v", err)
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

	msg := []byte("ricmp6-accept-ready")
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

// runRicmpDgramClientLoopback starts a privileged raw listener (which skips
// the test without CAP_NET_RAW) and forces the client onto the unprivileged
// datagram socket, covering the mixed raw-server / ping-socket-client path
// including echo-id translation. Only IPv4 has this mixed path: on Linux
// dgram-sent ICMPv6 echo requests do not reach raw v6 listeners, and Darwin
// has no ICMPv6 dgram support.
func runRicmpDgramClientLoopback(t *testing.T, host, label string) {
	t.Helper()
	if _, err := net.ResolveIPAddr("ip", host); err != nil {
		t.Skipf("%s unavailable: %v", label, err)
	}
	c, err := NewConn("ricmp")
	if err != nil {
		t.Fatal(err)
	}
	rc0, ok := c.(*RicmpConn)
	if !ok {
		t.Fatalf("expected *RicmpConn, got %T", c)
	}
	ln, err := rc0.Listen(host)
	if err != nil {
		t.Skipf("ricmp %s raw listen unavailable: %v", label, err)
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
	// Force the unprivileged datagram socket on the Dial side. On kernels
	// that disallow ping sockets (Linux net.ipv4.ping_group_range excludes
	// the runner group, or a platform without SOCK_DGRAM ICMP support),
	// skip instead of failing: the privileged raw listener is up, so this
	// reports an environment limitation, not a code regression.
	cli, err := rc0.dial(host, true)
	if err != nil {
		t.Skipf("dgram ping socket unavailable on %s (check ping_group_range / platform support): %v", label, err)
	}
	defer cli.Close()
	rc := cli.(*RicmpConn)
	if rc.dialer == nil || rc.dialer.mode != icmpModeDgram {
		t.Fatalf("expected datagram dialer, got %+v", cli)
	}

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

	msg := []byte("ricmp dgram client " + label)
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

func TestRicmpDgramClientIPv4(t *testing.T) {
	runRicmpDgramClientLoopback(t, "127.0.0.1", "ipv4")
}
