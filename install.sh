#!/bin/sh
# Install the latest rc (RevenueCat CLI) release from GitHub.
# No Homebrew or Xcode Command Line Tools required.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/RevenueCat/revenuecat-cli/main/install.sh | sh
#   curl -fsSL ... | sh -s -- --install-dir ~/.local/bin

set -e

REPO="RevenueCat/revenuecat-cli"
BINARY="rc"
INSTALL_DIR="/usr/local/bin"

# Parse arguments
while [ $# -gt 0 ]; do
  case "$1" in
    --install-dir)
      if [ -z "${2:-}" ]; then
        echo "Error: --install-dir requires a value" >&2
        exit 1
      fi
      INSTALL_DIR="$2"
      shift 2
      ;;
    --install-dir=*)
      INSTALL_DIR="${1#*=}"
      shift
      ;;
    --version)
      if [ -z "${2:-}" ]; then
        echo "Error: --version requires a value" >&2
        exit 1
      fi
      PINNED_VERSION="$2"
      shift 2
      ;;
    --version=*)
      PINNED_VERSION="${1#*=}"
      shift
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

# Detect OS and arch
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin)  GOOS="darwin" ;;
  Linux)   GOOS="linux" ;;
  MINGW*|MSYS*|CYGWIN*) GOOS="windows" ;;
  *)
    echo "Unsupported OS: $OS" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64) GOARCH="amd64" ;;
  arm64|aarch64) GOARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

# Resolve version — always normalize to include the 'v' prefix for the tag
if [ -n "${PINNED_VERSION:-}" ]; then
  case "$PINNED_VERSION" in
    v*) VERSION="$PINNED_VERSION" ;;
    *)  VERSION="v$PINNED_VERSION" ;;
  esac
else
  echo "Fetching latest version…"
  API_RESPONSE="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    -H "Accept: application/vnd.github+json")"

  # Use jq if available; fall back to a tightly anchored grep/sed
  if command -v jq >/dev/null 2>&1; then
    VERSION="$(printf '%s' "$API_RESPONSE" | jq -r '.tag_name')"
  else
    VERSION="$(printf '%s' "$API_RESPONSE" \
      | grep -o '"tag_name": *"[^"]*"' \
      | head -1 \
      | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
  fi

  if [ -z "$VERSION" ] || [ "$VERSION" = "null" ]; then
    echo "Could not determine latest version. Check your internet connection." >&2
    exit 1
  fi
fi

# Strip leading 'v' for the archive filename
VERSION_NUM="${VERSION#v}"

if [ "$GOOS" = "windows" ]; then
  ARCHIVE="${BINARY}_${VERSION_NUM}_${GOOS}_${GOARCH}.zip"
  BINARY_IN_ARCHIVE="${BINARY}.exe"
  INSTALL_NAME="rc.exe"
else
  ARCHIVE="${BINARY}_${VERSION_NUM}_${GOOS}_${GOARCH}.tar.gz"
  BINARY_IN_ARCHIVE="${BINARY}"
  INSTALL_NAME="rc"
fi

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"

echo "Installing ${BINARY} ${VERSION} (${GOOS}/${GOARCH})…"

# Create a temp dir
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Download
curl -fsSL "$DOWNLOAD_URL" -o "$TMP/$ARCHIVE"

# Extract only the rc binary — don't unpack the whole archive
EXTRACTED="$TMP/$INSTALL_NAME"
if [ "$GOOS" = "windows" ]; then
  unzip -q -j "$TMP/$ARCHIVE" "$BINARY_IN_ARCHIVE" -d "$TMP"
else
  tar -xzf "$TMP/$ARCHIVE" -C "$TMP" "$BINARY_IN_ARCHIVE"
fi

if [ ! -f "$EXTRACTED" ]; then
  echo "Binary not found after extraction." >&2
  exit 1
fi

chmod +x "$EXTRACTED"

DEST="$INSTALL_DIR/$INSTALL_NAME"

# Install — mkdir and mv in one function so sudo covers both operations.
# sudo is invoked with explicit arguments (not via sh -c) to avoid
# shell-injection from user-supplied --install-dir values.
install_binary() {
  mkdir -p "$INSTALL_DIR" && mv "$EXTRACTED" "$DEST"
}

if install_binary 2>/dev/null; then
  echo "Installed to $DEST"
else
  echo "Permission denied. Retrying with sudo…"
  sudo mkdir -p -- "$INSTALL_DIR"
  sudo mv -- "$EXTRACTED" "$DEST"
  echo "Installed to $DEST"
fi

# Verify using the installed path directly, not whatever is first on PATH
if [ -x "$DEST" ]; then
  "$DEST" --version
else
  echo ""
  echo "Installed, but $INSTALL_DIR is not on your PATH. Add it:"
  echo "  export PATH=\"\$PATH:$INSTALL_DIR\""
fi
