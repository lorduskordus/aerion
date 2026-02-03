#!/bin/bash
# Build Aerion Flatpak using Docker (hybrid approach: build binary on host in container, then package)

set -e

cd "$(dirname "$0")/../.."

echo "=== Aerion Flatpak Docker Builder ==="
echo ""

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed"
    echo ""
    echo "Install it with:"
    echo "  Ubuntu/Debian: sudo apt install docker.io"
    echo "  Fedora:        sudo dnf install docker"
    echo "  Arch:          sudo pacman -S docker"
    exit 1
fi

# Check if Docker daemon is running
if ! docker info &> /dev/null; then
    echo "❌ Docker daemon is not running"
    echo ""
    echo "Start it with:"
    echo "  sudo systemctl start docker"
    exit 1
fi

# Check for OAuth credentials
if [ -z "$GOOGLE_CLIENT_ID" ] && [ -z "$MICROSOFT_CLIENT_ID" ]; then
    echo "⚠️  Warning: No OAuth credentials found"
    echo "Gmail and Outlook OAuth will not work in the built app"
    echo ""
fi

echo "Building Docker image (this may take a few minutes on first run)..."
docker build -t aerion-flatpak-builder -f build/flatpak/Dockerfile build/flatpak

echo ""
echo "Building Aerion in Docker container..."
echo ""

# Get version from git tag
VERSION=$(git describe --tags --exact-match 2>/dev/null || echo "dev")

# Run the build in Docker
docker run --rm \
    -v "$(pwd):/workspace" \
    -w /workspace \
    -e GOOGLE_CLIENT_ID="${GOOGLE_CLIENT_ID}" \
    -e GOOGLE_CLIENT_SECRET="${GOOGLE_CLIENT_SECRET}" \
    -e MICROSOFT_CLIENT_ID="${MICROSOFT_CLIENT_ID}" \
    aerion-flatpak-builder \
    bash -c "
        echo 'Installing frontend dependencies...'
        cd frontend && npm install && cd ..

        echo ''
        echo 'Building Aerion binary...'
        make build-linux

        echo ''
        echo 'Packaging into Flatpak...'
        flatpak-builder --force-clean --repo=repo build-dir build/flatpak/com.github.hkdb.Aerion-prebuilt.yml

        echo ''
        echo 'Creating .flatpak bundle...'
        mkdir -p build/bin
        flatpak build-bundle repo build/bin/Aerion-${VERSION}.flatpak com.github.hkdb.Aerion
    "

echo ""
echo "✅ Build complete!"
echo ""
echo "Flatpak bundle created: build/bin/Aerion-${VERSION}.flatpak"
echo ""
echo "To install locally:"
echo "  flatpak install --user build/bin/Aerion-${VERSION}.flatpak"
echo ""
echo "To run:"
echo "  flatpak run com.github.hkdb.Aerion"
