// Command b64 encodes, decodes and validates Base64 from the command line.
//
// Install with: go install github.com/base64hub-com/base64hub-cli/cmd/b64@latest
// Documentation: https://base64hub.com/docs/cli/
package main

import (
	"os"
	"runtime/debug"
	"strings"

	"github.com/base64hub-com/base64hub-cli/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=1.0.0".
var version = ""

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, resolveVersion()))
}

// resolveVersion falls back to the module version recorded by go install.
func resolveVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return strings.TrimPrefix(v, "v")
		}
	}
	return "dev"
}
