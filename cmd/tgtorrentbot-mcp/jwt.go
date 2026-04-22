package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// jwtClaims is the full set of claims we ever put in tokens we mint. Unused
// fields stay empty. Kept as one struct so sign/verify share a shape.
type jwtClaims struct {
	Iss          string   `json:"iss,omitempty"`
	Sub          string   `json:"sub,omitempty"`
	Aud          string   `json:"aud,omitempty"`
	Exp          int64    `json:"exp,omitempty"`
	Iat          int64    `json:"iat,omitempty"`
	Jti          string   `json:"jti,omitempty"`
	Typ          string   `json:"typ,omitempty"` // "access" | "refresh" | "client"
	ClientID     string   `json:"client_id,omitempty"`
	ClientName   string   `json:"client_name,omitempty"`
	RedirectURIs []string `json:"redirect_uris,omitempty"`
}

func b64urlEncode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func b64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// signJWT produces a compact HS256 JWT. Does not mutate claims.
func signJWT(secret []byte, claims jwtClaims) (string, error) {
	header := `{"alg":"HS256","typ":"JWT"}`
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64urlEncode([]byte(header)) + "." + b64urlEncode(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64urlEncode(mac.Sum(nil)), nil
}

// verifyJWT parses, checks the HS256 signature, and validates exp/iat.
// Caller must additionally check typ/iss/aud as appropriate.
func verifyJWT(secret []byte, token string) (jwtClaims, error) {
	var c jwtClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return c, fmt.Errorf("jwt: malformed")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	wantSig := mac.Sum(nil)
	gotSig, err := b64urlDecode(parts[2])
	if err != nil {
		return c, fmt.Errorf("jwt: bad signature encoding")
	}
	if !hmac.Equal(wantSig, gotSig) {
		return c, fmt.Errorf("jwt: signature mismatch")
	}
	payload, err := b64urlDecode(parts[1])
	if err != nil {
		return c, fmt.Errorf("jwt: bad payload encoding")
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return c, fmt.Errorf("jwt: bad payload json: %w", err)
	}
	now := time.Now().Unix()
	if c.Exp != 0 && now >= c.Exp {
		return c, fmt.Errorf("jwt: expired")
	}
	if c.Iat != 0 && now+30 < c.Iat { // 30s skew tolerance
		return c, fmt.Errorf("jwt: issued in the future")
	}
	return c, nil
}
