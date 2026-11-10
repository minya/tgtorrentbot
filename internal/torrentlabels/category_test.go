package torrentlabels

import (
	"testing"

	"github.com/minya/tgtorrentbot/internal/category"
)

// F105: one fallback for every binary. Missing or empty labels are Others;
// an unknown label (a category removed since) is kept as is.
func TestCategory(t *testing.T) {
	for _, tc := range []struct {
		labels []string
		want   category.Category
	}{
		{nil, category.Others},
		{[]string{"42"}, category.Others},
		{[]string{"42", ""}, category.Others},
		{[]string{"42", "shows", "7"}, category.Shows},
		{[]string{"42", "documentaries"}, "documentaries"},
	} {
		if got := Category(tc.labels); got != tc.want {
			t.Errorf("Category(%q) = %q, want %q", tc.labels, got, tc.want)
		}
	}
	if got := Category([]string{"42"}).DisplayName(); got != "Other" {
		t.Errorf("legacy torrent displays as %q, want Other", got)
	}
}
