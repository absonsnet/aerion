package credentials

import (
	"database/sql"
	"fmt"

	gokeyring "github.com/zalando/go-keyring"
)

// Service secrets (Phase D): single global secrets that belong to no
// account, such as the DeepL API key. Stored in the OS keyring first
// (key "service:<name>"), with the encrypted service_secrets table
// (migration 49) as the fallback, mirroring SetPassword.

// ServiceSecretDeepL is the service name for the DeepL API key.
const ServiceSecretDeepL = "deepl-api-key"

// SetServiceSecret stores a global service secret. An empty value
// deletes it.
func (s *Store) SetServiceSecret(service, value string) error {
	if value == "" {
		return s.DeleteServiceSecret(service)
	}
	key := "service:" + service
	if s.keyringEnabled {
		if err := keyringSet(key, value); err == nil {
			s.clearDBServiceSecret(service)
			return nil
		} else {
			s.log.Warn().Err(err).Str("service", service).Msg("Failed to store service secret in OS keyring, using fallback")
		}
	}
	encrypted, err := s.encryptor.Encrypt(value)
	if err != nil {
		return fmt.Errorf("failed to encrypt service secret: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO service_secrets (service, encrypted_value) VALUES (?, ?)
		ON CONFLICT(service) DO UPDATE SET encrypted_value = excluded.encrypted_value, updated_at = CURRENT_TIMESTAMP
	`, service, encrypted)
	if err != nil {
		return fmt.Errorf("failed to store service secret: %w", err)
	}
	return nil
}

// GetServiceSecret retrieves a global service secret ("", nil when unset).
func (s *Store) GetServiceSecret(service string) (string, error) {
	key := "service:" + service
	if s.keyringEnabled {
		value, err := keyringGet(key)
		if err == nil {
			return value, nil
		}
		if err != gokeyring.ErrNotFound {
			s.log.Warn().Err(err).Str("service", service).Msg("Error reading service secret from OS keyring, trying fallback")
		}
	}
	var encrypted sql.NullString
	err := s.db.QueryRow("SELECT encrypted_value FROM service_secrets WHERE service = ?", service).Scan(&encrypted)
	if err == sql.ErrNoRows || !encrypted.Valid {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read service secret: %w", err)
	}
	return s.encryptor.Decrypt(encrypted.String)
}

// DeleteServiceSecret removes a global service secret from both stores.
func (s *Store) DeleteServiceSecret(service string) error {
	key := "service:" + service
	if s.keyringEnabled {
		_ = keyringDelete(key)
	}
	if _, err := s.db.Exec("DELETE FROM service_secrets WHERE service = ?", service); err != nil {
		return fmt.Errorf("failed to delete service secret: %w", err)
	}
	return nil
}

// clearDBServiceSecret removes the DB fallback row after a keyring
// write, so the two stores never disagree.
func (s *Store) clearDBServiceSecret(service string) {
	if _, err := s.db.Exec("DELETE FROM service_secrets WHERE service = ?", service); err != nil {
		s.log.Warn().Err(err).Str("service", service).Msg("Failed to clear fallback service secret")
	}
}
