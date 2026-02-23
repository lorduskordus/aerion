# Adding a New Language to Aerion

This guide walks through adding a new language to Aerion's frontend. The i18n system uses `svelte-i18n` with JSON locale files and lazy loading — only the active locale is loaded at runtime.

## Prerequisites

- Node.js and npm installed
- Familiarity with the target language
- Access to the `frontend/src/lib/i18n/` directory

## Steps

### 1. Create the Locale JSON File

Copy the English source file and translate all values:

```bash
cp frontend/src/lib/i18n/locales/en.json frontend/src/lib/i18n/locales/<code>.json
```

Replace `<code>` with the appropriate BCP 47 locale code (e.g., `ja` for Japanese, `ko` for Korean, `fr` for French, `de` for German).

Open the new file and translate every string value. Keep the JSON keys unchanged — only translate the values.

**Example** (`en.json` → `ja.json`):
```json
{
  "common": {
    "save": "Save",         →  "save": "保存",
    "cancel": "Cancel",     →  "cancel": "キャンセル",
    "delete": "Delete"      →  "delete": "削除"
  }
}
```

For a complete, real-world example of a translated locale file, refer to `frontend/src/lib/i18n/locales/zh-HK.json` (Traditional Chinese, Hong Kong).

**Important notes**:
- Preserve `{placeholder}` tokens exactly as-is — these are ICU MessageFormat interpolation variables
  - Example: `"undone": "Undone: {description}"` → `"undone": "取り消し: {description}"`
- **Reposition `{placeholder}` tokens** to match your language's grammar — don't assume the English word order is correct for your language
  - Some placeholders contain localized relative time strings from date-fns (e.g., `{time}` may render as "2 minutes ago" or "2 分鐘前")
  - Example: English `"synced": "Synced {time}"` → Chinese `"synced": "{time}同步"` (time goes before the verb in Chinese)
- Do not translate JSON keys (left side of `:`)
- The file has ~900+ keys organized by namespace: `common`, `sidebar`, `messageList`, `viewer`, `composer`, `contextMenu`, `toast`, `responsive`, `settings`, `settingsAbout`, `settingsAccounts`, `settingsGeneral`, `editor`, `account`, `identity`, `security`, `contactSource`, `certificate`, `terms`, `dialog`, `date`, `aria`, `window`, `attachment`, `search`, `sort`, `oauth`

### 2. Register the Locale

Edit `frontend/src/lib/i18n/index.ts` and add a `register()` call for the new locale:

```typescript
register('en', () => import('./locales/en.json'))
register('zh-TW', () => import('./locales/zh-TW.json'))
register('zh-HK', () => import('./locales/zh-HK.json'))
register('zh-CN', () => import('./locales/zh-CN.json'))
register('ja', () => import('./locales/ja.json'))     // ← Add this line
```

### 3. Add to Supported Locales

In the same file (`frontend/src/lib/i18n/index.ts`), add the locale to the `supportedLocales` array:

```typescript
export const supportedLocales = [
  { code: 'en', name: 'English' },
  { code: 'zh-TW', name: '繁體中文 (台灣)' },
  { code: 'zh-HK', name: '繁體中文 (香港)' },
  { code: 'zh-CN', name: '简体中文' },
  { code: 'ja', name: '日本語' },                     // ← Add this line
] as const
```

Use the language's native name for the `name` field — this is what appears in the Settings language picker.

### 4. Add date-fns Locale (for Date Formatting)

Edit `frontend/src/lib/i18n/dateFnsLocale.ts` and add a case to the switch statement in `loadDateFnsLocale()`:

```typescript
switch (code) {
  case 'zh-TW': {
    const mod = await import('date-fns/locale/zh-TW')
    dateFnsLocale = mod.zhTW
    break
  }
  // ... existing cases ...
  case 'ja': {                                         // ← Add this block
    const mod = await import('date-fns/locale/ja')
    dateFnsLocale = mod.ja
    break
  }
}
```

