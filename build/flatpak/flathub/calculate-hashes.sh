#!/bin/bash
# Calculate SHA256 hash for Flathub archive and update manifest
# Usage: ./calculate-hashes.sh v0.1.17

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v0.1.17"
    exit 1
fi

VERSION="$1"
REPO="https://github.com/hkdb/aerion"

echo "=========================================="
echo "Flathub Manifest Hash Calculator"
echo "=========================================="
echo "Version: $VERSION"
echo "Repository: $REPO"
echo ""
echo "Downloading and calculating..."
echo ""

# Create temp directory
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

# Download and calculate for Flathub archive
echo "📦 Flathub archive..."
ARCHIVE_NAME="aerion-${VERSION}-linux-flathub.tar.gz"
if wget -q "${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}"; then
    ARCHIVE_SHA256=$(sha256sum "$ARCHIVE_NAME" | awk '{print $1}')
    echo "   URL: ${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}"
    echo "   SHA256: $ARCHIVE_SHA256"
    echo ""
else
    echo "   ❌ ERROR: Could not download Flathub archive"
    echo ""
    ARCHIVE_SHA256="ERROR_FILE_NOT_FOUND"
fi

# Cleanup
cd - > /dev/null
rm -rf "$TEMP_DIR"

echo "=========================================="
echo "Updating manifest file..."
echo "=========================================="

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
MANIFEST="${SCRIPT_DIR}/io.github.hkdb.Aerion.yml"

if [ ! -f "$MANIFEST" ]; then
    echo "❌ ERROR: Manifest file not found: $MANIFEST"
    exit 1
fi

# Create backup
cp "$MANIFEST" "${MANIFEST}.backup"

# Update archive URL
sed -i "s|url: https://github.com/hkdb/aerion/releases/download/v[0-9.]\+[-a-zA-Z0-9]*/aerion-v[0-9.]\+[-a-zA-Z0-9]*-linux-flathub\.tar\.gz|url: ${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}|" "$MANIFEST"

# Update archive sha256
awk -v sha="$ARCHIVE_SHA256" '
/url:.*flathub\.tar\.gz$/ { found=1; print; next }
found && /sha256:/ {
    match($0, /^[ \t]*/);
    spaces=substr($0, 1, RLENGTH);
    print spaces "sha256: " sha;
    found=0;
    next
}
{ print }
' "$MANIFEST" > "${MANIFEST}.tmp" && mv "${MANIFEST}.tmp" "$MANIFEST"

echo ""
echo "✅ Manifest updated successfully!"
echo "   Backup saved: ${MANIFEST}.backup"
echo ""
echo "=========================================="
echo "Summary:"
echo "=========================================="
echo ""
echo "Flathub archive:"
echo "  url: ${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}"
echo "  sha256: $ARCHIVE_SHA256"
echo ""
echo "=========================================="
echo "Next steps:"
echo "1. ✅ Manifest updated with ${VERSION} hash"
echo "2. Review changes: git diff io.github.hkdb.Aerion.yml"
echo "3. Test build: flatpak-builder --force-clean build-dir io.github.hkdb.Aerion.yml"
echo "=========================================="
