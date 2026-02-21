<script lang="ts">
  import { fly } from 'svelte/transition'
  import { _ } from '$lib/i18n'

  interface Props {
    value: string
    onchange: (color: string) => void
  }

  let { value, onchange }: Props = $props()

  // Default color presets
  const presets = [
    '#3B82F6', // blue
    '#10B981', // green
    '#F59E0B', // amber
    '#EF4444', // red
    '#8B5CF6', // purple
    '#EC4899', // pink
    '#06B6D4', // cyan
    '#F97316', // orange
  ]

  let isOpen = $state(false)
  let hexInput = $state(presets[0])
  let popoverRef: HTMLDivElement | null = $state(null)

  // Sync hex input with value prop (including initial value)
  $effect(() => {
    hexInput = value || presets[0]
  })

  function togglePopover() {
    isOpen = !isOpen
  }

  function selectPreset(color: string) {
    hexInput = color
    onchange(color)
    isOpen = false
  }

  function handleHexInput(e: Event) {
    const input = e.target as HTMLInputElement
    let hex = input.value.trim()
    
    // Auto-add # if missing
    if (hex && !hex.startsWith('#')) {
      hex = '#' + hex
    }
    
    hexInput = hex
    
    // Validate hex color
    if (/^#[0-9A-Fa-f]{6}$/.test(hex)) {
      onchange(hex)
    }
  }

  function handleHexKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      isOpen = false
    }
  }

  // Close popover when clicking outside
  function handleClickOutside(e: MouseEvent) {
    if (popoverRef && !popoverRef.contains(e.target as Node)) {
      isOpen = false
    }
  }

  $effect(() => {
    if (isOpen) {
      document.addEventListener('click', handleClickOutside, true)
      return () => {
        document.removeEventListener('click', handleClickOutside, true)
      }
    }
  })

  // Display color (use value or fallback to first preset)
  const displayColor = $derived(value || presets[0])
</script>

<div class="relative inline-block" bind:this={popoverRef}>
  <!-- Color swatch button -->
  <button
    type="button"
    class="w-8 h-8 rounded-md border border-border shadow-sm cursor-pointer hover:ring-2 hover:ring-primary/50 transition-all"
    style="background-color: {displayColor}"
    onclick={togglePopover}
    aria-label={$_('aria.selectColor')}
  ></button>

  <!-- Popover -->
  {#if isOpen}
    <div
      class="absolute left-0 top-full mt-2 z-50 bg-popover border border-border rounded-lg shadow-lg p-3 w-56"
      transition:fly={{ y: -5, duration: 150 }}
    >
      <!-- Preset colors grid -->
      <div class="grid grid-cols-4 gap-2 mb-3">
        {#each presets as preset}
          <button
            type="button"
            class="w-10 h-10 rounded-md border-2 cursor-pointer hover:scale-110 transition-transform {preset === value ? 'border-primary ring-2 ring-primary/50' : 'border-transparent'}"
            style="background-color: {preset}"
            onclick={() => selectPreset(preset)}
            aria-label={$_('aria.selectPresetColor', { values: { color: preset } })}
          ></button>
        {/each}
      </div>

      <!-- Hex input -->
      <div class="flex items-center gap-2">
        <div
          class="w-8 h-8 rounded border border-border flex-shrink-0"
          style="background-color: {hexInput}"
        ></div>
        <input
          type="text"
          class="flex-1 h-8 px-2 text-sm bg-background border border-border rounded focus:outline-none focus:ring-2 focus:ring-primary/50 font-mono"
          placeholder="#000000"
          value={hexInput}
          oninput={handleHexInput}
          onkeydown={handleHexKeydown}
          maxlength="7"
        />
      </div>
    </div>
  {/if}
</div>
