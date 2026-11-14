<script lang="ts">
  // MailMergeDialog — the mail-merge dialog, mirroring Thunderbird's
  // Mail Merge dialog sections (Source / Attachments / Batch / Send).
  //
  // The template is the live draft, read through the ComposerContext at
  // open time (body HTML, subject, recipients, global attachments,
  // plain-text flag). To/Cc/Bcc/Subject stay editable here so a template
  // variable such as {{Email}} can be typed in. Only the user-composed
  // part of the body is merged — quoted/forwarded content is not sent
  // to recipients.
  //
  // Source rows: CSV/JSON files are picked with MailMerge_PickFile and
  // parsed in the frontend (see lib/mailmerge.ts — ParseFile returns
  // only 3 preview rows, so the full table comes from the file bytes
  // read via the existing readFileAsAttachment binding); Contacts loads
  // all unified records via MailMerge_GetContacts. XLSX has no
  // frontend parser, so it goes through the backend full-parse path
  // (MailMerge_ParseFileFull) — first sheet by default, sheet picker
  // when the workbook has more than one.
  //
  // Deliver modes (settled decisions): Save as drafts [DEFAULT] /
  // Send now / Send later. Phase 2 Send later renders each row into
  // the persisted outbox queue — nothing is sent by the run itself;
  // the background runner sends up to N per hour (see the Mail Merge
  // rail tab), or Send Unsent sends them now. The safety cap applies
  // at SEND time only, never to draft generation or queueing. Preview
  // & validate blocks Start while any variable is unresolved or
  // signing is requested but unavailable; invalid emails, duplicates
  // and empty-To rows are listed as will-be-skipped, never
  // run-blocking. Phase 3 signing: each message is signed
  // individually with the account's S/MIME certificate or PGP key
  // (S/MIME first, like the composer); drafts keep the signing choice
  // and are signed when sent.

  import Icon from '@iconify/svelte'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { toasts } from '$lib/stores/toast'
  import { htmlToPlainText } from '$lib/components/composer/composerUtils'
  import type { ComposerContext } from '$lib/composerContext'
  import type { ComposerApi } from '$lib/composerApi'
  import MailMergeProgress from './MailMergeProgress.svelte'
  import {
    DEFAULT_MAIL_MERGE_SETTINGS,
    base64ToBytes,
    formatAddress,
    formatAddressList,
    normalizeRenderedMessage,
    normalizeValidationResult,
    parseSourceBytes,
    type MailMergeParsedFileFull,
    type MailMergeRenderedMessage,
    type MailMergeRunRequest,
    type MailMergeSettings,
    type MailMergeSigningInfo,
    type MailMergeValidationResult,
    type Row,
  } from '$extensions/mail-merge/frontend/lib/mailmerge'

  interface Props {
    open: boolean
    /** Window-aware backend API from MailMergeButton (via getContext there). */
    api: ComposerApi
    composerContext: ComposerContext | null
    accountId: string
    onClose?: () => void
  }

  let { open = $bindable(false), api, composerContext, accountId, onClose }: Props = $props()

  type SourceKind = 'csv' | 'json' | 'xlsx' | 'contacts'
  type DeliverChoice = 'drafts' | 'send' | 'later'

  // --- Template (snapshot of the live draft, editable here) ---
  let fromAddress = $state('')
  let toTemplate = $state('')
  let ccTemplate = $state('')
  let bccTemplate = $state('')
  let replyToTemplate = $state('')
  let subjectTemplate = $state('')
  let bodyHtml = $state('')
  let bodyText = $state('')
  let isPlainText = $state(false)
  let globalAttachments = $state<Array<{ filename: string; contentType: string; size: number; dataBase64: string }>>([])
  let attachmentField = $state('')

  // --- Source ---
  let sourceKind = $state<SourceKind | null>(null)
  let sourceLabel = $state('')
  let columns = $state<string[]>([])
  let rows = $state<Row[]>([])
  let fileWarning = $state('')
  let sourceBusy = $state(false)
  // XLSX sheet selection (backend full-parse path only).
  let sheets = $state<string[]>([])
  let selectedSheet = $state('')

  // --- Batch / deliver ---
  let deliverMode = $state<DeliverChoice>('drafts')
  let pauseSeconds = $state(DEFAULT_MAIL_MERGE_SETTINGS.defaultPauseSeconds)
  let randomPause = $state(false)
  let startRow = $state(0) // 0 = first row (backend: 1-based, 0 = first)
  let stopRow = $state(0) // 0 = last row
  let capEnabled = $state(true)
  let capLimit = $state(100)
  let attachmentDir = $state('')

  // --- Signing (Phase 3) ---
  // signingInfo is the account's S/MIME/PGP setup, resolved once per
  // open for the From identity. The checkbox defaults to the saved
  // per-account choice, else to the composer's signing default (a sign
  // policy of "always"). If the user edits From to another identity
  // afterwards, the run-time pre-flight still resolves signing for
  // that identity — a mismatch can only block the run with a clear
  // error, never send unsigned.
  let signMessages = $state(false)
  let signingInfo = $state<MailMergeSigningInfo | null>(null)
  let signingLoaded = $state(false)

  // --- Preview / validation / run ---
  let validation = $state<MailMergeValidationResult | null>(null)
  let previews = $state<MailMergeRenderedMessage[]>([])
  let previewBusy = $state(false)
  let previewError = $state('')
  let startBusy = $state(false)
  let startError = $state('')
  let runId = $state<string | null>(null)
  let dialogError = $state('')
  let showHistory = $state(false)

  const rowCount = $derived(rows.length)
  const selectedRows = $derived.by(() => {
    if (rows.length === 0) return []
    const start = startRow > 1 ? startRow - 1 : 0
    let stop = rows.length
    if (stopRow > 0 && stopRow < stop) stop = stopRow
    if (start > rows.length) return []
    if (stop < start) return []
    return rows.slice(start, stop)
  })
  const unresolved = $derived(validation?.unresolvedVariables ?? [])
  const canStart = $derived(
    !!accountId &&
      rowCount > 0 &&
      selectedRows.length > 0 &&
      fromAddress.trim() !== '' &&
      validation !== null &&
      unresolved.length === 0 &&
      !validation.signingError &&
      !startBusy &&
      !runId,
  )

  function buildTemplate() {
    return {
      accountId,
      from: fromAddress.trim(),
      to: toTemplate,
      cc: ccTemplate,
      bcc: bccTemplate,
      replyTo: replyToTemplate,
      subject: subjectTemplate,
      bodyHtml,
      bodyText,
      isPlainText,
      globalAttachments,
      attachmentField: attachmentField.trim(),
      // Sign only when the account actually has a usable identity;
      // signKind is what the backend resolved (the run pre-flight
      // re-resolves it authoritatively, so this is informational).
      sign: signMessages && signingInfo?.available === true,
      signKind: signMessages && signingInfo?.available ? signingInfo.kind : '',
    }
  }

  // Persist an explicit signing choice per account. A save failure
  // must not block the dialog — the run still validates signing
  // before it starts.
  async function handleSignChange(checked: boolean): Promise<void> {
    signMessages = checked
    try {
      const s = (await api.mailMergeGetSettings(accountId)) as MailMergeSettings
      await api.mailMergeSaveSettings(accountId, { ...s, signMessages: checked, signMessagesSet: true })
    } catch (err) {
      console.error('mailMergeSaveSettings (signing) failed:', err)
    }
  }

  async function snapshotDraft(): Promise<void> {
    dialogError = ''
    if (!composerContext) {
      dialogError = 'Mail Merge needs the composer context, which is not available here.'
      return
    }
    const recipients = composerContext.getRecipients()
    toTemplate = formatAddressList(recipients.to)
    ccTemplate = formatAddressList(recipients.cc)
    bccTemplate = formatAddressList(recipients.bcc)
    replyToTemplate = recipients.replyTo
    subjectTemplate = composerContext.getSubject()
    bodyHtml = composerContext.getUserComposedHTML()
    bodyText = htmlToPlainText(bodyHtml)
    isPlainText = composerContext.isPlainText()
    globalAttachments = composerContext.getAttachments().map((a) => ({
      filename: a.filename,
      contentType: a.contentType,
      size: a.size,
      dataBase64: a.data,
    }))
    // From: the account's default identity (the merge service requires
    // a parseable From address; variables in From are rendered per row).
    try {
      const identities = (await api.getIdentities(accountId)) || []
      const def = identities.find((i) => i.isDefault) ?? identities[0]
      if (def) fromAddress = formatAddress(def.name, def.email)
    } catch (err) {
      console.error('mailMerge getIdentities failed:', err)
    }
    // Per-account defaults for pause / deliver / cap.
    let savedSettings: MailMergeSettings | null = null
    try {
      const s = (await api.mailMergeGetSettings(accountId)) as MailMergeSettings
      savedSettings = s
      pauseSeconds = typeof s.defaultPauseSeconds === 'number' ? s.defaultPauseSeconds : 2
      randomPause = s.defaultRandomPause ?? false
      capEnabled = s.capEnabled ?? true
      capLimit = s.capLimit && s.capLimit > 0 ? s.capLimit : 100
      deliverMode = s.defaultDeliverMode === 'send' ? 'send' : 'drafts'
    } catch (err) {
      console.error('mailMergeGetSettings failed:', err)
    }
    // Signing setup for the From identity (see the signing state
    // comment above). The checkbox defaults to the saved per-account
    // choice, else to the composer's signing default.
    signingLoaded = false
    signingInfo = null
    signMessages = false
    try {
      signingInfo = (await api.mailMergeGetSigningInfo(accountId, fromAddress)) as MailMergeSigningInfo
      if (signingInfo?.available) {
        signMessages = savedSettings?.signMessagesSet ? savedSettings.signMessages : signingInfo.defaultSign
      }
    } catch (err) {
      console.error('mailMergeGetSigningInfo failed:', err)
      signingInfo = null
    } finally {
      signingLoaded = true
    }
  }

  function resetSource(): void {
    sourceKind = null
    sourceLabel = ''
    columns = []
    rows = []
    fileWarning = ''
    validation = null
    previews = []
    previewError = ''
    sheets = []
    selectedSheet = ''
  }

  async function pickSourceFile(kind: 'csv' | 'json' | 'xlsx'): Promise<void> {
    sourceBusy = true
    fileWarning = ''
    try {
      const path = await api.mailMergePickFile(kind)
      if (!path) return
      if (kind === 'xlsx') {
        // No frontend XLSX parser: the backend returns the full table.
        await loadXlsx(path, '')
        return
      }
      // Read the file bytes via the existing attachment reader, then
      // parse locally (backend ParseFile returns preview rows only).
      const att = await api.readFileAsAttachment(path)
      if (!att) throw new Error('Could not read the selected file.')
      const parsed = parseSourceBytes(base64ToBytes(att.data), kind)
      sourceKind = kind
      sourceLabel = path
      columns = parsed.columns
      rows = parsed.rows
      sheets = []
      selectedSheet = ''
      validation = null
      previews = []
      // Server-side cross-check (columns + row count).
      try {
        const server = await api.mailMergeParseFile(path, kind)
        if (server.rowCount !== parsed.rows.length) {
          fileWarning = `Note: the server counted ${server.rowCount} rows, this view parsed ${parsed.rows.length}. The parsed rows below are the ones that will run.`
        } else if (server.columns.join('|') !== parsed.columns.join('|')) {
          fileWarning = 'Note: the server parsed different column names than this view. The columns below are the ones that will run.'
        }
      } catch (err) {
        fileWarning = `Server-side parse check failed (${(err as Error)?.message ?? String(err)}); the rows below are still usable.`
      }
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err)
      console.error('mailMerge pickSourceFile failed:', err)
      toasts.error(msg)
      fileWarning = msg
    } finally {
      sourceBusy = false
    }
  }

  /** Load an XLSX file via the backend full-parse path. sheet "" = first sheet. */
  async function loadXlsx(path: string, sheet: string): Promise<void> {
    sourceBusy = true
    fileWarning = ''
    try {
      const full = (await api.mailMergeParseFileFull(path, 'xlsx', sheet)) as MailMergeParsedFileFull
      sourceKind = 'xlsx'
      sourceLabel = path
      columns = full.columns || []
      rows = (full.rows || []) as Row[]
      sheets = full.sheets || []
      selectedSheet = sheet || sheets[0] || ''
      validation = null
      previews = []
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err)
      console.error('mailMerge loadXlsx failed:', err)
      toasts.error(msg)
      fileWarning = msg
    } finally {
      sourceBusy = false
    }
  }

  async function handleSheetChange(sheet: string): Promise<void> {
    if (!sourceLabel || sheet === selectedSheet) return
    await loadXlsx(sourceLabel, sheet)
  }

  async function loadContacts(): Promise<void> {
    sourceBusy = true
    fileWarning = ''
    try {
      const result = await api.mailMergeGetContacts()
      sourceKind = 'contacts'
      sourceLabel = 'All contacts (local, CardDAV, Google, Microsoft)'
      columns = result.columns || []
      rows = (result.rows || []) as Row[]
      sheets = []
      selectedSheet = ''
      validation = null
      previews = []
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err)
      console.error('mailMergeGetContacts failed:', err)
      toasts.error(msg)
      fileWarning = msg
    } finally {
      sourceBusy = false
    }
  }

  async function pickAttachmentDir(): Promise<void> {
    try {
      const dir = await api.mailMergePickAttachmentDir()
      if (dir) attachmentDir = dir
    } catch (err) {
      toasts.error((err as Error)?.message ?? String(err))
    }
  }

  async function handlePreviewValidate(): Promise<void> {
    if (selectedRows.length === 0) {
      previewError = 'Pick a source first — there are no rows to preview.'
      return
    }
    previewBusy = true
    previewError = ''
    validation = null
    previews = []
    try {
      const template = buildTemplate()
      validation = normalizeValidationResult(
        (await api.mailMergeValidate(template, selectedRows)) as MailMergeValidationResult,
      )
      // An unresolved variable cannot render — skip Preview (it would
      // throw the same error) and keep the validation set so the
      // blocking "unresolved variables" message renders below.
      if (validation.unresolvedVariables.length > 0) {
        return
      }
      previews = (((await api.mailMergePreview(template, selectedRows, [])) as MailMergeRenderedMessage[]) || []).map(
        normalizeRenderedMessage,
      )
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err)
      console.error('mailMerge preview/validate failed:', err)
      previewError = msg
      previews = []
    } finally {
      previewBusy = false
    }
  }

  async function handleStart(): Promise<void> {
    if (!canStart) return
    startBusy = true
    startError = ''
    try {
      const req: MailMergeRunRequest = {
        template: buildTemplate(),
        rows,
        sourceKind: sourceKind ?? 'csv',
        // "Send later" queues the rendered messages in the persisted
        // outbox (Phase 2) — nothing is sent by this run. The safety
        // cap does not apply to queueing; the per-hour budget governs
        // when the queued messages go out.
        deliverMode: deliverMode === 'send' ? 'send' : deliverMode === 'later' ? 'later' : 'drafts',
        pauseSeconds,
        randomPause,
        startRow,
        stopRow,
        capEnabled: deliverMode === 'send' ? capEnabled : false,
        capLimit,
        accountId,
        attachmentDir,
      }
      runId = await api.mailMergeStartRun(req)
      showHistory = false
    } catch (err) {
      const msg = (err as Error)?.message ?? String(err)
      console.error('mailMergeStartRun failed:', err)
      startError = msg
      toasts.error(msg)
    } finally {
      startBusy = false
    }
  }

  function handleClose() {
    open = false
    onClose?.()
  }

  $effect(() => {
    if (open) {
      runId = null
      validation = null
      previews = []
      startError = ''
      previewError = ''
      showHistory = false
      void snapshotDraft()
    }
  })
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) handleClose() }}>
  <Dialog.Content class="max-w-3xl">
    <Dialog.Header>
      <Dialog.Title>Mail Merge</Dialog.Title>
      <Dialog.Description>
        One personalised message per recipient, from this draft as the template. Put {'{{Email}}'} in the To field and use {'{{…}}'} variables in the subject and body — only the part you wrote is merged, quoted or forwarded content is not sent.
      </Dialog.Description>
    </Dialog.Header>

    <div class="space-y-5 mt-2 max-h-[68vh] overflow-y-auto pr-1">
      {#if dialogError}
        <p class="text-sm text-destructive">{dialogError}</p>
      {/if}

      <!-- Source -->
      <section class="space-y-2">
        <h3 class="text-sm font-medium">Recipients</h3>
        <div class="flex flex-wrap gap-2">
          <Button variant={sourceKind === 'csv' ? 'default' : 'outline'} size="sm" disabled={sourceBusy} onclick={() => void pickSourceFile('csv')}>
            CSV file…
          </Button>
          <Button variant={sourceKind === 'json' ? 'default' : 'outline'} size="sm" disabled={sourceBusy} onclick={() => void pickSourceFile('json')}>
            JSON file…
          </Button>
          <Button variant={sourceKind === 'xlsx' ? 'default' : 'outline'} size="sm" disabled={sourceBusy} onclick={() => void pickSourceFile('xlsx')}>
            XLSX file…
          </Button>
          <Button variant={sourceKind === 'contacts' ? 'default' : 'outline'} size="sm" disabled={sourceBusy} onclick={() => void loadContacts()}>
            Contacts
          </Button>
          {#if sourceKind}
            <Button variant="ghost" size="sm" onclick={resetSource}>Clear</Button>
          {/if}
        </div>
        {#if sourceBusy}
          <p class="text-sm text-muted-foreground flex items-center gap-2">
            <Icon icon="mdi:loading" class="w-4 h-4 animate-spin" /> Loading…
          </p>
        {/if}
        {#if sourceLabel}
          <p class="text-xs text-muted-foreground break-all">{sourceLabel}</p>
        {/if}
        {#if sourceKind === 'xlsx' && sheets.length > 1}
          <div class="flex items-center gap-2">
            <label for="mm-sheet" class="text-sm text-muted-foreground">Sheet</label>
            <select
              id="mm-sheet"
              value={selectedSheet}
              onchange={(e) => void handleSheetChange(e.currentTarget.value)}
              class="rounded-md border border-input bg-background px-2 py-1 text-sm text-foreground"
            >
              {#each sheets as sheet (sheet)}
                <option value={sheet}>{sheet}</option>
              {/each}
            </select>
          </div>
        {/if}
        {#if fileWarning}
          <p class="text-xs text-amber-700 dark:text-amber-400">{fileWarning}</p>
        {/if}
        {#if columns.length > 0}
          <p class="text-sm">
            <span class="font-medium">{rowCount}</span> recipient{rowCount === 1 ? '' : 's'} ·
            columns: {columns.join(', ')}
          </p>
          {#if rows.length > 0}
            <div class="max-h-40 overflow-auto rounded-md border border-border">
              <table class="w-full text-xs">
                <thead class="sticky top-0 bg-muted">
                  <tr class="text-left">
                    <th class="px-2 py-1 font-medium">#</th>
                    {#each columns.slice(0, 5) as col (col)}
                      <th class="px-2 py-1 font-medium">{col}</th>
                    {/each}
                    {#if columns.length > 5}<th class="px-2 py-1 font-medium">…</th>{/if}
                  </tr>
                </thead>
                <tbody>
                  {#each rows.slice(0, 3) as row, i (i)}
                    <tr class="border-t border-border">
                      <td class="px-2 py-1 tabular-nums text-muted-foreground">{i + 1}</td>
                      {#each columns.slice(0, 5) as col (col)}
                        <td class="px-2 py-1 break-all">{row[col] ?? ''}</td>
                      {/each}
                      {#if columns.length > 5}<td class="px-2 py-1 text-muted-foreground">…</td>{/if}
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
        {/if}
      </section>

      <!-- Per-row attachments -->
      <section class="space-y-2">
        <h3 class="text-sm font-medium">Per-recipient attachments</h3>
        <div class="grid grid-cols-[88px_1fr] items-center gap-2">
          <label for="mm-attfield" class="text-sm text-muted-foreground">Field</label>
          <Input id="mm-attfield" type="text" bind:value={attachmentField} placeholder={'{{Attachment}} — one file per line'} />
        </div>
        <div class="flex items-center gap-2">
          <Button variant="outline" size="sm" onclick={() => void pickAttachmentDir()}>Choose folder…</Button>
          <span class="text-xs text-muted-foreground break-all">{attachmentDir || 'No folder picked — relative names resolve against the working directory.'}</span>
        </div>
      </section>

      <!-- Deliver + batch -->
      <section class="space-y-3">
        <h3 class="text-sm font-medium">Send</h3>
        <div class="space-y-1.5">
          <label class="flex items-start gap-2 text-sm">
            <input type="radio" name="mm-deliver" value="drafts" bind:group={deliverMode} class="mt-0.5" />
            <span><span class="font-medium">Save as drafts</span> <span class="text-muted-foreground">(recommended — review each message in Drafts before sending)</span></span>
          </label>
          <label class="flex items-start gap-2 text-sm">
            <input type="radio" name="mm-deliver" value="send" bind:group={deliverMode} class="mt-0.5" />
            <span><span class="font-medium">Send now</span> <span class="text-muted-foreground">(sends immediately; the safety cap below applies)</span></span>
          </label>
          <label class="flex items-start gap-2 text-sm">
            <input type="radio" name="mm-deliver" value="later" bind:group={deliverMode} class="mt-0.5" />
            <span><span class="font-medium">Send later</span> <span class="text-muted-foreground">(queues each message in the outbox — sent automatically, up to 100 per hour, or now via Send Unsent in the Mail Merge rail tab)</span></span>
          </label>
        </div>

        <div class="flex flex-wrap items-end gap-4">
          <div class="space-y-1">
            <label for="mm-pause" class="text-sm text-muted-foreground">Pause between messages (s)</label>
            <Input id="mm-pause" type="number" min="0" bind:value={pauseSeconds} class="w-24" />
          </div>
          <label class="flex items-center gap-2 text-sm pb-2">
            <input type="checkbox" bind:checked={randomPause} />
            Random 0–X s
          </label>
          <div class="space-y-1">
            <label for="mm-start" class="text-sm text-muted-foreground">Start row</label>
            <Input id="mm-start" type="number" min="0" bind:value={startRow} class="w-24" />
          </div>
          <div class="space-y-1">
            <label for="mm-stop" class="text-sm text-muted-foreground">Stop row</label>
            <Input id="mm-stop" type="number" min="0" bind:value={stopRow} class="w-24" />
          </div>
        </div>
        <p class="text-xs text-muted-foreground">Start/Stop are 1-based row numbers; 0 means first / last row.</p>

        {#if deliverMode === 'send'}
          <div class="space-y-1.5 rounded-md border border-border p-3">
            <label class="flex items-center gap-2 text-sm font-medium">
              <input type="checkbox" bind:checked={capEnabled} />
              Safety cap — send at most
              <Input type="number" min="1" bind:value={capLimit} disabled={!capEnabled} class="w-24" />
              messages per run
            </label>
            <p class="text-xs text-muted-foreground">
              Rows over the cap are held as “held — over cap” and never sent until you approve them in the run log (Send next {capLimit} / Send all remaining / Cancel remaining). The cap applies at send time, not to draft generation.
            </p>
          </div>
        {/if}
      </section>

      <!-- Signing (Phase 3) -->
      <section class="space-y-2">
        <h3 class="text-sm font-medium">Signing</h3>
        {#if !signingLoaded}
          <p class="text-sm text-muted-foreground">Checking signing setup…</p>
        {:else if signingInfo?.available}
          <label class="flex items-start gap-2 text-sm">
            <input type="checkbox" checked={signMessages} onchange={(e) => void handleSignChange(e.currentTarget.checked)} class="mt-0.5" />
            <span>
              <span class="font-medium">Sign messages</span>
              <span class="text-muted-foreground">
                ({signingInfo.kind === 'smime' ? 'S/MIME' : 'PGP'} — each message is signed individually with this account's {signingInfo.kind === 'smime' ? 'certificate' : 'key'})
              </span>
            </span>
          </label>
          <p class="text-xs text-muted-foreground">
            {#if signMessages}
              Drafts keep the signing choice and are signed when sent; queued messages are signed when queued.
            {:else}
              Messages will be sent unsigned. The composer's signing setting for this account is {signingInfo.defaultSign ? 'on' : 'off'}.
            {/if}
          </p>
        {:else}
          <label class="flex items-start gap-2 text-sm opacity-60">
            <input type="checkbox" disabled class="mt-0.5" />
            <span><span class="font-medium">Sign messages</span></span>
          </label>
          <p class="text-xs text-muted-foreground">
            {signingInfo?.reason ?? 'No signing identity is configured for this account'}. Set one up in Settings → Accounts → Security (S/MIME certificate or PGP key).
          </p>
        {/if}
      </section>

      <!-- Preview & validation -->
      <section class="space-y-2">
        <div class="flex items-center gap-2">
          <h3 class="text-sm font-medium">Preview &amp; validation</h3>
          <Button variant="outline" size="sm" disabled={previewBusy || rowCount === 0} onclick={() => void handlePreviewValidate()}>
            {previewBusy ? 'Checking…' : 'Preview & validate'}
          </Button>
        </div>
        {#if previewError}
          <p class="text-sm text-destructive">{previewError}</p>
        {/if}
        {#if validation}
          {#if unresolved.length > 0}
            <p class="text-sm text-destructive" role="alert">
              Blocked — unresolved variable{unresolved.length === 1 ? '' : 's'}: {unresolved.join(', ')}. Add the column to your source or remove the variable from the template.
            </p>
          {:else}
            <p class="text-sm text-green-700 dark:text-green-400">All variables resolve.</p>
          {/if}
          {#if validation.signingError}
            <p class="text-sm text-destructive" role="alert">
              Blocked — {validation.signingError}. Set up signing in Settings → Accounts → Security, or turn signing off.
            </p>
          {/if}
          {#if validation.invalidEmails.length > 0}
            <p class="text-sm text-amber-700 dark:text-amber-400">
              {validation.invalidEmails.length} invalid address{validation.invalidEmails.length === 1 ? '' : 'es'} will be skipped:
              {validation.invalidEmails.slice(0, 5).map((e) => `row ${e.row + 1}: ${e.address}`).join('; ')}{validation.invalidEmails.length > 5 ? '…' : ''}
            </p>
          {/if}
          {#if validation.duplicateEmails > 0}
            <p class="text-sm text-muted-foreground">{validation.duplicateEmails} duplicate recipient{validation.duplicateEmails === 1 ? '' : 's'} will be sent once.</p>
          {/if}
          {#if validation.emptyToRows.length > 0}
            <p class="text-sm text-muted-foreground">{validation.emptyToRows.length} row{validation.emptyToRows.length === 1 ? '' : 's'} with an empty To address will be skipped.</p>
          {/if}
          {#if validation.missingAttachmentFiles.length > 0}
            <p class="text-sm text-amber-700 dark:text-amber-400">
              {validation.missingAttachmentFiles.length} per-row attachment{validation.missingAttachmentFiles.length === 1 ? '' : 's'} missing (those rows will be skipped):
              {validation.missingAttachmentFiles.slice(0, 3).map((m) => `row ${m.row + 1}: ${m.path}`).join('; ')}{validation.missingAttachmentFiles.length > 3 ? '…' : ''}
            </p>
          {/if}
        {/if}
        {#if previews.length > 0}
          <p class="text-xs text-muted-foreground">
            {#if signMessages && signingInfo?.available}
              These messages will be signed with {signingInfo.kind === 'smime' ? 'S/MIME' : 'PGP'}.
            {:else}
              These messages will not be signed.
            {/if}
          </p>
          <div class="space-y-3">
            {#each previews as msg (msg.rowIndex)}
              <div class="rounded-md border border-border p-3 space-y-1 text-sm">
                <p><span class="text-muted-foreground">To:</span> {msg.to || '—'}</p>
                {#if msg.cc}<p><span class="text-muted-foreground">Cc:</span> {msg.cc}</p>{/if}
                <p><span class="text-muted-foreground">Subject:</span> {msg.subject || '—'}</p>
                {#if msg.attachments.length > 0}
                  <p><span class="text-muted-foreground">Attachments:</span> {msg.attachments.join(', ')}</p>
                {/if}
                <div class="rounded bg-muted/50 p-2 text-sm max-h-32 overflow-y-auto">
                  {@html msg.bodyHtml || msg.bodyText}
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </section>

      <!-- Run -->
      {#if runId}
        <section class="space-y-2">
          <h3 class="text-sm font-medium">Run</h3>
          <MailMergeProgress {api} runId={runId} {capLimit} onRunStarted={(id) => { runId = id }} />
        </section>
      {/if}

      {#if showHistory}
        <section class="space-y-2">
          <MailMergeProgress {api} runId={null} {capLimit} />
        </section>
      {/if}
    </div>

    <div class="flex items-center justify-between gap-2 pt-4 border-t border-border mt-4">
      <Button variant="ghost" size="sm" onclick={() => { showHistory = !showHistory }}>
        {showHistory ? 'Hide run history' : 'Run history'}
      </Button>
      <div class="flex items-center gap-2">
        {#if startError}
          <span class="text-sm text-destructive max-w-72 text-right">{startError}</span>
        {/if}
        <Button variant="ghost" onclick={handleClose}>Close</Button>
        <Button
          disabled={!canStart}
          onclick={() => void handleStart()}
          title={!validation
            ? 'Run Preview & validate first'
            : unresolved.length > 0
              ? 'Unresolved variables block the run'
              : validation.signingError
                ? 'Signing is unavailable — see above'
                : rowCount === 0
                  ? 'Pick a recipient source first'
                  : ''}
        >
          {#if startBusy}
            <Icon icon="mdi:loading" class="w-4 h-4 mr-1 animate-spin" />
          {/if}
          {deliverMode === 'send' ? 'Start sending' : deliverMode === 'later' ? 'Queue for later sending' : 'Generate drafts'}
        </Button>
      </div>
    </div>
  </Dialog.Content>
</Dialog.Root>
