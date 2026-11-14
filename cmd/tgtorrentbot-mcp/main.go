package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/minya/logger"
	"github.com/minya/tgtorrentbot/internal/graceful"
	"github.com/minya/tgtorrentbot/internal/mediatools"
	"github.com/minya/tgtorrentbot/internal/transmissionrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// server holds shared dependencies used by the OAuth endpoints.
type server struct {
	config    Config
	codeStore *codeStore
	// loginLimiter caps login attempts per client IP; failedLoginLimiter
	// caps failed logins across all clients (see rateLimiter).
	loginLimiter       *rateLimiter
	failedLoginLimiter *rateLimiter
}

func main() {
	logLevel := "info"
	if v := os.Getenv("TGT_LOGLEVEL"); v != "" {
		logLevel = v
	}
	logger.InitLogger(logger.Config{
		Level:  logLevel,
		Pretty: true,
		Output: os.Stderr, // stdout is reserved for MCP framing
	})

	cfg, err := loadConfig()
	if err != nil {
		logger.Error(err, "Failed to load config")
		os.Exit(1)
	}

	srv := &server{
		config:             cfg,
		codeStore:          newCodeStore(),
		loginLimiter:       newRateLimiter(10, time.Minute),
		failedLoginLimiter: newRateLimiter(20, time.Minute),
	}
	// A nil interface, not a typed nil, when Transmission is unavailable.
	var trans mediatools.TorrentClient
	if cfg.TransmissionAddr != "" {
		t, err := transmissionrpc.New(
			cfg.TransmissionAddr,
			cfg.TransmissionUser,
			cfg.TransmissionPassword,
		)
		if err != nil {
			logger.Error(err, "Failed to create Transmission client; list_media will return filesystem items only")
		} else {
			trans = t
		}
	}
	tools := mediatools.New(cfg.Media, trans)

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "tgtorrentbot-mcp",
		Version: "0.1.0",
	}, nil)

	tools.Register(mcpServer)

	ctx := context.Background()
	if cfg.AuthToken != "" {
		const addr = ":8080"
		mcpHandler := newMCPHandler(mcpServer)

		mux := http.NewServeMux()
		mux.HandleFunc("/.well-known/oauth-authorization-server", srv.handleAuthServerMetadata)
		mux.HandleFunc("/.well-known/oauth-protected-resource", srv.handleProtectedResourceMetadata)
		mux.HandleFunc("/register", srv.handleRegister)
		mux.HandleFunc("/authorize", srv.handleAuthorize)
		mux.HandleFunc("/token", srv.handleToken)
		// MCP at root: matches whatever isn't a more-specific OAuth route.
		// The resource URL advertised in protected-resource metadata is the
		// base URL, so Claude's connector POSTs MCP here.
		mux.Handle("/", srv.jwtAuth(mcpHandler))

		logger.Info("starting MCP HTTP server on %s (OAuth 2.1)", addr)
		streams := newStreamCloser()
		httpSrv := newHTTPServer(addr, accessLog(corsMiddleware(streams.streams(mux))))
		// On SIGTERM, let in-flight tool calls finish (move_media removes the
		// torrent, then renames) before exiting; open event streams, which
		// would never finish, are ended first.
		httpSrv.RegisterOnShutdown(streams.closeAll)
		if err := graceful.Serve(graceful.SignalContext(), httpSrv, nil); err != nil {
			logger.Error(err, "MCP HTTP server exited with error")
			os.Exit(1)
		}
		return
	}

	// Without TGT_MCP_TOKEN the server speaks MCP over stdin/stdout, which
	// only works with a client attached (Claude Code over SSH). A container
	// has /dev/null on stdin, so the server would read EOF and exit at once,
	// and Docker would restart it in a silent loop. Say why instead.
	if stdinIsDevNull() {
		logger.Error(nil, "stdin is /dev/null, so the stdio transport has no client. Set TGT_MCP_TOKEN and TGT_MCP_PUBLIC_URL to serve MCP over HTTP (e.g. MCP_TOKEN in the compose .env), or run this binary from an MCP client")
		os.Exit(1)
	}
	if err := runStdio(ctx, mcpServer, os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, errNoStdioClient) {
			logger.Error(nil, "stdin closed before any MCP message arrived, so the stdio transport has no client. Set TGT_MCP_TOKEN and TGT_MCP_PUBLIC_URL to serve MCP over HTTP (e.g. MCP_TOKEN in the compose .env), or run this binary from an MCP client")
		} else {
			logger.Error(err, "MCP server exited with error")
		}
		os.Exit(1)
	}
}

// errNoStdioClient means stdin ended before a single byte arrived.
var errNoStdioClient = errors.New("no MCP client on stdin")

// runStdio serves MCP over in/out. A session whose stdin ends before any
// byte was read had no client: a container without TGT_MCP_TOKEN, whatever
// its stdin is. That is reported as errNoStdioClient, so the container
// exits with an explanation instead of looping silently; stdinIsDevNull
// is only the early, common case of it.
func runStdio(ctx context.Context, server *mcp.Server, in io.Reader, out io.Writer) error {
	counted := &countingReader{r: in}
	err := server.Run(ctx, &mcp.IOTransport{Reader: io.NopCloser(counted), Writer: nopWriteCloser{out}})
	if counted.n.Load() == 0 {
		return errNoStdioClient
	}
	return err
}

type countingReader struct {
	r io.Reader
	n atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// stdinIsDevNull reports whether stdin is the null device.
func stdinIsDevNull() bool {
	in, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	null, err := os.Stat(os.DevNull)
	if err != nil {
		return false
	}
	return os.SameFile(in, null)
}

// mcpSessionIdleTimeout closes an MCP session after this long without a
// request. Without it every session a client abandons (a closed chat, a
// reloaded connector) keeps its goroutine and state until the process
// restarts. A client whose session expired gets 404 and starts a new one,
// as the MCP spec requires; its OAuth tokens are unaffected. A variable so
// tests can shorten it.
var mcpSessionIdleTimeout = 30 * time.Minute

// newMCPHandler serves server over Streamable HTTP.
func newMCPHandler(server *mcp.Server) *mcp.StreamableHTTPHandler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{SessionTimeout: mcpSessionIdleTimeout})
}

// readHeaderTimeout bounds how long a client may take to send its request
// headers; a variable so tests can shorten it.
var readHeaderTimeout = 10 * time.Second

// newHTTPServer builds the MCP HTTP server. Slow or incomplete headers and
// idle keep-alive connections are cut off, so nothing on the network can hold
// connections and goroutines open indefinitely. There is deliberately no
// ReadTimeout or WriteTimeout: Streamable HTTP keeps SSE responses open for
// as long as a session lasts, and both would cut those streams off.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

// jwtAuth verifies Bearer access tokens on MCP requests. On failure, returns
// 401 with WWW-Authenticate pointing at our protected-resource metadata so
// clients can re-discover after a secret rotation.
func (s *server) jwtAuth(next http.Handler) http.Handler {
	challenge := fmt.Sprintf(`Bearer realm="tgtorrentbot-mcp", resource_metadata="%s/.well-known/oauth-protected-resource"`,
		s.config.PublicBaseURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(hdr, prefix) {
			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		token := hdr[len(prefix):]
		claims, err := verifyJWT(s.config.JWTSecret, token)
		if err != nil || claims.Typ != "access" {
			w.Header().Set("WWW-Authenticate", challenge+`, error="invalid_token"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if claims.Iss != s.config.PublicBaseURL || claims.Aud != s.config.PublicBaseURL {
			w.Header().Set("WWW-Authenticate", challenge+`, error="invalid_token"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
