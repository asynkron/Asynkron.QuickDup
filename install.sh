#!/bin/bash
set -e

REPO="asynkron/Asynkron.QuickDup"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
    darwin|linux) ;;
    *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Get latest version
VERSION=$(curl -sI https://github.com/$REPO/releases/latest | grep -i "location:" | sed 's/.*tag\///' | tr -d '\r\n')
if [ -z "$VERSION" ]; then
    echo "Failed to get latest version"
    exit 1
fi

echo "Installing quickdup $VERSION for $OS/$ARCH..."

# Download and extract
URL="https://github.com/$REPO/releases/download/$VERSION/quickdup_${VERSION#v}_${OS}_${ARCH}.tar.gz"
TMP_DIR=$(mktemp -d)
trap "rm -rf $TMP_DIR" EXIT

curl -sL "$URL" | tar xz -C "$TMP_DIR"

# Install
if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP_DIR/quickdup" "$INSTALL_DIR/"
else
    echo "Need sudo to install to $INSTALL_DIR"
    sudo mv "$TMP_DIR/quickdup" "$INSTALL_DIR/"
fi

chmod +x "$INSTALL_DIR/quickdup"

echo "quickdup installed to $INSTALL_DIR/quickdup"
echo "Run 'quickdup --help' to get started"
