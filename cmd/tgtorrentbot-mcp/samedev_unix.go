//go:build unix

package main

import (
	"os"
	"syscall"
)

// sameDevice reports whether a and b are on the same filesystem; rename(2)
// fails with EXDEV across filesystems. ok is false when it cannot tell.
func sameDevice(a, b string) (same, ok bool) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, false
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, false
	}
	sa, okA := fa.Sys().(*syscall.Stat_t)
	sb, okB := fb.Sys().(*syscall.Stat_t)
	if !okA || !okB {
		return false, false
	}
	return uint64(sa.Dev) == uint64(sb.Dev), true
}
