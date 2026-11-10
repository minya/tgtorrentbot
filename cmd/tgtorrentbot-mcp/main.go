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
	mcplib "github.com/minya/tgtorrentbot/internal/media"
	"github.com/minya/tgtorrentbot/internal/transmissionrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/odwrtw/transmission"
)

// torrentClient is the subset of *transmission.Client the tools use; an
// interface so tests can stub Transmission.
type torrentClient interface {
	GetTorrents() ([]*transmission.Torrent, error)
	RemoveTorrents(torrents []*transmission.Torrent, removeData bool) error
}

// server holds shared dependencies used by tool handlers and OAuth endpoints.
type server struct {
	config    Config
	trans     torrentClient
	jf        *mcplib.JellyfinClient
	abs       *mcplib.AudiobookshelfClient
	scan      *mcplib.FilesystemScanner
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
	if cfg.TransmissionAddr != "" {
		t, err := transmissionrpc.New(
			cfg.TransmissionAddr,
			cfg.TransmissionUser,
			cfg.TransmissionPassword,
		)
		if err != nil {
			logger.Error(err, "Failed to create Transmission client; list_media will return filesystem items only")
		} else {
			srv.trans = t
		}
	}
	srv.jf = mcplib.NewJellyfinClient(cfg.JellyfinURL, cfg.JellyfinAPIKey)
	srv.abs = mcplib.NewAudiobookshelfClient(cfg.AudiobookshelfURL, cfg.AudiobookshelfAPIKey)
	srv.scan = &mcplib.FilesystemScanner{
		DownloadPath:   cfg.DownloadPath,
		IncompletePath: cfg.IncompletePath,
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "tgtorrentbot-mcp",
		Version: "0.1.0",
	}, nil)

	srv.registerTools(mcpServer)

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

