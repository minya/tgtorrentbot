package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/minya/logger"
)

type AudiobookshelfItem struct {
	Name     string
	Category string
	ID       string
}

type AudiobookshelfClient struct {
	url    string
	apiKey string
	client *http.Client

	mu            sync.Mutex
	bookLibraryID string // cached by BookLibraryID
}

func NewAudiobookshelfClient(url, apiKey string) *AudiobookshelfClient {
	return &AudiobookshelfClient{
		url:    strings.TrimRight(url, "/"),
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

type absLibrariesResponse struct {
	Libraries []absLibrary `json:"libraries"`
}

type absLibrary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"` // "book" or "podcast"
}

type absLibraryItemsResponse struct {
	Results []absLibraryItem `json:"results"`
}

type absLibraryItem struct {
	ID    string   `json:"id"`
	Media absMedia `json:"media"`
	Ino   string   `json:"ino"`
	Path  string   `json:"path"`
	Name  string   `json:"relPath"`
}

type absMedia struct {
	Metadata absMetadata `json:"metadata"`
}

type absMetadata struct {
	Title string `json:"title"`
}

func (c *AudiobookshelfClient) doRequest(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url+path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return c.client.Do(req)
}

func (c *AudiobookshelfClient) GetItems(ctx context.Context) ([]AudiobookshelfItem, error) {
	if c.url == "" || c.apiKey == "" {
		return nil, nil
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/libraries")
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
		if err := ctx.Err(); err != nil {
			return items, err
		}
		libItems, err := c.getLibraryItems(ctx, lib.ID)
		if err != nil {
			logger.Error(err, "Failed to get items for library %s", lib.ID)
			continue
		}
		items = append(items, libItems...)
	}
	return items, nil
}

func (c *AudiobookshelfClient) getLibraryItems(ctx context.Context, libraryID string) ([]AudiobookshelfItem, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/api/libraries/%s/items", libraryID))
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

// RefreshLibrary starts a scan of every Audiobookshelf library. It returns
// the ids whose scan started and an error joining every failure; an
// unconfigured client returns nothing.
func (c *AudiobookshelfClient) RefreshLibrary(ctx context.Context) (scanned []string, err error) {
	if c.url == "" || c.apiKey == "" {
		return nil, nil
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/libraries")
	if err != nil {
		logger.Error(err, "Failed to list Audiobookshelf libraries for refresh")
		return nil, fmt.Errorf("listing libraries: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("Audiobookshelf libraries returned status %d during refresh", resp.StatusCode)
		return nil, fmt.Errorf("listing libraries: status %d", resp.StatusCode)
	}

	var libResp absLibrariesResponse
	if err := json.NewDecoder(resp.Body).Decode(&libResp); err != nil {
		logger.Error(err, "Failed to decode Audiobookshelf libraries for refresh")
		return nil, fmt.Errorf("decoding libraries: %w", err)
	}

	var errs []error
	for _, lib := range libResp.Libraries {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		scanResp, err := c.doRequest(ctx, http.MethodPost, fmt.Sprintf("/api/libraries/%s/scan", lib.ID))
		if err != nil {
			logger.Error(err, "Failed to trigger Audiobookshelf scan for library %s", lib.ID)
			errs = append(errs, fmt.Errorf("scanning library %s: %w", lib.ID, err))
			continue
		}
		scanResp.Body.Close()
		if scanResp.StatusCode >= 400 {
			logger.Warn("Audiobookshelf scan for library %s returned status %d", lib.ID, scanResp.StatusCode)
			errs = append(errs, fmt.Errorf("scanning library %s: status %d", lib.ID, scanResp.StatusCode))
			continue
		}
		logger.Info("Triggered Audiobookshelf library scan for %s", lib.Name)
		scanned = append(scanned, lib.ID)
	}
	return scanned, errors.Join(errs...)
}

// Configured reports whether both the URL and API key are set.
func (c *AudiobookshelfClient) Configured() bool {
	return c.url != "" && c.apiKey != ""
}

// BookLibraryID returns the id of the single library whose mediaType is
// "book", looking it up via GET /api/libraries on first use and caching it.
// It errors when there is no book library or more than one.
func (c *AudiobookshelfClient) BookLibraryID(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bookLibraryID != "" {
		return c.bookLibraryID, nil
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/api/libraries")
	if err != nil {
		return "", fmt.Errorf("requesting libraries: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("audiobookshelf libraries returned status %d", resp.StatusCode)
	}
	var libResp absLibrariesResponse
	if err := json.NewDecoder(resp.Body).Decode(&libResp); err != nil {
		return "", fmt.Errorf("decoding libraries response: %w", err)
	}

	var books []absLibrary
	for _, lib := range libResp.Libraries {
		if lib.MediaType == "book" {
			books = append(books, lib)
		}
	}
	switch len(books) {
	case 0:
		return "", fmt.Errorf("no audiobook (mediaType=book) library found")
	case 1:
		c.bookLibraryID = books[0].ID
		return c.bookLibraryID, nil
	default:
		names := make([]string, len(books))
		for i, b := range books {
			names[i] = fmt.Sprintf("%s (%s)", b.Name, b.ID)
		}
		return "", fmt.Errorf("multiple audiobook libraries found, pick one explicitly: %s", strings.Join(names, ", "))
	}
}

// RemoveLibraryIssues deletes every item in the library that Audiobookshelf
// flags as missing or invalid. Files on disk are not touched. It returns the
// HTTP status; non-2xx statuses are also reported as an error.
func (c *AudiobookshelfClient) RemoveLibraryIssues(ctx context.Context, libraryID string) (int, error) {
	return c.doDelete(ctx, fmt.Sprintf("/api/libraries/%s/issues", url.PathEscape(libraryID)))
}

// RemoveItem deletes a single library item by id. Without ?hard=1 ABS only
// drops the database entry; files on disk are not touched.
func (c *AudiobookshelfClient) RemoveItem(ctx context.Context, itemID string) (int, error) {
	return c.doDelete(ctx, fmt.Sprintf("/api/items/%s", url.PathEscape(itemID)))
}

func (c *AudiobookshelfClient) doDelete(ctx context.Context, path string) (int, error) {
	resp, err := c.doRequest(ctx, http.MethodDelete, path)
	if err != nil {
		return 0, fmt.Errorf("DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("DELETE %s returned status %d", path, resp.StatusCode)
	}
	return resp.StatusCode, nil
}
