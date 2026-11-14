// Package reaction stores email reactions: one emoji reaction per reactor
// per message, recorded from reaction emails received during sync or sent
// by this client (see send.go), and validates the emoji itself.
//
// A reaction is a separate email pointing back at the original via the
// In-Reply-To header. Two wire formats are detected (see internal/sync):
// Gmail's text/vnd.google.email-reaction+json part and RFC 9078
// (Content-Disposition: reaction).
package reaction

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/logging"
	"github.com/rs/zerolog"
)

// Reaction is one stored emoji reaction on a message.
type Reaction struct {
	// ID is a UUID primary key.
	ID string
	// MessageID is the internal id of the reacted-to message. Empty while
	// the reaction is still pending (original not synced yet) — see
	// TargetMessageID.
	MessageID string
	// TargetMessageID is the original message's Message-ID header value
	// (angle brackets stripped), used to link out-of-order reactions.
	TargetMessageID string
	// ReactorEmail is the reactor's address, lowercased at record time.
	ReactorEmail string
	// ReactorName is the reactor's display name, if any.
	ReactorName string
	// Emoji is exactly one Unicode emoji, validated by the sync parser.
	Emoji string
	// CreatedAt is the reaction email's Date header (when the reaction was
	// sent), falling back to record time when that is unavailable.
	CreatedAt time.Time
	// SourceMessageID is the reaction email's own Message-ID header value,
	// used to dedup re-recordings of the same email.
	SourceMessageID string
}

// Store provides reaction persistence operations.
type Store struct {
	db  *database.DB
	log zerolog.Logger
}

// NewStore creates a new reaction store.
func NewStore(db *database.DB) *Store {
	return &Store{
		db:  db,
		log: logging.WithComponent("reaction-store"),
	}
}

// normalizeMessageID strips surrounding angle brackets and whitespace so
// In-Reply-To values and Message-ID values compare equal regardless of
// which form the server sent.
func normalizeMessageID(msgID string) string {
	msgID = strings.TrimSpace(msgID)
	msgID = strings.TrimPrefix(msgID, "<")
	msgID = strings.TrimSuffix(msgID, ">")
	return strings.TrimSpace(msgID)
}

