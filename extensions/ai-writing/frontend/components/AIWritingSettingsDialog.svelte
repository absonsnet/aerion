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
    DEFAULT_PROMPT_IMPROVE,
    DEFAULT_TEMPERATURE,
    MODEL_OPTIONS,
    type AIWritingSaveSettingsInput,
    type AIWritingSettings,
  } from '$extensions/ai-writing/frontend/lib/aiwriting'
  // @ts-ignore - wailsjs bindings (AIWriting_* land after `make generate`)
  import { AIWriting_GetSettings, AIWriting_SaveSettings } from '$wailsjs/go/app/App'

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
  let promptImprove = $state(DEFAULT_PROMPT_IMPROVE)
  let temperature = $state(DEFAULT_TEMPERATURE)
  let maxTokens = $state(DEFAULT_MAX_TOKENS)
  let useConversationHistory = $state(true)

  const isCustom = $derived(selectedModel === CUSTOM_MODEL_VALUE)

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
      promptImprove = s.promptImprove || DEFAULT_PROMPT_IMPROVE
      useConversationHistory = s.useConversationHistory ?? true
      hasApiKey = s.hasApiKey ?? false
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

  /** Client-side mirror of the backend validation (options.js shape). */
  function validate(): string | null {
    if (!selectedModel) return $_('aiwriting.settings.errorModelRequired')
    if (isCustom) {
      if (!customModel.trim()) return $_('aiwriting.settings.errorCustomModel')
      if (!customApiEndpoint.trim()) return $_('aiwriting.settings.errorCustomEndpoint')
    } else if (!hasApiKey && !apiKey.trim()) {
      return $_('aiwriting.settings.errorApiKeyRequired')
    }
    if (!promptImprove.trim()) return $_('aiwriting.settings.errorPromptRequired')
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
      const input: AIWritingSaveSettingsInput = {
        selectedModel,
        // Empty string = keep the existing stored key (backend contract).
        apiKey: apiKey.trim(),
        temperature,
        maxTokens,
        customApiEndpoint: customApiEndpoint.trim(),
        customModel: customModel.trim(),
        promptImprove: promptImprove.trim(),
        useConversationHistory,
      }
      await AIWriting_SaveSettings(input)
      if (apiKey.trim() !== '') hasApiKey = true
      apiKey = ''
      status = { kind: 'success', message: $_('aiwriting.settings.saved') }
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
          <label for="aiwriting-prompt" class="text-sm font-medium text-foreground">
            {$_('aiwriting.settings.prompt')}
          </label>
          <textarea
            id="aiwriting-prompt"
            bind:value={promptImprove}
            rows="8"
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          ></textarea>
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
