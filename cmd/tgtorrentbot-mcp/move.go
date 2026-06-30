package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type moveMediaInput struct {
	Source      string `json:"source" jsonschema:"path of the file or directory to move, relative to the download path or absolute under it; must exist"`
	Destination string `json:"destination" jsonschema:"new path, relative to the download path or absolute under it; must not already exist (parent dirs are created automatically)"`
}

type moveMediaOutput struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Moved       bool   `json:"moved"`
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
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return formatError("creating destination parent: %v", err), moveMediaOutput{}, nil
	}
	if err := os.Rename(src, dst); err != nil {
		return formatError("moving %q to %q: %v (cross-device moves are not supported)", in.Source, in.Destination, err), moveMediaOutput{}, nil
	}
	out := moveMediaOutput{Source: src, Destination: dst, Moved: true}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mustJSON(out)}},
	}, out, nil
}
