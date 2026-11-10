package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bogem/id3v2/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// audioPayload is recognisable "audio" placed after the tag, so tests can
// check it survives a save byte for byte.
var audioPayload = bytes.Repeat([]byte("AUDIO-0123456789"), 64)

// seedMP3 writes an ID3v2.4 tag built by build, followed by audioPayload.
func seedMP3(t *testing.T, path string, mode os.FileMode, build func(tag *id3v2.Tag)) {
	t.Helper()
	tag := id3v2.NewEmptyTag()
	tag.SetVersion(4)
	tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	build(tag)
	var buf bytes.Buffer
	if _, err := tag.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	buf.Write(audioPayload)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func openTag(t *testing.T, path string) *id3v2.Tag {
	t.Helper()
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tag.Close() })
	return tag
}

// assertAudioIntact checks that path ends with exactly audioPayload right
// after its tag.
func assertAudioIntact(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	start, err := id3TagSize(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data[start:], audioPayload) {
		t.Fatalf("audio after the tag changed: %d bytes, want %d", len(data)-int(start), len(audioPayload))
	}
}

func writeOne(t *testing.T, srv *server, in writeTagsInput) writeTagsOutput {
	t.Helper()
	res, out, err := srv.writeTags(context.Background(), nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("writeTags error result: %+v", res.Content)
	}
	for _, f := range out.Files {
		if f.Error != "" {
			t.Fatalf("file %s: %s", f.Path, f.Error)
		}
	}
	return out
}

// F5: a stale "<file>-id3v2" left by an interrupted id3v2 Save must not end
// up appended to the mp3.
func TestWriteTagsIgnoresStaleID3v2Temp(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "a.mp3")
	seedMP3(t, mp3, 0o644, func(tag *id3v2.Tag) { tag.SetTitle("old") })
	stale := mp3 + "-id3v2"
	if err := os.WriteFile(stale, bytes.Repeat([]byte{0xEE}, 64<<10), 0o644); err != nil {
		t.Fatal(err)
	}

	writeOne(t, &server{config: Config{DownloadPath: root}}, writeTagsInput{Path: "a.mp3", Tags: map[string]string{"title": "new"}})

	if got := openTag(t, mp3).Title(); got != "new" {
		t.Fatalf("title = %q, want new", got)
	}
	assertAudioIntact(t, mp3)
}

// F5: a symlink at "<file>-id3v2" must not redirect the write outside the root.
func TestWriteTagsDoesNotFollowSymlinkedTemp(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("PRECIOUS"), 0o644); err != nil {
		t.Fatal(err)
	}
	mp3 := filepath.Join(root, "b.mp3")
	seedMP3(t, mp3, 0o644, func(tag *id3v2.Tag) { tag.SetTitle("old") })
	if err := os.Symlink(outside, mp3+"-id3v2"); err != nil {
		t.Fatal(err)
	}

	writeOne(t, &server{config: Config{DownloadPath: root}}, writeTagsInput{Path: "b.mp3", Tags: map[string]string{"title": "new"}})

	if data, _ := os.ReadFile(outside); string(data) != "PRECIOUS" {
		t.Fatalf("file outside the root was overwritten: %q", data)
	}
	if fi, err := os.Lstat(mp3); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("b.mp3 is no longer a regular file: %v %v", fi, err)
	}
	assertAudioIntact(t, mp3)
}

// F5: saving keeps the file mode and leaves no temp files behind.
func TestWriteTagsKeepsModeAndLeavesNoTemp(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "c.mp3")
	seedMP3(t, mp3, 0o640, func(tag *id3v2.Tag) { tag.SetTitle("old") })

	writeOne(t, &server{config: Config{DownloadPath: root}}, writeTagsInput{Path: "c.mp3", Tags: map[string]string{"title": "a much longer title than before, to grow the tag"}})

	fi, err := os.Stat(mp3)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only c.mp3", names)
	}
	assertAudioIntact(t, mp3)
}

