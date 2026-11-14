<script lang="ts">
  // Load offline icon data before anything else
  import './lib/iconify-offline'

  import { onMount, onDestroy } from 'svelte'
  import Icon from '@iconify/svelte'
  import ConversationViewer from './lib/components/viewer/ConversationViewer.svelte'
  import ViewSwitcher from './lib/components/viewer/ViewSwitcher.svelte'
  import ToastContainer from './lib/components/ui/toast/ToastContainer.svelte'
  import { addToast } from '$lib/stores/toast'
  import { _ } from '$lib/i18n'
  import { initTheme, handleThemeChanged, type ThemeMode } from '$lib/stores/theme.svelte'
  import type { MessageOpenMode } from '$lib/stores/settings.svelte'
  // @ts-ignore - wailsjs imports
  import { GetViewMode, GetConversation, GetThemeMode, GetSystemTheme, CloseWindow, ConfirmClose, SwitchView, OpenComposerForReply, OpenComposerForAddress, OpenComposerForDraft, NotifyStartupComplete, RefreshWindowConstraints } from '../wailsjs/go/app/ViewerApp.js'
  // @ts-ignore - wailsjs imports
  import { app } from '../wailsjs/go/models'
  // @ts-ignore - wailsjs runtime
  import { WindowShow, WindowSetTitle, EventsOn, EventsOff } from '../wailsjs/runtime/runtime'

  // Thread info from backend
  let viewMode = $state<app.ViewMode | null>(null)
  let subject = $state('')
  let loading = $state(true)
  let error = $state<string | null>(null)

  // Close request state — the OS close button is vetoed by BeforeClose
  // until the frontend confirms; there is no unsaved state here, so a
  // vetoed close just confirms and closes immediately.
  async function handleCloseRequested() {
    await ConfirmClose()
    CloseWindow()
  }

  async function handleClose() {
    await ConfirmClose()
    CloseWindow()
  }

  // View switcher: ask the main window to open this thread in the chosen
  // mode; the backend closes this popup after sending the IPC message.
  async function handleSwitch(mode: MessageOpenMode) {
    if (mode === 'popup') return
    try {
      await SwitchView(mode)
    } catch (err) {
      console.error('Failed to switch view:', err)
      addToast({ type: 'error', message: String(err) })
    }
  }

  // Reply/forward open a detached composer window (the popup never
  // composes itself — the composer process owns the full send path).
  async function handleReply(mode: 'reply' | 'reply-all' | 'forward', messageId: string) {
    try {
      await OpenComposerForReply(mode, messageId)
    } catch (err) {
      console.error(`Failed to open composer for ${mode}:`, err)
      addToast({ type: 'error', message: $_('toast.failedToPrepare', { values: { mode } }) })
    }
  }

  async function handleComposeToAddress(toAddress: string) {
    try {
      await OpenComposerForAddress(toAddress)
    } catch (err) {
      console.error('Failed to open composer for address:', err)
      addToast({ type: 'error', message: String(err) })
    }
  }

  async function handleEditDraft(draftId: string) {
    try {
      await OpenComposerForDraft(draftId)
    } catch (err) {
      console.error('Failed to open composer for draft:', err)
      addToast({ type: 'error', message: $_('composer.failedToLoadDraft') })
    }
  }

  // A destructive action (archive/trash/spam/delete) dismisses the
  // conversation: the thread is gone from its folder, so close the popup.
  // Non-dismissing actions call onActionComplete with no argument.
  async function handleActionComplete(autoSelectNext?: boolean) {
    if (autoSelectNext) {
      await handleClose()
    }
  }

  onMount(async () => {
    // Load saved theme mode from backend and apply (probes XDG portal)
    try {
      const savedThemeMode = await GetThemeMode() as ThemeMode
      await initTheme(savedThemeMode, GetSystemTheme)
    } catch (err) {
      console.error('Failed to load theme mode:', err)
      await initTheme('system', GetSystemTheme)
    }

    // Show window after theme is applied (prevents white flash on startup)
    WindowShow()

    // Clear the desktop-environment startup indicator.
    NotifyStartupComplete()

    // Remove GTK max size constraints that Wails v2 sets at startup
    RefreshWindowConstraints()

    // Listen for theme changes from main window via IPC
    EventsOn('theme:changed', (newTheme: string) => {
      handleThemeChanged(newTheme)
    })

    // Listen for shutdown request from main window
    EventsOn('app:shutdown', (_reason: string) => {
      addToast({
        type: 'info',
        message: $_('toast.mainWindowClosing'),
      })
      setTimeout(async () => {
        await ConfirmClose()
        CloseWindow()
      }, 1000)
    })

    // Listen for a close vetoed by the backend (OS close button)
    EventsOn('viewer:close-requested', () => {
      void handleCloseRequested()
    })

    // Load the thread this popup was opened for, plus its subject for the
    // header and window title.
    try {
      viewMode = await GetViewMode()
      if (!viewMode) {
        error = $_('viewer.failedToLoad')
      } else {
        try {
          const conv = await GetConversation(viewMode.threadId, viewMode.folderId)
          const first = conv?.messages?.[0]
          subject = first?.subject?.trim() ? first.subject : $_('viewer.noSubject')
          WindowSetTitle(subject)
        } catch (err) {
          console.error('Failed to load subject for viewer popup:', err)
          subject = $_('viewer.noSubject')
        }
      }
    } catch (err) {
      console.error('Failed to initialize viewer popup:', err)
      error = String(err)
    } finally {
      loading = false
    }
  })

  onDestroy(() => {
    EventsOff('theme:changed')
    EventsOff('app:shutdown')
    EventsOff('viewer:close-requested')
  })
