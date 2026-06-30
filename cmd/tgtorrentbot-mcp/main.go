package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/minya/logger"
	mcplib "github.com/minya/tgtorrentbot/internal/media"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/odwrtw/transmission"
)

// server holds shared dependencies used by tool handlers and OAuth endpoints.
type server struct {
	config       Config
	trans        *transmission.Client
	jf           *mcplib.JellyfinClient
	abs          *mcplib.AudiobookshelfClient
	scan         *mcplib.FilesystemScanner
	codeStore    *codeStore
	loginLimiter *rateLimiter
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
		config:       cfg,
		codeStore:    newCodeStore(),
		loginLimiter: newRateLimiter(10, time.Minute),
	}
	if cfg.TransmissionAddr != "" {
		t, err := transmission.New(transmission.Config{
			Address:  cfg.TransmissionAddr,
			User:     cfg.TransmissionUser,
			Password: cfg.TransmissionPassword,
		})
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
		mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return mcpServer
		}, nil)

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
		httpSrv := &http.Server{
			Addr:    addr,
			Handler: accessLog(corsMiddleware(mux)),
		}
		if err := httpSrv.ListenAndServe(); err != nil {
			logger.Error(err, "MCP HTTP server exited with error")
			os.Exit(1)
		}
		return
	}

	if err := mcpServer.Run(ctx, &mcp.StdioTransport{}); err != nil {
		logger.Error(err, "MCP server exited with error")
		os.Exit(1)
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
			"filter (movies, shows, music, musicvideos, audiobooks, others) and a " +
			"case-insensitive substring query on the item name.",
	}, s.listMedia)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "read_tags",
		Description: "Read ID3 tags from one audio file or every audio file under a " +
			"directory. Input path is relative to the configured download path (or " +
			"absolute under it). Returns per-file title/artist/album/album_artist/" +
			"composer/year/genre/track/disc/comment plus an 'encoding_hint' flag " +
			"suggesting cp1251-as-latin1 mojibake so Claude knows when to call " +
			"write_tags with fix_encoding='cp1251'. For multi-CD audiobooks, check " +
			"track/disc across files to spot missing or wrong values.",
	}, s.readTags)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "write_tags",
		Description: "Write ID3 tags to an audio file or every audio file under a " +
			"directory. Two modes, combinable (applied in order fix → explicit):\n" +
			"  1. fix_encoding: re-decode ALL text frames and comment frames from " +
			"the given codepage (e.g. 'cp1251') to UTF-8 — use this when read_tags " +
			"reports mojibake.\n" +
			"  2. tags: apply explicit overrides. Keys: title, artist, album, " +
			"album_artist, composer, year, genre, track, disc, comment.\n" +
			"For multi-CD audiobooks with missing or wrong track/disc numbers: call " +
			"read_tags recursively on the directory, inspect filenames and parent " +
			"folders, decide the correct TRCK/TPOS per file, and send those as " +
			"explicit overrides (one write_tags call per file).\n" +
			"Writes are restricted to the configured download path. After writing, " +
			"call abs_rescan so Audiobookshelf/Jellyfin pick up the change.",
	}, s.writeTags)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "abs_rescan",
		Description: "Trigger an Audiobookshelf library rescan so it re-ingests " +
			"files whose tags were just changed. No-op if Audiobookshelf isn't " +
			"configured. Call this after write_tags on audiobook files.",
	}, s.absRescan)

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
			"IMPORTANT: if the item's list_media sources include 'torrent', call " +
			"remove_torrent on its torrent_id FIRST, otherwise renaming breaks " +
			"Transmission seeding. After moving, call jellyfin_rescan so Jellyfin " +
			"re-identifies from the new name.",
	}, s.moveMedia)

	mcp.AddTool(mcpServer, &mcp.Tool{
		Name: "remove_torrent",
		Description: "Remove a torrent from Transmission by its id (the torrent_id " +
			"from list_media). By default (delete_data=false) it only stops " +
			"Transmission from tracking the torrent and KEEPS the files on disk — " +
			"call this before move_media to rename a folder that is still seeding. " +
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
