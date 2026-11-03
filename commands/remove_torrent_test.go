package commands

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/minya/telegram"
)

func removeUpdate() *telegram.Update {
	return &telegram.Update{Message: &telegram.Message{MessageID: 1, Chat: telegram.Chat{ID: 42}, From: &telegram.User{ID: 42}, Text: "/remove 99"}}
}

// F48: /remove for an id that does not exist says so and removes nothing.
func TestRemoveUnknownIDSaysSo(t *testing.T) {
	env, log := failingEnv(t, `{}`) // torrent-get answers with no torrents
	cmd := &RemoveTorrentCommand{TorrentID: 99, Env: env}

	if err := cmd.Handle(context.Background(), removeUpdate()); err != nil {
		t.Fatal(err)
	}
	methods, texts := log.snapshot()
	if slices.Contains(methods, "torrent-remove") {
		t.Errorf("RPCs %v include torrent-remove for an unknown id", methods)
	}
	if !slices.ContainsFunc(texts, func(s string) bool { return strings.Contains(s, "No torrent with id 99") }) || slices.Contains(texts, "Removed.") {
		t.Errorf("user got %q, want 'No torrent with id 99' and no 'Removed.'", texts)
	}
}
