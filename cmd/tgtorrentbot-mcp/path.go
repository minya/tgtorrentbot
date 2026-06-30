package main

import (
	"fmt"
	"os"
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

// resolveDestPath is the move-destination counterpart of resolvePath: it
// validates that input stays within DownloadPath without requiring the target
// to exist (a move destination usually does not yet exist, so EvalSymlinks
// cannot run on it). It rejects textual traversal, then symlink-resolves the
// nearest existing ancestor and confirms that ancestor is still inside the
// download root — so a symlinked parent cannot redirect the write outside.
func resolveDestPath(downloadRoot, input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("destination is required")
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
		return "", fmt.Errorf("destination %q is outside download path %q", input, downloadRoot)
	}

	evalRoot, err := filepath.EvalSymlinks(downloadRoot)
	if err != nil {
		return "", fmt.Errorf("resolving download root: %w", err)
	}
	// Walk up to the nearest ancestor that exists on disk and resolve it.
	ancestor := abs
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		ancestor = parent
	}
	evalAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", input, err)
	}
	if evalAncestor != evalRoot && !strings.HasPrefix(evalAncestor, evalRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("destination %q resolves outside download path %q", input, downloadRoot)
	}
	// Re-anchor the (possibly non-existent) tail onto the resolved ancestor.
	tail, err := filepath.Rel(ancestor, abs)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", input, err)
	}
	return filepath.Join(evalAncestor, tail), nil
}
