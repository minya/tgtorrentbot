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
	Path      string `json:"path" jsonschema:"path to an audio file or a directory, relative to the download path or absolute under it"`
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
	Files []fileTags `json:"files"`
}

type writeTagsInput struct {
	Path        string            `json:"path" jsonschema:"path to an audio file or a directory, relative to the download path or absolute under it"`
	Recursive   bool              `json:"recursive,omitempty" jsonschema:"when path is a directory, recurse into subdirectories (default false)"`
	FixEncoding string            `json:"fix_encoding,omitempty" jsonschema:"if set (e.g. 'cp1251'), re-decode text and comment frames from this codepage to UTF-8 before applying overrides; only values that look like cp1251 mojibake are rewritten"`
	Tags        map[string]string `json:"tags,omitempty" jsonschema:"optional explicit tag overrides; keys: title, artist, album, album_artist, composer, year, genre, track, disc, comment"`
}

type writeTagsResult struct {
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
	Error   string `json:"error,omitempty"`
}

type writeTagsOutput struct {
	Files []writeTagsResult `json:"files"`
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
		if commFrames := tag.GetFrames(tag.CommonID("Comments")); len(commFrames) > 0 {
			if cf, ok := commFrames[len(commFrames)-1].(id3v2.CommentFrame); ok {
				ft.Comment = cf.Text
			}
		}
		tag.Close()

		if hint := encodingHint(ft.Title, ft.Artist, ft.Album, ft.AlbumArtist, ft.Composer, ft.Genre, ft.Comment); hint != "" {
			ft.EncodingHint = hint
		}
		out.Files = append(out.Files, ft)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mustJSON(out)}},
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
		} else {
			res.Changed = changed
		}
		out.Files = append(out.Files, res)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mustJSON(out)}},
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
	tag.SetDefaultEncoding(id3v2.EncodingUTF8)

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
		commID := tag.CommonID("Comments")
		current := ""
		if frames := tag.GetFrames(commID); len(frames) > 0 {
			if cf, ok := frames[len(frames)-1].(id3v2.CommentFrame); ok {
				current = cf.Text
			}
		}
		if current != val {
			tag.DeleteFrames(commID)
			tag.AddCommentFrame(id3v2.CommentFrame{
				Encoding:    tag.DefaultEncoding(),
				Language:    "eng",
				Description: "",
				Text:        val,
			})
			return true
		}
	}
	return false
}

// fixAllEncoding iterates every text frame (T*** except TXXX) and every COMM
// frame, re-decodes the text values through decoder, and rewrites the frames
// with UTF-8 encoding. Returns whether any frame was changed.
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
					frame.Encoding = id3v2.EncodingUTF8
					anyChanged = true
				}
				newFrames = append(newFrames, frame)
			case id3v2.CommentFrame:
				fixedDesc, fixedText := frame.Description, frame.Text
				var derr, terr error
				if looksLikeCP1251Mojibake(frame.Description) {
					fixedDesc, derr = reinterpretAsCP1251(frame.Description, decoder)
				}
				if looksLikeCP1251Mojibake(frame.Text) {
					fixedText, terr = reinterpretAsCP1251(frame.Text, decoder)
				}
				if derr == nil && terr == nil && (fixedDesc != frame.Description || fixedText != frame.Text) {
					frame.Description = fixedDesc
					frame.Text = fixedText
					frame.Encoding = id3v2.EncodingUTF8
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

// looksLikeCP1251Mojibake reports whether s looks like cp1251 bytes that
// were decoded as Latin-1, so re-decoding it as cp1251 is safe. Such text
// (Cyrillic words read as Latin-1) consists of runs of U+00C0–U+00FF letters
// (cp1251 А–я), plus U+00A8/U+00B8 (Ё/ё), and outnumbers ASCII letters,
// while correct Western text (Café, Déjà Vu, Beyoncé) has a few accented
// letters among many ASCII ones. Any rune above U+00FF means the text is not
// Latin-1-decoded at all. fix_encoding only rewrites values that pass this
// check, so frames that are already correct survive.
func looksLikeCP1251Mojibake(s string) bool {
	high, ascii := 0, 0
	for _, r := range s {
		switch {
		case r > 0xFF:
			return false
		case r >= 0xC0 || r == 0xA8 || r == 0xB8:
			high++
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
			ascii++
		}
	}
	return high >= 2 && high > ascii
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
