package backend

import "fmt"

// Settings is the non-sensitive Mail Merge configuration, stored per
// account as one JSON blob in the per-extension SQLite (see store.go).
// JSON tags drive the Wails/TS binding shape — keep stable.
type Settings struct {
	// CapEnabled turns the Send Now safety cap on. When enabled, a send
	// run sends at most CapLimit messages; the rest are held as
	// "held — over cap" until the user approves the next batch.
	CapEnabled bool `json:"capEnabled"`
	// CapLimit is the per-run send cap when CapEnabled. Default 100.
	CapLimit int `json:"capLimit"`
	// DefaultPauseSeconds is the pause between messages for a new run.
	DefaultPauseSeconds int `json:"defaultPauseSeconds"`
	// DefaultRandomPause makes the pause a random 0–DefaultPauseSeconds
	// instead of exactly DefaultPauseSeconds.
	DefaultRandomPause bool `json:"defaultRandomPause"`
	// DefaultDeliverMode is "drafts" (default, safe) or "send".
	DefaultDeliverMode string `json:"defaultDeliverMode"`
	// SignMessages signs every generated message with the account's
	// configured identity (S/MIME preferred, else PGP). SignMessagesSet
	// records that the user chose this explicitly in the merge dialog;
	// when false, the dialog defaults to the account's composer
	// signing setting (a sign policy of "always") instead.
	SignMessages    bool `json:"signMessages"`
	SignMessagesSet bool `json:"signMessagesSet"`
}

// DefaultSettings returns the defaults: cap ON at 100 for Send Now,
// 2s pause, drafts as the default deliver mode.
func DefaultSettings() Settings {
	return Settings{
		CapEnabled:          true,
		CapLimit:            100,
		DefaultPauseSeconds: 2,
		DefaultRandomPause:  false,
		DefaultDeliverMode:  "drafts",
	}
}

// Validate checks settings before save.
func (s Settings) Validate() error {
	if s.CapLimit < 1 {
		return fmt.Errorf("mail-merge: cap limit must be at least 1")
	}
	if s.DefaultDeliverMode != "drafts" && s.DefaultDeliverMode != "send" {
		return fmt.Errorf("mail-merge: deliver mode must be %q or %q", "drafts", "send")
	}
	if s.DefaultPauseSeconds < 0 {
		return fmt.Errorf("mail-merge: pause must not be negative")
	}
	return nil
}