Check the [date-fns locale list](https://date-fns.org/docs/Locale) for available locale codes and export names. Most locales are available — if not, the app falls back to English date formatting.

### 5. Update System Locale Detection (if needed)

In `frontend/src/lib/i18n/index.ts`, the `detectSystemLocale()` function maps `navigator.language` to supported locales. For most languages, the automatic matching works (e.g., `ja-JP` matches `ja` via the language prefix).

If your language has regional variants that need special mapping (like Chinese: `zh` → `zh-TW`, `zh-HK` → `zh-HK`), add a case before the generic fallback:

```typescript
const lang = lower.split('-')[0]
if (lang === 'zh') return 'zh-TW'
// Add special cases here if needed
```

For most languages, no changes are needed here.

### 6. Verify

```bash
cd frontend
npm run check    # Ensure no TypeScript errors
npm run build    # Ensure production build succeeds
```

Then run the app, open Settings > General, and select the new language from the Language dropdown. Verify:
- All strings in the UI are translated
- Dynamic strings with `{placeholders}` interpolate correctly (e.g., toast messages)
- Date formatting uses the correct locale
- The detached composer window also picks up the language

## File Summary

| File | Change |
|------|--------|
| `frontend/src/lib/i18n/locales/<code>.json` | **New** — translated strings |
| `frontend/src/lib/i18n/index.ts` | Add `register()` + `supportedLocales` entry |
| `frontend/src/lib/i18n/dateFnsLocale.ts` | Add `case` for date-fns locale |

No backend changes are needed. The language setting is stored via the existing `GetLanguage`/`SetLanguage` Wails bindings in `app/settings.go`.

## Existing Locales

| Code | Language | File |
|------|----------|------|
| `en` | English | `locales/en.json` (source of truth) |
| `zh-TW` | Traditional Chinese (Taiwan) | `locales/zh-TW.json` |
| `zh-HK` | Traditional Chinese (Hong Kong) | `locales/zh-HK.json` |
| `zh-CN` | Simplified Chinese | `locales/zh-CN.json` |

## Translation Key Namespaces

| Namespace | Description |
|-----------|-------------|
| `common` | Shared buttons and labels (Save, Cancel, Delete, etc.) |
| `sidebar` | Sidebar navigation (Compose, All Inboxes, folder names) |
| `messageList` | Message list UI (select all, no messages, loading) |
| `viewer` | Message viewer (reply, forward, attachments, error states, S/MIME/PGP banners) |
| `composer` | Email composer (To, Cc, Subject, Send, formatting) |
| `contextMenu` | Right-click context menus (Reply, Archive, Mark as Read) |
| `toast` | Toast notification messages (clean translated messages without raw error details) |
| `responsive` | Responsive layout labels (back, folders) |
| `settings` | Settings dialog tabs and titles |
| `settingsAbout` | About tab in settings |
| `settingsAccounts` | Accounts tab in settings |
| `settingsGeneral` | General settings tab (theme, density, read receipts) |
| `editor` | TipTap editor toolbar labels |
| `account` | Account dialog and management |
| `identity` | Identity editor (email address management, display names, signatures) |
| `security` | S/MIME and PGP security settings |
| `contactSource` | CardDAV contact source management |
| `certificate` | TLS certificate trust dialog |
| `terms` | Terms of service dialog |
| `dialog` | Generic dialog strings (confirmations, warnings) |
| `date` | Date/time labels (just now, yesterday, etc.) |
| `aria` | Accessibility labels (screen reader text) |
| `window` | Window management (minimize, maximize, close) |
| `attachment` | Attachment handling (download, save, open) |
| `search` | Search UI |
| `sort` | Sort options (newest first, oldest first) |
| `oauth` | OAuth flow UI |

## Key Conventions

- **Error/failure messages**: Use clean, translated messages. Do not include raw error details or `{error}` interpolation tokens in failure messages — keep them user-friendly (e.g., `"Failed to save."` not `"Failed to save: {error}"`).
- **Placeholder tokens**: `{placeholder}` tokens are used for dynamic values. Common tokens include:
  - `{name}` — account or contact source name
  - `{email}` / `{emails}` — email address(es)
  - `{count}` — numeric count (messages, attachments, etc.)
  - `{mode}` — composer mode (reply, forward, etc.)
  - `{time}` — relative time string (from date-fns, already localized)
  - `{folder}` — folder name
  - `{percentage}` — sync progress percentage
  - `{version}` — app version string
  - `{provider}` — OAuth provider name (Google, Microsoft)
  - `{query}` — search query text
  - `{domain}` / `{sender}` — email domain or sender address
  - `{description}` — undo action description
  - `{filename}` — attachment filename
- **Token positioning**: Reposition tokens to match your language's grammar — don't assume English word order is correct for your language.
