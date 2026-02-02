<script lang="ts">
  // Load offline icon data before anything else
  import './lib/iconify-offline'

  import { onMount, onDestroy } from 'svelte'
  import Icon from '@iconify/svelte'
  import Composer from './lib/components/composer/Composer.svelte'
  import ToastContainer from './lib/components/ui/toast/ToastContainer.svelte'
  import { addToast } from '$lib/stores/toast'
  import { createComposerWindowApi } from '$lib/composerApi'
  import { getShowTitleBar, type ThemeMode } from '$lib/stores/settings.svelte'
  // @ts-ignore - wailsjs imports
  import { GetComposeMode, PrepareReply, GetDraft, CloseWindow, GetThemeMode } from '../wailsjs/go/app/ComposerApp.js'
  // @ts-ignore - wailsjs imports
  import { smtp, app } from '../wailsjs/go/models'
  // @ts-ignore - wailsjs runtime
  import { WindowMinimise, WindowToggleMaximise, WindowShow, Quit, EventsOn, EventsOff } from '../wailsjs/runtime/runtime'

  // Theme state - follows system preference or main window theme
  let theme = $state<ThemeMode>('light')
  
  // Compose mode info from backend
  let composeMode = $state<app.ComposeMode | null>(null)
  let initialMessage = $state<smtp.ComposeMessage | null>(null)
  let loading = $state(true)
  let error = $state<string | null>(null)
  
  // Window state
  let isMaximized = $state(false)
  let isHovering = $state(false)
  
  // Close request state - triggers Composer's close dialog
  let closeRequested = $state(false)
  
  // Window title based on mode
  let windowTitle = $derived(() => {
    if (!composeMode) return 'Compose'
    switch (composeMode.mode) {
      case 'reply': return 'Reply'
      case 'reply-all': return 'Reply All'
      case 'forward': return 'Forward'
      default: return composeMode.draftId ? 'Edit Draft' : 'New Message'
    }
  })

  onMount(async () => {
    // Load saved theme mode from backend
    try {
      const savedThemeMode = await GetThemeMode() as ThemeMode
      if (savedThemeMode === 'system') {
        // If system mode, use OS preference
        const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
        theme = mediaQuery.matches ? 'dark' : 'light'
      } else {
        // Use saved theme
        theme = savedThemeMode
      }
      applyTheme(theme)
    } catch (err) {
      console.error('Failed to load theme mode:', err)
      // Fallback to system preference
      const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
      theme = mediaQuery.matches ? 'dark' : 'light'
      applyTheme(theme)
    }

    // Show window after theme is applied (prevents white flash on startup)
    WindowShow()

    // Listen for system theme changes (only applies when mode is 'system')
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    mediaQuery.addEventListener('change', (e) => {
      // Only auto-switch if theme mode is 'system'
      if (theme === 'system' || theme === 'light' || theme === 'dark') {
        // This listener is for system mode only, but we keep it simple
        // The main window will broadcast theme changes via IPC
      }
    })
    
    // Listen for theme changes from main window via IPC
    EventsOn('theme:changed', (newTheme: string) => {
      // Accept all valid theme modes
      const validThemes: ThemeMode[] = ['system', 'light', 'light-blue', 'light-orange', 'dark', 'dark-gray']
      if (validThemes.includes(newTheme as ThemeMode)) {
        theme = newTheme as ThemeMode
        applyTheme(theme)
      }
    })
    
    // Listen for shutdown request from main window
    EventsOn('app:shutdown', (reason: string) => {
      addToast({
        type: 'info',
        message: 'Main window is closing. Your draft will be saved.',
      })
      // Give user a moment to see the toast, then close
      setTimeout(() => {
        CloseWindow()
      }, 1000)
    })
    
    // Load compose mode and initial data
    try {
      composeMode = await GetComposeMode()
      
      // If editing a draft, load it
      if (composeMode?.draftId) {
        const draft = await GetDraft()
        if (draft) {
          initialMessage = draft
        }
      } 
      // If replying/forwarding, prepare the message
      else if (composeMode?.mode !== 'new' && composeMode?.messageId) {
        const prepared = await PrepareReply()
        if (prepared) {
          initialMessage = prepared
        }
      }
      // For new message, PrepareReply returns a message with just the From address
      else {
        const prepared = await PrepareReply()
        if (prepared) {
          initialMessage = prepared
        }
      }
    } catch (err) {
      console.error('Failed to initialize composer:', err)
      error = `Failed to initialize: ${err}`
    } finally {
      loading = false
    }
  })
  
  onDestroy(() => {
    EventsOff('theme:changed')
    EventsOff('app:shutdown')
  })

  function applyTheme(themeName: ThemeMode) {
    document.documentElement.setAttribute('data-theme', themeName)

    // Legacy: Also set .dark class for backwards compat
    if (themeName.startsWith('dark')) {
      document.documentElement.classList.add('dark')
    } else {
      document.documentElement.classList.remove('dark')
    }
  }
  
  // Window control functions
  async function minimize() {
    await WindowMinimise()
  }
  
  async function toggleMaximize() {
    await WindowToggleMaximise()
    isMaximized = !isMaximized
  }
  
  // Request close - triggers Composer's close confirmation dialog
  function requestClose() {
    closeRequested = true
  }
  
  // Called when Composer has handled the close request (user made a choice)
  function handleCloseHandled() {
    closeRequested = false
  }
  
  // Handle composer close (after send or discard confirmation)
  function handleComposerClose() {
    CloseWindow()
  }
  
  // Handle message sent
  function handleMessageSent() {
    // The Composer component shows its own toast
    // Close the window after a brief delay
    setTimeout(() => {
      CloseWindow()
    }, 500)
  }
