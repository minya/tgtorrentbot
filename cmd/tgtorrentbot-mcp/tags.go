package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bogem/id3v2/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

type readTagsInput struct {
	Path      string `json:"path" jsonschema:"path to an .mp3 file or a directory, relative to the download path or absolute under it"`
	Recursive bool   `json:"recursive,omitempty" jsonschema:"when path is a directory, recurse into subdirectories (default false)"`
}

type fileTags struct {
	Path         string `json:"path"`
	Title        string `json:"title,omitempty"`
	Artist       string `json:"artist,omitempty"`
	Album        string `json:"album,omitempty"`
	AlbumArtist  string `json:"album_artist,omitempty"`
	Composer     string `json:"composer,omitempty"`
	Year         string `json:"year,omitempty"`
	Genre        string `json:"genre,omitempty"`
	Track        string `json:"track,omitempty"`
	Disc         string `json:"disc,omitempty"`
	Comment      string `json:"comment,omitempty"`
	EncodingHint string `json:"encoding_hint,omitempty"`
	Error        string `json:"error,omitempty"`
}

type readTagsOutput struct {
	Failed int        `json:"failed"` // files whose tags could not be read
	Files  []fileTags `json:"files"`
}

type writeTagsInput struct {
	Path        string            `json:"path" jsonschema:"path to an .mp3 file or a directory, relative to the download path or absolute under it"`
	Recursive   bool              `json:"recursive,omitempty" jsonschema:"when path is a directory, recurse into subdirectories (default false)"`
	FixEncoding string            `json:"fix_encoding,omitempty" jsonschema:"if set (only 'cp1251' is supported), re-decode text (T***, TXXX), comment and lyrics (USLT) frames from this codepage to Unicode before applying overrides; only values that look like cp1251 mojibake are rewritten"`
	Tags        map[string]string `json:"tags,omitempty" jsonschema:"optional explicit tag overrides; keys: title, artist, album, album_artist, composer, year, genre, track, disc, comment"`
}

type writeTagsResult struct {
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
	Error   string `json:"error,omitempty"`
}

type writeTagsOutput struct {
	// Summary leads the result ("3 of 120 files failed to save, 117
	// changed") so per-file errors cannot hide in a long files list.
	Summary string            `json:"summary"`
	Changed int               `json:"changed"`
	Failed  int               `json:"failed"`
	Files   []writeTagsResult `json:"files"`
}

var audioExtensions = map[string]bool{
	".mp3": true,
}

// Valid keys for explicit tag overrides.
var overrideKeys = map[string]bool{
	"title":        true,
	"artist":       true,
	"album":        true,
	"album_artist": true,
	"composer":     true,
	"year":         true,
	"genre":        true,
	"track":        true,
	"disc":         true,
	"comment":      true,
}

func (s *server) readTags(ctx context.Context, _ *mcp.CallToolRequest, in readTagsInput) (*mcp.CallToolResult, readTagsOutput, error) {
	resolved, err := resolvePath(s.config.DownloadPath, in.Path)
	if err != nil {
		return formatError("%v", err), readTagsOutput{}, nil
	}
	files, err := collectAudioFiles(resolved, in.Recursive)
	if err != nil {
		return formatError("%v", err), readTagsOutput{}, nil
	}
	if len(files) == 0 {
		return formatError("no audio files found at %q (only .mp3 is supported in this version)", in.Path), readTagsOutput{}, nil
	}

	out := readTagsOutput{Files: make([]fileTags, 0, len(files))}
	for _, f := range files {
		ft := fileTags{Path: displayPath(s.config.DownloadPath, f)}
		tag, err := id3v2.Open(f, id3v2.Options{Parse: true})
		if err != nil {
			ft.Error = err.Error()
			out.Failed++
			out.Files = append(out.Files, ft)
			continue
		}
		ft.Title = tag.Title()
		ft.Artist = tag.Artist()
		ft.Album = tag.Album()
		ft.AlbumArtist = tag.GetTextFrame(tag.CommonID("Band/Orchestra/Accompaniment")).Text
		ft.Composer = tag.GetTextFrame(tag.CommonID("Composer")).Text
		ft.Year = tag.Year()
		ft.Genre = tag.Genre()
		ft.Track = tag.GetTextFrame(tag.CommonID("Track number/Position in set")).Text
		ft.Disc = tag.GetTextFrame(tag.CommonID("Part of a set")).Text
		ft.Comment = userComment(tag)
		tag.Close()

		if hint := encodingHint(ft.Title, ft.Artist, ft.Album, ft.AlbumArtist, ft.Composer, ft.Genre, ft.Comment); hint != "" {
			ft.EncodingHint = hint
		}
		out.Files = append(out.Files, ft)
	}
	summary := fmt.Sprintf("read %d files", len(files))
	if out.Failed > 0 {
		summary = fmt.Sprintf("%d of %d files could not be read", out.Failed, len(files))
	}
	return &mcp.CallToolResult{
		IsError: out.Failed == len(files),
		Content: []mcp.Content{&mcp.TextContent{Text: summary + "\n" + mustJSON(out)}},
	}, out, nil
}

