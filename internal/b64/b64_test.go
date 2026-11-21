package b64

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"
)

type vector struct {
	Op      string `json:"op"`
	InHex   string `json:"in_hex"`
	URL     bool   `json:"url"`
	Pad     *bool  `json:"pad"`
	Wrap    int    `json:"wrap"`
	Out     string `json:"out"`
	In      string `json:"in"`
	OutHex  string `json:"out_hex"`
	Error   bool   `json:"error"`
	Valid   bool   `json:"valid"`
	Variant string `json:"variant"`
	Padded  bool   `json:"padded"`
	Bytes   int64  `json:"bytes"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct{ Cases []vector }
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) == 0 {
		t.Fatal("no vectors")
	}
	return file.Cases
}

// readers returns ways to feed s to the stream decoder, including one byte
// at a time to exercise every chunk boundary.
func readers(s string) map[string]func() io.Reader {
	return map[string]func() io.Reader{
		"whole":   func() io.Reader { return strings.NewReader(s) },
		"onebyte": func() io.Reader { return iotest.OneByteReader(strings.NewReader(s)) },
		"dataerr": func() io.Reader { return iotest.DataErrReader(strings.NewReader(s)) },
	}
}

func TestVectors(t *testing.T) {
	counts := map[string]int{}
	for i, v := range loadVectors(t) {
		counts[v.Op]++
		name := fmt.Sprintf("%d_%s", i, v.Op)
		t.Run(name, func(t *testing.T) {
			switch v.Op {
			case "encode":
				in, err := hex.DecodeString(v.InHex)
				if err != nil {
					t.Fatal(err)
				}
				o := EncodeOptions{URL: v.URL, NoPad: v.Pad != nil && !*v.Pad, Wrap: v.Wrap}
				if got := Encode(in, o); got != v.Out {
					t.Errorf("Encode(%s, %+v) = %q, want %q", v.InHex, o, got, v.Out)
				}
				var buf bytes.Buffer
				enc := NewEncoder(&buf, o)
				if _, err := io.Copy(enc, iotest.OneByteReader(bytes.NewReader(in))); err != nil {
					t.Fatal(err)
				}
				enc.Close()
				if buf.String() != v.Out {
					t.Errorf("streamed encode = %q, want %q", buf.String(), v.Out)
				}

			case "decode":
				got, err := Decode(v.In)
				if v.Error {
					var de *Error
					if !errors.As(err, &de) {
						t.Errorf("Decode(%q) = %x, %v; want a decode error", v.In, got, err)
					}
				} else if err != nil || hex.EncodeToString(got) != v.OutHex {
					t.Errorf("Decode(%q) = %x, %v; want %s", v.In, got, err, v.OutHex)
				}
				for rname, r := range readers(v.In) {
					var buf bytes.Buffer
					_, err := DecodeStream(&buf, r())
					if v.Error != (err != nil) || !v.Error && hex.EncodeToString(buf.Bytes()) != v.OutHex {
						t.Errorf("DecodeStream[%s](%q) = %x, %v", rname, v.In, buf.Bytes(), err)
					}
				}

			case "validate":
				info, err := Validate(v.In)
				if (err == nil) != v.Valid {
					t.Fatalf("Validate(%q) error = %v, want valid=%v", v.In, err, v.Valid)
				}
				if v.Valid {
					want := Info{Variant: v.Variant, Padded: v.Padded, Bytes: v.Bytes}
					if info != want {
						t.Errorf("Validate(%q) = %+v, want %+v", v.In, info, want)
					}
				}

			default:
				t.Fatalf("unknown op %q", v.Op)
			}
		})
	}
	t.Logf("vectors: %v", counts)
}

func TestDecodeErrors(t *testing.T) {
	cases := map[string]string{
		"Zm9v!":                "invalid character '!' at position 4",
		" Zm 9v\n!":            "invalid character '!' at position 4",
		"data:x;base64,Zm9v*":  "invalid character '*' at position 4",
		"Zm9vé":                "invalid character 'é' at position 4",
		"Zm9v\x00":             "invalid character '\\x00' at position 4",
		"Zm9v\xff":             "invalid character '\\xff' at position 4",
		"Zm=9v":                "unexpected '=' at position 2: padding is only allowed at the end",
		"Zg= =x":               "unexpected '=' at position 2: padding is only allowed at the end",
		"Zg===":                "too much padding: 3 '=' at position 2, at most 2 are allowed",
		"====":                 "too much padding: 4 '=' at position 0, at most 2 are allowed",
		"Zm9vY":                "invalid length: 5 characters leave a single character in the last group",
		"Zm9vY=":               "invalid length: 5 characters leave a single character in the last group",
		"data:text/plain,SGk=": "invalid character ':' at position 4",
	}
	for in, want := range cases {
		for rname, r := range readers(in) {
			_, err := DecodeStream(io.Discard, r())
			if err == nil || err.Error() != want {
				t.Errorf("DecodeStream[%s](%q) error = %v, want %q", rname, in, err, want)
			}
		}
	}
}

func TestDecodeLenient(t *testing.T) {
	cases := map[string]string{
		"=":                      "",
		"==":                     "",
		"  \r\n\t ":              "",
		"Zg = = \n":              "f",
		"Zh==":                   "f", // non-zero trailing bits are accepted
		"\n data:;base64,Zg==\n": "f",
		"data:image/svg+xml;charset=utf-8;base64, Zm9v\nYmFy": "foobar",
		"Data:text/plain;base64,":                             "",
	}
	for in, want := range cases {
		got, err := Decode(in)
		if err != nil || string(got) != want {
			t.Errorf("Decode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestDecodeHeaderBeyondWindow(t *testing.T) {
	// The streaming decoder looks for ";base64," only within the first 64 KiB.
	in := "data:" + strings.Repeat("a", headerWindow) + ";base64,Zg=="
	if _, err := Decode(in); err == nil {
		t.Fatal("expected an error for a header beyond the window")
	}
	in = "data:" + strings.Repeat("a", headerWindow-20) + ";base64,Zg=="
	if got, err := Decode(in); err != nil || string(got) != "f" {
		t.Fatalf("Decode = %q, %v", got, err)
	}
}

func TestWrap(t *testing.T) {
	in := bytes.Repeat([]byte{0xAB}, 300)
	for _, width := range []int{1, 3, 4, 5, 64, 76, 400, 401} {
		got := Encode(in, EncodeOptions{Wrap: width})
		plain := Encode(in, EncodeOptions{})
		var want strings.Builder
		for i := 0; i < len(plain); i += width {
			if i > 0 {
				want.WriteByte('\n')
			}
			want.WriteString(plain[i:min(i+width, len(plain))])
		}
		if got != want.String() {
			t.Errorf("wrap %d: got %q", width, got)
		}
		if strings.HasSuffix(got, "\n") {
			t.Errorf("wrap %d: trailing newline", width)
		}
		back, err := Decode(got)
		if err != nil || !bytes.Equal(back, in) {
			t.Errorf("wrap %d: round trip failed: %v", width, err)
		}
	}
}

func TestSniffMIME(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\x89PNG\r\n\x1a\n\x00\x00", "image/png"},
		{"\xff\xd8\xff\xe0", "image/jpeg"},
		{"GIF87a..", "image/gif"},
		{"GIF89a..", "image/gif"},
		{"RIFF\x10\x00\x00\x00WEBPVP8 ", "image/webp"},
		{"RIFF\x10\x00\x00\x00WAVEfmt ", "application/octet-stream"},
		{"%PDF-1.7\n", "application/pdf"},
		{`<svg xmlns="http://www.w3.org/2000/svg"/>`, "image/svg+xml"},
		{"\n  <svg/>", "image/svg+xml"},
		{`<?xml version="1.0"?><svg/>`, "image/svg+xml"},
		{`<?xml version="1.0"?><note/>`, "text/plain;charset=utf-8"},
		{"Hello, hub!\r\n\tПривет 日本語", "text/plain;charset=utf-8"},
		{"", "text/plain;charset=utf-8"},
		{"bell\x07", "application/octet-stream"},
		{"del\x7f", "application/octet-stream"},
		{"c1\u0085", "application/octet-stream"},
		{"\xfb\xff\xbf", "application/octet-stream"},
	}
	for _, c := range cases {
		if got := SniffMIME([]byte(c.in)); got != c.want {
			t.Errorf("SniffMIME(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDataURI(t *testing.T) {
	if got := DataURI([]byte("Hi"), ""); got != "data:text/plain;charset=utf-8;base64,SGk=" {
		t.Errorf("got %q", got)
	}
	if got := DataURI([]byte("Hi"), "application/json"); got != "data:application/json;base64,SGk=" {
		t.Errorf("got %q", got)
	}
	long := bytes.Repeat([]byte{0xfb, 0xff, 0xbf}, 100)
	got := DataURI(long, "")
	const prefix = "data:application/octet-stream;base64,"
	if !strings.HasPrefix(got, prefix) || strings.ContainsAny(got[len(prefix):], "\n-_") {
		t.Errorf("data URI must be standard, padded, unwrapped: %q", got)
	}
	back, err := Decode(got)
	if err != nil || !bytes.Equal(back, long) {
		t.Errorf("data URI round trip failed: %v", err)
	}
}
