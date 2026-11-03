// Package deploycheck holds tests of the deployment files.
package deploycheck

import (
	"os"
	"strings"
	"testing"
)

// F51: every third-party image in docker-compose.yaml names a release, so a
// redeploy (./restart recreates all containers) never pulls an unreviewed
// major upgrade. Images built here (*_img) are exempt.
func TestComposeImagesArePinned(t *testing.T) {
	data, err := os.ReadFile("../../docker-compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "image:") {
			continue // commented-out services start with "#image:"
		}
		image := strings.TrimSpace(strings.TrimPrefix(trimmed, "image:"))
		name, tag, hasTag := strings.Cut(image[strings.LastIndex(image, "/")+1:], ":")
		if strings.HasSuffix(name, "_img") {
			continue
		}
		checked++
		if !hasTag || tag == "" || tag == "latest" || strings.HasPrefix(tag, "latest") {
			t.Errorf("docker-compose.yaml:%d: %q is not pinned to a release", i+1, image)
		}
	}
	if checked == 0 {
		t.Fatal("no third-party images found; did the file move?")
	}
}