// Record stores a reaction, enforcing one current reaction per reactor per
// message: any earlier reaction from the same reactor to the same target
// is replaced. Re-recording the same reaction email (same
// SourceMessageID) is a no-op. MessageID may be empty for a pending
// reaction whose original has not synced yet.
func (s *Store) Record(r *Reaction) error {
	if r == nil {
		return fmt.Errorf("nil reaction")
	}
	if r.TargetMessageID == "" {
		return fmt.Errorf("reaction has no target message id")
	}
	if r.ReactorEmail == "" {
		return fmt.Errorf("reaction has no reactor email")
	}
	if r.Emoji == "" {
		return fmt.Errorf("reaction has no emoji")
	}
	if r.SourceMessageID == "" {
		return fmt.Errorf("reaction has no source message id")
	}

	r.TargetMessageID = normalizeMessageID(r.TargetMessageID)
	r.ReactorEmail = strings.ToLower(strings.TrimSpace(r.ReactorEmail))
	r.SourceMessageID = normalizeMessageID(r.SourceMessageID)
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Drop the reactor's earlier reaction to this target (linked or still
	// pending), so only the newest one is current. The row being recorded
	// is excluded so a re-record keeps its id and any link it gained.
	if _, err := tx.Exec(
		`DELETE FROM reactions WHERE target_message_id = ? AND reactor_email = ? AND source_message_id != ?`,
		r.TargetMessageID, r.ReactorEmail, r.SourceMessageID,
	); err != nil {
		return fmt.Errorf("failed to replace previous reaction: %w", err)
	}

	// ON CONFLICT DO NOTHING: re-recording the same reaction email (e.g.
	// its body is re-fetched) keeps the existing row, including a
	// message_id link applied since the first record.
	if _, err := tx.Exec(
		`INSERT INTO reactions (id, message_id, target_message_id, reactor_email, reactor_name, emoji, created_at, source_message_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(source_message_id) DO NOTHING`,
		r.ID, nullString(r.MessageID), r.TargetMessageID, r.ReactorEmail,
		nullString(r.ReactorName), r.Emoji, r.CreatedAt, r.SourceMessageID,
	); err != nil {
		return fmt.Errorf("failed to insert reaction: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// LinkPending links every still-unlinked reaction targeting the given
// Message-ID to the message's internal id, and returns the
// source_message_ids of the reactions it linked (the reaction emails'
// own Message-IDs, so the caller can hide those rows). Call when a new
// message syncs, to resolve reactions that arrived before their original.
func (s *Store) LinkPending(targetMessageID, messageID string) ([]string, error) {
	targetMessageID = normalizeMessageID(targetMessageID)
	if targetMessageID == "" || messageID == "" {
		return nil, nil
	}

	rows, err := s.db.Query(
		`SELECT source_message_id FROM reactions WHERE target_message_id = ? AND message_id IS NULL`,
		targetMessageID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending reactions: %w", err)
	}
	var sourceIDs []string
	for rows.Next() {
		var sourceID string
		if err := rows.Scan(&sourceID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan pending reaction: %w", err)
		}
		sourceIDs = append(sourceIDs, sourceID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("failed to read pending reactions: %w", err)
	}
	rows.Close()

	if len(sourceIDs) == 0 {
		return nil, nil
	}

	if _, err := s.db.Exec(
		`UPDATE reactions SET message_id = ? WHERE target_message_id = ? AND message_id IS NULL`,
		messageID, targetMessageID,
	); err != nil {
		return nil, fmt.Errorf("failed to link pending reactions: %w", err)
	}

	s.log.Debug().
		Str("targetMessageID", targetMessageID).
		Str("messageID", messageID).
		Int("linked", len(sourceIDs)).
		Msg("Linked pending reactions to newly synced message")
	return sourceIDs, nil
}

// RemoveReaction deletes the reactions on a message (internal id) from the
// given reactor emails, and reports whether any row was deleted. It only
// ever deletes local rows — it sends nothing, so a reaction already sent
// to the original sender stays sent. Callers pass the current user's own
// addresses (see App.RemoveOwnReaction), so only the user's own reaction
// can be removed through this path.
func (s *Store) RemoveReaction(messageID string, reactorEmails []string) (bool, error) {
	if strings.TrimSpace(messageID) == "" {
		return false, fmt.Errorf("reaction has no message id")
	}
	if len(reactorEmails) == 0 {
		return false, nil
	}
	placeholders := make([]string, 0, len(reactorEmails))
	args := make([]interface{}, 0, len(reactorEmails)+1)
	args = append(args, messageID)
	for _, email := range reactorEmails {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, email)
	}
	if len(placeholders) == 0 {
		return false, nil
	}
	res, err := s.db.Exec(
		`DELETE FROM reactions WHERE message_id = ? AND reactor_email IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return false, fmt.Errorf("failed to delete reaction: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to count deleted reactions: %w", err)
	}
	return affected > 0, nil
}

// ListByMessage returns the current reactions on a message (internal id),
// oldest first.
func (s *Store) ListByMessage(messageID string) ([]*Reaction, error) {
	rows, err := s.db.Query(
		`SELECT id, message_id, target_message_id, reactor_email, reactor_name, emoji, created_at, source_message_id
		 FROM reactions WHERE message_id = ? ORDER BY created_at ASC, id ASC`,
		messageID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query reactions: %w", err)
	}
	defer rows.Close()

	var reactions []*Reaction
	for rows.Next() {
		r := &Reaction{}
		var messageIDNS, reactorNameNS sql.NullString
		var createdAtStr sql.NullString
		if err := rows.Scan(
			&r.ID, &messageIDNS, &r.TargetMessageID, &r.ReactorEmail,
			&reactorNameNS, &r.Emoji, &createdAtStr, &r.SourceMessageID,
		); err != nil {
			return nil, fmt.Errorf("failed to scan reaction: %w", err)
		}
		if messageIDNS.Valid {
			r.MessageID = messageIDNS.String
		}
		if reactorNameNS.Valid {
			r.ReactorName = reactorNameNS.String
		}
		if createdAtStr.Valid && createdAtStr.String != "" {
			r.CreatedAt = parseTimeString(createdAtStr.String)
		}
		reactions = append(reactions, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read reactions: %w", err)
	}
	return reactions, nil
}

// CountPending returns how many reactions are still waiting for their
// original message to sync. Exposed for diagnostics/tests.
func (s *Store) CountPending() (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM reactions WHERE message_id IS NULL`).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count pending reactions: %w", err)
	}
	return count, nil
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// parseTimeString parses the time formats SQLite may return for DATETIME
// columns (RFC3339 and Go's default time.String layout).
func parseTimeString(s string) time.Time {
	for _, format := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	} {
		if parsed, err := time.Parse(format, s); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
