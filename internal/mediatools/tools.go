// Package mediatools holds the media-library tools: list_media, read_tags,
// write_tags, move_media, remove_torrent and the Jellyfin/Audiobookshelf
// rescans. The MCP server serves them to Claude Code and claude.ai; the bot's
// auto-fixer calls them in process (Connect).
package mediatools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"

	mcplib "github.com/minya/tgtorrentbot/internal/media"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/odwrtw/transmission"
)

// TorrentClient is the subset of *transmission.Client the tools use; an
// interface so tests can stub Transmission.
type TorrentClient interface {
	GetTorrents() ([]*transmission.Torrent, error)
	RemoveTorrents(torrents []*transmission.Torrent, removeData bool) error
}

// Config is what the tools need to know. Every service is optional: a
// missing one reduces what list_media returns and makes that service's
// tools return a "not configured" error.
type Config struct {
	// DownloadPath is the download root, resolved (see ResolveRoot); the
	// path guards confine every tool to it and to the incomplete directory.
	DownloadPath         string
	IncompletePath       string
	JellyfinURL          string
	JellyfinAPIKey       string
	AudiobookshelfURL    string
	AudiobookshelfAPIKey string
	// AudiobookshelfLibraryID, when set, is the default library for
	// abs_remove_issues; otherwise the single book-type library is looked up.
	AudiobookshelfLibraryID string
}

// Tools holds the dependencies of the tool handlers.
type Tools struct {
	config Config
	trans  TorrentClient
	jf     *mcplib.JellyfinClient
	abs    *mcplib.AudiobookshelfClient
	scan   *mcplib.FilesystemScanner
	// stop holds the context set by SetStop (*context.Context).
	stop atomic.Pointer[context.Context]
}

// SetStop makes the mutating tools (write_tags, move_media) stop at their
// next safe point once ctx is done, as they do when their own request is
// cancelled: move_media before it untracks the torrent, write_tags between
// files, returning what it changed. A caller that must not lose a call's
// result (the auto-fixer at shutdown) calls the tool with a context it
// does not cancel and ends the work through ctx instead, so it still gets
// the result.
func (s *Tools) SetStop(ctx context.Context) { s.stop.Store(&ctx) }

// stopped reports why a mutating tool should stop at its next safe point,
// or nil to go on.
func (s *Tools) stopped(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if stop := s.stop.Load(); stop != nil {
		return (*stop).Err()
	}
	return nil
}

// New returns the tools over cfg. trans may be nil (Transmission not
// configured).
func New(cfg Config, trans TorrentClient) *Tools {
	return &Tools{
		config: cfg,
		trans:  trans,
		jf:     mcplib.NewJellyfinClient(cfg.JellyfinURL, cfg.JellyfinAPIKey),
		abs:    mcplib.NewAudiobookshelfClient(cfg.AudiobookshelfURL, cfg.AudiobookshelfAPIKey),
		scan: &mcplib.FilesystemScanner{
			DownloadPath:   cfg.DownloadPath,
			IncompletePath: cfg.IncompletePath,
		},
	}
}

// HasJellyfin and HasAudiobookshelf report whether that media server is
// configured, so its tools can work.
func (s *Tools) HasJellyfin() bool {
	return s.config.JellyfinURL != "" && s.config.JellyfinAPIKey != ""
}

func (s *Tools) HasAudiobookshelf() bool {
	return s.config.AudiobookshelfURL != "" && s.config.AudiobookshelfAPIKey != ""
}

// Connect serves the tools on a new in-memory MCP server and returns a
// client session to it, for callers in this process (the bot's
// auto-fixer). Calls through it go through the same handlers and guards as
// remote MCP calls.
func (s *Tools) Connect(ctx context.Context) (*mcp.ClientSession, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: "tgtorrentbot-mediatools", Version: "0.1.0"}, nil)
	s.Register(server)
	serverT, clientT := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverT, nil); err != nil {
		return nil, fmt.Errorf("connecting MCP server: %w", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "tgtorrentbot-inprocess", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting MCP client: %w", err)
	}
	return session, nil
}

// Register adds the media tools to mcpServer.
func (s *Tools) Register(mcpServer *mcp.Server) {
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
			"files whose tags were just changed. Returns an error if Audiobookshelf " +
			"isn't configured. Call this after write_tags on audiobook files.",
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
			"re-identifies items whose folders were just renamed. Returns an error if " +
			"Jellyfin isn't configured. Call this after move_media on shows/movies.",
	}, s.jellyfinRescan)
}

// formatError builds an error-bearing CallToolResult so the model sees the message.
func formatError(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}

// ResolveRoots makes the download and incomplete paths absolute and
// resolves symlinks in them. The path guards compare resolved paths against
// the roots and the tools report paths relative to the download root, so an
// unresolved root (e.g. /downloads -> /mnt/disk/downloads) would make every
// returned path absolute and then rejected when passed back. incomplete
// defaults to {download}/incomplete.
func ResolveRoots(download, incomplete string) (string, string, error) {
	abs, err := filepath.Abs(download)
	if err != nil {
		return "", "", err
	}
	download = resolvedDir(filepath.Clean(abs))
	if incomplete == "" {
		return download, filepath.Join(download, "incomplete"), nil
	}
	if incAbs, err := filepath.Abs(incomplete); err == nil {
		incomplete = resolvedDir(filepath.Clean(incAbs))
	}
	return download, incomplete, nil
}

// resolvedDir returns p with symlinks resolved, or p itself when it does
// not exist (yet).
func resolvedDir(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
