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
echo "📦 x86_64 binary..."
if wget -q "${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-x86_64"; then
    X86_64_SHA256=$(sha256sum aerion-${VERSION}-linux-x86_64 | awk '{print $1}')
    X86_64_SIZE=$(stat -c%s aerion-${VERSION}-linux-x86_64)
    echo "   URL: ${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-x86_64"
    echo "   SHA256: $X86_64_SHA256"
    echo "   Size: $X86_64_SIZE bytes"
    echo ""
else
    echo "   ❌ ERROR: Could not download x86_64 binary"
    echo ""
    X86_64_SHA256="ERROR_FILE_NOT_FOUND"
    X86_64_SIZE="0"
fi

# Download and calculate for aarch64
echo "📦 aarch64 binary..."
if wget -q "${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-aarch64"; then
    AARCH64_SHA256=$(sha256sum aerion-${VERSION}-linux-aarch64 | awk '{print $1}')
    AARCH64_SIZE=$(stat -c%s aerion-${VERSION}-linux-aarch64)
    echo "   URL: ${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-aarch64"
    echo "   SHA256: $AARCH64_SHA256"
    echo "   Size: $AARCH64_SIZE bytes"
    echo ""
else
    echo "   ❌ ERROR: Could not download aarch64 binary"
    echo ""
    AARCH64_SHA256="ERROR_FILE_NOT_FOUND"
    AARCH64_SIZE="0"
fi

# Download and calculate for desktop file
echo "📄 Desktop file..."
if wget -q "${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.desktop"; then
    DESKTOP_SHA256=$(sha256sum io.github.hkdb.Aerion.desktop | awk '{print $1}')
    echo "   URL: ${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.desktop"
    echo "   SHA256: $DESKTOP_SHA256"
    echo ""
else
    echo "   ❌ ERROR: Could not download desktop file"
    echo ""
    DESKTOP_SHA256="ERROR_FILE_NOT_FOUND"
fi

# Download and calculate for icon
echo "🖼️  Icon..."
if wget -q "${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.png"; then
    ICON_SHA256=$(sha256sum io.github.hkdb.Aerion.png | awk '{print $1}')
    echo "   URL: ${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.png"
    echo "   SHA256: $ICON_SHA256"
    echo ""
else
    echo "   ❌ ERROR: Could not download icon"
    echo ""
    ICON_SHA256="ERROR_FILE_NOT_FOUND"
fi

# Download and calculate for metainfo
echo "📋 Metainfo..."
if wget -q "${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.metainfo.xml"; then
    METAINFO_SHA256=$(sha256sum io.github.hkdb.Aerion.metainfo.xml | awk '{print $1}')
    echo "   URL: ${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.metainfo.xml"
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

# Update x86_64 URL
sed -i "s|url: https://github.com/hkdb/aerion/releases/download/v[0-9.]\+[-a-zA-Z0-9]*/aerion-v[0-9.]\+[-a-zA-Z0-9]*-linux-x86_64|url: ${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-x86_64|" "$MANIFEST"

# Update x86_64 sha256 (find the sha256 line after x86_64 URL)
awk -v sha="$X86_64_SHA256" '
/url:.*x86_64$/ { found_x86=1; print; next }
found_x86 && /sha256:/ {
    match($0, /^[ \t]*/);
    spaces=substr($0, 1, RLENGTH);
    print spaces "sha256: " sha;
    found_x86=0;
    next
}
{ print }
' "$MANIFEST" > "${MANIFEST}.tmp" && mv "${MANIFEST}.tmp" "$MANIFEST"

# Update aarch64 URL
sed -i "s|url: https://github.com/hkdb/aerion/releases/download/v[0-9.]\+[-a-zA-Z0-9]*/aerion-v[0-9.]\+[-a-zA-Z0-9]*-linux-aarch64|url: ${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-aarch64|" "$MANIFEST"

