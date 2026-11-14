<script lang="ts">
  // AIWritingButton — the ai-writing extension's composer toolbar button.
  // Rendered by Composer.svelte into EditorToolbar's `composerActions`
  // snippet (next to the plain-text toggle). A real <button> (via the
  // dropdown-menu Trigger) so Alt+T hint mode picks it up. The slot keeps
  // it enabled in plain-text mode (the toolbar never disables slotted
  // actions there); the button disables itself only when the draft is
  // empty and there is nothing to reply to (see refreshTriggerState).
  //
  // Click flow (Thunderbird compose_action equivalent):
  //   1. The trigger opens an action-picker menu. The list comes from
  //      AIWriting_ListActions, fetched lazily the first time the menu
  //      opens — action ids/labels are backend data, never hardcoded
  //      here, so the menu shows whatever the registry ships. A failed
  //      fetch toasts and closes the menu; the next open retries.
  //   2. The menu is context-aware: its contents are computed at
  //      menu-open time from the live ComposerContext (never cached —
  //      the draft changes as the user types). Empty draft + quoted
  //      context → only "Generate Reply"; non-empty draft → the six
  //      revision actions in registry order (Generate Reply is for
  //      empty drafts only). Empty draft + no context → the trigger
  //      itself is disabled, with a tooltip explaining why.
  //   3. Choosing an action runs the existing flow for that action:
  //      read the draft via the ComposerContext (getContext, per the
  //      Phase 2 hook). Empty draft + a revision action → "Draft is
  //      empty. Write something first." toast (TB exact wording);
  //      Generate Reply with no quoted context → a "nothing to reply
  //      to" toast. The guards are a backstop for the menu filter —
  //      the menu normally never offers an action that would fail them.
  //   4. No API key (AIWriting_GetStatus) → open the settings dialog
  //      instead of calling Improve (TB equivalent: install opens options).
  //   5. Otherwise snapshot draft + quoted context and open the review
  //      dialog, which runs AIWriting_Improve with the chosen action.
  //
  // The settings dialog is rendered HERE (owned by this button) rather
  // than via the host's ExtensionSettingsDialog dispatcher, because that
  // dispatcher is mounted only in App.svelte (main window) — the detached
  // composer window (ComposerApp.svelte) has no dispatcher, so routing the
  // review dialog's Open Settings through openExtensionSettings() would
  // silently do nothing there. The normal Settings → Extensions → Edit
  // path still goes through the dispatcher; both render the same
  // AIWritingSettingsDialog component.

  import { getContext } from 'svelte'
  import Icon from '@iconify/svelte'
  import { _ } from 'svelte-i18n'
  import { toasts } from '$lib/stores/toast'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import { COMPOSER_CONTEXT_KEY, type ComposerContext } from '$lib/composerContext'
  import { htmlToPlainText } from '$lib/components/composer/composerUtils'
  import AIWritingReviewDialog from './AIWritingReviewDialog.svelte'
  import AIWritingSettingsDialog from './AIWritingSettingsDialog.svelte'
  import {
    GENERATE_REPLY_ACTION_ID,
    type AIWritingAction,
    type AIWritingImproveRequest,
  } from '$extensions/ai-writing/frontend/lib/aiwriting'
  // The backend calls go through the resolved composer API (Composer.svelte
  // sets COMPOSER_API_KEY), not a direct App.js import: the detached
  // composer window is a separate process whose bindings live on
  // ComposerApp, so App.js calls would throw there and the button would
  // silently never populate.
  import { COMPOSER_API_KEY, type ComposerApi } from '$lib/composerApi'

  // The button lives inside Composer.svelte's snippet, which Svelte renders
  // in the defining component's context — getContext resolves to the
  // ComposerContext set by Composer.svelte (verified against Svelte 5.53.9).
  const composerContext = getContext<ComposerContext | undefined>(COMPOSER_CONTEXT_KEY) ?? null
  // Same context mechanism for the window-aware backend API (main window:
  // App bindings; detached composer window: ComposerApp bindings).
  const api = getContext<ComposerApi>(COMPOSER_API_KEY)

  let reviewOpen = $state(false)
  let settingsOpen = $state(false)
  let menuOpen = $state(false)
  let actions = $state<AIWritingAction[] | null>(null)
  let actionsLoading = $state(false)
  let request = $state<AIWritingImproveRequest | null>(null)
  let nonce = 0

  // Menu snapshot, taken fresh at every menu open (see
  // handleMenuOpenChange) — never carried across opens, so the menu
  // always reflects the draft as it is when the user opens it.
  let menuDraftEmpty = $state(false)
  // Trigger disabled state (empty draft + no quoted context). The
  // composer context exposes no subscription, so this is refreshed at
  // every point it can change: on mount (+ a deferred re-check, since
  // the button can mount before Composer.svelte's onMount creates the
  // editor and quotes the original), on every input in the composer
  // window, on hover/focus of the trigger, and when the menu or the
  // review dialog closes (an Insert replaces the draft).
  let triggerDisabled = $state(false)

  /**
   * Live composer state, read fresh from the ComposerContext each time:
   * `draftEmpty` is true when the user-composed part has no plain text
   * (htmlToPlainText is the same helper the composer code uses);
   * `hasContext` is true when quoted/forwarded content exists. With no
   * composer context this fails open (menu shows everything, runAction
   * surfaces the error) — the button only renders inside the composer.
   */
  function readComposerState(): { draftEmpty: boolean; hasContext: boolean } {
    if (!composerContext) return { draftEmpty: false, hasContext: false }
    return {
      draftEmpty: htmlToPlainText(composerContext.getUserComposedHTML()).trim() === '',
      hasContext: composerContext.getQuotedHTML() !== '',
    }
  }

  function refreshTriggerState(): void {
    const { draftEmpty, hasContext } = readComposerState()
    triggerDisabled = draftEmpty && !hasContext
  }

  $effect(() => {
    refreshTriggerState()
    // Deferred re-check for the mount race described above.
    const timer = window.setTimeout(refreshTriggerState, 500)
    const onInput = () => refreshTriggerState()
    document.addEventListener('input', onInput)
    return () => {
      window.clearTimeout(timer)
      document.removeEventListener('input', onInput)
    }
  })

  /**
   * The actions the menu shows, from the open-time snapshot above:
   * empty draft → only Generate Reply (it writes a reply from the
   * quoted context); non-empty draft → the six revision actions in
   * registry order, Generate Reply excluded (it is for empty drafts
   * only). The registry list itself is backend data — only the
   * generate-reply id is named (see GENERATE_REPLY_ACTION_ID).
   */
  let visibleActions = $derived.by(() => {
    const list = actions ?? []
    if (menuDraftEmpty) {
      return list.filter((a) => a.id === GENERATE_REPLY_ACTION_ID)
    }
    return list.filter((a) => a.id !== GENERATE_REPLY_ACTION_ID)
  })

  function openSettingsFromReview() {
    // Mirror TB: opening settings closes the review popup.
    reviewOpen = false
    settingsOpen = true
  }

  function handleReviewClose() {
    reviewOpen = false
    // Insert replaced the draft (e.g. a generated reply), so re-read
    // the trigger state.
    refreshTriggerState()
  }

  /**
   * Display label for a registry action. The backend owns the canonical
   * English label; localize it when this extension ships a translation
   * for the action id (aiwriting.actions.<id>), else use the backend
   * label verbatim. svelte-i18n formats a missing key back to the key
   * itself, which is the "no translation" signal. Action ids arrive
   * from the backend — none are hardcoded here.
   */
  function actionLabel(action: AIWritingAction): string {
    const key = `aiwriting.actions.${action.id}`
    const translated = $_(key)
    return translated === key ? action.label : translated
  }

  /**
   * Lazily fetch the action list the first time the menu opens. A failed
   * fetch toasts (same raw-error style as the GetStatus failure below)
   * and closes the menu without crashing the composer; the next open
   * retries because `actions` stays null.
   */
  async function ensureActions(): Promise<void> {
    if (actions || actionsLoading) return
    actionsLoading = true
    try {
      const list = (await api.aiWritingListActions()) as AIWritingAction[] | null
      actions = Array.isArray(list) ? list : []
    } catch (err) {
      console.error('aiWritingListActions failed:', err)
      toasts.error((err as Error)?.message ?? String(err))
      menuOpen = false
    } finally {
      actionsLoading = false
    }
  }

  function handleMenuOpenChange(open: boolean) {
    menuOpen = open
    if (open) {
      // Snapshot the live composer state now — the menu contents are
      // computed from this, never from a stale earlier open. The same
      // read sets the trigger state (empty draft + no context → disabled).
      const { draftEmpty, hasContext } = readComposerState()
      menuDraftEmpty = draftEmpty
      triggerDisabled = draftEmpty && !hasContext
      void ensureActions()
    } else {
      refreshTriggerState()
    }
  }

  async function chooseAction(action: AIWritingAction) {
    // The menu closes itself on select; run the existing flow for the
    // chosen action.
    await runAction(action)
  }

  async function runAction(action: AIWritingAction) {
    if (!composerContext) {
      toasts.error($_('aiwriting.button.noComposerContext'))
      return
    }
    const draftHtml = composerContext.getUserComposedHTML()
    const quotedHtml = composerContext.getQuotedHTML()
    const draftEmpty = htmlToPlainText(draftHtml).trim() === ''
    // Guards matching the menu filter (backstop: the menu normally never
    // offers an action that would fail these).
    if (draftEmpty && action.id !== GENERATE_REPLY_ACTION_ID) {
      toasts.error($_('aiwriting.button.emptyDraft'))
      return
    }
    if (action.id === GENERATE_REPLY_ACTION_ID && quotedHtml === '') {
      toasts.error($_('aiwriting.button.noContext'))
      return
    }
    try {
      const status = (await api.aiWritingGetStatus()) as { hasApiKey?: boolean } | null
      if (!status?.hasApiKey) {
        settingsOpen = true
        return
      }
    } catch (err) {
      console.error('aiWritingGetStatus failed:', err)
      toasts.error((err as Error)?.message ?? String(err))
      return
    }
    nonce += 1
    request = {
      nonce,
      actionId: action.id,
      actionLabel: actionLabel(action),
      draftHtml,
      quotedHtml,
      // Contract IsNewEmail rule: no quoted part = new mail.
      isNewEmail: quotedHtml === '',
      originalSubject: composerContext.getSubject(),
      isPlainText: composerContext.isPlainText(),
    }
    reviewOpen = true
  }
