package torrentlabels

import "testing"

func TestNewLayout(t *testing.T) {
	l := New(-5, 9, "shows")
	if len(l) != 3 || l[0] != "-5" || l[1] != "shows" || l[2] != "9" {
		t.Fatalf("New = %v", l)
	}
}
