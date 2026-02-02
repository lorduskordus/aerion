// Runes-based settings store
// Provides reactive state for application settings

// @ts-ignore - wailsjs path
import { GetMessageListDensity, GetMessageListSortOrder, GetThemeMode, GetShowTitleBar } from '../../../wailsjs/go/app/App'

export type MessageListDensity = 'micro' | 'compact' | 'standard' | 'large'
export type MessageListSortOrder = 'newest' | 'oldest'
export type ThemeMode =
  | 'system'
  | 'light' | 'light-blue' | 'light-orange'
  | 'dark' | 'dark-gray'

// Module-level reactive state
let messageListDensity = $state<MessageListDensity>('standard')
let messageListSortOrder = $state<MessageListSortOrder>('newest')
let themeMode = $state<ThemeMode>('system')
let showTitleBar = $state<boolean>(true)

// Getter functions to access the state
export function getMessageListDensity(): MessageListDensity {
  return messageListDensity
}

export function getMessageListSortOrder(): MessageListSortOrder {
  return messageListSortOrder
}

export function getThemeMode(): ThemeMode {
  return themeMode
}

export function getShowTitleBar(): boolean {
  return showTitleBar
}

// Setter functions to update the state
export function setMessageListDensity(density: MessageListDensity) {
  messageListDensity = density
}

export function setMessageListSortOrder(sortOrder: MessageListSortOrder) {
  messageListSortOrder = sortOrder
}

export function setThemeMode(mode: ThemeMode) {
  themeMode = mode
}

export function setShowTitleBar(show: boolean) {
  showTitleBar = show
}

// Load settings from backend (call on app startup)
export async function loadSettings(): Promise<ThemeMode> {
  try {
    const [density, sortOrder, theme, titleBar] = await Promise.all([
      GetMessageListDensity(),
      GetMessageListSortOrder(),
      GetThemeMode(),
      GetShowTitleBar(),
    ])
    messageListDensity = (density as MessageListDensity) || 'standard'
    messageListSortOrder = (sortOrder as MessageListSortOrder) || 'newest'
    themeMode = (theme as ThemeMode) || 'system'
    showTitleBar = titleBar ?? true // Default to true
    return themeMode
  } catch (err) {
    console.error('Failed to load settings:', err)
    return 'system'
  }
}
