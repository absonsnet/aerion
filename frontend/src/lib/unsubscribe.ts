// Shared unsubscribe click flow (Phase C, Gmail behaviour), used by
// the message header button and the list-row context menu so the two
// surfaces cannot drift.
//
// The click itself is the consent: one-click fires the silent POST at
// once, with no confirm dialog. The follow-up extras (move existing
// mail to Trash, auto-delete future mail) are offered as toast actions
// afterwards — each is created only when tapped. For spam mail the
// Block sender action (both extras at once) comes first, because
// unsubscribing from genuine spam can confirm the address is live.
import { get } from 'svelte/store'
import { _ } from '$lib/i18n'
import { toasts } from '$lib/stores/toast'
import {
  UnsubscribeFromMessage,
  UnsubscribeFollowUp,
  OpenURL,
} from '$lib/viewerBindings'

export interface UnsubscribeFlowOptions {
  /** Called when the unsubscribe completes in Daak (flip the button to "Unsubscribed"). */
  onUnsubscribed?: () => void
  /** Called after a follow-up moves the sender's mail (reload the list/viewer). */
  onMoved?: () => void
}

function t(key: string, values?: Record<string, string | number>): string {
  const translate = get(_)
  return values ? translate(key, { values }) : translate(key)
}

async function handleFollowUp(
  messageId: string,
  moveExisting: boolean,
  autoDelete: boolean,
  senderName: string,
  opts: UnsubscribeFlowOptions
) {
  try {
    await UnsubscribeFollowUp(messageId, moveExisting, autoDelete)
    if (moveExisting && autoDelete) {
      toasts.success(t('viewer.unsubscribeBlockedSender', { sender: senderName }))
    } else if (moveExisting) {
      toasts.success(t('viewer.unsubscribeMovedExisting', { sender: senderName }))
    } else {
      toasts.success(t('viewer.unsubscribeAutoDeleteOn', { sender: senderName }))
    }
    if (moveExisting) opts.onMoved?.()
  } catch (err) {
    console.error('Unsubscribe follow-up failed:', err)
    toasts.error(t('viewer.unsubscribeFollowUpFailed'))
  }
}

/** Runs the full click flow for one message. Callers check the backend
 *  status (available, not already done) before calling. */
export async function runUnsubscribeFlow(
  messageId: string,
  opts: UnsubscribeFlowOptions = {}
): Promise<void> {
  try {
    const result = await UnsubscribeFromMessage(messageId)
    const senderName = result.senderName || result.senderEmail
    if (result.method === 'already') {
      opts.onUnsubscribed?.()
      toasts.info(t('viewer.alreadyUnsubscribed', { sender: senderName }))
      return
    }
    if (result.method === 'browser') {
      // The sender's page completes it — not done in Daak.
      toasts.info(t('viewer.unsubscribeOpenedInBrowser', { domain: result.domain }))
      if (result.url) {
        try {
          await OpenURL(result.url)
        } catch (err) {
          console.error('Failed to open unsubscribe URL:', err)
          toasts.error(t('viewer.unsubscribeOpenFailed'))
        }
      }
      return
    }
    // Completed in Daak (one-click POST sent, or unsubscribe email sent).
    opts.onUnsubscribed?.()
    const doneKey =
      result.method === 'mailto' ? 'viewer.unsubscribeEmailSent' : 'viewer.unsubscribedFrom'
    const mismatchNote = result.domainMismatch
      ? ' ' + t('viewer.unsubscribeDomainMismatch', { domain: result.domain })
      : ''
    const actions: Array<{ label: string; onClick: () => void }> = []
    if (result.isSpam) {
      actions.push({
        label: t('viewer.unsubscribeBlockSender'),
        onClick: () => handleFollowUp(messageId, true, true, senderName, opts),
      })
    }
    actions.push(
      {
        label: t('viewer.unsubscribeMoveExisting'),
        onClick: () => handleFollowUp(messageId, true, false, senderName, opts),
      },
      {
        label: t('viewer.unsubscribeAutoDelete'),
        onClick: () => handleFollowUp(messageId, false, true, senderName, opts),
      }
    )
    const message = t(doneKey, { sender: senderName }) + mismatchNote
    if (result.domainMismatch) {
      toasts.warning(message, actions)
    } else {
      toasts.success(message, actions)
    }
  } catch (err) {
    console.error('Unsubscribe failed:', err)
    toasts.error(
      `${t('viewer.unsubscribeFailed')}: ${err instanceof Error ? err.message : String(err)}`
    )
  }
}
