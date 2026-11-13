<script lang="ts">
  // AIWritingSettingsDialog — port of the Thunderbird add-on's options.html /
  // options.js settings form.
  //
  // Shape mirrors ContactsSettingsDialog / CalendarSettingsDialog: `open`
  // bindable + `onClose`, Dialog shell from $lib/components/ui/dialog.
  // Loaded fresh from AIWriting_GetSettings on every open. The API key is
  // never read back (GetSettings returns only `hasApiKey`); the key field
  // starts empty and saving with an empty key keeps the existing one.
  //
  // No promptHtml2Text field: plain-text conversion is local now
  // (htmlToPlainText via the ComposerContext), so the TB Html2Text prompt
  // has no Aerion counterpart. Validation mirrors the backend contract
  // (temperature 0–2, maxTokens 1–4000, custom needs endpoint + model,
  // non-custom needs a key unless one is already saved).
  //
  // Prompts are per action: the dialog loads AIWriting_GetActionPrompts
  // (id + label + built-in default + stored custom prompt) and shows a
  // select to pick the action whose prompt the textbox edits. The
  // textbox starts at the action's custom prompt, else its default.
  // Saving sends only the actions whose text differs from their
  // default; an action left at (or reset to) its default stores no
  // override, so future default updates still reach it. Clearing the
  // textbox also resets the action to its default.

  import { _ } from 'svelte-i18n'
  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import {
    CUSTOM_MODEL_VALUE,
    DEFAULT_CUSTOM_API_ENDPOINT,
    DEFAULT_MAX_TOKENS,
    DEFAULT_MODEL,
    DEFAULT_TEMPERATURE,
    MODEL_OPTIONS,
    type AIWritingActionPrompt,
    type AIWritingSaveSettingsInput,
    type AIWritingSettings,
  } from '$extensions/ai-writing/frontend/lib/aiwriting'
  // @ts-ignore - wailsjs bindings (AIWriting_* land after `make generate`)
  import { AIWriting_GetActionPrompts, AIWriting_GetSettings, AIWriting_SaveSettings } from '$wailsjs/go/app/App'

  interface Props {
    open: boolean
    onClose?: () => void
  }

  let { open = $bindable(false), onClose }: Props = $props()

  let loading = $state(false)
  let loadError = $state('')
  let saving = $state(false)
  let status = $state<{ kind: 'success' | 'error'; message: string } | null>(null)

  let selectedModel = $state(DEFAULT_MODEL)
  let apiKey = $state('')
  let showApiKey = $state(false)
  let hasApiKey = $state(false)
  let customApiEndpoint = $state(DEFAULT_CUSTOM_API_ENDPOINT)
  let customModel = $state('')
  let temperature = $state(DEFAULT_TEMPERATURE)
  let maxTokens = $state(DEFAULT_MAX_TOKENS)
  let useConversationHistory = $state(true)

  // Per-action prompts (from AIWriting_GetActionPrompts). `draftPrompts`
  // holds the textbox text per action id so switching actions never
  // loses an unsaved edit; `promptDraft` is the textbox's live content
  // for the selected action.
  let actionPrompts = $state<AIWritingActionPrompt[]>([])
  let selectedActionId = $state('improve')
  let promptDraft = $state('')
  let draftPrompts = $state<Record<string, string>>({})

  const isCustom = $derived(selectedModel === CUSTOM_MODEL_VALUE)
  const selectedAction = $derived(actionPrompts.find((a) => a.id === selectedActionId))
  const promptCustomized = $derived(
    !!selectedAction && promptDraft.trim() !== selectedAction.defaultPrompt.trim(),
  )

  $effect(() => {
    if (open) {
      void loadSettings()
    }
  })

  async function loadSettings(): Promise<void> {
    loading = true
    loadError = ''
    status = null
    apiKey = ''
    showApiKey = false
    try {
      const s = (await AIWriting_GetSettings()) as AIWritingSettings
      selectedModel = s.selectedModel || DEFAULT_MODEL
      temperature = typeof s.temperature === 'number' ? s.temperature : DEFAULT_TEMPERATURE
      maxTokens = s.maxTokens ? s.maxTokens : DEFAULT_MAX_TOKENS
      customApiEndpoint = s.customApiEndpoint || DEFAULT_CUSTOM_API_ENDPOINT
      customModel = s.customModel || ''
      useConversationHistory = s.useConversationHistory ?? true
      hasApiKey = s.hasApiKey ?? false
      const rows = (await AIWriting_GetActionPrompts()) as AIWritingActionPrompt[] | null
      actionPrompts = Array.isArray(rows) ? rows : []
      // Textbox text per action: the stored custom prompt, else the
      // built-in default. Prefer the Improve action on open.
      draftPrompts = {}
      for (const a of actionPrompts) {
        draftPrompts[a.id] = a.customPrompt || a.defaultPrompt
      }
      if (actionPrompts.some((a) => a.id === 'improve')) {
        selectedActionId = 'improve'
      } else if (actionPrompts.length > 0) {
        selectedActionId = actionPrompts[0].id
      }
      promptDraft = draftPrompts[selectedActionId] ?? ''
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err)
      loadError = $_('aiwriting.settings.loadFailed', { values: { message: msg } })
    } finally {
      loading = false
    }
  }

  function modelLabel(value: string): string {
    return MODEL_OPTIONS.find((o) => o.value === value)?.label ?? value
  }

  /**
   * Display label for a registry action. Same rule as AIWritingButton:
   * localize when the extension ships a translation for the action id
   * (aiwriting.actions.<id>), else use the backend label verbatim.
   */
  function actionLabel(action: AIWritingActionPrompt): string {
    const key = `aiwriting.actions.${action.id}`
    const translated = $_(key)
    return translated === key ? action.label : translated
  }

  /** Switch the prompt editor to another action, keeping the current
   *  action's unsaved textbox text in draftPrompts. */
  function selectAction(id: string): void {
    if (!id || id === selectedActionId) return
    draftPrompts[selectedActionId] = promptDraft
    selectedActionId = id
    promptDraft = draftPrompts[id] ?? ''
  }

  /** Restore the selected action's textbox to its built-in default.
   *  Saving afterwards stores no override for it (see handleSave). */
  function resetPrompt(): void {
    if (!selectedAction) return
    promptDraft = selectedAction.defaultPrompt
    draftPrompts[selectedActionId] = promptDraft
  }

  /** Client-side mirror of the backend validation (options.js shape). */
  function validate(): string | null {
    if (!selectedModel) return $_('aiwriting.settings.errorModelRequired')
    if (isCustom) {
      if (!customModel.trim()) return $_('aiwriting.settings.errorCustomModel')
      if (!customApiEndpoint.trim()) return $_('aiwriting.settings.errorCustomEndpoint')
    } else if (!hasApiKey && !apiKey.trim()) {
      return $_('aiwriting.settings.errorApiKeyRequired')
    }
    if (maxTokens < 1 || maxTokens > 4000) return $_('aiwriting.settings.errorMaxTokens')
    if (temperature < 0 || temperature > 2) return $_('aiwriting.settings.errorTemperature')
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
      // Stash the visible textbox before building the override map.
      draftPrompts[selectedActionId] = promptDraft
      // Only actions whose text differs from their built-in default
      // are stored; the rest (including a cleared textbox) reset to
      // the default because the backend drops empty/default-equal
      // entries.
      const overrides: Record<string, string> = {}
      for (const a of actionPrompts) {
        const text = (draftPrompts[a.id] ?? '').trim()
        if (text !== '' && text !== a.defaultPrompt.trim()) {
          overrides[a.id] = text
        }
      }
      const input: AIWritingSaveSettingsInput = {
        selectedModel,
        // Empty string = keep the existing stored key (backend contract).
        apiKey: apiKey.trim(),
        temperature,
        maxTokens,
        customApiEndpoint: customApiEndpoint.trim(),
        customModel: customModel.trim(),
        // Legacy single-prompt field (old backends only); the new
        // backend prefers actionPrompts.
        promptImprove: (draftPrompts['improve'] ?? '').trim(),
        actionPrompts: overrides,
        useConversationHistory,
      }
      await AIWriting_SaveSettings(input)
      if (apiKey.trim() !== '') hasApiKey = true
      apiKey = ''
      status = { kind: 'success', message: $_('aiwriting.settings.saved') }
      // Reflect the save in the per-action state without a full reload
      // (which would flash the spinner and clear this status): the
      // stored custom prompt for each action is now its override, or
      // "" when it uses its default.
      for (const a of actionPrompts) {
        a.customPrompt = overrides[a.id] ?? ''
      }
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err)
      status = { kind: 'error', message: $_('aiwriting.settings.saveFailed', { values: { message: msg } }) }
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
      <Dialog.Title>{$_('aiwriting.settings.title')}</Dialog.Title>
      <Dialog.Description>
        {$_('aiwriting.settings.description')}
      </Dialog.Description>
    </Dialog.Header>

    {#if loading}
      <div class="flex items-center justify-center py-12 gap-2">
        <Icon icon="mdi:loading" class="w-5 h-5 animate-spin text-primary" />
        <span class="text-sm text-muted-foreground">{$_('aiwriting.settings.loading')}</span>
      </div>
    {:else if loadError}
      <p class="text-sm text-destructive py-6">{loadError}</p>
    {:else}
      <div class="space-y-4 mt-2 max-h-[60vh] overflow-y-auto pr-1">
        <div class="space-y-1.5">
          <span class="text-sm font-medium text-foreground">
            {$_('aiwriting.settings.model')}
          </span>
          <Select.Root
            value={selectedModel}
            onValueChange={(v) => { if (v) selectedModel = v }}
          >
            <Select.Trigger class="w-full">
              {modelLabel(selectedModel)}
            </Select.Trigger>
            <Select.Content>
              {#each MODEL_OPTIONS as option (option.value)}
                <Select.Item value={option.value} label={option.label} />
              {/each}
            </Select.Content>
          </Select.Root>
        </div>

        <div class="space-y-1.5">
          <label for="aiwriting-apikey" class="text-sm font-medium text-foreground">
            {$_('aiwriting.settings.apiKey')}
          </label>
          <div class="flex items-center gap-2">
            <Input
              id="aiwriting-apikey"
              type={showApiKey ? 'text' : 'password'}
              bind:value={apiKey}
              placeholder={$_('aiwriting.settings.apiKeyPlaceholder')}
              class="flex-1 min-w-0"
              autocomplete="off"
            />
            <Button
              variant="outline"
              size="sm"
              onclick={() => { showApiKey = !showApiKey }}
              aria-label={showApiKey ? $_('aiwriting.settings.hideKey') : $_('aiwriting.settings.showKey')}
              title={showApiKey ? $_('aiwriting.settings.hideKey') : $_('aiwriting.settings.showKey')}
            >
              <Icon icon={showApiKey ? 'mdi:eye-off-outline' : 'mdi:eye-outline'} class="w-4 h-4" />
            </Button>
          </div>
          <p class="text-xs text-muted-foreground">
            {hasApiKey ? $_('aiwriting.settings.apiKeyStored') : $_('aiwriting.settings.apiKeyNotSaved')}
          </p>
        </div>

        {#if isCustom}
          <div class="space-y-1.5">
            <label for="aiwriting-custom-endpoint" class="text-sm font-medium text-foreground">
              {$_('aiwriting.settings.customEndpoint')}
            </label>
            <Input
              id="aiwriting-custom-endpoint"
              type="text"
              bind:value={customApiEndpoint}
              placeholder={$_('aiwriting.settings.customEndpointPlaceholder')}
            />
          </div>

          <div class="space-y-1.5">
            <label for="aiwriting-custom-model" class="text-sm font-medium text-foreground">
              {$_('aiwriting.settings.customModel')}
            </label>
            <Input
              id="aiwriting-custom-model"
              type="text"
              bind:value={customModel}
              placeholder={$_('aiwriting.settings.customModelPlaceholder')}
            />
          </div>
        {/if}

        <div class="space-y-1.5">
          <span class="text-sm font-medium text-foreground">
            {$_('aiwriting.settings.actionPrompt')}
          </span>
          <Select.Root
            value={selectedActionId}
            onValueChange={(v) => { if (v) selectAction(v) }}
          >
            <Select.Trigger class="w-full">
              {selectedAction ? actionLabel(selectedAction) : selectedActionId}
            </Select.Trigger>
            <Select.Content>
              {#each actionPrompts as action (action.id)}
                <Select.Item value={action.id} label={actionLabel(action)} />
              {/each}
            </Select.Content>
          </Select.Root>
          <p class="text-xs text-muted-foreground">{$_('aiwriting.settings.actionPromptHint')}</p>
        </div>

        <div class="space-y-1.5">
          <div class="flex items-center justify-between">
            <label for="aiwriting-prompt" class="text-sm font-medium text-foreground">
              {$_('aiwriting.settings.prompt')}
            </label>
            {#if promptCustomized}
              <Button
                variant="ghost"
                size="sm"
                onclick={resetPrompt}
              >
                {$_('aiwriting.settings.resetPrompt')}
              </Button>
            {/if}
          </div>
          <textarea
            id="aiwriting-prompt"
            value={promptDraft}
            oninput={(e) => {
              promptDraft = e.currentTarget.value
              draftPrompts[selectedActionId] = promptDraft
            }}
            rows="8"
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          ></textarea>
          <p class="text-xs text-muted-foreground">
            {promptCustomized
              ? $_('aiwriting.settings.promptCustomized')
              : $_('aiwriting.settings.promptDefault')}
          </p>
        </div>

        <div class="space-y-1.5">
          <label for="aiwriting-temperature" class="text-sm font-medium text-foreground">
            {$_('aiwriting.settings.temperature')}
          </label>
          <div class="flex items-center gap-3">
            <input
              id="aiwriting-temperature"
              type="range"
              min="0"
              max="2"
              step="0.01"
              bind:value={temperature}
              class="flex-1"
            />
            <span class="text-sm text-foreground w-10 text-right tabular-nums">{temperature.toFixed(2)}</span>
          </div>
          <p class="text-xs text-muted-foreground">{$_('aiwriting.settings.temperatureHint')}</p>
        </div>

        <div class="space-y-1.5">
          <label for="aiwriting-maxtokens" class="text-sm font-medium text-foreground">
            {$_('aiwriting.settings.maxTokens')}
          </label>
          <Input
            id="aiwriting-maxtokens"
            type="number"
            min="1"
            max="4000"
            bind:value={maxTokens}
          />
          <p class="text-xs text-muted-foreground">{$_('aiwriting.settings.maxTokensHint')}</p>
        </div>

        <div class="flex items-start gap-2">
          <input
            id="aiwriting-history"
            type="checkbox"
            bind:checked={useConversationHistory}
            class="mt-0.5"
          />
          <div>
            <label for="aiwriting-history" class="text-sm font-medium text-foreground">
              {$_('aiwriting.settings.useHistory')}
            </label>
            <p class="text-xs text-muted-foreground">{$_('aiwriting.settings.useHistoryHint')}</p>
          </div>
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
      <Button variant="ghost" onclick={handleClose}>{$_('aiwriting.settings.close')}</Button>
      <Button onclick={() => void handleSave()} disabled={loading || saving}>
        {#if saving}
          <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
        {/if}
        {$_('aiwriting.settings.save')}
      </Button>
    </div>
  </Dialog.Content>
</Dialog.Root>
