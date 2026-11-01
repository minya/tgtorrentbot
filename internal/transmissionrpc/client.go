// Package transmissionrpc builds the Transmission RPC client shared by the
// bot, the webapp and the MCP server.
//
// github.com/odwrtw/transmission is not safe for concurrent use as is: its
// Client.Do stores the X-Transmission-Session-Id from every HTTP 409 in an
// unsynchronised field, and our binaries share one client between background
// pollers and request handlers. It also defaults to an http.Client without a
// timeout and builds requests without a context, so a stalled Transmission
// blocks callers forever.
//
// New fixes both from the outside: the client gets an http.Client with a
// timeout whose transport handles the session id itself (under a mutex) and
// retries 409s internally, so the library never sees a 409 and never writes
// its session field.
package transmissionrpc

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/odwrtw/transmission"
)

// DefaultTimeout bounds every Transmission RPC call, including the 409 retry.
const DefaultTimeout = 30 * time.Second

const sessionHeader = "X-Transmission-Session-Id"

// New returns a Transmission client for address that is safe for concurrent
// use and whose calls time out after DefaultTimeout.
func New(address, user, password string) (*transmission.Client, error) {
	return newWithTimeout(address, user, password, DefaultTimeout)
}

func newWithTimeout(address, user, password string, timeout time.Duration) (*transmission.Client, error) {
	return transmission.New(transmission.Config{
		Address:  address,
		User:     user,
		Password: password,
		HTTPClient: &http.Client{
			Timeout:   timeout,
			Transport: &sessionTransport{base: http.DefaultTransport},
		},
	})
}

// sessionTransport adds the current Transmission session id to each request
// and, when Transmission answers 409 with a new one, stores it and resends
// the request once.
type sessionTransport struct {
	base http.RoundTripper

	mu        sync.Mutex
	sessionID string
}

func (t *sessionTransport) currentID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}

func (t *sessionTransport) setID(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessionID = id
}

func (t *sessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		body = b
	}

	send := func(id string) (*http.Response, error) {
		r := req.Clone(req.Context())
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Del(sessionHeader)
		if id != "" {
			r.Header.Set(sessionHeader, id)
		}
		return t.base.RoundTrip(r)
	}

	id := t.currentID()
	for range maxSessionRetries {
		resp, err := send(id)
		if err != nil || resp.StatusCode != http.StatusConflict {
			return resp, err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if id = resp.Header.Get(sessionHeader); id == "" {
			return nil, errors.New("transmission answered 409 without a session id")
		}
		t.setID(id)
	}
	// Never hand a 409 back: the library would store its session id in an
	// unsynchronised field, which is the race this transport exists to avoid.
	return nil, errors.New("transmission session id kept changing; giving up")
}

// maxSessionRetries is how many 409s in a row RoundTrip absorbs (the first
// request of a session, plus session id rotations racing the retry).
const maxSessionRetries = 4
