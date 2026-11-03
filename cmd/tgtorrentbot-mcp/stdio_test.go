package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// F53: a container's /dev/null stdin is recognised (the binary then exits
// with an explanation), while a real client's pipe is not.
func TestStdinIsDevNull(t *testing.T) {
	old := os.Stdin
	t.Cleanup(func() { os.Stdin = old })

	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	os.Stdin = null
	if !stdinIsDevNull() {
		t.Error("stdinIsDevNull() = false for /dev/null")
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	os.Stdin = r
	if stdinIsDevNull() {
		t.Error("stdinIsDevNull() = true for a pipe")
	}
}

// F119: a stdio session whose stdin ends before any byte arrived is
// reported as having no client, whatever kind of file stdin is (a pipe here,
// which stdinIsDevNull does not recognise).
func TestRunStdioWithoutClient(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.Close() // EOF at once, as an unattached container stdin gives
	defer r.Close()
	server := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	if err := runStdio(context.Background(), server, r, io.Discard); !errors.Is(err, errNoStdioClient) {
		t.Fatalf("runStdio = %v, want errNoStdioClient", err)
	}
}

// A real client session that ends is not reported as "no client".
func TestRunStdioWithClient(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}` + "\n")
	server := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	if err := runStdio(context.Background(), server, in, io.Discard); errors.Is(err, errNoStdioClient) {
		t.Fatalf("runStdio = %v after a client message", err)
	}
}
