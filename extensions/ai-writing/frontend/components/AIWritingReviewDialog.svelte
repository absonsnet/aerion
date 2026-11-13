<script lang="ts">
  // AIWritingReviewDialog — port of the Thunderbird add-on's popup.js /
  // popup.html review UI.
  //
  // States: loading / error / review (TB's showLoading/showError/showReview).
  // Review shows a word-level LCS diff tab (diffWords from lib/diff.ts) and
  // a Final tab (Improved/Original toggle, plain-text render), subject
  // suggestion chips for new mail only, and Insert / Try Again. Nothing is
  // sent anywhere after the Improve call returns: Insert only writes the
  // already-improved text back into the composer via the ComposerContext
  // (mode-aware — in plain-text mode setUserComposedHTML converts via
  // htmlToPlainText internally, so no second LLM call and no conversion
  // here). The quoted history is never part of the improved text.
  //
  // The dialog is rendered by AIWritingButton inside the composer window
  // that owns the editor, so it works in both the inline and the detached
  // composer. The ComposerContext arrives as a prop from the button (which
  // obtained it via getContext) rather than via getContext here, because
  // the dialog content is portaled in the DOM by bits-ui.

  import { _ } from 'svelte-i18n'
  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Tabs from '$lib/components/ui/tabs'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { toasts } from '$lib/stores/toast'
  import { htmlToPlainText } from '$lib/components/composer/composerUtils'
  import type { ComposerContext } from '$lib/composerContext'
  import { diffWords, type DiffToken } from '$extensions/ai-writing/frontend/lib/diff'
  import {
    isApiKeyError,
    normalizeImproveResult,
    type AIWritingImproveRequest,
    type AIWritingImproveResult,
  } from '$extensions/ai-writing/frontend/lib/aiwriting'
  // @ts-ignore - wailsjs bindings (AIWriting_* land after `make generate`)
  import { AIWriting_Improve } from '$wailsjs/go/app/App'

  interface Props {
    open: boolean
    /** Snapshot of one action run, or null when no request is active. */
    request: AIWritingImproveRequest | null
    /** Composer context from AIWritingButton (via getContext there). */
    composerContext: ComposerContext | null
    onClose?: () => void
    /** Open the settings dialog (parent closes this dialog first, mirroring
     *  TB's popup closing when it opens the options page). */
    onOpenSettings?: () => void
  }

  let { open = $bindable(false), request, composerContext, onClose, onOpenSettings }: Props = $props()

  type ViewState = 'loading' | 'error' | 'review'

  let viewState = $state<ViewState>('loading')
  let errorMessage = $state('')
  let errorIsApiKey = $state(false)
  let improvedHtml = $state('')
  let subjects = $state<string[]>([])
  let pickedSubject = $state('')
  let diffTokens = $state<DiffToken[]>([])
  let activeTab = $state<'changes' | 'final'>('changes')
  let finalView = $state<'improved' | 'original'>('improved')

  // Plain (non-reactive) guard so the auto-run effect fires once per
  // request nonce and Try Again re-runs on demand without re-triggering.
  let lastRunNonce = 0

  $effect(() => {
    if (open && request && request.nonce !== lastRunNonce) {
      lastRunNonce = request.nonce
      void runImprove()
    }
  })

  function resetForRun() {
    viewState = 'loading'
    errorMessage = ''
    errorIsApiKey = false
    improvedHtml = ''
    subjects = []
    pickedSubject = ''
    diffTokens = []
    activeTab = 'changes'
    finalView = 'improved'
  }

  async function runImprove(): Promise<void> {
    const req = request
    if (!req) return
    resetForRun()
    try {
      const raw = (await AIWriting_Improve({
        actionId: req.actionId,
        draftHtml: req.draftHtml,
        quotedHtml: req.quotedHtml,
        isNewEmail: req.isNewEmail,
      })) as AIWritingImproveResult
      const { improvedHtml: improved, subjects: parsed } = normalizeImproveResult(raw ?? { improvedHtml: '', subjects: [] })
      improvedHtml = improved
      // Subject suggestions are new-mail only (IsNewEmail rule: no quoted part).
      subjects = req.isNewEmail ? parsed : []
      diffTokens = diffWords(htmlToPlainText(req.draftHtml), htmlToPlainText(improvedHtml))
      viewState = 'review'
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err) ?? $_('aiwriting.review.errorFallback')
      errorIsApiKey = isApiKeyError(msg)
      errorMessage = errorIsApiKey ? $_('aiwriting.review.errorApiKey') : msg
      viewState = 'error'
    }
  }

  function handleInsert() {
    if (!composerContext || !improvedHtml || !request) return
    try {
      composerContext.setUserComposedHTML(improvedHtml)
      // Empty input = keep the original subject (TB behavior).
      if (request.isNewEmail && pickedSubject.trim() !== '') {
        composerContext.setSubject(pickedSubject.trim())
      }
      toasts.success($_('aiwriting.review.inserted'))
      handleClose()
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err)
      errorIsApiKey = false
      errorMessage = $_('aiwriting.review.insertFailed', { values: { message: msg } })
      viewState = 'error'
    }
  }

  function handleClose() {
    open = false
    onClose?.()
  }