</script>

<DropdownMenu.Root bind:open={menuOpen} onOpenChange={handleMenuOpenChange}>
  <!-- Wrapper carries the tooltip (a disabled button shows no native
       tooltip) and refreshes the trigger state on hover/focus, so the
       disabled state is current at the moment the user reaches for it.
       Pointer events reach the wrapper even when the button is disabled
       (the button also gets disabled:pointer-events-none below). -->
  <span
    class="inline-flex"
    role="presentation"
    title={triggerDisabled ? $_('aiwriting.button.disabledTooltip') : $_('aiwriting.button.tooltip')}
    onpointerenter={refreshTriggerState}
    onfocusin={refreshTriggerState}
  >
    <DropdownMenu.Trigger
      class="p-1.5 rounded hover:bg-muted transition-colors flex items-center gap-1.5 text-xs disabled:opacity-50 disabled:pointer-events-none"
      disabled={triggerDisabled}
    >
      <Icon icon="mdi:pencil-outline" class="w-5 h-5" />
      <span class="hidden lg:inline">{$_('aiwriting.button.label')}</span>
      <Icon icon="mdi:chevron-down" class="w-4 h-4" />
    </DropdownMenu.Trigger>
  </span>
  <DropdownMenu.Content side="bottom" align="start" sideOffset={4}>
    {#if actionsLoading}
      <DropdownMenu.Item disabled>
        {$_('common.loading')}
      </DropdownMenu.Item>
    {:else if visibleActions.length === 0}
      <DropdownMenu.Item disabled>
        {$_('aiwriting.button.noActions')}
      </DropdownMenu.Item>
    {:else}
      {#each visibleActions as action (action.id)}
        <DropdownMenu.Item onSelect={() => void chooseAction(action)}>
          {actionLabel(action)}
        </DropdownMenu.Item>
      {/each}
    {/if}
  </DropdownMenu.Content>
</DropdownMenu.Root>

<AIWritingReviewDialog
  bind:open={reviewOpen}
  {request}
  {composerContext}
  {api}
  onClose={handleReviewClose}
  onOpenSettings={openSettingsFromReview}
/>

<AIWritingSettingsDialog
  bind:open={settingsOpen}
  {api}
  onClose={() => { settingsOpen = false }}
/>
