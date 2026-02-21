package main

import (
	"io/fs"
	"os"
	"path/filepath"
)

// FsItem represents a media item found on the filesystem.
type FsItem struct {
	Name         string
	Size         int64
	IsIncomplete bool
}

// filesystemScanner scans download and incomplete directories for media items.
type filesystemScanner struct {
	downloadPath   string
	incompletePath string
}

// ScanCategory lists subdirectories in {downloadPath}/{category}/ and returns
// an FsItem for each one with the directory's total size.
func (s *filesystemScanner) ScanCategory(category string) ([]FsItem, error) {
	dir := filepath.Join(s.downloadPath, category)
	return scanDir(dir, false)
}

// ScanIncomplete lists subdirectories in the incomplete path and returns
// an FsItem for each one with IsIncomplete set to true.
func (s *filesystemScanner) ScanIncomplete() ([]FsItem, error) {
	return scanDir(s.incompletePath, true)
}

func scanDir(dir string, incomplete bool) ([]FsItem, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var items []FsItem
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(dir, entry.Name())
		size, err := dirSize(fullPath)
		if err != nil {
			size = 0
		}
		items = append(items, FsItem{
			Name:         entry.Name(),
			Size:         size,
			IsIncomplete: incomplete,
		})
	}
	return items, nil
}

// dirSize recursively computes the total size of all files in a directory.
func dirSize(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
