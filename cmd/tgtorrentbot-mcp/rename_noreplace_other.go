//go:build !linux && !darwin

package main

import (
	"errors"
	"fmt"
	"runtime"
)

// renameNoReplace is unsupported here: there is no portable atomic
// no-replace rename, and a check-then-rename could clobber a destination
// that appears in between, so move_media refuses instead.
func renameNoReplace(src, dst string) error {
	return fmt.Errorf("atomic no-replace rename is not supported on %s: %w", runtime.GOOS, errors.ErrUnsupported)
}
