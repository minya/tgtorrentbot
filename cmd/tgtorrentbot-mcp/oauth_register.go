package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// registrationRequest is the RFC 7591 subset we care about.
type registrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

// registrationResponse — RFC 7591. The client_id is itself a JWT that encodes
// the registered metadata, so the server needs no DB: /authorize re-decodes it
// to recover redirect_uris.
type registrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// /register is unauthenticated and its metadata is signed into the
	// client_id, so bound both the body and the fields.
	r.Body = http.MaxBytesReader(w, r.Body, maxRegistrationBody)
	var req registrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "request body is not valid JSON or is too large")
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	if len(req.RedirectURIs) > maxRedirectURIs {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", fmt.Sprintf("at most %d redirect_uris are allowed", maxRedirectURIs))
		return
	}
	if len(req.ClientName) > maxClientNameLen {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", fmt.Sprintf("client_name is longer than %d bytes", maxClientNameLen))
		return
	}
	for _, u := range req.RedirectURIs {
		if len(u) > maxRedirectURILen {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", fmt.Sprintf("redirect_uri is longer than %d bytes", maxRedirectURILen))
			return
		}
		if !s.redirectURIAllowed(u) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri",
				"redirect_uris must be https (http allowed only for localhost), on a host allowed by TGT_MCP_REDIRECT_HOSTS when set")
			return
		}
	}

	now := time.Now().Unix()
	claims := jwtClaims{
		Iss:          s.config.PublicBaseURL,
		Aud:          s.config.PublicBaseURL,
		Iat:          now,
		Typ:          "client",
		ClientName:   req.ClientName,
		RedirectURIs: req.RedirectURIs,
	}
	clientID, err := signJWT(s.config.JWTSecret, claims)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to mint client_id")
		return
	}

	resp := registrationResponse{
		ClientID:                clientID,
		ClientIDIssuedAt:        now,
		RedirectURIs:            req.RedirectURIs,
		ClientName:              req.ClientName,
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
	}
	writeJSON(w, http.StatusCreated, resp)
}

// Limits on /register input.
const (
	maxRegistrationBody = 16 << 10
	maxRedirectURIs     = 10
	maxRedirectURILen   = 2048
	maxClientNameLen    = 200
)

// redirectURIAllowed applies isAllowedRedirectURI and, when
// TGT_MCP_REDIRECT_HOSTS is set, requires the host to be one of those (or
// loopback, for native clients).
func (s *server) redirectURIAllowed(raw string) bool {
	if !isAllowedRedirectURI(raw) {
		return false
	}
	if len(s.config.RedirectHosts) == 0 {
		return true
	}
	u, _ := url.Parse(raw)
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return slices.Contains(s.config.RedirectHosts, host)
}

// isAllowedRedirectURI enforces OAuth 2.1: https everywhere, with the usual
// loopback-for-native-apps carve-out. The loopback test parses the URL and
// checks the hostname exactly — prefix matching would let attacker-controlled
// hosts like http://localhost.evil.example/cb sneak through.
func isAllowedRedirectURI(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return true
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return true
		}
	}
	return false
}

// decodeClientID recovers the registered client metadata from a client_id JWT.
func (s *server) decodeClientID(clientID string) (jwtClaims, error) {
	c, err := verifyJWT(s.config.JWTSecret, clientID)
	if err != nil {
		return c, err
	}
	if c.Typ != "client" {
		return c, errOAuth("invalid_client", "client_id is not a client credential")
	}
	return c, nil
}

type oauthError struct {
	Code string `json:"error"`
	Desc string `json:"error_description,omitempty"`
}

func (e oauthError) Error() string {
	if e.Desc != "" {
		return e.Code + ": " + e.Desc
	}
	return e.Code
}

func errOAuth(code, desc string) oauthError {
	return oauthError{Code: code, Desc: desc}
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, oauthError{Code: code, Desc: desc})
}
