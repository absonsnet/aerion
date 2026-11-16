<script lang="ts">
  import Icon from '@iconify/svelte'
  import { ContextMenu as ContextMenuPrimitive } from 'bits-ui'
  import {
    ContextMenuContent,
    ContextMenuItem,
    ContextMenuSeparator,
  } from '$lib/components/ui/context-menu'
  import {
    GetAccounts,
    GetMessage,
    MarkAsRead,
    MarkAsUnread,
    Star,
    Unstar,
    Archive,
    Trash,
    MarkAsSpam,
    MarkAsNotSpam,
    DeletePermanently,
    MoveToFolder,
    CopyToFolder,
    Undo,
    GetUnsubscribeStatuses,
    SetSenderCategoryOverride,
  } from '../../../../wailsjs/go/app/App'
  // @ts-ignore - wailsjs path
  import { account, app as appModels } from '../../../../wailsjs/go/models'
  import { runUnsubscribeFlow } from '$lib/unsubscribe'
  import { toasts } from '$lib/stores/toast'
  import { ConfirmDialog } from '$lib/components/ui/confirm-dialog'
  import FolderPickerDialog from './FolderPickerDialog.svelte'
  import type { Snippet } from 'svelte'
  import { _ } from '$lib/i18n'
  import { dialogGuardOpen, dialogGuardClose } from '$lib/stores/dialogGuard'

  interface Props {
    messageIds: string[]
    accountId: string
    currentFolderId: string
    folderType: string
    isStarred: boolean
    isRead: boolean
    onActionComplete?: (autoSelectNext?: boolean) => void
    onReply?: (mode: 'reply' | 'reply-all' | 'forward', messageId: string) => void
    onOpenChange?: (open: boolean) => void
    children?: Snippet
  }

  let {
    messageIds,
    accountId,
    currentFolderId,
    folderType,
    isStarred,
    isRead,
    onActionComplete,
    onReply,
    onOpenChange,
    children,
  }: Props = $props()

  // Accounts list — passed to FolderPickerDialog for its account dropdown.
  // Loaded lazily when the context menu opens.
  let accounts = $state<account.Account[]>([])
  let accountsLoaded = $state(false)
  let accountsLoading = $state(false)

  // Unsubscribe (Phase C): loaded lazily when the menu opens, single
  // message in Inbox/Spam only. Null = not offered for this message.
  let unsubscribeStatus = $state<appModels.UnsubscribeStatus | null>(null)
  let unsubscribeLoaded = $state(false)

  // Sender address for the "wrong tab" correction (Phase E): loaded
  // lazily when the menu opens, single message only. "" = not known yet.
  let senderEmail = $state('')
  let senderLoaded = $state(false)

  // Reset the per-message unsubscribe state when the menu's target
  // changes (a row component can be reused for another conversation).
  // Compare the id, not the array: callers pass a fresh array literal
  // on every render.
  let lastUnsubMessageId: string | null = null
  $effect(() => {
    const id = messageIds[0] ?? null
    if (id !== lastUnsubMessageId) {
      lastUnsubMessageId = id
      unsubscribeStatus = null
      unsubscribeLoaded = false
      senderEmail = ''
      senderLoaded = false
    }
  })

  // Permanent delete confirmation
  let showDeleteConfirm = $state(false)

  // Folder picker dialog state
  let showFolderPicker = $state(false)
  let folderPickerMode = $state<'move' | 'copy'>('move')

  // Track dialog open/close to prevent background reloads from dismissing dialogs
  $effect(() => {
    if (showFolderPicker) {
      dialogGuardOpen()
      return () => dialogGuardClose()
    }
  })

  $effect(() => {
    if (showDeleteConfirm) {
      dialogGuardOpen()
      return () => dialogGuardClose()
    }
  })

  // Computed values
  const isTrashFolder = $derived(folderType === 'trash')
  const isSpamFolder = $derived(folderType === 'spam')
  const isSingleMessage = $derived(messageIds.length === 1)

  // Load accounts when context menu opens (used by FolderPickerDialog's dropdown)
  async function loadAccounts() {
    if (accountsLoaded || accountsLoading) return

    accountsLoading = true
    try {
      const result = await GetAccounts()
      accounts = result || []
      accountsLoaded = true
    } catch (err) {
      console.error('Failed to load accounts:', err)
    } finally {
      accountsLoading = false
    }
  }

  // Load unsubscribe status when the context menu opens (used by the
  // Unsubscribe item). Single message in Inbox/Spam only.
  async function loadUnsubscribeStatus() {
    if (unsubscribeLoaded || !isSingleMessage) return
    if (folderType !== 'inbox' && folderType !== 'spam') return
    unsubscribeLoaded = true
    try {
      const statuses = await GetUnsubscribeStatuses(messageIds)
      const st = statuses?.[0] ?? null
      unsubscribeStatus = st?.available ? st : null
    } catch (err) {
      console.error('Failed to load unsubscribe status:', err)
    }
  }

  // Load the sender address when the context menu opens (used by the
  // "Always treat mail from this sender as …" items). Single message only.
  async function loadSenderEmail() {
    if (senderLoaded || !isSingleMessage) return
    senderLoaded = true
    try {
      const msg = await GetMessage(messageIds[0])
      senderEmail = msg?.fromEmail ?? ''
    } catch (err) {
      console.error('Failed to load sender for category override:', err)
    }
  }

  // Handle menu open
  function handleOpenChange(open: boolean) {
    if (open) {
      loadAccounts()
      loadUnsubscribeStatus()
      loadSenderEmail()
    }
    onOpenChange?.(open)
  }

  // Undo handler
  async function handleUndo() {
    try {
      const description = await Undo()
      toasts.success($_('toast.undone', { values: { description } }))
    } catch (err) {
      console.error('Undo failed:', err)
      toasts.error($_('toast.undoFailed'))
    }
  }

  // Action handlers
  async function handleReply() {
    if (isSingleMessage && onReply) {
      onReply('reply', messageIds[0])
    }
  }

  async function handleReplyAll() {
    if (isSingleMessage && onReply) {
      onReply('reply-all', messageIds[0])
    }
  }

  async function handleForward() {
    if (isSingleMessage && onReply) {
      onReply('forward', messageIds[0])
    }
  }

  async function handleArchive() {
    try {
      await Archive(messageIds)
      toasts.success($_('toast.archived'), [{ label: $_('common.undo'), onClick: handleUndo }])
      onActionComplete?.(true)
    } catch (err) {
      console.error('Archive failed:', err)
      toasts.error($_('toast.failedToArchive'))
    }
  }

  async function handleDelete() {
    if (isTrashFolder) {
      showDeleteConfirm = true
    } else {
      try {
        const movedToTrash = await Trash(messageIds)
        const toastMsg = movedToTrash ? $_('toast.movedToTrash') : $_('toast.deletedFromFolder')
        const actions = movedToTrash ? [{ label: $_('common.undo'), onClick: handleUndo }] : []
        toasts.success(toastMsg, actions)
        onActionComplete?.(true)
      } catch (err) {
        console.error('Delete failed:', err)
        toasts.error($_('toast.failedToDelete'))
      }
    }
  }

  async function handleConfirmPermanentDelete() {
    try {
      await DeletePermanently(messageIds)
      toasts.success($_('toast.permanentlyDeleted'))
      showDeleteConfirm = false
      onActionComplete?.(true)
    } catch (err) {
      console.error('Permanent delete failed:', err)
      toasts.error($_('toast.failedToDelete'))
      showDeleteConfirm = false
    }
  }

  async function handleSpam() {
    try {
      if (isSpamFolder) {
        // If we're in spam folder, mark as NOT spam
        await MarkAsNotSpam(messageIds)
        toasts.success($_('toast.markedAsNotSpam'), [{ label: $_('common.undo'), onClick: handleUndo }])
        onActionComplete?.(true)
        return
      }
      // Otherwise, mark as spam
      const movedToSpam = await MarkAsSpam(messageIds)
      const toastMsg = movedToSpam ? $_('toast.markedAsSpam') : $_('toast.deletedFromFolder')
      const actions = movedToSpam ? [{ label: $_('common.undo'), onClick: handleUndo }] : []
      toasts.success(toastMsg, actions)
      onActionComplete?.(true)
    } catch (err) {
      console.error('Spam toggle failed:', err)
      toasts.error($_(isSpamFolder ? 'toast.failedToMarkAsNotSpam' : 'toast.failedToMarkAsSpam'))
    }
  }

  async function handleToggleStar() {
    try {
      if (isStarred) {
        await Unstar(messageIds)
        toasts.success($_('toast.starRemoved'))
      } else {
        await Star(messageIds)
        toasts.success($_('toast.starred'))
      }
      onActionComplete?.()
    } catch (err) {
      console.error('Star toggle failed:', err)
      toasts.error($_('toast.failedToUpdateStar'))
    }
  }

  async function handleToggleRead() {
    try {
      if (isRead) {
        await MarkAsUnread(messageIds)
        toasts.success($_('toast.markedAsUnread'))
      } else {
        await MarkAsRead(messageIds)
        toasts.success($_('toast.markedAsRead'))
      }
      onActionComplete?.()
    } catch (err) {
      console.error('Read status toggle failed:', err)
      toasts.error($_('toast.failedToUpdateReadStatus'))
    }
  }

  async function handleUnsubscribe() {
    if (!unsubscribeStatus?.available || unsubscribeStatus.alreadyUnsubscribed) return
    await runUnsubscribeFlow(messageIds[0], {
      onUnsubscribed: () => {
        if (unsubscribeStatus) unsubscribeStatus = { ...unsubscribeStatus, alreadyUnsubscribed: true }
      },
      onMoved: () => onActionComplete?.(),
    })
  }

  // "Wrong tab" correction (Phase E): file all mail from this sender
  // under the chosen category from now on, and re-file what is already
  // stored. The list reloads via onActionComplete; there is nothing to
  // undo — no mail moves folders.
  async function handleTreatAs(category: 'personal' | 'automated') {
    if (!senderEmail) return
    try {
      await SetSenderCategoryOverride(senderEmail, category)
      toasts.success(
        $_(category === 'personal' ? 'toast.senderTreatedAsPersonal' : 'toast.senderTreatedAsAutomated', {
          values: { sender: senderEmail },
        })
      )
      onActionComplete?.()
    } catch (err) {
      console.error('Failed to set sender category override:', err)
      toasts.error($_('toast.failedToSetSenderCategory'))
    }
  }

  function openMoveTo() {
    folderPickerMode = 'move'
    showFolderPicker = true
  }

  function openCopyTo() {
    folderPickerMode = 'copy'
    showFolderPicker = true
  }

  // Exported for keyboard access (Alt+M / Alt+C): toggles the picker without
  // opening the context menu. Same-mode press closes; other-mode press
  // switches the dialog in place (title is reactive to folderPickerMode).
  export function isFolderPickerOpen(): boolean {
    return showFolderPicker
  }

  export function toggleFolderPicker(mode: 'move' | 'copy') {
    if (showFolderPicker && folderPickerMode === mode) {
      showFolderPicker = false
      return
    }
    if (showFolderPicker) {
      folderPickerMode = mode
      return
    }
    loadAccounts() // normally triggered by menu open; needed when bypassing the menu
    folderPickerMode = mode
    showFolderPicker = true
  }

  function handleFolderSelected(folderId: string, folderName: string, _destAccountId: string) {
    showFolderPicker = false
    switch (folderPickerMode) {
      case 'move':
        handleMoveTo(folderId, folderName)
        break
      case 'copy':
        handleCopyTo(folderId, folderName)
        break
    }
  }

  async function handleMoveTo(destFolderId: string, folderName: string) {
    try {
      await MoveToFolder(messageIds, destFolderId)
      toasts.success($_('toast.movedTo', { values: { folder: folderName } }), [{ label: $_('common.undo'), onClick: handleUndo }])
      onActionComplete?.(true)
    } catch (err) {
      console.error('Move failed:', err)
      toasts.error($_('toast.failedToMove'))
    }
  }

  async function handleCopyTo(destFolderId: string, folderName: string) {
    try {
      await CopyToFolder(messageIds, destFolderId)
      toasts.success($_('toast.copyingTo', { values: { folder: folderName } }))
      // CopyToFolder syncs in background; sidebar count + dest folder list refresh ride on folder:synced.
    } catch (err) {
      console.error('Copy failed:', err)
      toasts.error($_('toast.failedToCopy'))
    }
  }
