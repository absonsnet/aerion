package app

import (
	"fmt"
	"strings"
	"time"
)

// ============================================================================
// Message reactions API - Exposed to frontend via Wails bindings
// ============================================================================

// MessageReaction is one emoji reaction on a message, as returned to the
// frontend. IsOwn marks reactions from any of the current user's own
// addresses on the message's account (account email + identities).
type MessageReaction struct {
	Emoji        string    `json:"emoji"`
	ReactorEmail string    `json:"reactorEmail"`
	ReactorName  string    `json:"reactorName"`
	CreatedAt    time.Time `json:"createdAt"`
	IsOwn        bool      `json:"isOwn"`
}

// GetMessageReactions returns the current reactions on a message (internal
// id), oldest first. Reactions recorded before their original synced are
// linked on arrival, so this only ever returns reactions for a message
// that exists in the DB.
func (a *App) GetMessageReactions(messageID string) ([]MessageReaction, error) {
	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}

	reactions, err := a.reactionStore.ListByMessage(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to list reactions: %w", err)
	}

	ownEmails := a.ownEmailsForAccount(msg.AccountID)

	result := make([]MessageReaction, 0, len(reactions))
	for _, r := range reactions {
		result = append(result, MessageReaction{
			Emoji:        r.Emoji,
			ReactorEmail: r.ReactorEmail,
			ReactorName:  r.ReactorName,
			CreatedAt:    r.CreatedAt,
			IsOwn:        ownEmails[strings.ToLower(strings.TrimSpace(r.ReactorEmail))],
		})
	}
	return result, nil
}

// ownEmailsForAccount returns the set of the user's own addresses for an
// account: the account email plus every identity (alias) on it. Lookup
// failures degrade to an empty set rather than failing the whole call —
// IsOwn just reports false for every reaction then.
func (a *App) ownEmailsForAccount(accountID string) map[string]bool {
	own := make(map[string]bool)
	if acc, err := a.accountStore.Get(accountID); err == nil && acc != nil && acc.Email != "" {
		own[strings.ToLower(strings.TrimSpace(acc.Email))] = true
	}
	if identities, err := a.accountStore.GetIdentities(accountID); err == nil {
		for _, ident := range identities {
			if ident.Email != "" {
				own[strings.ToLower(strings.TrimSpace(ident.Email))] = true
			}
		}
	}
	return own
}
