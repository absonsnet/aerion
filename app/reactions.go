package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/absonsnet/daak/internal/logging"
	"github.com/absonsnet/daak/internal/reaction"
	"github.com/absonsnet/daak/internal/smtp"
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

// RemoveOwnReaction deletes only the current user's own reaction on a
// message (internal id) from the local store. It sends no email: a
// reaction already sent to the original sender cannot be retracted from
// their mailbox — removing it only hides it here.
func (a *App) RemoveOwnReaction(messageID string) error {
	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return fmt.Errorf("message not found: %s", messageID)
	}

	own := a.ownEmailsForAccount(msg.AccountID)
	emails := make([]string, 0, len(own))
	for email := range own {
		emails = append(emails, email)
	}
	if _, err := a.reactionStore.RemoveReaction(messageID, emails); err != nil {
		return fmt.Errorf("failed to remove reaction: %w", err)
	}
	return nil
}

// SendReaction sends an emoji reaction to a message (internal id) as a
// reaction email addressed to the original sender only, saves it to the
// Sent folder, and records the user's own reaction immediately so the UI
// shows it without waiting for a sync round-trip.
//
// Limits are enforced before anything is sent (see
// reaction.ValidateReaction): the emoji must be a single emoji, the
// original must not be encrypted, group-addressed, over 20 recipients,
// or one the user was BCC'd on. A limit violation returns a distinct
// error the UI surfaces.
func (a *App) SendReaction(messageID string, emoji string) error {
	return a.sendReaction(a.ctx, messageID, emoji)
}

func (a *App) sendReaction(ctx context.Context, messageID, emoji string) error {
	log := logging.WithComponent("app")

	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return fmt.Errorf("message not found: %s", messageID)
	}
	if strings.TrimSpace(msg.FromEmail) == "" {
		return fmt.Errorf("original message has no sender to react to")
	}

	// From is the identity the original was addressed to, else the
	// default identity — the same resolution a reply uses.
	from := smtp.Address{}
	identities, _ := a.accountStore.GetIdentities(msg.AccountID)
	if ident := selectReplyFromIdentity(identities, msg); ident != nil {
		from = smtp.Address{Name: ident.Name, Address: ident.Email}
	} else if acc, accErr := a.accountStore.Get(msg.AccountID); accErr == nil && acc != nil {
		from = smtp.Address{Name: acc.Name, Address: acc.Email}
	}
	if from.Address == "" {
		return fmt.Errorf("no sender identity for account %s", msg.AccountID)
	}

	orig := reaction.Original{
		MessageID:      msg.MessageID,
		Subject:        msg.Subject,
		FromName:       msg.FromName,
		FromEmail:      msg.FromEmail,
		To:             addressStrings(parseAddressList(msg.ToList)),
		Cc:             addressStrings(parseAddressList(msg.CcList)),
		Bcc:            addressStrings(parseAddressList(msg.BccList)),
		SMIMEEncrypted: msg.SMIMEEncrypted,
		PGPEncrypted:   msg.PGPEncrypted,
	}
	if err := reaction.ValidateReaction(emoji, orig, a.ownEmailsForAccount(msg.AccountID)); err != nil {
		return err
	}

	raw, reactionMessageID, err := reaction.BuildMessage(reaction.BuildOptions{
		Emoji:       emoji,
		SenderName:  from.Name,
		SenderEmail: from.Address,
		Original:    orig,
		References:  threadReferences(msg),
	})
	if err != nil {
		return fmt.Errorf("failed to build reaction message: %w", err)
	}

	// Send through the normal SMTP path (same client config, auth, and
	// TLS as a regular send). ComposeMessage cannot express the
	// reaction's multipart structure, so the pre-built bytes go through
	// sendRawMessage directly.
	to := smtp.Address{Name: msg.FromName, Address: strings.TrimSpace(msg.FromEmail)}
	if err := a.composeOps.sendRawMessage(ctx, msg.AccountID, from.Address, []string{to.Address}, raw); err != nil {
		return fmt.Errorf("failed to send reaction: %w", err)
	}

	// Save to Sent via IMAP APPEND exactly like a normal send: skipped
	// when the provider auto-saves Sent mail, and a save failure only
	// warns — the send already succeeded, and the Sent sync will pick
	// the message up later either way.
	if acc, accErr := a.accountStore.Get(msg.AccountID); accErr == nil && acc != nil && !providerAutoSavesSentMail(acc.IMAPHost) {
		if err := a.composeOps.saveToSentFolder(ctx, msg.AccountID, acc, raw); err != nil {
			log.Warn().Err(err).Str("accountID", msg.AccountID).Msg("Failed to save reaction to Sent folder")
		}
	}

	// Record immediately under the reaction email's own Message-ID, so
	// the UI shows it at once and a later Sent-folder sync dedups
	// against the same source id instead of double-recording.
	rec := &reaction.Reaction{
		MessageID:       msg.ID,
		TargetMessageID: msg.MessageID,
		ReactorEmail:    from.Address,
		ReactorName:     from.Name,
		Emoji:           emoji,
		CreatedAt:       time.Now().UTC(),
		SourceMessageID: reactionMessageID,
	}
	if err := a.reactionStore.Record(rec); err != nil {
		return fmt.Errorf("reaction sent but failed to record: %w", err)
	}

	log.Info().
		Str("messageID", messageID).
		Str("emoji", emoji).
		Str("from", from.Address).
		Str("to", to.Address).
		Msg("Reaction sent")
	return nil
}

// addressStrings extracts plain email strings from parsed addresses.
func addressStrings(addrs []smtp.Address) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if strings.TrimSpace(addr.Address) != "" {
			out = append(out, strings.TrimSpace(addr.Address))
		}
	}
	return out
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
