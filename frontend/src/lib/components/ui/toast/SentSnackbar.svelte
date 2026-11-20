<script lang="ts">
  import Icon from '@iconify/svelte'
  import { _ } from '$lib/i18n'

  interface Props {
    visible: boolean
    subject: string
    onView: () => void
    onClose: () => void
  }

  let { visible, subject, onView, onClose }: Props = $props()
</script>

<!-- Slim bottom snackbar, NOT the usual toast: a single dark strip,
  bottom-centre, no icon pile, no countdown. It only confirms the
  send already happened — with a View message action. -->
{#if visible}
  <div
    class="fixed bottom-4 left-1/2 -translate-x-1/2 z-50 pointer-events-none"
    role="status"
  >
    <div
      class="pointer-events-auto flex items-center gap-3 pl-4 pr-2 py-2 rounded-full bg-foreground text-background shadow-xl animate-snackbar-in"
    >
      <Icon icon="mdi:check" class="w-4 h-4 flex-shrink-0" />
      <span class="text-sm max-w-64 truncate">
        {$_('undoSend.sentMessage', { values: { subject: subject || $_('messageList.noContent') } })}
      </span>
      <button
        class="flex-shrink-0 px-2.5 py-1 text-xs font-semibold rounded-full bg-background/15 hover:bg-background/25 transition-colors"
        onclick={onView}
      >
        {$_('undoSend.viewMessage')}
      </button>
      <button
        class="flex-shrink-0 p-1 rounded-full hover:bg-background/15 transition-colors"
        onclick={onClose}
        aria-label={$_('common.close')}
      >
        <Icon icon="mdi:close" class="w-4 h-4" />
      </button>
    </div>
  </div>
{/if}

<style>
  @keyframes snackbar-in {
    from {
      transform: translateY(100%);
      opacity: 0;
    }
    to {
      transform: translateY(0);
      opacity: 1;
    }
  }

  .animate-snackbar-in {
    animation: snackbar-in 0.25s ease-out;
  }
</style>
