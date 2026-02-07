#!/bin/bash
# Copy Aerion files to Flathub repository for submission/update
# Usage: ./release.sh /path/to/flathub/io.github.hkdb.Aerion

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <flathub-repo-path>"
    echo "Example: $0 ~/flathub/io.github.hkdb.Aerion"
    exit 1
fi

FLATHUB_DIR="$1"

if [ ! -d "$FLATHUB_DIR" ]; then
    echo "❌ ERROR: Directory not found: $FLATHUB_DIR"
    exit 1
fi

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
BUILD_DIR="$( cd "${SCRIPT_DIR}/.." && pwd )"
LINUX_DIR="$( cd "${BUILD_DIR}/.." && pwd )/linux"

echo "=========================================="
echo "Aerion Flathub Release Helper"
echo "=========================================="
echo "Source: $SCRIPT_DIR"
echo "Target: $FLATHUB_DIR"
echo ""
echo "Copying files..."
echo ""
echo "NOTE: Binaries, desktop file, icon, and metainfo are bundled in a single"
echo "      archive on GitHub releases. Only the manifest and flathub.json are"
echo "      stored in the Flathub repo"
echo ""

# Copy manifest (rename to standard name)
echo "📄 Copying manifest..."
cp "${SCRIPT_DIR}/io.github.hkdb.Aerion.yml" \
   "${FLATHUB_DIR}/io.github.hkdb.Aerion.yml"
echo "   ✅ io.github.hkdb.Aerion.yml"

# Create flathub.json if it doesn't exist (only needed for initial submission)
if [ ! -f "${FLATHUB_DIR}/flathub.json" ]; then
    echo "📄 Creating flathub.json..."
    cat > "${FLATHUB_DIR}/flathub.json" << 'EOF'
{
  "only-arches": ["x86_64", "aarch64"]
}
EOF
    echo "   ✅ flathub.json (created)"
else
    echo "📄 flathub.json already exists (skipped)"
fi

echo ""
echo "=========================================="
echo "✅ All files copied successfully!"
echo "=========================================="
echo ""
echo "Next steps:"
echo "1. cd $FLATHUB_DIR"
echo "2. git status  # Review changes"
echo "3. git add ."
echo "4. git commit -m \"Update to vX.X.XX\""
echo "5. git push"
echo "=========================================="