func (s *server) writeTags(ctx context.Context, _ *mcp.CallToolRequest, in writeTagsInput) (*mcp.CallToolResult, writeTagsOutput, error) {
	if in.FixEncoding == "" && len(in.Tags) == 0 {
		return formatError("nothing to do: provide fix_encoding or tags (or both)"), writeTagsOutput{}, nil
	}

	for k := range in.Tags {
		if !overrideKeys[strings.ToLower(k)] {
			return formatError("unknown tag key %q (valid: title, artist, album, album_artist, composer, year, genre, track, disc, comment)", k), writeTagsOutput{}, nil
		}
	}

	var decoder *encoding.Decoder
	if in.FixEncoding != "" {
		switch strings.ToLower(in.FixEncoding) {
		case "cp1251", "windows-1251", "windows1251":
			decoder = charmap.Windows1251.NewDecoder()
		default:
			return formatError("unsupported fix_encoding %q (only cp1251 is supported)", in.FixEncoding), writeTagsOutput{}, nil
		}
	}

	resolved, err := resolvePath(s.config.DownloadPath, in.Path)
	if err != nil {
		return formatError("%v", err), writeTagsOutput{}, nil
	}
	files, err := collectAudioFiles(resolved, in.Recursive)
	if err != nil {
		return formatError("%v", err), writeTagsOutput{}, nil
	}
	if len(files) == 0 {
		return formatError("no audio files found at %q (only .mp3 is supported in this version)", in.Path), writeTagsOutput{}, nil
	}

	out := writeTagsOutput{Files: make([]writeTagsResult, 0, len(files))}
	for _, f := range files {
		res := writeTagsResult{Path: displayPath(s.config.DownloadPath, f)}
		changed, err := applyTags(f, decoder, in.Tags)
		if err != nil {
			res.Error = err.Error()
			out.Failed++
		} else {
			res.Changed = changed
			if changed {
				out.Changed++
			}
		}
		out.Files = append(out.Files, res)
	}
	out.Summary = fmt.Sprintf("%d of %d files failed to save, %d changed", out.Failed, len(files), out.Changed)
	if out.Failed == 0 {
		out.Summary = fmt.Sprintf("all %d files saved, %d changed", len(files), out.Changed)
	}
	return &mcp.CallToolResult{
		// Every file failing is a failed call, not a result to summarise.
		IsError: out.Failed == len(files),
		Content: []mcp.Content{&mcp.TextContent{Text: out.Summary + "\n" + mustJSON(out)}},
	}, out, nil
}

// mustJSON marshals v with indent. On failure, returns an error string —
// caller is expected to only pass marshalable structs, so this is defensive.
func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("(failed to marshal: %v)", err)
	}
	return string(b)
}

// applyTags opens path, applies fix encoding then explicit overrides, and
// saves if anything changed.
func applyTags(path string, decoder *encoding.Decoder, overrides map[string]string) (bool, error) {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return false, err
	}
	defer tag.Close()

	// id3v2/v2 locks frame encoding at Set* time, so this must come before any Set*.
	tag.SetDefaultEncoding(unicodeEncoding(tag))

	changed := false

	if decoder != nil {
		if fixAllEncoding(tag, decoder) {
			changed = true
		}
	}

	for k, v := range overrides {
		if setOverride(tag, strings.ToLower(k), v) {
			changed = true
		}
	}

	if !changed {
		return false, nil
	}
	return true, saveTag(tag, path)
}

