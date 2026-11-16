<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import Icon from '@iconify/svelte'
  import { Label } from '$lib/components/ui/label'
  import Switch from '$lib/components/ui/switch/Switch.svelte'
  import { Input } from '$lib/components/ui/input'
  import { Button } from '$lib/components/ui/button'
  import * as Select from '$lib/components/ui/select'
  import { _ } from '$lib/i18n'
  import { addToast } from '$lib/stores/toast'
  import { EventsOn, EventsOff } from '../../../../wailsjs/runtime/runtime'
  import {
    GetTranslationTargetLanguages,
    GetBergamotLanguages,
    DownloadBergamotLanguage,
    DeleteBergamotLanguage,
    GetBergamotEngineStatus,
    DownloadBergamotEngine,
    DeleteBergamotEngine,
  } from '$lib/viewerBindings'
  import { disposeBergamot } from '$lib/bergamot'
  import type { app as appModels } from '../../../../wailsjs/go/models'
  import type { translate as translateModels } from '../../../../wailsjs/go/models'

  interface Props {
    translateEnabled: boolean
    translateMode: string
    translateTargetLanguage: string
    translateOnlineProvider: string
    translateConsentGoogle: boolean
    translateConsentDeepL: boolean
    deeplKeyInput: string
    deeplKeyCleared: boolean
    deeplKeySet: boolean
    onTranslateEnabledChange: (value: boolean) => void
    onTranslateModeChange: (value: string) => void
    onTranslateTargetLanguageChange: (value: string) => void
    onTranslateOnlineProviderChange: (value: string) => void
    onTranslateConsentChange: (provider: string, value: boolean) => void
    onDeeplKeyInputChange: (value: string) => void
    onDeeplKeyClearedChange: (value: boolean) => void
  }

  let {
    translateEnabled = $bindable(),
    translateMode = $bindable(),
    translateTargetLanguage = $bindable(),
    translateOnlineProvider = $bindable(),
    translateConsentGoogle = $bindable(),
    translateConsentDeepL = $bindable(),
    deeplKeyInput = $bindable(),
    deeplKeyCleared = $bindable(),
    deeplKeySet = $bindable(),
    onTranslateEnabledChange,
    onTranslateModeChange,
    onTranslateTargetLanguageChange,
    onTranslateOnlineProviderChange,
    onTranslateConsentChange,
    onDeeplKeyInputChange,
    onDeeplKeyClearedChange,
  }: Props = $props()

  // Offline downloads (Bergamot engine + language models): loaded here,
  // not in the dialog's Save flow — downloads and deletes act
  // immediately. Nothing is bundled with the app: the engine (~5 MB)
  // and each language download on demand and stay cached in the Daak
  // data folder until removed here.
  let languages = $state<Array<translateModels.BergamotLanguage>>([])
  let languagesLoading = $state(true)
  let languagesError = $state('')
  let engine = $state<translateModels.EngineStatus | null>(null)
  let engineLoading = $state(true)
  let engineDownloading = $state(false)
  let engineProgress = $state({ done: 0, total: 0 })
  let engineDeleting = $state(false)
  // Per-language download progress, keyed by language code. A language
  // download is one or two legs (X→en, en→X); progress is the sum of
  // the latest per-leg values, tracked in legProgress.
  let legProgress = $state<Record<string, { done: number; total: number }>>({})
  let downloadingLang = $state<Record<string, boolean>>({})
  let deletingLang = $state('')
  let targetLanguages = $state<Array<appModels.TranslationTargetLanguage>>([])

  const totalSize = $derived(
    (engine?.downloadedBytes ?? 0) +
      languages.reduce((sum, l) => sum + (l.downloadedSize ?? 0), 0)
  )

  function languageProgress(code: string): { done: number; total: number } {
    let done = 0
    let total = 0
    for (const key of Object.keys(legProgress)) {
      const [from, to] = key.split('>')
      const lang = from === 'en' ? to : from
      if (lang !== code) continue
      done += legProgress[key].done
      total += legProgress[key].total
    }
    return { done, total }
  }

  function onDownloadProgress(payload: {
    kind: string
    from: string
    to: string
    downloaded: number
    total: number
  }) {
    if (payload.kind === 'engine') {
      engineProgress = { done: payload.downloaded, total: payload.total }
      return
    }
    const key = payload.from + '>' + payload.to
    legProgress = {
      ...legProgress,
      [key]: { done: payload.downloaded, total: payload.total },
    }
  }

  onMount(async () => {
    EventsOn('bergamot:download-progress', onDownloadProgress)
    try {
      targetLanguages = await GetTranslationTargetLanguages()
    } catch (err) {
      console.error('Failed to load translation target languages:', err)
    }
    await Promise.all([loadEngine(), loadLanguages()])
  })

  onDestroy(() => {
    EventsOff('bergamot:download-progress')
  })

  async function loadEngine() {
    engineLoading = true
    try {
      engine = await GetBergamotEngineStatus()
    } catch (err) {
      console.error('Failed to load Bergamot engine status:', err)
      engine = null
    } finally {
      engineLoading = false
    }
  }

  async function loadLanguages() {
    languagesLoading = true
    languagesError = ''
    try {
      languages = await GetBergamotLanguages()
    } catch (err) {
      console.error('Failed to load Bergamot languages:', err)
      languagesError = $_('settingsTranslation.catalogueFailed')
    } finally {
      languagesLoading = false
    }
  }

  function formatBytes(n: number): string {
    if (n < 1024) return n + ' B'
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
    return (n / (1024 * 1024)).toFixed(1) + ' MB'
  }

  function languageName(code: string): string {
    const known: Record<string, string> = {
      en: 'English',
      ur: 'Urdu',
      de: 'German',
      fr: 'French',
      es: 'Spanish',
      it: 'Italian',
      pt: 'Portuguese',
      nl: 'Dutch',
      ar: 'Arabic',
      hi: 'Hindi',
      fa: 'Persian',
      tr: 'Turkish',
      ru: 'Russian',
      ja: 'Japanese',
      'zh-Hans': 'Chinese (Simplified)',
      'zh-Hant': 'Chinese (Traditional)',
    }
    return known[code] ?? code
  }

  function directionsLabel(lang: translateModels.BergamotLanguage): string {
    if (lang.toEnglish && lang.fromEnglish) {
      return $_('settingsTranslation.languageBothDirections', {
        values: { language: lang.name },
      })
    }
    if (lang.toEnglish) {
      return $_('settingsTranslation.languageToEnglishOnly', {
        values: { language: lang.name },
      })
    }
    return $_('settingsTranslation.languageFromEnglishOnly', {
      values: { language: lang.name },
    })
  }

  const modeOptions = $derived([
    { value: 'prefer_offline', label: $_('settingsTranslation.modePreferOffline') },
    { value: 'offline_only', label: $_('settingsTranslation.modeOfflineOnly') },
    { value: 'online', label: $_('settingsTranslation.modeOnline') },
  ])

  function getModeLabel(value: string): string {
    return modeOptions.find((o) => o.value === value)?.label || value
  }

  function getTargetLabel(value: string): string {
    if (value === 'system') return $_('settingsTranslation.targetSystem')
    return targetLanguages.find((l) => l.code === value)?.name ?? languageName(value)
  }

  const targetOptions = $derived([
    { value: 'system', label: $_('settingsTranslation.targetSystem') },
    ...targetLanguages
      .filter((l) => l.code !== 'system')
      .map((l) => ({ value: l.code, label: l.name })),
  ])

  const providerOptions = $derived([
    { value: 'google', label: 'Google Translate' },
    { value: 'deepl', label: 'DeepL' },
  ])

  function getProviderLabel(value: string): string {
    return providerOptions.find((o) => o.value === value)?.label || value
  }

  function handleModeChange(value: string) {
    translateMode = value
    onTranslateModeChange?.(value)
  }
  function handleTargetChange(value: string) {
    translateTargetLanguage = value
    onTranslateTargetLanguageChange?.(value)
  }
  function handleProviderChange(value: string) {
    translateOnlineProvider = value
    onTranslateOnlineProviderChange?.(value)
  }

  function handleConsentChange(provider: string, value: boolean) {
    if (provider === 'google') translateConsentGoogle = value
    else translateConsentDeepL = value
    onTranslateConsentChange?.(provider, value)
  }

  async function handleDownloadEngine() {
    engineDownloading = true
    engineProgress = { done: 0, total: engine?.sizeBytes ?? 0 }
    try {
      await DownloadBergamotEngine()
      addToast({ type: 'success', message: $_('settingsTranslation.engineDownloadDone') })
      await loadEngine()
    } catch (err) {
      console.error('Bergamot engine download failed:', err)
      addToast({
        type: 'error',
        message: $_('settingsTranslation.engineDownloadFailed', {
          values: { error: err instanceof Error ? err.message : String(err) },
        }),
      })
    } finally {
      engineDownloading = false
    }
  }

  async function handleDeleteEngine() {
    engineDeleting = true
    try {
      await DeleteBergamotEngine()
      disposeBergamot()
      await loadEngine()
    } catch (err) {
      console.error('Bergamot engine delete failed:', err)
      addToast({
        type: 'error',
        message: $_('settingsTranslation.engineDeleteFailed', {
          values: { error: err instanceof Error ? err.message : String(err) },
        }),
      })
    } finally {
      engineDeleting = false
    }
  }

  async function handleDownloadLanguage(code: string) {
    downloadingLang = { ...downloadingLang, [code]: true }
    try {
      await DownloadBergamotLanguage(code)
      addToast({ type: 'success', message: $_('settingsTranslation.downloadDone') })
      await Promise.all([loadEngine(), loadLanguages()])
    } catch (err) {
      console.error('Bergamot language download failed:', err)
      addToast({
        type: 'error',
        message: $_('settingsTranslation.downloadFailed', {
          values: { error: err instanceof Error ? err.message : String(err) },
        }),
      })
    } finally {
      downloadingLang = { ...downloadingLang, [code]: false }
    }
  }

  async function handleDeleteLanguage(code: string) {
    deletingLang = code
    try {
      await DeleteBergamotLanguage(code)
      disposeBergamot()
      await loadLanguages()
    } catch (err) {
      console.error('Bergamot language delete failed:', err)
      addToast({
        type: 'error',
        message: $_('settingsTranslation.deleteFailed', {
          values: { error: err instanceof Error ? err.message : String(err) },
        }),
      })
    } finally {
      deletingLang = ''
    }
  }
