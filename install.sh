#!/bin/sh
# Install or update substatus from its GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/rothzeta/substatus/main/install.sh | sh
#
# Re-run it to update: it does nothing when the installed version is current.
#
# Environment:
#   BIN_DIR              install directory (default ~/.local/bin)
#   SUBSTATUS_VERSION    release tag to install, e.g. v0.1.0 (default: latest)
#   SUBSTATUS_NO_PROMPT  set to skip the OpenCode API key prompt
#
# The whole script is a function called on the last line, so a truncated
# download runs nothing.
set -eu

die() {
	echo "install.sh: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		die "sha256sum or shasum is required"
	fi
}

cleanup() {
	[ -z "$tmp" ] || rm -rf "$tmp"
	[ -z "$bin" ] || rm -f "$bin.new"
}

main() {
	repo=rothzeta/substatus
	bin_dir=${BIN_DIR:-$HOME/.local/bin}
	bin=$bin_dir/substatus
	need curl
	need tar
	tmp=
	trap cleanup EXIT
	trap 'cleanup; exit 130' INT
	trap 'cleanup; exit 143' TERM
	trap 'cleanup; exit 129' HUP

	case $(uname -s) in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die "unsupported OS: $(uname -s) (build from source instead)" ;;
	esac
	case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported architecture: $(uname -m) (build from source instead)" ;;
	esac

	version=${SUBSTATUS_VERSION:-}
	if [ -z "$version" ]; then
		# /releases/latest redirects to /releases/tag/<version>.
		url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
			die "could not reach GitHub"
		version=${url##*/}
		case $version in
		v*) ;;
		*) die "no release found for $repo" ;;
		esac
	fi

	installed=
	if [ -x "$bin" ]; then
		installed=$("$bin" --version 2>/dev/null) || installed=
		installed=${installed#substatus }
	fi

	if [ "$installed" = "$version" ]; then
		echo "substatus $version is already installed at $bin"
	else
		asset=substatus_${os}_${arch}.tar.gz
		base=https://github.com/$repo/releases/download/$version
		tmp=$(mktemp -d)
		echo "Downloading substatus $version ($os/$arch)"
		curl -fsSL -o "$tmp/$asset" "$base/$asset" || die "could not download $base/$asset"
		curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || die "could not download checksums"
		expected=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
		if [ -z "$expected" ] || [ "$expected" != "$(sha256 "$tmp/$asset")" ]; then
			die "checksum mismatch for $asset"
		fi
		tar -xzf "$tmp/$asset" -C "$tmp" substatus
		mkdir -p "$bin_dir"
		# Replace atomically so a running substatus keeps working.
		install -m 0755 "$tmp/substatus" "$bin.new"
		mv -f "$bin.new" "$bin"
		if [ -n "$installed" ]; then
			echo "Updated substatus $installed -> $version at $bin"
		else
			echo "Installed substatus $version at $bin"
		fi
	fi

	if [ -z "$installed" ]; then
		case ":$PATH:" in
		*":$bin_dir:"*) ;;
		*) echo "Note: $bin_dir is not on your PATH; add it to your shell profile." ;;
		esac
	fi
	other=$(command -v substatus 2>/dev/null) || other=
	if [ -n "$other" ] && [ "$other" != "$bin" ]; then
		echo "Note: $other earlier on your PATH shadows $bin."
	fi

	# On first install, offer to save an OpenCode API key.
	if [ -z "$installed" ] && [ -z "${SUBSTATUS_NO_PROMPT:-}" ] && [ -z "${OPENCODE_API_KEY:-}" ] &&
		(: </dev/tty) 2>/dev/null; then
		printf 'Save an OpenCode API key for OpenCode Go usage? [y/N] ' >/dev/tty
		read -r answer </dev/tty || answer=
		case $answer in
		[yY]*) "$bin" --set-opencode-key </dev/tty || echo "Key not saved. Run 'substatus --set-opencode-key' any time." ;;
		*) echo "Skipped. Run 'substatus --set-opencode-key' any time." ;;
		esac
	fi
}

main "$@"
