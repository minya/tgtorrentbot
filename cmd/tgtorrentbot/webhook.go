package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"

	"github.com/minya/logger"
	"github.com/minya/telegram"
)

// deriveWebhookSecret derives the webhook secret_token from the bot token, so
// the webhook is authenticated without extra configuration. Telegram allows
// 1-256 characters of A-Z, a-z, 0-9, _ and -; hex fits.
func deriveWebhookSecret(botToken string) string {
	sum := sha256.Sum256([]byte("tgtorrentbot/webhook-secret\x00" + botToken))
	return hex.EncodeToString(sum[:])
}

// validWebhookSecret reports whether s is a secret_token Telegram accepts.
func validWebhookSecret(s string) bool {
	if len(s) == 0 || len(s) > 256 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// newWebhookHandler serves Telegram webhook updates. Requests without the
// X-Telegram-Bot-Api-Secret-Token header matching secret are rejected with
// 401 (checked in constant time by telegram.WebhookHandler, which also
// accepts only POST and caps the body), so updates cannot be forged by
// anyone who learns the webhook URL. Accepted updates are acknowledged
// immediately and handled in the background so Telegram does not retry them;
// inFlight counts those background handlers so shutdown can wait for them.
func newWebhookHandler(secret string, handleUpdate func(context.Context, *telegram.Update) error, inFlight *sync.WaitGroup) http.Handler {
	return telegram.WebhookHandler(secret, func(ctx context.Context, upd *telegram.Update) error {
		inFlight.Add(1)
		go func() {
			defer inFlight.Done()
			// A panic in a command must not take the whole bot down: this
			// goroutine is not covered by net/http's per-request recovery.
			defer func() {
				if r := recover(); r != nil {
					logger.Error(fmt.Errorf("panic: %v\n%s", r, debug.Stack()), "Panic while handling update %d", upd.UpdateID)
				}
			}()
			if err := handleUpdate(context.WithoutCancel(ctx), upd); err != nil {
				logger.Error(err, "Failed to handle update from request")
			}
		}()
		return nil
	}, nil)
}
