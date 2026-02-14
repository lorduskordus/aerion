![Logo](frontend/src/assets/images/logo-universal.png)

# Aerion - An Open Source Lightweight E-Mail Client
Maintained by: @hkdb

![screenshot](docs/ss.png)


### ❓ Why?
---

Windows has Outlook

Mac has Mail

Linux has.....
 - Thunderbird - Clunky and too much legacy structure
 - Geary - Crippled by Gnome Online Accounts and search is unreliable
 - Mailspring - Electron...
 - Evolution - ... 1999

All are not necessarily always light on resource consumption...


### 👁️‍🗨️ Summary
---

A standalone lightweight e-mail client inspired by [Geary](https://wiki.gnome.org/Apps/Geary) focused on achieving the following goals:

- Resource Efficiency - Minimal CPU, RAM, and battery consumption
- Modern UX - Clean, intuitive interface with dark mode support
- Keyboard & Mouse Friendly - Full keyboard navigation with vim-style shortcuts
- Independence - No dependency on Gnome Online Accounts or other system services
- Search That Works - Basic search that actually finds your emails


### 🖥 OS Support
---

Although Linux is a first-class citizen here, it should also work on:

- MacOS
- Windows

Some of the system level features (clickable notifications & auto-sync on wake) are not yet implemented on MacOS and Windows.


### 🪶 Features
---

- Multiple Accounts
- Providers: (🧪 = NOT YET TESTED)
    - Generic IMAP/SMTP
    - GMail
    - Microsoft 365 / Outlook
    - Yahoo 🧪
    - Proton Mail (via Proton Bridge)
    - iCloud Mail 🧪
    - Fastmail 🧪
    - Zoho Mail 🧪
    - AOL Mail 🧪
    - GMX Mail 🧪
    - Mail.com 🧪
- Unified Inbox (Color Code Accounts)
- Conversation Threads
- Basic Removal of Tracking Elements in Mail Content
- WYSIWYG Detachable Composer ([TipTap Editor](https://github.com/ueberdosis/tiptap))
- WYSIWYG Signatures ([TipTap Editor](https://github.com/ueberdosis/tiptap))
- CardDav/Google/Microsoft Contact Sync for auto-complete
- Basic Search
- Notification that brings focus to the e-mail when clicked (Linux Only)
- Auto-Sync when system wakes from suspend (Linux Only)
- Multiple color themes (More to come...)
- PGP & S/MIME support
- [Keyboard Shortcuts](docs/KEYBOARD_SHORTCUTS.md)

### 🚀 Installation
---

Download from the release page:
- Linux: Binary Tarball (Flatpak coming soon)
- MacOS: .app
- Windows: .exe

For Linux ~

1. Install dependency if it's not already on your system:

Debian/Ubuntu:

```bash

sudo apt install libwebkit2gtk-4.1-0
```
Fedora:

```bash
sudo dnf install webkit2gtk4.1
```
Arch Linux:

```bash
sudo pacman -S webkit2gtk-4.1
```

2. Download the latest tarball for:

- [amd64](https://github.com/hkdb/aerion/releases/latest/download/aerion-linux-amd64.tar.gz)
- [arm64](https://github.com/hkdb/aerion/releases/latest/download/aerion-linux-arm64.tar.gz)

3. Untar and install:

```bash
tar -xzvf aerion-linux-*.tar.gz
cd aerion-linux-<arch>
./install.sh
# This install script will give you a choice to install it system-wide or just for the user.
# Follow the prompts and complete the installation.
```

For more information, check the [Installation Section](https://aerion.3df.io/docs/getting-started/installation/) of the official documentation.

**Note:** AppImage support has been removed due to webkit bundling incompatibilities. See `archive/AppImage/README.md` for technical details.

### 📖 Documentation
---

- [Official Documentation](https://aerion.3df.io/docs/intro)


### ⚗️ Tech Stack
---

This application was built with [Wails](https://wails.io) + [Svelte](https://svelte.dev/) and largely implemented by various versions of Claude Opus & Sonnet models with lots of prompted refactors and manual edits. No yolo-ing.


### 🧑🏻‍💻 Roadmap
---

Potential features in the future:

- Responsive layout (For tiled windows, Linux Phones, and Tablets)
- Customizable shortcut keys?
- Advance Search
- Explore the possibility of supporting [Age](https://github.com/FiloSottile/age) as an encryption method
- Integrated Calendar?
- AI Assisted Composition


### 💰 Sponsorship
---

[3DF](https://3df.io) is sponsoring by way of dedicating the team's time to work on this. There's otherwise currently no sponsorship. If you like this project, please feel free to give us a star or buy us a coffee:

[!["Buy Me A Coffee"](https://www.buymeacoffee.com/assets/img/custom_images/yellow_img.png)](https://www.buymeacoffee.com/3dfosi)


### 🏷️ Changelog
---

[CHANGELOG.md](CHANGELOG.md)


### 📑 Terms of Use & Privacy Policy
---

- [Terms of Use](docs/TERMS.md)
- [Privacy Policy](docs/PRIVACY.md)
