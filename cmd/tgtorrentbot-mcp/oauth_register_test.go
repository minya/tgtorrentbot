package main

import "testing"

func TestIsAllowedRedirectURI(t *testing.T) {
	cases := []struct {
		uri   string
		allow bool
	}{
		{"https://example.com/cb", true},
		{"https://example.com:8443/cb", true},
		{"http://localhost/cb", true},
		{"http://localhost:8080/cb", true},
		{"http://127.0.0.1/cb", true},
		{"http://127.0.0.1:4567/cb", true},
		{"http://[::1]/cb", true},
		{"http://[::1]:9000/cb", true},
		{"http://127.255.255.254/cb", true},

		{"http://localhost.evil.example/cb", false},
		{"http://127.0.0.1.evil.example/cb", false},
		{"http://evil/cb", false},
		{"http://example.com/cb", false},
		{"http://8.8.8.8/cb", false},
		{"ftp://localhost/cb", false},
		{"not a url", false},
		{"", false},
		{"http://", false},
		{"https://", false},
	}
	for _, c := range cases {
		got := isAllowedRedirectURI(c.uri)
		if got != c.allow {
			t.Errorf("isAllowedRedirectURI(%q) = %v, want %v", c.uri, got, c.allow)
		}
	}
}
