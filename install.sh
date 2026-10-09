#!/bin/sh
# Build substatus from source and install it into $BIN_DIR (default ~/.local/bin).
#
#   ./install.sh                                   # from a checkout
#   curl -fsSL https://raw.githubusercontent.com/rothzeta/substatus/main/install.sh | sh
#
# Builds with a local Go 1.24+ toolchain, or with Docker when Go is missing.
# Afterwards it offers to save an OpenCode API key (skip with SUBSTATUS_NO_PROMPT=1).
set -eu

REPO_URL=https://github.com/rothzeta/substatus.git
GO_IMAGE=golang:1.24-bookworm
BIN_DIR=${BIN_DIR:-$HOME/.local/bin}

die() {
	echo "install.sh: $*" >&2
	exit 1
}

# Use the checkout this script lives in, or clone one (e.g. when piped from curl).
src=$(cd "$(dirname "$0")" 2>/dev/null && pwd) || src=
if [ -z "$src" ] || ! grep -qs '^module github.com/rothzeta/substatus$' "$src/go.mod"; then
	command -v git >/dev/null 2>&1 || die "git is required to fetch the source"
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	git clone --quiet --depth 1 "$REPO_URL" "$tmp/substatus"
	src=$tmp/substatus
fi

echo "Building substatus from $src"
if command -v go >/dev/null 2>&1; then
	(cd "$src" && CGO_ENABLED=0 go build -trimpath -o substatus ./cmd/substatus)
elif command -v docker >/dev/null 2>&1; then
	echo "Go not found; building in Docker ($GO_IMAGE)"
	docker run --rm -v "$src:/src" -w /src -u "$(id -u):$(id -g)" \
		-e HOME=/tmp -e GOCACHE=/tmp/gocache -e CGO_ENABLED=0 \
		"$GO_IMAGE" go build -trimpath -o substatus ./cmd/substatus
else
	die "Go 1.24+ or Docker is required to build substatus"
fi

mkdir -p "$BIN_DIR"
install -m 0755 "$src/substatus" "$BIN_DIR/substatus"
echo "Installed $("$BIN_DIR/substatus" --version) to $BIN_DIR/substatus"
case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*) echo "Note: $BIN_DIR is not on your PATH; add it to your shell profile." ;;
esac

# Offer to save an OpenCode API key unless one is already configured.
key_file=${XDG_CONFIG_HOME:-$HOME/.config}/substatus/opencode_api_key
[ "$(uname -s)" = Darwin ] && key_file="$HOME/Library/Application Support/substatus/opencode_api_key"
if [ -z "${SUBSTATUS_NO_PROMPT:-}" ] && [ -z "${OPENCODE_API_KEY:-}" ] && [ ! -s "$key_file" ] &&
	(: </dev/tty) 2>/dev/null; then
	printf 'Save an OpenCode API key for OpenCode Go usage? [y/N] ' >/dev/tty
	read -r answer </dev/tty || answer=
	case $answer in
	[yY]*) "$BIN_DIR/substatus" --set-opencode-key </dev/tty ;;
	*) echo "Skipped. Run 'substatus --set-opencode-key' any time." ;;
	esac
fi
