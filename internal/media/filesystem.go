package media

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

// FilesystemScanner scans download and incomplete directories for media items.
type FilesystemScanner struct {
	DownloadPath   string
	IncompletePath string
}

// ScanCategory lists subdirectories in {DownloadPath}/{category}/ and returns
// an FsItem for each one with the directory's total size.
func (s *FilesystemScanner) ScanCategory(category string) ([]FsItem, error) {
	dir := filepath.Join(s.DownloadPath, category)
	return scanDir(dir, false)
}

// ScanIncomplete lists subdirectories in the incomplete path and returns
// an FsItem for each one with IsIncomplete set to true.
func (s *FilesystemScanner) ScanIncomplete() ([]FsItem, error) {
	return scanDir(s.IncompletePath, true)
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
		var size int64
		if entry.IsDir() {
			fullPath := filepath.Join(dir, entry.Name())
			size, err = dirSize(fullPath)
			if err != nil {
				size = 0
			}
		} else {
			info, err := entry.Info()
			if err != nil {
				size = 0
			} else {
				size = info.Size()
			}
		}
		items = append(items, FsItem{
			Name:         entry.Name(),
			Size:         size,
			IsIncomplete: incomplete,
		})
	}
	return items, nil
}

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
