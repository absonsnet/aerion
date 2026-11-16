<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { get } from 'svelte/store'
  import Icon from '@iconify/svelte'
  import { _, locale } from '$lib/i18n'
  import { EventsOn, EventsOff } from '../../../../wailsjs/runtime/runtime'
  import {
    DetectMessageLanguage,
    SaveCachedTranslation,
    TranslateMessageOnline,
    SetTranslationOnlineConsent,
    CancelBergamotDownload,
  } from '$lib/viewerBindings'
  import {
    translateWithBergamot,
    translateHtmlWithBergamot,
    collectTextNodes,
    downloadBergamotModel,
    disposeBergamot,
  } from '$lib/bergamot'
  import ConfirmDialog from '$lib/components/ui/confirm-dialog/ConfirmDialog.svelte'
  import type { app as appModels } from '../../../../wailsjs/go/models'

  // Per-message translation banner (Phase D). One instance lives inside
  // each expanded message's own block in the viewer — never one button
  // for the whole thread. Translating one message leaves the others
  // untouched: each instance owns its state and its translated copy.
  //
  // Nothing is translated and nothing leaves the machine until the user
  // presses Translate. The banner names what actually happened
  // ("Translated from Urdu · Offline" / "· via Google Translate") and
  // both versions are kept in memory for an instant flip back.

  interface Props {
    messageId: string
    subject: string
    bodyHtml: string
    bodyText: string
    // The body as displayed (after S/MIME/PGP processing). When set,
    // this is what gets translated; otherwise bodyHtml/bodyText.
    displayBodyHtml?: string
    displayBodyText?: string
    // The parent swaps EmailBody's html/subject when this fires
    // (null = show the original again).
    onBodyChange: (translatedHtml: string | null, translatedSubject: string | null) => void
  }

  let {
    messageId,
    subject,
    bodyHtml,
    bodyText,
    displayBodyHtml,
    displayBodyText,
    onBodyChange,
  }: Props = $props()

  type BannerState =
    | 'detecting'
    | 'hidden' // no banner: same language, too short, low confidence, or disabled
    | 'offer'
    | 'downloading'
    | 'translating'
    | 'translated'
    | 'error'

  let bannerState = $state<BannerState>('detecting')
  let detection = $state<appModels.MessageDetection | null>(null)
  let showingOriginal = $state(false)
  let translatedHtml = $state<string | null>(null)
  let translatedSubject = $state<string | null>(null)
  let translatedEngine = $state('')
  let error = $state('')
  let progressDone = $state(0)
  let progressTotal = $state(0)
  let downloadDone = $state(0)
  let downloadTotal = $state(0)
  let showConsent = $state(false)
  let consentProvider = $state('')
  let detectedFor = ''
  let lastAction = $state<'offline' | 'online' | 'download' | null>(null)
  // The offline engine ran but returned the text unchanged (Bergamot's
  // Urdu model only handles Urdu script, so Roman Urdu comes back as
  // itself). The offer stays up with a note and the online button.
  let offlineUnchanged = $state(false)

  // Plain-text form of an HTML body, for comparing a translation with
  // the original: equal means the engine changed nothing.
  function plainText(html: string): string {
    return html
      .replace(/<[^>]*>/g, ' ')
      .replace(/&amp;/g, '&')
      .replace(/&lt;/g, '<')
      .replace(/&gt;/g, '>')
      .replace(/&quot;/g, '"')
      .replace(/&#39;/g, "'")
      .replace(/\s+/g, ' ')
      .trim()
  }

  function currentHtml(): string {
    return displayBodyHtml ?? bodyHtml
  }
  function currentText(): string {
    return displayBodyText ?? bodyText
  }
  function uiLanguage(): string {
    return get(locale) ?? navigator.language ?? 'en'
  }

  function engineLabel(engine: string): string {
    if (engine === 'bergamot' || engine === 'os') return $_('viewer.translationOffline')
    if (engine === 'google') return $_('viewer.translationViaGoogle')
    if (engine === 'deepl') return $_('viewer.translationViaDeepL')
    return engine
  }

  function formatBytes(n: number): string {
    if (n < 1024) return n + ' B'
    if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
    return (n / (1024 * 1024)).toFixed(1) + ' MB'
  }

  function onDownloadProgress(payload: {
    from: string
    to: string
    downloaded: number
    total: number
  }) {
    if (!detection) return
    downloadDone = payload.downloaded
    downloadTotal = payload.total
  }

  onMount(() => {
    EventsOn('bergamot:download-progress', onDownloadProgress)
  })
  onDestroy(() => {
    EventsOff('bergamot:download-progress')
  })

  // The body can arrive after the message expands (on-demand fetch):
  // re-run detection when the message or its body changes.
  $effect(() => {
    const key = messageId + '|' + (currentText().length + currentHtml().length)
    if (key !== detectedFor) runDetection()
  })

  async function runDetection() {
    const key = messageId + '|' + (currentText().length + currentHtml().length)
    detectedFor = key
    bannerState = 'detecting'
    showingOriginal = false
    translatedHtml = null
    translatedSubject = null
    offlineUnchanged = false
    error = ''
    onBodyChange(null, null)
    try {
      const det = await DetectMessageLanguage(messageId, uiLanguage())
      detection = det
      if (!det.showBanner) {
        bannerState = 'hidden'
        return
      }
      // Re-opening reuses the cache: the cached translation is shown
      // straight away, with no engine work and no new request.
      if (det.cached) {
        // Except an identity result cached before the guard existed:
        // it is not a translation, so offer again instead.
        if (plainText(det.cached.translatedBody) === plainText(currentHtml())) {
          offlineUnchanged = true
          bannerState = 'offer'
          return
        }
        translatedHtml = det.cached.translatedBody
        translatedSubject = det.cached.translatedSubject
        translatedEngine = det.cached.engine
        bannerState = 'translated'
        onBodyChange(translatedHtml, translatedSubject)
        return
      }
      bannerState = 'offer'
    } catch (err) {
      console.error('Failed to detect message language:', err)
      bannerState = 'hidden'
    }
  }

  function applyTranslation(html: string, subj: string, engine: string) {
    translatedHtml = html
    translatedSubject = subj
    translatedEngine = engine
    showingOriginal = false
    bannerState = 'translated'
    onBodyChange(html, subj)
  }

  async function translateOffline() {
    if (!detection) return
    lastAction = 'offline'
    bannerState = 'translating'
    error = ''
    progressDone = 0
    progressTotal = 0
    try {
      const html = currentHtml()
      const [translatedBody, translatedSubj] = await Promise.all([
        html.trim()
          ? translateHtmlWithBergamot(
              html,
              detection.language,
              detection.target,
              (done, total) => {
                progressDone = done
                progressTotal = total
              },
              (partial) => {
                // Long messages render progressively.
                translatedHtml = partial
                onBodyChange(partial, translatedSubject)
              }
            )
          : Promise.resolve(''),
        subject.trim()
          ? translateWithBergamot([subject], detection.language, detection.target).then(
              (r) => r[0] ?? subject
            )
          : Promise.resolve(subject),
      ])
      // An engine that returns the text unchanged has not translated
      // it (Bergamot's Urdu model only handles Urdu script, so Roman
      // Urdu comes back as itself). Do not cache or claim that: go
      // back to the offer with a note and the online button instead.
      if (plainText(translatedBody) === plainText(html)) {
        translatedHtml = null
        translatedSubject = null
        onBodyChange(null, null)
        offlineUnchanged = true
        bannerState = 'offer'
        return
      }
      await SaveCachedTranslation(
        messageId,
        detection.target,
        'bergamot',
        translatedSubj,
        translatedBody
      )
      applyTranslation(translatedBody, translatedSubj, 'bergamot')
    } catch (err) {
      console.error('Offline translation failed:', err)
      error = err instanceof Error ? err.message : String(err)
      bannerState = 'error'
    }
  }
  async function translateOnline() {
    if (!detection) return
    // One-time per-provider consent, before the first send. The
    // backend enforces the same gate and never contacts the provider
    // without it.
    if (!detection.onlineConsented) {
      consentProvider =
        detection.onlineProvider === 'deepl' ? 'DeepL' : 'Google Translate'
      showConsent = true
      return
    }
    await runOnlineTranslation()
  }

  async function runOnlineTranslation() {
    if (!detection) return
    lastAction = 'online'
    bannerState = 'translating'
    error = ''
    progressDone = 0
    progressTotal = 0
    try {
      const html = currentHtml()
      const { doc, nodes } = collectTextNodes(html)
      const segments = nodes.map((n) => n.textContent ?? '')
      const inputs = [subject, ...segments]
      const translated = await TranslateMessageOnline(
        messageId,
        inputs,
        detection.language,
        detection.target
      )
      const translatedSubj = translated[0] ?? subject
      translated.slice(1).forEach((text, i) => {
        if (nodes[i]) nodes[i].textContent = text
      })
      const translatedBody = doc.body.innerHTML
      const engine =
        detection.onlineProvider === 'deepl' ? 'deepl' : 'google'
      await SaveCachedTranslation(
        messageId,
        detection.target,
        engine,
        translatedSubj,
        translatedBody
      )
      applyTranslation(translatedBody, translatedSubj, engine)
    } catch (err) {
      console.error('Online translation failed:', err)
      error = err instanceof Error ? err.message : String(err)
      bannerState = 'error'
    }
  }

  async function handleConsentConfirm() {
    if (!detection) return
    showConsent = false
    try {
      await SetTranslationOnlineConsent(detection.onlineProvider, true)
      // Mutate in place: spreading a Wails model class loses its
      // convertValues method, so the spread is not a MessageDetection.
      detection.onlineConsented = true
      await runOnlineTranslation()
    } catch (err) {
      console.error('Failed to record translation consent:', err)
      error = err instanceof Error ? err.message : String(err)
      bannerState = 'error'
    }
  }

  async function downloadThenTranslate() {
    if (!detection) return
    lastAction = 'download'
    bannerState = 'downloading'
    error = ''
    downloadDone = 0
    downloadTotal = detection.offlineDownloadSize
    try {
      await downloadBergamotModel(detection.language, detection.target)
      disposeBergamot()
      detection = await DetectMessageLanguage(messageId, uiLanguage())
      if (detection.cached) {
        translatedHtml = detection.cached.translatedBody
        translatedSubject = detection.cached.translatedSubject
        translatedEngine = detection.cached.engine
        bannerState = 'translated'
        onBodyChange(translatedHtml, translatedSubject)
        return
      }
      await translateOffline()
    } catch (err) {
      console.error('Bergamot model download failed:', err)
      error = err instanceof Error ? err.message : String(err)
      bannerState = 'error'
    }
  }

  async function handleTranslate() {
    if (!detection) return
    if (detection.mode === 'online') {
      await translateOnline()
      return
    }
    if (detection.offlineAvailable) {
      await translateOffline()
      return
    }
    if (detection.offlineNeedsDownload) {
      await downloadThenTranslate()
      return
    }
    await translateOnline()
  }

  function toggleOriginal() {
    showingOriginal = !showingOriginal
    if (showingOriginal) {
      onBodyChange(null, null)
    } else if (translatedHtml !== null) {
      onBodyChange(translatedHtml, translatedSubject)
    }
  }
</script>

{#if bannerState !== 'hidden' && bannerState !== 'detecting' && detection}
  <div
    class="flex flex-col gap-1 px-3 py-2 mb-4 bg-muted/50 border border-border rounded-md text-sm"
    data-testid="translation-banner"
    data-message-id={messageId}
  >
    {#if bannerState === 'offer'}
      <div class="flex items-center justify-between gap-3">
        <div class="flex items-center gap-2 text-muted-foreground">
          <Icon icon="mdi:translate" class="w-4 h-4 flex-shrink-0" />
          <span>
            {$_('viewer.translationOffer', {
              values: { language: detection.languageName, target: detection.targetName },
            })}
          </span>
        </div>
        <div class="flex items-center gap-2 flex-shrink-0">
          {#if detection.mode !== 'online' && detection.offlineNeedsDownload}
            <button
              class="px-3 py-1 text-xs font-medium text-white bg-primary hover:bg-primary/90 rounded transition-colors"
              onclick={(e) => {
                e.stopPropagation()
                downloadThenTranslate()
              }}
              onkeydown={(e) => e.stopPropagation()}
            >
              {$_('viewer.translationDownloadModel', {
                values: { size: formatBytes(detection.offlineDownloadSize) },
              })}
            </button>
          {:else if detection.offlineAvailable || detection.mode === 'online'}
            <button
              class="px-3 py-1 text-xs font-medium text-white bg-primary hover:bg-primary/90 rounded transition-colors"
              onclick={(e) => {
                e.stopPropagation()
                handleTranslate()
              }}
              onkeydown={(e) => e.stopPropagation()}
            >
              {$_('viewer.translationTranslate', {
                values: { target: detection.targetName },
              })}
            </button>
          {:else if !detection.offlineSupported}
            <span class="text-xs text-muted-foreground">
              {$_('viewer.translationNoOffline')}
            </span>
          {/if}
          {#if detection.onlineAllowed && (detection.offlineNeedsDownload || !detection.offlineSupported || offlineUnchanged) && detection.mode !== 'online'}
            <button
              class="px-3 py-1 text-xs font-medium text-primary hover:bg-primary/10 rounded transition-colors"
              onclick={(e) => {
                e.stopPropagation()
                translateOnline()
              }}
              onkeydown={(e) => e.stopPropagation()}
            >
              {$_('viewer.translationTranslateOnline')}
            </button>
          {/if}
        </div>
      </div>
      {#if detection.isSecurity}
        <p class="text-xs text-muted-foreground">
          {$_('viewer.translationSecurityNote')}
        </p>
      {/if}
      {#if offlineUnchanged}
        <p class="text-xs text-muted-foreground">
          {$_('viewer.translationOfflineUnchanged')}
        </p>
      {/if}
    {:else if bannerState === 'downloading'}
      <div class="flex items-center justify-between gap-3">
        <div class="flex items-center gap-2 text-muted-foreground">
          <Icon icon="mdi:loading" class="w-4 h-4 animate-spin flex-shrink-0" />
          <span>
            {$_('viewer.translationDownloading', {
              values: {
                from: detection.languageName,
                to: detection.targetName,
                done: formatBytes(downloadDone),
                total: formatBytes(downloadTotal),
              },
            })}
          </span>
        </div>
        <button
          class="px-3 py-1 text-xs font-medium text-muted-foreground hover:bg-muted rounded transition-colors"
          onclick={(e) => {
            e.stopPropagation()
            CancelBergamotDownload()
          }}
          onkeydown={(e) => e.stopPropagation()}
        >
          {$_('common.cancel')}
        </button>
      </div>
    {:else if bannerState === 'translating'}
      <div class="flex items-center gap-2 text-muted-foreground">
        <Icon icon="mdi:loading" class="w-4 h-4 animate-spin flex-shrink-0" />
        <span>
          {#if progressTotal > 0}
            {$_('viewer.translationTranslatingProgress', {
              values: { done: progressDone, total: progressTotal },
            })}
          {:else}
            {$_('viewer.translationTranslating')}
          {/if}
        </span>
      </div>
    {:else if bannerState === 'translated'}
      <div class="flex items-center justify-between gap-3">
        <div class="flex items-center gap-2 text-muted-foreground">
          <Icon icon="mdi:translate" class="w-4 h-4 flex-shrink-0" />
          <span>
            {$_('viewer.translationDone', {
              values: {
                language: detection.languageName,
                engine: engineLabel(translatedEngine),
              },
            })}
          </span>
        </div>
        <button
          class="px-3 py-1 text-xs font-medium text-primary hover:bg-primary/10 rounded transition-colors flex-shrink-0"
          onclick={(e) => {
            e.stopPropagation()
            toggleOriginal()
          }}
          onkeydown={(e) => e.stopPropagation()}
        >
          {showingOriginal
            ? $_('viewer.translationShowTranslation')
            : $_('viewer.translationShowOriginal')}
        </button>
      </div>
      {#if !showingOriginal && translatedSubject && translatedSubject !== subject}
        <p class="text-xs text-muted-foreground">
          {$_('viewer.translationSubject', { values: { subject: translatedSubject } })}
        </p>
      {/if}
    {:else if bannerState === 'error'}
      <div class="flex items-center justify-between gap-3">
        <div class="flex items-center gap-2 text-destructive">
          <Icon icon="mdi:alert-circle-outline" class="w-4 h-4 flex-shrink-0" />
          <span>{$_('viewer.translationFailed', { values: { error } })}</span>
        </div>
        <div class="flex items-center gap-2 flex-shrink-0">
          {#if translatedHtml !== null}
            <button
              class="px-3 py-1 text-xs font-medium text-primary hover:bg-primary/10 rounded transition-colors"
              onclick={(e) => {
                e.stopPropagation()
                toggleOriginal()
              }}
              onkeydown={(e) => e.stopPropagation()}
            >
              {$_('viewer.translationShowOriginal')}
            </button>
          {/if}
          <button
            class="px-3 py-1 text-xs font-medium text-white bg-primary hover:bg-primary/90 rounded transition-colors"
            onclick={(e) => {
              e.stopPropagation()
              error = ''
              if (lastAction === 'online') translateOnline()
              else if (lastAction === 'download') downloadThenTranslate()
              else if (lastAction === 'offline') translateOffline()
              else bannerState = 'offer'
            }}
            onkeydown={(e) => e.stopPropagation()}
          >
            {$_('common.retry')}
          </button>
        </div>
      </div>
    {/if}
  </div>
{/if}

<ConfirmDialog
  bind:open={showConsent}
  title={$_('viewer.translationConsentTitle')}
  description={$_('viewer.translationConsentDescription', {
    values: { provider: consentProvider },
  })}
  confirmLabel={$_('viewer.translationConsentConfirm')}
  onConfirm={handleConsentConfirm}
  onCancel={() => (showConsent = false)}
/>
