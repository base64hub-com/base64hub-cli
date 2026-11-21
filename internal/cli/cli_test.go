package cli

import (
	"bytes"
	"crypto/sha256"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/base64hub-com/base64hub-cli/internal/b64"
)

type result struct {
	code           int
	stdout, stderr string
}

func run(stdin string, args ...string) result {
	var out, errb bytes.Buffer
	code := Run(args, strings.NewReader(stdin), &out, &errb, "1.2.3")
	return result{code, out.String(), errb.String()}
}

func expect(t *testing.T, r result, code int, stdout, stderr string) {
	t.Helper()
	if r.code != code || r.stdout != stdout || r.stderr != stderr {
		t.Errorf("got code %d stdout %q stderr %q\nwant code %d stdout %q stderr %q",
			r.code, r.stdout, r.stderr, code, stdout, stderr)
	}
}

// The examples from the contract and the docs must stay true.
func TestContractExamples(t *testing.T) {
	expect(t, run("", "Hello, hub!"), 0, "SGVsbG8sIGh1YiE=\n", "")
	expect(t, run("", "-d", "SGVsbG8sIGh1YiE="), 0, "Hello, hub!", "")
	expect(t, run("\xfb\xff\xbf", "--url"), 0, "-_-_\n", "")
	expect(t, run("", "--url", "--no-pad", "Привет"), 0, "0J_RgNC40LLQtdGC\n", "")
}

func TestEncodeFlags(t *testing.T) {
	expect(t, run("foobar"), 0, "Zm9vYmFy\n", "")
	expect(t, run(""), 0, "\n", "")
	expect(t, run("", ""), 0, "\n", "")
	expect(t, run("", "f"), 0, "Zg==\n", "")
	expect(t, run("", "-n", "f"), 0, "Zg\n", "")
	expect(t, run("", "--no-pad", "f"), 0, "Zg\n", "")
	expect(t, run("ab?", "-u"), 0, "YWI_\n", "")
	expect(t, run("", "-u", "-n", "fo"), 0, "Zm8\n", "")
	expect(t, run("foobar", "-w", "4"), 0, "Zm9v\nYmFy\n", "")
	expect(t, run("foobar", "--wrap", "3"), 0, "Zm9\nvYm\nFy\n", "")
	expect(t, run("foobar", "--wrap=5"), 0, "Zm9vY\nmFy\n", "")
	expect(t, run("foobar", "-w", "0"), 0, "Zm9vYmFy\n", "")
	expect(t, run("", "-w", "8", "foobar"), 0, "Zm9vYmFy\n", "")
	// TEXT may come before flags; stdin is ignored when TEXT is given.
	expect(t, run("ignored", "f", "--no-pad"), 0, "Zg\n", "")
}

func TestDecodeFlag(t *testing.T) {
	expect(t, run("", "--decode", "Zm9v"), 0, "foo", "")
	expect(t, run("Zm9v\nYmFy\n"), 0, "Wm05dgpZbUZ5Cg==\n", "")
	expect(t, run("Zm9v\nYmFy\n", "-d"), 0, "foobar", "")
	expect(t, run("data:text/plain;base64,SGk=\n", "-d"), 0, "Hi", "")
	expect(t, run("", "-d", "a+b_"), 0, "\x6b\xe6\xff", "")
	expect(t, run("", "-d", ""), 0, "", "")
	expect(t, run("", "-d", "Zm9v!"), 1, "", "b64: invalid character '!' at position 4\n")
	expect(t, run("Zm9vY", "-d"), 1, "", "b64: invalid length: 5 characters leave a single character in the last group\n")
}

func TestValidateFlag(t *testing.T) {
	expect(t, run("", "--validate", "SGVsbG8sIGh1YiE="), 0, "valid (either, 11 bytes)\n", "")
	expect(t, run("-_-_", "--validate"), 0, "valid (url, 3 bytes)\n", "")
	expect(t, run("+/+/\n", "--validate"), 0, "valid (standard, 3 bytes)\n", "")
	expect(t, run("", "--validate", "a+b_"), 0, "valid (mixed, 3 bytes)\n", "")
	expect(t, run("", "--validate"), 0, "valid (either, 0 bytes)\n", "")
	expect(t, run("", "--validate", "Zm9v!"), 1, "", "invalid: invalid character '!' at position 4\n")
	expect(t, run("Zg===", "--validate"), 1, "", "invalid: too much padding: 3 '=' at position 2, at most 2 are allowed\n")
}

