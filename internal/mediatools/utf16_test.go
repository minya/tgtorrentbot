package mediatools

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/bogem/id3v2/v2"
)

// v23Frames returns the frame bodies of the ID3v2.3 tag at the start of b,
// read independently of github.com/bogem/id3v2, keyed by frame id.
func v23Frames(t *testing.T, b []byte) map[string][][]byte {
	t.Helper()
	if string(b[:3]) != "ID3" || b[3] != 3 {
		t.Fatalf("not an ID3v2.3 tag: % x", b[:10])
	}
	body := b[10 : 10+synchsafe(b[6:10])]
	out := map[string][][]byte{}
	for off := 0; off+10 <= len(body) && body[off] != 0; {
		size := int(binary.BigEndian.Uint32(body[off+4 : off+8]))
		id := string(body[off : off+4])
		out[id] = append(out[id], body[off+10:off+10+size])
		off += 10 + size
	}
	return out
}

func le16(s string) []byte { return appendUTF16(nil, s) }

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// F6: write_tags wrote UTF-16 strings with a stray zero byte (a NUL in text
// frames, a shifted value after a description), as github.com/bogem/id3v2
// does. Every UTF-16 frame of a saved v2.3 tag now has the exact layout,
// those written and those only kept.
func TestSavedUTF16FramesHaveNoStrayByte(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "a.mp3")
	tag := id3v2.NewEmptyTag()
	tag.SetVersion(3)
	u := id3v2.EncodingUTF16
	tag.AddTextFrame("TIT2", u, "Песня")
	tag.AddCommentFrame(id3v2.CommentFrame{Encoding: u, Language: "rus", Description: "note", Text: "Привет"})
	tag.AddAttachedPicture(id3v2.PictureFrame{Encoding: u, MimeType: "image/jpeg", PictureType: 3, Description: "обложка", Picture: []byte{1, 2, 3}})
	tag.AddTextFrame("TCON", id3v2.EncodingISO, "Rap")
	f, err := os.Create(mp3)
	if err != nil {
		t.Fatal(err)
	}
	tag.WriteTo(f)
	f.Write(make([]byte, 1024))
	f.Close()

	srv := &Tools{config: Config{DownloadPath: root}}
	res, out, _ := srv.writeTags(context.Background(), nil, writeTagsInput{
		Path: "a.mp3",
		Tags: map[string]string{"album_artist": "АК-47", "comment": "Комментарий"},
	})
	if res.IsError || out.Changed != 1 {
		t.Fatalf("writeTags: %+v %+v", res.Content, out)
	}

	b, _ := os.ReadFile(mp3)
	frames := v23Frames(t, b)
	term := []byte{0, 0}
	want := map[string][][]byte{
		"TPE2": {cat([]byte{1}, le16("АК-47"), term)},
		"TIT2": {cat([]byte{1}, le16("Песня"), term)},
		"APIC": {cat([]byte{1}, []byte("image/jpeg\x00\x03"), le16("обложка"), term, []byte{1, 2, 3})},
		"TCON": {[]byte("\x00Rap\x00")},
	}
	for id, bodies := range want {
		if len(frames[id]) != 1 || !bytes.Equal(frames[id][0], bodies[0]) {
			t.Errorf("%s = % x, want % x", id, frames[id], bodies[0])
		}
	}
	comms := map[string]bool{}
	for _, c := range frames["COMM"] {
		comms[string(c)] = true
	}
	for _, c := range [][]byte{
		cat([]byte("\x01rus"), le16("note"), term, le16("Привет")),
		cat([]byte{1}, []byte("eng"), le16(""), term, le16("Комментарий")),
	} {
		if !comms[string(c)] {
			t.Errorf("COMM % x missing from % x", c, frames["COMM"])
		}
	}

	// The tools read back what was written.
	back, err := openSupportedTag(mp3)
	if err != nil {
		t.Fatal(err)
	}
	defer back.Close()
	if got := back.GetTextFrame("TPE2").Text; got != "АК-47" {
		t.Errorf("TPE2 read back as %q", got)
	}
	if got := userComment(back); got != "Комментарий" {
		t.Errorf("comment read back as %q", got)
	}
}

// The new layout does not change what the library reads for a TXXX frame.
func TestUTF16UserTextRoundTrip(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "a.mp3")
	if err := seedEmptyMP3(t, mp3); err != nil {
		t.Fatal(err)
	}
	tag, _ := id3v2.Open(mp3, id3v2.Options{Parse: true})
	tag.SetVersion(3)
	tag.SetDefaultEncoding(id3v2.EncodingUTF16)
	tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: id3v2.EncodingUTF16, Description: "Серия", Value: "Книга 1"})
	if err := saveTag(tag, mp3); err != nil {
		t.Fatal(err)
	}
	tag.Close()
	b, _ := os.ReadFile(mp3)
	if got, want := v23Frames(t, b)["TXXX"], cat([]byte{1}, le16("Серия"), []byte{0, 0}, le16("Книга 1")); len(got) != 1 || !bytes.Equal(got[0], want) {
		t.Errorf("TXXX = % x, want % x", got, want)
	}
	back, err := openSupportedTag(mp3)
	if err != nil {
		t.Fatal(err)
	}
	defer back.Close()
	f := back.GetFrames("TXXX")
	if len(f) != 1 || f[0].(id3v2.UserDefinedTextFrame).Description != "Серия" || f[0].(id3v2.UserDefinedTextFrame).Value != "Книга 1" {
		t.Errorf("TXXX read back as %+v", f)
	}
}