func (s *server) registerTools(mcpServer *mcp.Server) {
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "list_media",
		Description: "List media items across Transmission torrents, the download " +
			"filesystem, Jellyfin, and Audiobookshelf. Use this to find the path of " +
			"an item before reading or writing its tags. Supports optional category " +
			"filter (" + strings.Join(knownCategories, ", ") + ", or 'incomplete' for " +
			"orphaned leftovers in the incomplete directory that no torrent owns) and a " +
			"case-insensitive substring query on the item name. Each item carries " +
			"added_date (when Transmission added it; torrents only) and mtime (newest " +
			"filesystem modification time; on-disk items only), both RFC 3339 UTC — " +
			"use them to find recently added items; torrent_id is NOT a recency " +
			"signal. Set recursive=true to also list every file inside each item " +
			"(e.g. the episodes of a show) with path/size/mtime; narrow with " +
			"category/query, since output is capped (files_truncated=true when hit). " +
			"If a source could not be read (Transmission, Jellyfin or Audiobookshelf " +
			"down, a directory unreadable), 'warnings' says so: then a missing " +
			"source on an item means nothing, so do not conclude an item is unknown " +
			"to that service.",
	}, s.listMedia)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "read_tags",
		Description: "Read ID3 tags from one .mp3 file or every .mp3 file under a " +
			"directory (other formats are not supported and are skipped). Input path is relative to the configured download path (or " +
			"absolute under it or under the incomplete directory, as list_media reports it). Returns per-file title/artist/album/album_artist/" +
			"composer/year/genre/track/disc/comment plus an 'encoding_hint' flag " +
			"suggesting cp1251-as-latin1 mojibake so Claude knows when to call " +
			"write_tags with fix_encoding='cp1251'. For multi-CD audiobooks, check " +
			"track/disc across files to spot missing or wrong values.",
	}, s.readTags)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "write_tags",
		Description: "Write ID3 tags to one .mp3 file or every .mp3 file under a " +
			"directory (only .mp3 is supported). Two modes, combinable (applied in order fix → explicit):\n" +
			"  1. fix_encoding: re-decode text (T***, TXXX), comment (COMM) and lyrics (USLT) frames from " +
			"the given codepage to Unicode; 'cp1251' is the only codepage supported. Use this when read_tags " +
			"reports mojibake. Only values that themselves look like cp1251 read " +
			"as Latin-1 are rewritten; correct text (including accented Latin " +
			"such as 'Café') is left as is, so it is safe on mixed directories.\n" +
			"  2. tags: apply explicit overrides. Keys: title, artist, album, " +
			"album_artist, composer, year, genre, track, disc, comment.\n" +
			"For multi-CD audiobooks with missing or wrong track/disc numbers: call " +
			"read_tags recursively on the directory, inspect filenames and parent " +
			"folders, decide the correct TRCK/TPOS per file, and send those as " +
			"explicit overrides (one write_tags call per file).\n" +
			"If the item's list_media sources include 'torrent', pass its torrent_id: " +
			"rewriting the files breaks the torrent's piece hashes, so the torrent is " +
			"removed from Transmission first (files kept, seeding stops); without it the " +
			"write is refused, and a torrent still downloading is refused too.\n" +
			"Writes are restricted to the configured download path. After writing " +
			"audiobooks, call abs_rescan so Audiobookshelf picks up the change; after " +
			"writing music or musicvideos, call jellyfin_rescan (abs_rescan does not " +
			"refresh Jellyfin).",
	}, s.writeTags)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "abs_rescan",
		Description: "Trigger an Audiobookshelf library rescan so it re-ingests " +
			"files whose tags were just changed. No-op if Audiobookshelf isn't " +
			"configured. Call this after write_tags on audiobook files.",
	}, s.absRescan)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "abs_remove_issues",
		Description: "Remove every Audiobookshelf library item flagged with issues " +
			"(isMissing/isInvalid — shown as 'items with issues' in ABS). ABS does " +
			"not drop items whose files were moved or deleted on disk; it only flags " +
			"them on rescan, so call this after move_media on audiobooks followed by " +
			"abs_rescan (and once the rescan has finished) to clear the stale " +
			"entries. Only database entries are removed; files on disk are never " +
			"touched. library_id is optional (defaults to the configured or the " +
			"single audiobook library).",
	}, s.absRemoveIssues)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "abs_remove_item",
		Description: "Remove a single Audiobookshelf library item by its id (e.g. " +
			"'li_...'). Only the ABS database entry is removed; files on disk are " +
			"never touched. Prefer abs_remove_issues for clearing items whose files " +
			"went missing.",
	}, s.absRemoveItem)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "move_media",
		Description: "Move or rename a file or directory within the download path. " +
			"Source must exist; destination must not (parent dirs are created " +
			"automatically). Use this to fix TV-show folders that Jellyfin " +
			"mis-identifies: Jellyfin matches shows by folder name, so rename the " +
			"top-level folder to a clean 'Show Name (Year)', stripping release junk " +
			"(quality, codec, release group) and translating transliterated or " +
			"Russian titles to their canonical English name (e.g. " +
			"'Proslushka.S01.2002...' -> 'The Wire (2002)'). Optionally create " +
			"'Season NN/' subfolders by moving episode files (Jellyfin also detects " +
			"SxxExx from filenames in a flat folder, so renaming the top folder is " +
			"usually enough).\n" +
			"If the item's list_media sources include 'torrent', pass its " +
			"torrent_id: the torrent is removed from Transmission (files kept) " +
			"before the move, so Transmission doesn't error on the vanished path. " +
			"A torrent that is still downloading is refused: move it once complete. " +
			"A move of data a torrent still tracks is refused without its torrent_id " +
			"(the error names the torrent); if several torrents track it, remove the " +
			"others with remove_torrent first. " +
			"A torrent_id that no longer exists is not an error — the move still " +
			"happens. Omit torrent_id for filesystem-only items. After moving, " +
			"call jellyfin_rescan so Jellyfin re-identifies from the new name.",
	}, s.moveMedia)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "remove_torrent",
		Description: "Remove a torrent from Transmission by its id (the torrent_id " +
			"from list_media). By default (delete_data=false) it only stops " +
			"Transmission from tracking the torrent and KEEPS the files on disk " +
			"(no need to call this before move_media — pass torrent_id to " +
			"move_media instead). " +
			"Set delete_data=true ONLY when you intend to erase the downloaded " +
			"files. Errors if Transmission isn't configured or no torrent matches.",
	}, s.removeTorrent)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "jellyfin_rescan",
		Description: "Trigger a Jellyfin library refresh so it re-scans and " +
			"re-identifies items whose folders were just renamed. No-op if Jellyfin " +
			"isn't configured. Call this after move_media on shows/movies.",
	}, s.jellyfinRescan)
}

// formatError builds an error-bearing CallToolResult so the model sees the message.
func formatError(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}
