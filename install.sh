#!/usr/bin/env bash
# bmg install script — downloads the latest released binary for your
# OS/arch from GitHub, verifies sha256, and installs to ~/.local/bin/bmg.
#
# Audit before piping. Usage:
#   curl -fsSL https://raw.githubusercontent.com/justinstimatze/be-my-geminis/main/install.sh | bash
#
# Or, if you trust `go install` and have Go on your path:
#   go install github.com/justinstimatze/be-my-geminis/cmd/bmg@latest

set -euo pipefail

REPO="justinstimatze/be-my-geminis"
INSTALL_DIR="${BMG_INSTALL_DIR:-$HOME/.local/bin}"

# Detect platform.
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux|darwin) ;;
  *)
    echo "bmg: unsupported OS '$os' (linux + darwin only)" >&2
    echo "     Windows is not supported — see README.md 'Known limitations'." >&2
    exit 1
    ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *)
    echo "bmg: unsupported arch '$arch' (amd64 + arm64 only)" >&2
    exit 1
    ;;
esac

# Resolve latest tag via the GitHub API (no auth needed for public repos;
# fewer redirects than the /releases/latest HTML page).
echo "bmg: resolving latest release..." >&2
tag="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' \
  | head -n 1 \
  | sed -E 's/.*"tag_name": "([^"]+)".*/\1/')"
if [ -z "$tag" ]; then
  echo "bmg: could not resolve latest release tag (network or API rate limit?)" >&2
  exit 1
fi
version="${tag#v}"

# Download the tarball + checksums file.
asset="bmg_${version}_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/download/${tag}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "bmg: downloading ${asset}..." >&2
curl -fsSL "${base}/${asset}" -o "${tmp}/${asset}"
curl -fsSL "${base}/checksums.txt" -o "${tmp}/checksums.txt"

# Verify sha256. checksums.txt format is `<sha256>  <filename>` per line.
echo "bmg: verifying sha256..." >&2
( cd "$tmp" && grep -E "  ${asset}\$" checksums.txt | sha256sum -c - )

# Install.
mkdir -p "${INSTALL_DIR}"
tar -xzf "${tmp}/${asset}" -C "$tmp"
install -m 0755 "${tmp}/bmg" "${INSTALL_DIR}/bmg"

echo "bmg: installed ${tag} to ${INSTALL_DIR}/bmg" >&2

# Helpful PATH hint if needed.
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo >&2
    echo "bmg: ${INSTALL_DIR} is not on your \$PATH." >&2
    echo "     Add this to your shell rc:" >&2
    echo "         export PATH=\"${INSTALL_DIR}:\$PATH\"" >&2
    ;;
esac

echo >&2
echo "Next: set a Gemini API key + run \`bmg init\`." >&2
echo "See: https://github.com/${REPO}#install" >&2
