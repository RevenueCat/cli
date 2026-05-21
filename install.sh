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
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    -H "Accept: application/vnd.github+json" \
    | grep '"tag_name"' \
    | sed 's/.*"tag_name": *"\(.*\)".*/\1/')"
  if [ -z "$VERSION" ]; then
    echo "Could not determine latest version. Check your internet connection." >&2
    exit 1
  fi
fi

# Strip leading 'v' for the archive filename
VERSION_NUM="${VERSION#v}"

if [ "$GOOS" = "windows" ]; then
  ARCHIVE="${BINARY}_${VERSION_NUM}_${GOOS}_${GOARCH}.zip"
  INSTALL_NAME="rc.exe"
else
  ARCHIVE="${BINARY}_${VERSION_NUM}_${GOOS}_${GOARCH}.tar.gz"
  INSTALL_NAME="rc"
fi

DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"

echo "Installing ${BINARY} ${VERSION} (${GOOS}/${GOARCH})…"

# Create a temp dir
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Download
curl -fsSL "$DOWNLOAD_URL" -o "$TMP/$ARCHIVE"

# Extract
if [ "$GOOS" = "windows" ]; then
  unzip -q "$TMP/$ARCHIVE" -d "$TMP"
  EXTRACTED="$TMP/${BINARY}.exe"
else
  tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
  EXTRACTED="$TMP/${BINARY}"
fi

if [ ! -f "$EXTRACTED" ]; then
  echo "Binary not found after extraction." >&2
  exit 1
fi

chmod +x "$EXTRACTED"

# Install — create dir and move binary, falling back to sudo if needed
DEST="$INSTALL_DIR/$INSTALL_NAME"

install_binary() {
  mkdir -p "$INSTALL_DIR" && mv "$EXTRACTED" "$DEST"
}

if install_binary 2>/dev/null; then
  echo "Installed to $DEST"
else
  echo "Permission denied. Retrying with sudo…"
  sudo sh -c "mkdir -p '$INSTALL_DIR' && mv '$EXTRACTED' '$DEST'"
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
