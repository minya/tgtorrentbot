package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// resolvePath interprets input as a path relative to DownloadPath (or an
// absolute path) and returns an absolute, symlink-resolved path that is
// guaranteed to be within DownloadPath. Textual traversal is rejected first
// (cheap), then EvalSymlinks runs on both root and target so a symlink inside
// the downloads tree cannot escape the confinement.
//
// The target must exist on disk — EvalSymlinks requires it. Every current
// caller (read_tags, write_tags) already operates on existing files, so this
// is not a functional restriction.
func resolvePath(downloadRoot, input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("path is required")
	}
	p := input
	if !filepath.IsAbs(p) {
		p = filepath.Join(downloadRoot, p)
	}
	p = filepath.Clean(p)
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", input, err)
	}
	if abs != downloadRoot && !strings.HasPrefix(abs, downloadRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside download path %q", input, downloadRoot)
	}

	evalRoot, err := filepath.EvalSymlinks(downloadRoot)
	if err != nil {
		return "", fmt.Errorf("resolving download root: %w", err)
	}
	evalAbs, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", input, err)
	}
	if evalAbs != evalRoot && !strings.HasPrefix(evalAbs, evalRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves outside download path %q", input, downloadRoot)
	}
	return evalAbs, nil
}
