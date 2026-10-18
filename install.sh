#!/bin/sh
# Installs dk, the Duck editor, for this machine from the GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/YouSysAdmin/duck-editor/master/install.sh | sh
#
# DUCK_VERSION      release to install, such as 0.1.0, the latest by default
# DUCK_INSTALL_DIR  target directory, ~/.local/bin by default
# DUCK_REPO         GitHub repository, YouSysAdmin/duck-editor by default
# DUCK_BASE_URL     directory holding the archives, overrides the release URL
set -eu

repo=${DUCK_REPO:-YouSysAdmin/duck-editor}
dir=${DUCK_INSTALL_DIR:-$HOME/.local/bin}

if [ -n "${DUCK_BASE_URL:-}" ]; then
	base=$DUCK_BASE_URL
elif [ -n "${DUCK_VERSION:-}" ]; then
	base="https://github.com/$repo/releases/download/v${DUCK_VERSION#v}"
else
	base="https://github.com/$repo/releases/latest/download"
fi

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) echo "dk: unsupported system $(uname -s)" >&2; exit 1 ;;
esac

case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) echo "dk: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

if [ -f /etc/alpine-release ] && ! [ -e /lib/ld-linux-x86-64.so.2 ] && ! [ -e /lib/ld-linux-aarch64.so.1 ]; then
	echo "dk: Alpine needs glibc compatibility first: apk add gcompat" >&2
	exit 1
fi

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q "$1" -O "$2"; }
else
	echo "dk: curl or wget is required" >&2
	exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	sha256() { echo none; }
fi

archive="dk-$os-$arch.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fetch "$base/$archive" "$tmp/$archive"
fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS"

want=$(grep " $archive\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)
got=$(sha256 "$tmp/$archive")
if [ "$got" = none ]; then
	echo "dk: no sha256sum or shasum, checksum not verified" >&2
elif [ -z "$want" ] || [ "$want" != "$got" ]; then
	echo "dk: checksum mismatch for $archive" >&2
	exit 1
fi

mkdir -p "$dir"
tar -xzf "$tmp/$archive" -C "$tmp"
install -m 0755 "$tmp/dk" "$dir/dk"
"$dir/dk" version

case ":$PATH:" in
*":$dir:"*) ;;
*) echo "dk: add $dir to PATH" ;;
esac
