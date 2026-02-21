package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// JellyfinItem represents a media item from the Jellyfin library.
type JellyfinItem struct {
	Name       string
	Category   string
	JellyfinID string
}

// jellyfinClient communicates with a Jellyfin server to retrieve library items.
type jellyfinClient struct {
	url    string
	apiKey string
	client *http.Client
}

// newJellyfinClient creates a new Jellyfin API client. If url or apiKey is empty,
// GetItems will return an empty list.
func newJellyfinClient(url, apiKey string) *jellyfinClient {
	return &jellyfinClient{
		url:    strings.TrimRight(url, "/"),
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// jellyfinResponse represents the top-level Jellyfin /Items API response.
type jellyfinResponse struct {
	Items []jellyfinResponseItem `json:"Items"`
}

// jellyfinResponseItem represents a single item in the Jellyfin API response.
type jellyfinResponseItem struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
	Path string `json:"Path"`
}

// GetItems fetches all items from Jellyfin. Returns an empty list if Jellyfin
// is not configured (empty URL or API key).
func (c *jellyfinClient) GetItems() ([]JellyfinItem, error) {
	if c.url == "" || c.apiKey == "" {
		return nil, nil
	}

	reqURL := fmt.Sprintf("%s/Items?Recursive=true&Fields=Path,MediaSources&IncludeItemTypes=Movie,Series,MusicAlbum,AudioBook,MusicVideo", c.url)
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf(`MediaBrowser Token="%s"`, c.apiKey))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting jellyfin items: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jellyfin returned status %d", resp.StatusCode)
	}

	var jResp jellyfinResponse
	if err := json.NewDecoder(resp.Body).Decode(&jResp); err != nil {
		return nil, fmt.Errorf("decoding jellyfin response: %w", err)
	}

	items := make([]JellyfinItem, 0, len(jResp.Items))
	for _, ri := range jResp.Items {
		category := categoryFromPath(ri.Path)
		items = append(items, JellyfinItem{
			Name:       ri.Name,
			Category:   category,
			JellyfinID: ri.ID,
		})
	}
	return items, nil
}

// categoryFromPath extracts the category from a Jellyfin item path.
// Jellyfin paths look like /media/{category}/ItemName/... so the category
// is the second path component after /media/.
func categoryFromPath(p string) string {
	// Normalize to forward slashes and clean the path.
	p = filepath.ToSlash(p)
	// Split and find the segment after "media".
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, part := range parts {
		if part == "media" && i+1 < len(parts) {
			return strings.ToLower(parts[i+1])
		}
	}
	return "others"
}
