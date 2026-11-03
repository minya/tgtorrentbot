package media

import (
	"context"
	"fmt"
	"sync"

	"github.com/minya/logger"
)

// FetchLibraries gets Jellyfin and Audiobookshelf items concurrently. Both
// are optional sources: a failure is logged, that source is left out, and
// the failure is returned in errs ("jellyfin unavailable: …") so a caller
// can tell "not in the library" from "the library did not answer".
// The caller bounds the wait with ctx (a deadline); once it expires the
// in-flight requests are cancelled, so nothing keeps running afterwards.
func FetchLibraries(ctx context.Context, jf *JellyfinClient, abs *AudiobookshelfClient) (jfItems []JellyfinItem, absItems []AudiobookshelfItem, errs []error) {
	var (
		wg            sync.WaitGroup
		jfErr, absErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		items, err := jf.GetItems(ctx)
		if err != nil {
			logger.Error(err, "Failed to get Jellyfin items; listing without them")
			jfErr = fmt.Errorf("jellyfin unavailable: %w", err)
			return
		}
		jfItems = items
	}()
	go func() {
		defer wg.Done()
		items, err := abs.GetItems(ctx)
		if err != nil {
			logger.Error(err, "Failed to get Audiobookshelf items; listing without them")
			absErr = fmt.Errorf("audiobookshelf unavailable: %w", err)
			return
		}
		absItems = items
	}()
	wg.Wait()
	for _, err := range []error{jfErr, absErr} {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return jfItems, absItems, errs
}
