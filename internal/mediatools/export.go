package mediatools

import (
	"path/filepath"

	"github.com/odwrtw/transmission"
)

// Path helpers for callers that confine tool calls (the auto-fixer's scope
// guard). They apply the same rules as the tools.

// Resolve resolves an existing path given as the tools take it (relative to
// the download root, or absolute under it or the incomplete directory).
func (s *Tools) Resolve(p string) (string, error) {
	return resolvePath(s.config.DownloadPath, p, s.extraRoots()...)
}

// ResolveDest resolves a path that need not exist yet, as move_media does
// for its destination.
func (s *Tools) ResolveDest(p string) (string, error) {
	return resolveDestPath(s.config.DownloadPath, p, s.extraRoots()...)
}

// CategoryDir returns the resolved directory of a category.
func (s *Tools) CategoryDir(category string) string {
	return evalOrClean(filepath.Join(s.config.DownloadPath, category))
}

// Display returns p in the form the tools report paths (relative to the
// download root when inside it).
func (s *Tools) Display(p string) string {
	return displayPath(s.config.DownloadPath, p)
}

// DataPath returns the resolved path of a torrent's data on disk: its
// {downloadDir}/{name}, or {incomplete}/{name} while it downloads.
func (s *Tools) DataPath(t *transmission.Torrent) (string, bool) {
	for _, p := range s.torrentDataPaths(t) {
		if abs, err := s.Resolve(p); err == nil {
			return abs, true
		}
	}
	return "", false
}

// File is one regular file under an item.
type File struct {
	Path string
	Size int64
}

// Files lists up to max regular files under root (root itself when it is a
// file); truncated is true when there were more.
func (s *Tools) Files(root string, max int) (files []File, truncated bool) {
	listed, truncated := listFiles(root, &max)
	for _, f := range listed {
		files = append(files, File{Path: f.Path, Size: f.Size})
	}
	return files, truncated
}

// IsWithin reports whether p lies strictly inside dir.
func IsWithin(p, dir string) bool { return isWithin(p, dir) }