</script>

<div class="h-screen flex flex-col bg-background text-foreground">
  <!-- Custom Title Bar for frameless window -->
  {#if getShowTitleBar()}
    <header class="h-10 flex items-center justify-between bg-muted/50 border-b border-border select-none shrink-0">
      <!-- Drag region - left side with title -->
      <div class="flex-1 flex items-center gap-2 px-3 h-full" style="--wails-draggable: drag">
        <Icon icon="mdi:email-edit-outline" class="w-5 h-5 text-primary" />
        <span class="text-sm font-medium text-foreground">{windowTitle()}</span>
      </div>

      <!-- Mac-style traffic light controls -->
      <div
        class="flex items-center gap-2 px-3 h-full"
        role="group"
        aria-label="Window controls"
        onmouseenter={() => isHovering = true}
        onmouseleave={() => isHovering = false}
      >
        <!-- Minimize (yellow) -->
        <button
          class="w-3 h-3 rounded-full flex items-center justify-center transition-all bg-[#FEBC2E] hover:brightness-90 active:brightness-75"
          onclick={minimize}
          title="Minimize"
          aria-label="Minimize window"
        >
          {#if isHovering}
            <span class="text-[10px] font-bold text-black/60 leading-none">−</span>
          {/if}
        </button>

        <!-- Maximize/Restore (green) -->
        <button
          class="w-3 h-3 rounded-full flex items-center justify-center transition-all bg-[#28C840] hover:brightness-90 active:brightness-75"
          onclick={toggleMaximize}
          title={isMaximized ? "Restore" : "Maximize"}
          aria-label={isMaximized ? "Restore window" : "Maximize window"}
        >
          {#if isHovering}
            <span class="text-[10px] font-bold text-black/60 leading-none">+</span>
          {/if}
        </button>

        <!-- Close (red) -->
        <button
          class="w-3 h-3 rounded-full flex items-center justify-center transition-all bg-[#FF5F57] hover:brightness-90 active:brightness-75"
          onclick={requestClose}
          title="Close"
          aria-label="Close window"
        >
          {#if isHovering}
            <span class="text-[10px] font-bold text-black/60 leading-none">×</span>
          {/if}
        </button>
      </div>
    </header>
  {/if}

  <!-- Main content -->
  <main class="flex-1 min-h-0 overflow-hidden">
    {#if loading}
      <div class="h-full flex items-center justify-center">
        <div class="flex flex-col items-center gap-3">
          <Icon icon="mdi:loading" class="w-8 h-8 animate-spin text-primary" />
          <span class="text-sm text-muted-foreground">Loading...</span>
        </div>
      </div>
    {:else if error}
      <div class="h-full flex items-center justify-center">
        <div class="flex flex-col items-center gap-3 text-center px-4">
          <Icon icon="mdi:alert-circle" class="w-12 h-12 text-destructive" />
          <p class="text-sm text-destructive">{error}</p>
          <button
            onclick={() => CloseWindow()}
            class="px-4 py-2 text-sm bg-muted hover:bg-muted/80 rounded-md transition-colors"
          >
            Close Window
          </button>
        </div>
      </div>
    {:else if composeMode}
      <Composer
        accountId={composeMode.accountId}
        initialMessage={initialMessage}
        draftId={composeMode.draftId || null}
        messageId={composeMode.messageId || null}
        onClose={handleComposerClose}
        onSent={handleMessageSent}
        api={createComposerWindowApi(composeMode.accountId)}
        isDetached={true}
        closeRequested={closeRequested}
        onCloseHandled={handleCloseHandled}
      />
    {/if}
  </main>
</div>

<!-- Toast notifications -->
<ToastContainer />