</script>

<div class="h-screen flex flex-col bg-background text-foreground">
  <!-- Popup header: subject + view switcher + close -->
  <header class="flex flex-shrink-0 items-center gap-2 border-b border-border bg-muted/30 px-3 py-2">
    <h1 class="min-w-0 flex-1 truncate text-sm font-medium" title={subject}>
      {subject || $_('viewer.noSubject')}
    </h1>
    <ViewSwitcher current="popup" onSwitch={handleSwitch} />
    <button
      type="button"
      class="flex-shrink-0 rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
      aria-label={$_('window.closeWindow')}
      title={$_('window.closeWindow')}
      onclick={handleClose}
    >
      <Icon icon="mdi:close" class="w-4 h-4" />
    </button>
  </header>

  <!-- Main content -->
  <main class="flex-1 min-h-0 overflow-hidden">
    {#if loading}
      <div class="h-full flex items-center justify-center">
        <div class="flex flex-col items-center gap-3">
          <Icon icon="mdi:loading" class="w-8 h-8 animate-spin text-primary" />
          <span class="text-sm text-muted-foreground">{$_('common.loading')}</span>
        </div>
      </div>
    {:else if error}
      <div class="h-full flex items-center justify-center">
        <div class="flex flex-col items-center gap-3 text-center px-4">
          <Icon icon="mdi:alert-circle" class="w-12 h-12 text-destructive" />
          <p class="text-sm text-destructive">{error}</p>
          <button
            onclick={handleClose}
            class="px-4 py-2 text-sm bg-muted hover:bg-muted/80 rounded-md transition-colors"
          >
            {$_('window.closeWindow')}
          </button>
        </div>
      </div>
    {:else if viewMode}
      <ConversationViewer
        threadId={viewMode.threadId}
        folderId={viewMode.folderId}
        folderType={viewMode.folderType}
        accountId={viewMode.accountId}
        onReply={handleReply}
        onComposeToAddress={handleComposeToAddress}
        onEditDraft={handleEditDraft}
        onActionComplete={handleActionComplete}
        showBackButton={false}
      />
    {/if}
  </main>
</div>

<!-- Toast notifications -->
<ToastContainer />
