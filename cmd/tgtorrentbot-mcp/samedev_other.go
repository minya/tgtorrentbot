//go:build !unix

package main

// sameDevice cannot tell on this platform; the rename reports EXDEV itself.
func sameDevice(a, b string) (same, ok bool) { return false, false }
