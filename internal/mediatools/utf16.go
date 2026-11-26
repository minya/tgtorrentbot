package mediatools

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"unicode/utf16"

	"github.com/bogem/id3v2/v2"
)

// github.com/bogem/id3v2/v2 (v2.1.4, the latest release) writes every
// UTF-16 string with one stray zero byte after it unless its last encoded
// byte already is zero (encodeWriteText), then the two-byte terminator. In a
// text frame (TPE2 "АК-47" → … 00 37 00 00 00) readers see a NUL character
// in the value: Jellyfin logs "contains a null character in its tag" and cuts
// the value there. In a frame with a description (TXXX, COMM, USLT, APIC) the
// stray byte shifts the value by one byte, and readers other than this
// library (mutagen, exiftool, TagLib) read it as garbage. UTF-16 is what
// write_tags writes into ID3v2.3 tags (unicodeEncoding), and any save writes
// back every frame of the tag, so correctUTF16Frames replaces each UTF-16
// frame with its correct bytes before the tag is written.

// encodedFrame is a frame whose body is already encoded.
type encodedFrame struct {
	uid  string
	body []byte
}

func (f encodedFrame) UniqueIdentifier() string { return f.uid }
func (f encodedFrame) Size() int                { return len(f.body) }
func (f encodedFrame) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(f.body)
	return int64(n), err
}

var utf16Terminator = []byte{0, 0}

// appendUTF16 appends s as UTF-16LE with a byte order mark, which ID3v2
// requires for each string of an encoding-1 frame (an empty one included).
func appendUTF16(b []byte, s string) []byte {
	b = append(b, 0xFF, 0xFE)
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return b
}

// utf16Body returns the correct body of f when it is a UTF-16 frame of a
// type the library writes wrong. The layout is the library's own (a text
// frame and a description end with the terminator; the value of TXXX, COMM
// and USLT does not), without the stray byte.
func utf16Body(f id3v2.Framer) ([]byte, bool) {
	key := []byte{id3v2.EncodingUTF16.Key}
	switch fr := f.(type) {
	case id3v2.TextFrame:
		if !fr.Encoding.Equals(id3v2.EncodingUTF16) {
			return nil, false
		}
		return append(appendUTF16(key, fr.Text), utf16Terminator...), true
	case id3v2.UserDefinedTextFrame:
		if !fr.Encoding.Equals(id3v2.EncodingUTF16) {
			return nil, false
		}
		b := append(appendUTF16(key, fr.Description), utf16Terminator...)
		return appendUTF16(b, fr.Value), true
	case id3v2.CommentFrame:
		if !fr.Encoding.Equals(id3v2.EncodingUTF16) || len(fr.Language) != 3 {
			return nil, false // a bad language is left to the library, which refuses it
		}
		b := append(appendUTF16(append(key, fr.Language...), fr.Description), utf16Terminator...)
		return appendUTF16(b, fr.Text), true
	case id3v2.UnsynchronisedLyricsFrame:
		if !fr.Encoding.Equals(id3v2.EncodingUTF16) || len(fr.Language) != 3 {
			return nil, false
		}
		b := append(appendUTF16(append(key, fr.Language...), fr.ContentDescriptor), utf16Terminator...)
		return appendUTF16(b, fr.Lyrics), true
	case id3v2.PictureFrame:
		if !fr.Encoding.Equals(id3v2.EncodingUTF16) {
			return nil, false
		}
		b := append(key, fr.MimeType...)
		b = append(b, 0, fr.PictureType)
		b = append(appendUTF16(b, fr.Description), utf16Terminator...)
		return append(b, fr.Picture...), true
	}
	return nil, false
}

