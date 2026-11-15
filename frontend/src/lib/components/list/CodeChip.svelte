<!-- Code/reference chip (Phase A). Whole chip is the click target
     (min 40px tall). Clicking never opens the message: the click handler
     stops propagation, same pattern as the star button. -->
<script lang="ts">
  import Icon from '@iconify/svelte'
  import { _ } from 'svelte-i18n'
  import { handleCodeChipClick } from '$lib/utils/codeChip'

  interface Props {
    messageId: string
    kind: 'code' | 'reference'
    value: string
    onDone?: () => void
  }

  let { messageId, kind, value, onDone }: Props = $props()

  async function onChipClick(e: MouseEvent) {
    e.stopPropagation()
    e.preventDefault()
    await handleCodeChipClick({ messageId, kind, value, onDone })
  }
</script>

<button
  type="button"
  class="flex items-center gap-1.5 min-h-10 px-2.5 py-1 rounded-md border text-left transition-colors {kind === 'code'
    ? 'border-primary/40 bg-primary/10 text-primary hover:bg-primary/20'
    : 'border-border bg-muted/60 text-muted-foreground hover:bg-muted'}"
  title={kind === 'code' ? $_('chip.codeClickHint') : $_('chip.referenceClickHint')}
  onclick={onChipClick}
  onkeydown={(e: KeyboardEvent) => {
    // Keyboard activation on the chip must not reach the row handler.
    if (e.key === 'Enter' || e.key === ' ') e.stopPropagation()
  }}
>
  <Icon icon={kind === 'code' ? 'mdi:shield-key-outline' : 'mdi:ticket-confirmation-outline'} class="w-4 h-4 flex-shrink-0" />
  <span class="text-sm font-mono font-semibold tracking-wider">{value}</span>
  <span class="text-xs font-normal opacity-80">
    {kind === 'code' ? $_('chip.codeLabel') : $_('chip.referenceLabel')}
  </span>
  <Icon icon="mdi:content-copy" class="w-3.5 h-3.5 flex-shrink-0 opacity-60" />
</button>
