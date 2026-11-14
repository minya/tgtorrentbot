package completion

import (
	"testing"

	"github.com/odwrtw/transmission"
)

func TestNewlyCompletedIgnoresMissingDoneDate(t *testing.T) {
	cur := transmission.TorrentMap{"h": {Name: "X", PercentDone: 1}}
	if got := NewlyCompleted(nil, cur, 0); len(got) != 0 {
		t.Fatalf("got %d, want 0 for a complete torrent without doneDate", len(got))
	}
}

func TestNewlyCompleted(t *testing.T) {
	prev := transmission.TorrentMap{
		"a": {PercentDone: 0.5},
		"b": {PercentDone: 1},
	}
	cur := transmission.TorrentMap{
		"a": {Name: "transition", PercentDone: 1},
		"b": {Name: "already done", PercentDone: 1, DoneDate: 200},
		"c": {Name: "new, done after since", PercentDone: 1, DoneDate: 150},
		"d": {Name: "new, done before since", PercentDone: 1, DoneDate: 50},
		"e": {Name: "downloading", PercentDone: 0.2},
	}
	got := map[string]bool{}
	for _, t := range NewlyCompleted(prev, cur, 100) {
		got[t.Name] = true
	}
	if len(got) != 2 || !got["transition"] || !got["new, done after since"] {
		t.Fatalf("got %v, want transition and new, done after since", got)
	}
}
