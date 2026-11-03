package main

import (
	"os"
	"testing"
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
