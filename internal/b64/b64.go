// Package b64 is the shared Base64 core of the b64 command: encoding with
// optional URL-safe alphabet, padding and line wrapping, lenient decoding,
// validation and data URIs. Both directions also work as streams, so input
// of any size is processed in constant memory.
package b64

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"
)

// EncodeOptions selects the alphabet, padding and line wrapping of encoded output.
type EncodeOptions struct {
	URL   bool // URL-safe alphabet (RFC 4648 §5)
	NoPad bool // drop trailing '=' padding
	Wrap  int  // insert '\n' after every Wrap output characters; 0 disables wrapping
}

func encoding(o EncodeOptions) *base64.Encoding {
	switch {
	case o.URL && o.NoPad:
		return base64.RawURLEncoding
	case o.URL:
		return base64.URLEncoding
	case o.NoPad:
		return base64.RawStdEncoding
	default:
		return base64.StdEncoding
	}
}

// Encode returns the Base64 encoding of src.
func Encode(src []byte, o EncodeOptions) string {
	var buf bytes.Buffer
	enc := NewEncoder(&buf, o)
	enc.Write(src) // writes to a bytes.Buffer cannot fail
	enc.Close()
	return buf.String()
}

// NewEncoder returns a stream encoder writing to w. The caller must Close it
// to flush the final partial group. No newline is written at the end.
func NewEncoder(w io.Writer, o EncodeOptions) io.WriteCloser {
	if o.Wrap > 0 {
		w = &wrapWriter{w: w, width: o.Wrap}
	}
	return base64.NewEncoder(encoding(o), w)
}

// wrapWriter inserts '\n' after every width bytes, never at the very end:
// the newline is written only when another byte follows.
type wrapWriter struct {
	w     io.Writer
	width int
	col   int
	buf   []byte
}

