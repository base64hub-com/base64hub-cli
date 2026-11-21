#!/usr/bin/env bash
# Build the release assets of b64 into dist/.
#
#   ./build.sh [version]
#
# The version defaults to `git describe`. Produces the archives, .deb
# packages and SHA256SUMS that are attached to a GitHub release of
# base64hub-com/base64hub-cli. Needs go, tar, gzip, zip, dpkg-deb and sha256sum.
set -euo pipefail
cd "$(dirname "$0")"

version="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
version="${version#v}"

# Debian versions must start with a digit; untagged builds get a 0.0.0~ prefix.
deb_version="$version"
case "$deb_version" in [0-9]*) ;; *) deb_version="0.0.0~$deb_version" ;; esac

# Fixed timestamps make the archives reproducible for a given commit.
export SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || date +%s)}"

dist="$PWD/dist"
rm -rf "$dist"
mkdir -p "$dist"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

targets="darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64"

for target in $targets; do
	goos="${target%/*}"
	goarch="${target#*/}"
	name="b64_${goos}_${goarch}"
	exe="b64"
	[ "$goos" = windows ] && exe="b64.exe"

	stage="$work/$name"
	mkdir -p "$stage"
	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
		-ldflags "-s -w -X main.version=$version" -o "$stage/$exe" ./cmd/b64
	cp LICENSE README.md "$stage/"
	chmod 0755 "$stage/$exe"
	chmod 0644 "$stage/LICENSE" "$stage/README.md"
	touch -d "@$SOURCE_DATE_EPOCH" "$stage"/*

	if [ "$goos" = windows ]; then
		(cd "$stage" && zip -q -X -9 "$dist/$name.zip" "$exe" LICENSE README.md)
	else
		tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$SOURCE_DATE_EPOCH" \
			-C "$stage" -cf - "$exe" LICENSE README.md | gzip -9 -n >"$dist/$name.tar.gz"
	fi

	if [ "$goos" = linux ]; then
		root="$work/deb_$goarch"
		install -d -m 0755 "$root/DEBIAN" "$root/usr/bin" "$root/usr/share/doc/b64"
		install -m 0755 "$stage/b64" "$root/usr/bin/b64"
		{
			echo "Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/"
			echo "Upstream-Name: b64"
			echo "Upstream-Contact: base64hub <g@base64hub.com>"
			echo "Source: https://github.com/base64hub-com/base64hub-cli"
			echo
			echo "Files: *"
			echo "Copyright: 2026 base64hub"
			echo "License: Expat"
			sed -e 's/^$/./' -e 's/^/ /' LICENSE
		} >"$root/usr/share/doc/b64/copyright"
		chmod 0644 "$root/usr/share/doc/b64/copyright"
		cat >"$root/DEBIAN/control" <<CONTROL
Package: b64
Version: $deb_version
Architecture: $goarch
Maintainer: base64hub <g@base64hub.com>
Installed-Size: $(du -sk "$root/usr" | cut -f1)
Section: utils
Priority: optional
Homepage: https://base64hub.com/cli/
Description: Base64 encode, decode and validate from the command line
 b64 encodes and decodes Base64 with the standard or URL-safe alphabet,
 validates input, wraps lines and builds data URIs. Decoding is lenient:
 whitespace, missing padding and a data: URI prefix are accepted.
 Input of any size is streamed in constant memory.
CONTROL
		chmod 0644 "$root/DEBIAN/control"
		find "$root" -exec touch -h -d "@$SOURCE_DATE_EPOCH" {} +
		dpkg-deb --root-owner-group -Zxz --build "$root" "$dist/b64_linux_$goarch.deb" >/dev/null
	fi
done

(cd "$dist" && sha256sum b64_* >SHA256SUMS)
echo "b64 $version:"
(cd "$dist" && ls -l)
