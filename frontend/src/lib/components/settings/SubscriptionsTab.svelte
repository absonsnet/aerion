<script lang="ts">
  import { onMount } from 'svelte'
  import Icon from '@iconify/svelte'
  import { Button } from '$lib/components/ui/button'
  // @ts-ignore - wailsjs path
  import { GetSubscriptions, UnsubscribeFromSubscription, SetSenderAutoDelete, OpenURL } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import type { app as appModels } from '../../../../wailsjs/go/models'
  import { toasts } from '$lib/stores/toast'
  import { _ } from '$lib/i18n'

  let senders = $state<appModels.SubscriptionSender[]>([])
  let loading = $state(true)
  let busySender = $state<string | null>(null)

  onMount(() => {
    loadSubscriptions()
  })

  async function loadSubscriptions() {
    loading = true
    try {
      senders = (await GetSubscriptions()) ?? []
    } catch (err) {
      console.error('Failed to load subscriptions:', err)
      toasts.error($_('settingsSubscriptions.failedToLoad'))
    } finally {
      loading = false
    }
  }

  async function handleUnsubscribe(sender: appModels.SubscriptionSender) {
    busySender = sender.senderEmail
    try {
      const result = await UnsubscribeFromSubscription(sender.senderEmail, sender.accountId)
      const senderName = result.senderName || result.senderEmail
      if (result.method === 'already') {
        toasts.info($_('viewer.alreadyUnsubscribed', { values: { sender: senderName } }))
      } else if (result.method === 'browser') {
        toasts.info($_('viewer.unsubscribeOpenedInBrowser', { values: { domain: result.domain } }))
        if (result.url) await OpenURL(result.url)
      } else if (result.method === 'mailto') {
        toasts.success($_('viewer.unsubscribeEmailSent', { values: { sender: senderName } }))
      } else {
        const mismatchNote = result.domainMismatch
          ? ' ' + $_('viewer.unsubscribeDomainMismatch', { values: { domain: result.domain } })
          : ''
        if (result.domainMismatch) {
          toasts.warning($_('viewer.unsubscribedFrom', { values: { sender: senderName } }) + mismatchNote)
        } else {
          toasts.success($_('viewer.unsubscribedFrom', { values: { sender: senderName } }))
        }
      }
      await loadSubscriptions()
    } catch (err) {
      console.error('Unsubscribe failed:', err)
      toasts.error(`${$_('viewer.unsubscribeFailed')}: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      busySender = null
    }
  }

  async function handleAutoDeleteChange(sender: appModels.SubscriptionSender, enabled: boolean) {
    try {
      await SetSenderAutoDelete(sender.senderEmail, sender.accountId, enabled)
      senders = senders.map(s =>
        s.senderEmail === sender.senderEmail && s.accountId === sender.accountId
          ? { ...s, autoDelete: enabled }
          : s
      )
      toasts.success(
        enabled
          ? $_('viewer.unsubscribeAutoDeleteOn', { values: { sender: sender.senderName || sender.senderEmail } })
          : $_('settingsSubscriptions.autoDeleteOff', { values: { sender: sender.senderName || sender.senderEmail } })
      )
    } catch (err) {
      console.error('Failed to set auto-delete:', err)
      toasts.error($_('settingsSubscriptions.failedToSaveRule'))
    }
  }
</script>

<div class="space-y-4">
  <h3 class="text-sm font-medium flex items-center gap-2">
    <Icon icon="mdi:email-remove-outline" class="w-4 h-4" />
    {$_('settingsSubscriptions.title')}
  </h3>
  <p class="text-sm text-muted-foreground">
    {$_('settingsSubscriptions.description')}
  </p>

  {#if loading}
    <div class="flex items-center justify-center py-8">
      <Icon icon="mdi:loading" class="w-6 h-6 animate-spin text-muted-foreground" />
    </div>
  {:else if senders.length === 0}
    <div class="text-sm text-muted-foreground py-8 text-center">
      <p>{$_('settingsSubscriptions.empty')}</p>
    </div>
  {:else}
    <div class="space-y-2">
      {#each senders as sender (sender.accountId + ':' + sender.senderEmail)}
        <div class="flex items-center gap-3 rounded-md border border-border p-3">
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="text-sm font-medium truncate">{sender.senderName || sender.senderEmail}</span>
              {#if sender.oneClick}
                <span class="text-xs px-1.5 py-0.5 rounded-full bg-primary/10 text-primary" title={$_('settingsSubscriptions.oneClickAvailableTitle')}>
                  {$_('settingsSubscriptions.oneClick')}
                </span>
              {/if}
              {#if sender.domainMismatch}
                <span
                  class="text-xs px-1.5 py-0.5 rounded-full bg-amber-500/10 text-amber-600 dark:text-amber-400"
                  title={$_('settingsSubscriptions.domainMismatchTitle')}
                >
                  {$_('settingsSubscriptions.domainMismatch')}
                </span>
              {/if}
            </div>
            <div class="text-xs text-muted-foreground truncate">{sender.senderEmail}</div>
            <div class="text-xs text-muted-foreground mt-0.5">
              {$_('settingsSubscriptions.received', { values: { recent: sender.count30Days, total: sender.countAll } })}
              {#if sender.lastReceived}
                · {$_('settingsSubscriptions.lastReceived', { values: { date: sender.lastReceived } })}
              {/if}
            </div>
            <label class="mt-1.5 flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
              <input
                type="checkbox"
                checked={sender.autoDelete}
                onchange={(e) => handleAutoDeleteChange(sender, (e.target as HTMLInputElement).checked)}
              />
              {$_('settingsSubscriptions.autoDelete')}
            </label>
          </div>
          {#if sender.unsubscribed}
            <Button variant="outline" size="sm" disabled>
              {$_('viewer.unsubscribed')}
            </Button>
          {:else}
            <Button
              variant="outline"
              size="sm"
              disabled={busySender === sender.senderEmail}
              onclick={() => handleUnsubscribe(sender)}
            >
              {#if busySender === sender.senderEmail}
                <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
              {/if}
              {$_('viewer.unsubscribe')}
            </Button>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>
