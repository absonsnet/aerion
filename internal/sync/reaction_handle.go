package sync

import (
	"database/sql"
	"errors"
	"time"

	"github.com/hkdb/aerion/internal/reaction"
)

// handleReaction records a detected reaction for the message with the given
// internal id. It returns true when the reaction was linked to its original
// (consumed: the reaction email is hidden and must not get a body update),
// false when the email stays a regular message — either the original is
// not in the DB yet (recorded pending; linked and hidden later when the
// original syncs) or reactions are not wired up / the email has no
// In-Reply-To to link against.
func (e *Engine) handleReaction(messageID string, detected *DetectedReaction) bool {
	if e.reactionStore == nil || detected == nil {
		return false
	}

	msg, err := e.messageStore.Get(messageID)
	if err != nil {
		e.log.Warn().Err(err).Str("messageID", messageID).Msg("Reaction: cannot load reaction email, leaving as regular message")
		return false
	}
	if msg.InReplyTo == "" {
		e.log.Debug().Str("messageID", messageID).Msg("Reaction: no In-Reply-To, leaving as regular message")
		return false
	}

	sourceID := msg.MessageID
	if sourceID == "" {
		// No Message-ID on the reaction email: fall back to the internal
		// id so dedup still has a stable, unique key.
		sourceID = "internal:" + messageID
	}
	createdAt := msg.Date
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	originalID, err := e.messageStore.GetInternalIDByMessageID(msg.AccountID, msg.InReplyTo)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		e.log.Warn().Err(err).Str("inReplyTo", msg.InReplyTo).Msg("Reaction: original lookup failed, leaving as regular message")
		return false
	}

	rec := &reaction.Reaction{
		MessageID:       originalID, // "" when the original is not in the DB yet
		TargetMessageID: msg.InReplyTo,
		ReactorEmail:    msg.FromEmail,
		ReactorName:     msg.FromName,
		Emoji:           detected.Emoji,
		CreatedAt:       createdAt,
		SourceMessageID: sourceID,
	}
	if err := e.reactionStore.Record(rec); err != nil {
		e.log.Warn().Err(err).Str("messageID", messageID).Msg("Reaction: failed to record, leaving as regular message")
		return false
	}

	if originalID == "" {
		e.log.Debug().
			Str("messageID", messageID).
			Str("targetMessageID", msg.InReplyTo).
			Msg("Reaction: original not synced yet, recorded pending")
		return false
	}

	// Linked: hide the reaction email so it never appears in the message
	// list. MarkHidden also marks it read and body-fetched, so it is
	// never re-fetched for a body. A failure here is not fatal — the
	// message stays unhidden with body_fetched=0, so the next body sync
	// re-detects it and retries the hide.
	if err := e.messageStore.MarkHidden(messageID); err != nil {
		e.log.Warn().Err(err).Str("messageID", messageID).Msg("Reaction: recorded but failed to hide reaction email, will retry on next body fetch")
	}
	e.log.Debug().
		Str("messageID", messageID).
		Str("originalID", originalID).
		Str("emoji", detected.Emoji).
		Msg("Reaction: recorded and reaction email hidden")
	return true
}

// linkPendingReactions resolves reactions that arrived before their
// original: when a message with the given Message-ID syncs, link every
// pending reaction targeting it and hide the corresponding reaction
// emails (they stop being regular messages now that their original is
// known). Called from finalizeSavedHeaders for every newly saved header.
func (e *Engine) linkPendingReactions(accountID, messageIDHeader, internalID string) {
	if e.reactionStore == nil || messageIDHeader == "" || internalID == "" {
		return
	}
	sourceIDs, err := e.reactionStore.LinkPending(messageIDHeader, internalID)
	if err != nil {
		e.log.Warn().Err(err).Str("messageID", messageIDHeader).Msg("Failed to link pending reactions for new message")
		return
	}
	for _, sourceID := range sourceIDs {
		reactionInternalID, err := e.messageStore.GetInternalIDByMessageID(accountID, sourceID)
		if err != nil {
			// The reaction email may live in a folder that hasn't synced,
			// or may have been deleted already; its reaction is recorded
			// either way, so there is nothing more to do here.
			e.log.Debug().Err(err).Str("sourceMessageID", sourceID).Msg("Pending reaction linked but its email is not in the DB, nothing to hide")
			continue
		}
		if err := e.messageStore.MarkHidden(reactionInternalID); err != nil {
			e.log.Warn().Err(err).Str("messageID", reactionInternalID).Msg("Failed to hide newly linked reaction email")
		}
	}
}
