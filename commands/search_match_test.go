package commands

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/minya/telegram"
)

// F49: multi-line /search queries match, whitespace-only text is not a
// search, and a bare /search asks for a query.
func TestMatchSearchQueries(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		isMatch bool
	}{
		{"/search foo\nbar", "foo bar", true},
		{"/search   The Wire \n 2002  ", "The Wire 2002", true},
		{"/search", "", true},
		{"/search   ", "", true},
		{"movie\ntitle", "movie title", true},
		{"   ", "", false},
		{"\n\t", "", false},
		{"/searchfoo", "", false},
		{"/list", "", false},
	} {
		got := Match(tc.in)
		if (got != nil) != tc.isMatch {
			t.Errorf("Match(%q) = %v, want match=%v", tc.in, got, tc.isMatch)
			continue
		}
		if got != nil && got.Pattern != tc.want {
			t.Errorf("Match(%q).Pattern = %q, want %q", tc.in, got.Pattern, tc.want)
		}
	}
}

func TestBareSearchRepliesUsage(t *testing.T) {
	env, log := failingEnv(t, `{}`)
	cmd := &SearchCommand{Pattern: "", Env: env}
	upd := &telegram.Update{Message: &telegram.Message{MessageID: 1, Chat: telegram.Chat{ID: 42}, Text: "/search"}}
	if err := cmd.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if _, texts := log.snapshot(); !slices.ContainsFunc(texts, func(s string) bool { return strings.HasPrefix(s, "Usage: /search") }) {
		t.Fatalf("user got %q, want a usage hint", texts)
	}
}
