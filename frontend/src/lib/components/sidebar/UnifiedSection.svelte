<script lang="ts" module>
  // One unified folder: a folder type merged across all accounts.
  // Clicking it opens the unified view of that type — the same folder
  // combined across every account. Spam merges each provider's Spam/Junk
  // folder and Trash merges Trash/Deleted Items/Bin (sync classifies them
  // to one folder type); Starred is flag-based across all folders; Outbox
  // is the local Mail Merge send queue, not a server folder.
  export interface UnifiedFolderDef {
    type: string
    labelKey: string
    icon: string
  }

  // Canonical order (Abid's list, v0.7.1). The displayed order is the
  // persisted unified_folder_order setting; this list only defines the
  // labels, icons and the first-run default.
  export const UNIFIED_FOLDER_DEFS: UnifiedFolderDef[] = [
    { type: 'inbox', labelKey: 'sidebar.inbox', icon: 'mdi:inbox-multiple' },
    { type: 'starred', labelKey: 'sidebar.starred', icon: 'mdi:star-outline' },
    { type: 'sent', labelKey: 'sidebar.sent', icon: 'mdi:send' },
    { type: 'drafts', labelKey: 'sidebar.draft', icon: 'mdi:file-document-edit-outline' },
    { type: 'spam', labelKey: 'sidebar.spam', icon: 'mdi:alert-octagon-outline' },
    { type: 'trash', labelKey: 'sidebar.trash', icon: 'mdi:delete-outline' },
    { type: 'outbox', labelKey: 'sidebar.outbox', icon: 'mdi:send-clock-outline' },
  ]
</script>

<script lang="ts">
  import Icon from '@iconify/svelte'
  import { _ } from '$lib/i18n'
  import {
    getUIStateVersion,
    isUnifiedInboxExpanded,
    setUnifiedInboxExpanded,
  } from '$lib/stores/uiState.svelte'

  interface Props {
    attentionCount: number
    // Unread counts per unified folder type (inbox/starred/sent/drafts/
    // spam/trash), from GetUnifiedFolderUnreadCounts.
    unreadCounts: Record<string, number>
    // Queued (not yet sent) Mail Merge items, for the Outbox badge.
    outboxCount: number
    // Folder type keys in display order (persisted unified_folder_order).
    folderOrder: string[]
    // The currently open unified folder type, if the selection is unified.
    selectedFolderType: string | null
    selectionSource: 'unified' | 'account' | null
    onSelectAttention: () => void
    onSelectUnifiedFolder: (type: string, label: string) => void
    // Clicking the UNIFIED heading text opens the unified inbox. Only
    // the chevron expands/collapses the section (see below).
    onSelectUnifiedInbox: () => void
    onReorderFolders: (order: string[]) => void
  }

  let {
    attentionCount,
    unreadCounts,
    outboxCount,
    folderOrder,
    selectedFolderType,
    selectionSource,
    onSelectAttention,
    onSelectUnifiedFolder,
    onSelectUnifiedInbox,
    onReorderFolders,
  }: Props = $props()

  // Section expand/collapse, persisted in the backend UI state
  // (unifiedInboxExpanded — the key predates this section, it now drives
  // the whole Unified zone). Local state mirrors it; the effect below
  // re-syncs once the persisted state has loaded.
  let sectionExpanded = $state(true)
  $effect(() => {
    // Read version to create dependency - effect re-runs when UI state finishes loading
    const _version = getUIStateVersion()
    sectionExpanded = isUnifiedInboxExpanded()
  })

  function toggleSection() {
    sectionExpanded = !sectionExpanded
    setUnifiedInboxExpanded(sectionExpanded)
  }

  // Rows in display order: the persisted order, with any def missing from
  // it (a type added by an upgrade) appended in canonical order.
  const orderedDefs = $derived.by<UnifiedFolderDef[]>(() => {
    const byType = new Map(UNIFIED_FOLDER_DEFS.map((d) => [d.type, d]))
    const seen = new Set<string>()
    const out: UnifiedFolderDef[] = []
    for (const type of folderOrder) {
      const def = byType.get(type)
      if (def && !seen.has(type)) {
        seen.add(type)
        out.push(def)
      }
    }
    for (const def of UNIFIED_FOLDER_DEFS) {
      if (!seen.has(def.type)) out.push(def)
    }
    return out
  })

  function badgeFor(def: UnifiedFolderDef): number {
    if (def.type === 'outbox') return outboxCount
    return unreadCounts[def.type] ?? 0
  }

  function isSelected(def: UnifiedFolderDef): boolean {
    return selectionSource === 'unified' && selectedFolderType === def.type
  }

  // Drag-to-reorder: drag a row onto another to move it before that row.
  // The new order is persisted by the parent (unified_folder_order).
  let dragType = $state<string | null>(null)
  let dropTargetType = $state<string | null>(null)

  function onDragStart(type: string, e: DragEvent) {
    dragType = type
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', type)
    }
  }

  function onDragOver(type: string, e: DragEvent) {
    if (!dragType || dragType === type) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    dropTargetType = type
  }

  function onDrop(type: string, e: DragEvent) {
    e.preventDefault()
    if (!dragType || dragType === type) {
      dragType = null
      dropTargetType = null
      return
    }
    const order = orderedDefs.map((d) => d.type)
    const from = order.indexOf(dragType)
    const to = order.indexOf(type)
    if (from === -1 || to === -1) {
      dragType = null
      dropTargetType = null
      return
    }
    order.splice(from, 1)
    order.splice(to, 0, dragType)
    dragType = null
    dropTargetType = null
    onReorderFolders(order)
  }

  function onDragEnd() {
    dragType = null
    dropTargetType = null
  }
