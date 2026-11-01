package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/odwrtw/transmission"
)

type moveMediaInput struct {
	Source      string `json:"source" jsonschema:"path of the file or directory to move, relative to the download path or absolute under it; must exist"`
	Destination string `json:"destination" jsonschema:"new path, relative to the download path or absolute under it; must not already exist (parent dirs are created automatically)"`
	TorrentID   *int   `json:"torrent_id,omitempty" jsonschema:"Transmission torrent id of the item being moved (the torrent_id field from list_media); when set, the torrent is removed from Transmission (files kept) before the move; refused when that torrent's data is not at the source path. Omit for filesystem-only items"`
}

type moveMediaOutput struct {
	Source         string `json:"source"`
	Destination    string `json:"destination"`
	Moved          bool   `json:"moved"`
	TorrentRemoved bool   `json:"torrent_removed"`
	TorrentNote    string `json:"torrent_note,omitempty"`
}

func (s *server) moveMedia(ctx context.Context, _ *mcp.CallToolRequest, in moveMediaInput) (*mcp.CallToolResult, moveMediaOutput, error) {
	// A symlink source would move the link's target (resolvePath returns
	// the resolved path), possibly another item in another category, while
	// the caller named the link. Refuse instead.
	if fi, err := os.Lstat(lexicalPath(s.config.DownloadPath, in.Source)); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return formatError("source %q is a symlink; move the item it points to by its own path", in.Source), moveMediaOutput{}, nil
	}
	src, err := resolvePath(s.config.DownloadPath, in.Source)
	if err != nil {
		return formatError("source: %v", err), moveMediaOutput{}, nil
	}
	dst, err := resolveDestPath(s.config.DownloadPath, in.Destination)
	if err != nil {
		return formatError("destination: %v", err), moveMediaOutput{}, nil
	}
	// The download root, the category directories and the incomplete
	// directory are structure, not media: moving one would pull every
	// torrent's data out from under Transmission and empty a library mount.
	if s.isStructuralDir(src) {
		return formatError("source %q is the download root, a category directory or the incomplete directory; move items inside it instead", in.Source), moveMediaOutput{}, nil
	}
	if s.isStructuralDir(dst) {
		return formatError("destination %q is the download root, a category directory or the incomplete directory; move into it with a name, e.g. %q", in.Destination, filepath.Join(in.Destination, filepath.Base(src))), moveMediaOutput{}, nil
	}
	if src == dst {
		return formatError("source and destination are the same path"), moveMediaOutput{}, nil
	}
	if _, err := os.Lstat(dst); err == nil {
		return formatError("destination %q already exists; refusing to overwrite", in.Destination), moveMediaOutput{}, nil
	}
	// Everything the rename itself would reject is checked here, before the
	// torrent is untracked, so a move that cannot happen does not cost it.
	if isWithin(dst, src) {
		return formatError("destination %q is inside the source %q; a directory cannot be moved into itself", in.Destination, in.Source), moveMediaOutput{}, nil
	}
	if same, ok := sameDevice(src, existingAncestor(dst)); ok && !same {
		return formatError("source %q and destination %q are on different filesystems; cross-device moves are not supported", in.Source, in.Destination), moveMediaOutput{}, nil
	}

	out := moveMediaOutput{Source: src, Destination: dst}

	// Drop the torrent before touching the files: once Transmission no longer
	// tracks the path, the move is a plain rename and Transmission can't error
	// or re-check when the files disappear from under it.
	if in.TorrentID != nil {
		removed, note, err := s.untrackTorrent(*in.TorrentID, src)
		if err != nil {
			return formatError("removing torrent %d before move (nothing was moved): %v", *in.TorrentID, err), moveMediaOutput{}, nil
		}
		out.TorrentRemoved = removed
		out.TorrentNote = note
	}

	created, err := mkdirAllTracked(filepath.Dir(dst))
	if err != nil {
		removeCreated(created)
		return formatError("creating destination parent%s: %v", torrentRemovedSuffix(out), err), moveMediaOutput{}, nil
	}
	// The Lstat above is only a fast pre-check: the destination can appear
	// while the torrent is being removed. renameNoReplace refuses to replace
	// it atomically, unlike os.Rename, which silently overwrites a file or an
	// empty directory.
	if err := renameNoReplace(src, dst); err != nil {
		removeCreated(created)
		if errors.Is(err, fs.ErrExist) {
			return formatError("destination %q already exists; refusing to overwrite (nothing was moved)%s", in.Destination, torrentRemovedSuffix(out)), moveMediaOutput{}, nil
		}
		hint := ""
		if errors.Is(err, syscall.EXDEV) {
			hint = " (cross-device moves are not supported)"
		}
		return formatError("moving %q to %q: %v%s (nothing was moved)%s", in.Source, in.Destination, err, hint, torrentRemovedSuffix(out)), moveMediaOutput{}, nil
	}
	out.Moved = true
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mustJSON(out)}},
	}, out, nil
}