</script>

{#snippet diffText(text: string)}
  {#each text.split('\n') as part, i}
    {#if i > 0}<br />{/if}{part}
  {/each}
{/snippet}

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) handleClose() }}>
  <Dialog.Content class="max-w-3xl">
    <Dialog.Header>
      <Dialog.Title>
        {request?.actionLabel
          ? $_('aiwriting.review.titleWithAction', { values: { action: request.actionLabel } })
          : $_('aiwriting.review.title')}
      </Dialog.Title>
      <Dialog.Description>
        {$_('aiwriting.review.description')}
      </Dialog.Description>
    </Dialog.Header>

    {#if viewState === 'loading'}
      <div class="flex flex-col items-center justify-center py-16 gap-3">
        <Icon icon="mdi:loading" class="w-10 h-10 animate-spin text-primary" />
        <p class="text-sm font-medium text-foreground">{$_('aiwriting.review.loading')}</p>
        <p class="text-xs text-muted-foreground">{$_('aiwriting.review.loadingHint')}</p>
      </div>
    {:else if viewState === 'error'}
      <div class="flex flex-col items-center justify-center py-12 gap-4 text-center">
        <Icon icon="mdi:alert-circle-outline" class="w-10 h-10 text-destructive" />
        <p class="text-sm text-foreground max-w-md">{errorMessage}</p>
        <div class="flex items-center gap-2">
          <Button onclick={() => void runImprove()}>
            <Icon icon="mdi:refresh" class="w-4 h-4 mr-1" />
            {$_('aiwriting.review.retry')}
          </Button>
          {#if errorIsApiKey}
            <Button variant="outline" onclick={() => onOpenSettings?.()}>
              {$_('aiwriting.review.openSettings')}
            </Button>
          {/if}
        </div>
      </div>
    {:else if request}
      <div class="space-y-4 mt-2">
        {#if request.isPlainText}
          <p class="text-xs text-muted-foreground rounded-md bg-muted/50 px-3 py-2">
            {$_('aiwriting.review.plainTextNote')}
          </p>
        {/if}

        {#if request.isNewEmail && subjects.length > 0}
          <section>
            <label for="aiwriting-subject-input" class="block text-sm font-medium text-foreground mb-1">
              {$_('aiwriting.review.subjectLabel')}
            </label>
            <Input
              id="aiwriting-subject-input"
              type="text"
              bind:value={pickedSubject}
              placeholder={$_('aiwriting.review.subjectPlaceholder')}
            />
            <div class="flex flex-wrap gap-2 mt-2">
              {#each subjects as subject (subject)}
                <button
                  type="button"
                  class="max-w-full truncate px-2.5 py-1 text-xs rounded-full border border-border bg-muted/50 hover:bg-muted transition-colors text-foreground"
                  title={subject}
                  onclick={() => { pickedSubject = subject }}
                >
                  {subject}
                </button>
              {/each}
            </div>
            {#if request.originalSubject}
              <p class="text-xs text-muted-foreground mt-2">
                {$_('aiwriting.review.currentSubject', { values: { subject: request.originalSubject } })}
              </p>
            {:else}
              <p class="text-xs text-muted-foreground mt-2">
                {$_('aiwriting.review.noSubject')}
              </p>
            {/if}
          </section>
        {/if}

        <Tabs.Root bind:value={activeTab} class="flex flex-col">
          <Tabs.List class="grid w-full grid-cols-2">
            <Tabs.Trigger value="changes">{$_('aiwriting.review.tabChanges')}</Tabs.Trigger>
            <Tabs.Trigger value="final">{$_('aiwriting.review.tabFinal')}</Tabs.Trigger>
          </Tabs.List>

          <Tabs.Content value="changes" class="mt-3">
            <div class="flex items-center gap-4 text-xs text-muted-foreground mb-2">
              <span class="flex items-center gap-1.5">
                <span class="inline-block w-3 h-3 rounded-sm bg-red-200 border border-red-300 dark:bg-red-900/50 dark:border-red-800"></span>
                {$_('aiwriting.review.legendRemoved')}
              </span>
              <span class="flex items-center gap-1.5">
                <span class="inline-block w-3 h-3 rounded-sm bg-green-200 border border-green-300 dark:bg-green-900/50 dark:border-green-800"></span>
                {$_('aiwriting.review.legendAdded')}
              </span>
            </div>
            <div class="max-h-[50vh] overflow-y-auto rounded-md border border-border p-3 whitespace-pre-wrap break-words text-sm leading-relaxed text-foreground">
              {#each diffTokens as token, i (i)}
                {#if token.type === 'delete'}
                  <span class="bg-red-100 text-red-900 border-b-2 border-red-300 dark:bg-red-900/40 dark:text-red-200 dark:border-red-800">{@render diffText(token.value)}</span>
                {:else if token.type === 'insert'}
                  <span class="bg-green-100 text-green-900 border-b-2 border-green-300 dark:bg-green-900/40 dark:text-green-200 dark:border-green-800">{@render diffText(token.value)}</span>
                {:else}
                  {@render diffText(token.value)}
                {/if}
              {/each}
            </div>
          </Tabs.Content>

          <Tabs.Content value="final" class="mt-3">
            <div class="inline-flex rounded-md border border-border p-0.5 mb-2" role="group">
              <button
                type="button"
                class="px-3 py-1 text-xs rounded-sm transition-colors {finalView === 'improved' ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground'}"
                onclick={() => { finalView = 'improved' }}
              >
                {$_('aiwriting.review.improved')}
              </button>
              <button
                type="button"
                class="px-3 py-1 text-xs rounded-sm transition-colors {finalView === 'original' ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground'}"
                onclick={() => { finalView = 'original' }}
              >
                {$_('aiwriting.review.original')}
              </button>
            </div>
            <div class="max-h-[50vh] overflow-y-auto rounded-md border border-border p-3 whitespace-pre-wrap break-words text-sm leading-relaxed text-foreground">
              {htmlToPlainText(finalView === 'improved' ? improvedHtml : request.draftHtml)}
            </div>
          </Tabs.Content>
        </Tabs.Root>
      </div>

      <div class="flex items-center justify-end gap-2 pt-4 border-t border-border mt-4">
        <Button variant="secondary" onclick={() => void runImprove()}>
          <Icon icon="mdi:refresh" class="w-4 h-4 mr-1" />
          {$_('aiwriting.review.tryAgain')}
        </Button>
        <Button onclick={handleInsert}>
          <Icon icon="mdi:check" class="w-4 h-4 mr-1" />
          {$_('aiwriting.review.insert')}
        </Button>
      </div>
    {/if}
  </Dialog.Content>
</Dialog.Root>
