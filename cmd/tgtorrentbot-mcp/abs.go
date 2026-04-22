package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type absRescanInput struct{}

type absRescanOutput struct {
	Triggered bool `json:"triggered"`
}

func (s *server) absRescan(ctx context.Context, _ *mcp.CallToolRequest, _ absRescanInput) (*mcp.CallToolResult, absRescanOutput, error) {
	if s.config.AudiobookshelfURL == "" || s.config.AudiobookshelfAPIKey == "" {
		return formatError("Audiobookshelf is not configured (set TGT_AUDIOBOOKSHELF_URL and TGT_AUDIOBOOKSHELF_API_KEY)"), absRescanOutput{}, nil
	}
	s.abs.RefreshLibrary()
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Audiobookshelf rescan triggered"}},
	}, absRescanOutput{Triggered: true}, nil
}
