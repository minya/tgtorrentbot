//go:build darwin

package main

import "golang.org/x/sys/unix"

// renameNoReplace renames src to dst atomically, failing with an error
// wrapping unix.EEXIST when dst already exists (renameatx_np RENAME_EXCL).
func renameNoReplace(src, dst string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_EXCL)
}
