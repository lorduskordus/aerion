<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import Icon from '@iconify/svelte'
  import ConversationRow from './ConversationRow.svelte'
  import { DropdownMenu } from 'bits-ui'
  import { cn } from '$lib/utils'
  // @ts-ignore - wailsjs bindings
  import { GetConversations, GetConversationCount, SyncFolder, ForceSyncFolder, CancelFolderSync, SetMessageListSortOrder, GetUnifiedInboxConversations, GetUnifiedInboxCount, SearchConversations, SearchUnifiedInbox, GetSearchCount, GetSearchCountUnifiedInbox, GetFTSIndexStatus, IsFTSIndexing } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import { message } from '../../../../wailsjs/go/models'
  // @ts-ignore - wailsjs runtime
  import { EventsOn, EventsOff } from '../../../../wailsjs/runtime/runtime'
  import { getMessageListDensity, getMessageListSortOrder, setMessageListSortOrder } from '$lib/stores/settings.svelte'
  import { accountStore } from '$lib/stores/accounts.svelte'

  interface Props {
    accountId?: string | null
    folderId?: string | null
    folderName?: string
    folderType?: string
    onConversationSelect?: (threadId: string, folderId: string, accountId: string) => void
    onReply?: (mode: 'reply' | 'reply-all' | 'forward', messageId: string) => void
    isFocused?: boolean
    isFlashing?: boolean
  }

  let {
    accountId = null,
    folderId = null,
    folderName = 'Inbox',
    folderType = 'inbox',
    onConversationSelect,
    onReply,
    isFocused = false,
    isFlashing = false,
  }: Props = $props()

  // State
  let conversations = $state<message.Conversation[]>([])
  let totalCount = $state(0)
  let loading = $state(false)
  let error = $state<string | null>(null)
  let selectedThreadId = $state<string | null>(null)
  let lastLoadedFolderId = $state<string | null>(null) // Track folder changes

  // Derived: check if this folder is currently syncing (from account store's progress tracking)
  const syncing = $derived(
    !!(accountId && folderId && accountStore.syncProgress[accountId]?.[folderId] !== undefined)
  )

  // Derived: get sync progress for this folder (if syncing)
  const syncProgress = $derived(
    accountId && folderId 
      ? accountStore.syncProgress[accountId]?.[folderId] 
      : null
  )

  // Multi-select state
  let checkedThreadIds = $state<Set<string>>(new Set())
  let lastClickedIndex = $state<number | null>(null)

  // Pagination
  const PAGE_SIZE = 50
  let offset = $state(0)

  // Debounce timer for reloading after flag changes
  let reloadTimer: ReturnType<typeof setTimeout> | null = null

  // Search state
  let showSearch = $state(false)
  let searchQuery = $state('')
  let searchResults = $state<any[]>([])  // ConversationSearchResult from backend
  let searchTotalCount = $state(0)
  let searchOffset = $state(0)
  let isSearching = $state(false)
  let searchDebounceTimer: ReturnType<typeof setTimeout> | null = null

  // FTS indexing state
  let indexProgress = $state(0)
  let indexComplete = $state(true)
  let isIndexing = $state(false)
  let searchInputRef = $state<HTMLInputElement | null>(null)

  // Listen for folder sync events from backend
  onMount(() => {
    EventsOn('folder:synced', (data: { accountId: string; folderId: string }) => {
      // Reload if this is the current folder or unified inbox (any inbox sync should refresh unified)
      if (isUnifiedView || (accountId && folderId && data.accountId === accountId && data.folderId === folderId)) {
        // Preserve loaded messages count, load at least PAGE_SIZE
        const totalLoaded = Math.max(conversations.length, PAGE_SIZE)
        offset = 0
        loadConversations(totalLoaded)
      }
    })

    // Listen for messages:updated events (e.g., from IDLE push notifications)
    EventsOn('messages:updated', (data: { accountId: string; folderId: string }) => {
      // Reload if this is the current folder or unified inbox
      if (isUnifiedView || (accountId && folderId && data.accountId === accountId && data.folderId === folderId)) {
        // Preserve loaded messages count, load at least PAGE_SIZE
        const totalLoaded = Math.max(conversations.length, PAGE_SIZE)
        offset = 0
        loadConversations(totalLoaded)
      }
    })

    // Listen for message flag changes (e.g., marked as read)
    EventsOn('messages:flagsChanged', (data: { messageIds: string[], isRead: boolean }) => {
      // Update conversations locally instead of reloading from DB
      for (const c of conversations) {
        const affectedCount = (c.messageIds || []).filter(id => data.messageIds.includes(id)).length
        if (affectedCount > 0) {
          const delta = data.isRead ? -affectedCount : affectedCount
          c.unreadCount = Math.max(0, (c.unreadCount || 0) + delta)
        }
      }
      // Trigger reactivity by reassigning
      conversations = conversations
    })

    // Listen for FTS indexing progress
    EventsOn('fts:progress', (data: { folderId: string; indexed: number; total: number; percentage: number }) => {
      if (folderId && data.folderId === folderId) {
        indexProgress = data.percentage
        indexComplete = false
        isIndexing = true
      }
    })

    // Listen for FTS indexing completion
    EventsOn('fts:complete', (data: { folderId: string }) => {
      if (folderId && data.folderId === folderId) {
        indexComplete = true
        isIndexing = false
        indexProgress = 100
      }
    })

    // Listen for FTS indexing status changes
    EventsOn('fts:indexing', (data: { status: string }) => {
      if (data.status === 'completed') {
        indexComplete = true
        isIndexing = false
      } else if (data.status === 'started') {
        isIndexing = true
      }
    })

    // Check initial FTS index status for current folder
    checkFTSIndexStatus()
  })

  onDestroy(() => {
    EventsOff('folder:synced')
    EventsOff('messages:updated')
    EventsOff('messages:flagsChanged')
    EventsOff('fts:progress')
    EventsOff('fts:complete')
    EventsOff('fts:indexing')
    if (reloadTimer) clearTimeout(reloadTimer)
    if (searchDebounceTimer) clearTimeout(searchDebounceTimer)
  })

  // Check FTS index status for current folder
  async function checkFTSIndexStatus() {
    if (!folderId) return
    try {
      const status = await GetFTSIndexStatus(folderId)
      if (status) {
        indexComplete = status.isComplete
        if (status.totalCount > 0) {
          indexProgress = Math.round((status.indexedCount / status.totalCount) * 100)
        }
      }
      isIndexing = await IsFTSIndexing()
    } catch (err) {
      console.error('Failed to check FTS index status:', err)
    }
  }

  // Track previous folder to detect actual changes
  let prevAccountId: string | null = null
  let prevFolderId: string | null = null

  // Clear selection and search when folder changes
  $effect(() => {
    const currentAccount = isUnifiedView ? 'unified' : accountId
    const currentFolder = isUnifiedView ? 'inbox' : folderId

    if (isUnifiedView || (accountId && folderId)) {
      // Only reset and reload if folder actually changed
      if (currentAccount !== prevAccountId || currentFolder !== prevFolderId) {
        prevAccountId = currentAccount
        prevFolderId = currentFolder
        offset = 0
        checkedThreadIds = new Set()
        lastClickedIndex = null
        // Clear search state when folder changes
        showSearch = false
        searchQuery = ''
        searchResults = []
        searchTotalCount = 0
        searchOffset = 0
        loadConversations()
        checkFTSIndexStatus()
      }
    } else {
      prevAccountId = null
      prevFolderId = null
      conversations = []
      totalCount = 0
      checkedThreadIds = new Set()
    }
  })

  // Compute selected message IDs from all checked conversations (for multi-select context menu)
  // Check both conversations and searchResults since selections can span both
  // Use Set to deduplicate in case same conversation appears in both arrays
  const selectedMessageIds = $derived(
    [...new Set(
      [...conversations, ...searchResults]
        .filter((c) => checkedThreadIds.has(c.threadId))
        .flatMap((c: any) => c.messageIds || c.messages?.map((m: any) => m.id) || [])
    )]
  )

  // Aggregated star/read state for multi-select context menu
  // Show "Star" if any selected is unstarred, show "Mark as Read" if any selected is unread
  const selectedHasUnstarred = $derived(
    [...conversations, ...searchResults]
      .filter((c) => checkedThreadIds.has(c.threadId))
      .some((c: any) => !c.isStarred)
  )
  const selectedHasUnread = $derived(
    [...conversations, ...searchResults]
      .filter((c) => checkedThreadIds.has(c.threadId))
      .some((c: any) => (c.unreadCount || 0) > 0)
  )

  // Clear multi-select (called when right-clicking on unchecked row)
  function clearSelection() {
    checkedThreadIds = new Set()
    lastClickedIndex = null
  }

  // Check if viewing unified inbox
  const isUnifiedView = $derived(accountId === 'unified' && folderId === 'inbox')

  async function loadConversations(customLimit?: number) {
    // For unified view, we don't need accountId/folderId
    if (!isUnifiedView && (!accountId || !folderId)) return

    // Prevent concurrent loads
    if (loading) return

    loading = true
    error = null

    // Capture offset at start - it may change during async operations
    const currentOffset = offset
    const limit = customLimit ?? PAGE_SIZE

    try {
      let convList: message.Conversation[]
      let count: number

      if (isUnifiedView) {
        // Load from unified inbox
        [convList, count] = await Promise.all([
          GetUnifiedInboxConversations(currentOffset, limit, getMessageListSortOrder()),
          GetUnifiedInboxCount(),
        ])
      } else {
        // Load from specific folder
        [convList, count] = await Promise.all([
          GetConversations(accountId!, folderId!, currentOffset, limit, getMessageListSortOrder()),
          GetConversationCount(accountId!, folderId!),
        ])
      }

      if (currentOffset === 0) {
        conversations = convList || []

        // Check if we switched to a different folder
        const folderChanged = lastLoadedFolderId !== folderId
        lastLoadedFolderId = folderId

        // Auto-select first message on folder navigation or initial load
        if (conversations.length > 0) {
          // If folder changed, always auto-select first message
          // If same folder (refresh), keep existing selection
          if (folderChanged || !selectedThreadId) {
            selectedThreadId = conversations[0].threadId
          }
        } else {
          selectedThreadId = null
        }
      } else {
        conversations = [...conversations, ...(convList || [])]
      }
      totalCount = count
    } catch (err) {
      error = err instanceof Error ? err.message : String(err)
      console.error('Failed to load conversations:', err)
    } finally {
      loading = false
    }
  }

  export async function syncFolder() {
    // Can't sync unified inbox directly - individual folders must be synced
    if (isUnifiedView || !accountId || !folderId) return

    error = null

    try {
      // SyncFolder returns after headers sync, but body fetch continues in background
      // The account store tracks sync:progress and folder:synced events to manage syncing state
      await SyncFolder(accountId, folderId)
      offset = 0
      await loadConversations()
    } catch (err) {
      error = err instanceof Error ? err.message : String(err)
      console.error('Failed to sync folder:', err)
    }
    // No need to manage syncing state - account store handles it via events
  }

  // Cancel folder sync
  export async function cancelFolderSync() {
    if (isUnifiedView || !accountId || !folderId) return

    try {
      await CancelFolderSync(accountId, folderId)
    } catch (err) {
      console.error('Failed to cancel folder sync:', err)
    }
  }

  // Toggle folder sync (start if not running, cancel if running) - for keyboard shortcut and UI
  export async function toggleFolderSync() {
    if (syncing) {
      await cancelFolderSync()
    } else {
      await syncFolder()
    }
  }

  // Force re-sync folder (clears bodies & attachments, then re-fetches)
  async function forceSyncFolder() {
    if (isUnifiedView || !accountId || !folderId) return

    error = null

    try {
      await ForceSyncFolder(accountId, folderId)
      offset = 0
      await loadConversations()
    } catch (err) {
      error = err instanceof Error ? err.message : String(err)
      console.error('Failed to force re-sync folder:', err)
    }
  }

  // Handle search input with debounce
  function handleSearchInput() {
    if (searchDebounceTimer) clearTimeout(searchDebounceTimer)
    
    if (!searchQuery.trim()) {
      // Clear search immediately if query is empty
      searchResults = []
      searchTotalCount = 0
      return
    }

    searchDebounceTimer = setTimeout(() => {
      performSearch()
    }, 300)
  }

  // Perform the actual search
  async function performSearch() {
    const query = searchQuery.trim()
    if (!query) {
      searchResults = []
      searchTotalCount = 0
      searchOffset = 0
      return
    }

    // Don't start a new search if one is already in progress
    if (isSearching) return

    isSearching = true
    error = null
    searchOffset = 0  // Reset offset for new search

    try {
      let results: any[]
      let count: number

      if (isUnifiedView) {
        [results, count] = await Promise.all([
          SearchUnifiedInbox(query, 0, PAGE_SIZE),
          GetSearchCountUnifiedInbox(query),
        ])
      } else if (accountId && folderId) {
        [results, count] = await Promise.all([
          SearchConversations(accountId, folderId, query, 0, PAGE_SIZE),
          GetSearchCount(accountId, folderId, query),
        ])
      } else {
        results = []
        count = 0
      }

      searchResults = results || []
      searchTotalCount = count
      // Auto-select first search result for keyboard navigation
      if (searchResults.length > 0) {
        selectedThreadId = searchResults[0].threadId
      }
    } catch (err) {
      error = err instanceof Error ? err.message : String(err)
      console.error('Search failed:', err)
    } finally {
      isSearching = false
    }
  }

  // Load more search results (pagination)
  async function loadMoreSearchResults() {
    const query = searchQuery.trim()
    if (!query || isSearching) return

    // Cancel any pending search debounce to prevent race conditions
    if (searchDebounceTimer) {
      clearTimeout(searchDebounceTimer)
      searchDebounceTimer = null
    }

    isSearching = true
    const newOffset = searchOffset + PAGE_SIZE

    try {
      let results: any[]

      if (isUnifiedView) {
        results = await SearchUnifiedInbox(query, newOffset, PAGE_SIZE)
      } else if (accountId && folderId) {
        results = await SearchConversations(accountId, folderId, query, newOffset, PAGE_SIZE)
      } else {
        results = []
      }

      if (results && results.length > 0) {
        searchResults = [...searchResults, ...results]
        searchOffset = newOffset
      }
    } catch (err) {
      error = err instanceof Error ? err.message : String(err)
      console.error('Load more search results failed:', err)
    } finally {
      isSearching = false
    }
  }

  // Clear search and return to normal view
  function clearSearch() {
    searchQuery = ''
    searchResults = []
    searchTotalCount = 0
    searchOffset = 0
    showSearch = false
    if (searchDebounceTimer) clearTimeout(searchDebounceTimer)
  }

  // Handle keyboard events in search input
  function handleSearchKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      clearSearch()
    } else if (event.key === 'Enter') {
      // Move focus from search input to message list so user can navigate with arrow keys
      event.preventDefault()
      searchInputRef?.blur()
      listContainerRef?.focus()
    }
  }

  // Toggle search visibility
  function toggleSearch() {
    showSearch = !showSearch
    if (showSearch) {
      // Focus input after it appears
      setTimeout(() => searchInputRef?.focus(), 50)
    } else {
      clearSearch()
    }
  }

  // Check if we're in search mode with results
  const isSearchMode = $derived(showSearch && searchQuery.trim().length > 0)

  // Active list - either conversations or search results depending on mode
  const activeList = $derived(isSearchMode ? searchResults : conversations)
  const activeCount = $derived(isSearchMode ? searchTotalCount : totalCount)

  function selectConversation(threadId: string, index: number, event?: MouseEvent) {
    // Handle multi-select with Shift/Ctrl/Cmd
    if (event?.shiftKey) {
      // Range select from lastClickedIndex (or current if none) to current
      const start = lastClickedIndex !== null ? Math.min(lastClickedIndex, index) : index
      const end = lastClickedIndex !== null ? Math.max(lastClickedIndex, index) : index
      const newChecked = new Set(checkedThreadIds)
      for (let i = start; i <= end; i++) {
        newChecked.add(activeList[i].threadId)
      }
      checkedThreadIds = newChecked
      // Don't change selectedThreadId or notify parent - keep current view
    } else if (event?.ctrlKey || event?.metaKey) {
      // Toggle single checkbox without changing selection
      const newChecked = new Set(checkedThreadIds)
      if (newChecked.has(threadId)) {
        newChecked.delete(threadId)
      } else {
        newChecked.add(threadId)
      }
      checkedThreadIds = newChecked
      // Don't change selectedThreadId - keep current view
    } else {
      // Normal click - select for viewing, clear checks (don't auto-check)
      checkedThreadIds = new Set()
      selectedThreadId = threadId

      // For unified view or search, use real folderId and accountId from conversation data
      const conversation = activeList[index] as any
      const realFolderId = (isUnifiedView || isSearchMode) && conversation.folderId ? conversation.folderId : folderId!
      const realAccountId = (isUnifiedView || isSearchMode) && conversation.accountId ? conversation.accountId : accountId!
      onConversationSelect?.(threadId, realFolderId, realAccountId)
    }
    lastClickedIndex = index
  }

  function handleCheck(threadId: string, isChecked: boolean) {
    const newChecked = new Set(checkedThreadIds)
    if (isChecked) {
      newChecked.add(threadId)
    } else {
      newChecked.delete(threadId)
    }
    checkedThreadIds = newChecked
  }

  export function handleActionComplete(autoSelectNext: boolean = false) {
    // Get current selection index BEFORE reload (for auto-select after delete/archive/spam)
    const currentIndex = getSelectedIndex()
    const scrollTop = listContainerRef?.scrollTop ?? 0

    // If in search mode, refresh search results instead of conversations
    if (isSearchMode) {
      performSearch().then(() => {
        // Restore scroll position
        if (listContainerRef) {
          requestAnimationFrame(() => {
            listContainerRef!.scrollTop = scrollTop
          })
        }

        // Auto-select next message if requested
        if (autoSelectNext && currentIndex >= 0 && searchResults.length > 0) {
          const newIndex = Math.min(currentIndex, searchResults.length - 1)
          const conv = searchResults[newIndex]
          if (conv) {
            selectConversation(conv.threadId, newIndex)
          }
        }
      })
      return
    }

    // Preserve loaded messages: reload all messages that were loaded
    // Use conversations.length to track actual loaded count (offset gets reset after first action)
    const totalLoaded = Math.max(conversations.length, PAGE_SIZE)
    offset = 0

    loadConversations(totalLoaded).then(() => {
      // Restore scroll position
      if (listContainerRef) {
        requestAnimationFrame(() => {
          listContainerRef!.scrollTop = scrollTop
        })
      }

      // Auto-select next message if requested (for delete/archive/spam actions)
      // After reload, the same index now points to what was the "next" message
      if (autoSelectNext && currentIndex >= 0 && conversations.length > 0) {
        const newIndex = Math.min(currentIndex, conversations.length - 1)
        const conv = conversations[newIndex]
        if (conv) {
          selectConversation(conv.threadId, newIndex)
        }
      }
    })
  }

  // Toggle sort order and persist to backend
  async function toggleSortOrder() {
    const newOrder = getMessageListSortOrder() === 'newest' ? 'oldest' : 'newest'
    try {
      await SetMessageListSortOrder(newOrder)
      setMessageListSortOrder(newOrder)
      offset = 0
      loadConversations()
    } catch (err) {
      console.error('Failed to save sort order:', err)
    }
  }

  // Calculate total unread count
  const unreadCount = $derived(
    conversations.reduce((sum, c) => sum + (c.unreadCount || 0), 0)
  )

  // Reference to the list container for scrolling
  let listContainerRef = $state<HTMLDivElement | null>(null)

  // Reference to the "Load more" button for keyboard navigation
  let loadMoreButtonRef = $state<HTMLButtonElement | null>(null)

  // Get current selected index
  function getSelectedIndex(): number {
    if (!selectedThreadId) return -1
    return activeList.findIndex(c => c.threadId === selectedThreadId)
  }

  // Select previous message (exposed for keyboard navigation)
  // Just moves focus, doesn't clear checkboxes or open in viewer
  export function selectPrevious() {
    if (activeList.length === 0) return

    const currentIndex = getSelectedIndex()
    const newIndex = currentIndex <= 0 ? 0 : currentIndex - 1

    const conv = activeList[newIndex]
    if (conv) {
      selectedThreadId = conv.threadId
      scrollToIndex(newIndex)
      // Blur any focused element so Enter key triggers openSelected() instead of the button
      ;(document.activeElement as HTMLElement)?.blur?.()
    }
  }

  // Select next message (exposed for keyboard navigation)
  // Just moves focus, doesn't clear checkboxes or open in viewer
  export function selectNext() {
    if (activeList.length === 0) return

    const currentIndex = getSelectedIndex()

    // If at last message and more are available, focus the "Load more" button
    if (currentIndex >= activeList.length - 1 && activeList.length < activeCount) {
      loadMoreButtonRef?.focus()
      return
    }

    const newIndex = currentIndex >= activeList.length - 1 ? activeList.length - 1 : currentIndex + 1

    const conv = activeList[newIndex]
    if (conv) {
      selectedThreadId = conv.threadId
      scrollToIndex(newIndex)
      // Blur any focused element so Enter key triggers openSelected() instead of the button
      ;(document.activeElement as HTMLElement)?.blur?.()
    }
  }

  // Open the currently selected conversation (exposed for keyboard navigation)
  export function openSelected() {
    if (!selectedThreadId) return

    const index = getSelectedIndex()
    if (index >= 0) {
      const conv = activeList[index] as any
      const realFolderId = (isUnifiedView || isSearchMode) && conv.folderId ? conv.folderId : folderId!
      const realAccountId = (isUnifiedView || isSearchMode) && conv.accountId ? conv.accountId : accountId!
      onConversationSelect?.(selectedThreadId, realFolderId, realAccountId)
    }
  }

  // Select a specific thread by ID (exposed for notification clicks)
  export function selectThread(threadId: string) {
    selectedThreadId = threadId
    const index = activeList.findIndex(c => c.threadId === threadId)
    if (index >= 0) {
      scrollToIndex(index)
    }
  }

  // Focus the search input (exposed for keyboard navigation)
  export function focusSearch() {
    showSearch = true
    setTimeout(() => searchInputRef?.focus(), 50)
  }

  // Get the currently selected thread ID (exposed for parent access)
  export function getSelectedThreadId(): string | null {
    return selectedThreadId
  }

  // Get message IDs for the keyboard-focused thread (for delete without checking)
  export function getSelectedMessageIds(): string[] {
    if (!selectedThreadId) return []
    const conv = activeList.find(c => c.threadId === selectedThreadId) as any
    if (!conv) return []
    return conv.messageIds || conv.messages?.map((m: any) => m.id) || []
  }

  // Get account and folder info for the keyboard-focused thread (for unified inbox)
  export function getSelectedConversationInfo(): { accountId: string; folderId: string } | null {
    if (!selectedThreadId) return null
    const conv = activeList.find(c => c.threadId === selectedThreadId) as any
    if (!conv) return null

    const realAccountId = (isUnifiedView || isSearchMode) && conv.accountId ? conv.accountId : accountId
    const realFolderId = (isUnifiedView || isSearchMode) && conv.folderId ? conv.folderId : folderId

    if (!realAccountId || !realFolderId) return null
    return { accountId: realAccountId, folderId: realFolderId }
  }

  // Check if the keyboard-focused thread is starred
  export function isSelectedStarred(): boolean {
    if (!selectedThreadId) return false
    const conv = activeList.find(c => c.threadId === selectedThreadId) as any
    return conv?.isStarred ?? false
  }

  // Toggle checkbox for focused message (Space key)
  export function toggleCheck() {
    if (!selectedThreadId) return
    const newChecked = new Set(checkedThreadIds)
    if (newChecked.has(selectedThreadId)) {
      newChecked.delete(selectedThreadId)
    } else {
      newChecked.add(selectedThreadId)
    }
    checkedThreadIds = newChecked
    lastClickedIndex = getSelectedIndex()
  }

  // Select previous message AND check both current and previous (Shift+Up/k)
  export function selectPreviousWithCheck() {
    if (activeList.length === 0) return

    const currentIndex = getSelectedIndex()
    if (currentIndex <= 0) return  // Already at top or no selection

    const newIndex = currentIndex - 1
    const conv = activeList[newIndex]
    if (!conv) return

    // Check both current and new message
    const newChecked = new Set(checkedThreadIds)
    newChecked.add(activeList[currentIndex].threadId)
    newChecked.add(conv.threadId)
    checkedThreadIds = newChecked

    // Move focus (but don't open in viewer)
    selectedThreadId = conv.threadId
    scrollToIndex(newIndex)
    // Blur any focused element so Enter key triggers openSelected() instead of the button
    ;(document.activeElement as HTMLElement)?.blur?.()
  }

  // Select next message AND check both current and next (Shift+Down/j)
  export function selectNextWithCheck() {
    if (activeList.length === 0) return

    const currentIndex = getSelectedIndex()
    if (currentIndex < 0 || currentIndex >= activeList.length - 1) return  // No selection or already at bottom

    const newIndex = currentIndex + 1
    const conv = activeList[newIndex]
    if (!conv) return

    // Check both current and new message
    const newChecked = new Set(checkedThreadIds)
    newChecked.add(activeList[currentIndex].threadId)
    newChecked.add(conv.threadId)
    checkedThreadIds = newChecked

    // Move focus (but don't open in viewer)
    selectedThreadId = conv.threadId
    scrollToIndex(newIndex)
    // Blur any focused element so Enter key triggers openSelected() instead of the button
    ;(document.activeElement as HTMLElement)?.blur?.()
  }

  // Get all checked message IDs for bulk operations
  export function getCheckedMessageIds(): string[] {
    return selectedMessageIds
  }

  // Check if any messages are checked
  export function hasCheckedMessages(): boolean {
    return checkedThreadIds.size > 0
  }

  // Get aggregated star state (true if any unstarred)
  export function getCheckedHasUnstarred(): boolean {
    return selectedHasUnstarred
  }

  // Get aggregated read state (true if any unread)
  export function getCheckedHasUnread(): boolean {
    return selectedHasUnread
  }

  // Clear all checkboxes
  export function clearChecked() {
    checkedThreadIds = new Set()
    lastClickedIndex = null
  }

  // Scroll to a specific index in the list
  function scrollToIndex(index: number) {
    if (!listContainerRef) return
    
    const rows = listContainerRef.querySelectorAll('[data-conversation-row]')
    const row = rows[index] as HTMLElement | undefined
    if (row) {
      row.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }
  }
