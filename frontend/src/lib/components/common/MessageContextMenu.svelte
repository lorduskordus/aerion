<script lang="ts">
  import Icon from '@iconify/svelte'
  import { ContextMenu as ContextMenuPrimitive } from 'bits-ui'
  import {
    ContextMenuContent,
    ContextMenuItem,
    ContextMenuSeparator,
    ContextMenuSub,
    ContextMenuSubTrigger,
    ContextMenuSubContent,
  } from '$lib/components/ui/context-menu'
  import {
    GetFolders,
    MarkAsRead,
    MarkAsUnread,
    Star,
    Unstar,
    Archive,
    Trash,
    MarkAsSpam,
    MarkAsNotSpam,
    DeletePermanently,
    MoveToFolder,
    CopyToFolder,
    Undo,
  } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import { folder } from '../../../../wailsjs/go/models'
  import { toasts } from '$lib/stores/toast'
  import { ConfirmDialog } from '$lib/components/ui/confirm-dialog'
  import type { Snippet } from 'svelte'

  interface Props {
    messageIds: string[]
    accountId: string
    currentFolderId: string
    folderType: string
    isStarred: boolean
    isRead: boolean
    onActionComplete?: (autoSelectNext?: boolean) => void
    onReply?: (mode: 'reply' | 'reply-all' | 'forward', messageId: string) => void
    onOpenChange?: (open: boolean) => void
    children?: Snippet
  }

  let {
    messageIds,
    accountId,
    currentFolderId,
    folderType,
    isStarred,
    isRead,
    onActionComplete,
    onReply,
    onOpenChange,
    children,
  }: Props = $props()

  // Folders state for move/copy submenus
  let folders = $state<folder.Folder[]>([])
  let foldersLoading = $state(false)
  let foldersLoaded = $state(false)

  // Permanent delete confirmation
  let showDeleteConfirm = $state(false)

  // Folder type to icon mapping
  const folderIcons: Record<string, string> = {
    inbox: 'mdi:inbox',
    sent: 'mdi:send',
    drafts: 'mdi:file-document-edit-outline',
    trash: 'mdi:delete-outline',
    archive: 'mdi:archive-outline',
    spam: 'mdi:alert-octagon-outline',
    all: 'mdi:email-multiple-outline',
    folder: 'mdi:folder-outline',
  }

  function getFolderIcon(type: string): string {
    return folderIcons[type] || folderIcons.folder
  }

  // Computed values
  const isTrashFolder = $derived(folderType === 'trash')
  const isSpamFolder = $derived(folderType === 'spam')
  const isSingleMessage = $derived(messageIds.length === 1)

  // Load folders when context menu opens
  async function loadFolders() {
    if (foldersLoaded || foldersLoading) return

    foldersLoading = true
    try {
      const result = await GetFolders(accountId)
      folders = result || []
      foldersLoaded = true
    } catch (err) {
      console.error('Failed to load folders:', err)
    } finally {
      foldersLoading = false
    }
  }

  // Handle menu open
  function handleOpenChange(open: boolean) {
    if (open) {
      loadFolders()
    }
    onOpenChange?.(open)
  }

  // Get folders excluding current folder (for move/copy)
  const availableFolders = $derived(
    folders.filter((f) => f.id !== currentFolderId)
  )

  // Group folders: special folders first, then custom folders
  const specialFolderTypes = ['inbox', 'sent', 'drafts', 'archive', 'trash', 'spam', 'all']
  const specialFolders = $derived(
    availableFolders.filter((f) => specialFolderTypes.includes(f.type))
  )
  const customFolders = $derived(
    availableFolders.filter((f) => !specialFolderTypes.includes(f.type))
  )

  // Undo handler
  async function handleUndo() {
    try {
      const description = await Undo()
      toasts.success(`Undone: ${description}`)
    } catch (err) {
      toasts.error(`Undo failed: ${err}`)
    }
  }

  // Action handlers
  async function handleReply() {
    if (isSingleMessage && onReply) {
      onReply('reply', messageIds[0])
    }
  }

  async function handleReplyAll() {
    if (isSingleMessage && onReply) {
      onReply('reply-all', messageIds[0])
    }
  }

  async function handleForward() {
    if (isSingleMessage && onReply) {
      onReply('forward', messageIds[0])
    }
  }

  async function handleArchive() {
    try {
      await Archive(messageIds)
      toasts.success('Archived', [{ label: 'Undo', onClick: handleUndo }])
      onActionComplete?.(true)
    } catch (err) {
      toasts.error(`Failed to archive: ${err}`)
    }
  }

  async function handleDelete() {
    if (isTrashFolder) {
      showDeleteConfirm = true
    } else {
      try {
        await Trash(messageIds)
        toasts.success('Moved to trash', [{ label: 'Undo', onClick: handleUndo }])
        onActionComplete?.(true)
      } catch (err) {
        toasts.error(`Failed to delete: ${err}`)
      }
    }
  }

  async function handleConfirmPermanentDelete() {
    try {
      await DeletePermanently(messageIds)
      toasts.success('Permanently deleted')
      showDeleteConfirm = false
      onActionComplete?.(true)
    } catch (err) {
      toasts.error(`Failed to delete: ${err}`)
      showDeleteConfirm = false
    }
  }

  async function handleSpam() {
    try {
      if (isSpamFolder) {
        // If we're in spam folder, mark as NOT spam
        await MarkAsNotSpam(messageIds)
        toasts.success('Marked as not spam', [{ label: 'Undo', onClick: handleUndo }])
      } else {
        // Otherwise, mark as spam
        await MarkAsSpam(messageIds)
        toasts.success('Marked as spam', [{ label: 'Undo', onClick: handleUndo }])
      }
      onActionComplete?.(true)
    } catch (err) {
      toasts.error(`Failed to ${isSpamFolder ? 'mark as not spam' : 'mark as spam'}: ${err}`)
    }
  }

  async function handleToggleStar() {
    try {
      if (isStarred) {
        await Unstar(messageIds)
        toasts.success('Star removed')
      } else {
        await Star(messageIds)
        toasts.success('Starred')
      }
      onActionComplete?.()
    } catch (err) {
      toasts.error(`Failed to update star: ${err}`)
    }
  }

  async function handleToggleRead() {
    try {
      if (isRead) {
        await MarkAsUnread(messageIds)
        toasts.success('Marked as unread')
      } else {
        await MarkAsRead(messageIds)
        toasts.success('Marked as read')
      }
      onActionComplete?.()
    } catch (err) {
      toasts.error(`Failed to update read status: ${err}`)
    }
  }

  async function handleMoveTo(destFolderId: string, folderName: string) {
    try {
      await MoveToFolder(messageIds, destFolderId)
      toasts.success(`Moved to ${folderName}`, [{ label: 'Undo', onClick: handleUndo }])
      onActionComplete?.(true)
    } catch (err) {
      toasts.error(`Failed to move: ${err}`)
    }
  }

  async function handleCopyTo(destFolderId: string, folderName: string) {
    try {
      await CopyToFolder(messageIds, destFolderId)
      toasts.success(`Copying to ${folderName}...`)
      // Note: CopyToFolder syncs in background and emits messages:copied event
    } catch (err) {
      toasts.error(`Failed to copy: ${err}`)
    }
  }