// correctUTF16Frames replaces every UTF-16 frame of tag that the library
// would write with a stray byte by a encodedFrame with the correct bytes,
// keeping the frame order and the identifiers that keep sequences apart.
func correctUTF16Frames(tag *id3v2.Tag) {
	for id, frames := range tag.AllFrames() {
		fixed := make([]id3v2.Framer, len(frames))
		changed := false
		for i, f := range frames {
			fixed[i] = f
			if body, ok := utf16Body(f); ok {
				fixed[i] = encodedFrame{uid: f.UniqueIdentifier(), body: body}
				changed = true
			}
		}
		if !changed {
			continue
		}
		tag.DeleteFrames(id)
		for _, f := range fixed {
			tag.AddFrame(id, f)
		}
	}
}

// The library's reader is built around its writer: for UTF-16 it ends a
// string at the first two zero bytes, aligned or not, and then takes one
// more byte. A correctly written UTF-16LE description whose last character
// is not ASCII ("Серия" → … 4f 04 00 00) is then read with half of its
// terminator, and the value or the picture after it is shifted by one byte,
// so a save writes the damage back. rereadDescribedFrames therefore decodes
// the frames with a description (TXXX, COMM, USLT, APIC) itself, from the
// raw tag: strings end at an aligned terminator, and the stray byte the
// library wrote before this fix is skipped.

var describedFrameIDs = map[string]bool{"TXXX": true, "COMM": true, "USLT": true, "APIC": true}

// rereadDescribedFrames replaces the described frames of tag, which was
// parsed from path, with frames decoded from the raw tag, when one of them
// uses UTF-16.
func rereadDescribedFrames(tag *id3v2.Tag, path string) error {
	frames, err := readRawFrames(path)
	if err != nil {
		return err
	}
	byID := map[string][]id3v2.Framer{}
	needed := map[string]bool{}
	for _, rf := range frames {
		if !describedFrameIDs[rf.id] || len(rf.body) == 0 {
			continue
		}
		f, ok := decodeDescribedFrame(rf.id, rf.body)
		if !ok {
			return fmt.Errorf("cannot decode %s frame", rf.id)
		}
		byID[rf.id] = append(byID[rf.id], f)
		if k := rf.body[0]; k == id3v2.EncodingUTF16.Key || k == id3v2.EncodingUTF16BE.Key {
			needed[rf.id] = true
		}
	}
	for id := range needed {
		tag.DeleteFrames(id)
		for _, f := range byID[id] {
			tag.AddFrame(id, f)
		}
	}
	return nil
}

type rawID3Frame struct {
	id   string
	body []byte
}

// readRawFrames returns the frames of the ID3v2.3/2.4 tag at the start of
// path. checkID3Layout has refused the layouts this cannot read.
func readRawFrames(path string) ([]rawID3Frame, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var hdr [10]byte
	if n, _ := f.ReadAt(hdr[:], 0); n < len(hdr) || string(hdr[:3]) != "ID3" {
		return nil, nil
	}
	version := hdr[3]
	body := make([]byte, synchsafe(hdr[6:10]))
	if _, err := f.ReadAt(body, 10); err != nil && err != io.EOF {
		return nil, err
	}
	var out []rawID3Frame
	for off := 0; off+10 <= len(body) && body[off] != 0; {
		fh := body[off : off+10]
		size := int(binary.BigEndian.Uint32(fh[4:8]))
		if version == 4 {
			size = int(synchsafe(fh[4:8]))
		}
		end := off + 10 + size
		if end > len(body) {
			end = len(body)
		}
		out = append(out, rawID3Frame{id: string(fh[:4]), body: body[off+10 : end]})
		off += 10 + size
	}
	return out, nil
}

