// Shared code/reference chip action (Phase A).
//
// Used by the chip in the message-list row and the chip at the top of the
// open message viewer. Reference chips copy only; code chips copy, then
// run the backend half (mark read / move to Trash) per the
// "Code chip click" setting, with a 10s Undo toast when a message was
// moved to Trash. The toast never claims the clipboard is restored —
// Undo moves the message back and restores the unread flag; the code
// stays copied. In a popup viewer window there is no undo stack, so the
// toast offers no Undo there.
//
// The mode and clipboard delay are read live from the backend at click
// time (viewerBindings dispatches to App or ViewerApp), so the chip
// honours a changed setting even in a popup window, where the frontend
// settings store is not loaded.
//
// Clipboard hygiene: when otp_clipboard_clear_seconds is > 0, the code
// is cleared after that delay — but only if the clipboard still holds
// exactly that code, so something copied since is never clobbered.
// Clipboard access is best-effort (readText needs focus in some
// webviews); failures are swallowed, never surfaced.
import { ApplyCodeChipAction, Undo, GetCodeChipClickMode, GetOtpClipboardClearSeconds, isViewerWindow } from '$lib/viewerBindings'
import { toasts } from '$lib/stores/toast'
import { _ } from 'svelte-i18n'
import { get } from 'svelte/store'
// @ts-ignore - wailsjs path
import { ClipboardGetText, ClipboardSetText } from '../../../wailsjs/runtime/runtime'

let clipboardClearTimer: ReturnType<typeof setTimeout> | undefined

// Native Wails clipboard first (no web-permission gate, so the read-back
// in the hygiene timer works in the webview); navigator.clipboard as a
// fallback. ClipboardSetText resolves false on failure.
async function copyText(text: string): Promise<void> {
  try {
    if (await ClipboardSetText(text)) return
  } catch {
    // Fall through to the web API.
  }
  await navigator.clipboard.writeText(text)
}

async function readClipboardText(): Promise<string> {
  try {
    return await ClipboardGetText()
  } catch {
    return await navigator.clipboard.readText()
  }
}

async function clearClipboardText(): Promise<void> {
  try {
    if (await ClipboardSetText('')) return
  } catch {
    // Fall through to the web API.
  }
  await navigator.clipboard.writeText('')
}

function scheduleClipboardClear(code: string, delaySeconds: number) {
  if (clipboardClearTimer) clearTimeout(clipboardClearTimer)
  if (delaySeconds <= 0) return
  clipboardClearTimer = setTimeout(async () => {
    try {
      if ((await readClipboardText()) === code) {
        await clearClipboardText()
      }
    } catch {
      // Best-effort only: clipboard read may be blocked without focus.
    }
  }, delaySeconds * 1000)
}

export interface CodeChipActionOptions {
  messageId: string
  kind: 'code' | 'reference'
  value: string
  /** Called after the action completes so the list/viewer can refresh. */
  onDone?: () => void
}

export async function handleCodeChipClick(opts: CodeChipActionOptions): Promise<void> {
  const { messageId, kind, value } = opts
  const t = get(_)

  try {
    await copyText(value)
  } catch (err) {
    console.error('Failed to copy code:', err)
    toasts.error(t('toast.failedToCopy'))
    return
  }

  if (kind === 'reference') {
    toasts.success(t('chip.referenceCopied', { values: { value } }))
    opts.onDone?.()
    return
  }

  // Code chip: clipboard hygiene first, then the backend action.
  try {
    scheduleClipboardClear(value, await GetOtpClipboardClearSeconds())
  } catch {
    // Clipboard hygiene is best-effort; a failed read must not block
    // the copy that already happened.
  }

  let mode = 'copy_mark_read_delete'
  try {
    mode = await GetCodeChipClickMode()
  } catch (err) {
    console.error('Failed to read code chip mode, using default:', err)
  }

  let moved = false
  try {
    moved = await ApplyCodeChipAction(messageId, mode)
  } catch (err) {
    console.error('Code chip action failed:', err)
    toasts.error(t('toast.failedToMove'))
    return
  }

  if (mode === 'copy_mark_read_delete' && moved && !isViewerWindow()) {
    toasts.add({
      message: t('chip.codeCopiedMessageDeleted', { values: { value } }),
      type: 'success',
      duration: 10000,
      actions: [
        {
          label: t('common.undo'),
          onClick: async () => {
            try {
              const description = await Undo()
              toasts.success(t('toast.undone', { values: { description } }))
            } catch (err) {
              console.error('Undo failed:', err)
              toasts.error(t('toast.undoFailed'))
            }
            opts.onDone?.()
          }
        }
      ]
    })
  } else if (mode === 'copy_mark_read_delete' && moved) {
    // Popup window: no undo stack, so the toast offers no Undo.
    toasts.success(t('chip.codeCopiedMessageDeleted', { values: { value } }))
  } else {
    toasts.success(t('chip.codeCopied', { values: { value } }))
  }
  opts.onDone?.()
}
