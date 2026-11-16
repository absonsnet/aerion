package app

import (
	"context"
	"fmt"
	"time"

	"github.com/absonsnet/daak/internal/enrich"
	"github.com/absonsnet/daak/internal/folder"
	"github.com/absonsnet/daak/internal/settings"
	"github.com/absonsnet/daak/internal/undo"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ============================================================================
// Needs attention + code chips (Phase A) - Wails bindings
// ============================================================================

// AttentionItem is one Needs attention bar: the message, its type, and the
// account it belongs to. The extracted value is included so the frontend
// can render a code chip inside the bar's target row; it is never logged.
type AttentionItem struct {
	MessageID    string    `json:"messageId"`
	ThreadID     string    `json:"threadId"`
	FolderID     string    `json:"folderId"`
	AccountID    string    `json:"accountId"`
	AccountName  string    `json:"accountName"`
	AccountColor string    `json:"accountColor"`
	Subject      string    `json:"subject"`
	Type         string    `json:"type"`
	TypeLabel    string    `json:"typeLabel"`
	Date         time.Time `json:"date"`
	ExtractKind  string    `json:"extractKind,omitempty"`
	ExtractValue string    `json:"extractValue,omitempty"`
}

// GetAttentionItems returns the Needs attention bars for a folder view.
// When unified is true the bars span every inbox; otherwise folderID must
// be an inbox folder (bars never appear in Spam/Trash/other folders).
// Items are newest first. An empty slice means no strip at all.
func (a *App) GetAttentionItems(folderID string, unified bool) ([]*AttentionItem, error) {
	enabled, err := a.settingsStore.GetAttentionEnabled()
	if err != nil {
		return nil, err
	}
	if !enabled {
		return []*AttentionItem{}, nil
	}

	var folderIDs []string
	if !unified {
		f, err := a.folderStore.Get(folderID)
		if err != nil || f == nil || f.Type != folder.TypeInbox {
			return []*AttentionItem{}, nil
		}
		folderIDs = []string{folderID}
	}

	now := time.Now()
	candidates, err := a.messageStore.ListAttentionCandidates(folderIDs, now.Add(-14*24*time.Hour))
	if err != nil {
		return nil, err
	}
	dismissed, err := a.messageStore.GetAttentionDismissalIDs()
	if err != nil {
		return nil, err
	}

	items := []*AttentionItem{}
	for _, c := range candidates {
		m := c.Message
		if dismissed[m.ID] {
			continue
		}
		derived, ok := enrich.EvaluateAttention(enrich.AttentionInput{
			MessageID:   m.ID,
			AccountID:   m.AccountID,
			AccountName: c.AccountName,
			Subject:     m.Subject,
			BodyHead:    m.BodyText,
			FromEmail:   m.FromEmail,
			Category:    m.Category,
			SubCategory: m.SubCategory,
			ExtractKind: m.ExtractKind,
			Date:        m.Date,
		}, now)
		if !ok {
			continue
		}
		typeOn, err := a.settingsStore.GetAttentionTypeEnabled(derived.Type)
		if err != nil {
			return nil, err
		}
		if !typeOn {
			continue
		}
		items = append(items, &AttentionItem{
			MessageID:    m.ID,
			ThreadID:     m.ThreadID,
			FolderID:     m.FolderID,
			AccountID:    m.AccountID,
			AccountName:  c.AccountName,
			AccountColor: c.AccountColor,
			Subject:      m.Subject,
			Type:         derived.Type,
			TypeLabel:    derived.TypeLabel,
			Date:         m.Date,
			ExtractKind:  m.ExtractKind,
			ExtractValue: m.ExtractValue,
		})
	}
	return items, nil
}

// GetAttentionCount returns the live Needs attention count for the
// unified inbox (sidebar badge).
func (a *App) GetAttentionCount() (int, error) {
	items, err := a.GetAttentionItems("", true)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

// DismissAttention removes a message from the Needs attention strip only.
// The mail stays in the list untouched (still unread, still in Inbox);
// the dismissal is recorded so the bar never resurfaces, even on re-sync.
func (a *App) DismissAttention(messageID string) error {
	if err := a.messageStore.DismissAttention(messageID); err != nil {
		return err
	}
	wailsRuntime.EventsEmit(a.ctx, "attention:changed", messageID)
	return nil
}

// GetAttentionEnabled returns the Needs attention master toggle.
func (a *App) GetAttentionEnabled() (bool, error) {
	return a.settingsStore.GetAttentionEnabled()
}

// SetAttentionEnabled enables or disables the Needs attention strip.
func (a *App) SetAttentionEnabled(enabled bool) error {
	if err := a.settingsStore.SetAttentionEnabled(enabled); err != nil {
		return err
	}
	wailsRuntime.EventsEmit(a.ctx, "attention:changed", "")
	return nil
}

// GetAttentionTypeEnabled returns one attention type's toggle.
func (a *App) GetAttentionTypeEnabled(attentionType string) (bool, error) {
	return a.settingsStore.GetAttentionTypeEnabled(attentionType)
}

// SetAttentionTypeEnabled enables or disables one attention type.
func (a *App) SetAttentionTypeEnabled(attentionType string, enabled bool) error {
	if err := a.settingsStore.SetAttentionTypeEnabled(attentionType, enabled); err != nil {
		return err
	}
	wailsRuntime.EventsEmit(a.ctx, "attention:changed", "")
	return nil
}

// GetCodeChipClickMode returns the code-chip click mode.
func (a *App) GetCodeChipClickMode() (string, error) {
	return a.settingsStore.GetCodeChipClickMode()
}

// SetCodeChipClickMode sets the code-chip click mode.
func (a *App) SetCodeChipClickMode(mode string) error {
	return a.settingsStore.SetCodeChipClickMode(mode)
}

// GetOtpClipboardClearSeconds returns the clipboard-clear delay in seconds.
func (a *App) GetOtpClipboardClearSeconds() (int, error) {
	return a.settingsStore.GetOtpClipboardClearSeconds()
}

// SetOtpClipboardClearSeconds sets the clipboard-clear delay (0 = never).
func (a *App) SetOtpClipboardClearSeconds(seconds int) error {
	return a.settingsStore.SetOtpClipboardClearSeconds(seconds)
}

// ApplyCodeChipAction performs the backend half of a code-chip click. The
// frontend copies the code to the clipboard first, then calls this with
// the mode from settings ("" = use the stored mode):
//
//   - "copy_mark_read_delete" (default): mark read + move to Trash
//   - "copy_mark_read": mark read only
//   - "copy_only": nothing on the backend
//
// Reference chips never call this: they are copy-only on the frontend.
//
// Returns whether the message was moved to Trash. The Trash move pushes
// its own undo command; when the message was unread we pop it and push a
// composite that also restores the unread flag, so one Undo press fully
// reverses the chip action (clipboard is not un-copied — the toast says
// so). Reference the existing bulk-action undo stack: the same one Delete
// uses.
func (a *App) ApplyCodeChipAction(messageID string, mode string) (bool, error) {
	if mode == "" {
		var err error
		mode, err = a.settingsStore.GetCodeChipClickMode()
		if err != nil {
			return false, err
		}
	}
	switch mode {
	case settings.CodeChipClickCopyMarkReadDelete,
		settings.CodeChipClickCopyMarkRead,
		settings.CodeChipClickCopyOnly:
	default:
		return false, fmt.Errorf("invalid code chip click mode: %s", mode)
	}

	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return false, fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return false, fmt.Errorf("message not found: %s", messageID)
	}
	if msg.ExtractKind == "" {
		return false, fmt.Errorf("message has no extracted value")
	}

	wasRead := msg.IsRead
	moved := false

	if mode != settings.CodeChipClickCopyOnly && !wasRead {
		if err := a.MarkAsRead([]string{messageID}); err != nil {
			return false, fmt.Errorf("failed to mark message read: %w", err)
		}
	}

	if mode == settings.CodeChipClickCopyMarkReadDelete {
		moved, err = a.Trash([]string{messageID})
		if err != nil {
			return false, fmt.Errorf("failed to move message to Trash: %w", err)
		}
		// One Undo must restore both the folder and the unread flag.
		// MarkAsRead pushes nothing (by design, #243), so the only
		// command on the stack is Trash's move — wrap it with a flag
		// restore when the message was unread.
		if moved && !wasRead {
			moveCmd := a.undoStack.Pop()
			if moveCmd != nil {
				trashFolder, err := a.GetSpecialFolder(msg.AccountID, folder.TypeTrash)
				if err == nil && trashFolder != nil {
					// FlagRestoreCommand (not FlagChangeCommand): the
					// Trash move reassigns the UID, so the restore must
					// resolve the message's current UID at undo time.
					flagCmd := undo.NewFlagRestoreCommand(
						a.ctx, a, msg.AccountID, trashFolder.Path,
						[]string{messageID},
						"read", false,
						"Mark as unread",
					)
					a.undoStack.Push(undo.NewCompositeCommand(
						"Undo code chip action", moveCmd, flagCmd,
					))
				} else {
					a.undoStack.Push(moveCmd)
				}
			}
		}
	}

	wailsRuntime.EventsEmit(a.ctx, "attention:changed", messageID)
	return moved, nil
}

// RunEnrichmentBackfill runs the one-off enrichment backfill for
// already-synced mail (migration 46), guarded by a settings flag so it
// runs exactly once. Returns how many messages were enriched. Safe to
// call repeatedly; a failed run leaves the flag unset and retries on the
// next launch.
func (a *App) RunEnrichmentBackfill() (int, error) {
	doneKey := enrichmentBackfillDoneKey
	done, err := a.settingsStore.Get(doneKey)
	if err != nil {
		return 0, err
	}
	if done == "true" {
		return 0, nil
	}
	n, err := a.syncEngine.BackfillEnrichment(context.Background())
	if err != nil {
		return n, err
	}
	if err := a.settingsStore.Set(doneKey, "true"); err != nil {
		return n, err
	}
	return n, nil
}
