package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"time"
)

const (
	accessTokenTTL  = 1 * time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
)

// tokenResponse — RFC 6749 §5.1 with the OAuth 2.1 additions we use.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

func (s *server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		s.handleTokenCode(w, r)
	case "refresh_token":
		s.handleTokenRefresh(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "unknown grant_type")
	}
}

func (s *server) handleTokenCode(w http.ResponseWriter, r *http.Request) {
	code := r.PostForm.Get("code")
	clientID := r.PostForm.Get("client_id")
	redirectURI := r.PostForm.Get("redirect_uri")
	codeVerifier := r.PostForm.Get("code_verifier")

	if code == "" || clientID == "" || redirectURI == "" || codeVerifier == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request",
			"code, client_id, redirect_uri, code_verifier are required")
		return
	}

	ac, ok := s.codeStore.take(code)
	if !ok {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code is invalid or expired")
		return
	}
	if ac.ClientID != clientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}
	if ac.RedirectURI != redirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}
	if !verifyPKCE(ac.CodeChallenge, codeVerifier) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
		return
	}

	s.issueTokens(w, clientID, ac.Scope, time.Now().Unix())
}

func (s *server) handleTokenRefresh(w http.ResponseWriter, r *http.Request) {
	refresh := r.PostForm.Get("refresh_token")
	clientID := r.PostForm.Get("client_id")

	if refresh == "" || clientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request",
			"refresh_token and client_id are required")
		return
	}

	claims, err := verifyJWT(s.config.JWTSecret, refresh)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh_token is invalid")
		return
	}
	if claims.Typ != "refresh" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "token is not a refresh token")
		return
	}
	if claims.Iss != s.config.PublicBaseURL || claims.Aud != s.config.PublicBaseURL {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh_token was issued for another server")
		return
	}
	if claims.ClientID != clientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}
	// Refreshing never extends a session past refreshTokenTTL after the
	// login it came from, so a leaked refresh token cannot renew itself
	// forever. Tokens minted before auth_time existed count from their iat.
	authTime := claims.AuthTime
	if authTime == 0 {
		authTime = claims.Iat
	}
	if time.Since(time.Unix(authTime, 0)) >= refreshTokenTTL {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "session expired; authorize again")
		return
	}

	s.issueTokens(w, clientID, "", authTime)
}

// issueTokens mints an access token and a refresh token for clientID.
// authTime is the unix time of the login the session started with; the
// refresh token expires refreshTokenTTL after it.
func (s *server) issueTokens(w http.ResponseWriter, clientID, scope string, authTime int64) {
	now := time.Now()
	refreshExp := time.Unix(authTime, 0).Add(refreshTokenTTL)
	jti, err := randomToken(16)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to mint token")
		return
	}
	access, err := signJWT(s.config.JWTSecret, jwtClaims{
		Iss:      s.config.PublicBaseURL,
		Aud:      s.config.PublicBaseURL,
		Sub:      clientID,
		Iat:      now.Unix(),
		Exp:      now.Add(accessTokenTTL).Unix(),
		Jti:      jti,
		Typ:      "access",
		ClientID: clientID,
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to mint access token")
		return
	}
	refreshJTI, err := randomToken(16)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to mint token")
		return
	}
	refresh, err := signJWT(s.config.JWTSecret, jwtClaims{
		Iss:      s.config.PublicBaseURL,
		Aud:      s.config.PublicBaseURL,
		Sub:      clientID,
		Iat:      now.Unix(),
		Exp:      refreshExp.Unix(),
		Jti:      refreshJTI,
		Typ:      "refresh",
		ClientID: clientID,
		AuthTime: authTime,
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to mint refresh token")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int64(accessTokenTTL.Seconds()),
		RefreshToken: refresh,
		Scope:        scope,
	})
}

// verifyPKCE checks S256(code_verifier) == code_challenge, constant-time.
func verifyPKCE(challenge, verifier string) bool {
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}
