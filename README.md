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
- [Keyboard Shortcuts](docs/KEYBOARD_SHORTCUTS.md)

### 🚀 Installation
---

Download from the release page:
- Linux: AppImage (Or the binary + .desktop file if you prefer)
- MacOS: .app
- Windows: .exe

For more information, check the [Installation Section](https://aerion.3df.io/docs/getting-started/installation/) of the official documentation.


### 📖 Documentation
---

- [Official Documentation](https://aerion.3df.io/docs/intro)


### ⚗️ Tech Stack
---

This application was built with [Wails](https://wails.io) + [Svelte](https://svelte.dev/) and mostly implemented by Claude Opus 4.5.


### 🧑🏻‍💻 Roadmap
---

Potential features in the future:

- PGP Support
- In-App Keyboard Shortcut Cheat Sheet
- Integrated Calendar?
- Theme (Color) Customization
- AI Assisted Composition
- Advance Search


### 💰 Sponsorship
---

[3DF](https://3df.io) is sponsoring by way of dedicating the team's time to work on this. There's otherwise currently no sponsorship. If you like this project, please feel free to give us a star or buy us a coffee:

[!["Buy Me A Coffee"](https://www.buymeacoffee.com/assets/img/custom_images/yellow_img.png)](https://www.buymeacoffee.com/3dfosi)


### 🏷️ Changelog
---

**01-19-2026 - v0.1.1**

- Compile AppImage with Ubuntu 22.04 instead to improve compatibility with older systems


**01-16-2026 - v0.1.0**

- First release - ALPHA


### 📑 Terms of Use & Privacy Policy
---

- [Terms of Use](docs/TERMS.md)
- [Privacy Policy](docs/PRIVACY.md)