// untrackTorrent removes a torrent from Transmission while keeping its files.
// It refuses a torrent whose data is not at src (resolved): a wrong id from
// the caller would otherwise untrack an unrelated torrent and leave the one
// that owns src to see its files vanish. A torrent that is already gone is
// not an error: it returns removed=false with an explanatory note so the
// move can proceed.
func (s *server) untrackTorrent(id int, src string) (removed bool, note string, err error) {
	if s.trans == nil {
		return false, "", fmt.Errorf("Transmission is not configured (set TGT_RPC_ADDR, TGT_RPC_USER, TGT_RPC_PASSWORD); omit torrent_id to move without it")
	}
	all, err := s.trans.GetTorrents()
	if err != nil {
		return false, "", fmt.Errorf("failed to fetch torrents: %v", err)
	}
	var match []*transmission.Torrent
	for _, t := range all {
		if t.ID == id {
			match = append(match, t)
		}
	}
	if len(match) == 0 {
		return false, fmt.Sprintf("no torrent with id %d (already removed?); moved without it", id), nil
	}
	for _, t := range match {
		if paths := s.torrentDataPaths(t); !anyOverlaps(paths, src) {
			return false, "", fmt.Errorf("torrent %d (%q) does not own %s: its data is at %v; pass the torrent_id list_media reports for this item, or omit it", id, t.Name, src, paths)
		}
	}
	if err := s.trans.RemoveTorrents(match, false); err != nil {
		return false, "", err
	}
	return true, "", nil
}

// torrentDataPaths returns where a torrent's data can be on disk, resolved:
// {downloadDir}/{name} once complete, and {incompletePath}/{name} while it
// downloads. Transmission and this server see the same paths (the compose
// file mounts the downloads at /downloads everywhere).
func (s *server) torrentDataPaths(t *transmission.Torrent) []string {
	if t.Name == "" {
		return nil
	}
	var paths []string
	if t.DownloadDir != "" {
		paths = append(paths, evalOrClean(filepath.Join(t.DownloadDir, t.Name)))
	}
	return append(paths, evalOrClean(filepath.Join(s.incompletePath(), t.Name)))
}

// anyOverlaps reports whether p equals one of paths or one contains the
// other: moving a torrent's folder, one file of it, or a folder holding it
// all take its data away from Transmission.
func anyOverlaps(paths []string, p string) bool {
	for _, q := range paths {
		if q == p || isWithin(p, q) || isWithin(q, p) {
			return true
		}
	}
	return false
}

// isWithin reports whether p lies strictly inside dir.
func isWithin(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// existingAncestor returns p or its nearest ancestor that exists.
func existingAncestor(p string) string {
	for {
		if _, err := os.Lstat(p); err == nil {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}

// mkdirAllTracked is os.MkdirAll that returns the directories it created,
// deepest first, so a failed move can take them back out.
func mkdirAllTracked(dir string) ([]string, error) {
	var missing []string
	for p := dir; ; p = filepath.Dir(p) {
		if _, err := os.Lstat(p); err == nil || filepath.Dir(p) == p {
			break
		}
		missing = append(missing, p)
	}
	var created []string
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return created, err
		}
		created = append([]string{missing[i]}, created...)
	}
	return created, nil
}

// removeCreated removes directories mkdirAllTracked made, deepest first; a
// directory something else has written into since is left alone.
func removeCreated(created []string) {
	for _, d := range created {
		_ = os.Remove(d)
	}
}

func torrentRemovedSuffix(out moveMediaOutput) string {
	if out.TorrentRemoved {
		return "; note: the torrent was already removed from Transmission (files kept at the source path)"
	}
	return ""
}

// isStructuralDir reports whether p (a resolved absolute path) is the
// download root, {root}/{category} for a known category, or the incomplete
// directory.
func (s *server) isStructuralDir(p string) bool {
	for _, d := range s.structuralDirs() {
		if p == d {
			return true
		}
	}
	return false
}

func (s *server) structuralDirs() []string {
	root := evalOrClean(s.config.DownloadPath)
	dirs := []string{root}
	for _, c := range knownCategories {
		dirs = append(dirs, evalOrClean(filepath.Join(s.config.DownloadPath, c)))
	}
	return append(dirs, evalOrClean(s.incompletePath()))
}

func (s *server) incompletePath() string {
	if s.config.IncompletePath != "" {
		return s.config.IncompletePath
	}
	return filepath.Join(s.config.DownloadPath, "incomplete")
}

// evalOrClean resolves symlinks in p, or returns it cleaned when it does not
// exist (yet).
func evalOrClean(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
