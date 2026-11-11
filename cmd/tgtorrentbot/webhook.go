package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

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
// anyone who learns the webhook URL. telegram.WebhookHandler acknowledges
// each update at once and handles it in the background (one chat's updates
// one after another in update_id order, widget queries excepted, at most 40
// at once), with a context
// that survives Telegram disconnecting; the returned Webhook's Wait lets
// shutdown finish the updates still being handled. Failures and panics in a
// command are logged and never take the bot down.
func newWebhookHandler(secret string, handleUpdate func(context.Context, *telegram.Update) error) *telegram.Webhook {
	return telegram.WebhookHandler(secret, handleUpdate, func(upd *telegram.Update, err error) {
		logger.Error(err, "Failed to handle update %d", upd.UpdateID)
	}, telegram.WithOrderKey(updateOrderKey))
}

// updateOrderKey orders a chat's updates (telegram.UpdateChatID), except
// widget queries: a progress widget asks every 5 s and must be answered
// within 15 s, so it must not wait behind a search in the same chat, and it
// changes nothing that the chat's commands depend on.
func updateOrderKey(upd *telegram.Update) (int64, bool) {
	if upd.WidgetQuery != nil || upd.SharedWidgetQuery != nil {
		return 0, false
	}
	return telegram.UpdateChatID(upd)
}
