package commands

import (
	"testing"

	"github.com/minya/telegram"
)

func TestSearchCommandFactoryAcceptsSlashSearch(t *testing.T) {
	factory := SearchCommandFactory{}
	upd := telegram.Update{
		Message: &telegram.Message{
			MessageID: 1,
			Text:      "/search   some query  ",
		},
	}

	ok, cmd := factory.Accepts(&upd)
	if !ok {
		t.Fatalf("expected factory to accept /search update")
	}

	searchCmd, ok := cmd.(*SearchCommand)
	if !ok {
		t.Fatalf("expected command to be *SearchCommand, got %T", cmd)
	}

	if searchCmd.Pattern != "some query" {
		t.Fatalf("expected pattern to be 'some query', got %q", searchCmd.Pattern)
	}
}

func TestSearchCommandFactoryAcceptsPlainText(t *testing.T) {
	factory := SearchCommandFactory{}
	upd := telegram.Update{
		Message: &telegram.Message{
			MessageID: 5,
			Text:      "   movie title  ",
		},
	}

	ok, cmd := factory.Accepts(&upd)
	if !ok {
		t.Fatalf("expected factory to accept plain text search")
	}

	searchCmd, ok := cmd.(*SearchCommand)
	if !ok {
		t.Fatalf("expected command to be *SearchCommand, got %T", cmd)
	}

	if searchCmd.Pattern != "movie title" {
		t.Fatalf("expected trimmed pattern 'movie title', got %q", searchCmd.Pattern)
	}
}

func TestSearchCommandFactoryRejectsOtherCommands(t *testing.T) {
	factory := SearchCommandFactory{}
	upd := telegram.Update{
		Message: &telegram.Message{
			MessageID: 7,
			Text:      "/list",
		},
	}

	ok, _ := factory.Accepts(&upd)
	if ok {
		t.Fatalf("expected factory to reject non-search command")
	}
}

func TestDownloadCommandFactoryAcceptsCallback(t *testing.T) {
	factory := DownloadCommandFactory{}
	upd := telegram.Update{
		CallbackQuery: &telegram.CallbackQuery{
			Data: "/dl https://example.org/download?id=123",
		},
	}

	ok, cmd := factory.Accepts(&upd)
	if !ok {
		t.Fatalf("expected download command to be accepted for callback data")
	}

	downloadCmd, ok := cmd.(*DownloadCommand)
	if !ok {
		t.Fatalf("expected command to be *DownloadCommand, got %T", cmd)
	}

	want := "https://example.org/download?id=123"
	if downloadCmd.URL != want {
		t.Fatalf("expected URL %q, got %q", want, downloadCmd.URL)
	}
}

func TestDownloadCommandFactoryRejectsWithoutCallback(t *testing.T) {
	factory := DownloadCommandFactory{}
	upd := telegram.Update{
		Message: &telegram.Message{
			MessageID: 10,
			Text:      "/dl https://example.org/download?id=456",
		},
	}

	ok, _ := factory.Accepts(&upd)
	if ok {
		t.Fatalf("expected factory to reject download command without callback data")
	}
}

func TestDownloadByFileCommandFactoryAcceptsDocument(t *testing.T) {
	factory := DownloadByFileCommandFactory{}
	upd := telegram.Update{
		Message: &telegram.Message{
			MessageID: 12,
			Document: &telegram.Document{
				FileID:   "file123",
				FileName: "Some.Show.S01.torrent",
			},
		},
	}

	ok, cmd := factory.Accepts(&upd)
	if !ok {
		t.Fatalf("expected factory to accept document upload")
	}

	downloadCmd, ok := cmd.(*DownloadByFileCommand)
	if !ok {
		t.Fatalf("expected *DownloadByFileCommand, got %T", cmd)
	}

	if downloadCmd.Doc == nil || downloadCmd.Doc.FileID != "file123" {
		t.Fatalf("document not propagated to command: %#v", downloadCmd.Doc)
	}
}

func TestDownloadByFileCommandFactoryRejectsWithoutDocument(t *testing.T) {
	factory := DownloadByFileCommandFactory{}
	upd := telegram.Update{
		Message: &telegram.Message{
			MessageID: 13,
		},
	}

	ok, _ := factory.Accepts(&upd)
	if ok {
		t.Fatalf("expected factory to reject update without document")
	}
}

// F76: only .torrent documents get a category prompt.
func TestDownloadByFileCommandFactoryAcceptsOnlyTorrents(t *testing.T) {
	factory := DownloadByFileCommandFactory{}
	for _, tc := range []struct {
		doc  telegram.Document
		want bool
	}{
		{telegram.Document{FileID: "a", FileName: "x.torrent"}, true},
		{telegram.Document{FileID: "b", FileName: "X.TORRENT"}, true},
		{telegram.Document{FileID: "c", FileName: "noext", MimeType: "application/x-bittorrent"}, true},
		{telegram.Document{FileID: "d", FileName: "book.pdf", MimeType: "application/pdf"}, false},
		{telegram.Document{FileID: "e", FileName: "song.mp3", MimeType: "audio/mpeg"}, false},
		{telegram.Document{FileID: "f"}, false},
	} {
		doc := tc.doc
		ok, _ := factory.Accepts(&telegram.Update{Message: &telegram.Message{MessageID: 1, Document: &doc}})
		if ok != tc.want {
			t.Errorf("Accepts(%q, %q) = %v, want %v", doc.FileName, doc.MimeType, ok, tc.want)
		}
	}
}
