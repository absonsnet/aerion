<!-- Needs attention strip (Phase A). Separate bars ABOVE the message
     list that push content down, never overlay. Shown only for Inbox /
     Unified Inbox (the parent renders this component only then). Max 3
     visible + "+N more" expander. Nothing renders when there are no
     items — no "all caught up" banner. -->
<script lang="ts">
  import Icon from '@iconify/svelte'
  import { _ } from 'svelte-i18n'
  // @ts-ignore - wailsjs path
  import { GetAttentionItems, DismissAttention, SetAttentionEnabled } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import type { app } from '../../../../wailsjs/go/models'
  import { setAttentionEnabled } from '$lib/stores/settings.svelte'
  import { toasts } from '$lib/stores/toast'
  // @ts-ignore - wailsjs path
  import { EventsOn, EventsOff } from '../../../../wailsjs/runtime/runtime'
  import { onMount, onDestroy } from 'svelte'

  interface Props {
    folderId: string | null
    unified: boolean
    onOpenItem: (item: app.AttentionItem) => void
    /** Called when the count may have changed (dismiss / turn off). */
    onChanged?: () => void
  }

  let { folderId, unified, onOpenItem, onChanged }: Props = $props()

  let items = $state<app.AttentionItem[]>([])
  let expanded = $state(false)
  let showMenu = $state(false)

  const TYPE_ICON: Record<string, string> = {
    verification_code: 'mdi:shield-key-outline',
    flight_changed: 'mdi:airplane',
    payment_failed: 'mdi:credit-card-alert-outline',
    delivery_problem: 'mdi:truck-alert-outline'
  }

  function typeLabel(type: string): string {
    switch (type) {
      case 'verification_code': return $_('attention.typeVerificationCode')
      case 'flight_changed': return $_('attention.typeFlightChanged')
      case 'payment_failed': return $_('attention.typePaymentFailed')
      case 'delivery_problem': return $_('attention.typeDeliveryProblem')
      default: return type
    }
  }

  export async function refresh() {
    try {
      items = await GetAttentionItems(folderId ?? '', unified)
      expanded = false
    } catch (err) {
      console.error('Failed to load attention items:', err)
      items = []
    }
  }

  // Reload when the folder view changes.
  $effect(() => {
    void folderId
    void unified
    refresh()
  })

  // Backend emits this after chip actions, dismissals and settings
  // changes, so the strip and the sidebar badge stay in sync.
  onMount(() => {
    EventsOn('attention:changed', () => refresh())
  })
  onDestroy(() => {
    EventsOff('attention:changed')
  })

  async function dismiss(item: app.AttentionItem, e: MouseEvent) {
    e.stopPropagation()
    try {
      await DismissAttention(item.messageId)
      items = items.filter((i) => i.messageId !== item.messageId)
      onChanged?.()
    } catch (err) {
      console.error('Failed to dismiss attention item:', err)
      toasts.error($_('attention.failedToDismiss'))
    }
  }

  async function turnOff() {
    showMenu = false
    try {
      await SetAttentionEnabled(false)
      setAttentionEnabled(false)
      items = []
      onChanged?.()
      toasts.success($_('attention.turnedOff'))
    } catch (err) {
      console.error('Failed to turn off Needs attention:', err)
      toasts.error($_('toast.failedToSaveSettings'))
    }
  }

  let visibleItems = $derived(expanded ? items : items.slice(0, 3))
  let hiddenCount = $derived(items.length - visibleItems.length)
</script>

{#if items.length > 0}
  <div class="flex-shrink-0 border-b border-border bg-muted/40" data-attention-strip>
    <!-- Header: title + count + turn-off menu -->
    <div class="flex items-center justify-between px-3 pt-2 pb-1">
      <span class="flex items-center gap-1.5 text-xs font-semibold text-foreground">
        <Icon icon="mdi:bell-alert-outline" class="w-4 h-4 text-primary" />
        {$_('attention.title')}
        <span class="text-muted-foreground font-normal">({items.length})</span>
      </span>
      <div class="relative">
        <button
          type="button"
          class="p-0.5 rounded hover:bg-muted text-muted-foreground"
          title={$_('attention.turnOff')}
          aria-label={$_('attention.turnOff')}
          onclick={(e: MouseEvent) => { e.stopPropagation(); showMenu = !showMenu }}
        >
          <Icon icon="mdi:close" class="w-4 h-4" />
        </button>
        {#if showMenu}
          <!-- Backdrop to close the menu -->
          <button
            type="button"
            class="fixed inset-0 z-40 cursor-default"
            aria-label={$_('common.close')}
            onclick={() => { showMenu = false }}
          ></button>
          <div class="absolute right-0 top-full z-50 mt-1 min-w-52 rounded-md border border-border bg-popover shadow-lg py-1">
            <button
              type="button"
              class="w-full text-left px-3 py-2 text-sm hover:bg-muted"
              onclick={turnOff}
            >
              {$_('attention.turnOffMenu')}
            </button>
          </div>
        {/if}
      </div>
    </div>

    <!-- Bars -->
    <div class="px-2 pb-2 space-y-1.5">
      {#each visibleItems as item (item.messageId)}
        <button
          type="button"
          class="w-full flex items-center gap-2.5 text-left rounded-md border border-border bg-background px-3 py-2 hover:border-primary/50 hover:bg-primary/5 transition-colors"
          onclick={() => onOpenItem(item)}
        >
          <Icon icon={TYPE_ICON[item.type] ?? 'mdi:bell-alert-outline'} class="w-5 h-5 flex-shrink-0 text-primary" />
          <span class="flex-1 min-w-0">
            <span class="block truncate text-sm font-medium text-foreground">
              {item.subject || $_('viewer.noSubject')}
            </span>
            <span class="block truncate text-xs text-muted-foreground">
              {typeLabel(item.type)} · {item.accountName}
            </span>
          </span>
          <span
            role="button"
            tabindex="0"
            class="flex-shrink-0 text-xs text-muted-foreground hover:text-foreground px-1.5 py-1 rounded hover:bg-muted"
            onclick={(e: MouseEvent) => dismiss(item, e)}
            onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter' || e.key === ' ') { e.stopPropagation(); e.preventDefault(); dismiss(item, e as unknown as MouseEvent) } }}
          >
            {$_('attention.dismiss')}
          </span>
        </button>
      {/each}

      {#if hiddenCount > 0}
        <button
          type="button"
          class="w-full text-center text-xs text-primary hover:underline py-1"
          onclick={() => { expanded = true }}
        >
          {$_('attention.more', { values: { count: hiddenCount } })}
        </button>
      {:else if expanded && items.length > 3}
        <button
          type="button"
          class="w-full text-center text-xs text-primary hover:underline py-1"
          onclick={() => { expanded = false }}
        >
          {$_('attention.showLess')}
        </button>
      {/if}
    </div>
  </div>
{/if}
