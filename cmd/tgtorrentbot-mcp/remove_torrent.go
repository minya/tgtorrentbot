package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/odwrtw/transmission"
)

type removeTorrentInput struct {
	TorrentID  int  `json:"torrent_id" jsonschema:"Transmission torrent id (the torrent_id field from list_media)"`
	DeleteData bool `json:"delete_data,omitempty" jsonschema:"when true, also delete the downloaded files; default false keeps the files on disk and only stops Transmission from tracking them"`
}

type removeTorrentOutput struct {
	Removed    bool `json:"removed"`
	DeleteData bool `json:"delete_data"`
}

func (s *server) removeTorrent(ctx context.Context, _ *mcp.CallToolRequest, in removeTorrentInput) (*mcp.CallToolResult, removeTorrentOutput, error) {
	if s.trans == nil {
		return formatError("Transmission is not configured (set TGT_RPC_ADDR, TGT_RPC_USER, TGT_RPC_PASSWORD)"), removeTorrentOutput{}, nil
	}
	all, err := s.trans.GetTorrents()
	if err != nil {
		return formatError("failed to fetch torrents: %v", err), removeTorrentOutput{}, nil
	}
	var match []*transmission.Torrent
	for _, t := range all {
		if t.ID == in.TorrentID {
			match = append(match, t)
		}
	}
	if len(match) == 0 {
		return formatError("no torrent with id %d", in.TorrentID), removeTorrentOutput{}, nil
	}
	if err := s.trans.RemoveTorrents(match, in.DeleteData); err != nil {
		return formatError("failed to remove torrent %d: %v", in.TorrentID, err), removeTorrentOutput{}, nil
	}
	out := removeTorrentOutput{Removed: true, DeleteData: in.DeleteData}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mustJSON(out)}},
	}, out, nil
}