func TestDataURIFlags(t *testing.T) {
	expect(t, run("", "--data-uri", "Hi"), 0, "data:text/plain;charset=utf-8;base64,SGk=\n", "")
	expect(t, run("\x89PNG\r\n\x1a\n", "--data-uri"), 0, "data:image/png;base64,iVBORw0KGgo=\n", "")
	expect(t, run("<svg/>", "--data-uri"), 0, "data:image/svg+xml;base64,PHN2Zy8+\n", "")
	expect(t, run("\x00\x01", "--data-uri"), 0, "data:application/octet-stream;base64,AAE=\n", "")
	expect(t, run("{}", "--data-uri", "-m", "application/json"), 0, "data:application/json;base64,e30=\n", "")
	expect(t, run("{}", "--data-uri", "--mime=application/json"), 0, "data:application/json;base64,e30=\n", "")
	// A data URI is never wrapped, even when long.
	long := strings.Repeat("x", 300)
	r := run(long, "--data-uri")
	if r.code != 0 || strings.Count(r.stdout, "\n") != 1 {
		t.Errorf("long data URI: %+v", r)
	}
	// Binary detected by magic bytes is streamed; a large one must round-trip.
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0, 1, 2, 3}, 100000)...)
	r = run(string(png), "--data-uri")
	back, err := b64.Decode(r.stdout)
	if r.code != 0 || err != nil || !bytes.Equal(back, png) || !strings.HasPrefix(r.stdout, "data:image/png;base64,") {
		t.Errorf("png data URI round trip: code %d err %v", r.code, err)
	}
}

func TestHelpVersion(t *testing.T) {
	for _, f := range []string{"-h", "--help"} {
		r := run("", f)
		if r.code != 0 || !strings.HasPrefix(r.stdout, "b64 – Base64") || !strings.Contains(r.stdout, "--data-uri") || r.stderr != "" {
			t.Errorf("%s: %+v", f, r)
		}
	}
	expect(t, run("", "-v"), 0, "b64 1.2.3\n", "")
	expect(t, run("", "--version"), 0, "b64 1.2.3\n", "")
	// Help wins over the rest of the command line.
	if r := run("", "-d", "--help", "-x"); r.code != 0 {
		t.Errorf("help after flags: %+v", r)
	}
}

func TestDoubleDash(t *testing.T) {
	expect(t, run("", "--", "-d"), 0, "LWQ=\n", "")
	expect(t, run("", "-d", "--", "-_-_"), 0, "\xfb\xff\xbf", "")
	expect(t, run("", "--", "--"), 0, "LS0=\n", "")
	expect(t, run("", "-n", "--", "-"), 0, "LQ\n", "")
	expect(t, run("Zm9v", "-d", "-", "--"), 0, "foo", "")
	expect(t, run("", "--", "a", "b"), 2, "", "b64: too many arguments: expected one TEXT, got 2 (quote TEXT that contains spaces)\n")
}

func TestStdinDash(t *testing.T) {
	expect(t, run("foo", "-"), 0, "Zm9v\n", "")
	expect(t, run("foo", "-i", "-"), 0, "Zm9v\n", "")
	expect(t, run("Zm9v", "-d", "--input", "-"), 0, "foo", "")
}

