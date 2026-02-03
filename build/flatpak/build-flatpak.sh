#!/bin/bash
# Build Aerion Flatpak locally

set -e

# Change to project root
cd "$(dirname "$0")/../.."

echo "=== Aerion Flatpak Builder ==="
echo ""

# Check if flatpak-builder is installed
if ! command -v flatpak-builder &> /dev/null; then
    echo "❌ flatpak-builder is not installed"
    echo ""
    echo "Install it with:"
    echo "  Fedora:        sudo dnf install flatpak-builder"
    echo "  Ubuntu/Debian: sudo apt install flatpak-builder"
    echo "  Arch:          sudo pacman -S flatpak-builder"
    exit 1
fi

# Check if runtimes are installed
echo "Checking for required runtimes..."
if ! flatpak list --runtime | grep -q "org.gnome.Platform.*47"; then
    echo "⚠️  GNOME Platform 47 not found"
    echo "Installing..."
    flatpak install -y flathub org.gnome.Platform//47 org.gnome.Sdk//47
fi

if ! flatpak list | grep -q "org.freedesktop.Sdk.Extension.golang"; then
    echo "⚠️  Go SDK extension not found"
    echo "Installing..."
    flatpak install -y flathub org.freedesktop.Sdk.Extension.golang
fi

if ! flatpak list | grep -q "org.freedesktop.Sdk.Extension.node20"; then
    echo "⚠️  Node.js 20 SDK extension not found"
    echo "Installing..."
    flatpak install -y flathub org.freedesktop.Sdk.Extension.node20
fi

echo "✅ All runtimes installed"
echo ""

# Check for OAuth credentials
if [ -z "$GOOGLE_CLIENT_ID" ] && [ -z "$MICROSOFT_CLIENT_ID" ]; then
    echo "⚠️  Warning: No OAuth credentials found"
    echo "Gmail and Outlook OAuth will not work in the built app"
    echo ""
    echo "To include OAuth credentials, set environment variables before running:"
    echo "  export GOOGLE_CLIENT_ID='your-client-id'"
    echo "  export GOOGLE_CLIENT_SECRET='your-client-secret'"
    echo "  export MICROSOFT_CLIENT_ID='your-microsoft-client-id'"
    echo ""
    read -p "Continue without OAuth credentials? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Validate metainfo if appstream-util is available
if command -v appstream-util &> /dev/null; then
    echo "Validating AppStream metadata..."
    if appstream-util validate build/flatpak/com.github.hkdb.Aerion.metainfo.xml 2>&1 | grep -q "FAILED"; then
        echo "⚠️  AppStream validation warnings (non-fatal):"
        appstream-util validate build/flatpak/com.github.hkdb.Aerion.metainfo.xml 2>&1 | grep -v "Validation was successful" || true
    else
        echo "✅ AppStream metadata valid"
    fi
    echo ""
fi

# Clean previous build
echo "Cleaning previous build..."
rm -rf .flatpak-builder build-dir

# Build
echo ""
echo "Building Flatpak..."
echo "This will take several minutes on first build..."
echo ""

flatpak-builder --force-clean --user --install-deps-from=flathub build-dir build/flatpak/com.github.hkdb.Aerion.yml

# Install
echo ""
echo "Installing Flatpak..."
flatpak-builder --user --install --force-clean build-dir build/flatpak/com.github.hkdb.Aerion.yml

echo ""
echo "✅ Build complete!"
echo ""
echo "Run with: flatpak run com.github.hkdb.Aerion"
echo "Or search for 'Aerion' in your application menu"
echo ""
echo "To uninstall: flatpak uninstall --user com.github.hkdb.Aerion"
