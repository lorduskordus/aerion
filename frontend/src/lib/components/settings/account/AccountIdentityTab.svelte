<script lang="ts">
  import { onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import { Button } from '$lib/components/ui/button'
  import IdentityEditor from './IdentityEditor.svelte'
  import { addToast } from '$lib/stores/toast'
  import { _ } from '$lib/i18n'
  // @ts-ignore - wailsjs path
  import { account } from '../../../../../wailsjs/go/models'
  // @ts-ignore - wailsjs path
  import { GetIdentities, CreateIdentity, UpdateIdentity, DeleteIdentity, SetDefaultIdentity } from '../../../../../wailsjs/go/app/App'

  interface Props {
    /** The account being edited */
    accountId: string
  }

  let { accountId }: Props = $props()

  // State
  let identities = $state<account.Identity[]>([])
  let loading = $state(true)
  let showEditor = $state(false)
  let editingIdentity = $state<account.Identity | null>(null)
  let deletingId = $state<string | null>(null)

  onMount(async () => {
    await loadIdentities()
  })

  async function loadIdentities() {
    loading = true
    try {
      identities = await GetIdentities(accountId)
    } catch (err) {
      console.error('Failed to load identities:', err)
      addToast({
        type: 'error',
        message: $_('identity.failedToLoadAddresses'),
      })
    } finally {
      loading = false
    }
  }

  function handleAddIdentity() {
    editingIdentity = null
    showEditor = true
  }

  function handleEditIdentity(identity: account.Identity) {
    editingIdentity = identity
    showEditor = true
  }

  async function handleSaveIdentity(config: account.IdentityConfig) {
    if (editingIdentity) {
      // Update existing
      await UpdateIdentity(editingIdentity.id, config)
      addToast({
        type: 'success',
        message: $_('identity.emailUpdated'),
      })
    } else {
      // Create new
      await CreateIdentity(accountId, config)
      addToast({
        type: 'success',
        message: $_('identity.emailAdded'),
      })
    }
    await loadIdentities()
  }

  async function handleDeleteIdentity(identity: account.Identity) {
    if (identity.isDefault) {
      addToast({
        type: 'error',
        message: $_('identity.cannotDeleteDefault'),
      })
      return
    }

    deletingId = identity.id
    try {
      await DeleteIdentity(identity.id)
      addToast({
        type: 'success',
        message: $_('identity.emailDeleted'),
      })
      await loadIdentities()
    } catch (err) {
      console.error('Failed to delete identity:', err)
      addToast({
        type: 'error',
        message: err instanceof Error ? err.message : $_('identity.failedToDeleteAddress'),
      })
    } finally {
      deletingId = null
    }
  }

  async function handleSetDefault(identity: account.Identity) {
    if (identity.isDefault) return

    try {
      await SetDefaultIdentity(accountId, identity.id)
      addToast({
        type: 'success',
        message: $_('identity.isNowDefault', { values: { email: identity.email } }),
      })
      await loadIdentities()
    } catch (err) {
      console.error('Failed to set default identity:', err)
      addToast({
        type: 'error',
        message: $_('identity.failedToSetDefault'),
      })
    }
  }

  // Get a preview of the signature (first line, truncated)
  function getSignaturePreview(identity: account.Identity): string {
    if (!identity.signatureEnabled) return $_('identity.noSignature')
    if (!identity.signatureHtml) return $_('identity.noSignature')
    
    // Strip HTML and get first line
    const temp = document.createElement('div')
    temp.innerHTML = identity.signatureHtml
    const text = temp.textContent || ''
    const firstLine = text.split('\n')[0].trim()
    
    if (firstLine.length > 50) {
      return firstLine.substring(0, 50) + '...'
    }
    return firstLine || $_('identity.emptySignature')
  }
</script>

<div class="space-y-4">
  <div class="flex items-center justify-between">
    <div>
      <h3 class="text-sm font-medium flex items-center gap-2">
        <Icon icon="mdi:email-multiple-outline" class="w-4 h-4" />
        {$_('identity.emailAddresses')}
      </h3>
      <p class="text-xs text-muted-foreground mt-1">
        {$_('identity.emailAddressesHelp')}
      </p>
    </div>
    <Button size="sm" onclick={handleAddIdentity}>
      <Icon icon="mdi:plus" class="w-4 h-4 mr-1" />
      {$_('identity.addEmailAddress')}
    </Button>
  </div>

  {#if loading}
    <div class="flex items-center justify-center py-8">
      <Icon icon="mdi:loading" class="w-6 h-6 animate-spin text-muted-foreground" />
    </div>
  {:else if identities.length === 0}
    <div class="text-center py-8 text-muted-foreground">
      <Icon icon="mdi:email-outline" class="w-12 h-12 mx-auto mb-2 opacity-50" />
      <p>{$_('identity.noEmailAddresses')}</p>
    </div>
  {:else}
    <div class="space-y-2">
      {#each identities as identity (identity.id)}
        <div class="flex items-center gap-3 p-3 rounded-lg border border-border bg-card hover:bg-accent/50 transition-colors group">
          <!-- Default radio button -->
          <button
            type="button"
            onclick={() => handleSetDefault(identity)}
            class="flex-shrink-0 w-5 h-5 rounded-full border-2 flex items-center justify-center transition-colors
              {identity.isDefault 
                ? 'border-primary bg-primary' 
                : 'border-muted-foreground hover:border-primary'}"
            title={identity.isDefault ? $_('identity.defaultAddress') : $_('identity.setAsDefaultAddress')}
          >
            {#if identity.isDefault}
              <div class="w-2 h-2 rounded-full bg-white"></div>
            {/if}
          </button>

          <!-- Identity info -->
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2">
              <span class="font-medium text-sm truncate">{identity.email}</span>
              {#if identity.isDefault}
                <span class="text-xs bg-primary/10 text-primary px-1.5 py-0.5 rounded">{$_('identity.default')}</span>
              {/if}
            </div>
            <div class="text-xs text-muted-foreground truncate">
              {identity.name}
            </div>
            <div class="text-xs text-muted-foreground truncate mt-0.5">
              <Icon icon="mdi:signature-text" class="w-3 h-3 inline-block mr-1" />
              {getSignaturePreview(identity)}
            </div>
          </div>

          <!-- Actions -->
          <div class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
            <Button
              variant="ghost"
              size="sm"
              onclick={() => handleEditIdentity(identity)}
              class="h-8 w-8 p-0"
              title={$_('common.edit')}
            >
              <Icon icon="mdi:pencil" class="w-4 h-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onclick={() => handleDeleteIdentity(identity)}
              disabled={identity.isDefault || deletingId === identity.id}
              class="h-8 w-8 p-0 text-destructive hover:text-destructive"
              title={identity.isDefault ? $_('identity.cannotDeleteDefaultTitle') : $_('common.delete')}
            >
              {#if deletingId === identity.id}
                <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
              {:else}
                <Icon icon="mdi:delete" class="w-4 h-4" />
              {/if}
            </Button>
          </div>
        </div>
      {/each}
    </div>
  {/if}

  <p class="text-xs text-muted-foreground">
    {$_('identity.defaultHelp')}
  </p>
</div>

<!-- Identity Editor Dialog -->
<IdentityEditor
  bind:open={showEditor}
  {accountId}
  identity={editingIdentity}
  onSave={handleSaveIdentity}
  onClose={() => { showEditor = false; editingIdentity = null }}
/>
