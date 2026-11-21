// Package cli implements the b64 command line: flag parsing, input and
// output selection, exit codes and error reporting.
package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/base64hub-com/base64hub-cli/internal/b64"
)

// Exit codes.
const (
	ExitOK      = 0
	ExitInvalid = 1 // invalid input or I/O error
	ExitUsage   = 2
)

const help = `b64 – Base64 encode, decode and validate. https://base64hub.com/cli/

Usage: b64 [flags] [TEXT]

Encodes the UTF-8 bytes of TEXT or, without TEXT, standard input to Base64.

Flags:
  -d, --decode        decode Base64 to raw bytes (whitespace, either alphabet,
                      missing padding and a data: URI prefix are accepted)
  -u, --url           encode with the URL-safe alphabet (- and _)
  -n, --no-pad        encode without = padding
  -w, --wrap N        wrap encoded output every N characters (0 = off, 76 = MIME)
      --data-uri      encode as a data: URI, MIME type detected from the content
  -m, --mime TYPE     MIME type for --data-uri
      --validate      check the input and report its variant and decoded size
  -i, --input FILE    read FILE instead of standard input (- = stdin)
  -o, --output FILE   write FILE instead of standard output (- = stdout)
  -h, --help          show this help
  -v, --version       print the version
  --                  end of flags: the next argument is TEXT even if it starts with -

Exit status: 0 success, 1 invalid input or I/O error, 2 usage error.

Examples:
  b64 "Hello, hub!"                 SGVsbG8sIGh1YiE=
  b64 -d SGVsbG8sIGh1YiE=           Hello, hub!
  b64 --url --no-pad "Привет"       0J_RgNC40LLQtdGC
  b64 --validate -i token.txt
  b64 -w 76 -i photo.jpg -o photo.b64
  b64 --data-uri -i logo.png

Docs: https://base64hub.com/docs/cli/
`

type config struct {
	decode, url, noPad, dataURI, validate bool
	wrap                                  int
	mime                                  string
	mimeSet                               bool
	input, output                         string
	text                                  *string
	help, version                         bool
}

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

// parse reads the command line. Flags and TEXT may come in any order; "--"
// ends the flags. Short flags take their value as the next argument and
// cannot be combined; long flags also accept --name=value.
func parse(args []string) (*config, error) {
	c := &config{}
	var positional []string
	stdinDash := false // a bare "-" before "--" means standard input
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if a == "-" || !strings.HasPrefix(a, "-") {
			if a == "-" && len(positional) == 0 {
				stdinDash = true
			}
			positional = append(positional, a)
			continue
		}
		name, val, hasVal := a, "", false
		if strings.HasPrefix(a, "--") {
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				name, val, hasVal = a[:eq], a[eq+1:], true
			}
		}
		flag := func(p *bool) error {
			if hasVal {
				return usagef("option %s takes no value", name)
			}
			*p = true
			return nil
		}
		value := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", usagef("option %s needs a value", name)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "-d", "--decode":
			err = flag(&c.decode)
		case "-u", "--url":
			err = flag(&c.url)
		case "-n", "--no-pad":
			err = flag(&c.noPad)
		case "--data-uri":
			err = flag(&c.dataURI)
		case "--validate":
			err = flag(&c.validate)
		case "-h", "--help":
			err = flag(&c.help)
		case "-v", "--version":
			err = flag(&c.version)
		case "-w", "--wrap":
			var v string
			if v, err = value(); err == nil {
				n, perr := strconv.Atoi(v)
				if perr != nil || n < 0 {
					err = usagef("option %s needs a whole number of 0 or more, got %q", name, v)
				}
				c.wrap = n
			}
		case "-m", "--mime":
			if c.mime, err = value(); err == nil {
				c.mimeSet = true
				if strings.TrimSpace(c.mime) == "" || strings.ContainsAny(c.mime, " \t\r\n,") {
					err = usagef("option %s needs a MIME type like image/png, got %q", name, c.mime)
				}
			}
		case "-i", "--input":
			c.input, err = value()
		case "-o", "--output":
			c.output, err = value()
		default:
			if !strings.HasPrefix(a, "--") && len(a) > 2 {
				return nil, usagef("unknown option %s (short options can't be combined)", a)
			}
			return nil, usagef("unknown option %s", name)
		}
		if err != nil {
			return nil, err
		}
		if c.help || c.version {
			return c, nil
		}
	}

	switch len(positional) {
	case 0:
	case 1:
		if stdinDash {
			if c.input != "" && c.input != "-" {
				return nil, usagef("give either TEXT or --input, not both")
			}
			c.input = "-"
		} else {
			if c.input != "" {
				return nil, usagef("give either TEXT or --input, not both")
			}
			c.text = &positional[0]
		}
	default:
		return nil, usagef("too many arguments: expected one TEXT, got %d (quote TEXT that contains spaces)", len(positional))
	}

	if c.decode && c.validate {
		return nil, usagef("--decode and --validate can't be combined")
	}
	if c.decode || c.validate {
		mode := "--decode"
		if c.validate {
			mode = "--validate"
		}
		for _, f := range []struct {
			set  bool
			name string
		}{{c.url, "--url"}, {c.noPad, "--no-pad"}, {c.wrap > 0, "--wrap"}, {c.dataURI, "--data-uri"}, {c.mimeSet, "--mime"}} {
			if f.set {
				return nil, usagef("%s only applies to encoding, not to %s", f.name, mode)
			}
		}
	}
	if c.dataURI {
		for _, f := range []struct {
			set  bool
			name string
		}{{c.url, "--url"}, {c.noPad, "--no-pad"}, {c.wrap > 0, "--wrap"}} {
			if f.set {
				return nil, usagef("%s can't be combined with --data-uri: data URIs use standard padded Base64 on one line", f.name)
			}
		}
	}
	if c.mimeSet && !c.dataURI {
		return nil, usagef("--mime needs --data-uri")
	}
	return c, nil
}

