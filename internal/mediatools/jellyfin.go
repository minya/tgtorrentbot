package mediatools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type jellyfinRescanInput struct {
	Path string `json:"path,omitempty" jsonschema:"after write_tags: the path that was written (as passed to write_tags, an item or a part of it inside a category directory). Jellyfin then reads the tags of the albums and music videos at, inside or around it again and replaces what it stored, artists and album artists included, which a plain library scan keeps. Omit after move_media: a library scan picks up renamed folders"`
}

type jellyfinRescanOutput struct {
	Triggered bool `json:"triggered"`
	// Refreshed names the items whose metadata was replaced (path given).
	Refreshed []string `json:"refreshed,omitempty"`
	// LibraryScan is set when a library scan was started: no path given, or
	// Jellyfin has no item at the path yet.
	LibraryScan bool `json:"library_scan,omitempty"`
}

func (s *Tools) jellyfinRescan(ctx context.Context, _ *mcp.CallToolRequest, in jellyfinRescanInput) (*mcp.CallToolResult, jellyfinRescanOutput, error) {
	if s.config.JellyfinURL == "" || s.config.JellyfinAPIKey == "" {
		return formatError("Jellyfin is not configured (set TGT_JELLYFIN_URL and TGT_JELLYFIN_API_KEY)"), jellyfinRescanOutput{}, nil
	}
	if in.Path == "" {
		return s.jellyfinLibraryScan(ctx, "Jellyfin library refresh triggered")
	}

	rel, err := s.itemRelPath(in.Path)
	if err != nil {
		return formatError("%v", err), jellyfinRescanOutput{}, nil
	}
	refreshed, err := s.jf.RefreshItemsAt(ctx, rel)
	if err != nil {
		return formatError("Jellyfin metadata refresh failed: %v (refreshed before the failure: %s)", err, strings.Join(refreshed, ", ")), jellyfinRescanOutput{Refreshed: refreshed}, nil
	}
	if len(refreshed) == 0 {
		// Not in Jellyfin yet: its first scan reads the tags as they are now.
		return s.jellyfinLibraryScan(ctx, fmt.Sprintf("Jellyfin has no album or music video at %s yet; library scan triggered, which reads its current tags", rel))
	}
	out := jellyfinRescanOutput{Triggered: true, Refreshed: refreshed}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Jellyfin is reading the metadata of %d item(s) again, replacing what it stored: %s", len(refreshed), strings.Join(refreshed, ", "))}},
	}, out, nil
}

func (s *Tools) jellyfinLibraryScan(ctx context.Context, msg string) (*mcp.CallToolResult, jellyfinRescanOutput, error) {
	if err := s.jf.RefreshLibrary(ctx); err != nil {
		return formatError("Jellyfin library refresh failed: %v", err), jellyfinRescanOutput{}, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}, jellyfinRescanOutput{Triggered: true, LibraryScan: true}, nil
}

// itemRelPath resolves input like the tag tools and returns it relative to
// the download path, which lays out the media root Jellyfin sees. It must
// name an item or a part of one: the download root and a category
// directory would refresh a whole library, and the incomplete directory is
// not in Jellyfin.
func (s *Tools) itemRelPath(input string) (string, error) {
	resolved, err := resolvePath(s.config.DownloadPath, input)
	if err != nil {
		return "", err
	}
	roots, err := evalRootsOf([]string{s.config.DownloadPath})
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(roots[0], resolved)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%q is not an item inside a category directory", input)
	}
	rel = filepath.ToSlash(rel)
	if !strings.Contains(rel, "/") {
		return "", fmt.Errorf("%q is a category directory; pass the item (or the part of it) whose tags changed", input)
	}
	return rel, nil
}
