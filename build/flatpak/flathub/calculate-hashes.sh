#!/bin/bash
# Calculate SHA256 hashes and sizes for Flathub extra-data manifest
# Usage: ./calculate-hashes.sh v0.1.13

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v0.1.13"
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

# Download and calculate for x86_64
echo "📦 x86_64 tarball..."
if wget -q "${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-x86_64.tar.gz"; then
    X86_64_SHA256=$(sha256sum aerion-${VERSION}-linux-x86_64.tar.gz | awk '{print $1}')
    X86_64_SIZE=$(stat -c%s aerion-${VERSION}-linux-x86_64.tar.gz)
    echo "   URL: ${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-x86_64.tar.gz"
    echo "   SHA256: $X86_64_SHA256"
    echo "   Size: $X86_64_SIZE bytes"
    echo ""
else
    echo "   ❌ ERROR: Could not download x86_64 tarball"
    echo ""
    X86_64_SHA256="ERROR_FILE_NOT_FOUND"
    X86_64_SIZE="0"
fi

# Download and calculate for aarch64
echo "📦 aarch64 tarball..."
if wget -q "${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-aarch64.tar.gz"; then
    AARCH64_SHA256=$(sha256sum aerion-${VERSION}-linux-aarch64.tar.gz | awk '{print $1}')
    AARCH64_SIZE=$(stat -c%s aerion-${VERSION}-linux-aarch64.tar.gz)
    echo "   URL: ${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-aarch64.tar.gz"
    echo "   SHA256: $AARCH64_SHA256"
    echo "   Size: $AARCH64_SIZE bytes"
    echo ""
else
    echo "   ❌ ERROR: Could not download aarch64 tarball"
    echo ""
    AARCH64_SHA256="ERROR_FILE_NOT_FOUND"
    AARCH64_SIZE="0"
fi

# Desktop file
echo "📄 Desktop file..."
if wget -q "${REPO}/raw/${VERSION}/build/linux/aerion.desktop"; then
    DESKTOP_SHA256=$(sha256sum aerion.desktop | awk '{print $1}')
    echo "   URL: ${REPO}/raw/${VERSION}/build/linux/aerion.desktop"
    echo "   SHA256: $DESKTOP_SHA256"
    echo ""
else
    echo "   ❌ ERROR: Could not download desktop file"
    echo ""
    DESKTOP_SHA256="ERROR_FILE_NOT_FOUND"
fi

# Icon
echo "🖼️  Icon..."
if wget -q "${REPO}/raw/${VERSION}/build/appicon.png"; then
    ICON_SHA256=$(sha256sum appicon.png | awk '{print $1}')
    echo "   URL: ${REPO}/raw/${VERSION}/build/appicon.png"
    echo "   SHA256: $ICON_SHA256"
    echo ""
else
    echo "   ❌ ERROR: Could not download icon"
    echo ""
    ICON_SHA256="ERROR_FILE_NOT_FOUND"
fi

# Metainfo
echo "📋 Metainfo..."
if wget -q "${REPO}/raw/${VERSION}/build/flatpak/com.github.hkdb.Aerion.metainfo.xml"; then
    METAINFO_SHA256=$(sha256sum com.github.hkdb.Aerion.metainfo.xml | awk '{print $1}')
    echo "   URL: ${REPO}/raw/${VERSION}/build/flatpak/com.github.hkdb.Aerion.metainfo.xml"
    echo "   SHA256: $METAINFO_SHA256"
    echo ""
else
    echo "   ❌ ERROR: Could not download metainfo"
    echo ""
    METAINFO_SHA256="ERROR_FILE_NOT_FOUND"
fi

# Cleanup
cd - > /dev/null
rm -rf "$TEMP_DIR"

echo "=========================================="
echo "Summary for com.github.hkdb.Aerion-extradata.yml:"
echo "=========================================="
echo ""
echo "x86_64 tarball:"
echo "  url: ${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-x86_64.tar.gz"
echo "  sha256: $X86_64_SHA256"
echo "  size: $X86_64_SIZE"
echo ""
echo "aarch64 tarball:"
echo "  url: ${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-aarch64.tar.gz"
echo "  sha256: $AARCH64_SHA256"
echo "  size: $AARCH64_SIZE"
echo ""
echo "Desktop file:"
echo "  url: ${REPO}/raw/${VERSION}/build/linux/aerion.desktop"
echo "  sha256: $DESKTOP_SHA256"
echo ""
echo "Icon:"
echo "  url: ${REPO}/raw/${VERSION}/build/appicon.png"
echo "  sha256: $ICON_SHA256"
echo ""
echo "Metainfo:"
echo "  url: ${REPO}/raw/${VERSION}/build/flatpak/com.github.hkdb.Aerion.metainfo.xml"
echo "  sha256: $METAINFO_SHA256"
echo ""
echo "=========================================="
echo "Next steps:"
echo "1. Update the URLs in com.github.hkdb.Aerion-extradata.yml to use ${VERSION}"
echo "2. Replace REPLACE_WITH_* placeholders with values above"
echo "3. Test build: flatpak-builder --force-clean build-dir com.github.hkdb.Aerion-extradata.yml"
echo "=========================================="
