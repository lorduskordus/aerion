<script lang="ts">
  import { onMount } from 'svelte'
  import TitleBar from './lib/components/common/TitleBar.svelte'
  import Sidebar from './lib/components/sidebar/Sidebar.svelte'
  import MessageList from './lib/components/list/MessageList.svelte'
  import ConversationViewer from './lib/components/viewer/ConversationViewer.svelte'
  import Composer from './lib/components/composer/Composer.svelte'
  import ToastContainer from './lib/components/ui/toast/ToastContainer.svelte'
  import TermsDialog from './lib/components/TermsDialog.svelte'
  import { accountStore } from '$lib/stores/accounts.svelte'
  import { addToast } from '$lib/stores/toast'
  import { loadSettings, getThemeMode, type ThemeMode } from '$lib/stores/settings.svelte'
  import { loadUIState, saveUIState, paneConstraints } from '$lib/stores/uiState.svelte'
  import { 
    type FocusablePane,
    getFocusedPane, 
    setFocusedPane, 
    focusPreviousPane, 
    focusNextPane,
    isPaneFlashing,
    isInputElement 
  } from '$lib/stores/keyboard.svelte'
  // @ts-ignore - wailsjs path
  import { PrepareReply, GetPendingMailto, GetDraft, Trash, DeletePermanently, MarkAsRead, MarkAsUnread, Star, Unstar, Archive, MarkAsSpam, MarkAsNotSpam, Undo, GetTermsAccepted, SetTermsAccepted } from '../wailsjs/go/app/App.js'
  // @ts-ignore - wailsjs path
  import { smtp, folder } from '../wailsjs/go/models'
  // @ts-ignore - wailsjs runtime
  import { WindowShow, EventsOn } from '../wailsjs/runtime/runtime'
  // @ts-ignore - wailsjs path
  import { InitiateShutdown } from '../wailsjs/go/app/App.js'
  
  // Component refs for keyboard navigation
  let sidebarRef: Sidebar | null = null
  let messageListRef: MessageList | null = null
  let viewerRef: ConversationViewer | null = null
  let messageListContainerRef: HTMLElement | null = null

  // Theme state - follows stored preference or system
  let theme = $state<'light' | 'dark'>('light')

  // React to theme mode changes from settings store
  $effect(() => {
    const mode = getThemeMode()
    applyThemeFromMode(mode)
  })

  // Selected folder state
  let selectedAccountId = $state<string | null>(null)
  let selectedFolderId = $state<string | null>(null)
  let selectedFolderName = $state('Inbox')
  let selectedFolderType = $state<string | null>(null)
  // Track where the selection came from: 'unified' for unified section, 'account' for account tree
  let selectionSource = $state<'unified' | 'account' | null>(null)
  
  // Selected conversation state
  let selectedThreadId = $state<string | null>(null)
  let selectedConversationFolderId = $state<string | null>(null)
  let selectedConversationAccountId = $state<string | null>(null)
  
  // Composer state
  let showComposer = $state(false)
  let composerAccountId = $state<string | null>(null)
  let composerInitialMessage = $state<smtp.ComposeMessage | null>(null)
  let composerDraftId = $state<string | null>(null)

  // Shutdown state
  let isShuttingDown = $state(false)

  // Terms acceptance state
  let showTermsDialog = $state(false)

  // Handle graceful shutdown with overlay
  function handleShutdown() {
    isShuttingDown = true
    setTimeout(() => InitiateShutdown(), 100)
  }

  // Handle terms acceptance
  async function handleTermsAccepted() {
    try {
      await SetTermsAccepted(true)
      showTermsDialog = false
    } catch (err) {
      console.error('Failed to save terms acceptance:', err)
    }
  }

  // Helper to find folder info by ID from account store
  function findFolderById(accountId: string, folderId: string): { name: string; type: string; path: string } | null {
    const acc = accountStore.accounts.find(a => a.account.id === accountId)
    if (!acc) return null

    function searchTree(trees: folder.FolderTree[]): { name: string; type: string; path: string } | null {
      for (const tree of trees) {
        if (tree.folder?.id === folderId) {
          return { name: tree.folder.name, type: tree.folder.type, path: tree.folder.path }
        }
        if (tree.children) {
          const found = searchTree(tree.children)
          if (found) return found
        }
      }
      return null
    }

    // Check if folders are loaded before searching
    if (!acc.folders || acc.folders.length === 0) return null
    return searchTree(acc.folders)
  }

  onMount(async () => {
    // Listen for notification click events from backend
    EventsOn('notification:clicked', (data: { accountId: string; folderId: string; threadId: string }) => {
      console.log('[App] Notification clicked:', data)

      // Find folder info for display
      const folderInfo = findFolderById(data.accountId, data.folderId)

      // Navigate to the folder (use 'unified' source to highlight under Unified Inbox)
      selectedAccountId = data.accountId
      selectedFolderId = data.folderId
      selectedFolderName = folderInfo?.name || 'Inbox'
      selectedFolderType = folderInfo?.type || 'inbox'
      selectionSource = 'unified'

      // Select the conversation
      selectedThreadId = data.threadId
      selectedConversationAccountId = data.accountId
      selectedConversationFolderId = data.folderId

      // Highlight the thread in the message list (with small delay to ensure list has loaded)
      setTimeout(() => {
        messageListRef?.selectThread(data.threadId)
      }, 100)

      // Persist state
      saveUIState({
        selectedAccountId: data.accountId,
        selectedFolderId: data.folderId,
        selectedFolderName: folderInfo?.name || 'Inbox',
        selectedFolderType: folderInfo?.type || 'inbox',
        selectedThreadId: data.threadId,
        selectedConversationAccountId: data.accountId,
        selectedConversationFolderId: data.folderId,
      })
    })

    // Listen for shutdown event from backend (triggered by OS close signal)
    EventsOn('app:shutting-down', () => {
      isShuttingDown = true
    })

    // Listen for escape-iframe-focus event (from EmailBody when navigating away from iframe)
    const handleEscapeIframeFocus = () => {
      // Focus the message list container to take keyboard focus away from iframe
      messageListContainerRef?.focus()
    }
    window.addEventListener('escape-iframe-focus', handleEscapeIframeFocus)

    // Load application settings (including theme mode) and apply theme
    const storedThemeMode = await loadSettings()
    applyThemeFromMode(storedThemeMode)

    // Check if terms have been accepted
    try {
      const termsAccepted = await GetTermsAccepted()
      if (!termsAccepted) {
        showTermsDialog = true
      }
    } catch (err) {
      console.error('Failed to check terms acceptance:', err)
      // Show dialog on error to be safe
      showTermsDialog = true
    }

    // Load persisted UI state
    const uiState = await loadUIState()
    
    // Restore pane widths (already validated/clamped by loadUIState)
    sidebarWidth = uiState.sidebarWidth
    listWidth = uiState.listWidth
    
    // Restore folder selection if valid
    if (uiState.selectedAccountId && uiState.selectedFolderId) {
      // Validate account still exists (unless unified inbox)
      const isUnified = uiState.selectedAccountId === 'unified'
      const accountExists = isUnified || accountStore.accounts.some(
        a => a.account.id === uiState.selectedAccountId
      )
      
      if (accountExists) {
        selectedAccountId = uiState.selectedAccountId
        selectedFolderId = uiState.selectedFolderId
        selectedFolderName = uiState.selectedFolderName || 'Inbox'
        selectedFolderType = uiState.selectedFolderType
        
        // Restore conversation selection
        if (uiState.selectedThreadId) {
          selectedThreadId = uiState.selectedThreadId
          selectedConversationAccountId = uiState.selectedConversationAccountId
          selectedConversationFolderId = uiState.selectedConversationFolderId
        }
      }
    }

    // Listen for system theme changes (only applies when mode is 'system')
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    mediaQuery.addEventListener('change', (e) => {
      if (getThemeMode() === 'system') {
        theme = e.matches ? 'dark' : 'light'
        applyTheme(theme)
      }
    })

    // Show window after UI is ready (prevents white flash on startup)
    WindowShow()

    // Check for pending mailto: URL from command line
    try {
      const mailtoData = await GetPendingMailto()
      if (mailtoData && (mailtoData.to?.length > 0 || mailtoData.subject || mailtoData.body)) {
        // Wait a moment for accounts to load
        await new Promise(resolve => setTimeout(resolve, 100))
        handleMailtoData(mailtoData)
      }
    } catch (err) {
      console.error('Failed to check pending mailto:', err)
    }
  })

  function applyTheme(t: 'light' | 'dark') {
    if (t === 'dark') {
      document.documentElement.classList.add('dark')
    } else {
      document.documentElement.classList.remove('dark')
    }
  }

  // Apply theme based on mode setting (system/light/dark)
  function applyThemeFromMode(mode: ThemeMode) {
    if (mode === 'system') {
      const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
      theme = mediaQuery.matches ? 'dark' : 'light'
    } else {
      theme = mode as 'light' | 'dark'
    }
    applyTheme(theme)
  }

  // Handle folder selection from sidebar (account tree)
  function handleFolderSelect(
    accountId: string,
    folderId: string,
    folderPath: string,
    folderName: string,
    folderType: string
  ) {
    selectedAccountId = accountId
    selectedFolderId = folderId
    selectedFolderName = folderName
    selectedFolderType = folderType
    selectionSource = 'account'
    selectedThreadId = null // Clear conversation selection when changing folders
    selectedConversationFolderId = null
    selectedConversationAccountId = null
    
    // Persist state
    saveUIState({
      selectedAccountId: accountId,
      selectedFolderId: folderId,
      selectedFolderName: folderName,
      selectedFolderType: folderType,
      selectedThreadId: null,
      selectedConversationAccountId: null,
      selectedConversationFolderId: null,
    })
  }

  // Handle folder selection from unified inbox section
  function handleUnifiedFolderSelect(
    accountId: string,
    folderId: string,
    folderPath: string,
    folderName: string,
    folderType: string
  ) {
    selectedAccountId = accountId
    selectedFolderId = folderId
    selectedFolderName = folderName
    selectedFolderType = folderType
    selectionSource = 'unified'
    selectedThreadId = null
    selectedConversationFolderId = null
    selectedConversationAccountId = null
    
    // Persist state
    saveUIState({
      selectedAccountId: accountId,
      selectedFolderId: folderId,
      selectedFolderName: folderName,
      selectedFolderType: folderType,
      selectedThreadId: null,
      selectedConversationAccountId: null,
      selectedConversationFolderId: null,
    })
  }

  // Handle unified inbox selection from sidebar (All Inboxes)
  function handleUnifiedInboxSelect() {
    selectedAccountId = 'unified'
    selectedFolderId = 'inbox'
    selectedFolderName = 'All Inboxes'
    selectedFolderType = 'inbox'
    selectionSource = 'unified'
    selectedThreadId = null
    selectedConversationFolderId = null
    selectedConversationAccountId = null
    
    // Persist state
    saveUIState({
      selectedAccountId: 'unified',
      selectedFolderId: 'inbox',
      selectedFolderName: 'All Inboxes',
      selectedFolderType: 'inbox',
      selectedThreadId: null,
      selectedConversationAccountId: null,
      selectedConversationFolderId: null,
    })
  }

  // Handle conversation selection from list
  function handleConversationSelect(threadId: string, folderId: string, accountId: string) {
    selectedThreadId = threadId
    selectedConversationFolderId = folderId
    selectedConversationAccountId = accountId
    
    // Persist state
    saveUIState({
      selectedThreadId: threadId,
      selectedConversationAccountId: accountId,
      selectedConversationFolderId: folderId,
    })
  }
  
  // Handle compose button click (new message)
  function handleCompose() {
    // Use the selected account, or the first account if none selected
    const accountId = selectedAccountId || accountStore.accounts[0]?.account.id
    if (accountId) {
      composerAccountId = accountId
      composerInitialMessage = null
      composerDraftId = null
      showComposer = true
    }
  }

  // Handle edit draft (opens composer with existing draft)
  async function handleEditDraft(draftId: string) {
    // Use conversation's account ID, fall back to selected account or first account
    const accountId = selectedConversationAccountId || selectedAccountId || accountStore.accounts[0]?.account.id
    if (!accountId || accountId === 'unified') return

    try {
      // Load the draft content from backend
      const draftMessage = await GetDraft(draftId)

      composerAccountId = accountId
      composerInitialMessage = draftMessage || null
      composerDraftId = draftId
      showComposer = true
    } catch (err) {
      console.error('Failed to load draft:', err)
      addToast({
        type: 'error',
        message: 'Failed to load draft',
      })
    }
  }

  // Handle compose to a specific email address (from mailto: links in emails)
  function handleComposeToAddress(toAddress: string) {
    // Use conversation's account ID, or selected account, or first account
    const accountId = selectedConversationAccountId || selectedAccountId || accountStore.accounts[0]?.account.id
    if (accountId && accountId !== 'unified') {
      composerAccountId = accountId
      composerDraftId = null
      // Create a minimal ComposeMessage with just the To address
      composerInitialMessage = new smtp.ComposeMessage({
        from: new smtp.Address({ name: '', address: '' }),
        to: [new smtp.Address({ name: '', address: toAddress })],
        cc: [],
        bcc: [],
        subject: '',
        text_body: '',
        html_body: '',
        attachments: [],
        request_read_receipt: false,
      })
      showComposer = true
    }
  }

  // Handle mailto: URL data (from command line launch)
  interface MailtoData {
    to?: string[]
    cc?: string[]
    bcc?: string[]
    subject?: string
    body?: string
  }

  function handleMailtoData(data: MailtoData) {
    // Use selected account or first account
    const accountId = selectedAccountId || accountStore.accounts[0]?.account.id
    if (!accountId || accountId === 'unified') {
      // No accounts available, can't compose
      addToast({
        type: 'error',
        message: 'No email account configured. Please add an account first.',
      })
      return
    }

    composerAccountId = accountId
    composerDraftId = null
    composerInitialMessage = new smtp.ComposeMessage({
      from: new smtp.Address({ name: '', address: '' }),
      to: (data.to || []).map(addr => new smtp.Address({ name: '', address: addr })),
      cc: (data.cc || []).map(addr => new smtp.Address({ name: '', address: addr })),
      bcc: (data.bcc || []).map(addr => new smtp.Address({ name: '', address: addr })),
      subject: data.subject || '',
      text_body: data.body || '',
      html_body: '',
      attachments: [],
      request_read_receipt: false,
    })
    showComposer = true
  }
  
  // Handle reply/reply-all/forward - calls backend API
  async function handleReply(mode: 'reply' | 'reply-all' | 'forward', messageId: string) {
    // Use conversation's account ID (important for unified inbox), fall back to selected account or first account
    const accountId = selectedConversationAccountId || selectedAccountId || accountStore.accounts[0]?.account.id
    if (!accountId || accountId === 'unified') return

    try {
      // Call backend to prepare the reply message (backend gets account from message)
      const composeMessage = await PrepareReply(messageId, mode)
      composerAccountId = accountId
      composerDraftId = null
      composerInitialMessage = composeMessage
      showComposer = true
    } catch (err) {
      console.error(`Failed to prepare ${mode}:`, err)
      addToast({
        type: 'error',
        message: `Failed to prepare ${mode}: ${err}. Opening blank composer.`,
      })
      // Fallback: open blank composer
      composerAccountId = accountId
      composerDraftId = null
      composerInitialMessage = null
      showComposer = true
    }
  }
  
  // Close composer
  function closeComposer() {
    showComposer = false
    composerAccountId = null
    composerInitialMessage = null
  }

  // Pane sizing state
  let sidebarWidth = $state(240)
  let listWidth = $state(420)

  // Resizing state
  let isResizingSidebar = $state(false)
  let isResizingList = $state(false)

  function startResizeSidebar(e: MouseEvent) {
    isResizingSidebar = true
    e.preventDefault()
  }

  function startResizeList(e: MouseEvent) {
    isResizingList = true
    e.preventDefault()
  }

  function handleMouseMove(e: MouseEvent) {
    if (isResizingSidebar) {
      sidebarWidth = Math.max(paneConstraints.sidebar.min, Math.min(paneConstraints.sidebar.max, e.clientX))
    } else if (isResizingList) {
      listWidth = Math.max(paneConstraints.list.min, Math.min(paneConstraints.list.max, e.clientX - sidebarWidth))
    }
  }

  function handleMouseUp() {
    // Save pane widths if we were resizing
    if (isResizingSidebar || isResizingList) {
      saveUIState({ sidebarWidth, listWidth })
    }
    isResizingSidebar = false
    isResizingList = false
  }

  // Global keyboard shortcut handler
  function handleGlobalKeyDown(e: KeyboardEvent) {
    const inInput = isInputElement(e.target)
    const focusedPane = getFocusedPane()
    const hasConversation = selectedThreadId !== null
    
    // When composer is open, only handle Escape (composer handles its own shortcuts)
    if (showComposer) {
      // Block compose/reply shortcuts that might conflict
      if ((e.ctrlKey || e.metaKey) && ['r', 'f'].includes(e.key.toLowerCase())) {
        e.preventDefault()
        return
      }
      return
    }

    // Handle Ctrl/Cmd shortcuts (global, always work)
    if (e.ctrlKey || e.metaKey) {
      switch (e.key.toLowerCase()) {
        case 'q':
          e.preventDefault()
          handleShutdown()
          return
        case 'n':
          e.preventDefault()
          handleCompose()
          return
        case 'r':
          if (!hasConversation) return
          e.preventDefault()
          if (e.shiftKey) {
            // Reply All - need last message ID
            const lastMsgId = getLastMessageId()
            if (lastMsgId) handleReply('reply-all', lastMsgId)
          } else {
            // Reply
            const lastMsgId = getLastMessageId()
            if (lastMsgId) handleReply('reply', lastMsgId)
          }
          return
        case 'f':
          if (!hasConversation) return
          e.preventDefault()
          const lastMsgId = getLastMessageId()
          if (lastMsgId) handleReply('forward', lastMsgId)
          return
        case 's':
          e.preventDefault()
          if (e.shiftKey) {
            // Ctrl-Shift-S: Toggle sync current folder (start sync or cancel if already running)
            messageListRef?.toggleFolderSync()
          } else {
            // Ctrl-S: Focus search
            messageListRef?.focusSearch()
            setFocusedPane('messageList')
          }
          return
        case 'a':
          if (e.shiftKey) {
            // Ctrl-Shift-A: Toggle sync all accounts (start sync or cancel if already running)
            e.preventDefault()
            sidebarRef?.toggleSync()
            return
          }
          break
        case 'l':
          e.preventDefault()
          if (e.shiftKey) {
            // Ctrl-Shift-L: Open "Always Load" dropdown
            viewerRef?.openAlwaysLoadDropdown()
          } else {
            // Ctrl-L: Load images for this message
            viewerRef?.loadImages()
          }
          return
        case 'u':
          e.preventDefault()
          if (messageListRef?.hasCheckedMessages()) {
            const messageIds = messageListRef.getCheckedMessageIds()
            if (e.shiftKey) {
              handleBulkMarkUnread(messageIds)
            } else {
              handleBulkMarkRead(messageIds)
            }
          } else {
            // Mark the keyboard-focused message as read/unread
            const focusedIds = messageListRef?.getSelectedMessageIds() ?? []
            if (focusedIds.length > 0) {
              if (e.shiftKey) {
                handleBulkMarkUnread(focusedIds)
              } else {
                handleBulkMarkRead(focusedIds)
              }
            }
          }
          return
        case 'k':
          e.preventDefault()
          if (messageListRef?.hasCheckedMessages()) {
            handleBulkArchive(messageListRef.getCheckedMessageIds())
          } else {
            // Archive the keyboard-focused message
            const focusedIds = messageListRef?.getSelectedMessageIds() ?? []
            if (focusedIds.length > 0) {
              handleBulkArchive(focusedIds)
            }
          }
          return
        case 'j':
          e.preventDefault()
          if (messageListRef?.hasCheckedMessages()) {
            handleBulkSpam(messageListRef.getCheckedMessageIds())
          } else {
            // Spam the keyboard-focused message
            const focusedIds = messageListRef?.getSelectedMessageIds() ?? []
            if (focusedIds.length > 0) {
              handleBulkSpam(focusedIds)
            }
          }
          return
      }
      return
    }

    // Handle Alt shortcuts (pane/folder navigation, always work)
    if (e.altKey) {
      switch (e.key) {
        case 'ArrowLeft':
        case 'h':
          e.preventDefault()
          focusPreviousPane()
          return
        case 'ArrowRight':
        case 'l':
          e.preventDefault()
          focusNextPane()
          return
        case 'ArrowUp':
        case 'k':
          e.preventDefault()
          sidebarRef?.selectPreviousFolder()
          return
        case 'ArrowDown':
        case 'j':
          e.preventDefault()
          sidebarRef?.selectNextFolder()
          return
        case 'Enter':
          // Toggle expand/collapse for focused account header
          if (sidebarRef?.hasFocusedAccount()) {
            e.preventDefault()
            sidebarRef.toggleFocusedAccount()
          }
          return
      }
      return
    }

    // Skip single-key shortcuts if in input field
    if (inInput) return

    // Handle Escape (context-dependent, progressive)
    // First Esc: clear checkboxes, Second Esc: close conversation
    if (e.key === 'Escape') {
      if (messageListRef?.hasCheckedMessages()) {
        // First: clear checkboxes
        messageListRef.clearChecked()
      } else if (selectedThreadId) {
        // Second: close conversation viewer
        selectedThreadId = null
        selectedConversationFolderId = null
        selectedConversationAccountId = null
      }
      return
    }

    // Handle pane-focused navigation shortcuts
    switch (e.key) {
      case 'ArrowUp':
      case 'k':
        e.preventDefault()
        if (focusedPane === 'sidebar') {
          sidebarRef?.selectPreviousFolder()
        } else if (focusedPane === 'messageList') {
          if (e.shiftKey) {
            messageListRef?.selectPreviousWithCheck()
          } else {
            messageListRef?.selectPrevious()
          }
        } else if (focusedPane === 'viewer') {
          viewerRef?.scrollUp()
        }
        return
      case 'ArrowDown':
      case 'j':
        e.preventDefault()
        if (focusedPane === 'sidebar') {
          sidebarRef?.selectNextFolder()
        } else if (focusedPane === 'messageList') {
          if (e.shiftKey) {
            messageListRef?.selectNextWithCheck()
          } else {
            messageListRef?.selectNext()
          }
        } else if (focusedPane === 'viewer') {
          viewerRef?.scrollDown()
        }
        return
      case 'Enter':
        // Only let buttons handle Enter if they're in the focused pane
        // This prevents sidebar buttons from intercepting Enter when messageList is focused
        if (document.activeElement?.tagName === 'BUTTON') {
          const btn = document.activeElement as HTMLElement
          const inMessageList = btn.closest('[data-pane="messageList"]')
          const inViewer = btn.closest('[data-pane="viewer"]')
          // Only let button handle Enter if it's in the currently focused pane
          if ((focusedPane === 'messageList' && inMessageList) ||
              (focusedPane === 'viewer' && inViewer)) {
            return
          }
          // Otherwise, prevent button click and handle with our logic
          e.preventDefault()
        }
        if (focusedPane === 'sidebar' && sidebarRef?.hasFocusedAccount()) {
          e.preventDefault()
          sidebarRef.toggleFocusedAccount()
        } else if (focusedPane === 'messageList') {
          e.preventDefault()
          messageListRef?.openSelected()
        }
        return
      case ' ':  // Space - toggle checkbox on focused message, or expand/collapse account
        // Only let buttons handle Space if they're in the focused pane
        if (document.activeElement?.tagName === 'BUTTON') {
          const btn = document.activeElement as HTMLElement
          const inMessageList = btn.closest('[data-pane="messageList"]')
          const inViewer = btn.closest('[data-pane="viewer"]')
          if ((focusedPane === 'messageList' && inMessageList) ||
              (focusedPane === 'viewer' && inViewer)) {
            return
          }
          e.preventDefault()
        }
        e.preventDefault()
        if (focusedPane === 'sidebar' && sidebarRef?.hasFocusedAccount()) {
          sidebarRef.toggleFocusedAccount()
        } else if (focusedPane === 'messageList') {
          messageListRef?.toggleCheck()
        }
        return
    }

    // Single-key shortcuts
    switch (e.key) {
      case 's':
        if (messageListRef?.hasCheckedMessages()) {
          handleBulkToggleStar(messageListRef.getCheckedMessageIds(), messageListRef.getCheckedHasUnstarred())
        } else {
          // Toggle star on the keyboard-focused message
          const focusedIds = messageListRef?.getSelectedMessageIds() ?? []
          if (focusedIds.length > 0) {
            const isStarred = messageListRef?.isSelectedStarred() ?? false
            handleBulkToggleStar(focusedIds, !isStarred)
          }
        }
        return
      case 'Backspace':
      case 'Delete':
        if (messageListRef?.hasCheckedMessages()) {
          // Delete checked messages
          const messageIds = messageListRef.getCheckedMessageIds()
          if (e.shiftKey) {
            handleBulkDeletePermanently(messageIds)
          } else {
            handleBulkTrash(messageIds)
          }
        } else {
          // Delete the keyboard-focused message (not the viewed one)
          const focusedMessageIds = messageListRef?.getSelectedMessageIds() ?? []
          if (focusedMessageIds.length > 0) {
            if (e.shiftKey) {
              handleBulkDeletePermanently(focusedMessageIds)
            } else {
              handleBulkTrash(focusedMessageIds)
            }
          }
        }
        return
    }
  }

  // Get the last message ID from the current conversation (for reply/forward)
  function getLastMessageId(): string | null {
    return viewerRef?.getLastMessageId() ?? null
  }

  // Handle click on pane to set focus
  function handlePaneClick(pane: FocusablePane) {
    setFocusedPane(pane)
  }

  // Bulk action handlers
  async function handleBulkTrash(messageIds: string[]) {
    try {
      await Trash(messageIds)
      addToast({ type: 'success', message: 'Moved to trash', actions: [{ label: 'Undo', onClick: handleUndo }] })
      messageListRef?.clearChecked()
      messageListRef?.handleActionComplete(true)
    } catch (err) {
      addToast({ type: 'error', message: `Failed to delete: ${err}` })
    }
  }

  async function handleBulkDeletePermanently(messageIds: string[]) {
    try {
      await DeletePermanently(messageIds)
      addToast({ type: 'success', message: 'Permanently deleted' })
      messageListRef?.clearChecked()
      messageListRef?.handleActionComplete(true)
    } catch (err) {
      addToast({ type: 'error', message: `Failed to delete: ${err}` })
    }
  }

  async function handleBulkArchive(messageIds: string[]) {
    try {
      await Archive(messageIds)
      addToast({ type: 'success', message: 'Archived', actions: [{ label: 'Undo', onClick: handleUndo }] })
      messageListRef?.clearChecked()
      messageListRef?.handleActionComplete(true)
    } catch (err) {
      addToast({ type: 'error', message: `Failed to archive: ${err}` })
    }
  }

  async function handleBulkSpam(messageIds: string[]) {
    try {
      const isSpamFolder = selectedFolderType === 'spam'

      if (isSpamFolder) {
        // If we're in spam folder, mark as NOT spam
        await MarkAsNotSpam(messageIds)
        addToast({ type: 'success', message: 'Marked as not spam', actions: [{ label: 'Undo', onClick: handleUndo }] })
      } else {
        // Otherwise, mark as spam
        await MarkAsSpam(messageIds)
        addToast({ type: 'success', message: 'Marked as spam', actions: [{ label: 'Undo', onClick: handleUndo }] })
      }

      messageListRef?.clearChecked()
      messageListRef?.handleActionComplete(true)
    } catch (err) {
      const isSpamFolder = selectedFolderType === 'spam'
      addToast({ type: 'error', message: `Failed to ${isSpamFolder ? 'mark as not spam' : 'mark as spam'}: ${err}` })
    }
  }

  async function handleBulkMarkRead(messageIds: string[]) {
    try {
      await MarkAsRead(messageIds)
      addToast({ type: 'success', message: 'Marked as read' })
      messageListRef?.clearChecked()
      messageListRef?.handleActionComplete()
    } catch (err) {
      addToast({ type: 'error', message: `Failed to mark as read: ${err}` })
    }
  }

  async function handleBulkMarkUnread(messageIds: string[]) {
    try {
      await MarkAsUnread(messageIds)
      addToast({ type: 'success', message: 'Marked as unread' })
      messageListRef?.clearChecked()
      messageListRef?.handleActionComplete()
    } catch (err) {
      addToast({ type: 'error', message: `Failed to mark as unread: ${err}` })
    }
  }

  async function handleBulkToggleStar(messageIds: string[], shouldStar: boolean) {
    try {
      if (shouldStar) {
        await Star(messageIds)
        addToast({ type: 'success', message: 'Starred' })
      } else {
        await Unstar(messageIds)
        addToast({ type: 'success', message: 'Star removed' })
      }
      messageListRef?.clearChecked()
      messageListRef?.handleActionComplete()
    } catch (err) {
      addToast({ type: 'error', message: `Failed to update star: ${err}` })
    }
  }

  async function handleUndo() {
    try {
      const description = await Undo()
      addToast({ type: 'success', message: `Undone: ${description}` })
      messageListRef?.handleActionComplete()
    } catch (err) {
      addToast({ type: 'error', message: `Undo failed: ${err}` })
    }
  }
