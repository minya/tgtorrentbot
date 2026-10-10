package main

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type absRescanInput struct{}

type absRescanOutput struct {
	Triggered bool `json:"triggered"`
}

const absNotConfigured = "Audiobookshelf is not configured (set TGT_AUDIOBOOKSHELF_URL and TGT_AUDIOBOOKSHELF_API_KEY)"

func (s *server) absRescan(ctx context.Context, _ *mcp.CallToolRequest, _ absRescanInput) (*mcp.CallToolResult, absRescanOutput, error) {
	if s.config.AudiobookshelfURL == "" || s.config.AudiobookshelfAPIKey == "" {
		return formatError(absNotConfigured), absRescanOutput{}, nil
	}
	s.abs.RefreshLibrary()
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Audiobookshelf rescan triggered"}},
	}, absRescanOutput{Triggered: true}, nil
}

type absRemoveIssuesInput struct {
	LibraryID string `json:"library_id,omitempty" jsonschema:"optional Audiobookshelf library id; defaults to TGT_AUDIOBOOKSHELF_LIBRARY_ID, else the single audiobook (book-type) library"`
}

type absRemoveIssuesOutput struct {
	LibraryID string `json:"library_id"`
	Status    int    `json:"status"`
	Removed   bool   `json:"removed"`
}

func (s *server) absRemoveIssues(ctx context.Context, _ *mcp.CallToolRequest, in absRemoveIssuesInput) (*mcp.CallToolResult, absRemoveIssuesOutput, error) {
	if !s.abs.Configured() {
		return formatError(absNotConfigured), absRemoveIssuesOutput{}, nil
	}
	libID := strings.TrimSpace(in.LibraryID)
	if libID == "" {
		libID = s.config.AudiobookshelfLibraryID
	}
	if libID == "" {
		id, err := s.abs.BookLibraryID()
		if err != nil {
			return formatError("resolving library id: %v", err), absRemoveIssuesOutput{}, nil
		}
		libID = id
	}
	status, err := s.abs.RemoveLibraryIssues(libID)
	if err != nil {
		return formatError("removing items with issues from library %s: %v%s", libID, err, absStatusHint(status, "library")), absRemoveIssuesOutput{}, nil
	}
	out := absRemoveIssuesOutput{LibraryID: libID, Status: status, Removed: true}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mustJSON(out)}},
	}, out, nil
}

type absRemoveItemInput struct {
	ItemID string `json:"item_id" jsonschema:"Audiobookshelf library item id, e.g. li_abc123"`
}

type absRemoveItemOutput struct {
	ItemID  string `json:"item_id"`
	Status  int    `json:"status"`
	Removed bool   `json:"removed"`
}

func (s *server) absRemoveItem(ctx context.Context, _ *mcp.CallToolRequest, in absRemoveItemInput) (*mcp.CallToolResult, absRemoveItemOutput, error) {
	if !s.abs.Configured() {
		return formatError(absNotConfigured), absRemoveItemOutput{}, nil
	}
	id := strings.TrimSpace(in.ItemID)
	if id == "" {
		return formatError("item_id is required"), absRemoveItemOutput{}, nil
	}
	status, err := s.abs.RemoveItem(id)
	if err != nil {
		return formatError("removing item %s: %v%s", id, err, absStatusHint(status, "item")), absRemoveItemOutput{}, nil
	}
	out := absRemoveItemOutput{ItemID: id, Status: status, Removed: true}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mustJSON(out)}},
	}, out, nil
}

func absStatusHint(status int, what string) string {
	switch status {
	case 403:
		return " (the API token's user lacks permission; removing requires an admin / delete-permitted user)"
	case 404:
		return " (" + what + " not found)"
	}
	return ""
}
