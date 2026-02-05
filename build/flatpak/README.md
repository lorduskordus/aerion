# Flatpak Packaging for Aerion

This directory contains files for building and distributing Aerion as a Flatpak.

## Quick Links

- **For Flathub submission**: See [`flathub/README.md`](flathub/README.md) for complete submission guide
- **For local development**: Continue reading this file

## Files

- `com.github.hkdb.Aerion.yml` - Flatpak manifest for local building (builds from source)
- `com.github.hkdb.Aerion-prebuilt.yml` - Flatpak manifest using pre-built binary
- `com.github.hkdb.Aerion.metainfo.xml` - AppStream metadata (required for Flathub)
- `build-flatpak.sh` - Automated build script
- `flathub/` - Flathub submission files (pre-built binary approach)
- `README.md` - This file

## Prerequisites

Install flatpak-builder:

```bash
# Fedora
sudo dnf install flatpak-builder

# Ubuntu/Debian
sudo apt install flatpak-builder

# Arch
sudo pacman -S flatpak-builder
```

Add Flathub repository (if not already added):

```bash
flatpak remote-add --if-not-exists flathub https://flathub.org/repo/flathub.flatpakrepo
```

Install required runtimes and SDKs:

```bash
flatpak install flathub org.gnome.Platform//47 org.gnome.Sdk//47
flatpak install flathub org.freedesktop.Sdk.Extension.golang
flatpak install flathub org.freedesktop.Sdk.Extension.node20
```

## Building Locally

Build the Flatpak from the project root:

```bash
# Using the build script (recommended)
./build/flatpak/build-flatpak.sh

# Or manually from project root
flatpak-builder --force-clean --user --install build-dir build/flatpak/com.github.hkdb.Aerion.yml

# Or via make
make flatpak
```

This will:
1. Download and set up the GNOME 47 runtime
2. Install Go and Node.js SDK extensions
3. Build Aerion with all dependencies
4. Install it locally for your user

## Running

After building, run the Flatpak:

```bash
flatpak run com.github.hkdb.Aerion
```

## Testing

Test the app thoroughly:

```bash
# Run with terminal output for debugging
flatpak run com.github.hkdb.Aerion

# Check permissions
flatpak info --show-permissions com.github.hkdb.Aerion

# Override permissions for testing (example: restrict to Downloads only)
flatpak override --user --nofilesystem=home --filesystem=xdg-download com.github.hkdb.Aerion
```

## OAuth Credentials

### For Local Development

Set OAuth credentials as environment variables before building:

```bash
export GOOGLE_CLIENT_ID="your-client-id"
export GOOGLE_CLIENT_SECRET="your-client-secret"
export MICROSOFT_CLIENT_ID="your-microsoft-client-id"

./build/flatpak/build-flatpak.sh
```

### For Flathub Distribution

For Flathub submission, OAuth credentials should **NOT** be hardcoded in the manifest. Options:

1. **Recommended:** Use your own OAuth client IDs (users won't need to set up their own)
   - Add credentials to Flathub secrets during submission
   - Update manifest to use build-args for credentials

2. **Alternative:** Don't embed credentials, require users to configure OAuth
   - Less user-friendly but more transparent
   - Users provide their own OAuth credentials in app settings

For option 1, update the manifest build-commands section:

```yaml
build-commands:
  - |
    export PATH=$PATH:/run/build/aerion/go/bin
    /run/build/aerion/go/bin/wails build \
      -ldflags "-X 'github.com/hkdb/aerion/internal/oauth2.GoogleClientID=${GOOGLE_CLIENT_ID}' \
                -X 'github.com/hkdb/aerion/internal/oauth2.GoogleClientSecret=${GOOGLE_CLIENT_SECRET}' \
                -X 'github.com/hkdb/aerion/internal/oauth2.MicrosoftClientID=${MICROSOFT_CLIENT_ID}'" \
      -tags webkit2_41,linux,production -o aerion
```

## Validation

Before submitting to Flathub, validate the metainfo file:

```bash
# Install appstream-util
sudo dnf install libappstream-glib  # Fedora
sudo apt install appstream-util      # Ubuntu/Debian

# Validate (from project root)
appstream-util validate build/flatpak/com.github.hkdb.Aerion.metainfo.xml
```

Validate the desktop file:

```bash
desktop-file-validate build/linux/aerion.desktop
```

## Submitting to Flathub

**See [`flathub/README.md`](flathub/README.md)** for complete Flathub submission instructions.

Aerion uses the **pre-built binary approach** (extra-data) for Flathub distribution, which allows OAuth credentials to be embedded in the binaries. The flathub directory contains:
- Updated manifest for pre-built binaries
- Submission guide
- Helper script to calculate hashes for new releases

## Troubleshooting

### Build Fails with "Cannot find module"

Frontend dependencies aren't installed. Check the npm install command in the manifest.

### "Permission denied" Errors

The app may be trying to access directories outside the sandbox. Check finish-args permissions.

### WebKit Not Working

Make sure you're using `runtime: org.gnome.Platform` which includes webkit2gtk-4.1.

### Can't Access Home Directory

The manifest grants `--filesystem=home` access. For production, consider restricting to:
```yaml
- --filesystem=xdg-download
- --filesystem=xdg-documents
```

## Additional Resources

- [Flatpak Documentation](https://docs.flatpak.org/)
- [Flathub Submission Guide](https://github.com/flathub/flathub/wiki/App-Submission)
- [AppStream Guidelines](https://www.freedesktop.org/software/appstream/docs/)
- [Flatpak Builder Manifest](https://docs.flatpak.org/en/latest/flatpak-builder-command-reference.html)

## Advantages Over AppImage

- ✅ WebKit provided by GNOME runtime (no bundling needed)
- ✅ Works on ALL Linux distros consistently
- ✅ Sandboxing is properly implemented
- ✅ Automatic updates via Flatpak
- ✅ Centralized distribution through Flathub
- ✅ Better integration with desktop environments
- ✅ Shared runtime = smaller download size

## Maintenance

### For Local Builds

When testing a new version locally:

1. Update `build/flatpak/com.github.hkdb.Aerion.metainfo.xml` with new release info
2. Update OAuth credentials in environment if needed
3. Build and test locally: `./build/flatpak/build-flatpak.sh`

### For Flathub Updates

See [`flathub/README.md`](flathub/README.md) for instructions on updating your Flathub release.
