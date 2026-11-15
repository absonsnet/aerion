// Viewer bindings indirection.
//
// ConversationViewer (and its EmailBody / AttachmentList children) call
// backend methods that exist on BOTH App (main window) and ViewerApp
// (detached popup window) under the same names. The components import
// from here instead of `wailsjs/go/app/App` directly; this module
// dispatches each call to the binding set of the window it runs in, based
// on the `__DAAK_VIEWER_WINDOW__` flag viewerMain.ts sets before
// mounting. The main window never calls ViewerApp methods and vice versa
// — the unused module's functions are only ever imported, never invoked,
// so its missing `window.go` namespace is never dereferenced there.
import * as AppBindings from '../../wailsjs/go/app/App.js'
import * as ViewerBindings from '../../wailsjs/go/app/ViewerApp.js'
import type { message, app } from '../../wailsjs/go/models'

export function isViewerWindow(): boolean {
  return (
    (window as unknown as { __DAAK_VIEWER_WINDOW__?: boolean })
      .__DAAK_VIEWER_WINDOW__ === true
  )
}

function call<T>(name: string, ...args: unknown[]): Promise<T> {
  const bindings = isViewerWindow() ? ViewerBindings : AppBindings
  const fn = (bindings as Record<string, unknown>)[name]
  if (typeof fn !== 'function') {
    return Promise.reject(
      new Error(`Viewer binding ${name} is not available in this window`)
    )
  }
  return (fn as (...a: unknown[]) => Promise<T>)(...args)
}

// — Conversation —
export const GetConversation = (
  threadId: string,
  folderId: string
): Promise<message.Conversation> => call('GetConversation', threadId, folderId)
export const GetReadReceiptResponsePolicy = (): Promise<string> =>
  call('GetReadReceiptResponsePolicy')
export const SendReadReceipt = (
  accountId: string,
  messageId: string
): Promise<void> => call('SendReadReceipt', accountId, messageId)
export const IgnoreReadReceipt = (
  accountId: string,
  messageId: string
): Promise<void> => call('IgnoreReadReceipt', accountId, messageId)
export const GetMarkAsReadDelay = (): Promise<number> => call('GetMarkAsReadDelay')
export const GetMessageSource = (messageId: string): Promise<string> =>
  call('GetMessageSource', messageId)
export const ProcessSMIMEMessage = (
  messageId: string
): Promise<app.SMIMEViewResult> => call('ProcessSMIMEMessage', messageId)
export const ProcessPGPMessage = (messageId: string): Promise<app.PGPViewResult> =>
  call('ProcessPGPMessage', messageId)
export const FetchMessageBody = (messageId: string): Promise<message.Message> =>
  call('FetchMessageBody', messageId)

// — Message actions —
export const MarkAsRead = (messageIds: string[]): Promise<void> =>
  call('MarkAsRead', messageIds)
export const MarkAsUnread = (messageIds: string[]): Promise<void> =>
  call('MarkAsUnread', messageIds)
export const Star = (messageIds: string[]): Promise<void> =>
  call('Star', messageIds)
export const Unstar = (messageIds: string[]): Promise<void> =>
  call('Unstar', messageIds)
export const Archive = (messageIds: string[]): Promise<void> =>
  call('Archive', messageIds)
export const Trash = (messageIds: string[]): Promise<boolean> =>
  call('Trash', messageIds)
export const MarkAsSpam = (messageIds: string[]): Promise<boolean> =>
  call('MarkAsSpam', messageIds)
export const MarkAsNotSpam = (messageIds: string[]): Promise<void> =>
  call('MarkAsNotSpam', messageIds)
export const DeletePermanently = (messageIds: string[]): Promise<void> =>
  call('DeletePermanently', messageIds)
export const Undo = (): Promise<string> => call('Undo')
// — Code chips (Phase A) —
export const ApplyCodeChipAction = (
  messageId: string,
  mode: string
): Promise<boolean> => call('ApplyCodeChipAction', messageId, mode)
export const GetCodeChipClickMode = (): Promise<string> =>
  call('GetCodeChipClickMode')
export const GetOtpClipboardClearSeconds = (): Promise<number> =>
  call('GetOtpClipboardClearSeconds')
export const GetMessageReactions = (
  messageId: string
): Promise<Array<app.MessageReaction>> => call('GetMessageReactions', messageId)
export const SendReaction = (messageId: string, emoji: string): Promise<void> =>
  call('SendReaction', messageId, emoji)
export const RemoveOwnReaction = (messageId: string): Promise<void> =>
  call('RemoveOwnReaction', messageId)

// — Unsubscribe (Phase C) —
export const GetUnsubscribeStatuses = (
  messageIds: Array<string>
): Promise<Array<app.UnsubscribeStatus>> => call('GetUnsubscribeStatuses', messageIds)
export const UnsubscribeFromMessage = (
  messageId: string
): Promise<app.UnsubscribeResult> => call('UnsubscribeFromMessage', messageId)
export const UnsubscribeFollowUp = (
  messageId: string,
  moveExisting: boolean,
  autoDelete: boolean
): Promise<void> => call('UnsubscribeFollowUp', messageId, moveExisting, autoDelete)
export const GetSubscriptions = (): Promise<Array<app.SubscriptionSender>> =>
  call('GetSubscriptions')
export const UnsubscribeFromSubscription = (
  senderEmail: string,
  accountId: string
): Promise<app.UnsubscribeResult> => call('UnsubscribeFromSubscription', senderEmail, accountId)
export const SetSenderAutoDelete = (
  senderEmail: string,
  accountId: string,
  enabled: boolean
): Promise<void> => call('SetSenderAutoDelete', senderEmail, accountId, enabled)

// — Email body —
export const GetInlineAttachments = (
  messageId: string
): Promise<Record<string, string>> => call('GetInlineAttachments', messageId)
export const AddImageAllowlist = (
  entryType: string,
  value: string
): Promise<void> => call('AddImageAllowlist', entryType, value)
export const OpenURL = (url: string): Promise<void> => call('OpenURL', url)

// — Attachments —
export const GetAttachments = (
  messageId: string
): Promise<Array<message.Attachment>> => call('GetAttachments', messageId)
export const SaveAttachmentAs = (attachmentId: string): Promise<string> =>
  call('SaveAttachmentAs', attachmentId)
export const SaveAllAttachments = (messageId: string): Promise<string> =>
  call('SaveAllAttachments', messageId)
export const OpenAttachment = (attachmentId: string): Promise<void> =>
  call('OpenAttachment', attachmentId)
export const SaveEncryptedAttachmentAs = (
  messageId: string,
  filename: string
): Promise<string> => call('SaveEncryptedAttachmentAs', messageId, filename)
export const OpenEncryptedAttachment = (
  messageId: string,
  filename: string
): Promise<void> => call('OpenEncryptedAttachment', messageId, filename)
export const SaveAllEncryptedAttachments = (messageId: string): Promise<string> =>
  call('SaveAllEncryptedAttachments', messageId)
export const OpenFile = (path: string): Promise<void> => call('OpenFile', path)
export const OpenFolder = (path: string): Promise<void> => call('OpenFolder', path)
