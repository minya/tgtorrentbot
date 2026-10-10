package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	mcplib "github.com/minya/tgtorrentbot/internal/media"
)

func TestAbsRemoveIssuesUsesConfiguredLibrary(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/libraries" {
			t.Error("library lookup should be skipped when TGT_AUDIOBOOKSHELF_LIBRARY_ID is set")
		}
		path = r.Method + " " + r.URL.Path
	}))
	defer srv.Close()

	s := &server{
		config: Config{AudiobookshelfLibraryID: "lib_cfg"},
		abs:    mcplib.NewAudiobookshelfClient(srv.URL, "key"),
	}
	res, out, _ := s.absRemoveIssues(context.Background(), nil, absRemoveIssuesInput{})
	if res.IsError {
		t.Fatalf("unexpected error: %v", res.Content)
	}
	if path != "DELETE /api/libraries/lib_cfg/issues" || out.LibraryID != "lib_cfg" || !out.Removed {
		t.Fatalf("unexpected call %q / output %+v", path, out)
	}

	// An explicit library_id wins over the configured default.
	_, out, _ = s.absRemoveIssues(context.Background(), nil, absRemoveIssuesInput{LibraryID: "lib_arg"})
	if path != "DELETE /api/libraries/lib_arg/issues" || out.LibraryID != "lib_arg" {
		t.Fatalf("expected explicit library_id to win, got %q / %+v", path, out)
	}
}

func TestAbsRemoveToolsNotConfigured(t *testing.T) {
	s := &server{abs: mcplib.NewAudiobookshelfClient("", "")}
	if res, _, _ := s.absRemoveIssues(context.Background(), nil, absRemoveIssuesInput{}); !res.IsError {
		t.Error("abs_remove_issues: expected error when ABS isn't configured")
	}
	if res, _, _ := s.absRemoveItem(context.Background(), nil, absRemoveItemInput{ItemID: "li_1"}); !res.IsError {
		t.Error("abs_remove_item: expected error when ABS isn't configured")
	}
}

func TestAbsRemoveItemNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	s := &server{abs: mcplib.NewAudiobookshelfClient(srv.URL, "key")}
	res, _, _ := s.absRemoveItem(context.Background(), nil, absRemoveItemInput{ItemID: "li_gone"})
	if !res.IsError {
		t.Fatal("expected error for 404")
	}
}