// saveTag writes tag followed by the audio of the file at path to a fresh
// temp file in the same directory and renames it over path.
//
// It replaces id3v2's Tag.Save, which writes through the fixed name
// "<file>-id3v2" opened without O_TRUNC/O_EXCL and following symlinks: a
// stale temp of that name left trailing garbage in the mp3, and a symlink of
// that name made the save write outside TGT_DOWNLOADPATH. Here the temp name
// is random and created exclusively (os.CreateTemp), the data is fsynced
// before the rename, and the original file mode is kept.
func saveTag(tag *id3v2.Tag, path string) (err error) {
	orig, err := os.Open(path)
	if err != nil {
		return err
	}
	defer orig.Close()
	st, err := orig.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	audioStart, err := id3TagSize(orig)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()

	if _, err = tag.WriteTo(tmp); err != nil {
		return err
	}
	if _, err = orig.Seek(audioStart, io.SeekStart); err != nil {
		return err
	}
	if _, err = io.Copy(tmp, orig); err != nil {
		return err
	}
	if err = tmp.Chmod(st.Mode().Perm()); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	// Persist the rename itself; a failure here does not undo the save.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// id3TagSize returns the size of the ID3v2 tag at the start of f (header
// plus frames), i.e. the offset where the audio starts, computed the same way
// id3v2 does when parsing: 10 + the synchsafe size of an "ID3" header, or 0
// when the file has no ID3v2 tag.
func id3TagSize(f *os.File) (int64, error) {
	var hdr [10]byte
	n, err := f.ReadAt(hdr[:], 0)
	if n < len(hdr) {
		if err == io.EOF || err == nil {
			return 0, nil
		}
		return 0, err
	}
	if string(hdr[:3]) != "ID3" {
		return 0, nil
	}
	raw := binary.BigEndian.Uint32(hdr[6:])
	size := int64(raw&0x7f) | int64(raw>>8&0x7f)<<7 | int64(raw>>16&0x7f)<<14 | int64(raw>>24&0x7f)<<21
	return 10 + size, nil
}

// setOverride applies a single explicit override, returning whether it changed
// the current value. Caller must have validated the key.
func setOverride(tag *id3v2.Tag, key, val string) bool {
	switch key {
	case "title":
		if tag.Title() != val {
			tag.SetTitle(val)
			return true
		}
	case "artist":
		if tag.Artist() != val {
			tag.SetArtist(val)
			return true
		}
	case "album":
		if tag.Album() != val {
			tag.SetAlbum(val)
			return true
		}
	case "album_artist":
		id := tag.CommonID("Band/Orchestra/Accompaniment")
		if tag.GetTextFrame(id).Text != val {
			tag.AddTextFrame(id, tag.DefaultEncoding(), val)
			return true
		}
	case "composer":
		id := tag.CommonID("Composer")
		if tag.GetTextFrame(id).Text != val {
			tag.AddTextFrame(id, tag.DefaultEncoding(), val)
			return true
		}
	case "year":
		if tag.Year() != val {
			tag.SetYear(val)
			return true
		}
	case "genre":
		if tag.Genre() != val {
			tag.SetGenre(val)
			return true
		}
	case "track":
		id := tag.CommonID("Track number/Position in set")
		if tag.GetTextFrame(id).Text != val {
			tag.AddTextFrame(id, tag.DefaultEncoding(), val)
			return true
		}
	case "disc":
		id := tag.CommonID("Part of a set")
		if tag.GetTextFrame(id).Text != val {
			tag.AddTextFrame(id, tag.DefaultEncoding(), val)
			return true
		}
	case "comment":
		if userComment(tag) != val {
			setUserComment(tag, val)
			return true
		}
	}
	return false
}

// userComment returns the user comment: the COMM frame with an empty
// description. Other COMM frames carry player data under a description
// (iTunSMPB gapless info, iTunNORM normalisation) and are not the comment.
func userComment(tag *id3v2.Tag) string {
	comment := ""
	for _, f := range tag.GetFrames(tag.CommonID("Comments")) {
		if cf, ok := f.(id3v2.CommentFrame); ok && cf.Description == "" {
			comment = cf.Text
		}
	}
	return comment
}

// setUserComment replaces the user comment (empty-description COMM frames)
// with val and keeps every described COMM frame as it was.
func setUserComment(tag *id3v2.Tag, val string) {
	commID := tag.CommonID("Comments")
	var keep []id3v2.CommentFrame
	for _, f := range tag.GetFrames(commID) {
		if cf, ok := f.(id3v2.CommentFrame); ok && cf.Description != "" {
			keep = append(keep, cf)
		}
	}
	tag.DeleteFrames(commID)
	for _, cf := range keep {
		tag.AddCommentFrame(cf)
	}
	tag.AddCommentFrame(id3v2.CommentFrame{
		Encoding:    tag.DefaultEncoding(),
		Language:    "eng",
		Description: "",
		Text:        val,
	})
}

// fixAllEncoding iterates every text-bearing frame: text frames (T***),
// user-defined text (TXXX), comments (COMM) and unsynchronised lyrics (USLT).
// It re-decodes the values that look like mojibake through decoder and
// rewrites those frames in the tag's default encoding (unicodeEncoding). Returns whether any frame was
// changed. Binary frames (APIC, PRIV, ...) are left alone.
func fixAllEncoding(tag *id3v2.Tag, decoder *encoding.Decoder) bool {
	type rewrite struct {
		id     string
		frames []id3v2.Framer
	}
	var rewrites []rewrite

	for id, frames := range tag.AllFrames() {
		newFrames := make([]id3v2.Framer, 0, len(frames))
		anyChanged := false
		for _, f := range frames {
			switch frame := f.(type) {
			case id3v2.TextFrame:
				if !looksLikeCP1251Mojibake(frame.Text) {
					newFrames = append(newFrames, frame)
					continue
				}
				fixed, err := reinterpretAsCP1251(frame.Text, decoder)
				if err == nil && fixed != frame.Text {
					frame.Text = fixed
					frame.Encoding = tag.DefaultEncoding()
					anyChanged = true
				}
				newFrames = append(newFrames, frame)
			case id3v2.CommentFrame:
				if fixFields(decoder, &frame.Description, &frame.Text) {
					frame.Encoding = tag.DefaultEncoding()
					anyChanged = true
				}
				newFrames = append(newFrames, frame)
			case id3v2.UserDefinedTextFrame: // TXXX: album artist, series, narrator...
				if fixFields(decoder, &frame.Description, &frame.Value) {
					frame.Encoding = tag.DefaultEncoding()
					anyChanged = true
				}
				newFrames = append(newFrames, frame)
			case id3v2.UnsynchronisedLyricsFrame: // USLT
				if fixFields(decoder, &frame.ContentDescriptor, &frame.Lyrics) {
					frame.Encoding = tag.DefaultEncoding()
					anyChanged = true
				}
				newFrames = append(newFrames, frame)
			default:
				newFrames = append(newFrames, f)
			}
		}
		if anyChanged {
			rewrites = append(rewrites, rewrite{id, newFrames})
		}
	}

	for _, r := range rewrites {
		tag.DeleteFrames(r.id)
		for _, f := range r.frames {
			tag.AddFrame(r.id, f)
		}
	}

	return len(rewrites) > 0
}

// unicodeEncoding returns the Unicode text encoding the tag's version allows:
// UTF-8 (encoding byte 3) exists only in ID3v2.4, so a v2.3 tag, as most
// rutracker MP3s carry, gets UTF-16 with BOM (1). Strict v2.3 readers (WMP,
// car stereos) ignore UTF-8 frames or show them as Latin-1 mojibake.
func unicodeEncoding(tag *id3v2.Tag) id3v2.Encoding {
	if tag.Version() == 4 {
		return id3v2.EncodingUTF8
	}
	return id3v2.EncodingUTF16
}

// fixFields re-decodes, in place, each of fields that looks like mojibake.
// It changes nothing unless every re-decode succeeds, and reports whether a
// value changed.
func fixFields(decoder *encoding.Decoder, fields ...*string) bool {
	fixed := make([]string, len(fields))
	changed := false
	for i, f := range fields {
		fixed[i] = *f
		if !looksLikeCP1251Mojibake(*f) {
			continue
		}
		v, err := reinterpretAsCP1251(*f, decoder)
		if err != nil {
			return false
		}
		fixed[i] = v
		changed = changed || v != *f
	}
	if changed {
		for i, f := range fields {
			*f = fixed[i]
		}
	}
	return changed
}

// looksLikeCP1251Mojibake reports whether s looks like cp1251 bytes that
// were decoded as Latin-1, so re-decoding it as cp1251 is safe. It judges
// each word (a run of letters): a Cyrillic word read as Latin-1 consists of
// U+00C0–U+00FF letters only (cp1251 А–я, plus U+00A8/U+00B8 for Ё/ё),
// while correct Western words (Café, Déjà, Beyoncé, Björk) always mix those
// letters with ASCII ones. So s is mojibake when it has a word of two or
// more such letters and no word that mixes them with ASCII letters; Latin
// words beside the mojibake ("Pink Floyd - Ñòåíà", "Track 01 - Ãëàâà 1")
// do not hide it. Any rune above U+00FF means s is not Latin-1-decoded at
// all. fix_encoding only rewrites values that pass this check, so frames
// that are already correct survive.
func looksLikeCP1251Mojibake(s string) bool {
	mojibakeWord := false
	high, ascii := 0, 0
	endWord := func() bool {
		if high > 0 && ascii > 0 {
			return false // a mixed word: correct accented Latin text
		}
		if high >= 2 {
			mojibakeWord = true
		}
		high, ascii = 0, 0
		return true
	}
	for _, r := range s {
		switch {
		case r > 0xFF:
			return false
		case r >= 0xC0 || r == 0xA8 || r == 0xB8:
			high++
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			ascii++
		default:
			if !endWord() {
				return false
			}
		}
	}
	return endWord() && mojibakeWord
}

// reinterpretAsCP1251 takes a string that was decoded as Latin-1 (or similar)
// but whose source bytes were actually cp1251. It re-encodes the string as
// Latin-1 bytes, then decodes those bytes as cp1251, yielding correct UTF-8.
func reinterpretAsCP1251(s string, dec *encoding.Decoder) (string, error) {
	raw := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			// Rune doesn't fit in a single byte — string isn't mojibake of this form.
			return s, nil
		}
		raw = append(raw, byte(r))
	}
	return dec.String(string(raw))
}

