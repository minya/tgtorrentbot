package main

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func countSessions(s *mcp.Server) int {
	n := 0
	for range s.Sessions() {
		n++
	}
	return n
}

// F121: an MCP session that receives no requests is closed after the idle
// timeout instead of living until the process restarts.
func TestIdleMCPSessionsAreClosed(t *testing.T) {
	old := mcpSessionIdleTimeout
	mcpSessionIdleTimeout = 300 * time.Millisecond
	t.Cleanup(func() { mcpSessionIdleTimeout = old })

	server := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	ts := httptest.NewServer(newMCPHandler(server))
	defer ts.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: ts.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if n := countSessions(server); n != 1 {
		t.Fatalf("%d sessions after connecting, want 1", n)
	}

	deadline := time.Now().Add(5 * time.Second)
	for countSessions(server) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := countSessions(server); n != 0 {
		t.Fatalf("%d sessions still open after the idle timeout", n)
	}
}
