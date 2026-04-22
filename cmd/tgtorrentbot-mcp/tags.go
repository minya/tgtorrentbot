package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

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
	FixEncoding string            `json:"fix_encoding,omitempty" jsonschema:"if set (e.g. 'cp1251'), re-decode ALL text and comment frames from this codepage to UTF-8 before applying overrides"`
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
	return true, tag.Save()
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
				fixed, err := reinterpretAsCP1251(frame.Text, decoder)
				if err == nil && fixed != frame.Text {
					frame.Text = fixed
					frame.Encoding = id3v2.EncodingUTF8
					anyChanged = true
				}
				newFrames = append(newFrames, frame)
			case id3v2.CommentFrame:
				fixedDesc, derr := reinterpretAsCP1251(frame.Description, decoder)
				fixedText, terr := reinterpretAsCP1251(frame.Text, decoder)
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

// encodingHint returns "cp1251" when the combined tags look like cp1251 bytes
// mis-decoded as Latin-1, else "".
func encodingHint(parts ...string) string {
	joined := strings.Join(parts, "")
	if joined == "" {
		return ""
	}
	total, suspect := 0, 0
	for _, r := range joined {
		if r < 0x80 {
			continue
		}
		total++
		if r >= 0xC0 && r <= 0xFF {
			// If re-decoding this byte as cp1251 yields Cyrillic, that's mojibake.
			decoded, err := charmap.Windows1251.NewDecoder().Bytes([]byte{byte(r)})
			if err == nil && utf8.RuneCountInString(string(decoded)) == 1 {
				dr, _ := utf8.DecodeRune(decoded)
				if dr >= 0x0400 && dr <= 0x04FF {
					suspect++
				}
			}
		}
	}
	if total > 2 && suspect*2 > total {
		return "cp1251"
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
			if d.IsDir() {
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
		if e.IsDir() {
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
