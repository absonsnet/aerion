import { writable } from 'svelte/store'

// One live Undo Send countdown: a message queued for background
// send, ticking down to dispatch. The countdown is the attention
// device — the user sees the seconds drain and hits Undo before
// the message goes out. When it hits zero the worker sends the
// message and the row disappears from here (the sent snackbar
// arrives separately via the sendqueue:sent event).
export interface UndoSendCountdown {
  queueId: string
  accountId: string
  draftId: string
  subject: string
  recipient: string
  totalSeconds: number
  remainingSeconds: number
  sendAt: number // epoch ms, for the progress bar
}

function createUndoSendStore() {
  const { subscribe, update } = writable<UndoSendCountdown[]>([])
  let ticker: ReturnType<typeof setInterval> | null = null

  function ensureTicker() {
    if (ticker) return
    // 200ms ticks: smooth progress-bar drain and a per-second
    // countdown label that visibly ticks.
    ticker = setInterval(() => {
      const now = Date.now()
      update((items) => {
        const next = items
          .map((c) => ({ ...c, remainingSeconds: Math.max(0, (c.sendAt - now) / 1000) }))
          .filter((c) => c.remainingSeconds > 0)
        if (next.length === 0 && ticker) {
          clearInterval(ticker)
          ticker = null
        }
        return next
      })
    }, 200)
  }

  function start(input: {
    queueId: string
    accountId: string
    draftId: string
    subject: string
    recipient: string
    delaySeconds: number
  }) {
    const totalSeconds = Math.max(1, input.delaySeconds)
    const sendAt = Date.now() + totalSeconds * 1000
    update((items) => [
      ...items.filter((c) => c.queueId !== input.queueId),
      {
        queueId: input.queueId,
        accountId: input.accountId,
        draftId: input.draftId,
        subject: input.subject,
        recipient: input.recipient,
        totalSeconds,
        remainingSeconds: totalSeconds,
        sendAt,
      },
    ])
    ensureTicker()
  }

  function remove(queueId: string) {
    update((items) => items.filter((c) => c.queueId !== queueId))
  }

  function clear() {
    update(() => [])
    if (ticker) {
      clearInterval(ticker)
      ticker = null
    }
  }

  return { subscribe, start, remove, clear }
}

export const undoSendCountdowns = createUndoSendStore()

// Start (or restart) the countdown for one queued send.
export function startUndoSendCountdown(input: {
  queueId: string
  accountId: string
  draftId: string
  subject: string
  recipient: string
  delaySeconds: number
}) {
  undoSendCountdowns.start(input)
}

// Drop a countdown (undone, or expired and sent).
export function removeUndoSendCountdown(queueId: string) {
  undoSendCountdowns.remove(queueId)
}
