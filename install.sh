#!/bin/sh
# gowasm installer — downloads the release binary for this platform.
#
#   curl -fsSL https://raw.githubusercontent.com/paulgrammer/gowasm/main/install.sh | sh
#
# Options via environment variables:
#   GOWASM_VERSION  release tag to install (default: latest, e.g. v0.1.0)
#   GOWASM_INSTALL  install directory (default: /usr/local/bin, falling back to
#                   ~/.local/bin when /usr/local/bin is not writable)
#
# While the repository is private the release assets are not publicly
# downloadable. The script detects that and falls back to `gh release download`,
# which uses your existing GitHub CLI login.
#
# Installing via curl and tar deliberately avoids the com.apple.quarantine
# attribute that browsers set on downloads, so macOS Gatekeeper does not flag
# the binary even when the release is unsigned.
set -eu

REPO="paulgrammer/gowasm"
BIN="gowasm"

fail() {
	echo "error: $1" >&2
	exit 1
}

for dep in curl tar; do
	command -v "$dep" >/dev/null 2>&1 || fail "$dep is required but not installed"
done

os=$(uname -s)
case "$os" in
Darwin) os="darwin" ;;
Linux) os="linux" ;;
*) fail "unsupported OS: $os (use the .zip release asset on Windows)" ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch="amd64" ;;
arm64 | aarch64) arch="arm64" ;;
*) fail "unsupported architecture: $arch" ;;
esac

version="${GOWASM_VERSION:-}"
if [ -z "$version" ]; then
	version=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null |
		grep '"tag_name"' | head -n1 | cut -d'"' -f4 || true)
fi
if [ -z "$version" ] && command -v gh >/dev/null 2>&1; then
	# A private repository returns 404 to the unauthenticated API.
	version=$(gh release view --repo "$REPO" --json tagName --jq .tagName 2>/dev/null || true)
fi
if [ -z "$version" ]; then
	fail "could not determine the latest release of $REPO
  If the repository is private, install the GitHub CLI and run 'gh auth login',
  or set GOWASM_VERSION to a known tag."
fi

asset="$BIN-$version-$os-$arch.tar.gz"
base_url="https://github.com/$REPO/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "→ downloading $asset ($version)"
if curl -fsSL -o "$tmp/$asset" "$base_url/$asset" 2>/dev/null &&
	curl -fsSL -o "$tmp/checksums.txt" "$base_url/checksums.txt" 2>/dev/null; then
	:
elif command -v gh >/dev/null 2>&1; then
	echo "  public download failed; retrying with the GitHub CLI"
	gh release download "$version" --repo "$REPO" \
		--pattern "$asset" --pattern "checksums.txt" --dir "$tmp" --clobber ||
		fail "could not download $asset from $REPO"
else
	fail "could not download $asset
  If the repository is private, install the GitHub CLI and run 'gh auth login'."
fi

echo "→ verifying checksum"
expected=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
[ -n "$expected" ] || fail "$asset not found in checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
else
	actual=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
fi
if [ "$expected" != "$actual" ]; then
	fail "checksum mismatch for $asset
  expected: $expected
  actual:   $actual"
fi

tar -xzf "$tmp/$asset" -C "$tmp"
[ -f "$tmp/$BIN" ] || fail "$asset did not contain $BIN"

install_dir="${GOWASM_INSTALL:-/usr/local/bin}"
if [ ! -w "$install_dir" ] && [ -z "${GOWASM_INSTALL:-}" ]; then
	install_dir="$HOME/.local/bin"
fi
mkdir -p "$install_dir"

echo "→ installing to $install_dir/$BIN"
mv "$tmp/$BIN" "$install_dir/$BIN"
chmod 0755 "$install_dir/$BIN"

case ":$PATH:" in
*":$install_dir:"*) ;;
*) echo "note: $install_dir is not on your PATH" ;;
esac

# A different copy earlier on PATH would silently win, which is confusing when
# the version reported is not the one just installed.
found=$(command -v "$BIN" 2>/dev/null || true)
if [ -n "$found" ] && [ "$found" != "$install_dir/$BIN" ]; then
	echo "note: $found comes earlier on your PATH and will be used instead"
fi

echo "✓ installed $("$install_dir/$BIN" -version)"

# gowasm drives the Go and Node toolchains rather than bundling them, so say so
# now instead of letting the first build fail.
missing=""
command -v go >/dev/null 2>&1 || missing="$missing go"
command -v node >/dev/null 2>&1 || missing="$missing node"
if [ -n "$missing" ]; then
	echo
	echo "gowasm needs these on your PATH and did not find them:$missing"
	echo "  Go 1.24+ to compile to WebAssembly, Node 20+ to build and test the package."
	echo "  Run 'gowasm doctor' once they are installed."
fi
