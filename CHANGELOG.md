# CHANGELOG

**v0.1.23 - 02-16-2026**
---

- Fixed race condition on marking message read when notification clicked


**v0.1.22 - 02-16-2026**
---

- Fixed wake from sleep flow - [#17](https://github.com/hkdb/aerion/issues/17)
- Added proper network state monitoring
- Improved wake, scheduled syncs, idle, and status logic with net state
- Added proper logic for offline mode
- Fixed S/MIME algo - [#13](https://github.com/hkdb/aerion/issues/13)


**v0.1.21 - 02-14-2026**
---

- Added PGP support - needs more testing
- Added S/MIME support - needs more testing
- Fixed composer rapid enter lag issue with 0 margin `<p>` instead of `<br>`
- Added auto refresh of draft folder on discard
- Added logic to prevent uneccessary reloads of loaded conversations if there's no change
- Fixed draft synced to server indication regression
- Fixed inserted images and attachments saved in draft folder
- Max window size fix [#4](https://github.com/hkdb/aerion/issues/4)
- Auto-focus to the To: field on launch of new composer and on forwards
- Fixed reliability issues with attach file and insert image
- Fixed deletion while syncing
- Improved dead connections handling which makes wake from sleep more reliable & should fix [#9](https://github.com/hkdb/aerion/issues/9)
- Fixed delete mail from trash [#9](https://github.com/hkdb/aerion/issues/9)
- Added reply, reply-all, and forward of a specific message
- Fixed move mail from trash back to inbox
- Improved Sent Folder detection (Wrong sent folder mapping will break threading)
- Ctrl+A when focused on message list will select all messages [#14](https://github.com/hkdb/aerion/issues/14)
- Ctrl+A when focused on conversation viewer will select all text of the expanded email in viewport
    

**v0.1.20 - 02-11-2026**
---

- Added resolution change detection - [#4](https://github.com/hkdb/aerion/issues/4)
- Added trusted self-signed cert flow and store - [#6](https://github.com/hkdb/aerion/issues/6)
- Improved imap login logic
- Improved image blocking to include CSS loaded images
- Enabled horizontal scroll in conversation viewer
    

**v0.1.19 - 02-09-2026**
---

- Fixed terms acceptance visibility
- Enhanced system theme detection
- Fixed idle.go/server.go
- Implemented a workaround for calling dialog through portal
- Removed redundant desktop-file-edit commands from Flatpak manifest
    

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
