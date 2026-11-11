package network

import (
	"bytes"
	"github.com/miekg/dns"
	"google.golang.org/protobuf/proto"
	"reflect"
	"testing"
	"time"
)

func testRdnsConfig(domain string) *RdnsConfig {
	cfg := DefaultRdnsConfig()
	cfg.Domain = domain
	cfg.ConnectTimeoutMs = 8000
	cfg.ResendTimems = 200
	cfg.PollIntervalMs = 20
	cfg.CutSize = 64
	cfg.MaxWin = 500
	return cfg
}

func cloneRdnsConfig(cfg *RdnsConfig) *RdnsConfig {
	cp := *cfg
	return &cp
}

func TestRdnsCodecRoundTrip(t *testing.T) {
	domain := "tunnel.example.com"
	session := "abcd1234"
	payloads := [][]byte{nil, {}, []byte("hi"), bytes.Repeat([]byte{0xab}, 40)}
	for _, p := range payloads {
		sealed, err := rdnsSeal("secret", p)
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		qname, err := rdnsBuildQName(domain, session, sealed)
		if err != nil {
			t.Fatalf("build qname for %d bytes: %v", len(p), err)
		}
		gotSession, gotSealed, ok, err := rdnsParseQName(qname, domain)
		if err != nil || !ok {
			t.Fatalf("parse qname %q: ok=%v err=%v", qname, ok, err)
		}
		if gotSession != session {
			t.Fatalf("session=%q want %q", gotSession, session)
		}
		plain, err := rdnsOpen("secret", gotSealed)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if len(p) == 0 {
			if len(plain) != 0 {
				t.Fatalf("payload mismatch: got %v want empty", plain)
			}
			continue
		}
		if !bytes.Equal(plain, p) {
			t.Fatalf("payload mismatch: got %v want %v", plain, p)
		}
	}

	_, _, ok, _ := rdnsParseQName("www.example.com.", domain)
	if ok {
		t.Fatal("expected non-tunnel name to be rejected")
	}
}

func TestRdnsPackUnpackFrames(t *testing.T) {
	in := [][]byte{[]byte("a"), []byte("bb"), {}}
	blob := rdnsPackFrames(in)
	out, err := rdnsUnpackFrames(blob)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("len=%d", len(out))
	}
	if !bytes.Equal(out[0], []byte("a")) || !bytes.Equal(out[1], []byte("bb")) || len(out[2]) != 0 {
		t.Fatalf("unexpected %#v", out)
	}
}

func TestRdnsNewConnRegistered(t *testing.T) {
	c, err := NewConn("rdns")
	if err != nil || c == nil || c.Name() != "rdns" {
		t.Fatalf("NewConn rdns: conn=%v err=%v", c, err)
	}
	if !HasReliableProto("rdns") || !HasProto("rdns") {
		t.Fatal("rdns should be in Support* lists")
	}
}

func TestRdnsDialRequiresDomain(t *testing.T) {
	c, err := NewConn("rdns")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Dial("127.0.0.1:1")
	if err == nil {
		t.Fatal("expected domain required error")
	}
}

func TestRdnsLoopbackEcho(t *testing.T) {
	domain := "t.rdns.test"
	cfg := testRdnsConfig(domain)

	serverDriver, err := NewConn("rdns")
	if err != nil {
		t.Fatal(err)
	}
	serverDriver.(*RdnsConn).SetConfig(cloneRdnsConfig(cfg))

	listener, err := serverDriver.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	dnsAddr := listener.(*RdnsConn).listener.listenerconn.LocalAddr().String()

	acceptedCh := make(chan Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		a, err := listener.Accept()
		if err != nil {
			errCh <- err
			return
		}
		acceptedCh <- a
	}()

	clientDriver, err := NewConn("rdns")
	if err != nil {
		t.Fatal(err)
	}
	clientDriver.(*RdnsConn).SetConfig(cloneRdnsConfig(cfg))
	client, err := clientDriver.Dial(dnsAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	var accepted Conn
	select {
	case accepted = <-acceptedCh:
	case err := <-errCh:
		t.Fatalf("accept: %v", err)
	case <-time.After(8 * time.Second):
		t.Fatal("accept timeout")
	}
	defer accepted.Close()

	msg := []byte("hello-rdns")
	writeErr := make(chan error, 1)
	go func() {
		_, err := client.Write(msg)
		writeErr <- err
	}()

	buf := make([]byte, 64)
	got := make([]byte, 0, len(msg))
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for len(got) < len(msg) {
			n, err := accepted.Read(buf)
			if err != nil {
				return
			}
			got = append(got, buf[:n]...)
		}
	}()

	select {
	case <-readDone:
	case <-time.After(8 * time.Second):
		t.Fatal("server read timeout")
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("client write: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("server got %q want %q", got, msg)
	}

	reply := []byte("pong-rdns")
	go func() { _, _ = accepted.Write(reply) }()
	got = nil
	buf = make([]byte, 64)
	deadline := time.After(8 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("client got %q want %q", got, reply)
		default:
		}
		n, err := client.Read(buf)
		if err != nil {
			t.Fatalf("client read: %v", err)
		}
		got = append(got, buf[:n]...)
		if bytes.Equal(got, reply) {
			break
		}
	}
}

