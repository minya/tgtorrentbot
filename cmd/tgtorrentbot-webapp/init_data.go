package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type initData struct {
	values url.Values
}

func newInitData(initDataStr string) (*initData, error) {
	values, err := url.ParseQuery(initDataStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse init data: %w", err)
	}
	return &initData{values: values}, nil
}

func (d *initData) Get(key string) string {
	return d.values.Get(key)
}

func (d *initData) userID() (int64, error) {
	userJSON := d.values.Get("user")
	if userJSON == "" {
		return 0, fmt.Errorf("user not found in init data")
	}

	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(userJSON), &user); err != nil {
		return 0, fmt.Errorf("failed to parse user data: %w", err)
	}

	return user.ID, nil
}

func (d *initData) validate(botToken string) error {
	hash := d.values.Get("hash")
	if hash == "" {
		return fmt.Errorf("missing hash in init data")
	}

	expectedHash := initDataHash(d.values, botToken)
	if !hmac.Equal([]byte(hash), []byte(expectedHash)) {
		return fmt.Errorf("invalid init data: hash mismatch")
	}

	authDateStr := d.values.Get("auth_date")
	if authDateStr == "" {
		return fmt.Errorf("missing auth_date in init data")
	}
	authDate, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid auth_date: %w", err)
	}
	if time.Since(time.Unix(authDate, 0)) > initDataMaxAge {
		return fmt.Errorf("init data expired")
	}

	return nil
}

// initDataMaxAge is how long init data is accepted after its auth_date.
const initDataMaxAge = 24 * time.Hour

// initDataHash is the hash Telegram puts into init data: HMAC-SHA256 of the
// sorted key=value lines (all but hash), keyed by HMAC-SHA256("WebAppData",
// botToken). validate checks it and signInitData makes it, from this one
// function, so the two cannot drift apart.
func initDataHash(values url.Values, botToken string) string {
	var keys []string
	for key := range values {
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, values.Get(key)))
	}
	dataCheckString := strings.Join(pairs, "\n")

	secretKey := computeHMACSHA256Bytes([]byte(botToken), []byte("WebAppData"))
	return computeHMACSHA256Hex([]byte(dataCheckString), secretKey)
}

// signInitData makes init data for userID, signed with the bot token as
// Telegram signs it, for opening the webapp in a normal browser (debug-link).
// The server accepts it like init data from Telegram, for initDataMaxAge
// from authDate.
func signInitData(botToken string, userID int64, authDate time.Time) string {
	user, _ := json.Marshal(map[string]any{"id": userID, "first_name": "browser debug"})
	values := url.Values{}
	values.Set("user", string(user))
	values.Set("auth_date", strconv.FormatInt(authDate.Unix(), 10))
	values.Set("hash", initDataHash(values, botToken))
	return values.Encode()
}

func computeHMACSHA256Bytes(data, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func computeHMACSHA256Hex(data, key []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}
