package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/minya/logger"
)

type AudiobookshelfItem struct {
	Name     string
	Category string
	ID       string
}

type audiobookshelfClient struct {
	url    string
	apiKey string
	client *http.Client
}

func newAudiobookshelfClient(url, apiKey string) *audiobookshelfClient {
	return &audiobookshelfClient{
		url:    strings.TrimRight(url, "/"),
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

type absLibrariesResponse struct {
	Libraries []absLibrary `json:"libraries"`
}

type absLibrary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type absLibraryItemsResponse struct {
	Results []absLibraryItem `json:"results"`
}

type absLibraryItem struct {
	ID    string      `json:"id"`
	Media absMedia    `json:"media"`
	Ino   string      `json:"ino"`
	Path  string      `json:"path"`
	Name  string      `json:"relPath"`
}

type absMedia struct {
	Metadata absMetadata `json:"metadata"`
}

type absMetadata struct {
	Title string `json:"title"`
}

func (c *audiobookshelfClient) doRequest(method, path string) (*http.Response, error) {
	req, err := http.NewRequest(method, c.url+path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return c.client.Do(req)
}

func (c *audiobookshelfClient) GetItems() ([]AudiobookshelfItem, error) {
	if c.url == "" || c.apiKey == "" {
		return nil, nil
	}

	resp, err := c.doRequest(http.MethodGet, "/api/libraries")
	if err != nil {
		return nil, fmt.Errorf("requesting libraries: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("audiobookshelf libraries returned status %d", resp.StatusCode)
	}

	var libResp absLibrariesResponse
	if err := json.NewDecoder(resp.Body).Decode(&libResp); err != nil {
		return nil, fmt.Errorf("decoding libraries response: %w", err)
	}

	var items []AudiobookshelfItem
	for _, lib := range libResp.Libraries {
		libItems, err := c.getLibraryItems(lib.ID)
		if err != nil {
			logger.Error(err, "Failed to get items for library %s", lib.ID)
			continue
		}
		items = append(items, libItems...)
	}
	return items, nil
}

func (c *audiobookshelfClient) getLibraryItems(libraryID string) ([]AudiobookshelfItem, error) {
	resp, err := c.doRequest(http.MethodGet, fmt.Sprintf("/api/libraries/%s/items", libraryID))
	if err != nil {
		return nil, fmt.Errorf("requesting library items: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("audiobookshelf library items returned status %d", resp.StatusCode)
	}

	var itemsResp absLibraryItemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&itemsResp); err != nil {
		return nil, fmt.Errorf("decoding library items: %w", err)
	}

	items := make([]AudiobookshelfItem, 0, len(itemsResp.Results))
	for _, ri := range itemsResp.Results {
		name := ri.Name
		if name == "" {
			name = ri.Media.Metadata.Title
		}
		if name == "" {
			continue
		}
		items = append(items, AudiobookshelfItem{
			Name:     name,
			Category: "audiobooks",
			ID:       ri.ID,
		})
	}
	return items, nil
}

func (c *audiobookshelfClient) RefreshLibrary() {
	if c.url == "" || c.apiKey == "" {
		return
	}

	resp, err := c.doRequest(http.MethodGet, "/api/libraries")
	if err != nil {
		logger.Error(err, "Failed to list Audiobookshelf libraries for refresh")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("Audiobookshelf libraries returned status %d during refresh", resp.StatusCode)
		return
	}

	var libResp absLibrariesResponse
	if err := json.NewDecoder(resp.Body).Decode(&libResp); err != nil {
		logger.Error(err, "Failed to decode Audiobookshelf libraries for refresh")
		return
	}

	for _, lib := range libResp.Libraries {
		scanResp, err := c.doRequest(http.MethodPost, fmt.Sprintf("/api/libraries/%s/scan", lib.ID))
		if err != nil {
			logger.Error(err, "Failed to trigger Audiobookshelf scan for library %s", lib.ID)
			continue
		}
		scanResp.Body.Close()
		if scanResp.StatusCode >= 400 {
			logger.Warn("Audiobookshelf scan for library %s returned status %d", lib.ID, scanResp.StatusCode)
			continue
		}
		logger.Info("Triggered Audiobookshelf library scan for %s", lib.Name)
	}
}
