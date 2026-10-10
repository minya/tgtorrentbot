package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func librariesHandler(t *testing.T, libs []absLibrary, calls *int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/libraries" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		*calls++
		json.NewEncoder(w).Encode(absLibrariesResponse{Libraries: libs})
	}
}

func TestBookLibraryIDPicksBookAndCaches(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(librariesHandler(t, []absLibrary{
		{ID: "lib_pod", Name: "Podcasts", MediaType: "podcast"},
		{ID: "lib_book", Name: "Audiobooks", MediaType: "book"},
	}, &calls))
	defer srv.Close()

	c := NewAudiobookshelfClient(srv.URL, "key")
	for range 2 {
		id, err := c.BookLibraryID()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "lib_book" {
			t.Fatalf("expected lib_book, got %s", id)
		}
	}
	if calls != 1 {
		t.Errorf("expected library lookup to be cached, got %d calls", calls)
	}
}

func TestBookLibraryIDAmbiguous(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(librariesHandler(t, []absLibrary{
		{ID: "a", Name: "A", MediaType: "book"},
		{ID: "b", Name: "B", MediaType: "book"},
	}, &calls))
	defer srv.Close()

	if _, err := NewAudiobookshelfClient(srv.URL, "key").BookLibraryID(); err == nil {
		t.Fatal("expected error for multiple book libraries")
	}
}

func TestRemoveLibraryIssuesAndItem(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		got = append(got, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		if r.URL.Path == "/api/items/li_missing" {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewAudiobookshelfClient(srv.URL, "key")
	if status, err := c.RemoveLibraryIssues("lib_book"); err != nil || status != 200 {
		t.Fatalf("RemoveLibraryIssues: status=%d err=%v", status, err)
	}
	if status, err := c.RemoveItem("li_1"); err != nil || status != 200 {
		t.Fatalf("RemoveItem: status=%d err=%v", status, err)
	}
	if status, err := c.RemoveItem("li_missing"); err == nil || status != 404 {
		t.Fatalf("expected 404 error, got status=%d err=%v", status, err)
	}

	want := []string{
		"DELETE /api/libraries/lib_book/issues?",
		"DELETE /api/items/li_1?", // no ?hard=1: files must stay on disk
		"DELETE /api/items/li_missing?",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}
