package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildBinary compiles the command the way build.sh does, version included.
func buildBinary(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the binary")
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin := filepath.Join(t.TempDir(), "b64")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command(goBin, "build", "-trimpath", "-ldflags", "-s -w -X main.version=9.9.9", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		return -1
	}
	return 0
}

func TestBinary(t *testing.T) {
	bin := buildBinary(t)
	cases := []struct {
		stdin, stdout, stderr string
		code                  int
		args                  []string
	}{
		{"", "SGVsbG8sIGh1YiE=\n", "", 0, []string{"Hello, hub!"}},
		{"", "Hello, hub!", "", 0, []string{"-d", "SGVsbG8sIGh1YiE="}},
		{"\xfb\xff\xbf", "-_-_\n", "", 0, []string{"--url"}},
		{"", "0J_RgNC40LLQtdGC\n", "", 0, []string{"--url", "--no-pad", "Привет"}},
		{"", "b64 9.9.9\n", "", 0, []string{"--version"}},
		{"", "valid (either, 11 bytes)\n", "", 0, []string{"--validate", "SGVsbG8sIGh1YiE"}},
		{"Zm9v!", "", "invalid: invalid character '!' at position 4\n", 1, []string{"--validate"}},
		{"", "", "b64: invalid character '!' at position 4\n", 1, []string{"-d", "Zm9v!"}},
		{"", "", "b64: unknown option -dn (short options can't be combined)\n", 2, []string{"-dn", "x"}},
		{"", "", "b64: option -w needs a value\n", 2, []string{"-w"}},
		{"", "LWQ=\n", "", 0, []string{"--", "-d"}},
	}
	for _, c := range cases {
		cmd := exec.Command(bin, c.args...)
		cmd.Stdin = strings.NewReader(c.stdin)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		code := exitCode(cmd.Run())
		if code != c.code || out.String() != c.stdout || errb.String() != c.stderr {
			t.Errorf("b64 %q: code %d stdout %q stderr %q; want %d %q %q",
				c.args, code, out.String(), errb.String(), c.code, c.stdout, c.stderr)
		}
	}
	if out, err := exec.Command(bin, "--help").Output(); err != nil || !bytes.Contains(out, []byte("Usage: b64 [flags] [TEXT]")) {
		t.Errorf("--help: %v %q", err, out)
	}
}

// Pipes several megabytes through two processes: b64 -w 76 | b64 -d.
func TestBinaryPipeRoundTrip(t *testing.T) {
	bin := buildBinary(t)
	data := make([]byte, 6<<20+1)
	rand.Read(data)

	enc := exec.Command(bin, "-w", "76")
	dec := exec.Command(bin, "-d")
	enc.Stdin = bytes.NewReader(data)
	pipe, err := enc.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	dec.Stdin = pipe
	var out bytes.Buffer
	dec.Stdout = &out
	if err := enc.Start(); err != nil {
		t.Fatal(err)
	}
	if err := dec.Start(); err != nil {
		t.Fatal(err)
	}
	if err := enc.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := dec.Wait(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Fatalf("round trip differs: got %d bytes, want %d", out.Len(), len(data))
	}

	// Every encoded line is at most 76 characters.
	encoded, err := exec.Command(bin, "-w", "76", "-i", writeTemp(t, data[:10000])).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(encoded), "\n"), "\n") {
		if len(line) > 76 {
			t.Fatalf("line of %d characters", len(line))
		}
	}
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
