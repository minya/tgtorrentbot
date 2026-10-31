//go:build linux

package main

import "golang.org/x/sys/unix"

// renameNoReplace renames src to dst atomically, failing with an error
// wrapping unix.EEXIST when dst already exists (renameat2 RENAME_NOREPLACE).
// Filesystems without RENAME_NOREPLACE support make it fail with EINVAL
// rather than fall back to a rename that could clobber.
func renameNoReplace(src, dst string) error {
	return unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_NOREPLACE)
}
