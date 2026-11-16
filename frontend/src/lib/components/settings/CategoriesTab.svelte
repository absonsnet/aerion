<script lang="ts">
  import { onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import { Label } from '$lib/components/ui/label'
  import { Button } from '$lib/components/ui/button'
  // @ts-ignore - wailsjs path
  import { GetSenderCategoryOverrides, RemoveSenderCategoryOverride, GetBackfillStatus, RunEnrichmentBackfillNow } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import type { app as appModels } from '../../../../wailsjs/go/models'
  import { toasts } from '$lib/stores/toast'
  import { _ } from '$lib/i18n'

  interface Props {
    inboxTabDefault: string
    onInboxTabDefaultChange: (value: string) => void
  }

  let {
    inboxTabDefault = $bindable(),
    onInboxTabDefaultChange,
  }: Props = $props()

  // Sender overrides + backfill status load themselves (like the
  // Subscriptions tab): they act immediately, not through Save.
  let overrides = $state<appModels.SenderCategoryOverride[]>([])
  let overridesLoading = $state(true)
  let removingSender = $state<string | null>(null)
  let backfillStatus = $state<appModels.BackfillStatus | null>(null)
  let backfillRunning = $state(false)
  let lastClassified = $state<number | null>(null)

  const defaultTabOptions = $derived([
    { value: 'all', label: $_('settingsCategories.defaultTabAll') },
    { value: 'personal', label: $_('settingsCategories.defaultTabPersonal') },
    { value: 'automated', label: $_('settingsCategories.defaultTabAutomated') },
  ])

  onMount(() => {
    loadOverrides()
    loadBackfillStatus()
  })

  async function loadOverrides() {
    overridesLoading = true
    try {
      overrides = (await GetSenderCategoryOverrides()) ?? []
    } catch (err) {
      console.error('Failed to load sender category overrides:', err)
      toasts.error($_('settingsCategories.failedToLoad'))
    } finally {
      overridesLoading = false
    }
  }

  async function loadBackfillStatus() {
    try {
      backfillStatus = await GetBackfillStatus()
    } catch (err) {
      console.error('Failed to load backfill status:', err)
    }
  }

  async function handleRemoveOverride(senderEmail: string) {
    removingSender = senderEmail
    try {
      await RemoveSenderCategoryOverride(senderEmail)
      overrides = overrides.filter((o) => o.senderEmail !== senderEmail)
    } catch (err) {
      console.error('Failed to remove sender category override:', err)
      toasts.error($_('settingsCategories.failedToRemoveOverride'))
    } finally {
      removingSender = null
    }
  }

  async function handleRunBackfill() {
    backfillRunning = true
    lastClassified = null
    try {
      const n = await RunEnrichmentBackfillNow()
      lastClassified = n ?? 0
      toasts.success($_('settingsCategories.backfillClassified', { values: { count: n ?? 0 } }))
      await loadBackfillStatus()
    } catch (err) {
      console.error('Backfill failed:', err)
      toasts.error($_('settingsCategories.backfillFailed'))
    } finally {
      backfillRunning = false
    }
  }
</script>

<div class="space-y-6">
  <!-- Default tab -->
  <div class="space-y-2">
    <Label>{$_('settingsCategories.defaultTab')}</Label>
    <p class="text-xs text-muted-foreground">
      {$_('settingsCategories.defaultTabHelp')}
    </p>
    <div class="flex items-center gap-1 rounded-md border border-border p-1 w-fit">
      {#each defaultTabOptions as opt (opt.value)}
        <button
          class="px-3 py-1.5 text-sm rounded transition-colors {inboxTabDefault === opt.value
            ? 'bg-primary text-primary-foreground'
            : 'text-muted-foreground hover:text-foreground'}"
          onclick={() => onInboxTabDefaultChange?.(opt.value)}
        >
          {opt.label}
        </button>
      {/each}
    </div>
  </div>

  <!-- Divider -->
  <div class="border-t border-border"></div>

  <!-- Sender rules -->
  <div class="space-y-2">
    <p class="text-sm font-medium">{$_('settingsCategories.overridesHeading')}</p>
    <p class="text-xs text-muted-foreground">
      {$_('settingsCategories.overridesHelp')}
    </p>
    {#if overridesLoading}
      <div class="flex items-center justify-center py-4">
        <Icon icon="mdi:loading" class="w-5 h-5 animate-spin text-muted-foreground" />
      </div>
    {:else if overrides.length === 0}
      <p class="text-sm text-muted-foreground">{$_('settingsCategories.overridesEmpty')}</p>
    {:else}
      <ul class="divide-y divide-border rounded-md border border-border">
        {#each overrides as o (o.senderEmail)}
          <li class="flex items-center justify-between gap-2 px-3 py-2">
            <div class="min-w-0">
              <p class="text-sm truncate">{o.senderEmail}</p>
              <p class="text-xs text-muted-foreground">
                {o.category === 'personal'
                  ? $_('messageList.categoryPersonal')
                  : $_('messageList.categoryAutomated')}
              </p>
            </div>
            <Button
              variant="ghost"
              size="sm"
              disabled={removingSender === o.senderEmail}
              onclick={() => handleRemoveOverride(o.senderEmail)}
              title={$_('settingsCategories.removeOverride', { values: { sender: o.senderEmail } })}
            >
              {#if removingSender === o.senderEmail}
                <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
              {:else}
                <Icon icon="mdi:close" class="w-4 h-4" />
              {/if}
            </Button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>

  <!-- Divider -->
  <div class="border-t border-border"></div>

  <!-- Backfill -->
  <div class="space-y-2">
    <p class="text-sm font-medium">{$_('settingsCategories.backfillHeading')}</p>
    {#if backfillStatus}
      <p class="text-xs text-muted-foreground">
        {backfillStatus.remaining > 0
          ? $_('settingsCategories.backfillRemaining', { values: { count: backfillStatus.remaining } })
          : $_('settingsCategories.backfillDone')}
      </p>
    {/if}
    {#if lastClassified !== null}
      <p class="text-xs text-muted-foreground">
        {$_('settingsCategories.backfillClassified', { values: { count: lastClassified } })}
      </p>
    {/if}
    <Button
      variant="outline"
      size="sm"
      disabled={backfillRunning || (backfillStatus?.remaining ?? 0) === 0}
      onclick={handleRunBackfill}
    >
      {#if backfillRunning}
        <Icon icon="mdi:loading" class="w-4 h-4 mr-2 animate-spin" />
        {$_('settingsCategories.backfillRunning')}
      {:else}
        {$_('settingsCategories.backfillRun')}
      {/if}
    </Button>
  </div>
</div>