</script>

<div class="space-y-6">
  <!-- Master toggle -->
  <div class="space-y-2">
    <div class="flex items-center justify-between">
      <div class="space-y-0.5">
        <Label for="translate-enabled">{$_('settingsTranslation.enabled')}</Label>
        <p class="text-xs text-muted-foreground">
          {$_('settingsTranslation.enabledHelp')}
        </p>
      </div>
      <Switch
        id="translate-enabled"
        bind:checked={translateEnabled}
        onCheckedChange={(v) => onTranslateEnabledChange?.(v)}
      />
    </div>
  </div>

  <div class="border-t border-border"></div>

  <!-- Translation mode -->
  <div class="space-y-2">
    <Label>{$_('settingsTranslation.mode')}</Label>
    <Select.Root value={translateMode} onValueChange={handleModeChange}>
      <Select.Trigger>
        <Select.Value>{getModeLabel(translateMode)}</Select.Value>
      </Select.Trigger>
      <Select.Content>
        {#each modeOptions as opt (opt.value)}
          <Select.Item value={opt.value} label={opt.label} />
        {/each}
      </Select.Content>
    </Select.Root>
    <p class="text-xs text-muted-foreground">
      {$_('settingsTranslation.modeHelp')}
    </p>
  </div>

  <!-- Target language -->
  <div class="space-y-2">
    <Label>{$_('settingsTranslation.target')}</Label>
    <Select.Root value={translateTargetLanguage} onValueChange={handleTargetChange}>
      <Select.Trigger>
        <Select.Value>{getTargetLabel(translateTargetLanguage)}</Select.Value>
      </Select.Trigger>
      <Select.Content>
        {#each targetOptions as opt (opt.value)}
          <Select.Item value={opt.value} label={opt.label} />
        {/each}
      </Select.Content>
    </Select.Root>
    <p class="text-xs text-muted-foreground">
      {$_('settingsTranslation.targetHelp')}
    </p>
  </div>

  <div class="border-t border-border"></div>

  <!-- Online provider -->
  <div class="space-y-2">
    <Label>{$_('settingsTranslation.onlineProvider')}</Label>
    <Select.Root value={translateOnlineProvider} onValueChange={handleProviderChange}>
      <Select.Trigger>
        <Select.Value>{getProviderLabel(translateOnlineProvider)}</Select.Value>
      </Select.Trigger>
      <Select.Content>
        {#each providerOptions as opt (opt.value)}
          <Select.Item value={opt.value} label={opt.label} />
        {/each}
      </Select.Content>
    </Select.Root>
    <p class="text-xs text-muted-foreground">
      {$_('settingsTranslation.onlineProviderHelp')}
    </p>
  </div>

  <!-- DeepL API key -->
  {#if translateOnlineProvider === 'deepl'}
    <div class="space-y-2">
      <Label for="deepl-key">{$_('settingsTranslation.deeplKey')}</Label>
      <div class="flex items-center gap-2">
        <Input
          id="deepl-key"
          type="password"
          placeholder={deeplKeySet
            ? $_('settingsTranslation.deeplKeySet')
            : $_('settingsTranslation.deeplKeyPlaceholder')}
          value={deeplKeyInput}
          oninput={(e) => {
            const v = (e.target as HTMLInputElement).value
            deeplKeyInput = v
            deeplKeyCleared = false
            onDeeplKeyInputChange?.(v)
            onDeeplKeyClearedChange?.(false)
          }}
        />
        {#if deeplKeySet || deeplKeyInput}
          <Button
            variant="ghost"
            onclick={() => {
              deeplKeyInput = ''
              deeplKeyCleared = true
              onDeeplKeyInputChange?.('')
              onDeeplKeyClearedChange?.(true)
            }}
          >
            {$_('settingsTranslation.deeplKeyClear')}
          </Button>
        {/if}
      </div>
      <p class="text-xs text-muted-foreground">
        {$_('settingsTranslation.deeplKeyHelp')}
      </p>
    </div>
  {/if}

  <!-- Online consent -->
  <div class="space-y-4">
    <p class="text-sm font-medium">{$_('settingsTranslation.consentHeading')}</p>
    <p class="text-xs text-muted-foreground">
      {$_('settingsTranslation.consentHelp')}
    </p>
    <div class="flex items-center justify-between">
      <div class="space-y-0.5">
        <Label for="consent-google">Google Translate</Label>
        <p class="text-xs text-muted-foreground">
          {$_('settingsTranslation.consentGoogleHelp')}
        </p>
      </div>
      <Switch
        id="consent-google"
        bind:checked={translateConsentGoogle}
        onCheckedChange={(v) => handleConsentChange('google', v)}
      />
    </div>
    <div class="flex items-center justify-between">
      <div class="space-y-0.5">
        <Label for="consent-deepl">DeepL</Label>
        <p class="text-xs text-muted-foreground">
          {$_('settingsTranslation.consentDeepLHelp')}
        </p>
      </div>
      <Switch
        id="consent-deepl"
        bind:checked={translateConsentDeepL}
        onCheckedChange={(v) => handleConsentChange('deepl', v)}
      />
    </div>
  </div>

  <div class="border-t border-border"></div>

  <!-- Offline downloads (Bergamot engine + language models) -->
  <div class="space-y-4">
    <p class="text-sm font-medium">{$_('settingsTranslation.packsHeading')}</p>
    <p class="text-xs text-muted-foreground">
      {$_('settingsTranslation.packsHelp')}
    </p>

    {#if !engineLoading && !languagesLoading}
      <p class="text-xs text-muted-foreground">
        {$_('settingsTranslation.packsTotal', { values: { size: formatBytes(totalSize) } })}
      </p>
    {/if}

    <!-- Offline engine -->
    <div class="space-y-2">
      <p class="text-sm font-medium">{$_('settingsTranslation.engineHeading')}</p>
      <p class="text-xs text-muted-foreground">
        {$_('settingsTranslation.engineHelp')}
      </p>
      {#if engineLoading}
        <div class="flex items-center gap-2 text-sm text-muted-foreground">
          <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
          <span>{$_('settingsTranslation.packsLoading')}</span>
        </div>
      {:else if engine}
        <div class="flex items-center justify-between gap-2 rounded-md border border-border px-3 py-2">
          <div class="text-sm">
            {#if engineDownloading}
              <span class="text-muted-foreground">
                {$_('settingsTranslation.engineDownloading', {
                  values: {
                    done: formatBytes(engineProgress.done),
                    total: formatBytes(engineProgress.total || engine.sizeBytes),
                  },
                })}
              </span>
            {:else if engine.available}
              <span class="font-medium">
                {$_('settingsTranslation.engineDownloaded', {
                  values: { size: formatBytes(engine.sizeBytes) },
                })}
              </span>
            {:else}
              <span class="text-muted-foreground">
                {$_('settingsTranslation.engineNotDownloaded', {
                  values: { size: formatBytes(engine.sizeBytes) },
                })}
              </span>
            {/if}
          </div>
          <div class="flex items-center gap-2 flex-shrink-0">
            {#if engine.available}
              <Button
                variant="ghost"
                disabled={engineDeleting}
                onclick={handleDeleteEngine}
              >
                {$_('settingsTranslation.engineRemove')}
              </Button>
            {:else}
              <Button disabled={engineDownloading} onclick={handleDownloadEngine}>
                {#if engineDownloading}
                  <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
                {/if}
                {$_('settingsTranslation.engineDownload')}
              </Button>
            {/if}
          </div>
        </div>
      {/if}
    </div>

    <!-- Offline languages -->
    <div class="space-y-2">
      <p class="text-sm font-medium">{$_('settingsTranslation.languagesHeading')}</p>
      <p class="text-xs text-muted-foreground">
        {$_('settingsTranslation.languagesHelp')}
      </p>

      {#if languagesLoading}
        <div class="flex items-center gap-2 text-sm text-muted-foreground">
          <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
          <span>{$_('settingsTranslation.languagesLoading')}</span>
        </div>
      {:else if languagesError}
        <p class="text-sm text-destructive">{languagesError}</p>
      {:else if languages.length === 0}
        <p class="text-sm text-muted-foreground">
          {$_('settingsTranslation.packsEmpty')}
        </p>
      {:else}
        <div class="space-y-2">
          {#each languages as lang (lang.code)}
            {@const progress = languageProgress(lang.code)}
            {@const isDownloading = !!downloadingLang[lang.code]}
            <div class="flex items-center justify-between gap-2 rounded-md border border-border px-3 py-2">
              <div class="text-sm">
                <span class="font-medium">{directionsLabel(lang)}</span>
                <span class="text-muted-foreground">
                  {#if isDownloading}
                    · {$_('settingsTranslation.languageDownloading', {
                      values: {
                        done: formatBytes(progress.done),
                        total: formatBytes(progress.total || lang.sizeBytes),
                      },
                    })}
                  {:else if lang.downloaded}
                    · {$_('settingsTranslation.languageDownloaded', {
                      values: { size: formatBytes(lang.downloadedSize) },
                    })}
                  {:else}
                    · {$_('settingsTranslation.languageNotDownloaded', {
                      values: { size: formatBytes(lang.sizeBytes) },
                    })}
                  {/if}
                </span>
              </div>
              <div class="flex items-center gap-2 flex-shrink-0">
                {#if lang.downloaded}
                  <Button
                    variant="ghost"
                    disabled={deletingLang === lang.code}
                    onclick={() => handleDeleteLanguage(lang.code)}
                  >
                    {$_('settingsTranslation.languageRemove')}
                  </Button>
                {:else}
                  <Button
                    disabled={isDownloading}
                    onclick={() => handleDownloadLanguage(lang.code)}
                  >
                    {#if isDownloading}
                      <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" />
                    {/if}
                    {$_('settingsTranslation.languageDownload')}
                  </Button>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>
