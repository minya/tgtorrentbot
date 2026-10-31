package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/minya/tgtorrentbot/internal/category"
)

// categoryInfo is one entry of the category list served to the frontend.
type categoryInfo struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// categoriesJS is the body of /categories.js: the download categories from
// internal/category as a global the embedded page builds its category grid,
// counts and download modal from. Computed once; the list is static.
var categoriesJS = buildCategoriesJS()

func buildCategoriesJS() []byte {
	all := category.All()
	infos := make([]categoryInfo, len(all))
	for i, c := range all {
		infos[i] = categoryInfo{Key: c.String(), Name: c.DisplayName()}
	}
	data, err := json.Marshal(infos)
	if err != nil {
		panic(fmt.Sprintf("marshal categories: %v", err))
	}
	return []byte("window.CATEGORIES = " + string(data) + ";\n")
}

// handleCategoriesJS serves the category list. It needs no Telegram init
// data: the list is not sensitive and the page loads it before it has any.
func handleCategoriesJS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(categoriesJS)
}
