package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// F42: an SSE handler behind accessLog can flush, so the headers and each
// event reach the client while the stream is still open.
func TestAccessLogKeepsSSEFlushing(t *testing.T) {
	release := make(chan struct{})
	flushErr := make(chan error, 1)
	srv := httptest.NewServer(accessLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: hello\n\n"))
		flushErr <- http.NewResponseController(w).Flush()
		<-release // hold the stream open, as the standalone GET stream does
	})))
	defer srv.Close()
	defer close(release)

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("headers did not arrive while the stream was open: %v", err)
	}
	defer resp.Body.Close()
	if err := <-flushErr; err != nil {
		t.Fatalf("Flush through accessLog: %v", err)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "data: hello" {
		t.Fatalf("first event = %q, %v", line, err)
	}
}

func TestStatusRecorderFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	var w http.ResponseWriter = &statusRecorder{ResponseWriter: rec}
	f, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("statusRecorder is not an http.Flusher")
	}
	f.Flush()
	if !rec.Flushed {
		t.Fatal("Flush was not forwarded")
	}
}

// F90: the preflight allows what Streamable HTTP clients send: DELETE to end
// a session and Last-Event-ID to resume a stream.
func TestCORSPreflightAllowsStreamableHTTP(t *testing.T) {
	h := corsMiddleware(http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://claude.ai")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if m := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(m, "DELETE") {
		t.Errorf("Allow-Methods = %q, want DELETE", m)
	}
	if hd := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(hd, "Last-Event-ID") {
		t.Errorf("Allow-Headers = %q, want Last-Event-ID", hd)
	}
	if e := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(e, "Mcp-Protocol-Version") {
		t.Errorf("Expose-Headers = %q, want Mcp-Protocol-Version", e)
	}
}

// F122: an open event stream does not hold up shutdown, while a tool call
// in progress still finishes.
func TestShutdownEndsStreamsAndFinishesCalls(t *testing.T) {
	streams := newStreamCloser()
	callDone := make(chan struct{})
	handler := streams.streams(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet { // an event stream: never ends by itself
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		time.Sleep(300 * time.Millisecond) // a tool call in progress
		close(callDone)
		io.WriteString(w, "done")
	}))
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	srv.RegisterOnShutdown(streams.closeAll)
	go srv.Serve(l)
	base := "http://" + l.Addr().String()

	stream, err := http.Get(base) // headers arrive, body stays open
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	callResp := make(chan string, 1)
	go func() {
		resp, err := http.Post(base, "application/json", strings.NewReader("{}"))
		if err != nil {
			callResp <- err.Error()
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		callResp <- string(b)
	}()
	time.Sleep(50 * time.Millisecond) // the call is in progress

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown = %v after %s, want a clean shutdown", err, time.Since(start))
	}
	select {
	case <-callDone:
	default:
		t.Fatal("Shutdown returned before the tool call finished")
	}
	if got := <-callResp; got != "done" {
		t.Fatalf("tool call got %q, want its full response", got)
	}
}
