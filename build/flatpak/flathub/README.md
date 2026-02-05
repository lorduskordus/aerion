# Flathub Submission

This directory contains the assets for submitting Aerion to Flathub using **pre-built binaries** (extra-data approach) after each Github release.

## Why Pre-Built Binaries?

Aerion uses the extra-data approach (similar to Discord, Spotify) because OAuth credentials are embedded at build time in GitHub Actions. This allows users to have Gmail/Outlook OAuth working out-of-the-box without exposing secrets to Flathub's build infrastructure.

## Prerequisites

Before submitting to Flathub:

1. **Create a GitHub release** with version tag (e.g., `v0.1.13`)
2. **Ensure release includes**:
   - `aerion-v0.1.13-linux-x86_64.tar.gz` (x86_64 binary with OAuth credentials)
   - `aerion-v0.1.13-linux-aarch64.tar.gz` (aarch64 binary with OAuth credentials)
3. **Update metainfo.xml** with new version and changelog
4. **Test the manifest** works with the release

## Updating the Manifest for New Releases

### Step 1: Calculate Hashes

Use the provided script to get SHA256 hashes and file sizes:

```bash
./calculate-hashes.sh v0.1.14
```

This will output all the values you need for the manifest.

### Step 2: Update the Manifest

Edit `com.github.hkdb.Aerion-extradata.yml` and update:

1. **Version in URLs**: Change `v0.1.13` to `v0.1.14` in all URL fields
2. **SHA256 hashes**: Replace with values from calculate-hashes.sh output
3. **File sizes**: Replace with values from calculate-hashes.sh output

The script output will look like:
```
x86_64 tarball:
  url: https://github.com/hkdb/aerion/releases/download/v0.1.14/aerion-v0.1.14-linux-x86_64.tar.gz
  sha256: abc123...
  size: 12345678
```

### Step 3: Test Locally

```bash
# Install runtimes if not already installed
flatpak install flathub org.gnome.Platform//47
flatpak install flathub org.gnome.Sdk//47

# Test build with extradata manifest
flatpak-builder --force-clean --install-deps-from=flathub \
    test-build-dir com.github.hkdb.Aerion-extradata.yml

# Test run
flatpak-builder --run test-build-dir \
    com.github.hkdb.Aerion-extradata.yml aerion
```

**Note**: The build will download your pre-built binaries from GitHub releases.

## Initial Flathub Submission

### Step 1: Create Flathub Issue

Go to https://github.com/flathub/flathub/issues/new and create a new issue:

**Title**: `New app: Aerion`

**Body**:
```markdown
# New Application Submission: Aerion

**Name**: Aerion
**Summary**: Lightweight open-source email client for Linux
**App ID**: com.github.hkdb.Aerion
**Homepage**: https://aerion.3df.io
**Source Repository**: https://github.com/hkdb/aerion

## Description
Aerion is a modern, lightweight email client built with Wails + Svelte,
focused on resource efficiency and modern UX. Supports Gmail, Outlook,
and generic IMAP/SMTP with OAuth2 authentication.

## Key Features
- Multiple accounts with unified inbox
- OAuth2 for Gmail and Microsoft
- Conversation threading
- CardDAV contact sync
- Keyboard-friendly navigation
- Dark mode and themes

## Technical Details
- **Distribution Method**: Pre-built binaries (extra-data) - similar to Discord, Spotify
- **Why pre-built**: OAuth credentials are embedded in binaries at build time
- **Architectures**: x86_64 and aarch64
- **License**: Apache-2.0
- **Runtime**: org.gnome.Platform//47

## Maintainer
GitHub: @hkdb

## Request
Please create repository: `flathub/com.github.hkdb.Aerion`
I'm ready to populate it with manifest files.
```

### Step 2: Wait for Repository Creation

The Flathub team will:
1. Review your request
2. Create `https://github.com/flathub/com.github.hkdb.Aerion`
3. Grant you write access

This usually takes 2-5 days.

### Step 3: Populate Flathub Repository

Once you have access:

```bash
# Clone the Flathub repository
git clone git@github.com:flathub/com.github.hkdb.Aerion.git
cd com.github.hkdb.Aerion

# Copy manifest (rename to standard name)
cp /path/to/aerion/build/flatpak/flathub/com.github.hkdb.Aerion-extradata.yml \
   com.github.hkdb.Aerion.yml

# Copy metainfo
cp /path/to/aerion/build/flatpak/com.github.hkdb.Aerion.metainfo.xml .

# Create flathub.json
cat > flathub.json << 'EOF'
{
  "only-arches": ["x86_64", "aarch64"]
}
EOF

# Commit and push
git add .
git commit -m "Initial Flathub submission for Aerion"
git push origin master
```

### Step 4: Monitor Build

After pushing:
- Flathub CI will automatically build
- Monitor at: https://buildbot.flathub.org
- Check repository's Actions/Checks tab

### Step 5: Review Process

Flathub reviewers will check:
- Manifest correctness
- Metadata completeness
- Build success
- Permissions

**Common feedback**:
- May ask to restrict `--filesystem=home` to more specific paths
- Verify extra-data checksums match

## Updating on Flathub

For subsequent releases (v0.1.14, v0.1.15, etc.):

```bash
# 1. Create GitHub release with new binaries (GitHub Actions does this)

# 2. Calculate new hashes
cd /path/to/aerion/build/flatpak/flathub
./calculate-hashes.sh v0.1.14

# 3. Update extradata manifest with new values
# Edit com.github.hkdb.Aerion-extradata.yml

# 4. Update metainfo with new release entry
# Edit ../com.github.hkdb.Aerion.metainfo.xml

# 5. Commit changes to `hkdb/aerion` repo
git add .
git commit -m "<version> - Flathub submission"
git push

# 6. Push to Flathub repository
cd /path/to/flathub/com.github.hkdb.Aerion
cp /path/to/aerion/repo/build/flatpak/flathub/com.github.hkdb.Aerion-extradata.yml \
   com.github.hkdb.Aerion.yml
cp /path/to/aerion/repo/build/flatpak/com.github.hkdb.Aerion.metainfo.xml .

git add .
git commit -m "Update to v0.1.14"
git push

# Flathub auto-builds and publishes (no re-review needed!)
```

## Files in This Directory

- `com.github.hkdb.Aerion-extradata.yml` - Flatpak manifest using pre-built binaries
- `com.github.hkdb.Aerion.yml` - Alternative manifest (builds from source, not used)
- `calculate-hashes.sh` - Helper script to calculate SHA256 and sizes for new releases
- `README.md` - This file

## OAuth Credentials

With the extra-data approach, OAuth credentials are **already embedded** in the pre-built binaries. Users get working Gmail/Outlook OAuth out-of-the-box without any additional configuration.

## Resources

- [Flathub Submission Guide](https://docs.flathub.org/docs/for-app-authors/submission)
- [App Requirements](https://docs.flathub.org/docs/for-app-authors/requirements)
- [Flathub Review Guidelines](https://docs.flathub.org/docs/for-app-authors/review-guidelines)
- [Extra Data Documentation](https://docs.flatpak.org/en/latest/flatpak-builder-command-reference.html#extra-data-sources)

## Troubleshooting

**Build fails with "Could not download file"**:
- Ensure release tarballs are publicly accessible on GitHub
- Verify URLs match exactly (case-sensitive)

**SHA256 mismatch error**:
- Re-run `./calculate-hashes.sh` with correct version
- Ensure you're pointing to the correct GitHub release tag

**Permission errors during runtime**:
- Review `finish-args` in manifest
- May need to justify or restrict filesystem access
