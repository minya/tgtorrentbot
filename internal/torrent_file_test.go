package internal

import (
	"errors"
	"testing"
)

func TestCheckTorrentFile(t *testing.T) {
	for in, ok := range map[string]bool{
		"d8:announce3:urle":                 true,
		"<!DOCTYPE html><html>login</html>": false,
		"":                                  false,
		"\n<html>":                          false,
	} {
		err := CheckTorrentFile([]byte(in))
		if (err == nil) != ok || (err != nil && !errors.Is(err, ErrNotTorrentFile)) {
			t.Errorf("CheckTorrentFile(%q) = %v, want ok=%v", in, err, ok)
		}
	}
}
