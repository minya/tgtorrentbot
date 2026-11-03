// Package rutrackersession keeps one logged-in rutracker client per process.
//
// Every search and download used to build a new client, and so log in
// again: a search plus a download cost two logins, and rutracker asks for
// a captcha after repeated logins, which then breaks every login until it
// is cleared. One client is shared instead; it logs in on first use, and
// the rutracker library logs in again by itself when a response shows the
// session has expired. The client is safe for concurrent use.
package rutrackersession

import (
	"context"
	"sync"
	"time"

	"github.com/minya/rutracker"
)

// requestTimeout bounds each rutracker request.
const requestTimeout = 30 * time.Second

// Session hands out the shared client. The zero value is not usable; use New.
type Session struct {
	newClient func(ctx context.Context) (*rutracker.RutrackerClient, error)

	mu     sync.Mutex
	client *rutracker.RutrackerClient
}

// New returns a Session that logs in with username and password.
func New(username, password string) *Session {
	return &Session{newClient: func(ctx context.Context) (*rutracker.RutrackerClient, error) {
		c, err := rutracker.NewAuthenticatedRutrackerClientContext(ctx, username, password, rutracker.WithTimeout(requestTimeout))
		if err != nil {
			return nil, err
		}
		return &c, nil
	}}
}

// Client returns the logged-in client, logging in first if no login has
// succeeded yet. A failed login is not remembered, so the next call tries
// again; concurrent first calls wait for one login.
func (s *Session) Client(ctx context.Context) (*rutracker.RutrackerClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	c, err := s.newClient(ctx)
	if err != nil {
		return nil, err
	}
	s.client = c
	return c, nil
}