func (ww *wrapWriter) Write(p []byte) (int, error) {
	ww.buf = ww.buf[:0]
	for _, c := range p {
		if ww.col == ww.width {
			ww.buf = append(ww.buf, '\n')
			ww.col = 0
		}
		ww.buf = append(ww.buf, c)
		ww.col++
	}
	if _, err := ww.w.Write(ww.buf); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Error is a decoding error caused by the input (as opposed to an I/O error).
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func errorf(format string, args ...any) error { return &Error{Msg: fmt.Sprintf(format, args...)} }

// Info describes successfully decoded input.
type Info struct {
	Variant string // "standard", "url", "mixed" or "either"
	Padded  bool   // cleaned length, '=' included, is a multiple of 4
	Bytes   int64  // decoded length
}

const invalid = 0xff

// decodeMap accepts both alphabets: '+' and '-' are 62, '/' and '_' are 63.
var decodeMap = func() (m [256]byte) {
	for i := range m {
		m[i] = invalid
	}
	const std = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for i := 0; i < len(std); i++ {
		m[std[i]] = byte(i)
	}
	m['-'] = 62
	m['_'] = 63
	return m
}()

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

// headerWindow bounds how far into the input the ";base64," of a data URI
// prefix is looked for when streaming.
const headerWindow = 64 << 10

// DecodeStream decodes Base64 text from r and writes the bytes to w.
//
// Decoding is lenient: surrounding whitespace and a leading data URI header
// ("data:...;base64,") are dropped, spaces, tabs, CR and LF are ignored
// anywhere, up to two trailing '=' are optional, both alphabets are accepted
// (also mixed), and non-zero trailing bits are allowed. Errors are reported
// left to right with the 0-based position in the cleaned text (whitespace and
// data URI header removed). Decoded bytes are buffered in 64 KiB blocks, so
// input that fails before that much output has been produced writes nothing.
func DecodeStream(w io.Writer, r io.Reader) (Info, error) {
	br := bufio.NewReaderSize(r, headerWindow)
	if err := skipHeader(br); err != nil {
		return Info{}, err
	}

	var (
		in       = make([]byte, 32<<10)
		out      = make([]byte, 0, 64<<10)
		quad     [4]byte
		n        int   // characters in quad
		pos      int64 // position in the cleaned text
		pads     int   // '=' seen so far
		padPos   int64 // position of the first '='
		std, url bool
		total    int64
	)
	flush := func() error {
		if len(out) == 0 {
			return nil
		}
		_, err := w.Write(out)
		out = out[:0]
		return err
	}
	emit := func(b ...byte) error {
		if len(out)+len(b) > cap(out) {
			if err := flush(); err != nil {
				return err
			}
		}
		out = append(out, b...)
		total += int64(len(b))
		return nil
	}

	for {
		k, rerr := br.Read(in)
		chunk := in[:k]
		for i := 0; i < len(chunk); i++ {
			c := chunk[i]
			if isSpace(c) {
				continue
			}
			if c == '=' {
				if pads == 0 {
					padPos = pos
				}
				pads++
				pos++
				continue
			}
			if pads > 0 {
				return Info{}, errorf("unexpected '=' at position %d: padding is only allowed at the end", padPos)
			}
			v := decodeMap[c]
			if v == invalid {
				return Info{}, errorf("invalid character %s at position %d", describe(chunk[i:], br), pos)
			}
			switch c {
			case '+', '/':
				std = true
			case '-', '_':
				url = true
			}
			quad[n] = v
			n++
			pos++
			if n == 4 {
				if err := emit(quad[0]<<2|quad[1]>>4, quad[1]<<4|quad[2]>>2, quad[2]<<6|quad[3]); err != nil {
					return Info{}, err
				}
				n = 0
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return Info{}, rerr
		}
	}

	if pads > 2 {
		return Info{}, errorf("too much padding: %d '=' at position %d, at most 2 are allowed", pads, padPos)
	}
	switch n {
	case 1:
		return Info{}, errorf("invalid length: %d characters leave a single character in the last group", pos-int64(pads))
	case 2:
		if err := emit(quad[0]<<2 | quad[1]>>4); err != nil {
			return Info{}, err
		}
	case 3:
		if err := emit(quad[0]<<2|quad[1]>>4, quad[1]<<4|quad[2]>>2); err != nil {
			return Info{}, err
		}
	}
	if err := flush(); err != nil {
		return Info{}, err
	}

	info := Info{Variant: "either", Padded: pos%4 == 0, Bytes: total}
	switch {
	case std && url:
		info.Variant = "mixed"
	case std:
		info.Variant = "standard"
	case url:
		info.Variant = "url"
	}
	return info, nil
}

// skipHeader drops leading whitespace and, when the input starts with
// "data:" (any case) and contains ";base64," within the header window,
// everything up to and including ";base64,".
func skipHeader(br *bufio.Reader) error {
	for {
		c, err := br.ReadByte()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if !isSpace(c) {
			br.UnreadByte()
			break
		}
	}
	head, err := br.Peek(5)
	if err != nil && err != io.EOF {
		return err
	}
	if len(head) < 5 || !bytes.EqualFold(head, []byte("data:")) {
		return nil
	}
	window, err := br.Peek(headerWindow)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return err
	}
	if i := bytes.Index(window, []byte(";base64,")); i >= 0 {
		br.Discard(i + len(";base64,"))
	}
	return nil
}

// describe quotes the character starting at p[0], reading continuation bytes
// of a multi-byte UTF-8 sequence from br when it is split across chunks.
func describe(p []byte, br *bufio.Reader) string {
	if !utf8.FullRune(p) {
		more, _ := br.Peek(utf8.UTFMax - len(p))
		p = append(append([]byte{}, p...), more...)
	}
	r, size := utf8.DecodeRune(p)
	if r == utf8.RuneError && size <= 1 {
		return fmt.Sprintf("'\\x%02x'", p[0])
	}
	return fmt.Sprintf("%q", r)
}

// Decode decodes s with the same lenient rules as DecodeStream.
func Decode(s string) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := DecodeStream(&buf, bytes.NewReader([]byte(s))); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Validate reports whether s decodes, and if so its variant, padding and size.
func Validate(s string) (Info, error) {
	return DecodeStream(io.Discard, bytes.NewReader([]byte(s)))
}

// SniffLen is how many leading bytes SniffMagic needs to recognize a format.
const SniffLen = 12

// SniffMagic returns the MIME type recognized from the first bytes of the
// data (PNG, JPEG, GIF, WebP, PDF, SVG), or "" when there is no match.
func SniffMagic(head []byte) string {
	switch {
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(head, []byte("\xff\xd8\xff")):
		return "image/jpeg"
	case bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")):
		return "image/gif"
	case len(head) >= 12 && bytes.HasPrefix(head, []byte("RIFF")) && string(head[8:12]) == "WEBP":
		return "image/webp"
	case bytes.HasPrefix(head, []byte("%PDF-")):
		return "application/pdf"
	}
	return ""
}

// SniffMIME returns the MIME type for a data URI of data: a magic-byte match,
// else "text/plain;charset=utf-8" for valid UTF-8 without control characters
// other than tab, CR and LF, else "application/octet-stream".
func SniffMIME(data []byte) string {
	if m := SniffMagic(data); m != "" {
		return m
	}
	if isSVG(data) {
		return "image/svg+xml"
	}
	if isText(data) {
		return "text/plain;charset=utf-8"
	}
	return "application/octet-stream"
}

// isSVG: text starting with "<svg", or with "<?xml" and containing "<svg".
// Leading spaces, tabs, CR and LF are ignored.
func isSVG(data []byte) bool {
	t := bytes.TrimLeft(data, " \t\r\n")
	return bytes.HasPrefix(t, []byte("<svg")) ||
		bytes.HasPrefix(t, []byte("<?xml")) && bytes.Contains(t, []byte("<svg"))
}

func isText(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for _, r := range string(data) {
		if unicode.IsControl(r) && r != '\t' && r != '\r' && r != '\n' {
			return false
		}
	}
	return true
}

// DataURI returns "data:<mime>;base64,<payload>" for data. An empty mime is
// sniffed with SniffMIME. Data URIs are never wrapped.
func DataURI(data []byte, mime string) string {
	if mime == "" {
		mime = SniffMIME(data)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}
