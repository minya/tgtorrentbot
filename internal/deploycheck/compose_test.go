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

// F129: the internet-facing bot container gets no added capability (port 80
// binds without NET_BIND_SERVICE: Docker sets ip_unprivileged_port_start=0)
// and no mount but the downloads, which the auto-fixer works on, at the
// path Transmission sees (TGT_DOWNLOADPATH).
func TestBotServiceMountsOnlyDownloads(t *testing.T) {
	data, err := os.ReadFile("../../docker-compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	start := -1
	for i, l := range lines {
		if l == "  tgtorrentbot:" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("tgtorrentbot service not found; did it move?")
	}
	var mounts []string
	inVolumes := false
	for _, l := range lines[start+1:] {
		if strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, "    ") && strings.TrimSpace(l) != "" {
			break // the next service
		}
		key := strings.TrimSpace(l)
		if key == "cap_add:" {
			t.Error("tgtorrentbot service has cap_add")
		}
		if strings.HasPrefix(l, "    ") && !strings.HasPrefix(l, "      ") {
			inVolumes = key == "volumes:"
			continue
		}
		if inVolumes && strings.HasPrefix(key, "- ") {
			mounts = append(mounts, strings.TrimPrefix(key, "- "))
		}
	}
	want := "/var/transmission/downloads:/downloads"
	if len(mounts) != 1 || mounts[0] != want {
		t.Errorf("tgtorrentbot mounts %q, want only %q", mounts, want)
	}
	if !strings.Contains(string(data), "      - TGT_DOWNLOADPATH=/downloads\n") {
		t.Error("TGT_DOWNLOADPATH is no longer /downloads; keep the bot's mount at the path Transmission sees")
	}
}
