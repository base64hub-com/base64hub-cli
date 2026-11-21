# b64

`b64` is a small command-line tool to encode, decode and validate Base64. It handles the standard
and URL-safe alphabets, optional padding, line wrapping and data URIs, decodes leniently, and
streams input of any size in constant memory. One static binary, no dependencies.

Website: [base64hub.com/cli](https://base64hub.com/cli/) · Documentation:
[base64hub.com/docs/cli](https://base64hub.com/docs/cli/)

```
$ b64 "Hello, hub!"
SGVsbG8sIGh1YiE=
$ b64 -d SGVsbG8sIGh1YiE=
Hello, hub!
$ printf '\xfb\xff\xbf' | b64 --url
-_-_
$ b64 --url --no-pad "Привет"
0J_RgNC40LLQtdGC
```

## Install

| Platform | Command |
|---|---|
| macOS, Linux (Homebrew) | `brew install base64hub-com/tap/b64` |
| Windows (Scoop) | `scoop bucket add base64hub https://github.com/base64hub-com/homebrew-tap` then `scoop install b64` |
| Windows (winget) | `winget install base64hub.b64` |
| Arch Linux (AUR) | `yay -S b64-bin` |
| macOS, Linux (script) | `curl -fsSL https://base64hub.com/install.sh \| sh` |
| Go 1.24+ | `go install github.com/base64hub-com/base64hub-cli/cmd/b64@latest` |
| Node.js | `npm i -g base64hub` |
| Python | `pipx install base64hub` |

The npm and PyPI packages ship the same `b64` command, written in JavaScript and Python.

Debian and Ubuntu: download `b64_linux_amd64.deb` or `b64_linux_arm64.deb` from the
[latest release](https://github.com/base64hub-com/base64hub-cli/releases/latest) and run
`sudo apt install ./b64_linux_amd64.deb`.

Manual install: every [release](https://github.com/base64hub-com/base64hub-cli/releases) has an archive per
platform (`b64_<os>_<arch>.tar.gz`, `.zip` for Windows) and a `SHA256SUMS` file. Check the archive with
`sha256sum --check --ignore-missing SHA256SUMS`, unpack it and put `b64` somewhere on your `PATH`.

The install script downloads the archive for your system from the latest release, verifies it against
`SHA256SUMS` and installs `b64` to `/usr/local/bin` (or `~/.local/bin` without root access). Set
`B64_VERSION=1.0.0` to pin a version.

## Usage

```
b64 [flags] [TEXT]
```

With TEXT, `b64` works on the UTF-8 bytes of TEXT (no newline added). Without TEXT it reads standard
input to the end, or the file given with `-i`. Encoding prints the result and one newline; decoding
writes the raw bytes and nothing else.

| Flag | Meaning |
|---|---|
| `-d`, `--decode` | Decode Base64 to bytes |
| `-u`, `--url` | Encode with the URL-safe alphabet (`-` and `_`) |
| `-n`, `--no-pad` | Encode without `=` padding |
| `-w N`, `--wrap N` | Wrap encoded output every N characters (`0` = off, `76` = MIME) |
| `--data-uri` | Output a data URI, `data:<mime>;base64,...` |
| `-m TYPE`, `--mime TYPE` | MIME type for `--data-uri` (detected from the content otherwise) |
| `--validate` | Check the input: prints `valid (<variant>, <bytes> bytes)` or `invalid: <reason>` |
| `-i FILE`, `--input FILE` | Read FILE (`-` = standard input) |
| `-o FILE`, `--output FILE` | Write FILE (`-` = standard output) |
| `-h`, `--help` | Show help |
| `-v`, `--version` | Print `b64 <version>` |

Short flags can't be combined (`-dn` is an error) and take their value as the next argument
(`-w 76`); long flags also accept `--wrap=76`. `--` ends the flags, so `b64 -- -d` encodes the text
`-d`. Encoding flags (`-u`, `-n`, `-w`, `--data-uri`, `-m`) can't be used with `-d` or `--validate`.

### Examples

```sh
# Encode a file as MIME (76-character lines) and decode it back
b64 -w 76 -i photo.jpg -o photo.b64
b64 -d -i photo.b64 -o photo.jpg

# URL-safe token without padding
head -c 32 /dev/urandom | b64 -u -n

# Decode a JWT payload (URL-safe, unpadded)
b64 -d eyJzdWIiOiIxMjM0NTY3ODkwIn0

# Embed an image in HTML or CSS
b64 --data-uri -i logo.png

# Check a value before using it
b64 --validate "SGVsbG8sIGh1YiE"
# valid (either, 11 bytes)
```

### Decoding rules

Decoding is lenient on purpose:

1. Surrounding whitespace is trimmed. Input that starts with `data:` (any case) and contains
   `;base64,` loses everything up to and including `;base64,`.
2. Spaces, tabs, CR and LF are ignored anywhere.
3. Up to two trailing `=` are optional. More than two, or `=` anywhere else, is an error.
4. Both alphabets are accepted, even mixed in one string.
5. A length that leaves a single character in the last group is an error. Non-zero trailing bits are
   accepted.

Errors name the character and its 0-based position in the cleaned input (whitespace and data URI
prefix removed), for example `b64: invalid character '!' at position 4`.

`--validate` also reports the variant: `standard` (contains `+` or `/`), `url` (contains `-` or `_`),
`mixed` (both) or `either` (neither, valid in both alphabets).

### Data URIs

`--data-uri` uses the standard alphabet with padding and never wraps. Without `-m`, the MIME type is
detected: PNG, JPEG, GIF, WebP, PDF and SVG by their content, otherwise `text/plain;charset=utf-8`
for UTF-8 text and `application/octet-stream` for anything else. Detecting text needs the whole
input in memory; pass `-m` to stream large files.

### Exit status

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Invalid input or an I/O error |
| 2 | Usage error |

Errors go to standard error as `b64: <message>`. When writing to a file with `-o` fails, the partial
file is removed.

## Build from source

Go 1.24 or newer, no third-party modules.

```sh
go test ./...
./build.sh 1.0.0
```

`build.sh` writes the release archives, Debian packages and `SHA256SUMS` for macOS, Linux and
Windows on amd64 and arm64 to `dist/`.

## License

MIT, see [LICENSE](LICENSE).
