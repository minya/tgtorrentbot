package media

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/minya/logger"
	"github.com/minya/tgtorrentbot/internal/category"
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
func (c *JellyfinClient) GetItems(ctx context.Context) ([]JellyfinItem, error) {
	if c.url == "" || c.apiKey == "" {
		return nil, nil
	}

	reqURL := fmt.Sprintf("%s/Items?Recursive=true&Fields=Path&EnableImages=false&EnableUserData=false&EnableTotalRecordCount=false&IncludeItemTypes=Movie,Series,MusicAlbum,AudioBook,MusicVideo", c.url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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
// It returns an error for a failed request or an HTTP error status; an
// unconfigured client returns nil.
func (c *JellyfinClient) RefreshLibrary(ctx context.Context) error {
	if c.url == "" || c.apiKey == "" {
		return nil
	}

	reqURL := fmt.Sprintf("%s/Library/Refresh", c.url)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		logger.Error(err, "Failed to create Jellyfin refresh request")
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf(`MediaBrowser Token="%s"`, c.apiKey))

	resp, err := c.client.Do(req)
	if err != nil {
		logger.Error(err, "Failed to trigger Jellyfin library refresh")
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		logger.Warn("Jellyfin library refresh returned status %d", resp.StatusCode)
		return fmt.Errorf("library refresh returned status %d", resp.StatusCode)
	}
	logger.Info("Triggered Jellyfin library refresh")
	return nil
}

// RefreshItemsAt makes Jellyfin read the metadata of the albums and music
// videos at, inside or around rel again, replacing what it stored. rel is a
// path relative to the media root, as the download path lays it out
// ("music/Artist - Discography/2017 - Album"); Jellyfin's paths are matched
// from their "media" segment on, as GetItems does. A library scan
// (RefreshLibrary) does not do this: it reads changed files again but keeps
// the artists and album artists a track already has, so a tag change of
// those never reaches Jellyfin. Returns the names of the refreshed items,
// none when Jellyfin has no item there (not scanned yet). An unconfigured
// client refreshes nothing.
func (c *JellyfinClient) RefreshItemsAt(ctx context.Context, rel string) ([]string, error) {
	if c.url == "" || c.apiKey == "" {
		return nil, nil
	}
	rel = strings.Trim(filepath.ToSlash(filepath.Clean(rel)), "/")

	reqURL := fmt.Sprintf("%s/Items?Recursive=true&Fields=Path&EnableImages=false&EnableUserData=false&EnableTotalRecordCount=false&IncludeItemTypes=MusicAlbum,MusicVideo", c.url)
	var jResp jellyfinResponse
	if err := c.do(ctx, http.MethodGet, reqURL, &jResp); err != nil {
		return nil, fmt.Errorf("listing jellyfin items: %w", err)
	}

	var refreshed []string
	for _, ri := range jResp.Items {
		itemRel, ok := mediaRelPath(ri.Path)
		if !ok || !(pathWithin(itemRel, rel) || pathWithin(rel, itemRel)) {
			continue
		}
		q := url.Values{
			"metadataRefreshMode": {"FullRefresh"},
			"imageRefreshMode":    {"Default"},
			"replaceAllMetadata":  {"true"},
			"replaceAllImages":    {"false"},
		}
		refreshURL := fmt.Sprintf("%s/Items/%s/Refresh?%s", c.url, url.PathEscape(ri.ID), q.Encode())
		if err := c.do(ctx, http.MethodPost, refreshURL, nil); err != nil {
			return refreshed, fmt.Errorf("refreshing %q: %w", ri.Name, err)
		}
		refreshed = append(refreshed, ri.Name)
	}
	if len(refreshed) > 0 {
		logger.Info("Triggered Jellyfin metadata refresh of %d item(s) at %s", len(refreshed), rel)
	}
	return refreshed, nil
}

// do sends an authenticated request and decodes a JSON answer into out when
// out is not nil. An HTTP error status is an error.
func (c *JellyfinClient) do(ctx context.Context, method, reqURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf(`MediaBrowser Token="%s"`, c.apiKey))
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("jellyfin returned status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// mediaRelPath returns the part of a Jellyfin path after its "media"
// segment ("/media/music/A/B" → "music/A/B").
func mediaRelPath(p string) (string, bool) {
	parts := strings.Split(strings.Trim(filepath.ToSlash(p), "/"), "/")
	for i, part := range parts {
		if part == "media" && i+1 < len(parts) {
			return strings.Join(parts[i+1:], "/"), true
		}
	}
	return "", false
}

// pathWithin reports whether slash path p is base or lies inside it.
func pathWithin(p, base string) bool {
	return p == base || strings.HasPrefix(p, base+"/")
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
	return string(category.Others)
}