</script>

<ContextMenuPrimitive.Root onOpenChange={handleOpenChange}>
  <ContextMenuPrimitive.Trigger>
    {#if children}
      {@render children()}
    {/if}
  </ContextMenuPrimitive.Trigger>

  <ContextMenuContent>
    <!-- Reply actions (single message only) -->
    {#if isSingleMessage}
      <ContextMenuItem onSelect={handleReply}>
        <Icon icon="mdi:reply" class="mr-2 h-4 w-4" />
        Reply
      </ContextMenuItem>
      <ContextMenuItem onSelect={handleReplyAll}>
        <Icon icon="mdi:reply-all" class="mr-2 h-4 w-4" />
        Reply All
      </ContextMenuItem>
      <ContextMenuItem onSelect={handleForward}>
        <Icon icon="mdi:share" class="mr-2 h-4 w-4" />
        Forward
      </ContextMenuItem>
      <ContextMenuSeparator />
    {/if}

    <!-- Move/Delete actions -->
    <ContextMenuItem onSelect={handleArchive}>
      <Icon icon="mdi:archive-outline" class="mr-2 h-4 w-4" />
      Archive
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleDelete}>
      <Icon icon={isTrashFolder ? 'mdi:delete-forever' : 'mdi:delete-outline'} class="mr-2 h-4 w-4" />
      {isTrashFolder ? 'Delete Permanently' : 'Delete'}
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleSpam}>
      <Icon icon={isSpamFolder ? "mdi:email-check-outline" : "mdi:alert-octagon-outline"} class="mr-2 h-4 w-4" />
      {isSpamFolder ? 'Mark as NOT Spam' : 'Mark as Spam'}
    </ContextMenuItem>

    <ContextMenuSeparator />

    <!-- Move to submenu -->
    <ContextMenuSub>
      <ContextMenuSubTrigger>
        <Icon icon="mdi:folder-move-outline" class="mr-2 h-4 w-4" />
        Move to
      </ContextMenuSubTrigger>
      <ContextMenuSubContent>
        {#if foldersLoading}
          <ContextMenuItem disabled>
            <Icon icon="mdi:loading" class="mr-2 h-4 w-4 animate-spin" />
            Loading...
          </ContextMenuItem>
        {:else if availableFolders.length === 0}
          <ContextMenuItem disabled>
            No folders available
          </ContextMenuItem>
        {:else}
          <!-- Special folders -->
          {#each specialFolders as f (f.id)}
            <ContextMenuItem onSelect={() => handleMoveTo(f.id, f.name)}>
              <Icon icon={getFolderIcon(f.type)} class="mr-2 h-4 w-4" />
              {f.name}
            </ContextMenuItem>
          {/each}
          <!-- Separator if we have custom folders -->
          {#if customFolders.length > 0 && specialFolders.length > 0}
            <ContextMenuSeparator />
          {/if}
          <!-- Custom folders -->
          {#each customFolders as f (f.id)}
            <ContextMenuItem onSelect={() => handleMoveTo(f.id, f.name)}>
              <Icon icon={getFolderIcon(f.type)} class="mr-2 h-4 w-4" />
              {f.name}
            </ContextMenuItem>
          {/each}
        {/if}
      </ContextMenuSubContent>
    </ContextMenuSub>

    <!-- Copy to submenu -->
    <ContextMenuSub>
      <ContextMenuSubTrigger>
        <Icon icon="mdi:content-copy" class="mr-2 h-4 w-4" />
        Copy to
      </ContextMenuSubTrigger>
      <ContextMenuSubContent>
        {#if foldersLoading}
          <ContextMenuItem disabled>
            <Icon icon="mdi:loading" class="mr-2 h-4 w-4 animate-spin" />
            Loading...
          </ContextMenuItem>
        {:else if availableFolders.length === 0}
          <ContextMenuItem disabled>
            No folders available
          </ContextMenuItem>
        {:else}
          <!-- Special folders -->
          {#each specialFolders as f (f.id)}
            <ContextMenuItem onSelect={() => handleCopyTo(f.id, f.name)}>
              <Icon icon={getFolderIcon(f.type)} class="mr-2 h-4 w-4" />
              {f.name}
            </ContextMenuItem>
          {/each}
          <!-- Separator if we have custom folders -->
          {#if customFolders.length > 0 && specialFolders.length > 0}
            <ContextMenuSeparator />
          {/if}
          <!-- Custom folders -->
          {#each customFolders as f (f.id)}
            <ContextMenuItem onSelect={() => handleCopyTo(f.id, f.name)}>
              <Icon icon={getFolderIcon(f.type)} class="mr-2 h-4 w-4" />
              {f.name}
            </ContextMenuItem>
          {/each}
        {/if}
      </ContextMenuSubContent>
    </ContextMenuSub>

    <ContextMenuSeparator />

    <!-- Flag actions -->
    <ContextMenuItem onSelect={handleToggleStar}>
      <Icon
        icon={isStarred ? 'mdi:star' : 'mdi:star-outline'}
        class="mr-2 h-4 w-4 {isStarred ? 'text-yellow-500' : ''}"
      />
      {isStarred ? 'Remove Star' : 'Star'}
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleToggleRead}>
      <Icon
        icon={isRead ? 'mdi:email-outline' : 'mdi:email-open-outline'}
        class="mr-2 h-4 w-4"
      />
      {isRead ? 'Mark as Unread' : 'Mark as Read'}
    </ContextMenuItem>
  </ContextMenuContent>
</ContextMenuPrimitive.Root>

<!-- Permanent Delete Confirmation Dialog -->
<ConfirmDialog
  bind:open={showDeleteConfirm}
  title="Delete Permanently?"
  description="This will permanently delete the selected message(s). This action cannot be undone."
  confirmLabel="Delete Permanently"
  variant="destructive"
  onConfirm={handleConfirmPermanentDelete}
  onCancel={() => (showDeleteConfirm = false)}
/>
