package network

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
)

// Temporary debug probe: raw ICMPv6 echo server + unprivileged dgram client.
func TestRicmpDgramV6DebugDump(t *testing.T) {
	host := "::1"
	srv, err := icmp.ListenPacket("ip6:ipv6-icmp", host)
	if err != nil {
		t.Skipf("raw listen unavailable: %v", err)
	}
	defer srv.Close()
	go func() {
		buf := make([]byte, 512)
		for {
			n, src, rerr := srv.ReadFrom(buf)
			if rerr != nil {
				return
			}
			out := append([]byte{}, buf[:n]...)
			out[0] = byte(ipv6.ICMPTypeEchoReply) // 128 -> 129; kernel recomputes checksum
			_, _ = srv.WriteTo(out, src)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	dgramAddr := dgramBindAddress(icmpFamilyV6, &net.IPAddr{IP: net.ParseIP(host)})
	t.Logf("dgramBindAddress=%q", dgramAddr)
	cli, err := icmp.ListenPacket("udp6", dgramAddr)
	if err != nil {
		t.Skipf("udp6 listen: %v", err)
	}
	defer cli.Close()
	t.Logf("client LocalAddr=%#v", cli.LocalAddr())

	ua := cli.LocalAddr().(*net.UDPAddr)
	id := 12345
	if ua.Port > 0 {
		id = ua.Port
	}
	t.Logf("using echo id=%d (local port=%d)", id, ua.Port)

	dst := &net.UDPAddr{IP: net.ParseIP(host)}
	body := []byte("DEBUG-PAYLOAD")

	go func() {
		buf := make([]byte, 512)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			_ = cli.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, src, rerr := cli.ReadFrom(buf)
			if rerr != nil {
				continue
			}
			echoID := -1
			if n >= 8 {
				echoID = int(binary.BigEndian.Uint16(buf[4:6]))
			}
			head := n
			if head > 24 {
				head = 24
			}
			t.Logf("client recv n=%d src=%v type=%d echoId=%d head=% x", n, src, buf[0], echoID, buf[:head])
		}
	}()

	for seq := 0; seq < 10; seq++ {
		msg := &icmp.Message{
			Type: ipv6.ICMPTypeEchoRequest, Code: 0,
			Body: &icmp.Echo{ID: id, Seq: seq, Data: body},
		}
		out, merr := msg.Marshal(nil)
		if merr != nil {
			t.Fatal(merr)
		}
		if _, werr := cli.WriteTo(out, dst); werr != nil {
			t.Logf("write err: %v", werr)
		}
		time.Sleep(300 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
}
