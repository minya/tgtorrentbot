package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func synchsafeBytes(n int) []byte {
	return []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
}

// rawFrame builds an ID3v2.3/2.4 text frame (ISO-8859-1) with the given flags.
func rawFrame(version byte, id, text string, flags uint16) []byte {
	data := append([]byte{0}, text...)
	var size []byte
	if version == 4 {
		size = synchsafeBytes(len(data))
	} else {
		size = binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	}
	f := append([]byte(id), size...)
	f = binary.BigEndian.AppendUint16(f, flags)
	return append(f, data...)
}

// rawMP3 writes a tag with the given header flags and body, then audio.
func rawMP3(t *testing.T, path string, version, headerFlags byte, body []byte) []byte {
	t.Helper()
	file := append([]byte{'I', 'D', '3', version, 0, headerFlags}, synchsafeBytes(len(body))...)
	file = append(file, body...)
	file = append(file, audioPayload...)
	if err := os.WriteFile(path, file, 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

// F107: tags using an extended header, unsynchronisation or frame flags are
// refused by write_tags and read_tags, instead of being misread and saved
// back with frames lost or flag bytes turned into text.
func TestTagToolsRefuseUnsupportedID3Flags(t *testing.T) {
	frames3 := append(append(rawFrame(3, "TIT2", "Title", 0), rawFrame(3, "TPE1", "Artist", 0)...), rawFrame(3, "TALB", "Album", 0)...)
	extHeader := []byte{0, 0, 0, 6, 0, 0, 0, 0, 0, 0} // v2.3: size 6, no flags, no padding
	cases := map[string]struct {
		version, flags byte
		body           []byte
	}{
		"extended-header.mp3": {3, 0x40, append(extHeader, frames3...)},
		"unsync.mp3":          {3, 0x80, frames3},
		"frame-dli.mp3":       {4, 0, append(rawFrame(4, "TIT2", "Title", 0), rawFrame(4, "TPE1", "Artist", 0x0001)...)},
	}
	root := t.TempDir()
	srv := &server{config: Config{DownloadPath: root}}
	for name, c := range cases {
		path := filepath.Join(root, name)
		before := rawMP3(t, path, c.version, c.flags, c.body)

		_, out, err := srv.writeTags(context.Background(), nil, writeTagsInput{Path: name, Tags: map[string]string{"title": "New"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Files) != 1 || out.Files[0].Error == "" || !strings.Contains(out.Files[0].Error, "cannot read or write safely") {
			t.Errorf("%s: write_tags result %+v, want a per-file refusal", name, out.Files)
		}
		if after, _ := os.ReadFile(path); !bytes.Equal(after, before) {
			t.Errorf("%s: the file was changed", name)
		}

		_, rout, _ := srv.readTags(context.Background(), nil, readTagsInput{Path: name})
		if len(rout.Files) != 1 || rout.Files[0].Error == "" {
			t.Errorf("%s: read_tags returned %+v, want an error instead of empty or garbled values", name, rout.Files)
		}
	}
}

// Ordinary tags without flags still work.
func TestCheckID3LayoutAcceptsPlainTags(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plain.mp3")
	rawMP3(t, path, 3, 0, append(rawFrame(3, "TIT2", "Title", 0), make([]byte, 64)...)) // with padding
	if err := checkID3Layout(path); err != nil {
		t.Fatalf("plain v2.3 tag refused: %v", err)
	}
	none := filepath.Join(root, "notag.mp3")
	os.WriteFile(none, audioPayload, 0o644)
	if err := checkID3Layout(none); err != nil {
		t.Fatalf("file without a tag refused: %v", err)
	}
}
