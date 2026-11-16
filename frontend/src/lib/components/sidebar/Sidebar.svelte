<script lang="ts">
  import Icon from '@iconify/svelte'
  import { onMount } from 'svelte'
  import AccountSection from './AccountSection.svelte'
  import UnifiedSection, { UNIFIED_FOLDER_DEFS } from './UnifiedSection.svelte'
  import AccountDialog from '$lib/components/settings/AccountDialog.svelte'
  import DeleteAccountDialog from '$lib/components/settings/DeleteAccountDialog.svelte'
  import SettingsDialog from '$lib/components/settings/SettingsDialog.svelte'
  import SidebarFooter from '$lib/components/kit/SidebarFooter.svelte'
  import { Button } from '$lib/components/ui/button'
  import { accountStore } from '$lib/stores/accounts.svelte'
  import { contactSourcesStore } from '$lib/stores/contactSources.svelte'
  import { setFolderCollapsed, getUIState, getUIStateVersion, saveUIState } from '$lib/stores/uiState.svelte'
  import { loadSidebarSettings, hasStoredExpandedAccounts, getExpandedAccountIds, setExpandedAccountIds, setSidebarAccountExpanded, getHideUnifiedSingleAccount, getUnifiedFolderOrder, setUnifiedFolderOrder } from '$lib/stores/sidebarSettings.svelte'
  import { setFocusedPane } from '$lib/stores/keyboard.svelte'
  import { _ } from '$lib/i18n'
  // @ts-ignore - wailsjs path
  import { account, folder } from '../../../../wailsjs/go/models'
  // @ts-ignore - wailsjs path
  import { GetUnifiedFolderUnreadCounts, GetAttentionCount, MailMerge_GetQueueStats } from '../../../../wailsjs/go/app/App'
  import { formatDistanceToNow } from 'date-fns'
  import { getCurrentDateFnsLocale } from '$lib/stores/settings.svelte'
  import { EventsOn, EventsEmit } from '../../../../wailsjs/runtime/runtime'

  // Folder item type for flat navigation list
  interface FolderNavItem {
    type: 'unified-folder' | 'attention' | 'account-header' | 'folder'
    accountId?: string
    folderId?: string
    folderPath?: string
    folderName: string
    folderType?: string
  }

  // Track focused account header for keyboard navigation
  let focusedAccountId = $state<string | null>(null)

  // Ref to scrollable container for auto-scroll
  let scrollContainer: HTMLDivElement | null = null

  // Track expanded state for each account (reactive, synced with persisted
  // state in the sidebar_expanded_accounts setting)
  let expandedAccounts = $state<Record<string, boolean>>({})

  // Initialize expanded state from the persisted setting.
  // First run after the upgrade (key never stored): first account expanded,
  // rest collapsed. Afterwards the stored set rules — expanding several
  // accounts is allowed, there is no accordion.
  $effect(() => {
    // Read version to create dependency - effect re-runs when UI state finishes loading
    const _version = getUIStateVersion()

    const ids = accountStore.accounts.map((acc) => acc.account.id)
    if (ids.length === 0) {
      expandedAccounts = {}
      return
    }

    if (!hasStoredExpandedAccounts()) {
      setExpandedAccountIds([ids[0]])
    } else {
      // Prune IDs of accounts that no longer exist
      const stored = getExpandedAccountIds()
      const pruned = stored.filter((id) => ids.includes(id))
      if (pruned.length !== stored.length) setExpandedAccountIds(pruned)
    }

    const storedSet = new Set(getExpandedAccountIds())
    const newExpanded: Record<string, boolean> = {}
    for (const id of ids) {
      newExpanded[id] = storedSet.has(id)
    }
    expandedAccounts = newExpanded
  })

  // Set account expansion (local state + persisted setting)
  function setAccountExpandedValue(accountId: string, expanded: boolean) {
    expandedAccounts = { ...expandedAccounts, [accountId]: expanded }
    setSidebarAccountExpanded(accountId, expanded)
  }

  // Toggle account expansion
  function toggleAccountExpanded(accountId: string) {
    setAccountExpandedValue(accountId, !expandedAccounts[accountId])
  }

  // Track collapsed state for folders with children (reactive, synced with persisted state)
  let collapsedFolders = $state<Record<string, boolean>>({})

  // Initialize collapsed state from persisted storage and prune stale entries
  $effect(() => {
    const _version = getUIStateVersion()

    // Collect all folder IDs that exist in current accounts
    const allFolderIds = new Set<string>()
    const collectIds = (trees: folder.FolderTree[]) => {
      for (const tree of trees) {
        if (tree.folder) allFolderIds.add(tree.folder.id)
        if (tree.children) collectIds(tree.children)
      }
    }
    for (const acc of accountStore.accounts) {
      collectIds(acc.folders || [])
    }

    // Read persisted collapsed state, keep only entries for existing folders
    const persisted = getUIState().collapsedFolders
    const newCollapsed: Record<string, boolean> = {}
    let hasStale = false
    for (const folderId of Object.keys(persisted)) {
      if (allFolderIds.has(folderId)) {
        newCollapsed[folderId] = persisted[folderId]
      } else {
        hasStale = true
      }
    }
    collapsedFolders = newCollapsed

    // Persist cleaned state if stale entries were pruned
    if (hasStale) {
      saveUIState({ collapsedFolders: newCollapsed })
    }
  })

  // Toggle folder collapse
  function toggleFolderCollapsed(folderId: string) {
    const isCurrentlyCollapsed = collapsedFolders[folderId] !== false
    const newValue = !isCurrentlyCollapsed
    setFolderCollapsedValue(folderId, newValue)
  }

  // Set folder collapse explicitly (keyboard expand/collapse)
  function setFolderCollapsedValue(folderId: string, collapsed: boolean) {
    collapsedFolders = { ...collapsedFolders, [folderId]: collapsed }
    setFolderCollapsed(folderId, collapsed)
  }

  interface Props {
    onFolderSelect?: (accountId: string, folderId: string, folderPath: string, folderName: string, folderType: string) => void
    onUnifiedFolderSelect?: (folderType: string, label: string) => void
    onUnifiedInboxSelect?: () => void
    onCompose?: () => void
    onMessagesMoved?: () => void
    selectedAccountId?: string | null
    selectedFolderId?: string | null
    selectionSource?: 'unified' | 'account' | null
    isFocused?: boolean
    isFlashing?: boolean
    showBackButton?: boolean
    onBack?: () => void
  }

  let {
    onFolderSelect,
    onUnifiedFolderSelect,
    onUnifiedInboxSelect,
    onCompose,
    onMessagesMoved,
    selectedAccountId = null,
    selectedFolderId = null,
    selectionSource = null,
    isFocused: _isFocused = false,
    isFlashing = false,
    showBackButton = false,
    onBack,
  }: Props = $props()

  // Unified folder counts: unread per folder type, for the sidebar badges.
  let unifiedUnreadCounts = $state<Record<string, number>>({})

  // Outbox badge: Mail Merge items still waiting to be sent.
  let outboxCount = $state(0)

  // Unified folder display order (drag-to-reorder, persisted).
  let unifiedFolderOrder = $state<string[]>(getUnifiedFolderOrder())

  // Needs attention (Phase A): live count for the shortcut entry in the
  // Unified zone. Hidden when the count is 0.
  let attentionCount = $state(0)

  function handleUnifiedFolderReorder(order: string[]) {
    unifiedFolderOrder = order
    setUnifiedFolderOrder(order)
  }

  // Dialog state
  let showAccountDialog = $state(false)
  let showDeleteDialog = $state(false)
  let showSettingsDialog = $state(false)
  let editingAccount = $state<account.Account | null>(null)
  let deletingAccount = $state<account.Account | null>(null)

  // Load accounts and contact sources on mount
  onMount(() => {
    loadSidebarSettings()

    // Load accounts, then trigger comprehensive sync on launch
    accountStore.load().then(async () => {
      try {
        await accountStore.syncAllComplete()
      } catch (err) {
        console.error('Failed to sync on launch:', err)
      }
    })

    contactSourcesStore.load()
    loadUnifiedFolderCounts()
    loadAttentionCount()

    // Listen for folder count changes to update unified folder counts
    const unsubscribe = EventsOn('folders:countsChanged', (_data: Record<string, number>) => {
      loadUnifiedFolderCounts()
    })
    // The outbox badge follows the Mail Merge queue.
    const unsubscribeQueue = EventsOn('mailmerge:queue-updated', () => {
      loadOutboxCount()
    })
    // Chip actions, dismissals and settings changes all emit this.
    const unsubscribeAttention = EventsOn('attention:changed', () => {
      loadAttentionCount()
    })

    return () => {
      unsubscribe()
      unsubscribeQueue()
      unsubscribeAttention()
    }
  })

  // Load unified folder unread counts + the outbox badge
  async function loadUnifiedFolderCounts() {
    try {
      unifiedUnreadCounts = await GetUnifiedFolderUnreadCounts()
    } catch (err) {
      console.error('Failed to load unified folder counts:', err)
    }
    await loadOutboxCount()
  }

  async function loadOutboxCount() {
    try {
      const stats = await MailMerge_GetQueueStats()
      outboxCount = stats?.queued ?? 0
    } catch (err) {
      console.error('Failed to load outbox count:', err)
    }
  }

  // Load Needs attention count
  async function loadAttentionCount() {
    try {
      attentionCount = await GetAttentionCount()
    } catch (err) {
      console.error('Failed to load attention count:', err)
    }
  }

  // Needs attention shortcut: opens Inbox/Unified Inbox with the bars in
  // place (and scrolls the list to them when already there). This is a
  // shortcut to the same bars, not a separate folder.
  function handleAttentionSelect() {
    handleUnifiedInboxSelect()
    EventsEmit('attention:scrollToTop')
  }

  // Find the first folder of a given type in a folder tree (recursive —
  // special folders can be nested, e.g. Gmail's [Gmail]/Sent).
  function findFolderOfType(trees: folder.FolderTree[], type: string): folder.Folder | null {
    for (const tree of trees) {
      if (tree.folder?.type === type) return tree.folder
      if (tree.children) {
        const found = findFolderOfType(tree.children, type)
        if (found) return found
      }
    }
    return null
  }

  // Find the inbox folder in a folder tree
  function findInboxFolder(trees: folder.FolderTree[]): folder.Folder | null {
    return findFolderOfType(trees, 'inbox')
  }

  // The Unified zone still shows with a single account, unless the
  // sidebar_hide_unified_single_account setting hides it.
  let showUnified = $derived(
    accountStore.accounts.length !== 1 || !getHideUnifiedSingleAccount()
  )

  // Handle unified inbox selection (All Inboxes)
  function handleUnifiedInboxSelect() {
    onUnifiedInboxSelect?.()
  }

  // Handle folder selection from the Unified zone (one of the seven
  // unified folders — the view merges that folder across all accounts).
  function handleUnifiedFolderSelect(folderType: string, label: string) {
    onUnifiedFolderSelect?.(folderType, label)
  }

  // Sync status: { accountName, label, percentage } — accountName + percentage are populated only when a sync is active
  let syncStatus = $derived.by<{ accountName: string | null; label: string; percentage: number | null }>(() => {
    if (accountStore.isAnySyncing) {
      const syncingAcc = accountStore.accounts.find((a) => a.syncing)
      if (syncingAcc) {
        const accountName = syncingAcc.account.name
        const progress = accountStore.getSyncProgress(syncingAcc.account.id)
        if (progress) {
          if (progress.phase === 'folders') return { accountName, label: $_('sidebar.syncingFolders'), percentage: null }
          if (progress.phase === 'messages') return { accountName, label: $_('sidebar.fetchingMessageList'), percentage: null }
          if (progress.phase === 'headers') return { accountName, label: $_('sidebar.fetchingHeaders', { values: { percentage: progress.percentage } }), percentage: progress.percentage }
          return { accountName, label: $_('sidebar.syncingContent', { values: { percentage: progress.percentage } }), percentage: progress.percentage }
        }
        return { accountName, label: $_('sidebar.syncing'), percentage: null }
      }
      return { accountName: null, label: $_('sidebar.syncing'), percentage: null }
    }
    if (!accountStore.isOnline) return { accountName: null, label: $_('sidebar.offline'), percentage: null }
    if (!accountStore.lastSyncTime) return { accountName: null, label: $_('sidebar.notSynced'), percentage: null }
    return { accountName: null, label: $_('sidebar.synced', { values: { time: formatDistanceToNow(accountStore.lastSyncTime, { addSuffix: true, locale: getCurrentDateFnsLocale() }) } }), percentage: null }
  })

  // Handle folder selection
  function handleFolderSelect(accountId: string, folderId: string, folderPath: string, folderName: string, folderType: string) {
    accountStore.selectFolder(accountId, folderId, folderPath, folderName)
    onFolderSelect?.(accountId, folderId, folderPath, folderName, folderType)
  }

  // Open an account's Inbox (account-name click): navigates to the Inbox
  // and expands the account if it is collapsed. When the account has no
  // Inbox folder synced yet, just expands so its state is visible.
  function openAccountInbox(accountId: string) {
    const accWithFolders = accountStore.accounts.find((a) => a.account.id === accountId)
    if (!accWithFolders) return
    if (!expandedAccounts[accountId]) {
      setAccountExpandedValue(accountId, true)
    }
    const inbox = findInboxFolder(accWithFolders.folders || [])
    if (inbox) {
      handleFolderSelect(accountId, inbox.id, inbox.path, inbox.name, 'inbox')
    }
  }

  // Does this account's folder tree contain the open folder?
  function treeContainsFolder(trees: folder.FolderTree[], folderId: string): boolean {
    for (const tree of trees) {
      if (tree.folder?.id === folderId) return true
      if (tree.children && treeContainsFolder(tree.children, folderId)) return true
    }
    return false
  }

  function accountContainsSelection(accountId: string): boolean {
    if (!selectedFolderId) return false
    const acc = accountStore.accounts.find((a) => a.account.id === accountId)
    if (!acc) return false
    return treeContainsFolder(acc.folders || [], selectedFolderId)
  }

  function accountInboxUnread(accountId: string): number {
    const acc = accountStore.accounts.find((a) => a.account.id === accountId)
    if (!acc) return 0
    return findInboxFolder(acc.folders || [])?.unreadCount ?? 0
  }

  // Open add account dialog
  function openAddAccount() {
    editingAccount = null
    showAccountDialog = true
  }

  // Open edit account dialog
  function openEditAccount(acc: account.Account) {
    editingAccount = acc
    showAccountDialog = true
  }

  // Open delete confirmation
  function openDeleteAccount(acc: account.Account) {
    deletingAccount = acc
    showDeleteDialog = true
  }

  // Sync all accounts (comprehensive sync)
  export async function syncAllAccounts() {
    try {
      await accountStore.syncAllComplete()
    } catch (err) {
      console.error('Sync failed:', err)
      // Error is already stored in account store
    }
  }

  // Cancel all running syncs
  export async function cancelSync() {
    try {
      await accountStore.cancelAllSyncs()
    } catch (err) {
      console.error('Failed to cancel sync:', err)
    }
  }

  // Toggle sync (start if not running, cancel if running) - for keyboard shortcut
  export async function toggleSync() {
    if (accountStore.isAnySyncing) {
      await cancelSync()
    } else {
      await syncAllAccounts()
    }
  }

  // Build flat list of all navigable rows in exact visual order:
  // Unified zone (inbox, attention, type groups + their per-account rows),
  // then the accounts block (header + folders when expanded).
  function buildFolderNavList(): FolderNavItem[] {
    const items: FolderNavItem[] = []

    if (showUnified && accountStore.accounts.length > 0) {
      // The seven unified folders, in the persisted display order.
      for (const type of unifiedFolderOrder) {
        items.push({
          type: 'unified-folder',
          folderName: type,
          folderType: type,
        })
      }

      if (attentionCount > 0) {
        items.push({
          type: 'attention',
          folderName: 'Needs attention',
        })
      }
    }

    // Accounts block, in store order (first account on top)
    for (const accWithFolders of accountStore.accounts) {
      // Skip if account is not fully loaded yet (can happen during reauth)
      if (!accWithFolders.account) continue

      // Always add the account header (so user can navigate to it and expand)
      items.push({
        type: 'account-header',
        accountId: accWithFolders.account.id,
        folderName: accWithFolders.account.name,
      })

      // Only add folders if the account is expanded
      if (expandedAccounts[accWithFolders.account.id]) {
        const flattenFolders = (trees: folder.FolderTree[]) => {
          for (const tree of trees) {
            if (tree.folder) {
              items.push({
                type: 'folder',
                accountId: accWithFolders.account.id,
                folderId: tree.folder.id,
                folderPath: tree.folder.path,
                folderName: tree.folder.name,
                folderType: tree.folder.type,
              })
            }
            // Skip children of collapsed folders
            if (tree.children && tree.children.length > 0 && tree.folder && collapsedFolders[tree.folder.id] === false) {
              flattenFolders(tree.children)
            }
          }
        }
        flattenFolders(accWithFolders.folders || [])
      }
    }

    return items
  }

  // Get current folder index in navigation list
  function getCurrentFolderIndex(): number {
    const navList = buildFolderNavList()

    // Check if an account header is focused
    if (focusedAccountId) {
      return navList.findIndex(item =>
        item.type === 'account-header' && item.accountId === focusedAccountId
      )
    }

    // Check selectionSource to find the correct item
    if (selectionSource === 'unified') {
      // Looking for a unified folder row (matched by folder type)
      return navList.findIndex(item =>
        item.type === 'unified-folder' && item.folderType === selectedFolderId
      )
    } else {
      // Looking for regular folder item
      return navList.findIndex(item =>
        item.type === 'folder' && item.folderId === selectedFolderId
      )
    }
  }

  // Navigate to previous folder (exposed for keyboard navigation)
  export function selectPreviousFolder() {
    const navList = buildFolderNavList()
    if (navList.length === 0) return

    const currentIndex = getCurrentFolderIndex()
    const newIndex = currentIndex <= 0 ? 0 : currentIndex - 1

    selectFolderByIndex(navList, newIndex)
  }

  // Navigate to next folder (exposed for keyboard navigation)
  export function selectNextFolder() {
    const navList = buildFolderNavList()
    if (navList.length === 0) return

    const currentIndex = getCurrentFolderIndex()
    const newIndex = currentIndex >= navList.length - 1 ? navList.length - 1 : currentIndex + 1

    selectFolderByIndex(navList, newIndex)
  }

  // Jump to the top sidebar item — Unified Inbox when shown (Alt+G)
  export function selectFirstFolder() {
    const navList = buildFolderNavList()
    if (navList.length === 0) return
    selectFolderByIndex(navList, 0)
  }

  // Jump to the last visible item: last folder of the last expanded account,
  // or the last account header when all trees are collapsed (Alt+Shift+G)
  export function selectLastFolder() {
    const navList = buildFolderNavList()
    if (navList.length === 0) return
    selectFolderByIndex(navList, navList.length - 1)
  }

  // Scroll an item into view
  function scrollItemIntoView(item: FolderNavItem) {
    if (!scrollContainer) return

    // Build selector based on item type
    let selector: string | null = null
    if (item.type === 'attention') {
      selector = '[data-sidebar-item="attention"]'
    } else if (item.type === 'unified-folder' && item.folderType) {
      selector = `[data-sidebar-item="unified-folder"][data-folder-type="${item.folderType}"]`
    } else if (item.type === 'account-header' && item.accountId) {
      selector = `[data-sidebar-item="account-header"][data-account-id="${item.accountId}"]`
    } else if (item.type === 'folder' && item.folderId) {
      selector = `[data-sidebar-item="folder"][data-folder-id="${item.folderId}"]`
    }

    if (selector) {
      const element = scrollContainer.querySelector(selector)
      element?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    }
  }

  // Clear keyboard focus flags (moving to a real folder)
  function clearRowFocus() {
    focusedAccountId = null
  }

  // Select folder by index in nav list
  function selectFolderByIndex(navList: FolderNavItem[], index: number) {
    const item = navList[index]
    if (!item) return

    if (item.type === 'attention') {
      clearRowFocus()
      handleAttentionSelect()
    } else if (item.type === 'unified-folder' && item.folderType) {
      // Select a unified folder - merges that folder across all accounts
      clearRowFocus()
      const def = UNIFIED_FOLDER_DEFS.find((d) => d.type === item.folderType)
      onUnifiedFolderSelect?.(item.folderType, def ? $_(def.labelKey) : (item.folderType ?? ''))
    } else if (item.type === 'account-header' && item.accountId) {
      // Focus on account header (Enter opens its Inbox, Space/→/← expand/collapse)
      focusedAccountId = item.accountId
    } else if (item.type === 'folder' && item.accountId && item.folderId && item.folderPath) {
      // Select from account tree - uses handleFolderSelect
      clearRowFocus()
      handleFolderSelect(item.accountId, item.folderId, item.folderPath, item.folderName, item.folderType || 'folder')
    }

    // Scroll the selected item into view
    scrollItemIntoView(item)
  }

  // Toggle expand/collapse for the focused account (called on Space/Alt+Enter)
  export function toggleFocusedAccount() {
    if (focusedAccountId) {
      toggleAccountExpanded(focusedAccountId)
    }
  }

  // Check if an account header is focused
  export function hasFocusedAccount(): boolean {
    return focusedAccountId !== null
  }

  // Open the focused account's Inbox (called on Enter)
  export function openFocusedAccountInbox() {
    if (focusedAccountId) {
      openAccountInbox(focusedAccountId)
    }
  }

  // Expand the focused row: account header, or the selected
  // folder when it has children (called on ArrowRight)
  export function expandFocusedRow() {
    if (focusedAccountId) {
      setAccountExpandedValue(focusedAccountId, true)
      return
    }
    if (selectedFolderId && selectionSource === 'account' && folderHasChildren(selectedFolderId)) {
      setFolderCollapsedValue(selectedFolderId, false)
    }
  }

  // Collapse the focused row (called on ArrowLeft)
  export function collapseFocusedRow() {
    if (focusedAccountId) {
      setAccountExpandedValue(focusedAccountId, false)
      return
    }
    if (selectedFolderId && selectionSource === 'account' && folderHasChildren(selectedFolderId)) {
      setFolderCollapsedValue(selectedFolderId, true)
    }
  }

  // Check if the currently selected folder has children
  export function hasSelectedFolderWithChildren(): boolean {
    if (!selectedFolderId || selectionSource !== 'account') return false
    return folderHasChildren(selectedFolderId)
  }

  // Toggle collapse for the currently selected folder
  export function toggleSelectedFolderCollapse(): void {
    if (!selectedFolderId || selectionSource !== 'account') return
    if (!folderHasChildren(selectedFolderId)) return
    toggleFolderCollapsed(selectedFolderId)
  }

  // Check if a folder has children by searching the account folder trees
  function folderHasChildren(folderId: string): boolean {
    for (const acc of accountStore.accounts) {
      const found = findTreeNode(acc.folders || [], folderId)
      if (found) return (found.children && found.children.length > 0) || false
    }
    return false
  }

  // Find a FolderTree node by folder ID
  function findTreeNode(trees: folder.FolderTree[], folderId: string): folder.FolderTree | null {
    for (const tree of trees) {
      if (tree.folder?.id === folderId) return tree
      if (tree.children) {
        const found = findTreeNode(tree.children, folderId)
        if (found) return found
      }
    }
    return null
  }
