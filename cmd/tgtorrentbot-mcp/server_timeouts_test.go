package main

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// F71: a client that never finishes its request headers is disconnected
// after readHeaderTimeout instead of holding the connection forever.
func TestHTTPServerDropsSlowHeaders(t *testing.T) {
	old := readHeaderTimeout
	readHeaderTimeout = 300 * time.Millisecond
	t.Cleanup(func() { readHeaderTimeout = old })

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := newHTTPServer(l.Addr().String(), http.NotFoundHandler())
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n")) // headers never finish

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	start := time.Now()
	_, err = io.ReadAll(conn) // returns when the server closes the connection
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("connection still open after %s (err %v)", took, err)
	}
}
