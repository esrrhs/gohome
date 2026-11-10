# WSS byte-stream transport

`NewConn("wss")` returns `*WssConn`. It uses the existing `Conn` factory
`Dial` / `Listen` / `Accept` interface and is registered as reliable.
No change is required for existing transports.

- `WssConfig.TLSConfig`: standard Go TLS configuration; system roots and URL
  hostname verification by default. Set `RootCAs` for a private CA, `ServerName`
  when dialing an IP for a named certificate, and `Certificates` or
  `GetCertificate` on the server. Optional mutual TLS uses Go's usual fields.
  `InsecureSkipVerify` is rejected; there is no plaintext fallback. TLS >= 1.2.
- `Path`: exact upgrade path, default `/gohome`. `Dial("host:port")` uses it;
  `Dial("wss://host:port/custom")` uses the explicit URL path. No redirects.
- `HandshakeTimeout`: default 10 seconds. `DialContext` adds caller cancellation
  for the handshake only. Closing a factory cancels its pending dials; returned
  connections are independently owned. Configure the factory **before** use.
- Each Write is one binary WebSocket message. Read exposes a byte stream across
  message/frame boundaries; empty messages do not cause zero-progress reads.
  Large messages stream without whole-message buffering. Concurrent reads and
  writes are safe, with same-direction operations serialized. Text messages
  are rejected. Compression is disabled.
- Concrete streams expose `SetDeadline`, `SetReadDeadline`, `SetWriteDeadline`.
  An active deadline expiry permanently closes the stream (coder/websocket
  NetConn semantics), unlike a TCP deadline which can usually be reset afterward.
  Deadline failures satisfy `errors.Is(err, os.ErrDeadlineExceeded)`.
- `Close` is idempotent, aborts IO immediately and does not drain pending data or
  wait for a WebSocket close handshake. Graceful peer close frames yield EOF;
  abrupt closes may yield an error. Closing a listener closes its active/queued
  streams and unblocks Accept. A full accept queue rejects the upgraded stream.
- `Addr()` on a listener reports the actual bound address (including port `0`).
  Client-side `Info()` uses the WebSocket adapter's synthetic remote address.

## Runnable local client/server

From the repository root, create a temporary development certificate (never use
this example key/certificate for production):

```sh
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -keyout /tmp/gohome-wss.key -out /tmp/gohome-wss.crt -days 1 \
  -subj /CN=localhost -addext subjectAltName=DNS:localhost
```

Save the following program as `/tmp/gohome-wss.go`, then run
`go run /tmp/gohome-wss.go` from the module root:

```go
package main

import (
    "crypto/tls"
    "crypto/x509"
    "fmt"
    "io"
    "log"
    "os"
    "time"

    "github.com/esrrhs/gohome/network"
)

func main() {
    cert, err := tls.LoadX509KeyPair("/tmp/gohome-wss.crt", "/tmp/gohome-wss.key")
    if err != nil { log.Fatal(err) }
    server, _ := network.NewConn("wss")
    sc := network.DefaultWssConfig()
    sc.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
    server.(*network.WssConn).SetConfig(sc)
    listener, err := server.Listen("127.0.0.1:0")
    if err != nil { log.Fatal(err) }
    defer listener.Close()
    done := make(chan error, 1)
    go func() {
        conn, err := listener.Accept()
        if err != nil { done <- err; return }
        defer conn.Close()
        conn.(*network.WssConn).SetDeadline(time.Now().Add(5*time.Second))
        buf := make([]byte, 5)
        if _, err = io.ReadFull(conn, buf); err == nil {
            _, err = conn.Write(buf)
        }
        done <- err
    }()
    pem, err := os.ReadFile("/tmp/gohome-wss.crt")
    if err != nil { log.Fatal(err) }
    roots := x509.NewCertPool()
    if !roots.AppendCertsFromPEM(pem) { log.Fatal("invalid CA") }
    client, _ := network.NewConn("wss")
    cc := network.DefaultWssConfig()
    cc.TLSConfig = &tls.Config{RootCAs: roots, ServerName: "localhost"}
    client.(*network.WssConn).SetConfig(cc)
    conn, err := client.Dial(listener.(*network.WssConn).Addr().String())
    if err != nil { log.Fatal(err) }
    defer conn.Close()
    conn.(*network.WssConn).SetDeadline(time.Now().Add(5*time.Second))
    if _, err = conn.Write([]byte("hello")); err != nil { log.Fatal(err) }
    buf := make([]byte, 5)
    if _, err = io.ReadFull(conn, buf); err != nil { log.Fatal(err) }
    if err = <-done; err != nil { log.Fatal(err) }
    fmt.Println(string(buf))
}
```

## Reverse proxy

A reverse proxy must forward HTTP/1.1 Upgrade and Connection headers, preserve
(or deliberately rewrite) the configured path, and permit long-lived tunnels.
This implementation intentionally requires **TLS on the upstream too**: use
`https://127.0.0.1:8443`, install the upstream CA in the proxy and verify its
certificate name. Example Nginx location inside an already configured TLS server:

```nginx
location = /gohome {
    proxy_pass https://127.0.0.1:8443;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_ssl_verify on;
    proxy_ssl_trusted_certificate /etc/nginx/upstream-ca.pem;
    proxy_ssl_server_name on;
    proxy_ssl_name tunnel.internal.example;
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
}
```

The WSS listener needs a certificate for `tunnel.internal.example`; the public
client verifies the proxy's public domain certificate. This supports reverse
proxies without exposing an unencrypted backend or disabling verification.
WSS encrypts application data, not IP addresses, TLS handshake metadata, traffic
sizes/timing, or the fact that a WebSocket tunnel exists. It does **not** promise
undetectability or bypass of every network restriction. Apply authentication,
access control and resource limits at the proxy; TLS server authentication alone
is not client authorization.
