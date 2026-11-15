<script lang="ts">
  import Icon from '@iconify/svelte'
  import { _ } from '$lib/i18n'
  import { APP_NAME } from '$lib/config/brand'
  import { WindowMinimise, WindowToggleMaximise, Quit } from '../../../../wailsjs/runtime/runtime'
  import { getEffectiveWindowControls, type WindowControlButton } from '$lib/stores/windowControls.svelte'

  interface Props {
    onClose?: () => void
  }

  let { onClose }: Props = $props()

  let isMaximized = $state(false)
  let isHovering = $state(false)

  // Reactive: re-computed when the detected layout loads or when the
  // Settings override (Auto / Left / Right) changes, so the buttons move
  // without an app restart.
  let layout = $derived(getEffectiveWindowControls())

  async function minimize() {
    await WindowMinimise()
  }

  async function toggleMaximize() {
    await WindowToggleMaximise()
    isMaximized = !isMaximized
  }

  function close() {
    if (onClose) {
      onClose()
    } else {
      // For windows without custom onClose (e.g., composer), just quit directly
      Quit()
    }
  }

  function activate(button: WindowControlButton) {
    if (button === 'minimize') {
      void minimize()
    } else if (button === 'maximize') {
      void toggleMaximize()
    } else {
      close()
    }
  }

  function buttonTitle(button: WindowControlButton): string {
    if (button === 'minimize') return $_('window.minimize')
    if (button === 'close') return $_('window.close')
    return isMaximized ? $_('window.restore') : $_('window.maximize')
  }

  function buttonAriaLabel(button: WindowControlButton): string {
    if (button === 'minimize') return $_('aria.minimizeWindow')
    if (button === 'close') return $_('aria.closeWindow')
    return isMaximized ? $_('aria.restoreWindow') : $_('aria.maximizeWindow')
  }

  // Traffic-light circle colour and hover symbol per button.
  const trafficMeta: Record<WindowControlButton, { bg: string; symbol: string }> = {
    minimize: { bg: 'bg-[#FEBC2E]', symbol: '−' },
    maximize: { bg: 'bg-[#28C840]', symbol: '+' },
    close: { bg: 'bg-[#FF5F57]', symbol: '×' },
  }
</script>

{#snippet dragRegion()}
  <!-- Drag region with app title -->
  <div class="flex-1 flex items-center gap-2 px-3 h-full" style="--wails-draggable: drag">
    <Icon icon="mdi:email-fast-outline" class="w-5 h-5 text-primary" />
    <span class="text-sm font-medium text-foreground">{APP_NAME}</span>
  </div>
{/snippet}

{#snippet controls()}
  {#if layout.style === 'glyphs'}
    <!-- Windows-style glyph buttons (─ ▢ ✕), flush to the window edge -->
    <div
      class="flex items-stretch h-full"
      role="group"
      aria-label={$_('aria.windowControls')}
    >
      {#each layout.order as button (button)}
        <button
          class="w-[46px] h-full flex items-center justify-center transition-colors text-foreground {button === 'close' ? 'hover:bg-[#E81123] hover:text-white' : 'hover:bg-foreground/10'}"
          onclick={() => activate(button)}
          title={buttonTitle(button)}
          aria-label={buttonAriaLabel(button)}
        >
          {#if button === 'minimize'}
            <svg class="w-2.5 h-2.5" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="1.2" aria-hidden="true">
              <line x1="1" y1="5" x2="9" y2="5" />
            </svg>
          {:else if button === 'maximize'}
            <svg class="w-2.5 h-2.5" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="1.2" aria-hidden="true">
              <rect x="1.2" y="1.2" width="7.6" height="7.6" />
            </svg>
          {:else}
            <svg class="w-2.5 h-2.5" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="1.2" aria-hidden="true">
              <line x1="1.5" y1="1.5" x2="8.5" y2="8.5" />
              <line x1="8.5" y1="1.5" x2="1.5" y2="8.5" />
            </svg>
          {/if}
        </button>
      {/each}
    </div>
  {:else}
    <!-- macOS-style traffic light controls -->
    <div
      class="flex items-center gap-2 px-3 h-full"
      role="group"
      aria-label={$_('aria.windowControls')}
      onmouseenter={() => isHovering = true}
      onmouseleave={() => isHovering = false}
    >
      {#each layout.order as button (button)}
        <button
          class="w-3 h-3 rounded-full flex items-center justify-center transition-all {trafficMeta[button].bg} hover:brightness-90 active:brightness-75"
          onclick={() => activate(button)}
          title={buttonTitle(button)}
          aria-label={buttonAriaLabel(button)}
        >
          {#if isHovering}
            <span class="text-[10px] font-bold text-black/60 leading-none">{trafficMeta[button].symbol}</span>
          {/if}
        </button>
      {/each}
    </div>
  {/if}
{/snippet}

<header class="h-10 flex items-center justify-between bg-muted/50 border-b border-border select-none shrink-0">
  {#if layout.side === 'left'}
    {@render controls()}
    {@render dragRegion()}
  {:else}
    {@render dragRegion()}
    {@render controls()}
  {/if}
</header>
