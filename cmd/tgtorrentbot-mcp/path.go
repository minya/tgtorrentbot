package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolvePath interprets input as a path relative to DownloadPath (or an
// absolute path) and returns an absolute, symlink-resolved path that is
// guaranteed to be within downloadRoot or one of extraRoots (the incomplete
// directory, which Transmission may keep outside the download path).
// Textual traversal is rejected first (cheap), then EvalSymlinks runs on
// the roots and the target so a symlink cannot escape the confinement.
//
// The target must exist on disk — EvalSymlinks requires it. Every current
// caller (read_tags, write_tags, a move source) operates on existing files.
func resolvePath(downloadRoot, input string, extraRoots ...string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("path is required")
	}
	roots := append([]string{downloadRoot}, extraRoots...)
	abs, err := absInput(downloadRoot, input)
	if err != nil {
		return "", err
	}
	if !withinAny(abs, roots) {
		return "", fmt.Errorf("path %q is outside %s", input, describeRoots(roots))
	}
	evalAbs, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", input, err)
	}
	evalRoots, err := evalRootsOf(roots)
	if err != nil {
		return "", err
	}
	if !withinAny(evalAbs, evalRoots) {
		return "", fmt.Errorf("path %q resolves outside %s", input, describeRoots(roots))
	}
	return evalAbs, nil
}

// resolveDestPath is the move-destination counterpart of resolvePath: it
// validates that input stays within the roots without requiring the target
// to exist (a move destination usually does not yet exist, so EvalSymlinks
// cannot run on it). It rejects textual traversal, then symlink-resolves the
// nearest existing ancestor and confirms that ancestor is still inside a
// root — so a symlinked parent cannot redirect the write outside.
func resolveDestPath(downloadRoot, input string, extraRoots ...string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("destination is required")
	}
	roots := append([]string{downloadRoot}, extraRoots...)
	abs, err := absInput(downloadRoot, input)
	if err != nil {
		return "", err
	}
	if !withinAny(abs, roots) {
		return "", fmt.Errorf("destination %q is outside %s", input, describeRoots(roots))
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
	evalRoots, err := evalRootsOf(roots)
	if err != nil {
		return "", err
	}
	if !withinAny(evalAncestor, evalRoots) {
		return "", fmt.Errorf("destination %q resolves outside %s", input, describeRoots(roots))
	}
	// Re-anchor the (possibly non-existent) tail onto the resolved ancestor.
	tail, err := filepath.Rel(ancestor, abs)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", input, err)
	}
	return filepath.Join(evalAncestor, tail), nil
}

func absInput(downloadRoot, input string) (string, error) {
	abs, err := filepath.Abs(lexicalPath(downloadRoot, input))
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", input, err)
	}
	return abs, nil
}

// withinAny reports whether p is one of roots or inside one.
func withinAny(p string, roots []string) bool {
	for _, r := range roots {
		if r == "" {
			continue
		}
		r = filepath.Clean(r)
		if p == r || strings.HasPrefix(p, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// evalRootsOf resolves the roots' symlinks. The download root must exist;
// an extra root that does not exist yet is kept as given.
func evalRootsOf(roots []string) ([]string, error) {
	out := make([]string, 0, len(roots))
	for i, r := range roots {
		if r == "" {
			continue
		}
		e, err := filepath.EvalSymlinks(r)
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("resolving download root: %w", err)
			}
			e = filepath.Clean(r)
		}
		out = append(out, e)
	}
	return out, nil
}

func describeRoots(roots []string) string {
	if len(roots) == 1 {
		return fmt.Sprintf("download path %q", roots[0])
	}
	return fmt.Sprintf("download path %q and incomplete directory %q", roots[0], roots[1])
}

// lexicalPath returns input as an absolute, cleaned path under downloadRoot
// without resolving symlinks (relative inputs are joined to the root). It
// does no validation; use resolvePath for that.
func lexicalPath(downloadRoot, input string) string {
	p := input
	if !filepath.IsAbs(p) {
		p = filepath.Join(downloadRoot, p)
	}
	return filepath.Clean(p)
}