</script>

<ContextMenuPrimitive.Root onOpenChange={handleOpenChange}>
  <ContextMenuPrimitive.Trigger>
    {#if children}
      {@render children()}
    {/if}
  </ContextMenuPrimitive.Trigger>

  <ContextMenuContent>
    <!-- Reply actions (single message only) -->
    {#if isSingleMessage}
      <ContextMenuItem onSelect={handleReply}>
        <Icon icon="mdi:reply" class="mr-2 h-4 w-4" />
        {$_('contextMenu.reply')}
      </ContextMenuItem>
      <ContextMenuItem onSelect={handleReplyAll}>
        <Icon icon="mdi:reply-all" class="mr-2 h-4 w-4" />
        {$_('contextMenu.replyAll')}
      </ContextMenuItem>
      <ContextMenuItem onSelect={handleForward}>
        <Icon icon="mdi:share" class="mr-2 h-4 w-4" />
        {$_('contextMenu.forward')}
      </ContextMenuItem>
      <ContextMenuSeparator />
    {/if}

    <!-- Move/Delete actions -->
    <ContextMenuItem onSelect={handleArchive}>
      <Icon icon="mdi:archive-outline" class="mr-2 h-4 w-4" />
      {$_('contextMenu.archive')}
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleDelete}>
      <Icon icon={isTrashFolder ? 'mdi:delete-forever' : 'mdi:delete-outline'} class="mr-2 h-4 w-4" />
      {$_(isTrashFolder ? 'contextMenu.deletePermanently' : 'contextMenu.delete')}
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleSpam}>
      <Icon icon={isSpamFolder ? 'mdi:email-check-outline' : 'mdi:alert-octagon-outline'} class="mr-2 h-4 w-4" />
      {$_(isSpamFolder ? 'contextMenu.markAsNotSpam' : 'contextMenu.markAsSpam')}
    </ContextMenuItem>

    {#if unsubscribeStatus}
      <ContextMenuItem onSelect={handleUnsubscribe} disabled={unsubscribeStatus.alreadyUnsubscribed}>
        <Icon icon="mdi:email-remove-outline" class="mr-2 h-4 w-4" />
        {unsubscribeStatus.alreadyUnsubscribed
          ? $_('contextMenu.unsubscribed')
          : isSpamFolder
            ? $_('contextMenu.unsubscribeSpam')
            : $_('contextMenu.unsubscribe')}
      </ContextMenuItem>
    {/if}

    <ContextMenuSeparator />

    <!-- Move to folder picker -->
    <ContextMenuItem onSelect={openMoveTo}>
      <Icon icon="mdi:folder-move-outline" class="mr-2 h-4 w-4" />
      {$_('contextMenu.moveTo')}
    </ContextMenuItem>

    <!-- Copy to folder picker -->
    <ContextMenuItem onSelect={openCopyTo}>
      <Icon icon="mdi:content-copy" class="mr-2 h-4 w-4" />
      {$_('contextMenu.copyTo')}
    </ContextMenuItem>

    <ContextMenuSeparator />

    <!-- Flag actions -->
    <ContextMenuItem onSelect={handleToggleStar}>
      <Icon
        icon={isStarred ? 'mdi:star' : 'mdi:star-outline'}
        class="mr-2 h-4 w-4 {isStarred ? 'text-yellow-500' : ''}"
      />
      {$_(isStarred ? 'contextMenu.removeStar' : 'contextMenu.star')}
    </ContextMenuItem>
    <ContextMenuItem onSelect={handleToggleRead}>
      <Icon
        icon={isRead ? 'mdi:email-outline' : 'mdi:email-open-outline'}
        class="mr-2 h-4 w-4"
      />
      {$_(isRead ? 'contextMenu.markAsUnread' : 'contextMenu.markAsRead')}
    </ContextMenuItem>

    {#if isSingleMessage && senderEmail}
      <ContextMenuSeparator />
      <!-- "Wrong tab" correction (Phase E): the sender override beats
           every heuristic, for all future mail from this sender. -->
      <ContextMenuItem onSelect={() => handleTreatAs('personal')}>
        <Icon icon="mdi:account-outline" class="mr-2 h-4 w-4" />
        {$_('contextMenu.treatAsPersonal')}
      </ContextMenuItem>
      <ContextMenuItem onSelect={() => handleTreatAs('automated')}>
        <Icon icon="mdi:robot-outline" class="mr-2 h-4 w-4" />
        {$_('contextMenu.treatAsAutomated')}
      </ContextMenuItem>
    {/if}
  </ContextMenuContent>
</ContextMenuPrimitive.Root>

<!-- Permanent Delete Confirmation Dialog -->
<ConfirmDialog
  bind:open={showDeleteConfirm}
  title={$_('dialog.deletePermanently')}
  description={$_('dialog.deleteDescription')}
  confirmLabel={$_('dialog.confirmDeletePermanently')}
  variant="destructive"
  onConfirm={handleConfirmPermanentDelete}
  onCancel={() => (showDeleteConfirm = false)}
/>

<!-- Folder Picker Dialog -->
<FolderPickerDialog
  bind:open={showFolderPicker}
  title={$_(folderPickerMode === 'move' ? 'contextMenu.moveTo' : 'contextMenu.copyTo')}
  initialAccountId={accountId}
  {accounts}
  excludeFolderId={currentFolderId}
  onSelect={handleFolderSelected}
/>
