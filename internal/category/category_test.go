package category

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestNamesMatchAll(t *testing.T) {
	all := All()
	names := Names()
	if len(names) != len(all) {
		t.Fatalf("Names() has %d entries, All() has %d", len(names), len(all))
	}
	for i, c := range all {
		if names[i] != string(c) {
			t.Errorf("Names()[%d] = %q, want %q", i, names[i], c)
		}
		if c.DisplayName() == string(c) {
			t.Errorf("category %q has no display name", c)
		}
	}
}

func TestParse(t *testing.T) {
	for _, c := range All() {
		got, ok := Parse(string(c))
		if !ok || got != c {
			t.Errorf("Parse(%q) = %q, %v", c, got, ok)
		}
	}
	for _, s := range []string{"", "incomplete", "Movies", "unknown"} {
		if _, ok := Parse(s); ok {
			t.Errorf("Parse(%q) accepted an invalid category", s)
		}
	}
}

// TestComposeMountsCoverAllCategories guards the one category list that
// cannot be generated: the per-category volume mounts in docker-compose.yaml.
// Jellyfin needs /media/{category} and dufs needs /data/{category} for every
// category, or new downloads are invisible to them.
func TestComposeMountsCoverAllCategories(t *testing.T) {
	data, err := os.ReadFile("../../docker-compose.yaml")
	if err != nil {
		t.Fatalf("read docker-compose.yaml: %v", err)
	}
	var mounts []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- /var/transmission/downloads/") {
			mounts = append(mounts, strings.TrimPrefix(line, "- "))
		}
	}
	for _, c := range All() {
		for _, target := range []string{"/media/", "/data/"} {
			want := "/var/transmission/downloads/" + string(c) + ":" + target + string(c)
			if !slices.Contains(mounts, want) {
				t.Errorf("docker-compose.yaml has no volume %q", want)
			}
		}
	}
}