// decodeDescribedFrame decodes the body of a TXXX, COMM, USLT or APIC frame.
func decodeDescribedFrame(id string, b []byte) (id3v2.Framer, bool) {
	if len(b) < 1 {
		return nil, false
	}
	enc, rest := b[0], b[1:]
	encoding, ok := encodingByKey(enc)
	if !ok {
		return nil, false
	}
	switch id {
	case "TXXX":
		desc, rest := readString(enc, rest)
		return id3v2.UserDefinedTextFrame{Encoding: encoding, Description: desc, Value: finalString(enc, rest)}, true
	case "COMM", "USLT":
		if len(rest) < 3 {
			return nil, false
		}
		lang := string(rest[:3])
		desc, rest := readString(enc, rest[3:])
		text := finalString(enc, rest)
		if id == "COMM" {
			return id3v2.CommentFrame{Encoding: encoding, Language: lang, Description: desc, Text: text}, true
		}
		return id3v2.UnsynchronisedLyricsFrame{Encoding: encoding, Language: lang, ContentDescriptor: desc, Lyrics: text}, true
	case "APIC":
		i := bytes.IndexByte(rest, 0)
		if i < 0 || i+1 >= len(rest) {
			return nil, false
		}
		mime, ptype := string(rest[:i]), rest[i+1]
		desc, pic := readString(enc, rest[i+2:])
		return id3v2.PictureFrame{Encoding: encoding, MimeType: mime, PictureType: ptype, Description: desc, Picture: append([]byte(nil), pic...)}, true
	}
	return nil, false
}

func encodingByKey(k byte) (id3v2.Encoding, bool) {
	for _, e := range []id3v2.Encoding{id3v2.EncodingISO, id3v2.EncodingUTF16, id3v2.EncodingUTF16BE, id3v2.EncodingUTF8} {
		if e.Key == k {
			return e, true
		}
	}
	return id3v2.Encoding{}, false
}

func isUTF16(enc byte) bool {
	return enc == id3v2.EncodingUTF16.Key || enc == id3v2.EncodingUTF16BE.Key
}

// readString reads one terminated string and returns it with the bytes after
// its terminator. Without a terminator the string runs to the end.
func readString(enc byte, b []byte) (string, []byte) {
	if !isUTF16(enc) {
		i := bytes.IndexByte(b, 0)
		if i < 0 {
			return decodeString(enc, b), nil
		}
		return decodeString(enc, b[:i]), b[i+1:]
	}
	for i := 0; i+1 < len(b); i += 2 {
		if b[i] == 0 && b[i+1] == 0 {
			rest := b[i+2:]
			// The stray byte of the library's writer (see utf16Body). In a
			// correct frame a description is followed by a BOM (TXXX, COMM,
			// USLT) or by picture data, never by a zero byte.
			if enc == id3v2.EncodingUTF16.Key && len(rest) > 0 && rest[0] == 0 {
				rest = rest[1:]
			}
			return decodeString(enc, b[:i]), rest
		}
	}
	return decodeString(enc, b), nil
}

// finalString decodes the last string of a frame, which runs to its end;
// trailing terminators are dropped.
func finalString(enc byte, b []byte) string {
	if isUTF16(enc) {
		if len(b)%2 == 1 {
			b = b[:len(b)-1]
		}
		for len(b) >= 2 && b[len(b)-2] == 0 && b[len(b)-1] == 0 {
			b = b[:len(b)-2]
		}
	} else {
		b = bytes.TrimRight(b, "\x00")
	}
	return decodeString(enc, b)
}

func hasBOM(b []byte) bool {
	return len(b) >= 2 && (b[0] == 0xFF && b[1] == 0xFE || b[0] == 0xFE && b[1] == 0xFF)
}

// decodeString decodes b, a string without its terminator, as the library
// does: ISO-8859-1 maps each byte to a rune, UTF-16 with a BOM follows the
// BOM and is big-endian without one.
func decodeString(enc byte, b []byte) string {
	switch enc {
	case id3v2.EncodingISO.Key:
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		return string(r)
	case id3v2.EncodingUTF8.Key:
		return string(b)
	}
	order := binary.ByteOrder(binary.BigEndian)
	if enc == id3v2.EncodingUTF16.Key && hasBOM(b) {
		if b[0] == 0xFF {
			order = binary.LittleEndian
		}
		b = b[2:]
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, order.Uint16(b[i:]))
	}
	return string(utf16.Decode(u))
}
