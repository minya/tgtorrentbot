package main

import (
	"net/http"
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
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Session-Id, Mcp-Protocol-Version")
			w.Header().Set("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id")
			w.Header().Set("Access-Control-Max-Age", "3600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id")
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
