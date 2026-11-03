package graceful

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func waitUp(t *testing.T, addr string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server did not start")
}

// F44: a request in flight when the stop signal arrives runs to completion,
// and drain waits for background work, before ServeContext returns.
func TestServeContextFinishesInFlightWork(t *testing.T) {
	addr := freeAddr(t)
	entered := make(chan struct{})
	var finished, drained atomic.Bool
	srv := &http.Server{Addr: addr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		time.Sleep(300 * time.Millisecond) // e.g. AddTorrent then setting labels
		finished.Store(true)
		io.WriteString(w, "done")
	})}
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- ServeContext(ctx, srv, func(context.Context) {
			time.Sleep(100 * time.Millisecond)
			drained.Store(true)
		}, 2*time.Second)
	}()
	waitUp(t, addr)

	body := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + addr)
		if err != nil {
			body <- "error: " + err.Error()
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		body <- string(b)
	}()
	<-entered
	stop() // the SIGTERM

	if err := <-served; err != nil {
		t.Fatalf("ServeContext = %v, want nil", err)
	}
	if !finished.Load() || !drained.Load() {
		t.Fatalf("returned before in-flight work finished: handler %v, drain %v", finished.Load(), drained.Load())
	}
	if got := <-body; got != "done" {
		t.Fatalf("client got %q, want the full response", got)
	}
}

// A request that outlives the timeout does not hold the process forever.
func TestServeContextGivesUpAtTimeout(t *testing.T) {
	addr := freeAddr(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	srv := &http.Server{Addr: addr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
	})}
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- ServeContext(ctx, srv, nil, 200*time.Millisecond) }()
	waitUp(t, addr)
	go http.Get("http://" + addr)
	<-entered
	start := time.Now()
	stop()
	<-served
	if took := time.Since(start); took > time.Second {
		t.Fatalf("shutdown took %s with a 200 ms timeout", took)
	}
}

func TestServeContextReportsListenError(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	srv := &http.Server{Addr: l.Addr().String()}
	if err := ServeContext(context.Background(), srv, nil, time.Second); err == nil {
		t.Fatal("want an error for an address in use")
	}
}

// Serve itself reacts to a real SIGTERM, as docker stop sends it.
func TestServeStopsOnSIGTERM(t *testing.T) {
	addr := freeAddr(t)
	srv := &http.Server{Addr: addr, Handler: http.NotFoundHandler()}
	served := make(chan error, 1)
	go func() { served <- Serve(srv, nil) }()
	waitUp(t, addr) // Serve registers for signals before it listens
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after SIGTERM")
	}
}