</script>

<div class="flex flex-col h-full {isFlashing ? 'pane-focus-flash' : ''}">
  <!-- Header -->
  <div class="flex items-center justify-between px-4 py-3 border-b border-border">
    <div class="flex items-center gap-2">
      {#if showSearch}
        <!-- Search input -->
        <div class="flex items-center gap-1 bg-muted rounded-md px-2 flex-1">
          <Icon icon="mdi:magnify" class="w-4 h-4 text-muted-foreground flex-shrink-0" />
          <input
            bind:this={searchInputRef}
            type="text"
            placeholder="Search messages..."
            class="bg-transparent border-none outline-none text-sm py-1.5 w-full min-w-[200px]"
            bind:value={searchQuery}
            oninput={handleSearchInput}
            onkeydown={handleSearchKeydown}
          />
          {#if searchQuery || isSearching}
            <button 
              onclick={clearSearch}
              class="p-0.5 hover:bg-muted-foreground/20 rounded"
              title="Clear search"
            >
              {#if isSearching}
                <Icon icon="mdi:loading" class="w-4 h-4 animate-spin text-muted-foreground" />
              {:else}
                <Icon icon="mdi:close" class="w-4 h-4 text-muted-foreground" />
              {/if}
            </button>
          {/if}
        </div>
      {:else}
        <h2 class="font-semibold text-foreground">{folderName}</h2>
        <span class="text-sm text-muted-foreground">
          ({unreadCount} unread)
        </span>
      {/if}
    </div>
    <div class="flex items-center gap-1">
      {#if syncing}
        <!-- While syncing, show spinning icon that cancels on click -->
        <button
          class="p-2 rounded-md hover:bg-muted transition-colors"
          title={syncProgress ? `Syncing ${syncProgress.phase}: ${syncProgress.percentage}% - Click to cancel` : 'Syncing... Click to cancel'}
          onclick={cancelFolderSync}
        >
          <Icon
            icon="mdi:refresh"
            class="w-5 h-5 text-muted-foreground animate-spin"
          />
        </button>
      {:else}
        <!-- Dropdown menu for sync options -->
        <DropdownMenu.Root>
          <DropdownMenu.Trigger
            class="p-2 rounded-md hover:bg-muted transition-colors disabled:opacity-50"
            disabled={loading || isUnifiedView}
          >
            <Icon
              icon="mdi:refresh"
              class="w-5 h-5 text-muted-foreground"
            />
          </DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content
              side="bottom"
              align="end"
              sideOffset={4}
              class={cn(
                'z-50 min-w-[180px] rounded-md border bg-popover p-1 text-popover-foreground shadow-md',
                'data-[state=open]:animate-in data-[state=closed]:animate-out',
                'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
                'data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
                'data-[side=bottom]:slide-in-from-top-2'
              )}
            >
              <DropdownMenu.Item
                onSelect={syncFolder}
                class="relative flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none focus:bg-accent focus:text-accent-foreground"
              >
                <Icon icon="mdi:refresh" class="w-4 h-4 mr-2" />
                Sync Folder
              </DropdownMenu.Item>
              <DropdownMenu.Separator class="-mx-1 my-1 h-px bg-border" />
              <DropdownMenu.Item
                onSelect={forceSyncFolder}
                class="relative flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none focus:bg-accent focus:text-accent-foreground"
              >
                <Icon icon="mdi:refresh-auto" class="w-4 h-4 mr-2" />
                Force Re-sync
              </DropdownMenu.Item>
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>
      {/if}
      <button
        class="p-2 rounded-md hover:bg-muted transition-colors {showSearch ? 'bg-muted' : ''}"
        title={showSearch ? 'Close search' : 'Search'}
        onclick={toggleSearch}
      >
        <Icon icon={showSearch ? 'mdi:close' : 'mdi:magnify'} class="w-5 h-5 text-muted-foreground" />
      </button>
      <button
        class="p-2 rounded-md hover:bg-muted transition-colors"
        title={getMessageListSortOrder() === 'newest' ? 'Showing newest first' : 'Showing oldest first'}
        onclick={toggleSortOrder}
      >
        <Icon
          icon={getMessageListSortOrder() === 'newest' ? 'mdi:sort-descending' : 'mdi:sort-ascending'}
          class="w-5 h-5 text-muted-foreground"
        />
      </button>
    </div>
  </div>

  <!-- FTS Indexing indicator (only shown when searching and index is incomplete) -->
  {#if showSearch && !indexComplete && isIndexing}
    <div class="px-4 py-2 bg-muted/50 border-b border-border">
      <div class="flex items-center gap-2 text-sm text-muted-foreground">
        <Icon icon="mdi:database-sync" class="w-4 h-4 animate-pulse" />
        <span>Building search index... ({indexProgress}%)</span>
      </div>
      <div class="h-1 bg-muted rounded-full mt-1.5 overflow-hidden">
        <div 
          class="h-full bg-primary transition-all duration-300" 
          style="width: {indexProgress}%"
        ></div>
      </div>
    </div>
  {/if}

  <!-- Conversation List -->
  <div bind:this={listContainerRef} class="flex-1 overflow-y-auto scrollbar-thin">
    {#if loading && conversations.length === 0 && !isSearchMode}
      <div class="flex items-center justify-center h-32">
        <Icon icon="mdi:loading" class="w-6 h-6 animate-spin text-muted-foreground" />
      </div>
    {:else if error}
      <div class="flex flex-col items-center justify-center h-32 text-center px-4">
        <Icon icon="mdi:alert-circle-outline" class="w-8 h-8 text-destructive mb-2" />
        <p class="text-sm text-destructive">{error}</p>
        <button
          class="mt-2 text-sm text-primary hover:underline"
          onclick={() => isSearchMode ? performSearch() : loadConversations()}
        >
          Try again
        </button>
      </div>
    {:else if !isUnifiedView && (!accountId || !folderId)}
      <div class="flex flex-col items-center justify-center h-full text-muted-foreground">
        <Icon icon="mdi:email-outline" class="w-12 h-12 mb-2" />
        <p>Select a folder to view messages</p>
      </div>
    {:else if isSearchMode}
      <!-- Search Results -->
      {#if isSearching}
        <div class="flex items-center justify-center h-32">
          <Icon icon="mdi:loading" class="w-6 h-6 animate-spin text-muted-foreground" />
        </div>
      {:else if searchResults.length === 0}
        <div class="flex flex-col items-center justify-center h-full text-muted-foreground">
          <Icon icon="mdi:magnify" class="w-12 h-12 mb-2" />
          <p>No results found for "{searchQuery}"</p>
          {#if !indexComplete}
            <p class="text-xs mt-1">Search index is still building...</p>
          {/if}
        </div>
      {:else}
        <!-- Search results header -->
        <div class="px-4 py-2 bg-muted/30 border-b border-border text-sm text-muted-foreground">
          Found {searchTotalCount} result{searchTotalCount !== 1 ? 's' : ''} for "{searchQuery}"
        </div>
        {#each searchResults as result, index (result.threadId + '-' + index)}
          {@const resultAccountId = result.accountId || accountId}
          {@const resultFolderId = result.folderId || folderId}
          {@const resultAccountColor = result.accountColor || ''}
          {@const resultAccountName = result.accountName || ''}
          <ConversationRow
            conversation={result}
            density={getMessageListDensity()}
            selected={selectedThreadId === result.threadId}
            checked={checkedThreadIds.has(result.threadId)}
            accountId={isUnifiedView ? resultAccountId : accountId!}
            folderId={isUnifiedView ? resultFolderId : folderId!}
            {folderType}
            {selectedMessageIds}
            selectedIsStarred={!selectedHasUnstarred}
            selectedIsRead={!selectedHasUnread}
            showAccountIndicator={isUnifiedView}
            accountColor={resultAccountColor}
            accountName={resultAccountName}
            highlightedSubject={result.highlightedSubject}
            highlightedSnippet={result.highlightedSnippet}
            highlightedFromName={result.highlightedFromName}
            searchFolderName={result.folderName}
            searchFolderType={result.folderType}
            onSelect={(e) => selectConversation(result.threadId, index, e)}
            onCheck={(checked) => handleCheck(result.threadId, checked)}
            onClearSelection={clearSelection}
            onActionComplete={handleActionComplete}
            {onReply}
          />
        {/each}

        <!-- Load more search results -->
        {#if searchResults.length < searchTotalCount}
          <div class="flex justify-center py-4">
            <button
              bind:this={loadMoreButtonRef}
              class="text-sm text-primary hover:underline focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 rounded px-2 py-1"
              onclick={() => loadMoreSearchResults()}
              disabled={isSearching}
            >
              {isSearching ? 'Loading...' : `Load more (${searchTotalCount - searchResults.length} remaining)`}
            </button>
          </div>
        {/if}
      {/if}
    {:else if conversations.length === 0}
      <div class="flex flex-col items-center justify-center h-full text-muted-foreground">
        <Icon icon="mdi:inbox-outline" class="w-12 h-12 mb-2" />
        <p>No messages in this folder</p>
        <button
          class="mt-2 text-sm text-primary hover:underline"
          onclick={syncFolder}
          disabled={syncing}
        >
          Sync now
        </button>
      </div>
    {:else}
      {#each conversations as conv, index (conv.threadId)}
        {@const convAccountId = (conv as any).accountId || accountId}
        {@const convFolderId = (conv as any).folderId || folderId}
        {@const convAccountColor = (conv as any).accountColor || ''}
        {@const convAccountName = (conv as any).accountName || ''}
        <ConversationRow
          conversation={conv}
          density={getMessageListDensity()}
          selected={selectedThreadId === conv.threadId}
          checked={checkedThreadIds.has(conv.threadId)}
          accountId={isUnifiedView ? convAccountId : accountId!}
          folderId={isUnifiedView ? convFolderId : folderId!}
          {folderType}
          {selectedMessageIds}
          selectedIsStarred={!selectedHasUnstarred}
          selectedIsRead={!selectedHasUnread}
          showAccountIndicator={isUnifiedView}
          accountColor={convAccountColor}
          accountName={convAccountName}
          onSelect={(e) => selectConversation(conv.threadId, index, e)}
          onCheck={(checked) => handleCheck(conv.threadId, checked)}
          onClearSelection={clearSelection}
          onActionComplete={handleActionComplete}
          {onReply}
        />
      {/each}

      <!-- Load more button for pagination -->
      {#if conversations.length < totalCount}
        <div class="flex justify-center py-4">
          <button
            bind:this={loadMoreButtonRef}
            class="text-sm text-primary hover:underline focus:outline-none focus:ring-2 focus:ring-primary focus:ring-offset-2 rounded px-2 py-1"
            onclick={() => {
              offset += PAGE_SIZE
              loadConversations()
            }}
            disabled={loading}
          >
            {loading ? 'Loading...' : `Load more (${totalCount - conversations.length} remaining)`}
          </button>
        </div>
      {/if}
    {/if}
  </div>
</div>