</script>

<svelte:window onmousemove={handleMouseMove} onmouseup={handleMouseUp} onkeydown={handleGlobalKeyDown} />

<div class="flex flex-col h-full w-full overflow-hidden bg-background">
  <!-- Custom Title Bar -->
  <TitleBar onClose={handleShutdown} />

  <!-- Main Content -->
  <div class="flex flex-1 min-h-0 overflow-hidden">
    <!-- Sidebar (Folder List) -->
    <aside
      class="flex-shrink-0 border-r border-border bg-muted/30"
      style="width: {sidebarWidth}px"
      role="presentation"
      onclick={() => handlePaneClick('sidebar')}
    >
      <Sidebar 
        bind:this={sidebarRef}
        onFolderSelect={handleFolderSelect} 
        onUnifiedFolderSelect={handleUnifiedFolderSelect}
        onCompose={handleCompose}
        onUnifiedInboxSelect={handleUnifiedInboxSelect}
        selectedAccountId={selectedAccountId}
        selectedFolderId={selectedFolderId}
        selectionSource={selectionSource}
        isFocused={getFocusedPane() === 'sidebar'}
        isFlashing={isPaneFlashing('sidebar')}
      />
    </aside>

    <!-- Sidebar Resize Handle -->
    <button
      type="button"
      class="w-1 cursor-col-resize hover:bg-primary/20 active:bg-primary/40 transition-colors border-0 p-0 {isResizingSidebar
        ? 'bg-primary/40'
        : ''}"
      onmousedown={startResizeSidebar}
      aria-label="Resize sidebar"
    ></button>

    <!-- Message List -->
    <section
      bind:this={messageListContainerRef}
      class="flex-shrink-0 border-r border-border bg-background"
      style="width: {listWidth}px"
      role="presentation"
      data-pane="messageList"
      tabindex="-1"
      onclick={() => handlePaneClick('messageList')}
    >
      <MessageList
        bind:this={messageListRef}
        accountId={selectedAccountId}
        folderId={selectedFolderId}
        folderName={selectedFolderName}
        folderType={selectedFolderType || 'inbox'}
        onConversationSelect={handleConversationSelect}
        onReply={handleReply}
        isFocused={getFocusedPane() === 'messageList'}
        isFlashing={isPaneFlashing('messageList')}
      />
    </section>

    <!-- List Resize Handle -->
    <button
      type="button"
      class="w-1 cursor-col-resize hover:bg-primary/20 active:bg-primary/40 transition-colors border-0 p-0 {isResizingList
        ? 'bg-primary/40'
        : ''}"
      onmousedown={startResizeList}
      aria-label="Resize message list"
    ></button>

    <!-- Conversation Viewer -->
    <main
      class="flex-1 min-w-0 bg-background"
      role="presentation"
      data-pane="viewer"
      onclick={() => handlePaneClick('viewer')}
    >
      <ConversationViewer
        bind:this={viewerRef}
        threadId={selectedThreadId}
        folderId={selectedConversationFolderId}
        folderType={selectedFolderType}
        accountId={selectedConversationAccountId}
        onReply={handleReply}
        onComposeToAddress={handleComposeToAddress}
        onEditDraft={handleEditDraft}
        onActionComplete={(autoSelectNext) => messageListRef?.handleActionComplete(autoSelectNext)}
        isFocused={getFocusedPane() === 'viewer'}
        isFlashing={isPaneFlashing('viewer')}
      />
    </main>
  </div>
</div>

<!-- Resize cursor overlay when dragging -->
{#if isResizingSidebar || isResizingList}
  <div class="fixed inset-0 cursor-col-resize z-50"></div>
{/if}

<!-- Toast notifications -->
<ToastContainer />

<!-- Composer Modal -->
{#if showComposer && composerAccountId}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
    <div class="w-full max-w-3xl h-[80vh] bg-background rounded-lg shadow-xl overflow-hidden">
      <Composer
        accountId={composerAccountId}
        initialMessage={composerInitialMessage}
        draftId={composerDraftId}
        onClose={closeComposer}
        onSent={closeComposer}
      />
    </div>
  </div>
{/if}

<!-- Shutdown Overlay -->
{#if isShuttingDown}
  <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/80">
    <p class="text-white/90 text-sm font-medium">Shutting down...</p>
  </div>
{/if}

<!-- Terms Acceptance Dialog -->
<TermsDialog bind:open={showTermsDialog} onAccept={handleTermsAccepted} />