# Update aarch64 sha256
awk -v sha="$AARCH64_SHA256" '
/url:.*aarch64$/ { found_arm=1; print; next }
found_arm && /sha256:/ {
    match($0, /^[ \t]*/);
    spaces=substr($0, 1, RLENGTH);
    print spaces "sha256: " sha;
    found_arm=0;
    next
}
{ print }
' "$MANIFEST" > "${MANIFEST}.tmp" && mv "${MANIFEST}.tmp" "$MANIFEST"

# Update desktop file URL and SHA256
sed -i "s|url: https://github.com/hkdb/aerion/releases/download/v[0-9.]\+[-a-zA-Z0-9]*/io.github.hkdb.Aerion.desktop|url: ${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.desktop|" "$MANIFEST"
awk -v sha="$DESKTOP_SHA256" '
/url:.*Aerion\.desktop$/ { found=1; print; next }
found && /sha256:/ {
    match($0, /^[ \t]*/);
    spaces=substr($0, 1, RLENGTH);
    print spaces "sha256: " sha;
    found=0;
    next
}
{ print }
' "$MANIFEST" > "${MANIFEST}.tmp" && mv "${MANIFEST}.tmp" "$MANIFEST"

# Update icon URL and SHA256
sed -i "s|url: https://github.com/hkdb/aerion/releases/download/v[0-9.]\+[-a-zA-Z0-9]*/io.github.hkdb.Aerion.png|url: ${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.png|" "$MANIFEST"
awk -v sha="$ICON_SHA256" '
/url:.*Aerion\.png$/ { found=1; print; next }
found && /sha256:/ {
    match($0, /^[ \t]*/);
    spaces=substr($0, 1, RLENGTH);
    print spaces "sha256: " sha;
    found=0;
    next
}
{ print }
' "$MANIFEST" > "${MANIFEST}.tmp" && mv "${MANIFEST}.tmp" "$MANIFEST"

# Update metainfo URL and SHA256
sed -i "s|url: https://github.com/hkdb/aerion/releases/download/v[0-9.]\+[-a-zA-Z0-9]*/io.github.hkdb.Aerion.metainfo.xml|url: ${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.metainfo.xml|" "$MANIFEST"
awk -v sha="$METAINFO_SHA256" '
/url:.*Aerion\.metainfo\.xml$/ { found=1; print; next }
found && /sha256:/ {
    match($0, /^[ \t]*/);
    spaces=substr($0, 1, RLENGTH);
    print spaces "sha256: " sha;
    found=0;
    next
}
{ print }
' "$MANIFEST" > "${MANIFEST}.tmp" && mv "${MANIFEST}.tmp" "$MANIFEST"

# Update git tag
sed -i "s|tag: v.*|tag: ${VERSION}|" "$MANIFEST"

echo ""
echo "✅ Manifest updated successfully!"
echo "   Backup saved: ${MANIFEST}.backup"
echo ""
echo "=========================================="
echo "Summary:"
echo "=========================================="
echo ""
echo "x86_64 binary:"
echo "  url: ${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-x86_64"
echo "  sha256: $X86_64_SHA256"
echo ""
echo "aarch64 binary:"
echo "  url: ${REPO}/releases/download/${VERSION}/aerion-${VERSION}-linux-aarch64"
echo "  sha256: $AARCH64_SHA256"
echo ""
echo "Desktop file:"
echo "  url: ${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.desktop"
echo "  sha256: $DESKTOP_SHA256"
echo ""
echo "Icon:"
echo "  url: ${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.png"
echo "  sha256: $ICON_SHA256"
echo ""
echo "Metainfo:"
echo "  url: ${REPO}/releases/download/${VERSION}/io.github.hkdb.Aerion.metainfo.xml"
echo "  sha256: $METAINFO_SHA256"
echo ""
echo "=========================================="
echo "Next steps:"
echo "1. ✅ Manifest updated with ${VERSION} hashes"
echo "2. Review changes: git diff io.github.hkdb.Aerion.yml"
echo "3. Copy updated desktop/icon/metainfo to Flathub repo if changed"
echo "4. Test build: flatpak-builder --force-clean build-dir io.github.hkdb.Aerion.yml"
echo "=========================================="
