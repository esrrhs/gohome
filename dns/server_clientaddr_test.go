package dns

import (
	"context"
	"net"
	"testing"
)

func TestClientAddrFromContext(t *testing.T) {
	if got := ClientAddrFromContext(context.Background()); got != nil {
		t.Fatalf("empty context: got %v, want nil", got)
	}
	addr := &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 53}
	ctx := ContextWithClientAddr(context.Background(), addr)
	got := ClientAddrFromContext(ctx)
	if got == nil {
		t.Fatal("ClientAddrFromContext returned nil after ContextWithClientAddr")
	}
	udp, ok := got.(*net.UDPAddr)
	if !ok || !udp.IP.Equal(addr.IP) || udp.Port != addr.Port {
		t.Fatalf("ClientAddrFromContext = %#v, want %#v", got, addr)
	}
}
