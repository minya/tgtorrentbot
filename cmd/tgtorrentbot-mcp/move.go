package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/odwrtw/transmission"
)

type moveMediaInput struct {
	Source      string `json:"source" jsonschema:"path of the file or directory to move, relative to the download path or absolute under it; must exist"`
	Destination string `json:"destination" jsonschema:"new path, relative to the download path or absolute under it; must not already exist (parent dirs are created automatically)"`
	TorrentID   *int   `json:"torrent_id,omitempty" jsonschema:"Transmission torrent id of the item being moved (the torrent_id field from list_media); when set, the torrent is removed from Transmission (files kept) before the move. Omit for filesystem-only items"`
}

type moveMediaOutput struct {
	Source         string `json:"source"`
	Destination    string `json:"destination"`
	Moved          bool   `json:"moved"`
	TorrentRemoved bool   `json:"torrent_removed"`
	TorrentNote    string `json:"torrent_note,omitempty"`
}

func (s *server) moveMedia(ctx context.Context, _ *mcp.CallToolRequest, in moveMediaInput) (*mcp.CallToolResult, moveMediaOutput, error) {
	src, err := resolvePath(s.config.DownloadPath, in.Source)
	if err != nil {
		return formatError("source: %v", err), moveMediaOutput{}, nil
	}
	dst, err := resolveDestPath(s.config.DownloadPath, in.Destination)
	if err != nil {
		return formatError("destination: %v", err), moveMediaOutput{}, nil
	}
	if src == dst {
		return formatError("source and destination are the same path"), moveMediaOutput{}, nil
	}
	if _, err := os.Lstat(dst); err == nil {
		return formatError("destination %q already exists; refusing to overwrite", in.Destination), moveMediaOutput{}, nil
	}

	out := moveMediaOutput{Source: src, Destination: dst}

	// Drop the torrent before touching the files: once Transmission no longer
	// tracks the path, the move is a plain rename and Transmission can't error
	// or re-check when the files disappear from under it.
	if in.TorrentID != nil {
		removed, note, err := s.untrackTorrent(*in.TorrentID)
		if err != nil {
			return formatError("removing torrent %d before move (nothing was moved): %v", *in.TorrentID, err), moveMediaOutput{}, nil
		}
		out.TorrentRemoved = removed
		out.TorrentNote = note
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return formatError("creating destination parent%s: %v", torrentRemovedSuffix(out), err), moveMediaOutput{}, nil
	}
	if err := os.Rename(src, dst); err != nil {
		return formatError("moving %q to %q: %v (cross-device moves are not supported)%s", in.Source, in.Destination, err, torrentRemovedSuffix(out)), moveMediaOutput{}, nil
	}
	out.Moved = true
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mustJSON(out)}},
	}, out, nil
}

// untrackTorrent removes a torrent from Transmission while keeping its files.
// A torrent that is already gone is not an error: it returns removed=false
// with an explanatory note so the move can proceed.
func (s *server) untrackTorrent(id int) (removed bool, note string, err error) {
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
	if err := s.trans.RemoveTorrents(match, false); err != nil {
		return false, "", err
	}
	return true, "", nil
}

func torrentRemovedSuffix(out moveMediaOutput) string {
	if out.TorrentRemoved {
		return "; note: the torrent was already removed from Transmission (files kept at the source path)"
	}
	return ""
}
