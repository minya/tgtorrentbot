package media

import (
	"context"
	"sync"

	"github.com/minya/logger"
)

// FetchLibraries gets Jellyfin and Audiobookshelf items concurrently. Both
// are optional sources: a failure is logged and that source is left out.
// The caller bounds the wait with ctx (a deadline); once it expires the
// in-flight requests are cancelled, so nothing keeps running afterwards.
func FetchLibraries(ctx context.Context, jf *JellyfinClient, abs *AudiobookshelfClient) ([]JellyfinItem, []AudiobookshelfItem) {
	var (
		wg       sync.WaitGroup
		jfItems  []JellyfinItem
		absItems []AudiobookshelfItem
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		items, err := jf.GetItems(ctx)
		if err != nil {
			logger.Error(err, "Failed to get Jellyfin items; listing without them")
			return
		}
		jfItems = items
	}()
	go func() {
		defer wg.Done()
		items, err := abs.GetItems(ctx)
		if err != nil {
			logger.Error(err, "Failed to get Audiobookshelf items; listing without them")
			return
		}
		absItems = items
	}()
	wg.Wait()
	return jfItems, absItems
}