// encodingHint returns "cp1251" when any one tag value looks like cp1251
// bytes mis-decoded as Latin-1, else "". Each value is judged on its own with
// the same test fix_encoding applies, so the hint never recommends a fix that
// would leave everything unchanged, and accents spread over several correct
// Western values (Jóga / Björk / Homogénic) do not add up to a false hint.
func encodingHint(parts ...string) string {
	for _, p := range parts {
		if looksLikeCP1251Mojibake(p) {
			return "cp1251"
		}
	}
	return ""
}

// collectAudioFiles returns the list of supported audio files at or under root.
// If root is a single file, returns [root] if its extension is supported.
func collectAudioFiles(root string, recursive bool) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", root, err)
	}
	if !info.IsDir() {
		if audioExtensions[strings.ToLower(filepath.Ext(root))] {
			return []string{root}, nil
		}
		return nil, nil
	}

	var files []string
	if recursive {
		err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Regular files only: a symlinked .mp3 may point outside
			// TGT_DOWNLOADPATH, and WalkDir does not follow it as a dir.
			if !d.Type().IsRegular() {
				return nil
			}
			if audioExtensions[strings.ToLower(filepath.Ext(p))] {
				files = append(files, p)
			}
			return nil
		})
		return files, err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		// Regular files only (see the recursive branch).
		if !e.Type().IsRegular() {
			continue
		}
		full := filepath.Join(root, e.Name())
		if audioExtensions[strings.ToLower(filepath.Ext(full))] {
			files = append(files, full)
		}
	}
	return files, nil
}

// displayPath returns p relative to downloadRoot when possible, else p.
func displayPath(downloadRoot, p string) string {
	if rel, err := filepath.Rel(downloadRoot, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}
