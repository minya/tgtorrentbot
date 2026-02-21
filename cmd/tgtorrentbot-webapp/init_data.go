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

	var keys []string
	for key := range d.values {
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var pairs []string
	for _, key := range keys {
		value := d.values.Get(key)
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, value))
	}

	dataCheckString := strings.Join(pairs, "\n")

	secretKey := computeHMACSHA256Bytes([]byte(botToken), []byte("WebAppData"))

	expectedHash := computeHMACSHA256Hex([]byte(dataCheckString), secretKey)

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
	if time.Since(time.Unix(authDate, 0)) > 24*time.Hour {
		return fmt.Errorf("init data expired")
	}

	return nil
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
