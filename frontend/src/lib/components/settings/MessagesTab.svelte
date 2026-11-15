<script lang="ts">
  import { Label } from '$lib/components/ui/label'
  import * as Select from '$lib/components/ui/select'
  import { _ } from '$lib/i18n'

  interface Props {
    codeChipClickMode: string
    otpClipboardClearSeconds: number
    onCodeChipClickModeChange: (value: string) => void
    onOtpClipboardClearSecondsChange: (value: number) => void
  }

  let {
    codeChipClickMode = $bindable(),
    otpClipboardClearSeconds = $bindable(),
    onCodeChipClickModeChange,
    onOtpClipboardClearSecondsChange,
  }: Props = $props()

  const codeChipOptions = $derived([
    { value: 'copy_mark_read_delete', label: $_('settingsMessages.codeChipCopyMarkReadDelete') },
    { value: 'copy_mark_read', label: $_('settingsMessages.codeChipCopyMarkRead') },
    { value: 'copy_only', label: $_('settingsMessages.codeChipCopyOnly') },
  ])

  function getCodeChipLabel(value: string): string {
    return codeChipOptions.find((opt) => opt.value === value)?.label || value
  }

  function handleCodeChipChange(value: string) {
    codeChipClickMode = value
    onCodeChipClickModeChange?.(value)
  }

  function handleClearSecondsInput(e: Event) {
    const target = e.target as HTMLInputElement
    const value = parseInt(target.value, 10)
    const clamped = Number.isNaN(value) ? 0 : Math.max(0, Math.min(3600, value))
    otpClipboardClearSeconds = clamped
    onOtpClipboardClearSecondsChange?.(clamped)
  }
</script>

<div class="space-y-6">
  <!-- Code chip click -->
  <div class="space-y-2">
    <Label>{$_('settingsMessages.codeChipClick')}</Label>
    <Select.Root value={codeChipClickMode} onValueChange={handleCodeChipChange}>
      <Select.Trigger>
        <Select.Value>
          {getCodeChipLabel(codeChipClickMode)}
        </Select.Value>
      </Select.Trigger>
      <Select.Content>
        {#each codeChipOptions as opt (opt.value)}
          <Select.Item value={opt.value} label={opt.label} />
        {/each}
      </Select.Content>
    </Select.Root>
    <p class="text-xs text-muted-foreground">
      {$_('settingsMessages.codeChipClickHelp')}
    </p>
  </div>

  <!-- Divider -->
  <div class="border-t border-border"></div>

  <!-- Clipboard clear delay -->
  <div class="space-y-2">
    <Label for="otp-clear-seconds">{$_('settingsMessages.otpClearSeconds')}</Label>
    <input
      id="otp-clear-seconds"
      type="number"
      min="0"
      max="3600"
      step="1"
      value={otpClipboardClearSeconds}
      oninput={handleClearSecondsInput}
      class="w-24 rounded-md border border-input bg-background px-3 py-1.5 text-sm"
    />
    <p class="text-xs text-muted-foreground">
      {$_('settingsMessages.otpClearSecondsHelp')}
    </p>
  </div>
</div>
