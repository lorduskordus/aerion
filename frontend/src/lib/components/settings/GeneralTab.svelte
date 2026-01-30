<script lang="ts">
  import Icon from '@iconify/svelte'
  import * as Select from '$lib/components/ui/select'
  import { Label } from '$lib/components/ui/label'
  import { Input } from '$lib/components/ui/input'
  import Switch from '$lib/components/ui/switch/Switch.svelte'

  interface Props {
    readReceiptResponsePolicy: string
    markAsReadDelaySeconds: number
    messageListDensity: string
    themeMode: string
    showTitleBar: boolean
    onPolicyChange: (value: string) => void
    onDelayChange: (value: number) => void
    onDensityChange: (value: string) => void
    onThemeChange: (value: string) => void
    onShowTitleBarChange: (value: boolean) => void
  }

  let {
    readReceiptResponsePolicy = $bindable(),
    markAsReadDelaySeconds = $bindable(),
    messageListDensity = $bindable(),
    themeMode = $bindable(),
    showTitleBar = $bindable(),
    onPolicyChange,
    onDelayChange,
    onDensityChange,
    onThemeChange,
    onShowTitleBarChange,
  }: Props = $props()

  // Read receipt response policy options
  const readReceiptResponseOptions = [
    { value: 'never', label: 'Never send read receipts' },
    { value: 'ask', label: 'Ask me each time' },
    { value: 'always', label: 'Always send read receipts' },
  ]

  // Message list density options
  const densityOptions = [
    { value: 'micro', label: 'Micro' },
    { value: 'compact', label: 'Compact' },
    { value: 'standard', label: 'Standard' },
    { value: 'large', label: 'Large' },
  ]

  // Theme mode options
  const themeModeOptions = [
    { value: 'system', label: 'System' },
    { value: 'light', label: 'Light' },
    { value: 'dark', label: 'Dark' },
  ]

  // Get the label for the current value
  function getSelectedLabel(value: string): string {
    return readReceiptResponseOptions.find(opt => opt.value === value)?.label || value
  }

  function getDensityLabel(value: string): string {
    return densityOptions.find(opt => opt.value === value)?.label || value
  }

  function getThemeModeLabel(value: string): string {
    return themeModeOptions.find(opt => opt.value === value)?.label || value
  }

  function handlePolicyChange(value: string) {
    readReceiptResponsePolicy = value
    onPolicyChange?.(value)
  }

  function handleDensityChange(value: string) {
    messageListDensity = value
    onDensityChange?.(value)
  }

  function handleThemeChange(value: string) {
    themeMode = value
    onThemeChange?.(value)
  }

  function handleShowTitleBarChange(value: boolean) {
    showTitleBar = value
    onShowTitleBarChange?.(value)
  }

  function handleDelayInput(e: Event) {
    const target = e.target as HTMLInputElement
    const value = parseFloat(target.value)
    markAsReadDelaySeconds = value
    onDelayChange?.(value)
  }
</script>

<div class="space-y-6">
  <!-- Display Section -->
  <div class="space-y-4">
    <h3 class="text-sm font-medium flex items-center gap-2">
      <Icon icon="mdi:format-size" class="w-4 h-4" />
      Display
    </h3>

    <div class="space-y-2">
      <div class="flex items-center justify-between">
        <div class="space-y-0.5">
          <Label for="show-title-bar">Show title bar</Label>
          <p class="text-xs text-muted-foreground">
            Disable for less UI clutter
          </p>
        </div>
        <Switch
          id="show-title-bar"
          bind:checked={showTitleBar}
          onCheckedChange={handleShowTitleBarChange}
        />
      </div>
    </div>

    <div class="space-y-2">
      <Label>Theme</Label>
      <Select.Root value={themeMode} onValueChange={handleThemeChange}>
        <Select.Trigger>
          <Select.Value placeholder="Select theme">
            {getThemeModeLabel(themeMode)}
          </Select.Value>
        </Select.Trigger>
        <Select.Content>
          {#each themeModeOptions as opt (opt.value)}
            <Select.Item value={opt.value} label={opt.label} />
          {/each}
        </Select.Content>
      </Select.Root>
      <p class="text-xs text-muted-foreground">
        Choose light, dark, or follow system preference
      </p>
    </div>

    <div class="space-y-2">
      <Label>Message list density</Label>
      <Select.Root value={messageListDensity} onValueChange={handleDensityChange}>
        <Select.Trigger>
          <Select.Value placeholder="Select density">
            {getDensityLabel(messageListDensity)}
          </Select.Value>
        </Select.Trigger>
        <Select.Content>
          {#each densityOptions as opt (opt.value)}
            <Select.Item value={opt.value} label={opt.label} />
          {/each}
        </Select.Content>
      </Select.Root>
      <p class="text-xs text-muted-foreground">
        Adjust the spacing and text size in the message list
      </p>
    </div>
  </div>

  <!-- Divider -->
  <div class="border-t border-border"></div>

  <!-- Read Receipts Section -->
  <div class="space-y-4">
    <h3 class="text-sm font-medium flex items-center gap-2">
      <Icon icon="mdi:email-check-outline" class="w-4 h-4" />
      Read Receipts
    </h3>
    
    <div class="space-y-2">
      <Label>When someone requests a read receipt</Label>
      <Select.Root value={readReceiptResponsePolicy} onValueChange={handlePolicyChange}>
        <Select.Trigger>
          <Select.Value placeholder="Select policy">
            {getSelectedLabel(readReceiptResponsePolicy)}
          </Select.Value>
        </Select.Trigger>
        <Select.Content>
          {#each readReceiptResponseOptions as opt (opt.value)}
            <Select.Item value={opt.value} label={opt.label} />
          {/each}
        </Select.Content>
      </Select.Root>
      <p class="text-xs text-muted-foreground">
        Read receipts notify the sender when you've read their message
      </p>
    </div>
  </div>

  <!-- Divider -->
  <div class="border-t border-border"></div>

  <!-- Mark as Read Section -->
  <div class="space-y-4">
    <h3 class="text-sm font-medium flex items-center gap-2">
      <Icon icon="mdi:email-open-outline" class="w-4 h-4" />
      Mark Messages as Read
    </h3>
    
    <div class="space-y-2">
      <Label>Mark as read after</Label>
      <div class="flex items-center gap-2">
        <Input
          type="number"
          value={markAsReadDelaySeconds}
          oninput={handleDelayInput}
          min={-1}
          max={5}
          step={0.1}
          class="w-24"
        />
        <span class="text-sm text-muted-foreground">seconds</span>
      </div>
      <p class="text-xs text-muted-foreground">
        -1 = manual only, 0 = immediate, 0.1-5 = delay in seconds
      </p>
    </div>
  </div>

</div>
