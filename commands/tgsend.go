package commands

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/minya/logger"
	"github.com/minya/telegram"
)

// maxRetryAfter caps how long a send waits on Telegram flood control before
// its one retry; a longer wait is reported as the error instead.
const maxRetryAfter = 30 * time.Second

// retryAfter returns how long Telegram asked to wait (HTTP 429), or 0.
func retryAfter(err error) time.Duration {
	var tgErr *telegram.Error
	if errors.As(err, &tgErr) && tgErr.RetryAfter > 0 {
		return time.Duration(tgErr.RetryAfter) * time.Second
	}
	return 0
}

// withFloodRetry runs call, and once more after Telegram's retry_after when
// the first attempt was rate-limited (a search sends up to ten messages in a
// row, which is the burst Telegram throttles).
func withFloodRetry(ctx context.Context, call func() error) error {
	err := call()
	if d := retryAfter(err); d > 0 && d <= maxRetryAfter {
		select {
		case <-time.After(d):
			err = call()
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		}
	}
	return err
}

// SendMessage sends a message and logs a failure, which callers used to
// discard. It returns the error for callers whose result is the message.
func SendMessage(ctx context.Context, api *telegram.API, params telegram.SendMessageParams) error {
	err := withFloodRetry(ctx, func() error {
		_, err := api.SendMessage(ctx, params)
		return err
	})
	if err != nil {
		logger.Error(err, "Failed to send Telegram message to chat %d", params.ChatID)
	}
	return err
}

// EditMessageText edits a message and logs a failure. Telegram's "message is
// not modified" (the same page tapped twice) is not a failure.
func EditMessageText(ctx context.Context, api *telegram.API, params telegram.EditMessageTextParams) error {
	err := withFloodRetry(ctx, func() error {
		_, err := api.EditMessageText(ctx, params)
		return err
	})
	if err != nil && strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	if err != nil {
		logger.Error(err, "Failed to edit Telegram message %d in chat %d", params.MessageID, params.ChatID)
	}
	return err
}

// AnswerCallback answers a button press. A failure only leaves the button's
// spinner running a little longer, so it is logged at debug level.
func AnswerCallback(ctx context.Context, api *telegram.API, params telegram.AnswerCallbackQueryParams) {
	if err := api.AnswerCallbackQuery(ctx, params); err != nil {
		logger.Debug("Failed to answer callback query %s: %v", params.CallbackQueryID, err)
	}
}