</script>

<div class="flex flex-col h-full {isFlashing ? 'pane-focus-flash' : ''}">
  <!-- Header with Compose Button -->
  <div class="px-4 py-3 border-b border-border">
    <div class="flex items-center gap-2">
      <button
        class="flex-1 flex items-center justify-center gap-2 px-3 py-2 bg-primary text-primary-foreground rounded-md text-sm font-medium hover:bg-primary/90 transition-colors"
        onclick={onCompose}
      >
        <Icon icon="mdi:pencil" class="w-4 h-4" />
        <span>{$_('sidebar.compose')}</span>
      </button>
      {#if showBackButton}
        <button
          class="p-2 rounded-md hover:bg-muted transition-colors flex-shrink-0"
          title={$_('responsive.back')}
          aria-label={$_('aria.closeSidebar')}
          onclick={onBack}
        >
          <Icon icon="mdi:close" class="w-5 h-5 text-muted-foreground" />
        </button>
      {/if}
    </div>
  </div>

  <!-- Sidebar column: one scrollable column. Zone 1 (Unified) sits at the
       top; a flexible spacer docks Zone 2 (Accounts) at the bottom when the
       column is shorter than the window. When the column overflows, the
       whole sidebar scrolls as one column — nothing is clipped to preserve
       the docking. -->
  <div class="flex-1 overflow-y-auto scrollbar-thin py-2" bind:this={scrollContainer}>
    <div class="flex flex-col min-h-full">
      {#if accountStore.loading}
        <div class="flex items-center justify-center py-8">
          <Icon icon="mdi:loading" class="w-6 h-6 animate-spin text-muted-foreground" />
        </div>
      {:else if accountStore.accounts.length === 0}
        <!-- Empty State -->
        <div class="flex flex-col items-center justify-center py-8 px-4 text-center">
          <Icon icon="mdi:email-plus-outline" class="w-12 h-12 text-muted-foreground mb-3" />
          <h3 class="text-sm font-medium mb-1">{$_('sidebar.noAccountsYet')}</h3>
          <p class="text-xs text-muted-foreground mb-4">
            {$_('sidebar.addFirstAccount')}
          </p>
          <Button size="sm" onclick={openAddAccount}>
            <Icon icon="mdi:plus" class="w-4 h-4 mr-1" />
            {$_('sidebar.addAccount')}
          </Button>
        </div>
      {:else}
        <!-- Zone 1 — Unified: always at the top. The section collapses
             via its own chevron (persisted unifiedInboxExpanded). Hidden
             only with a single account when the
             sidebar_hide_unified_single_account setting is on. -->
        {#if showUnified}
          <UnifiedSection
            {attentionCount}
            unreadCounts={unifiedUnreadCounts}
            {outboxCount}
            folderOrder={unifiedFolderOrder}
            selectedFolderType={selectionSource === 'unified' ? selectedFolderId : null}
            {selectionSource}
            onSelectAttention={handleAttentionSelect}
            onSelectUnifiedFolder={handleUnifiedFolderSelect}
            onReorderFolders={handleUnifiedFolderReorder}
          />
          <div class="border-b border-border mx-3 my-1"></div>
        {/if}

        <!-- Flexible spacer: docks the accounts block at the bottom -->
        <div class="flex-1"></div>

        <!-- Zone 2 — Accounts: docked at the bottom of the column, in
             normal order (first account on top, second below it). -->
        <div class="pt-1">
          <div class="px-4 pb-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            {$_('sidebar.accounts')}
          </div>

          {#each accountStore.accounts as accWithFolders (accWithFolders.account.id)}
            <AccountSection
              account={accWithFolders.account}
              folders={accWithFolders.folders}
              loading={accWithFolders.loading}
              syncing={accWithFolders.syncing}
              error={accWithFolders.error}
              selectedFolderId={accountStore.selectedFolder?.folderId ?? ''}
              {selectionSource}
              isHeaderFocused={focusedAccountId === accWithFolders.account.id}
              isExpanded={expandedAccounts[accWithFolders.account.id] ?? true}
              syncError={accountStore.getSyncError(accWithFolders.account.id)}
              inboxUnreadCount={accountInboxUnread(accWithFolders.account.id)}
              containsSelection={accountContainsSelection(accWithFolders.account.id)}
              {collapsedFolders}
              {onMessagesMoved}
              onFolderSelect={handleFolderSelect}
              onToggleExpanded={() => toggleAccountExpanded(accWithFolders.account.id)}
              onSelectInbox={() => openAccountInbox(accWithFolders.account.id)}
              onEdit={() => openEditAccount(accWithFolders.account)}
              onDelete={() => openDeleteAccount(accWithFolders.account)}
              onSync={() => {
                // Clear any sync error before retrying
                accountStore.clearSyncError(accWithFolders.account.id)
                accountStore.syncAccount(accWithFolders.account.id)
              }}
            />
          {/each}

          <!-- Add Account Button -->
          <div class="px-3 py-2">
            <button
              class="w-full flex items-center gap-2 px-3 py-2 text-sm text-muted-foreground hover:text-foreground hover:bg-muted/50 rounded-md transition-colors"
              onclick={openAddAccount}
            >
              <Icon icon="mdi:plus" class="w-4 h-4" />
              <span>{$_('sidebar.addAccount')}</span>
            </button>
          </div>
        </div>
      {/if}
    </div>
  </div>

  <!-- Footer: Sync Status + Settings. Chrome (padding/border/min-height)
       lives in kit `SidebarFooter` so mail, calendar, and contacts all
       render the same strip height. Mail's progress bar overlay + 2-line
       leading content (account name + status label) pass through
       SidebarFooter's overlay/leading snippets unchanged. -->
  <SidebarFooter>
    {#snippet overlay()}
      {#if syncStatus.percentage !== null}
        <div class="absolute top-0 left-0 right-0 h-1 bg-muted overflow-hidden">
          <div
            class="h-full bg-primary transition-all duration-300 ease-out"
            style="width: {syncStatus.percentage}%"
          ></div>
        </div>
      {/if}
    {/snippet}
    {#snippet leading()}
      <button
        class="flex-1 min-w-0 flex items-end gap-2 hover:text-foreground transition-colors text-left"
        onclick={accountStore.isAnySyncing ? cancelSync : syncAllAccounts}
        title={$_(accountStore.isAnySyncing ? 'sidebar.clickToCancel' : 'sidebar.syncAllAccounts')}
      >
        <Icon
          icon="mdi:sync"
          class="w-4 h-4 flex-shrink-0 {accountStore.isAnySyncing ? 'animate-spin' : ''}"
        />
        <div class="flex-1 min-w-0">
          {#if syncStatus.accountName}
            <div class="truncate text-foreground font-medium leading-tight mb-0.5">{syncStatus.accountName}</div>
          {/if}
          <span class="block leading-tight">{syncStatus.label}</span>
        </div>
      </button>
    {/snippet}
    {#snippet trailing()}
      <button
        class="p-1 hover:text-foreground hover:bg-muted rounded transition-colors relative"
        onclick={() => showSettingsDialog = true}
        title={$_('sidebar.settings')}
      >
        <Icon icon="mdi:cog" class="w-4 h-4" />
        {#if contactSourcesStore.hasErrors}
          <span class="absolute -top-0.5 -right-0.5 w-2.5 h-2.5 bg-destructive rounded-full border border-background"></span>
        {/if}
      </button>
    {/snippet}
  </SidebarFooter>
</div>

<!-- Account Dialog -->
<AccountDialog
  bind:open={showAccountDialog}
  editAccount={editingAccount}
  onClose={() => {
    showAccountDialog = false
    editingAccount = null
    setFocusedPane('messageList')
  }}
/>

<!-- Delete Confirmation Dialog -->
<DeleteAccountDialog
  bind:open={showDeleteDialog}
  account={deletingAccount}
  onClose={() => {
    showDeleteDialog = false
    deletingAccount = null
    setFocusedPane('messageList')
  }}
/>

<!-- Settings Dialog -->
<SettingsDialog
  bind:open={showSettingsDialog}
  onClose={() => {
    showSettingsDialog = false
    setFocusedPane('messageList')
  }}
/>
