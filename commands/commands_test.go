package commands

import (
	"testing"

	"github.com/minya/telegram"
)

func TestSearchCommandFactoryAcceptsSlashSearch(t *testing.T) {
	factory := SearchCommandFactory{}
	upd := telegram.Update{
		Message: &telegram.Message{
			MessageId: 1,
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
			MessageId: 5,
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
			MessageId: 7,
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
			MessageId: 10,
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
			MessageId: 12,
			Document: &telegram.Document{
				FileID: "file123",
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
			MessageId: 13,
		},
	}

	ok, _ := factory.Accepts(&upd)
	if ok {
		t.Fatalf("expected factory to reject update without document")
	}
}
