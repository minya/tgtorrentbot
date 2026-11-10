package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/minya/logger"
)

// corsMiddleware permits cross-origin calls from any origin on the OAuth and
// MCP endpoints. The MCP spec allows browser-based clients, and claude.ai's
// connector runs OAuth flows through the browser: without these headers the
// browser silently drops successful responses from /token and /mcp.
//
// We don't gate on Origin: the endpoints are already authenticated (JWT on
// /mcp, PKCE+secret on /authorize, DCR is public-by-design).
// corsExposedHeaders are the response headers browser MCP clients must read.
const corsExposedHeaders = "WWW-Authenticate, Mcp-Session-Id, Mcp-Protocol-Version"

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		// Streamable HTTP uses DELETE to end a session and Last-Event-ID to
		// resume an SSE stream; browsers block both unless allowed here.
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Session-Id, Mcp-Protocol-Version, Last-Event-ID")
			w.Header().Set("Access-Control-Expose-Headers", corsExposedHeaders)
			w.Header().Set("Access-Control-Max-Age", "3600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Access-Control-Expose-Headers", corsExposedHeaders)
		next.ServeHTTP(w, r)
	})
}

// statusRecorder wraps ResponseWriter to capture the status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap exposes the wrapped writer to http.ResponseController, which the
// MCP SDK uses to flush every SSE event. Without it the flush fails with
// http.ErrNotSupported and events sit in the server's buffer until the
// handler returns.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Flush forwards to the wrapped writer, for code that type-asserts
// http.Flusher instead of using http.ResponseController.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// accessLog logs every request at info level with method, path, status, and
// elapsed time. Keeps the OAuth flow debuggable in prod without a debugger.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("%s %s -> %d (%s) ua=%q",
			r.Method, r.URL.RequestURI(), rec.status,
			time.Since(start).Round(time.Millisecond), r.Header.Get("User-Agent"))
	})
}

// streamCloser ends long-lived event streams when the server shuts down.
// A Streamable HTTP client holds a GET event stream open for as long as it
// is connected; http.Server.Shutdown waits for every active request, so
// that stream made every shutdown run into its deadline and then
// force-close the connections, in-flight tool calls included. Wrap the
// handler with streams() and register closeAll with RegisterOnShutdown:
// GET streams then end at once while POST requests (tool calls) finish.
type streamCloser struct {
	mu      sync.Mutex
	cancels map[*context.CancelFunc]struct{}
	closed  bool
}

func newStreamCloser() *streamCloser {
	return &streamCloser{cancels: map[*context.CancelFunc]struct{}{}}
}

func (c *streamCloser) streams(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			http.Error(w, "shutting down", http.StatusServiceUnavailable)
			return
		}
		c.cancels[&cancel] = struct{}{}
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			delete(c.cancels, &cancel)
			c.mu.Unlock()
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// closeAll ends every open stream and refuses new ones.
func (c *streamCloser) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for cancel := range c.cancels {
		(*cancel)()
	}
}
