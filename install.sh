#!/bin/sh
# ompcraft installer.
#
#   curl -fsSL https://raw.githubusercontent.com/TechDufus/ompcraft/main/install.sh | sh
#
# or, from a clone:  ./install.sh
#
# It puts one program, `ompcraft`, in ~/.local/bin (override with OMPCRAFT_BIN_DIR) and runs
# `ompcraft setup`, which installs the WoW addon, links the Tern plugin and starts the companion.
# Any arguments are passed to `ompcraft setup` (e.g. --game DIR, --yes).
#
# Where the program comes from, first match wins:
#   1. this checkout, built with Go       (running ./install.sh inside a clone)
#   2. the latest GitHub release           (prebuilt; no Go needed)
#   3. `go install …@latest`               (needs Go 1.27+)
set -eu

REPO="TechDufus/ompcraft"
MODULE="github.com/$REPO"
BIN_DIR="${OMPCRAFT_BIN_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
ok() { printf '\342\234\223 %s\n' "$*"; }
die() {
	printf '\342\234\227 %s\n' "$*" >&2
	exit 1
}
have() { command -v "$1" >/dev/null 2>&1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
linux | darwin) ;;
*) die "ompcraft supports Linux and macOS for now (this is $os)." ;;
esac
arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported CPU: $arch" ;;
esac

mkdir -p "$BIN_DIR"
target="$BIN_DIR/ompcraft"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

# 1. A checkout: this script sits next to the module's go.mod.
from_checkout() {
	case "$0" in */install.sh | install.sh) ;; *) return 1 ;; esac
	dir=$(cd "$(dirname "$0")" && pwd)
	[ -f "$dir/go.mod" ] && grep -q "^module $MODULE\$" "$dir/go.mod" || return 1
	have go || die "found the ompcraft source in $dir, but Go is not installed (https://go.dev/dl/)."
	say "Building ompcraft from $dir"
	(cd "$dir" && go build -trimpath -o "$tmp/ompcraft" ./companion/cmd/ompcraft) || die "build failed"
	mv "$tmp/ompcraft" "$target"
}

sha256() {
	if have sha256sum; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

# 2. A prebuilt release, verified against the release's checksums.txt.
from_release() {
	have curl || return 1
	asset="ompcraft_${os}_${arch}.tar.gz"
	base="https://github.com/$REPO/releases/latest/download"
	curl -fsSL "$base/$asset" -o "$tmp/$asset" 2>/dev/null || return 1
	curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null || die "the release has no checksums.txt"
	want=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
	[ -n "$want" ] || die "checksums.txt does not list $asset"
	[ "$(sha256 "$tmp/$asset")" = "$want" ] || die "checksum mismatch for $asset; not installing"
	tar -xzf "$tmp/$asset" -C "$tmp" ompcraft || die "could not unpack $asset"
	chmod 755 "$tmp/ompcraft"
	mv "$tmp/ompcraft" "$target"
	say "Downloaded the latest ompcraft release"
}

# 3. Build the latest source with Go.
from_go() {
	have go || return 1
	say "Building ompcraft with go install"
	GOBIN="$BIN_DIR" go install "$MODULE/companion/cmd/ompcraft@latest" || die "go install failed"
}

from_checkout || from_release || from_go ||
	die "could not get ompcraft: no release for ${os}/${arch} yet and Go is not installed (https://go.dev/dl/)."
ok "Installed $("$target" version) to $target"

case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*) say "Note: $BIN_DIR is not on your PATH. Add it to run ompcraft directly (the service works either way)." ;;
esac

# Setup may ask a question (several WoW installs found); read answers from the terminal even
# when this script itself arrived through a pipe.
if [ -t 0 ]; then
	exec "$target" setup "$@"
elif (: </dev/tty) 2>/dev/null; then
	exec "$target" setup "$@" </dev/tty
else
	exec "$target" setup --yes "$@"
fi
