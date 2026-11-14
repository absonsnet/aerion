<script lang="ts">
  // MailMergeSettingsDialog — per-account Mail Merge settings.
  //
  // Shape mirrors AIWritingSettingsDialog: `open` bindable + `onClose`,
  // Dialog shell from $lib/components/ui/dialog. Loaded fresh from
  // MailMerge_GetSettings on every open.
  //
  // Settings are per account (MailMerge_GetSettings/SaveSettings take
  // an accountId), but the host settings dispatcher
  // (ExtensionSettingsDialog.svelte) knows no account — so this dialog
  // offers its own account picker, from
  // getAllAccountIdentities (main window only; the dispatcher mounts
  // in App.svelte). The API comes in as a prop, exactly as
  // AIWritingSettingsDialog receives its api.
  //
  // Defaults (backend contract): cap ON at 100, pause 2s, deliver mode
  // drafts. Validation mirrors the backend (cap limit ≥ 1, deliver
  // mode drafts|send, pause ≥ 0).

  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import type { ComposerApi } from '$lib/composerApi'
  import {
    DEFAULT_MAIL_MERGE_SETTINGS,
    type MailMergeSettings,
  } from '$extensions/mail-merge/frontend/lib/mailmerge'

  interface Props {
    open: boolean
    /** Main-window backend API from the settings dispatcher. */
    api: ComposerApi
    onClose?: () => void
  }

  let { open = $bindable(false), api, onClose }: Props = $props()

  let loading = $state(false)
  let loadError = $state('')
  let saving = $state(false)
  let status = $state<{ kind: 'success' | 'error'; message: string } | null>(null)

  let accounts = $state<Array<{ id: string; label: string }>>([])
  let accountId = $state('')
  let capEnabled = $state(DEFAULT_MAIL_MERGE_SETTINGS.capEnabled)
  let capLimit = $state(DEFAULT_MAIL_MERGE_SETTINGS.capLimit)
  let defaultPauseSeconds = $state(DEFAULT_MAIL_MERGE_SETTINGS.defaultPauseSeconds)
  let defaultRandomPause = $state(DEFAULT_MAIL_MERGE_SETTINGS.defaultRandomPause)
  let defaultDeliverMode = $state(DEFAULT_MAIL_MERGE_SETTINGS.defaultDeliverMode)
  // Signing preference (Phase 3): this dialog has no signing control —
  // the choice is made in the merge dialog — but the fields are kept
  // in state and round-tripped on save so saving settings here never
  // silently resets the per-account signing choice.
  let signMessages = $state(DEFAULT_MAIL_MERGE_SETTINGS.signMessages)
  let signMessagesSet = $state(DEFAULT_MAIL_MERGE_SETTINGS.signMessagesSet)

  $effect(() => {
    if (open) {
      void loadAccountsAndSettings()
    }
  })

  async function loadAccountsAndSettings(): Promise<void> {
    loading = true
    loadError = ''
    status = null
    try {
      const groups = (await api.getAllAccountIdentities?.()) || []
      accounts = groups
        .filter((g) => g.account)
        .map((g) => ({
          id: g.account!.id,
          label: g.account!.email || g.account!.name || g.account!.id,
        }))
      if (accounts.length > 0 && !accounts.some((a) => a.id === accountId)) {
        accountId = accounts[0].id
      }
      if (accountId) await loadSettings(accountId)
    } catch (err) {
      loadError = `Could not load settings: ${(err as Error)?.message ?? String(err)}`
    } finally {
      loading = false
    }
  }

  async function loadSettings(id: string): Promise<void> {
    try {
      const s = (await api.mailMergeGetSettings(id)) as MailMergeSettings
      capEnabled = s.capEnabled ?? DEFAULT_MAIL_MERGE_SETTINGS.capEnabled
      capLimit = s.capLimit && s.capLimit > 0 ? s.capLimit : DEFAULT_MAIL_MERGE_SETTINGS.capLimit
      defaultPauseSeconds = typeof s.defaultPauseSeconds === 'number'
        ? s.defaultPauseSeconds
        : DEFAULT_MAIL_MERGE_SETTINGS.defaultPauseSeconds
      defaultRandomPause = s.defaultRandomPause ?? false
      defaultDeliverMode = s.defaultDeliverMode === 'send' ? 'send' : 'drafts'
      signMessages = s.signMessages ?? false
      signMessagesSet = s.signMessagesSet ?? false
    } catch (err) {
      loadError = `Could not load settings: ${(err as Error)?.message ?? String(err)}`
    }
  }

  async function handleAccountChange(id: string): Promise<void> {
    if (!id || id === accountId) return
    accountId = id
    status = null
    await loadSettings(id)
  }

  function validate(): string | null {
    if (!accountId) return 'Pick an account first.'
    if (capLimit < 1) return 'Cap limit must be at least 1.'
    if (defaultPauseSeconds < 0) return 'Pause must not be negative.'
    if (defaultDeliverMode !== 'drafts' && defaultDeliverMode !== 'send') {
      return 'Deliver mode must be drafts or send.'
    }
    return null
  }

  async function handleSave(): Promise<void> {
    const error = validate()
    if (error) {
      status = { kind: 'error', message: error }
      return
    }
    saving = true
    status = null
    try {
      await api.mailMergeSaveSettings(accountId, {
        capEnabled,
        capLimit,
        defaultPauseSeconds,
        defaultRandomPause,
        defaultDeliverMode,
        signMessages,
        signMessagesSet,
      })
      status = { kind: 'success', message: 'Mail Merge settings saved.' }
    } catch (err) {
      status = {
        kind: 'error',
        message: `Could not save settings: ${(err as Error)?.message ?? String(err)}`,
      }
    } finally {
      saving = false
    }
  }

  function handleClose() {
    open = false
    onClose?.()
  }
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) handleClose() }}>
  <Dialog.Content class="max-w-2xl">
    <Dialog.Header>
      <Dialog.Title>Mail Merge settings</Dialog.Title>
      <Dialog.Description>
        Defaults for new mail merge runs, per account. The safety cap applies to Send now runs only — draft generation is never capped.
      </Dialog.Description>
    </Dialog.Header>

    {#if loading}
      <p class="text-sm text-muted-foreground py-6">Loading…</p>
    {:else if loadError && accounts.length === 0}
      <p class="text-sm text-destructive py-6">{loadError}</p>
    {:else}
      <div class="space-y-4 mt-2 max-h-[60vh] overflow-y-auto pr-1">
        <div class="space-y-1.5">
          <label for="mm-settings-account" class="text-sm font-medium text-foreground">Account</label>
          <select
            id="mm-settings-account"
            value={accountId}
            onchange={(e) => void handleAccountChange(e.currentTarget.value)}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground"
          >
            {#each accounts as a (a.id)}
              <option value={a.id}>{a.label}</option>
            {/each}
          </select>
          {#if accounts.length === 0}
            <p class="text-xs text-muted-foreground">No accounts found.</p>
          {/if}
        </div>

        <div class="space-y-1.5">
          <label for="mm-settings-deliver" class="text-sm font-medium text-foreground">Default deliver mode</label>
          <select
            id="mm-settings-deliver"
            bind:value={defaultDeliverMode}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground"
          >
            <option value="drafts">Save as drafts (recommended)</option>
            <option value="send">Send now</option>
          </select>
          <p class="text-xs text-muted-foreground">Save as drafts is the safe default — review each message in Drafts before sending.</p>
        </div>

        <div class="space-y-1.5">
          <label for="mm-settings-pause" class="text-sm font-medium text-foreground">Default pause between messages (seconds)</label>
          <Input id="mm-settings-pause" type="number" min="0" bind:value={defaultPauseSeconds} />
        </div>

        <div class="flex items-start gap-2">
          <input id="mm-settings-random" type="checkbox" bind:checked={defaultRandomPause} class="mt-0.5" />
          <div>
            <label for="mm-settings-random" class="text-sm font-medium text-foreground">Random pause</label>
            <p class="text-xs text-muted-foreground">When on, the pause is a random 0–X seconds instead of exactly X.</p>
          </div>
        </div>

        <div class="space-y-1.5 rounded-md border border-border p-3">
          <label class="flex items-center gap-2 text-sm font-medium">
            <input type="checkbox" bind:checked={capEnabled} />
            Safety cap — send at most
            <Input type="number" min="1" bind:value={capLimit} disabled={!capEnabled} class="w-24" />
            messages per run
          </label>
          <p class="text-xs text-muted-foreground">
            Rows over the cap are held as “held — over cap” until approved in the run log. The cap applies at send time, not to draft generation.
          </p>
        </div>

        {#if status}
          <p
            class="text-sm rounded-md px-3 py-2 {status.kind === 'success'
              ? 'bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-200'
              : 'bg-red-100 text-red-800 dark:bg-red-900/40 dark:text-red-200'}"
            role="status"
          >
            {status.message}
          </p>
        {/if}
      </div>
    {/if}

    <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
      <Button variant="ghost" onclick={handleClose}>Close</Button>
      <Button onclick={() => void handleSave()} disabled={loading || saving || !accountId}>
        {saving ? 'Saving…' : 'Save'}
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>