func TestID3TagSizeWithoutTag(t *testing.T) {
	p := filepath.Join(t.TempDir(), "raw.mp3")
	if err := os.WriteFile(p, []byte("not a tag, just audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(p)
	defer f.Close()
	if n, err := id3TagSize(f); err != nil || n != 0 {
		t.Fatalf("id3TagSize = %d, %v; want 0, nil", n, err)
	}
}

// F7: a symlinked .mp3 pointing outside the root is neither read nor written.
func TestTagToolsSkipSymlinkedMP3(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	secret := filepath.Join(outsideDir, "secret.mp3")
	seedMP3(t, secret, 0o644, func(tag *id3v2.Tag) { tag.SetTitle("SECRET-OUTSIDE") })
	secretBefore, _ := os.ReadFile(secret)

	alb := filepath.Join(root, "music", "alb")
	seedMP3(t, filepath.Join(alb, "ok.mp3"), 0o644, func(tag *id3v2.Tag) { tag.SetTitle("inside") })
	evil := filepath.Join(alb, "evil.mp3")
	if err := os.Symlink(secret, evil); err != nil {
		t.Fatal(err)
	}
	srv := &server{config: Config{DownloadPath: root}}

	for _, in := range []readTagsInput{{Path: "music", Recursive: true}, {Path: "music/alb"}} {
		_, out, err := srv.readTags(context.Background(), nil, in)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range out.Files {
			if f.Title == "SECRET-OUTSIDE" || strings.Contains(f.Path, "evil") {
				t.Fatalf("read_tags %+v returned the symlinked file outside the root: %+v", in, f)
			}
		}
		if len(out.Files) != 1 {
			t.Fatalf("read_tags %+v returned %d files, want 1", in, len(out.Files))
		}
	}

	writeOne(t, srv, writeTagsInput{Path: "music", Recursive: true, Tags: map[string]string{"album": "X"}})
	if fi, err := os.Lstat(evil); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("evil.mp3 is no longer the symlink: %v %v", fi, err)
	}
	if after, _ := os.ReadFile(secret); !bytes.Equal(after, secretBefore) {
		t.Fatal("file outside the root changed")
	}
}

// F8: fix_encoding repairs mojibake frames and leaves correct ones alone,
// even within the same tag.
func TestFixEncodingKeepsCorrectFrames(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "mixed.mp3")
	seedMP3(t, mp3, 0o644, func(tag *id3v2.Tag) {
		tag.SetTitle("Déjà Vu")
		tag.SetArtist("Beyoncé")
		tag.SetAlbum("Café del Mar")
		tag.AddTextFrame(tag.CommonID("Composer"), id3v2.EncodingISO, "Ïðèâåò Ìèð")
		tag.AddTextFrame(tag.CommonID("Band/Orchestra/Accompaniment"), id3v2.EncodingISO, "Pink Floyd - Ñòåíà")
		tag.AddCommentFrame(id3v2.CommentFrame{Encoding: id3v2.EncodingUTF8, Language: "eng", Text: "Señor"})
	})

	writeOne(t, &server{config: Config{DownloadPath: root}}, writeTagsInput{Path: "mixed.mp3", FixEncoding: "cp1251"})

	tag := openTag(t, mp3)
	for name, got := range map[string]string{
		"title":  tag.Title(),
		"artist": tag.Artist(),
		"album":  tag.Album(),
	} {
		want := map[string]string{"title": "Déjà Vu", "artist": "Beyoncé", "album": "Café del Mar"}[name]
		if got != want {
			t.Errorf("%s = %q, want %q (correct text was rewritten)", name, got, want)
		}
	}
	if got := tag.GetTextFrame(tag.CommonID("Composer")).Text; got != "Привет Мир" {
		t.Errorf("composer = %q, want %q (mojibake not repaired)", got, "Привет Мир")
	}
	if got := tag.GetTextFrame(tag.CommonID("Band/Orchestra/Accompaniment")).Text; got != "Pink Floyd - Стена" {
		t.Errorf("album artist = %q, want %q (mixed-script mojibake not repaired)", got, "Pink Floyd - Стена")
	}
	if frames := tag.GetFrames(tag.CommonID("Comments")); len(frames) != 1 || frames[0].(id3v2.CommentFrame).Text != "Señor" {
		t.Errorf("comment changed: %+v", frames)
	}
	assertAudioIntact(t, mp3)
}

