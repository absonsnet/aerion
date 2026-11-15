// Package crypto provides encryption utilities for secure credential storage
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// keyFileName is the name of the file storing the encryption key
	keyFileName = "device.key"

	// saltSize is the size of the salt in bytes
	saltSize = 32

	// keySize is the size of the derived key in bytes (AES-256)
	keySize = 32

	// pbkdf2Iterations is the number of PBKDF2 iterations
	pbkdf2Iterations = 100000
)

// Encryptor provides AES-256-GCM encryption/decryption.
// key is the current derivation; legacyKey is the pre-rename derivation
// kept only so values encrypted by the old build still decrypt (see
// legacy.go). Encrypt always uses key; Decrypt tries key first, then
// legacyKey.
type Encryptor struct {
	key       []byte
	legacyKey []byte
}

// NewEncryptor creates a new Encryptor using a device-specific key
// The key is stored in the data directory and generated if it doesn't exist
func NewEncryptor(dataDir string) (*Encryptor, error) {
	keyPath := filepath.Join(dataDir, keyFileName)

	key, legacyKey, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load or create key: %w", err)
	}

	return &Encryptor{key: key, legacyKey: legacyKey}, nil
}

// loadOrCreateKey loads the encryption key from disk, or creates a new one.
// It returns the current key and the legacy (pre-rename) key derived from
// the same salt, so ciphertexts from the old build keep decrypting. When
// the stored key file still holds the legacy derived key, it is rewritten
// with the current derived key (the salt is unchanged).
func loadOrCreateKey(keyPath string) ([]byte, []byte, error) {
	// Try to read existing key
	data, err := os.ReadFile(keyPath)
	if err == nil && len(data) == keySize+saltSize {
		// Key file exists, derive keys from stored salt and machine-specific data
		salt := data[:saltSize]
		key := deriveKey(salt)
		legacyKey := legacyDeriveKey(salt)
		// Migrate the stored key bytes to the current derivation. The
		// legacy key remains re-derivable from the salt at any time.
		if string(data[saltSize:]) != string(key) {
			keyData := make([]byte, saltSize+keySize)
			copy(keyData[:saltSize], salt)
			copy(keyData[saltSize:], key)
			_ = os.WriteFile(keyPath, keyData, 0600)
		}
		return key, legacyKey, nil
	}

	// Generate new key
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	key := deriveKey(salt)

	// Store salt (we can regenerate key from salt + machine data)
	keyData := make([]byte, saltSize+keySize)
	copy(keyData[:saltSize], salt)
	copy(keyData[saltSize:], key)

	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return nil, nil, fmt.Errorf("failed to create key directory: %w", err)
	}

	// Write key file with restricted permissions
	if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
		return nil, nil, fmt.Errorf("failed to write key file: %w", err)
	}

	return key, legacyDeriveKey(salt), nil
}

// deriveKey derives an encryption key from salt and machine-specific data
func deriveKey(salt []byte) []byte {
	return deriveKeyWithPrefix(salt, "daak")
}

// deriveKeyWithPrefix derives an encryption key from salt, machine-specific
// data, and the given derivation prefix.
func deriveKeyWithPrefix(salt []byte, prefix string) []byte {
	// Use machine-specific data as the base for key derivation
	// This includes hostname and user info for some uniqueness
	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}

	// Combine machine-specific data
	machineData := fmt.Sprintf("%s:%s:%s:%d", prefix, hostname, username, os.Getuid())

	// Derive key using PBKDF2
	return pbkdf2.Key([]byte(machineData), salt, pbkdf2Iterations, keySize, sha256.New)
}

// Encrypt encrypts plaintext using AES-256-GCM
// Returns base64-encoded ciphertext (nonce prepended)
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and prepend nonce
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Return base64-encoded result
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64-encoded ciphertext (with prepended nonce).
// Values encrypted by a pre-rename build are decrypted with the legacy
// key when the current key fails (see legacy.go); new values always use
// the current key.
func (e *Encryptor) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	// Decode base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	plaintext, err := decryptWithKey(data, e.key)
	if err == nil {
		return plaintext, nil
	}
	if len(e.legacyKey) > 0 {
		if plaintext, legacyErr := decryptWithKey(data, e.legacyKey); legacyErr == nil {
			return plaintext, nil
		}
	}
	return "", fmt.Errorf("failed to decrypt: %w", err)
}

// decryptWithKey decrypts raw (already base64-decoded) ciphertext with the
// given key. The nonce is prepended to the ciphertext.
func decryptWithKey(data, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	// Extract nonce and ciphertext
	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}
