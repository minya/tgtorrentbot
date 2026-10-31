package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minya/tgtorrentbot/internal/category"
)

func TestHandleCategoriesJS(t *testing.T) {
	rec := httptest.NewRecorder()
	handleCategoriesJS(rec, httptest.NewRequest(http.MethodGet, "/categories.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("Content-Type = %q", ct)
	}
	body := strings.TrimSpace(rec.Body.String())
	const prefix = "window.CATEGORIES = "
	if !strings.HasPrefix(body, prefix) || !strings.HasSuffix(body, ";") {
		t.Fatalf("unexpected body: %q", body)
	}
	var got []categoryInfo
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(body, prefix), ";")), &got); err != nil {
		t.Fatalf("body is not a JSON category list: %v", err)
	}
	all := category.All()
	if len(got) != len(all) {
		t.Fatalf("got %d categories, want %d", len(got), len(all))
	}
	for i, c := range all {
		if got[i].Key != c.String() || got[i].Name != c.DisplayName() {
			t.Errorf("entry %d = %+v, want {%s %s}", i, got[i], c, c.DisplayName())
		}
	}
}

func TestHandleCategoriesJSRejectsPost(t *testing.T) {
	rec := httptest.NewRecorder()
	handleCategoriesJS(rec, httptest.NewRequest(http.MethodPost, "/categories.js", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestValidCategoriesExcludesIncomplete(t *testing.T) {
	for _, c := range validCategories {
		if c == "incomplete" {
			t.Fatal("validCategories must not contain the virtual incomplete category")
		}
	}
}