</script>

<div class="px-2 py-1">
  <!-- Section heading: the chevron (and ONLY the chevron) expands or
       collapses the section; the UNIFIED label opens the unified inbox.
       Both are real buttons, so Enter/Space work on the chevron too. -->
  <div class="w-full flex items-center gap-1 px-2 pb-1">
    <button
      type="button"
      class="p-0.5 rounded hover:bg-muted transition-colors flex-shrink-0"
      aria-label={sectionExpanded ? $_('sidebar.collapseUnified') : $_('sidebar.expandUnified')}
      aria-expanded={sectionExpanded}
      onclick={toggleSection}
      onkeydown={(e) => {
        // Enter/Space on a focused sidebar button is swallowed by the
        // app's global keydown handler; keep the event here so the
        // button's native activation runs.
        if (e.key === 'Enter' || e.key === ' ') e.stopPropagation()
      }}
    >
      <Icon
        icon={sectionExpanded ? 'mdi:chevron-down' : 'mdi:chevron-right'}
        class="w-4 h-4 text-muted-foreground"
      />
    </button>
    <button
      type="button"
      class="flex-1 min-w-0 text-left"
      onclick={onSelectUnifiedInbox}
    >
      <span class="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        {$_('sidebar.unified')}
      </span>
    </button>
  </div>

  {#if sectionExpanded}
    <!-- Unified folders, in the persisted order. Each row opens the
         unified view of that folder type across all accounts. -->
    {#each orderedDefs as def (def.type)}
      {@const badge = badgeFor(def)}
      <button
        type="button"
        draggable="true"
        class="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-sm transition-colors {isSelected(def)
          ? 'bg-primary/10 text-primary font-medium'
          : 'hover:bg-muted/50'} {dropTargetType === def.type ? 'ring-1 ring-primary' : ''} {dragType === def.type ? 'opacity-50' : ''}"
        data-sidebar-item="unified-folder"
        data-folder-type={def.type}
        title={$_('sidebar.dragToReorder')}
        onclick={() => onSelectUnifiedFolder(def.type, $_(def.labelKey))}
        ondragstart={(e) => onDragStart(def.type, e)}
        ondragover={(e) => onDragOver(def.type, e)}
        ondragleave={() => {
          if (dropTargetType === def.type) dropTargetType = null
        }}
        ondrop={(e) => onDrop(def.type, e)}
        ondragend={onDragEnd}
      >
        <Icon icon={def.icon} class="w-4 h-4 flex-shrink-0" />
        <span class="flex-1 text-left truncate">{$_(def.labelKey)}</span>
        {#if badge > 0}
          <span class="px-1.5 py-0.5 text-xs font-medium bg-primary text-primary-foreground rounded-full">
            {badge}
          </span>
        {/if}
      </button>
    {/each}

    <!-- Needs attention shortcut: opens Inbox/Unified Inbox with the bars in
         place (and scrolls the list to them when already there). This is a
         shortcut to the same bars, not a separate folder. Hidden when empty. -->
    {#if attentionCount > 0}
      <button
        type="button"
        class="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-sm transition-colors hover:bg-muted/50"
        data-sidebar-item="attention"
        onclick={onSelectAttention}
      >
        <Icon icon="mdi:bell-alert-outline" class="w-4 h-4 text-primary flex-shrink-0" />
        <span class="flex-1 text-left truncate font-medium">{$_('attention.title')}</span>
        <span class="flex-shrink-0 min-w-5 h-5 px-1 rounded-full bg-primary text-primary-foreground text-xs flex items-center justify-center">
          {attentionCount}
        </span>
      </button>
    {/if}
  {/if}
</div>
