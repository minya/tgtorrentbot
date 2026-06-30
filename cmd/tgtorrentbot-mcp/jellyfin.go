package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type jellyfinRescanInput struct{}

type jellyfinRescanOutput struct {
	Triggered bool `json:"triggered"`
}

func (s *server) jellyfinRescan(ctx context.Context, _ *mcp.CallToolRequest, _ jellyfinRescanInput) (*mcp.CallToolResult, jellyfinRescanOutput, error) {
	if s.config.JellyfinURL == "" || s.config.JellyfinAPIKey == "" {
		return formatError("Jellyfin is not configured (set TGT_JELLYFIN_URL and TGT_JELLYFIN_API_KEY)"), jellyfinRescanOutput{}, nil
	}
	s.jf.RefreshLibrary()
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Jellyfin library refresh triggered"}},
	}, jellyfinRescanOutput{Triggered: true}, nil
}
