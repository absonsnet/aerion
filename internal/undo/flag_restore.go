package undo

import (
	"context"
	"fmt"

	"github.com/emersion/go-imap/v2"
)

// FlagRestoreCommand restores a previous flag state, resolving the
// message's CURRENT uid at undo time (unlike FlagChangeCommand, which
// replays the uid captured when the action ran). That matters after a
// move: IMAP COPY assigns a new UID in the destination folder, so the
// old UID would flag the wrong message — or nothing at all.
//
// The IMAP step is best-effort: when the message still has a temporary
// local UID (the destination sync hasn't assigned a real one yet) or the
// server is unreachable, the local flag is still restored and the next
// flag sync reconciles the server. The local restore is what the user
// sees, so it must not fail because the network did.
type FlagRestoreCommand struct {
	BaseCommand
	ctx           context.Context
	undoCtx       UndoContext
	accountID     string
	folderPath    string // folder the message is currently in
	messageIDs    []string
	flagType      string // "read" or "starred"
	previousState bool
}

// NewFlagRestoreCommand creates a flag-restore command.
func NewFlagRestoreCommand(
	ctx context.Context,
	undoCtx UndoContext,
	accountID, folderPath string,
	messageIDs []string,
	flagType string,
	previousState bool,
	description string,
) *FlagRestoreCommand {
	return &FlagRestoreCommand{
		BaseCommand:   NewBaseCommand(description),
		ctx:           ctx,
		undoCtx:       undoCtx,
		accountID:     accountID,
		folderPath:    folderPath,
		messageIDs:    messageIDs,
		flagType:      flagType,
		previousState: previousState,
	}
}

// Execute performs the action (already done at creation time).
func (c *FlagRestoreCommand) Execute() error { return nil }

// Undo restores the previous flag state, locally always and on IMAP when
// the message has a real server UID.
func (c *FlagRestoreCommand) Undo() error {
	var flag imap.Flag
	switch c.flagType {
	case "read":
		flag = imap.FlagSeen
	case "starred":
		flag = imap.FlagFlagged
	default:
		return fmt.Errorf("unknown flag type: %s", c.flagType)
	}

	// Resolve the current UID now — the message may have moved since the
	// action ran. A temp (<=0) UID has no server counterpart: skip IMAP.
	if uid, _, err := c.undoCtx.ResolveMessageUID(c.messageIDs[0]); err == nil && uid > 0 {
		if client, release, err := c.undoCtx.GetIMAPConnectionForUndo(c.ctx, c.accountID); err == nil {
			func() {
				defer release()
				if _, err := client.SelectMailbox(c.ctx, c.folderPath); err != nil {
					return
				}
				uids := []imap.UID{imap.UID(uid)}
				if c.previousState {
					_ = client.AddMessageFlags(uids, []imap.Flag{flag})
				} else {
					_ = client.RemoveMessageFlags(uids, []imap.Flag{flag})
				}
			}()
		}
	}

	var isRead, isStarred *bool
	switch c.flagType {
	case "read":
		isRead = &c.previousState
	case "starred":
		isStarred = &c.previousState
	}
	if err := c.undoCtx.UpdateLocalFlags(c.messageIDs, isRead, isStarred); err != nil {
		return fmt.Errorf("failed to update local flags: %w", err)
	}
	return nil
}