// writeRawV23 writes an mp3 whose ID3v2.3 tag holds frames (id, body) as given.
func writeRawV23(t *testing.T, path string, frames ...[2][]byte) {
	t.Helper()
	var body []byte
	for _, f := range frames {
		hdr := make([]byte, 10)
		copy(hdr, f[0])
		binary.BigEndian.PutUint32(hdr[4:8], uint32(len(f[1])))
		body = append(append(body, hdr...), f[1]...)
	}
	n := len(body)
	tag := []byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	if err := os.WriteFile(path, cat(tag, body, make([]byte, 1024)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func frame(id string, parts ...[]byte) [2][]byte { return [2][]byte{[]byte(id), cat(parts...)} }

// F6: UTF-16LE frames other taggers write correctly (Picard, mutagen) are
// read and saved unchanged, though the library misreads a description that
// ends in a non-ASCII character.
func TestCorrectUTF16FramesSurviveAWrite(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "a.mp3")
	term := []byte{0, 0}
	txxx := frame("TXXX", []byte{1}, le16("Серия"), term, le16("Книга 1"))
	comm := frame("COMM", []byte("\x01rus"), le16("заметка"), term, le16("Текст"))
	apic := frame("APIC", []byte("\x01image/png\x00\x03"), le16("обложка"), term, []byte{0x89, 'P', 'N', 'G'})
	writeRawV23(t, mp3, frame("TIT2", []byte{1}, le16("Песня"), term), txxx, comm, apic)

	srv := &Tools{config: Config{DownloadPath: root}}
	if _, out, _ := srv.writeTags(context.Background(), nil, writeTagsInput{Path: "a.mp3", Tags: map[string]string{"album": "Альбом"}}); out.Changed != 1 {
		t.Fatalf("writeTags: %+v", out)
	}
	b, _ := os.ReadFile(mp3)
	got := v23Frames(t, b)
	for _, f := range [][2][]byte{txxx, comm, apic} {
		if len(got[string(f[0])]) != 1 || !bytes.Equal(got[string(f[0])][0], f[1]) {
			t.Errorf("%s = % x, want % x", f[0], got[string(f[0])], f[1])
		}
	}
}

// F6: frames the library wrote before this fix, with the stray byte, are
// read correctly and saved without it; the picture keeps its bytes.
func TestStrayByteFramesAreRepaired(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "a.mp3")
	tag := id3v2.NewEmptyTag()
	tag.SetVersion(3)
	u := id3v2.EncodingUTF16
	tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: u, Description: "MusicBrainz Artist Id", Value: "abc-123"})
	tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: u, Description: "Серия", Value: "Книга 1"})
	tag.AddAttachedPicture(id3v2.PictureFrame{Encoding: u, MimeType: "image/png", PictureType: 3, Description: "обложка", Picture: []byte{0x89, 'P', 'N', 'G'}})
	f, _ := os.Create(mp3)
	tag.WriteTo(f) // the library's own writer: stray bytes
	f.Write(make([]byte, 1024))
	f.Close()

	back, err := openSupportedTag(mp3)
	if err != nil {
		t.Fatal(err)
	}
	vals := map[string]string{}
	for _, f := range back.GetFrames("TXXX") {
		u := f.(id3v2.UserDefinedTextFrame)
		vals[u.Description] = u.Value
	}
	pic := back.GetFrames("APIC")[0].(id3v2.PictureFrame)
	back.Close()
	if vals["MusicBrainz Artist Id"] != "abc-123" || vals["Серия"] != "Книга 1" || len(vals) != 2 {
		t.Errorf("TXXX read as %q", vals)
	}
	if pic.Description != "обложка" || !bytes.Equal(pic.Picture, []byte{0x89, 'P', 'N', 'G'}) {
		t.Errorf("APIC read as %q % x", pic.Description, pic.Picture)
	}

	srv := &Tools{config: Config{DownloadPath: root}}
	if _, out, _ := srv.writeTags(context.Background(), nil, writeTagsInput{Path: "a.mp3", Tags: map[string]string{"title": "Песня"}}); out.Changed != 1 {
		t.Fatalf("writeTags: %+v", out)
	}
	b, _ := os.ReadFile(mp3)
	got := v23Frames(t, b)
	term := []byte{0, 0}
	if want := cat([]byte("\x01image/png\x00\x03"), le16("обложка"), term, []byte{0x89, 'P', 'N', 'G'}); !bytes.Equal(got["APIC"][0], want) {
		t.Errorf("APIC = % x, want % x", got["APIC"][0], want)
	}
	txxx := map[string]bool{}
	for _, f := range got["TXXX"] {
		txxx[string(f)] = true
	}
	for _, want := range [][]byte{
		cat([]byte{1}, le16("MusicBrainz Artist Id"), term, le16("abc-123")),
		cat([]byte{1}, le16("Серия"), term, le16("Книга 1")),
	} {
		if !txxx[string(want)] {
			t.Errorf("TXXX % x missing from % x", want, got["TXXX"])
		}
	}
}
