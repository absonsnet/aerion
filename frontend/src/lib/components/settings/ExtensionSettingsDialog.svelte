<script lang="ts">
  // ExtensionSettingsDialog — host-level dispatcher for per-extension settings
  // dialogs. Watches extensionRegistry's openSettingsExtension state and
  // renders the matching extension's dialog component.
  //
  // Static dispatch by extension ID (same pattern as AccountDialog's
  // account-setup hook dispatch). When a new first-party extension lands,
  // add its case to the {#if} chain. Community extensions (v0.4+) will use
  // a different mechanism since their Svelte components aren't compiled
  // into Aerion's binary.
  //
  // Mount once at App.svelte level so the dialog can be opened from anywhere.

  import { getOpenSettingsExtension, closeExtensionSettings } from '$lib/stores/extensionRegistry.svelte'
  import ContactsSettingsDialog from '$extensions/contacts/frontend/components/ContactsSettingsDialog.svelte'
  import CalendarSettingsDialog from '$extensions/calendar/frontend/components/CalendarSettingsDialog.svelte'
  import AIWritingSettingsDialog from '$extensions/ai-writing/frontend/components/AIWritingSettingsDialog.svelte'
  import MailMergeSettingsDialog from '$extensions/mail-merge/frontend/components/MailMergeSettingsDialog.svelte'
  // This dispatcher is mounted only in App.svelte (main window), so the
  // settings dialog gets the main-window API here. The detached composer
  // window's button passes its own (ComposerApp) API instead.
  import { createMainWindowApi } from '$lib/composerApi'

  const mainWindowApi = createMainWindowApi()

  let openExtension = $derived(getOpenSettingsExtension())

  // Each per-extension dialog binds its `open` prop to a derived true/false
  // based on the open state. Closing the dialog calls closeExtensionSettings.
  let contactsOpen = $derived(openExtension === 'contacts')
  let calendarOpen = $derived(openExtension === 'calendar')
  let aiWritingOpen = $derived(openExtension === 'ai-writing')
  let mailMergeOpen = $derived(openExtension === 'mail-merge')
</script>

<ContactsSettingsDialog
  open={contactsOpen}
  onClose={closeExtensionSettings}
/>

<CalendarSettingsDialog
  open={calendarOpen}
  onClose={closeExtensionSettings}
/>

<AIWritingSettingsDialog
  open={aiWritingOpen}
  api={mainWindowApi}
  onClose={closeExtensionSettings}
/>

<MailMergeSettingsDialog
  open={mailMergeOpen}
  api={mainWindowApi}
  onClose={closeExtensionSettings}
/>
