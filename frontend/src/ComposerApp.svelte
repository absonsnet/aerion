<script lang="ts">
  // Load offline icon data before anything else
  import './lib/iconify-offline'

  import { onMount, onDestroy } from 'svelte'
  import Icon from '@iconify/svelte'
  import Composer from './lib/components/composer/Composer.svelte'
  import ToastContainer from './lib/components/ui/toast/ToastContainer.svelte'
  import SpellSuggestionMenu from './lib/spellcheck/SpellSuggestionMenu.svelte'
  import { addToast } from '$lib/stores/toast'
  import { _ } from '$lib/i18n'
  import { createComposerWindowApi } from '$lib/composerApi'
  import { setDarkComposerBody } from '$lib/stores/settings.svelte'
  import { initTheme, handleThemeChanged, type ThemeMode } from '$lib/stores/theme.svelte'
  // @ts-ignore - wailsjs imports
  import { GetComposeMode, PrepareReply, GetDraft, CloseWindow, ConfirmClose, GetThemeMode, GetSystemTheme, GetDarkComposerBody, RefreshWindowConstraints, NotifyStartupComplete } from '../wailsjs/go/app/ComposerApp.js'
  // @ts-ignore - wailsjs imports
  import { smtp, app } from '../wailsjs/go/models'
  // @ts-ignore - wailsjs runtime
  import { WindowShow, WindowSetTitle, EventsOn, EventsOff } from '../wailsjs/runtime/runtime'

  // Compose mode info from backend
  let composeMode = $state<app.ComposeMode | null>(null)
  let initialMessage = $state<smtp.ComposeMessage | null>(null)
  let loading = $state(true)
  let error = $state<string | null>(null)

  // Close request state - triggers Composer's close dialog
  let closeRequested = $state(false)

  // Dynamic title parts from Composer
  let titleTo = $state('')
  let titleSubject = $state('')

  // Window title based on mode + dynamic recipient/subject
  let windowTitle = $derived(() => {
    if (!composeMode) return $_('sidebar.compose')
    let base: string
    switch (composeMode.mode) {
      case 'reply': base = $_('composer.reply'); break
      case 'reply-all': base = $_('composer.replyAll'); break
      case 'forward': base = $_('composer.forward'); break
      default: base = composeMode.draftId ? $_('composer.editDraft') : $_('composer.newMessage')
    }
    if (!titleTo && !titleSubject) return base
    const detail = titleTo && titleSubject
      ? `${titleTo} | ${titleSubject}`
      : titleTo || titleSubject
    return `${base} — ${detail}`
  })

  function handleTitleChange(to: string, subject: string) {
    titleTo = to
    titleSubject = subject
    WindowSetTitle(windowTitle())
  }

  onMount(async () => {
    // Load the dark-composer-body preference (the composer window always
    // uses the native title bar now, so there are no title bar settings
    // for it to load)
    try {
      const dcb = await GetDarkComposerBody()
      setDarkComposerBody(dcb ?? false)
    } catch (err) {
      console.error('Failed to load dark composer body setting:', err)
    }

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

    // Clear the desktop-environment startup indicator. Called after WindowShow()
    // so KDE/Plasma sees the placeholder → real window handoff cleanly (#154).
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
      // Give user a moment to see the toast, then close
      setTimeout(async () => {
        await ConfirmClose()
        CloseWindow()
      }, 1000)
    })

    // Listen for a close vetoed by the backend (OS close button): show the
    // discard/save confirmation instead of closing immediately
    EventsOn('composer:close-requested', () => {
      closeRequested = true
    })

    // Load compose mode and initial data
    try {
      composeMode = await GetComposeMode()

      // If editing a draft, load it
      if (composeMode?.draftId) {
        const draft = await GetDraft()
        if (draft) {
          initialMessage = draft
        }
      }
      // If replying/forwarding, prepare the message
      else if (composeMode?.mode !== 'new' && composeMode?.messageId) {
        const prepared = await PrepareReply()
        if (prepared) {
          initialMessage = prepared
        }
      }
      // For new message, PrepareReply returns a message with just the From address
      else {
        const prepared = await PrepareReply()
        if (prepared) {
          initialMessage = prepared
        }
      }
    } catch (err) {
      console.error('Failed to initialize composer:', err)
      error = String(err)
    } finally {
      loading = false
    }
  })

  onDestroy(() => {
    EventsOff('theme:changed')
    EventsOff('app:shutdown')
    EventsOff('composer:close-requested')
  })

  // Called when Composer has handled the close request (user made a choice)
  function handleCloseHandled() {
    closeRequested = false
  }

  // Handle composer close (after send or discard confirmation). Confirm first
  // so BeforeClose lets the programmatic close through (it vetoes the OS
  // close button until the user has chosen).
  async function handleComposerClose() {
    await ConfirmClose()
    CloseWindow()
  }

  // Handle message sent
  function handleMessageSent() {
    // The Composer component shows its own toast
    // Close the window after a brief delay
    setTimeout(async () => {
      await ConfirmClose()
      CloseWindow()
    }, 500)
  }
</script>

<div class="h-screen flex flex-col bg-background text-foreground">
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
            onclick={async () => { await ConfirmClose(); CloseWindow() }}
            class="px-4 py-2 text-sm bg-muted hover:bg-muted/80 rounded-md transition-colors"
          >
            {$_('window.closeWindow')}
          </button>
        </div>
      </div>
    {:else if composeMode}
      <Composer
        accountId={composeMode.accountId}
        initialMessage={initialMessage}
        draftId={composeMode.draftId || null}
        messageId={composeMode.messageId || null}
        onClose={handleComposerClose}
        onSent={handleMessageSent}
        api={createComposerWindowApi(composeMode.accountId)}
        isDetached={true}
        closeRequested={closeRequested}
        onCloseHandled={handleCloseHandled}
        onTitleChange={handleTitleChange}
      />
    {/if}
  </main>
</div>

<!-- Toast notifications -->
<ToastContainer />

<!-- Spellcheck right-click suggestion menu -->
<SpellSuggestionMenu />
