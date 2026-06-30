package main

import (
	"context"
	"testing"
)

func TestRemoveTorrentNoTransmission(t *testing.T) {
	s := &server{config: Config{}}
	res, out, _ := s.removeTorrent(context.Background(), nil, removeTorrentInput{TorrentID: 1})
	if !res.IsError {
		t.Fatal("expected error when Transmission is not configured")
	}
	if out.Removed {
		t.Fatal("Removed should be false")
	}
}
