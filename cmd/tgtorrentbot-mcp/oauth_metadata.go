package main

import (
	"encoding/json"
	"net/http"
)

// authServerMetadata is the RFC 8414 subset we publish so clients can
// dynamically wire themselves up against us.
type authServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

// protectedResourceMetadata is RFC 9728: tells MCP clients which AS to use.
// For us the AS is the same origin, so this basically loops back.
type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

func (s *server) handleAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.config.PublicBaseURL
	m := authServerMetadata{
		Issuer:                            base,
		AuthorizationEndpoint:             base + "/authorize",
		TokenEndpoint:                     base + "/token",
		RegistrationEndpoint:              base + "/register",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		ScopesSupported:                   []string{"mcp"},
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *server) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.config.PublicBaseURL
	m := protectedResourceMetadata{
		Resource:               base,
		AuthorizationServers:   []string{base},
		BearerMethodsSupported: []string{"header"},
	}
	writeJSON(w, http.StatusOK, m)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
