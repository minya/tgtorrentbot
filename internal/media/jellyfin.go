package media

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/minya/logger"
)

// JellyfinItem represents a media item from the Jellyfin library.
type JellyfinItem struct {
	Name       string
	Category   string
	JellyfinID string
}

// JellyfinClient communicates with a Jellyfin server to retrieve library items.
type JellyfinClient struct {
	url    string
	apiKey string
	client *http.Client
}

// NewJellyfinClient creates a new Jellyfin API client. If url or apiKey is empty,
// GetItems will return an empty list.
func NewJellyfinClient(url, apiKey string) *JellyfinClient {
	return &JellyfinClient{
		url:    strings.TrimRight(url, "/"),
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

type jellyfinResponse struct {
	Items []jellyfinResponseItem `json:"Items"`
}

type jellyfinResponseItem struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
	Path string `json:"Path"`
}

// GetItems fetches all items from Jellyfin. Returns an empty list if Jellyfin
// is not configured (empty URL or API key).
func (c *JellyfinClient) GetItems() ([]JellyfinItem, error) {
	if c.url == "" || c.apiKey == "" {
		return nil, nil
	}

	reqURL := fmt.Sprintf("%s/Items?Recursive=true&Fields=Path&EnableImages=false&EnableUserData=false&EnableTotalRecordCount=false&IncludeItemTypes=Movie,Series,MusicAlbum,AudioBook,MusicVideo", c.url)
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
		name := folderNameFromPath(ri.Path)
		if name == "" {
			name = ri.Name
		}
		items = append(items, JellyfinItem{
			Name:       name,
			Category:   category,
			JellyfinID: ri.ID,
		})
	}
	return items, nil
}

// RefreshLibrary triggers a Jellyfin library scan so it picks up file changes.
func (c *JellyfinClient) RefreshLibrary() {
	if c.url == "" || c.apiKey == "" {
		return
	}

	reqURL := fmt.Sprintf("%s/Library/Refresh", c.url)
	req, err := http.NewRequest(http.MethodPost, reqURL, nil)
	if err != nil {
		logger.Error(err, "Failed to create Jellyfin refresh request")
		return
	}
	req.Header.Set("Authorization", fmt.Sprintf(`MediaBrowser Token="%s"`, c.apiKey))

	resp, err := c.client.Do(req)
	if err != nil {
		logger.Error(err, "Failed to trigger Jellyfin library refresh")
		return
	}
	logger.Debug("Jellyfin refresh response status: %v", resp)
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		logger.Warn("Jellyfin library refresh returned status %d", resp.StatusCode)
		return
	}
	logger.Info("Triggered Jellyfin library refresh")
}

func folderNameFromPath(p string) string {
	p = filepath.ToSlash(p)
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, part := range parts {
		if part == "media" && i+2 < len(parts) {
			return parts[i+2]
		}
	}
	return ""
}

func categoryFromPath(p string) string {
	p = filepath.ToSlash(p)
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, part := range parts {
		if part == "media" && i+1 < len(parts) {
			return strings.ToLower(parts[i+1])
		}
	}
	return "others"
}