func TestLooksLikeCP1251Mojibake(t *testing.T) {
	for s, want := range map[string]bool{
		"Ïðèâåò":             true,
		"Ïðèâåò World":       true,
		"Ãðóïïà Êèíî":        true,
		"¨ëêà":               true, // Ёлка
		"Café del Mar":       false,
		"Beyoncé":            false,
		"Déjà Vu":            false,
		"Été à Évian":        false,
		"Ação":               false,
		"Hello":              false,
		"Привет":             false,
		"":                   false,
		"ÏðèâåòĀ":            false,
		"Motörhead - Ace of": false,
		// F118: Latin words beside the mojibake do not hide it.
		"Track 01 - Ãëàâà 1":          true, // Track 01 - Глава 1
		"Pink Floyd - Ñòåíà":          true, // Pink Floyd - Стена
		"Ëåâ Òîëñòîé - War and Peace": true, // Лев Толстой - War and Peace
		"Áè-2 feat. Brainstorm":       true, // Би-2 feat. Brainstorm
		"Jóga Björk":                  false,
		"Ação 2 - Ãëàâà":              false, // a mixed word means the text is correct Latin-1
	} {
		if got := looksLikeCP1251Mojibake(s); got != want {
			t.Errorf("looksLikeCP1251Mojibake(%q) = %v, want %v", s, got, want)
		}
	}
}

// F31: fix_encoding also repairs TXXX (user-defined text) and USLT (lyrics)
// frames, leaving correct ones alone.
func TestFixEncodingRepairsTXXXAndUSLT(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "txxx.mp3")
	seedMP3(t, mp3, 0o644, func(tag *id3v2.Tag) {
		tag.SetTitle("Ïðèâåò")
		tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: id3v2.EncodingISO, Description: "ALBUMARTIST", Value: "Òîëñòîé"})
		tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: id3v2.EncodingUTF8, Description: "NARRATOR", Value: "Beyoncé"})
		tag.AddUnsynchronisedLyricsFrame(id3v2.UnsynchronisedLyricsFrame{Encoding: id3v2.EncodingISO, Language: "rus", ContentDescriptor: "", Lyrics: "Ïðèâåò ìèð"})
	})

	writeOne(t, &server{config: Config{DownloadPath: root}}, writeTagsInput{Path: "txxx.mp3", FixEncoding: "cp1251"})

	tag := openTag(t, mp3)
	got := map[string]string{}
	for _, f := range tag.GetFrames(tag.CommonID("User defined text information frame")) {
		u := f.(id3v2.UserDefinedTextFrame)
		got[u.Description] = u.Value
	}
	if got["ALBUMARTIST"] != "Толстой" {
		t.Errorf("TXXX:ALBUMARTIST = %q, want Толстой", got["ALBUMARTIST"])
	}
	if got["NARRATOR"] != "Beyoncé" {
		t.Errorf("TXXX:NARRATOR = %q, want Beyoncé (correct text rewritten)", got["NARRATOR"])
	}
	lyr := tag.GetFrames(tag.CommonID("Unsynchronised lyrics/text transcription"))
	if len(lyr) != 1 || lyr[0].(id3v2.UnsynchronisedLyricsFrame).Lyrics != "Привет мир" {
		t.Errorf("USLT = %+v, want Привет мир", lyr)
	}
	if tag.Title() != "Привет" {
		t.Errorf("title = %q", tag.Title())
	}
	assertAudioIntact(t, mp3)
}

