<script lang="ts">
  import { undoSendCountdowns, type UndoSendCountdown } from '$lib/stores/undoSend'
  import { _ } from '$lib/i18n'

  interface Props {
    onUndo: (countdown: UndoSendCountdown) => void
  }

  let { onUndo }: Props = $props()

  // Distinct from the usual toast on purpose: solid primary surface
  // (not a tinted card), bottom-centre (not the bottom-right toast
  // stack), a draining progress bar and a big ticking number. This is
  // the one that matters — it has to read as different at a glance.
  function progressPct(c: UndoSendCountdown): number {
    if (c.totalSeconds <= 0) return 0
    return Math.max(0, Math.min(100, (c.remainingSeconds / c.totalSeconds) * 100))
  }
</script>

{#if $undoSendCountdowns.length > 0}
  <div
    class="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 w-[calc(100%-2rem)] max-w-md pointer-events-none"
    role="alert"
    aria-live="assertive"
  >
    {#each $undoSendCountdowns as countdown (countdown.queueId)}
      <div
        class="pointer-events-auto overflow-hidden rounded-xl bg-primary text-primary-foreground shadow-xl animate-undo-send-in"
      >
        <div class="flex items-center gap-3 px-4 pt-3 pb-2">
          <span class="flex-shrink-0 flex items-center justify-center w-9 h-9 rounded-full bg-primary-foreground/15 font-semibold tabular-nums">
            {Math.ceil(countdown.remainingSeconds)}
          </span>
          <div class="flex-1 min-w-0">
            <p class="text-sm font-medium truncate">
              {$_('undoSend.sendingIn', { values: { seconds: Math.ceil(countdown.remainingSeconds) } })}
            </p>
            <p class="text-xs opacity-80 truncate">
              {countdown.subject || $_('messageList.noContent')} — {countdown.recipient}
            </p>
          </div>
          <button
            class="flex-shrink-0 px-3 py-1.5 text-sm font-semibold rounded-lg bg-primary-foreground text-primary hover:opacity-90 transition-opacity"
            onclick={() => onUndo(countdown)}
          >
            {$_('common.undo')}
          </button>
        </div>
        <!-- Draining progress bar: the remaining time, at a glance. -->
        <div
          class="h-1 bg-primary-foreground/40 transition-[width] duration-200 ease-linear"
          style="width: {progressPct(countdown)}%"
        ></div>
      </div>
    {/each}
  </div>
{/if}

<style>
  @keyframes undo-send-in {
    from {
      transform: translateY(100%);
      opacity: 0;
    }
    to {
      transform: translateY(0);
      opacity: 1;
    }
  }

  .animate-undo-send-in {
    animation: undo-send-in 0.25s ease-out;
  }
</style>
