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


### 🪶 Features
---

- Multiple Accounts
- Providers: (🧪 = NOT YET TESTED)
    - Generic IMAP/SMTP
    - GMail
    - Microsoft 365 / Outlook
    - Yahoo 🧪
    - Proton Mail (via Proton Bridge)
    - iCloud Mail 
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
- PGP & S/MIME experimental support
- [Keyboard Shortcuts](docs/KEYBOARD_SHORTCUTS.md)

### 🚀 Installation
---

- [Official Installation Guide](https://aerion.3df.io/docs/getting-started/installation/)


### 📖 Documentation
---

- [Official Documentation](https://aerion.3df.io/docs/intro)


### ⚗️ Tech Stack
---

This application was built with [Wails](https://wails.io) + [Svelte](https://svelte.dev/) and implemented by various versions of Claude Opus & Sonnet models with lots of prompted refactors and manual edits.


### 🧑🏻‍💻 Roadmap
---

Potential features in the future:

- Customizable shortcut keys?
- Advance Search
- Explore the possibility of supporting [Age](https://github.com/FiloSottile/age) as an encryption method
- Integrated Calendar?
- AI Assisted Composition (Ollama)


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