// F33: an ID3v2.3 tag keeps its version and gets UTF-16 frames, never UTF-8
// (encoding byte 3), which v2.3 does not define.
func TestWriteTagsV23UsesUTF16(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "v23.mp3")
	seedMP3(t, mp3, 0o644, func(tag *id3v2.Tag) {
		tag.SetVersion(3)
		tag.AddTextFrame(tag.CommonID("Title"), id3v2.EncodingISO, "Âîéíà è ìèð")
		tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: id3v2.EncodingISO, Description: "ALBUMARTIST", Value: "Òîëñòîé"})
	})

	writeOne(t, &server{config: Config{DownloadPath: root}}, writeTagsInput{
		Path: "v23.mp3", FixEncoding: "cp1251", Tags: map[string]string{"album": "Война и мир", "comment": "Аудиокнига"},
	})

	tag := openTag(t, mp3)
	if tag.Version() != 3 {
		t.Fatalf("version = %d, want 3", tag.Version())
	}
	for _, id := range []string{"TIT2", "TALB", "TXXX", "COMM"} {
		frames := tag.GetFrames(id)
		if len(frames) == 0 {
			t.Errorf("%s missing", id)
		}
		for _, f := range frames {
			var enc id3v2.Encoding
			switch fr := f.(type) {
			case id3v2.TextFrame:
				enc = fr.Encoding
			case id3v2.UserDefinedTextFrame:
				enc = fr.Encoding
			case id3v2.CommentFrame:
				enc = fr.Encoding
			}
			if enc.Equals(id3v2.EncodingUTF8) {
				t.Errorf("%s written as UTF-8 into a v2.3 tag", id)
			}
		}
	}
	if tag.Title() != "Война и мир" || tag.Album() != "Война и мир" {
		t.Errorf("title=%q album=%q", tag.Title(), tag.Album())
	}
	assertAudioIntact(t, mp3)
}

// A v2.4 tag still gets UTF-8.
func TestWriteTagsV24UsesUTF8(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "v24.mp3")
	seedMP3(t, mp3, 0o644, func(tag *id3v2.Tag) { tag.SetTitle("x") })
	writeOne(t, &server{config: Config{DownloadPath: root}}, writeTagsInput{Path: "v24.mp3", Tags: map[string]string{"title": "Привет"}})
	tag := openTag(t, mp3)
	if f := tag.GetTextFrame("TIT2"); !f.Encoding.Equals(id3v2.EncodingUTF8) || f.Text != "Привет" {
		t.Fatalf("TIT2 = %+v, want UTF-8 Привет", f)
	}
}

// F73: a comment override replaces only the user comment and keeps
// described COMM frames such as iTunSMPB; read_tags shows the user comment.
func TestCommentOverrideKeepsDescribedComments(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "itunes.mp3")
	seedMP3(t, mp3, 0o644, func(tag *id3v2.Tag) {
		tag.AddCommentFrame(id3v2.CommentFrame{Encoding: id3v2.EncodingUTF8, Language: "eng", Description: "", Text: "old comment"})
		tag.AddCommentFrame(id3v2.CommentFrame{Encoding: id3v2.EncodingUTF8, Language: "eng", Description: "iTunSMPB", Text: " 00000000 00000210 0000099C"})
	})
	srv := &server{config: Config{DownloadPath: root}}

	_, out, _ := srv.readTags(context.Background(), nil, readTagsInput{Path: "itunes.mp3"})
	if len(out.Files) != 1 || out.Files[0].Comment != "old comment" {
		t.Fatalf("read_tags comment = %+v, want the user comment, not iTunSMPB", out.Files)
	}

	writeOne(t, srv, writeTagsInput{Path: "itunes.mp3", Tags: map[string]string{"comment": "new comment"}})

	tag := openTag(t, mp3)
	got := map[string]string{}
	for _, f := range tag.GetFrames(tag.CommonID("Comments")) {
		cf := f.(id3v2.CommentFrame)
		got[cf.Description] = cf.Text
	}
	if len(got) != 2 || got[""] != "new comment" || got["iTunSMPB"] != " 00000000 00000210 0000099C" {
		t.Fatalf("COMM frames after override = %q, want the new comment and iTunSMPB kept", got)
	}
	assertAudioIntact(t, mp3)
}

