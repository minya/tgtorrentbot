package torrentlabels

import "testing"

func TestOwnedBy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels []string
		user   int64
		want   bool
	}{
		{"group download, owner", New(-100123, 42, "movies"), 42, true},
		{"group download, group id is not a user", New(-100123, 42, "movies"), -100123, false},
		{"private chat", New(42, 42, "movies"), 42, true},
		{"other user", New(42, 42, "movies"), 7, false},
		{"legacy two labels", []string{"42", "movies"}, 42, true},
		{"legacy one label", []string{"42"}, 42, true},
		{"no labels", nil, 42, false},
		{"garbage owner", []string{"42", "movies", "x"}, 42, false},
	} {
		if got := OwnedBy(tc.labels, tc.user); got != tc.want {
			t.Errorf("%s: OwnedBy(%v, %d) = %v, want %v", tc.name, tc.labels, tc.user, got, tc.want)
		}
	}
}

func TestNewLayout(t *testing.T) {
	l := New(-5, 9, "shows")
	if len(l) != 3 || l[0] != "-5" || l[1] != "shows" || l[2] != "9" {
		t.Fatalf("New = %v", l)
	}
}
