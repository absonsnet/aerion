<script lang="ts">
  import Icon from '@iconify/svelte'
  // @ts-ignore - wailsjs path
  import type { account, folder } from '../../../../wailsjs/go/models'
  import FolderContextMenu from './FolderContextMenu.svelte'
  import { _ } from '$lib/i18n'

  // One entry of a unified folder-type group: the account's own folder of
  // that type (e.g. its Sent folder).
  export interface UnifiedFolderEntry {
    account: account.Account
    folder: folder.Folder
  }

  // A folder type shown in the Unified zone (Starred, Sent, Drafts, Spam,
  // Trash). There is no cross-account unified view for these types — the
  // backend only builds one for the Inbox — so each group lists the
  // per-account folders of that type instead of pretending to merge them.
  // The badge is the summed unread count across those accounts.
  export interface UnifiedFolderGroup {
    type: string
    labelKey: string
    icon: string
    totalUnread: number
    entries: UnifiedFolderEntry[]
  }

  interface Props {
    unifiedUnreadCount: number
    attentionCount: number
    groups: UnifiedFolderGroup[]
    expandedGroups: Record<string, boolean>
    focusedGroupType: string | null
    selectedAccountId: string | null
    selectedFolderId: string | null
    selectionSource: 'unified' | 'account' | null
    onSelectUnified: () => void
    onSelectAttention: () => void
    onSelectUnifiedFolder: (accountId: string, folderId: string, folderPath: string, folderName: string, folderType: string) => void
    onToggleGroup: (type: string) => void
  }

  let {
    unifiedUnreadCount,
    attentionCount,
    groups,
    expandedGroups,
    focusedGroupType,
    selectedAccountId,
    selectedFolderId,
    selectionSource,
    onSelectUnified,
    onSelectAttention,
    onSelectUnifiedFolder,
    onToggleGroup,
  }: Props = $props()

  // Check if Unified Inbox is selected (All Inboxes)
  const isUnifiedSelected = $derived(selectedAccountId === 'unified' && selectedFolderId === 'inbox')

  // Check if a specific folder is selected IN THE UNIFIED SECTION
  // Only highlight if selectionSource is 'unified'
  function isUnifiedFolderSelected(folderId: string): boolean {
    return selectionSource === 'unified' && selectedFolderId === folderId
  }

  // Get account color with fallback
  function getAccountColor(acc: account.Account): string {
    // @ts-ignore - color field from backend
    return acc.color || '#6B7280' // Default gray if no color set
  }

  // A group "contains the selection" when one of its per-account folders
  // is open — shown as a dot on the group header while it is collapsed.
  function groupContainsSelection(group: UnifiedFolderGroup): boolean {
    return group.entries.some((entry) => isUnifiedFolderSelected(entry.folder.id))
  }
</script>

<div class="px-2 py-1">
  <!-- Section label -->
  <div class="px-2 pb-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
    {$_('sidebar.unified')}
  </div>

  <!-- Unified Inbox -->
  <button
    class="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-sm transition-colors {isUnifiedSelected
      ? 'bg-primary/10 text-primary font-medium'
      : 'hover:bg-muted/50'}"
    data-sidebar-item="unified"
    onclick={onSelectUnified}
  >
    <Icon icon="mdi:inbox-multiple" class="w-4 h-4 flex-shrink-0" />
    <span class="flex-1 text-left truncate">{$_('sidebar.allInboxes')}</span>
    {#if unifiedUnreadCount > 0}
      <span class="px-1.5 py-0.5 text-xs font-medium bg-primary text-primary-foreground rounded-full">
        {unifiedUnreadCount}
      </span>
    {/if}
  </button>

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

  <!-- Unified folder-type groups: Starred / Sent / Drafts / Spam / Trash.
       Each group lists the per-account folders of that type; the group
       header itself is not a folder, so clicking it expands/collapses. -->
  {#each groups as group (group.type)}
    {@const isExpanded = expandedGroups[group.type] === true}
    {@const isFocused = focusedGroupType === group.type}
    {@const containsSelection = groupContainsSelection(group)}
    <div>
      <button
        class="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-sm transition-colors hover:bg-muted/50 {isFocused ? 'bg-muted ring-1 ring-primary/50' : ''}"
        data-sidebar-item="unified-group"
        data-folder-type={group.type}
        onclick={() => onToggleGroup(group.type)}
      >
        <Icon
          icon={isExpanded ? 'mdi:chevron-down' : 'mdi:chevron-right'}
          class="w-4 h-4 text-muted-foreground flex-shrink-0"
        />
        <Icon icon={group.icon} class="w-4 h-4 flex-shrink-0" />
        <span class="flex-1 text-left truncate">{$_(group.labelKey)}</span>
        {#if containsSelection && !isExpanded}
          <span
            class="w-1.5 h-1.5 rounded-full bg-primary flex-shrink-0"
            title={$_('sidebar.containsSelection')}
          ></span>
        {/if}
        {#if group.totalUnread > 0}
          <span class="px-1.5 py-0.5 text-xs font-medium bg-primary text-primary-foreground rounded-full">
            {group.totalUnread}
          </span>
        {/if}
      </button>

      {#if isExpanded}
        <div class="ml-4 mt-0.5 space-y-0.5">
          {#each group.entries as entry (entry.account.id)}
            <FolderContextMenu folderId={entry.folder.id}>
              <button
                class="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-sm transition-colors {isUnifiedFolderSelected(entry.folder.id)
                  ? 'bg-primary/10 text-primary font-medium'
                  : 'hover:bg-muted/50 text-muted-foreground hover:text-foreground'}"
                data-sidebar-item="unified-folder"
                data-folder-id={entry.folder.id}
                onclick={() => onSelectUnifiedFolder(entry.account.id, entry.folder.id, entry.folder.path, entry.folder.name, entry.folder.type)}
              >
                <span
                  class="w-2 h-2 rounded-full flex-shrink-0"
                  style="background-color: {getAccountColor(entry.account)}"
                ></span>
                <span class="flex-1 text-left truncate">{entry.account.name}</span>
                {#if entry.folder.unreadCount > 0}
                  <span class="px-1.5 py-0.5 text-xs font-medium bg-muted text-muted-foreground rounded-full">
                    {entry.folder.unreadCount}
                  </span>
                {/if}
              </button>
            </FolderContextMenu>
          {/each}
        </div>
      {/if}
    </div>
  {/each}
</div>
