# CHANGELOG

**v0.1.18 - 02-08-2026**
---

- Converted to Flathub build from source


**v0.1.17 - 02-07-2026**
---

- Added refresh conversation viewer if new mail arrives in the thread
- Added auto scroll to the bottom (newest mail) in conversation viewer on long threads
- GA/Flathub submission fix


**v0.1.16 - 02-07-2026**
---

- Removed flatpak perm that's already allowed by default
- Fixed hash calculation for Flatpak build and Flathub submission


**v0.1.15 - 02-05-2026**
---

- Refactored Linux notifications to use org.freedesktop.portal.Desktop
- Kept DBUS direct notifications if launched with --dbus-notify
- Added trigger to refocus to Aerion if notification is clicked
- Added `install.sh` and `uninstall.sh` to Linux binary release
- Distribute binary tarballs with assets instead of just binary for Linux
- Fixed flatpak app ID
- Flathub submission fixes
- New Github Actions worksflow that makes much more sense


**v0.1.14 - 02-05-2026**
---

- Finalized flatpak submission


**v0.1.13 - 02-04-2026**
---

- Fixed links that don't open in browser (ie. Linkedin, etc)
- Added show link on hover
- Added context menu for links so users can choose to copy the link instead of clicking it directly


**v0.1.12 - 02-03-2026**
---

- Removed AppImage build
- Implemented Flatpak build


**v0.1.11 - 02-02-2026**
---

- Fixed detached composer theme
- Fixed message focus on refresh
- Improved transitions for smoother UX


**v0.1.10 - 02-02-2026**
---

- Added other themes:
    - Dark (Gray)
    - Light (Blue)
    - Light (Orange)


**v0.1.9 - 01-29-2026**
---

- Ability to disable window title bar in settings
- Added an AppImage just for Immutable/Atomic distros [#1](https://github.com/hkdb/aerion/issues/1)


**v0.1.8 - 01-29-2026**
---

- Fixed AppImage support for more popular immutable/atomic distros


**v0.1.7 - 01-29-2026**
---

- Fixed AppImage regression for non-atomic distros
- Sticking with 22.04 LTS to build since 20.04 doesn't have webkit2gtk-4.1 and 20.04 is only a few months away from EOS.


**v0.1.6 - 01-28-2026**
---

- Fixed signature insertion on reply
- Fixed replies not being tracked in conversations
- Fixed ghost recipient on reply-All 
- Cleaned up console.log/warn in frontend
- Added ability to delete single message from conversation
- Sync draft folder after saving draft from inline composer
- Reload conversation viewer after saving draft
- Added keyboard driven single message delete (focus on conversation viewer pane --> tab to msg --> delete)


**v0.1.5 - 01-27-2026**
---

- Bundle icons instead of downloading on launch
- Improved AppImage compatibility


**v0.1.4 - 01-26-2026**
---

- Fixed delete flow regression
- Fixed null reference errors


**v0.1.3 - 01-25-2026**
---

- Added "Mark as NOT Spam" to spam folders
- Improved Google contact sync error handling
- Auto-focus on the first message of search results on enter
- Added cancel folder sync
- Added shortcut keys for sync all accounts and folder sync


**v0.1.2 - 01-22-2026**
---

- Looses keyboard control if e-mail content was clicked
- Autofocus on first message when switched to new folder
- Disable focus on conversation viewer when links are clicked


**v0.1.1 - 01-19-2026**
---

- Compile AppImage with Ubuntu 22.04 instead to improve compatibility with older systems


**v0.1.0 - 01-16-2026**
---

- First release - ALPHA