func TestRdnsLoopbackWithPSK(t *testing.T) {
	domain := "secure.rdns.test"
	cfg := testRdnsConfig(domain)
	cfg.PSK = "test-psk-please-change"

	serverDriver, _ := NewConn("rdns")
	serverDriver.(*RdnsConn).SetConfig(cloneRdnsConfig(cfg))
	listener, err := serverDriver.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	dnsAddr := listener.(*RdnsConn).listener.listenerconn.LocalAddr().String()

	acceptedCh := make(chan Conn, 1)
	go func() {
		a, err := listener.Accept()
		if err == nil {
			acceptedCh <- a
		}
	}()

	clientDriver, _ := NewConn("rdns")
	clientDriver.(*RdnsConn).SetConfig(cloneRdnsConfig(cfg))
	client, err := clientDriver.Dial(dnsAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	var accepted Conn
	select {
	case accepted = <-acceptedCh:
	case <-time.After(8 * time.Second):
		t.Fatal("accept timeout")
	}
	defer accepted.Close()

	payload := bytes.Repeat([]byte("x"), 200)
	go func() { _, _ = client.Write(payload) }()

	got := make([]byte, 0, len(payload))
	buf := make([]byte, 128)
	deadline := time.After(10 * time.Second)
	for len(got) < len(payload) {
		select {
		case <-deadline:
			t.Fatalf("psk transfer mismatch: got %d want %d", len(got), len(payload))
		default:
		}
		n, err := accepted.Read(buf)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		got = append(got, buf[:n]...)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("psk transfer mismatch")
	}
}

func TestRdnsNULLQueryType(t *testing.T) {
	domain := "null.rdns.test"
	cfg := testRdnsConfig(domain)
	cfg.QueryType = "NULL"

	serverDriver, _ := NewConn("rdns")
	serverDriver.(*RdnsConn).SetConfig(cloneRdnsConfig(cfg))
	listener, err := serverDriver.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	dnsAddr := listener.(*RdnsConn).listener.listenerconn.LocalAddr().String()

	acceptedCh := make(chan Conn, 1)
	go func() {
		a, err := listener.Accept()
		if err == nil {
			acceptedCh <- a
		}
	}()

	clientDriver, _ := NewConn("rdns")
	clientDriver.(*RdnsConn).SetConfig(cloneRdnsConfig(cfg))
	client, err := clientDriver.Dial(dnsAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	var accepted Conn
	select {
	case accepted = <-acceptedCh:
	case <-time.After(8 * time.Second):
		t.Fatal("accept timeout")
	}
	defer accepted.Close()

	msg := []byte("null-rr")
	go func() { _, _ = client.Write(msg) }()
	buf := make([]byte, 32)
	got := make([]byte, 0, len(msg))
	deadline := time.After(8 * time.Second)
	for len(got) < len(msg) {
		select {
		case <-deadline:
			t.Fatalf("got %q", got)
		default:
		}
		n, err := accepted.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, buf[:n]...)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("got %q", got)
	}
}

// FrameMgr may recycle DATA pointers on its next Update. RDNS must own bytes,
// deduplicate staged retransmits, and split ACK/REQ ID lists for tiny QNAMEs.
func TestRdnsUplinkQueueOwnershipAndBudget(t *testing.T) {
	fm := NewFrameMgr(32, 100000, 4096, 64, 200, 0, 0)
	c := &RdnsConn{dialer: &rdnsConnDialer{fm: fm, rdnsFrameQueue: rdnsFrameQueue{maxFrame: 64, pending: make(map[rdnsFrameKey][]byte)}}}
	f := &Frame{Type: int32(Frame_DATA), Id: 7, Data: &FrameData{Data: []byte("original payload")}}
	fm.sendFrame(f)
	fm.sendFrame(f)
	if err := c.queueUplink(); err != nil {
		t.Fatal(err)
	}
	if len(c.dialer.order) != 1 {
		t.Fatalf("duplicate DATA staged %d times", len(c.dialer.order))
	}
	f.Data.Data[0] = 'X'
	var got Frame
	if err := proto.Unmarshal(c.dialer.pending[c.dialer.order[0]], &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Data.Data) != "original payload" {
		t.Fatal("queue retained pooled frame memory")
	}
	ids := make([]int32, 300)
	for i := range ids {
		ids[i] = int32(i + 1000)
	}
	for _, kind := range []Frame_TYPE{Frame_ACK, Frame_REQ} {
		fm.sendFrame(&Frame{Type: int32(kind), Id: 99, Dataid: ids})
		if err := c.queueUplink(); err != nil {
			t.Fatal(err)
		}
		var all []int32
		for _, key := range c.dialer.order {
			if key.kind != int32(kind) {
				continue
			}
			wire := c.dialer.pending[key]
			if len(wire) > 64 {
				t.Fatal("oversized frame")
			}
			var part Frame
			if err := proto.Unmarshal(wire, &part); err != nil {
				t.Fatal(err)
			}
			all = append(all, part.Dataid...)
		}
		if !reflect.DeepEqual(ids, all) {
			t.Fatal("split control IDs lost or reordered")
		}
	}
}

// DNS IDs repeat on redial. A response for another session must never feed
// FrameMgr, even when it has a valid PSK and happens to match the current ID.
func TestRdnsResponseSessionValidation(t *testing.T) {
	query := new(dns.Msg)
	query.SetQuestion("payload.s.current.rdns.test.", dns.TypeTXT)
	response := new(dns.Msg)
	response.SetReply(query)
	if !rdnsMatchesResponse(query, response) {
		t.Fatal("valid reply rejected")
	}
	for _, name := range []string{"id", "session", "querytype", "queryclass", "rcode", "notresponse", "noquestion"} {
		t.Run(name, func(t *testing.T) {
			bad := response.Copy()
			switch name {
			case "id":
				bad.Id++
			case "session":
				bad.Question[0].Name = "payload.s.previous.rdns.test."
			case "querytype":
				bad.Question[0].Qtype = dns.TypeNULL
			case "queryclass":
				bad.Question[0].Qclass = dns.ClassCHAOS
			case "rcode":
				bad.Rcode = dns.RcodeRefused
			case "notresponse":
				bad.Response = false
			case "noquestion":
				bad.Question = nil
			}
			if rdnsMatchesResponse(query, bad) {
				t.Fatal("mismatched DNS reply accepted")
			}
		})
	}
}

func TestRdnsUplinkRetransmitFairness(t *testing.T) {
	q := rdnsFrameQueue{pending: make(map[rdnsFrameKey][]byte), sent: map[int32]bool{1: true}}
	for id := int32(2); id <= 100; id++ {
		key := rdnsFrameKey{kind: int32(Frame_DATA), id: id}
		q.order = append(q.order, key)
		q.pending[key] = []byte{byte(id)}
	}
	lost := rdnsFrameKey{kind: int32(Frame_DATA), id: 1}
	q.order = append(q.order, lost)
	q.pending[lost] = []byte{1}
	if got := q.pop(); !bytes.Equal(got, []byte{2}) {
		t.Fatalf("new data starved: %v", got)
	}
	if got := q.pop(); !bytes.Equal(got, []byte{1}) {
		t.Fatalf("retransmit stuck behind window: %v", got)
	}
	if got := q.pop(); !bytes.Equal(got, []byte{3}) {
		t.Fatalf("FIFO service lost: %v", got)
	}
}
