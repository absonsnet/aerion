package translate

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/absonsnet/daak/internal/database"
)

// Translation cache (message_translations, migration 47).
//
// The cache is keyed by message + target language + engine, and stores
// the translated subject and body. Re-opening a message reuses the
// cache. A row is only reused when the body's hash still matches: if
// the message body changed (re-sync, decryption on view), the cached
// translation is stale and the message is translated again.
//
// The translated body stored here is the translated HTML the viewer
// renders (same markup as the original, only text nodes changed), so
// re-opening is instant with no engine work.

// CachedTranslation is one cache row.
type CachedTranslation struct {
	Engine           string `json:"engine"`
	TranslatedSubject string `json:"translatedSubject"`
	TranslatedBody   string `json:"translatedBody"`
}

// BodyHash hashes the message content a translation depends on. Any
// change to subject or body invalidates cached translations.
func BodyHash(subject, bodyText, bodyHTML string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(subject))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(bodyText))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(bodyHTML))
	return hex.EncodeToString(h.Sum(nil))
}

// GetCached returns the cached translation for a message + target +
// engine, but only when the body hash matches the message's current
// body. found is false when there is no row or the body changed.
func GetCached(db *database.DB, messageID, targetLang, engine, bodyHash string) (*CachedTranslation, bool, error) {
	var subject, body, storedHash sql.NullString
	err := db.QueryRow(`
		SELECT translated_subject, translated_body, body_hash
		FROM message_translations
		WHERE message_id = ? AND target_lang = ? AND engine = ?
	`, messageID, targetLang, engine).Scan(&subject, &body, &storedHash)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to read translation cache: %w", err)
	}
	// Rows written before migration 49 have no hash: they are usable
	// only when the caller passes an empty hash (no body to compare).
	if storedHash.Valid && storedHash.String != "" && bodyHash != "" && storedHash.String != bodyHash {
		return nil, false, nil
	}
	return &CachedTranslation{
		Engine:            engine,
		TranslatedSubject: subject.String,
		TranslatedBody:    body.String,
	}, true, nil
}

// GetCachedAnyEngine returns the cached translation for a message +
// target in any engine, preferring the given engine order. The banner
// names the engine that made the cached translation.
func GetCachedAnyEngine(db *database.DB, messageID, targetLang, bodyHash string, prefer []string) (*CachedTranslation, bool, error) {
	rows, err := db.Query(`
		SELECT engine, translated_subject, translated_body, body_hash
		FROM message_translations
		WHERE message_id = ? AND target_lang = ?
	`, messageID, targetLang)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read translation cache: %w", err)
	}
	defer rows.Close()
	type row struct {
		engine, subject, body string
		hash                  sql.NullString
	}
	var found []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.engine, &r.subject, &r.body, &r.hash); err != nil {
			return nil, false, err
		}
		if r.hash.Valid && r.hash.String != "" && bodyHash != "" && r.hash.String != bodyHash {
			continue // stale: the body changed since this was translated
		}
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(found) == 0 {
		return nil, false, nil
	}
	// Prefer the requested engine order, else the first row.
	for _, want := range prefer {
		for _, r := range found {
			if r.engine == want {
				return &CachedTranslation{Engine: r.engine, TranslatedSubject: r.subject, TranslatedBody: r.body}, true, nil
			}
		}
	}
	r := found[0]
	return &CachedTranslation{Engine: r.engine, TranslatedSubject: r.subject, TranslatedBody: r.body}, true, nil
}

// SaveCached stores a translation, replacing any earlier row for the
// same message + target + engine.
func SaveCached(db *database.DB, messageID, targetLang, engine, subject, body, bodyHash string) error {
	_, err := db.Exec(`
		INSERT INTO message_translations (message_id, target_lang, engine, translated_subject, translated_body, body_hash)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_id, target_lang, engine) DO UPDATE SET
			translated_subject = excluded.translated_subject,
			translated_body = excluded.translated_body,
			body_hash = excluded.body_hash,
			created_at = CURRENT_TIMESTAMP
	`, messageID, targetLang, engine, subject, body, bodyHash)
	if err != nil {
		return fmt.Errorf("failed to save translation cache: %w", err)
	}
	return nil
}
