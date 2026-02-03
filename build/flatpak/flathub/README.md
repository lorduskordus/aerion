# Flathub Submission

This directory contains the Flathub-ready manifest for submitting Aerion to Flathub.

## Prerequisites

Before submitting to Flathub:

1. **Create a GitHub release** with a version tag (e.g., `v0.1.11`)
2. **Test the build** works with the Flathub manifest
3. **Ensure metainfo.xml** is up to date with current version and changelog

## Submitting to Flathub

### 1. Update the manifest

Edit `com.github.hkdb.Aerion.yml` and update the git source:

```yaml
sources:
  - type: git
    url: https://github.com/hkdb/aerion.git
    tag: v0.1.11  # Your release tag
    commit: abc123...  # Full commit hash for that tag
```

Get the commit hash with:
```bash
git rev-parse v0.1.11
```

### 2. Test the build locally

```bash
# Install flathub beta runtime if using newer features
flatpak install flathub org.gnome.Platform//47
flatpak install flathub org.gnome.Sdk//47

# Test build
flatpak-builder --force-clean --install-deps-from=flathub \
    build-dir build/flatpak/flathub/com.github.hkdb.Aerion.yml

# Test run
flatpak-builder --run build-dir build/flatpak/flathub/com.github.hkdb.Aerion.yml aerion
```

### 3. Fork and submit to Flathub

1. **Fork** https://github.com/flathub/flathub
2. **Create a new branch** in your fork (e.g., `add-aerion`)
3. **Add your app**:
   ```bash
   git clone https://github.com/YOUR_USERNAME/flathub.git
   cd flathub
   git checkout -b add-aerion
   git submodule add https://github.com/flathub/com.github.hkdb.Aerion.git
   git commit -m "Add Aerion email client"
   git push origin add-aerion
   ```
4. **Create a PR** to flathub/flathub
5. **Wait for review** - Flathub maintainers will review and test your submission

### 4. Create the app repository

You'll need to create a repository at `https://github.com/flathub/com.github.hkdb.Aerion` containing:

- `com.github.hkdb.Aerion.yml` (this manifest)
- `com.github.hkdb.Aerion.metainfo.xml` (from `build/flatpak/`)
- `flathub.json` (build configuration)

Example `flathub.json`:
```json
{
  "only-arches": ["x86_64", "aarch64"]
}
```

## OAuth Credentials

Flathub builds don't have access to OAuth credentials by default. You have two options:

1. **Public client IDs only** - Use OAuth public client IDs that don't require secrets
2. **Contact Flathub** - For apps that need secrets, contact Flathub maintainers about secure credential handling

For now, the app will build without OAuth credentials and users will need to configure IMAP/SMTP manually.

## Resources

- [Flathub Submission Guide](https://docs.flathub.org/docs/for-app-authors/submission)
- [App Requirements](https://docs.flathub.org/docs/for-app-authors/requirements)
- [Flathub Review Guidelines](https://docs.flathub.org/docs/for-app-authors/review-guidelines)

## Differences from Local Build

- **Source**: Uses git source from GitHub instead of local files
- **Network**: Flathub's build infrastructure has proper DNS/network configuration
- **OAuth**: Won't have OAuth credentials (unless configured with Flathub)
- **Build time**: May take longer on Flathub's infrastructure

## Updating on Flathub

After initial submission, to publish new versions:

1. Update the manifest in your `flathub/com.github.hkdb.Aerion` repository
2. Update the tag and commit hash to point to the new release
3. Update the metainfo.xml with new version and changelog
4. Commit and push - Flathub will automatically build and publish
