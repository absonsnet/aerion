<script lang="ts">
  import { _ } from '$lib/i18n'
  import type { MessageOpenMode } from '$lib/stores/settings.svelte'

  interface Props {
    /** The mode the message is currently open in — rendered disabled/checked. */
    current: MessageOpenMode
    /** Called with the mode the user picked. The caller moves the message. */
    onSwitch: (mode: MessageOpenMode) => void
  }

  let { current, onSwitch }: Props = $props()

  const modes: { value: MessageOpenMode; labelKey: string; titleKey: string }[] = [
    { value: 'tab', labelKey: 'settingsGeneral.messageOpenModeTab', titleKey: 'messageView.switchToTab' },
    { value: 'dialog', labelKey: 'settingsGeneral.messageOpenModeDialog', titleKey: 'messageView.switchToDialog' },
    { value: 'popup', labelKey: 'settingsGeneral.messageOpenModePopup', titleKey: 'messageView.switchToPopup' },
  ]
</script>

<div
  class="flex flex-shrink-0 items-center gap-0.5 rounded-md border border-border bg-muted/40 p-0.5"
  role="group"
  aria-label={$_('messageView.viewAs')}
  title={$_('messageView.viewAs')}
>
  {#each modes as mode (mode.value)}
    <button
      type="button"
      class="rounded px-2 py-1 text-xs transition-colors {current === mode.value
        ? 'bg-background font-medium text-foreground shadow-sm'
        : 'text-muted-foreground hover:text-foreground'}"
      disabled={current === mode.value}
      aria-pressed={current === mode.value}
      title={$_(`${mode.titleKey}`)}
      onclick={() => onSwitch(mode.value)}
    >
      {$_(`${mode.labelKey}`)}
    </button>
  {/each}
</div>
