package main

import (
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// F85: a slow route that extends its write deadline still delivers its
// response after the server's WriteTimeout, instead of the client seeing a
// dropped connection for work that succeeded.
func TestExtendWriteDeadline(t *testing.T) {
	for _, tc := range []struct {
		name   string
		extend bool
		want   bool
	}{{"extended", true, true}, {"not extended", false, false}} {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{WriteTimeout: 200 * time.Millisecond, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.extend {
				extendWriteDeadline(w, 2*time.Second)
			}
			time.Sleep(400 * time.Millisecond) // rutracker + Transmission taking longer than WriteTimeout
			io.WriteString(w, `{"success":true}`)
		})}
		go srv.Serve(l)

		resp, err := http.Get("http://" + l.Addr().String())
		got := false
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			got = string(b) == `{"success":true}`
		}
		srv.Close()
		if got != tc.want {
			t.Errorf("%s: response delivered = %v (err %v), want %v", tc.name, got, err, tc.want)
		}
	}
}