// Run executes b64 with args (without the program name) and returns the exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	c, err := parse(args)
	if err != nil {
		fmt.Fprintf(stderr, "b64: %v\n", err)
		return ExitUsage
	}
	if c.help {
		io.WriteString(stdout, help)
		return ExitOK
	}
	if c.version {
		fmt.Fprintf(stdout, "b64 %s\n", version)
		return ExitOK
	}

	in, closeIn, err := openInput(c, stdin)
	if err != nil {
		return fail(stderr, err)
	}
	defer closeIn()

	out, finish, err := openOutput(c, stdout)
	if err != nil {
		return fail(stderr, err)
	}
	bw := bufio.NewWriterSize(out, 64<<10)

	err = process(c, in, bw, stderr)
	if err == nil {
		err = bw.Flush()
	}
	if ferr := finish(err != nil); err == nil {
		err = ferr
	}
	if err != nil {
		if errors.Is(err, errInvalidReported) {
			return ExitInvalid
		}
		return fail(stderr, err)
	}
	return ExitOK
}

var errInvalidReported = errors.New("invalid input, already reported")

func process(c *config, in io.Reader, w *bufio.Writer, stderr io.Writer) error {
	switch {
	case c.validate:
		info, err := b64.DecodeStream(io.Discard, in)
		var invalid *b64.Error
		if errors.As(err, &invalid) {
			fmt.Fprintf(stderr, "invalid: %s\n", invalid.Msg)
			return errInvalidReported
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "valid (%s, %d bytes)\n", info.Variant, info.Bytes)
		return err

	case c.decode:
		_, err := b64.DecodeStream(w, in)
		return err

	case c.dataURI:
		br := bufio.NewReaderSize(in, 64<<10)
		var src io.Reader = br
		mime := c.mime
		if mime == "" {
			head, err := br.Peek(b64.SniffLen)
			if err != nil && err != io.EOF {
				return err
			}
			if mime = b64.SniffMagic(head); mime == "" {
				// Text, SVG and binary are told apart by the whole content.
				data, err := io.ReadAll(br)
				if err != nil {
					return err
				}
				mime = b64.SniffMIME(data)
				src = bytes.NewReader(data)
			}
		}
		w.WriteString("data:" + mime + ";base64,")
		return encode(w, src, b64.EncodeOptions{})

	default:
		return encode(w, in, b64.EncodeOptions{URL: c.url, NoPad: c.noPad, Wrap: c.wrap})
	}
}

func encode(w *bufio.Writer, r io.Reader, o b64.EncodeOptions) error {
	enc := b64.NewEncoder(w, o)
	if _, err := io.Copy(enc, r); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return w.WriteByte('\n')
}

func openInput(c *config, stdin io.Reader) (io.Reader, func(), error) {
	switch {
	case c.text != nil:
		return strings.NewReader(*c.text), func() {}, nil
	case c.input == "" || c.input == "-":
		return stdin, func() {}, nil
	}
	f, err := os.Open(c.input)
	if err != nil {
		return nil, nil, err
	}
	if st, err := f.Stat(); err == nil && st.IsDir() {
		f.Close()
		return nil, nil, fmt.Errorf("%s is a directory", c.input)
	}
	return f, func() { f.Close() }, nil
}

// openOutput opens the -o file. finish closes it and, when the run failed,
// removes a regular file so no partial output is left behind.
func openOutput(c *config, stdout io.Writer) (io.Writer, func(failed bool) error, error) {
	if c.output == "" || c.output == "-" {
		return stdout, func(bool) error { return nil }, nil
	}
	ost, err := os.Stat(c.output)
	if err == nil && c.input != "" && c.input != "-" {
		if ist, err := os.Stat(c.input); err == nil && os.SameFile(ist, ost) {
			return nil, nil, &usageError{msg: "input and output are the same file: " + c.output}
		}
	}
	removable := errors.Is(err, os.ErrNotExist) || err == nil && ost.Mode().IsRegular()
	f, err := os.Create(c.output)
	if err != nil {
		return nil, nil, err
	}
	return f, func(failed bool) error {
		cerr := f.Close()
		if failed && removable {
			os.Remove(c.output)
		}
		return cerr
	}, nil
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "b64: %v\n", err)
	var u *usageError
	if errors.As(err, &u) {
		return ExitUsage
	}
	return ExitInvalid
}
