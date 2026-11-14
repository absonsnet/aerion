<script lang="ts">
  // MailMergeButton — the mail-merge extension's composer toolbar button.
  // Rendered by Composer.svelte into EditorToolbar's `composerActions`
  // snippet (next to the AI Writing button), via the static dispatch in
  // the snippet: extensionId 'mail-merge' → this component. The registry
  // item comes from the backend (extensions/mail-merge/backend/
  // register.go), so the button only renders when the extension is
  // enabled; it works in both the inline composer and the detached
  // composer window because both share Composer.svelte.
  //
  // Click opens MailMergeDialog. The dialog is portaled, so getContext
  // does NOT resolve inside it — the window-aware ComposerApi and the
  // ComposerContext are passed in as props, exactly like
  // AIWritingButton does for its review/settings dialogs.

  import { getContext } from 'svelte'
  import Icon from '@iconify/svelte'
  import { COMPOSER_CONTEXT_KEY, type ComposerContext } from '$lib/composerContext'
  import { COMPOSER_API_KEY, type ComposerApi } from '$lib/composerApi'
  import MailMergeDialog from './MailMergeDialog.svelte'

  interface Props {
    /** Active account id (from Composer.svelte's activeAccountId). */
    accountId: string
  }

  let { accountId }: Props = $props()

  const composerContext = getContext<ComposerContext | undefined>(COMPOSER_CONTEXT_KEY) ?? null
  const api = getContext<ComposerApi>(COMPOSER_API_KEY)

  let dialogOpen = $state(false)
</script>

<button
  type="button"
  class="p-1 rounded hover:bg-muted transition-colors flex items-center gap-1 text-xs"
  title="Mail Merge — send one personalised message per recipient from this draft"
  onclick={() => { dialogOpen = true }}
>
  <Icon icon="mdi:email-multiple" class="w-4 h-4" />
  <span class="hidden lg:inline">Mail Merge</span>
</button>

<MailMergeDialog
  bind:open={dialogOpen}
  {api}
  {composerContext}
  {accountId}
  onClose={() => { dialogOpen = false }}
/>
