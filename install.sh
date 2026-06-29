#!/bin/sh
# Rootless installer for Jenking.
#
# Installs the latest release binary into a user-owned directory (no sudo),
# so the built-in self-updater can replace it later without elevated rights.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Breina/Jenking/master/install.sh | sh
#
# Override the install directory with JENKING_INSTALL_DIR.
set -eu

repo="Breina/Jenking"
bin="jenking"

# --- install directory (user-owned, rootless) -------------------------------
# Honour an explicit override, else XDG_BIN_HOME, else the conventional
# ~/.local/bin which most distros already add to PATH.
install_dir="${JENKING_INSTALL_DIR:-${XDG_BIN_HOME:-$HOME/.local/bin}}"

# --- detect OS/arch ---------------------------------------------------------
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch="amd64" ;;
	aarch64 | arm64) arch="arm64" ;;
	*)
		echo "jenking: unsupported architecture: $arch" >&2
		echo "See https://github.com/$repo/releases for available builds." >&2
		exit 1
		;;
esac
case "$os" in
	linux | darwin) ;;
	*)
		echo "jenking: unsupported OS: $os (this script handles linux and macOS)" >&2
		echo "See https://github.com/$repo/releases for available builds." >&2
		exit 1
		;;
esac

asset="${bin}_${os}_${arch}.tar.gz"
url="https://github.com/$repo/releases/latest/download/$asset"

# --- download and extract ---------------------------------------------------
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $asset ..."
if ! curl -fSL --proto '=https' -o "$tmp/$asset" "$url"; then
	echo "jenking: download failed: $url" >&2
	exit 1
fi

tar -xf "$tmp/$asset" -C "$tmp" "$bin"

# --- install ----------------------------------------------------------------
mkdir -p "$install_dir"
chmod u+x "$tmp/$bin"
mv "$tmp/$bin" "$install_dir/$bin"

echo "Installed $bin to $install_dir/$bin"

# --- PATH check -------------------------------------------------------------
case ":$PATH:" in
	*":$install_dir:"*) ;;
	*)
		echo
		echo "NOTE: $install_dir is not on your PATH."
		echo "Add this line to your shell profile (~/.profile, ~/.bashrc or ~/.zshrc):"
		echo
		echo "    export PATH=\"$install_dir:\$PATH\""
		echo
		;;
esac

echo "Run with: $bin"