// F75: failed saves are counted and summarised first, and a call in which
// every file failed is a tool error.
func TestWriteTagsReportsFailures(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	seedMP3(t, filepath.Join(root, "alb", "ok", "1.mp3"), 0o644, func(tag *id3v2.Tag) { tag.SetTitle("a") })
	ro := filepath.Join(root, "alb", "ro")
	seedMP3(t, filepath.Join(ro, "2.mp3"), 0o644, func(tag *id3v2.Tag) { tag.SetTitle("b") })
	if err := os.Chmod(ro, 0o555); err != nil { // saveTag cannot create its temp file here
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(ro, 0o755) })
	srv := &server{config: Config{DownloadPath: root}}

	res, out, _ := srv.writeTags(context.Background(), nil, writeTagsInput{Path: "alb", Recursive: true, Tags: map[string]string{"album": "X"}})
	if res.IsError || out.Failed != 1 || out.Changed != 1 || !strings.HasPrefix(out.Summary, "1 of 2 files failed") {
		t.Fatalf("partial failure: IsError=%v failed=%d changed=%d summary=%q", res.IsError, out.Failed, out.Changed, out.Summary)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.HasPrefix(text, "1 of 2 files failed") {
		t.Errorf("result text starts %q, want the summary first", text[:min(40, len(text))])
	}

	res, out, _ = srv.writeTags(context.Background(), nil, writeTagsInput{Path: "alb/ro", Tags: map[string]string{"album": "Y"}})
	if !res.IsError || out.Failed != 1 {
		t.Fatalf("all failed: IsError=%v failed=%d, want a tool error", res.IsError, out.Failed)
	}
}

// F110: encoding_hint flags mojibake in any frame fix_encoding repairs, not
// only in the seven fields read_tags shows.
func TestEncodingHintCoversAllFixableFrames(t *testing.T) {
	root := t.TempDir()
	cases := map[string]func(tag *id3v2.Tag){
		"txxx.mp3": func(tag *id3v2.Tag) {
			tag.SetTitle("Correct Title")
			tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: id3v2.EncodingISO, Description: "NARRATOR", Value: "Ïðèâåò Ìèð"})
		},
		"uslt.mp3": func(tag *id3v2.Tag) {
			tag.AddUnsynchronisedLyricsFrame(id3v2.UnsynchronisedLyricsFrame{Encoding: id3v2.EncodingISO, Language: "rus", Lyrics: "Ïðèâåò Ìèð"})
		},
		"tpub.mp3": func(tag *id3v2.Tag) {
			tag.AddTextFrame("TPUB", id3v2.EncodingISO, "Ïðèâåò Ìèð")
		},
	}
	for name, build := range cases {
		seedMP3(t, filepath.Join(root, name), 0o644, build)
	}
	seedMP3(t, filepath.Join(root, "clean.mp3"), 0o644, func(tag *id3v2.Tag) {
		tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: id3v2.EncodingUTF8, Description: "NARRATOR", Value: "Beyoncé"})
	})

	_, out, err := (&server{config: Config{DownloadPath: root}}).readTags(context.Background(), nil, readTagsInput{Path: "."})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range out.Files {
		want := ""
		if _, ok := cases[filepath.Base(f.Path)]; ok {
			want = "cp1251"
		}
		if f.EncodingHint != want {
			t.Errorf("%s: encoding_hint = %q, want %q", f.Path, f.EncodingHint, want)
		}
	}
}