func TestUsageErrors(t *testing.T) {
	cases := [][]string{
		{"-dn", "x"},
		{"-x"},
		{"--nope"},
		{"-w"},
		{"-w", "-1"},
		{"-w", "abc"},
		{"--wrap="},
		{"--decode=yes"},
		{"-i"},
		{"-o"},
		{"-m"},
		{"a", "b"},
		{"-i", "file", "TEXT"},
		{"-d", "--validate"},
		{"-d", "-u", "x"},
		{"-d", "-n", "x"},
		{"-d", "-w", "4", "x"},
		{"-d", "--data-uri", "x"},
		{"--validate", "-u", "x"},
		{"--data-uri", "-u", "x"},
		{"--data-uri", "-n", "x"},
		{"--data-uri", "-w", "76", "x"},
		{"-m", "image/png", "x"},
		{"--data-uri", "-m", "", "x"},
		{"--data-uri", "-m", "text/plain, x", "x"},
	}
	for _, args := range cases {
		r := run("", args...)
		if r.code != 2 || r.stdout != "" || !strings.HasPrefix(r.stderr, "b64: ") || strings.Count(r.stderr, "\n") != 1 {
			t.Errorf("%q: %+v", args, r)
		}
	}
	expect(t, run("", "-dn"), 2, "", "b64: unknown option -dn (short options can't be combined)\n")
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.bin")
	enc := filepath.Join(dir, "out.b64")
	dec := filepath.Join(dir, "back.bin")
	data := []byte("\x00\x01binary\xff payload\n")
	if err := os.WriteFile(in, data, 0o644); err != nil {
		t.Fatal(err)
	}

	expect(t, run("", "-i", in, "-o", enc), 0, "", "")
	if got, _ := os.ReadFile(enc); string(got) != "AAFiaW5hcnn/IHBheWxvYWQK\n" {
		t.Errorf("encoded file = %q", got)
	}
	expect(t, run("", "--decode", "--input", enc, "--output", dec), 0, "", "")
	if got, _ := os.ReadFile(dec); !bytes.Equal(got, data) {
		t.Errorf("decoded file = %q", got)
	}
	expect(t, run("", "--validate", "-i", enc), 0, "valid (standard, 18 bytes)\n", "")
	expect(t, run("", "-o", enc, "Hi"), 0, "", "")
	if got, _ := os.ReadFile(enc); string(got) != "SGk=\n" {
		t.Errorf("-o with TEXT = %q", got)
	}
	expect(t, run("", "-o", "-", "Hi"), 0, "SGk=\n", "")

	// Missing input: I/O error, exit 1.
	r := run("", "-i", filepath.Join(dir, "missing"))
	if r.code != 1 || !strings.HasPrefix(r.stderr, "b64: open ") {
		t.Errorf("missing input: %+v", r)
	}
	// A directory is not an input.
	if r := run("", "-i", dir); r.code != 1 {
		t.Errorf("directory input: %+v", r)
	}
	// Same file for input and output is refused before it gets truncated.
	expect(t, run("", "-i", in, "-o", in), 2, "", "b64: input and output are the same file: "+in+"\n")
	if got, _ := os.ReadFile(in); !bytes.Equal(got, data) {
		t.Error("input was modified")
	}
	// A failed decode leaves no output file behind.
	bad := filepath.Join(dir, "bad.bin")
	expect(t, run("Zm9v!", "-d", "-o", bad), 1, "", "b64: invalid character '!' at position 4\n")
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Errorf("partial output left behind: %v", err)
	}
}

// genReader yields n pseudo-random bytes without holding them in memory.
type genReader struct {
	n   int64
	rng *rand.ChaCha8
}

func (g *genReader) Read(p []byte) (int, error) {
	if g.n <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > g.n {
		p = p[:g.n]
	}
	g.rng.Read(p)
	g.n -= int64(len(p))
	return len(p), nil
}

func newGen(n int64) *genReader { return &genReader{n: n, rng: rand.NewChaCha8([32]byte{7})} }

func TestStreamingRoundTripFiles(t *testing.T) {
	const size = 8<<20 + 5 // not a multiple of 3
	dir := t.TempDir()
	in, enc, dec := filepath.Join(dir, "in"), filepath.Join(dir, "enc"), filepath.Join(dir, "dec")
	data, _ := io.ReadAll(newGen(size))
	if err := os.WriteFile(in, data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{{"-w", "76"}, {"-u", "-n"}, {}} {
		expect(t, run("", append([]string{"-i", in, "-o", enc}, flags...)...), 0, "", "")
		expect(t, run("", "-d", "-i", enc, "-o", dec), 0, "", "")
		got, _ := os.ReadFile(dec)
		if !bytes.Equal(got, data) {
			t.Fatalf("round trip %v differs", flags)
		}
	}
	text, _ := os.ReadFile(enc)
	if string(text) != b64.Encode(data, b64.EncodeOptions{})+"\n" {
		t.Fatal("streamed encode differs from in-memory encode")
	}
}

// Encoding and decoding 64 MiB must not allocate anywhere near 64 MiB.
func TestStreamingConstantMemory(t *testing.T) {
	const size = 64 << 20
	want := sha256.New()
	io.Copy(want, newGen(size))

	pr, pw := io.Pipe()
	go func() {
		var stderr bytes.Buffer
		code := Run([]string{"-w", "76"}, newGen(size), pw, &stderr, "test")
		if code != 0 {
			pw.CloseWithError(io.ErrUnexpectedEOF)
			return
		}
		pw.Close()
	}()
	got := sha256.New()
	var stderr bytes.Buffer

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	code := Run([]string{"-d"}, pr, got, &stderr, "test")
	runtime.ReadMemStats(&after)

	if code != 0 {
		t.Fatalf("decode exit %d: %s", code, stderr.String())
	}
	if !bytes.Equal(got.Sum(nil), want.Sum(nil)) {
		t.Fatal("64 MiB round trip differs")
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 8<<20 {
		t.Errorf("streaming 64 MiB through encode and decode allocated %d bytes", alloc)
	}
}
