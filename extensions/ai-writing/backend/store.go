package backend

import (
	"encoding/json"
	"fmt"

	"github.com/absonsnet/daak/internal/extensions"
)

// migrations is the per-extension migration sequence for the AI Writing
// extension's isolated DB. v1 needs NO custom tables: non-sensitive
// settings live as one JSON blob in the canonical ext_kv table (created
// automatically by extensions.OpenStore and exposed via Store.KV()), and
// the API key lives in the secret store, never in SQLite. The sequence is kept empty rather than inventing a
// placeholder table; add a v1 migration here only when v1+ data needs a
// real table.
var migrations = []extensions.Migration{}

// Store wraps the per-extension DB for the AI Writing extension. Lives in
// an isolated SQLite file at <dataDir>/extensions/ai-writing/data.db,
// separate from Aerion's main DB. No tables in this file are read or
// written by core code; cross-extension access flows through coreapi only.
type Store struct {
	*extensions.Store
}

// NewStore opens the AI Writing extension's isolated SQLite DB and applies
// pending migrations. Called lazily from Bridge.ensureInit on the first
// enabled bridge call (per EXT_RULES.md R10/R11) — a disabled extension
// never opens its DB.
func NewStore(dataDir string) (*Store, error) {
	s, err := extensions.OpenStore(dataDir, "ai-writing", migrations)
	if err != nil {
		return nil, err
	}
	return &Store{Store: s}, nil
}

// settingsKVKey is the ext_kv key under which the non-sensitive settings
// JSON blob is stored. Chosen storage: the Store KV accessor
// (extensions.Store.KV() → coreapi.KVStore over the ext_kv table) — it
// exists, is created automatically by OpenStore, and a single JSON blob
// needs no custom table. No `settings` table is created in v1.
const settingsKVKey = "settings"

// GetSettings reads the stored settings blob. Returns the defaults when
// nothing has been saved yet. The returned Settings never carries a key
// value — HasAPIKey is filled in by the bridge from the secret store.
func (s *Store) GetSettings() (Settings, error) {
	raw, err := s.KV().Get(settingsKVKey)
	if err != nil {
		return Settings{}, fmt.Errorf("ai-writing: read settings: %w", err)
	}
	st := DefaultSettings()
	if raw == "" {
		return st, nil
	}
	// Decode over the defaults so settings saved by an older version
	// (missing newer fields) still yield sane values for those fields.
	// Note: encoding/json leaves missing fields untouched, so overlaying
	// on top of defaults is exactly the upgrade path.
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return Settings{}, fmt.Errorf("ai-writing: parse stored settings: %w", err)
	}
	// The blob must never be able to smuggle a key-presence claim in:
	// HasAPIKey is always recomputed from the secret store by the caller.
	st.HasAPIKey = false
	return st, nil
}

// SaveSettings persists the non-sensitive settings blob. The API key is
// NOT part of this blob — it is written to the secret store separately
// by the bridge.
func (s *Store) SaveSettings(st Settings) error {
	st.HasAPIKey = false // never persist the key-presence flag either; it is derived.
	raw, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("ai-writing: encode settings: %w", err)
	}
	if err := s.KV().Set(settingsKVKey, string(raw)); err != nil {
		return fmt.Errorf("ai-writing: save settings: %w", err)
	}
	return nil
}
