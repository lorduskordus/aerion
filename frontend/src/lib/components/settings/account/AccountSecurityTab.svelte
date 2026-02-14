<script lang="ts">
  import { onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import { Button } from '$lib/components/ui/button'
  import { addToast } from '$lib/stores/toast'
  // @ts-ignore - wailsjs path
  import { smime } from '../../../../../wailsjs/go/models'
  // @ts-ignore - wailsjs path
  import {
    ListSMIMECertificates,
    DeleteSMIMECertificate,
    SetDefaultSMIMECertificate,
    GetSMIMESignPolicy,
    SetSMIMESignPolicy,
    GetSMIMEEncryptPolicy,
    SetSMIMEEncryptPolicy,
    ListSenderCerts,
    DeleteSenderCert,
    PickSMIMECertificateFile,
    ImportSMIMECertificateFromPath,
    PickRecipientCertFile,
    ImportRecipientCert,
    ListPGPKeys,
    DeletePGPKey,
    SetDefaultPGPKey,
    GetPGPSignPolicy,
    SetPGPSignPolicy,
    GetPGPEncryptPolicy,
    SetPGPEncryptPolicy,
    ListPGPSenderKeys,
    DeletePGPSenderKey,
    PickPGPKeyFile,
    ImportPGPKeyFromPath,
    PickRecipientPGPKeyFile,
    ImportRecipientPGPKey,
    LookupPGPKey,
    GetPGPKeyServers,
    AddPGPKeyServer,
    RemovePGPKeyServer,
  } from '../../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import { pgp } from '../../../../../wailsjs/go/models'

  interface Props {
    accountId: string
  }

  let { accountId }: Props = $props()

  // State
  let certificates = $state<smime.Certificate[]>([])
  let senderCerts = $state<smime.SenderCert[]>([])
  let signPolicy = $state('never')
  let encryptPolicy = $state('never')
  let loading = $state(true)
  let importing = $state(false)

  // Import dialog state
  let showImportDialog = $state(false)
  let importFilePath = $state('')
  let importPassword = $state('')
  let importError = $state('')

  // Recipient cert import dialog state
  let showRecipientImportDialog = $state(false)
  let recipientImportFilePath = $state('')
  let recipientImportEmail = $state('')
  let recipientImportError = $state('')
  let recipientImporting = $state(false)

  // PGP state
  let pgpKeys = $state<pgp.Key[]>([])
  let pgpSenderKeys = $state<pgp.SenderKey[]>([])
  let pgpSignPolicy = $state('never')
  let pgpEncryptPolicy = $state('never')
  let pgpImporting = $state(false)

  // PGP import dialog state
  let showPGPImportDialog = $state(false)
  let pgpImportFilePath = $state('')
  let pgpImportPassphrase = $state('')
  let pgpImportError = $state('')

  // PGP recipient key import dialog state
  let showPGPRecipientImportDialog = $state(false)
  let pgpRecipientImportFilePath = $state('')
  let pgpRecipientImportEmail = $state('')
  let pgpRecipientImportError = $state('')
  let pgpRecipientImporting = $state(false)

  // Key lookup state (unified WKD + HKP)
  let keyLookupEmail = $state('')
  let keyLookupLoading = $state(false)

  // Key server state
  let keyServers = $state<pgp.KeyServer[]>([])
  let newKeyServerURL = $state('')
  let addingKeyServer = $state(false)
  let keyServersCollapsed = $state(true)

  // Section collapse state (collapsed by default)
  let pgpCollapsed = $state(true)
  let smimeCollapsed = $state(true)

  onMount(async () => {
    await loadData()
  })

  async function loadData() {
    loading = true
    try {
      const [certs, sPolicy, ePolicy, senderCertList, pKeys, pgpSPolicy, pgpEPolicy, pSenderKeys, pKeyServers] = await Promise.all([
        ListSMIMECertificates(accountId),
        GetSMIMESignPolicy(accountId),
        GetSMIMEEncryptPolicy(accountId),
        ListSenderCerts(),
        ListPGPKeys(accountId),
        GetPGPSignPolicy(accountId),
        GetPGPEncryptPolicy(accountId),
        ListPGPSenderKeys(),
        GetPGPKeyServers(),
      ])
      certificates = certs || []
      signPolicy = sPolicy || 'never'
      encryptPolicy = ePolicy || 'never'
      senderCerts = senderCertList || []
      pgpKeys = pKeys || []
      pgpSignPolicy = pgpSPolicy || 'never'
      pgpEncryptPolicy = pgpEPolicy || 'never'
      pgpSenderKeys = pSenderKeys || []
      keyServers = pKeyServers || []
    } catch (err) {
      console.error('Failed to load security data:', err)
    } finally {
      loading = false
    }
  }

  async function handlePickAndImport() {
    importError = ''
    try {
      const path = await PickSMIMECertificateFile()
      if (!path) return

      importFilePath = path
      showImportDialog = true
    } catch (err) {
      console.error('Failed to pick certificate file:', err)
    }
  }

  async function handleImport() {
    if (!importFilePath) return

    importing = true
    importError = ''
    try {
      const result = await ImportSMIMECertificateFromPath(accountId, importFilePath, importPassword)
      addToast({
        type: 'success',
        message: `Certificate imported (${result.chainLength} cert${result.chainLength !== 1 ? 's' : ''} in chain)`,
      })
      showImportDialog = false
      importFilePath = ''
      importPassword = ''
      await loadData()
    } catch (err) {
      importError = err instanceof Error ? err.message : String(err)
    } finally {
      importing = false
    }
  }

  function handleCancelImport() {
    showImportDialog = false
    importFilePath = ''
    importPassword = ''
    importError = ''
  }

  async function handleDeleteCert(certId: string) {
    try {
      await DeleteSMIMECertificate(certId)
      addToast({ type: 'success', message: 'Certificate removed' })
      await loadData()
    } catch (err) {
      addToast({
        type: 'error',
        message: err instanceof Error ? err.message : 'Failed to remove certificate',
      })
    }
  }

  async function handleSetDefault(certId: string) {
    try {
      await SetDefaultSMIMECertificate(accountId, certId)
      addToast({ type: 'success', message: 'Default certificate updated' })
      await loadData()
    } catch (err) {
      addToast({
        type: 'error',
        message: err instanceof Error ? err.message : 'Failed to set default certificate',
      })
    }
  }

  async function handleSignPolicyChange(policy: string) {
    try {
      await SetSMIMESignPolicy(accountId, policy)
      signPolicy = policy
    } catch (err) {
      addToast({
        type: 'error',
        message: err instanceof Error ? err.message : 'Failed to update signing policy',
      })
    }
  }

  async function handleEncryptPolicyChange(policy: string) {
    try {
      await SetSMIMEEncryptPolicy(accountId, policy)
      encryptPolicy = policy
    } catch (err) {
      addToast({
        type: 'error',
        message: err instanceof Error ? err.message : 'Failed to update encryption policy',
      })
    }
  }

  async function handlePickRecipientCert() {
    recipientImportError = ''
    try {
      const path = await PickRecipientCertFile()
      if (!path) return
      recipientImportFilePath = path
      showRecipientImportDialog = true
    } catch (err) {
      console.error('Failed to pick recipient certificate file:', err)
    }
  }

  async function handleImportRecipientCert() {
    if (!recipientImportFilePath || !recipientImportEmail.trim()) {
      recipientImportError = 'Please enter the recipient email address'
      return
    }

    recipientImporting = true
    recipientImportError = ''
    try {
      await ImportRecipientCert(recipientImportEmail.trim(), recipientImportFilePath)
      addToast({ type: 'success', message: `Certificate imported for ${recipientImportEmail.trim()}` })
      showRecipientImportDialog = false
      recipientImportFilePath = ''
      recipientImportEmail = ''
      await loadData()
    } catch (err) {
      recipientImportError = err instanceof Error ? err.message : String(err)
    } finally {
      recipientImporting = false
    }
  }

  function handleCancelRecipientImport() {
    showRecipientImportDialog = false
    recipientImportFilePath = ''
    recipientImportEmail = ''
    recipientImportError = ''
  }

  async function handleDeleteSenderCert(certId: string) {
    try {
      await DeleteSenderCert(certId)
      addToast({ type: 'success', message: 'Sender certificate removed' })
      await loadData()
    } catch (err) {
      addToast({
        type: 'error',
        message: err instanceof Error ? err.message : 'Failed to remove sender certificate',
      })
    }
  }

  // PGP handlers
  async function handlePickAndImportPGP() {
    pgpImportError = ''
    try {
      const path = await PickPGPKeyFile()
      if (!path) return
      pgpImportFilePath = path
      showPGPImportDialog = true
    } catch (err) {
      console.error('Failed to pick PGP key file:', err)
    }
  }

  async function handleImportPGP() {
    if (!pgpImportFilePath) return
    pgpImporting = true
    pgpImportError = ''
    try {
      await ImportPGPKeyFromPath(accountId, pgpImportFilePath, pgpImportPassphrase)
      addToast({ type: 'success', message: 'PGP key imported successfully' })
      showPGPImportDialog = false
      pgpImportFilePath = ''
      pgpImportPassphrase = ''
      await loadData()
    } catch (err) {
      pgpImportError = err instanceof Error ? err.message : String(err)
    } finally {
      pgpImporting = false
    }
  }

  function handleCancelPGPImport() {
    showPGPImportDialog = false
    pgpImportFilePath = ''
    pgpImportPassphrase = ''
    pgpImportError = ''
  }

  async function handleDeletePGPKey(keyId: string) {
    try {
      await DeletePGPKey(keyId)
      addToast({ type: 'success', message: 'PGP key removed' })
      await loadData()
    } catch (err) {
      addToast({ type: 'error', message: err instanceof Error ? err.message : 'Failed to remove PGP key' })
    }
  }

  async function handleSetDefaultPGP(keyId: string) {
    try {
      await SetDefaultPGPKey(accountId, keyId)
      addToast({ type: 'success', message: 'Default PGP key updated' })
      await loadData()
    } catch (err) {
      addToast({ type: 'error', message: err instanceof Error ? err.message : 'Failed to set default PGP key' })
    }
  }

  async function handlePGPSignPolicyChange(policy: string) {
    try {
      await SetPGPSignPolicy(accountId, policy)
      pgpSignPolicy = policy
    } catch (err) {
      addToast({ type: 'error', message: err instanceof Error ? err.message : 'Failed to update PGP signing policy' })
    }
  }

  async function handlePGPEncryptPolicyChange(policy: string) {
    try {
      await SetPGPEncryptPolicy(accountId, policy)
      pgpEncryptPolicy = policy
    } catch (err) {
      addToast({ type: 'error', message: err instanceof Error ? err.message : 'Failed to update PGP encryption policy' })
    }
  }

  async function handlePickPGPRecipientKey() {
    pgpRecipientImportError = ''
    try {
      const path = await PickRecipientPGPKeyFile()
      if (!path) return
      pgpRecipientImportFilePath = path
      showPGPRecipientImportDialog = true
    } catch (err) {
      console.error('Failed to pick recipient PGP key file:', err)
    }
  }

  async function handleImportPGPRecipientKey() {
    if (!pgpRecipientImportFilePath || !pgpRecipientImportEmail.trim()) {
      pgpRecipientImportError = 'Please enter the recipient email address'
      return
    }
    pgpRecipientImporting = true
    pgpRecipientImportError = ''
    try {
      await ImportRecipientPGPKey(pgpRecipientImportEmail.trim(), pgpRecipientImportFilePath)
      addToast({ type: 'success', message: `PGP key imported for ${pgpRecipientImportEmail.trim()}` })
      showPGPRecipientImportDialog = false
      pgpRecipientImportFilePath = ''
      pgpRecipientImportEmail = ''
      await loadData()
    } catch (err) {
      pgpRecipientImportError = err instanceof Error ? err.message : String(err)
    } finally {
      pgpRecipientImporting = false
    }
  }

  function handleCancelPGPRecipientImport() {
    showPGPRecipientImportDialog = false
    pgpRecipientImportFilePath = ''
    pgpRecipientImportEmail = ''
    pgpRecipientImportError = ''
  }

  async function handleDeletePGPSenderKey(keyId: string) {
    try {
      await DeletePGPSenderKey(keyId)
      addToast({ type: 'success', message: 'Recipient PGP key removed' })
      await loadData()
    } catch (err) {
      addToast({ type: 'error', message: err instanceof Error ? err.message : 'Failed to remove recipient PGP key' })
    }
  }

  async function handleAddKeyServer() {
    const url = newKeyServerURL.trim()
    if (!url) return
    addingKeyServer = true
    try {
      await AddPGPKeyServer(url)
      addToast({ type: 'success', message: 'Key server added' })
      newKeyServerURL = ''
      await loadData()
    } catch (err) {
      addToast({ type: 'error', message: err instanceof Error ? err.message : 'Failed to add key server' })
    } finally {
      addingKeyServer = false
    }
  }

  async function handleRemoveKeyServer(id: number) {
    try {
      await RemovePGPKeyServer(id)
      addToast({ type: 'success', message: 'Key server removed' })
      await loadData()
    } catch (err) {
      addToast({ type: 'error', message: err instanceof Error ? err.message : 'Failed to remove key server' })
    }
  }

  async function handleKeyLookup() {
    if (!keyLookupEmail.trim()) return
    keyLookupLoading = true
    try {
      const armored = await LookupPGPKey(keyLookupEmail.trim())
      if (armored) {
        addToast({ type: 'success', message: `PGP key found and imported for ${keyLookupEmail.trim()}` })
        keyLookupEmail = ''
        await loadData()
      } else {
        addToast({ type: 'info', message: `No PGP key found for ${keyLookupEmail.trim()}` })
      }
    } catch (err) {
      addToast({ type: 'error', message: err instanceof Error ? err.message : 'Key lookup failed' })
    } finally {
      keyLookupLoading = false
    }
  }

  function formatDate(dateStr: any): string {
    if (!dateStr) return 'N/A'
    try {
      const d = new Date(dateStr)
      return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
    } catch {
      return 'N/A'
    }
  }

  function getFileName(path: string): string {
    return path.split('/').pop() || path.split('\\').pop() || path
  }
</script>

<div class="space-y-6">
  {#if loading}
    <div class="flex items-center justify-center py-8">
      <Icon icon="mdi:loading" class="w-6 h-6 animate-spin text-muted-foreground" />
    </div>
  {:else}
    <!-- PGP Section -->
    <div class="space-y-4">
      <button
        class="w-full flex items-center gap-2 text-sm font-semibold text-foreground hover:text-primary transition-colors text-left"
        onclick={() => pgpCollapsed = !pgpCollapsed}
      >
        <Icon icon={pgpCollapsed ? 'mdi:chevron-right' : 'mdi:chevron-down'} class="w-4 h-4 flex-shrink-0" />
        <Icon icon="mdi:key-outline" class="w-4 h-4" />
        PGP
        {#if pgpKeys.length > 0}
          <span class="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground font-medium">{pgpKeys.length} key{pgpKeys.length !== 1 ? 's' : ''}</span>
        {/if}
      </button>

      {#if !pgpCollapsed}
      <!-- Your PGP Keys -->
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <h4 class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Your Keys</h4>
          <Button variant="outline" size="sm" onclick={handlePickAndImportPGP}>
            <Icon icon="mdi:key-plus" class="w-4 h-4 mr-1" />
            Import Secret Key
          </Button>
        </div>

        {#if pgpKeys.length === 0}
          <p class="text-sm text-muted-foreground py-2">No PGP keys imported. Import an ASCII-armored (.asc) or binary (.gpg) key file to enable PGP signing and encryption.</p>
        {:else}
          <div class="space-y-2">
            {#each pgpKeys as key}
              <div class="flex items-start gap-3 p-3 rounded-md border border-border bg-card">
                <div class="flex-shrink-0 mt-0.5">
                  {#if key.isExpired}
                    <Icon icon="mdi:key-remove" class="w-5 h-5 text-destructive" />
                  {:else}
                    <Icon icon="mdi:key" class="w-5 h-5 text-green-600 dark:text-green-400" />
                  {/if}
                </div>
                <div class="flex-1 min-w-0">
                  <div class="flex items-center gap-2">
                    <span class="text-sm font-medium truncate">{key.email}</span>
                    {#if key.isDefault}
                      <span class="text-[10px] px-1.5 py-0.5 rounded bg-primary/10 text-primary font-medium">Default</span>
                    {/if}
                    {#if key.isExpired}
                      <span class="text-[10px] px-1.5 py-0.5 rounded bg-destructive/10 text-destructive font-medium">Expired</span>
                    {/if}
                  </div>
                  <p class="text-xs text-muted-foreground truncate mt-0.5">{key.userId}</p>
                  <p class="text-xs text-muted-foreground mt-0.5">
                    {key.algorithm}{key.keySize ? ` ${key.keySize}-bit` : ''} &middot; {key.fingerprint?.slice(-16)}
                  </p>
                  <p class="text-xs text-muted-foreground">
                    Created: {formatDate(key.createdAtKey)}{key.expiresAtKey ? ` · Expires: ${formatDate(key.expiresAtKey)}` : ''}
                  </p>
                </div>
                <div class="flex items-center gap-1 flex-shrink-0">
                  {#if !key.isDefault}
                    <Button variant="ghost" size="sm" onclick={() => handleSetDefaultPGP(key.id)} title="Set as default">
                      <Icon icon="mdi:star-outline" class="w-4 h-4" />
                    </Button>
                  {/if}
                  <Button variant="ghost" size="sm" onclick={() => handleDeletePGPKey(key.id)} title="Remove key">
                    <Icon icon="mdi:delete-outline" class="w-4 h-4 text-destructive" />
                  </Button>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>

      <!-- PGP Signing Policy -->
      {#if pgpKeys.length > 0}
        <div class="space-y-2">
          <h4 class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Signing Policy</h4>
          <div class="flex items-center gap-4">
            <label class="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="pgpSignPolicy"
                value="never"
                checked={pgpSignPolicy === 'never'}
                onchange={() => handlePGPSignPolicyChange('never')}
                class="accent-primary"
              />
              Never sign by default
            </label>
            <label class="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="pgpSignPolicy"
                value="always"
                checked={pgpSignPolicy === 'always'}
                onchange={() => handlePGPSignPolicyChange('always')}
                class="accent-primary"
              />
              Always sign by default
            </label>
          </div>
          <p class="text-xs text-muted-foreground">You can override this per-message in the composer.</p>
        </div>

        <!-- PGP Encryption Policy -->
        <div class="space-y-2">
          <h4 class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Encryption Policy</h4>
          <div class="flex items-center gap-4">
            <label class="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="pgpEncryptPolicy"
                value="never"
                checked={pgpEncryptPolicy === 'never'}
                onchange={() => handlePGPEncryptPolicyChange('never')}
                class="accent-primary"
              />
              Never encrypt by default
            </label>
            <label class="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="pgpEncryptPolicy"
                value="always"
                checked={pgpEncryptPolicy === 'always'}
                onchange={() => handlePGPEncryptPolicyChange('always')}
                class="accent-primary"
              />
              Always encrypt by default
            </label>
          </div>
          <p class="text-xs text-muted-foreground">You can override this per-message in the composer. Encryption requires recipient public keys.</p>
        </div>
      {/if}

      <!-- Key Servers -->
      <div class="space-y-3">
        <button
          class="w-full flex items-center gap-2 text-xs font-medium text-muted-foreground uppercase tracking-wider hover:text-foreground transition-colors text-left"
          onclick={() => keyServersCollapsed = !keyServersCollapsed}
        >
          <Icon icon={keyServersCollapsed ? 'mdi:chevron-right' : 'mdi:chevron-down'} class="w-3.5 h-3.5 flex-shrink-0" />
          KEY SERVERS
          <span class="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground font-medium">{keyServers.length}</span>
        </button>

        {#if !keyServersCollapsed}
          {#if keyServers.length > 0}
            <div class="space-y-1">
              {#each keyServers as server}
                <div class="flex items-center gap-3 p-2 rounded-md border border-border">
                  <Icon icon="mdi:web" class="w-4 h-4 text-muted-foreground flex-shrink-0" />
                  <span class="text-sm flex-1 truncate">{server.url.replace('https://', '')}</span>
                  <Button variant="ghost" size="sm" onclick={() => handleRemoveKeyServer(server.id)} title="Remove server">
                    <Icon icon="mdi:close" class="w-3.5 h-3.5" />
                  </Button>
                </div>
              {/each}
            </div>
          {/if}

          <div class="flex items-center gap-2">
            <input
              type="url"
              bind:value={newKeyServerURL}
              placeholder="https://"
              class="flex-1 px-3 py-1.5 rounded-md border border-border bg-background text-sm focus:outline-none focus:ring-2 focus:ring-primary"
              onkeydown={(e) => { if (e.key === 'Enter') handleAddKeyServer() }}
            />
            <Button variant="outline" size="sm" onclick={handleAddKeyServer} disabled={addingKeyServer || !newKeyServerURL.trim()}>
              {#if addingKeyServer}
                <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
              {:else}
                Add
              {/if}
            </Button>
          </div>
          <p class="text-xs text-muted-foreground">Add key servers to search for recipient PGP keys. Servers are queried in order during key lookups.</p>
        {/if}
      </div>

      <!-- PGP Recipient Keys -->
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <h4 class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Recipient Keys</h4>
          <Button variant="outline" size="sm" onclick={handlePickPGPRecipientKey}>
            <Icon icon="mdi:key-plus" class="w-4 h-4 mr-1" />
            Import
          </Button>
        </div>

        <!-- Key Lookup (WKD + HKP) -->
        <div class="flex items-center gap-2">
          <input
            type="email"
            bind:value={keyLookupEmail}
            placeholder="Search by email (WKD + key servers)..."
            class="flex-1 px-3 py-1.5 rounded-md border border-border bg-background text-sm focus:outline-none focus:ring-2 focus:ring-primary"
            onkeydown={(e) => { if (e.key === 'Enter') handleKeyLookup() }}
          />
          <Button variant="outline" size="sm" onclick={handleKeyLookup} disabled={keyLookupLoading || !keyLookupEmail.trim()}>
            {#if keyLookupLoading}
              <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
            {:else}
              <Icon icon="mdi:magnify" class="w-4 h-4" />
            {/if}
          </Button>
        </div>

        {#if pgpSenderKeys.length === 0}
          <p class="text-sm text-muted-foreground py-2">No recipient keys collected yet. Keys are automatically saved when you receive PGP-signed messages, discovered via WKD or key servers, or imported manually.</p>
        {:else}
          <div class="space-y-2">
            {#each pgpSenderKeys as key}
              <div class="flex items-center gap-3 p-2 rounded-md border border-border">
                <Icon icon="mdi:key-variant" class="w-4 h-4 text-muted-foreground flex-shrink-0" />
                <div class="flex-1 min-w-0">
                  <span class="text-sm truncate block">{key.email}</span>
                  <span class="text-xs text-muted-foreground truncate block">{key.fingerprint?.slice(-16)} &middot; {key.algorithm}</span>
                </div>
                <span class="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground font-medium flex-shrink-0">
                  {key.source}
                </span>
                <span class="text-xs text-muted-foreground flex-shrink-0">
                  {formatDate(key.lastSeenAt)}
                </span>
                <Button variant="ghost" size="sm" onclick={() => handleDeletePGPSenderKey(key.id)} title="Remove">
                  <Icon icon="mdi:close" class="w-3.5 h-3.5" />
                </Button>
              </div>
            {/each}
          </div>
        {/if}
      </div>
      {/if}
    </div>

    <!-- S/MIME Section -->
    <div class="space-y-4 mt-8 pt-6 border-t border-border">
      <button
        class="w-full flex items-center gap-2 text-sm font-semibold text-foreground hover:text-primary transition-colors text-left"
        onclick={() => smimeCollapsed = !smimeCollapsed}
      >
        <Icon icon={smimeCollapsed ? 'mdi:chevron-right' : 'mdi:chevron-down'} class="w-4 h-4 flex-shrink-0" />
        <Icon icon="mdi:shield-lock-outline" class="w-4 h-4" />
        S/MIME
        {#if certificates.length > 0}
          <span class="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground font-medium">{certificates.length} cert{certificates.length !== 1 ? 's' : ''}</span>
        {/if}
      </button>

      {#if !smimeCollapsed}
      <!-- Your Certificates -->
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <h4 class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Your Certificates</h4>
          <Button variant="outline" size="sm" onclick={handlePickAndImport}>
            <Icon icon="mdi:certificate" class="w-4 h-4 mr-1" />
            Import .p12
          </Button>
        </div>

        {#if certificates.length === 0}
          <p class="text-sm text-muted-foreground py-2">No certificates imported. Import a PKCS#12 (.p12/.pfx) certificate to enable message signing.</p>
        {:else}
          <div class="space-y-2">
            {#each certificates as cert}
              <div class="flex items-start gap-3 p-3 rounded-md border border-border bg-card">
                <div class="flex-shrink-0 mt-0.5">
                  {#if cert.isExpired}
                    <Icon icon="mdi:certificate-outline" class="w-5 h-5 text-destructive" />
                  {:else}
                    <Icon icon="mdi:certificate" class="w-5 h-5 text-green-600 dark:text-green-400" />
                  {/if}
                </div>
                <div class="flex-1 min-w-0">
                  <div class="flex items-center gap-2">
                    <span class="text-sm font-medium truncate">{cert.email}</span>
                    {#if cert.isDefault}
                      <span class="text-[10px] px-1.5 py-0.5 rounded bg-primary/10 text-primary font-medium">Default</span>
                    {/if}
                    {#if cert.isExpired}
                      <span class="text-[10px] px-1.5 py-0.5 rounded bg-destructive/10 text-destructive font-medium">Expired</span>
                    {/if}
                    {#if cert.isSelfSigned}
                      <span class="text-[10px] px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-600 dark:text-amber-400 font-medium">Self-signed</span>
                    {/if}
                  </div>
                  <p class="text-xs text-muted-foreground truncate mt-0.5">{cert.subject}</p>
                  <p class="text-xs text-muted-foreground mt-0.5">
                    Issuer: {cert.issuer}
                  </p>
                  <p class="text-xs text-muted-foreground">
                    Valid: {formatDate(cert.notBefore)} - {formatDate(cert.notAfter)}
                  </p>
                </div>
                <div class="flex items-center gap-1 flex-shrink-0">
                  {#if !cert.isDefault}
                    <Button variant="ghost" size="sm" onclick={() => handleSetDefault(cert.id)} title="Set as default">
                      <Icon icon="mdi:star-outline" class="w-4 h-4" />
                    </Button>
                  {/if}
                  <Button variant="ghost" size="sm" onclick={() => handleDeleteCert(cert.id)} title="Remove certificate">
                    <Icon icon="mdi:delete-outline" class="w-4 h-4 text-destructive" />
                  </Button>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>

      <!-- Signing Policy -->
      {#if certificates.length > 0}
        <div class="space-y-2">
          <h4 class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Signing Policy</h4>
          <div class="flex items-center gap-4">
            <label class="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="signPolicy"
                value="never"
                checked={signPolicy === 'never'}
                onchange={() => handleSignPolicyChange('never')}
                class="accent-primary"
              />
              Never sign by default
            </label>
            <label class="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="signPolicy"
                value="always"
                checked={signPolicy === 'always'}
                onchange={() => handleSignPolicyChange('always')}
                class="accent-primary"
              />
              Always sign by default
            </label>
          </div>
          <p class="text-xs text-muted-foreground">You can override this per-message in the composer.</p>
        </div>

        <!-- Encryption Policy -->
        <div class="space-y-2">
          <h4 class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Encryption Policy</h4>
          <div class="flex items-center gap-4">
            <label class="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="encryptPolicy"
                value="never"
                checked={encryptPolicy === 'never'}
                onchange={() => handleEncryptPolicyChange('never')}
                class="accent-primary"
              />
              Never encrypt by default
            </label>
            <label class="flex items-center gap-2 text-sm cursor-pointer">
              <input
                type="radio"
                name="encryptPolicy"
                value="always"
                checked={encryptPolicy === 'always'}
                onchange={() => handleEncryptPolicyChange('always')}
                class="accent-primary"
              />
              Always encrypt by default
            </label>
          </div>
          <p class="text-xs text-muted-foreground">You can override this per-message in the composer. Encryption requires recipient certificates.</p>
        </div>
      {/if}

      <!-- Sender Certificates (auto-collected) -->
      <div class="space-y-3">
        <div class="flex items-center justify-between">
          <h4 class="text-xs font-medium text-muted-foreground uppercase tracking-wider">Recipient Certificates</h4>
          <Button variant="outline" size="sm" onclick={handlePickRecipientCert}>
            <Icon icon="mdi:certificate" class="w-4 h-4 mr-1" />
            Import
          </Button>
        </div>
        {#if senderCerts.length === 0}
          <p class="text-sm text-muted-foreground py-2">No recipient certificates collected yet. Certificates are automatically saved when you receive signed messages, or you can import them manually.</p>
        {:else}
          <div class="space-y-2">
            {#each senderCerts as cert}
              <div class="flex items-center gap-3 p-2 rounded-md border border-border">
                <Icon icon="mdi:account-key-outline" class="w-4 h-4 text-muted-foreground flex-shrink-0" />
                <div class="flex-1 min-w-0">
                  <span class="text-sm truncate block">{cert.email}</span>
                  <span class="text-xs text-muted-foreground truncate block">{cert.subject}</span>
                </div>
                <span class="text-xs text-muted-foreground flex-shrink-0">
                  {formatDate(cert.lastSeenAt)}
                </span>
                <Button variant="ghost" size="sm" onclick={() => handleDeleteSenderCert(cert.id)} title="Remove">
                  <Icon icon="mdi:close" class="w-3.5 h-3.5" />
                </Button>
              </div>
            {/each}
          </div>
        {/if}
      </div>
      {/if}
    </div>
  {/if}
</div>

<!-- Import Dialog -->
{#if showImportDialog}
  <div class="fixed inset-0 z-50 flex items-center justify-center">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div role="button" tabindex="-1" class="absolute inset-0 bg-black/50" onclick={handleCancelImport} onkeydown={(e) => { if (e.key === 'Escape') handleCancelImport() }}></div>
    <div class="relative bg-background border border-border rounded-lg shadow-xl p-6 w-full max-w-md mx-4">
      <h3 class="text-lg font-semibold mb-4">Import Certificate</h3>

      <div class="space-y-4">
        <div>
          <p class="text-sm text-muted-foreground mb-1">File</p>
          <p class="text-sm font-mono bg-muted/50 px-3 py-2 rounded truncate">{getFileName(importFilePath)}</p>
        </div>

        <div>
          <label for="cert-password" class="text-sm text-muted-foreground block mb-1">
            Certificate Password
          </label>
          <input
            id="cert-password"
            type="password"
            bind:value={importPassword}
            placeholder="Enter the password for this .p12 file"
            class="w-full px-3 py-2 rounded-md border border-border bg-background text-sm focus:outline-none focus:ring-2 focus:ring-primary"
            onkeydown={(e) => { if (e.key === 'Enter') handleImport() }}
          />
          <p class="text-xs text-muted-foreground mt-1">The password used when the certificate was exported.</p>
        </div>

        {#if importError}
          <div class="text-sm text-destructive bg-destructive/10 px-3 py-2 rounded-md">
            {importError}
          </div>
        {/if}
      </div>

      <div class="flex items-center justify-end gap-2 mt-6">
        <Button variant="ghost" onclick={handleCancelImport} disabled={importing}>
          Cancel
        </Button>
        <Button onclick={handleImport} disabled={importing}>
          {#if importing}
            <Icon icon="mdi:loading" class="w-4 h-4 mr-2 animate-spin" />
          {/if}
          Import
        </Button>
      </div>
    </div>
  </div>
{/if}

<!-- Recipient Cert Import Dialog -->
{#if showRecipientImportDialog}
  <div class="fixed inset-0 z-50 flex items-center justify-center">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div role="button" tabindex="-1" class="absolute inset-0 bg-black/50" onclick={handleCancelRecipientImport} onkeydown={(e) => { if (e.key === 'Escape') handleCancelRecipientImport() }}></div>
    <div class="relative bg-background border border-border rounded-lg shadow-xl p-6 w-full max-w-md mx-4">
      <h3 class="text-lg font-semibold mb-4">Import Recipient Certificate</h3>

      <div class="space-y-4">
        <div>
          <p class="text-sm text-muted-foreground mb-1">File</p>
          <p class="text-sm font-mono bg-muted/50 px-3 py-2 rounded truncate">{getFileName(recipientImportFilePath)}</p>
        </div>

        <div>
          <label for="recipient-email" class="text-sm text-muted-foreground block mb-1">
            Recipient Email Address
          </label>
          <input
            id="recipient-email"
            type="email"
            bind:value={recipientImportEmail}
            placeholder="recipient@example.com"
            class="w-full px-3 py-2 rounded-md border border-border bg-background text-sm focus:outline-none focus:ring-2 focus:ring-primary"
            onkeydown={(e) => { if (e.key === 'Enter') handleImportRecipientCert() }}
          />
          <p class="text-xs text-muted-foreground mt-1">The email address of the person this certificate belongs to.</p>
        </div>

        {#if recipientImportError}
          <div class="text-sm text-destructive bg-destructive/10 px-3 py-2 rounded-md">
            {recipientImportError}
          </div>
        {/if}
      </div>

      <div class="flex items-center justify-end gap-2 mt-6">
        <Button variant="ghost" onclick={handleCancelRecipientImport} disabled={recipientImporting}>
          Cancel
        </Button>
        <Button onclick={handleImportRecipientCert} disabled={recipientImporting}>
          {#if recipientImporting}
            <Icon icon="mdi:loading" class="w-4 h-4 mr-2 animate-spin" />
          {/if}
          Import
        </Button>
      </div>
    </div>
  </div>
{/if}

<!-- PGP Key Import Dialog -->
{#if showPGPImportDialog}
  <div class="fixed inset-0 z-50 flex items-center justify-center">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div role="button" tabindex="-1" class="absolute inset-0 bg-black/50" onclick={handleCancelPGPImport} onkeydown={(e) => { if (e.key === 'Escape') handleCancelPGPImport() }}></div>
    <div class="relative bg-background border border-border rounded-lg shadow-xl p-6 w-full max-w-md mx-4">
      <h3 class="text-lg font-semibold mb-4">Import PGP Key</h3>

      <div class="space-y-4">
        <div>
          <p class="text-sm text-muted-foreground mb-1">File</p>
          <p class="text-sm font-mono bg-muted/50 px-3 py-2 rounded truncate">{getFileName(pgpImportFilePath)}</p>
        </div>

        <div>
          <label for="pgp-passphrase" class="text-sm text-muted-foreground block mb-1">
            Key Passphrase (optional)
          </label>
          <input
            id="pgp-passphrase"
            type="password"
            bind:value={pgpImportPassphrase}
            placeholder="Enter passphrase if the key is encrypted"
            class="w-full px-3 py-2 rounded-md border border-border bg-background text-sm focus:outline-none focus:ring-2 focus:ring-primary"
            onkeydown={(e) => { if (e.key === 'Enter') handleImportPGP() }}
          />
          <p class="text-xs text-muted-foreground mt-1">Leave blank if the private key is not passphrase-protected.</p>
        </div>

        {#if pgpImportError}
          <div class="text-sm text-destructive bg-destructive/10 px-3 py-2 rounded-md">
            {pgpImportError}
          </div>
        {/if}
      </div>

      <div class="flex items-center justify-end gap-2 mt-6">
        <Button variant="ghost" onclick={handleCancelPGPImport} disabled={pgpImporting}>
          Cancel
        </Button>
        <Button onclick={handleImportPGP} disabled={pgpImporting}>
          {#if pgpImporting}
            <Icon icon="mdi:loading" class="w-4 h-4 mr-2 animate-spin" />
          {/if}
          Import
        </Button>
      </div>
    </div>
  </div>
{/if}

<!-- PGP Recipient Key Import Dialog -->
{#if showPGPRecipientImportDialog}
  <div class="fixed inset-0 z-50 flex items-center justify-center">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div role="button" tabindex="-1" class="absolute inset-0 bg-black/50" onclick={handleCancelPGPRecipientImport} onkeydown={(e) => { if (e.key === 'Escape') handleCancelPGPRecipientImport() }}></div>
    <div class="relative bg-background border border-border rounded-lg shadow-xl p-6 w-full max-w-md mx-4">
      <h3 class="text-lg font-semibold mb-4">Import Recipient PGP Key</h3>

      <div class="space-y-4">
        <div>
          <p class="text-sm text-muted-foreground mb-1">File</p>
          <p class="text-sm font-mono bg-muted/50 px-3 py-2 rounded truncate">{getFileName(pgpRecipientImportFilePath)}</p>
        </div>

        <div>
          <label for="pgp-recipient-email" class="text-sm text-muted-foreground block mb-1">
            Recipient Email Address
          </label>
          <input
            id="pgp-recipient-email"
            type="email"
            bind:value={pgpRecipientImportEmail}
            placeholder="recipient@example.com"
            class="w-full px-3 py-2 rounded-md border border-border bg-background text-sm focus:outline-none focus:ring-2 focus:ring-primary"
            onkeydown={(e) => { if (e.key === 'Enter') handleImportPGPRecipientKey() }}
          />
          <p class="text-xs text-muted-foreground mt-1">The email address of the person this key belongs to.</p>
        </div>

        {#if pgpRecipientImportError}
          <div class="text-sm text-destructive bg-destructive/10 px-3 py-2 rounded-md">
            {pgpRecipientImportError}
          </div>
        {/if}
      </div>

      <div class="flex items-center justify-end gap-2 mt-6">
        <Button variant="ghost" onclick={handleCancelPGPRecipientImport} disabled={pgpRecipientImporting}>
          Cancel
        </Button>
        <Button onclick={handleImportPGPRecipientKey} disabled={pgpRecipientImporting}>
          {#if pgpRecipientImporting}
            <Icon icon="mdi:loading" class="w-4 h-4 mr-2 animate-spin" />
          {/if}
          Import
        </Button>
      </div>
    </div>
  </div>
{/if}
