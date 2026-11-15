package backend

import (
	"fmt"

	mailmerge "github.com/absonsnet/daak/extensions/mail-merge"
	coreapi "github.com/absonsnet/daak/internal/core/api/v1"
)

// Extension is the Mail Merge extension's lifecycle handle. Tiny — just the
// manifest plus the Register handshake. The Wails-bound surface lives on
// Bridge (bridge.go); the run log + settings store live in store.go; the
// merge engine, sources and run orchestration live in the host-side
// package internal/mailmerge.
type Extension struct {
	manifest coreapi.Manifest
}

// NewExtension constructs the Extension lifecycle handle. Allocates only
// a manifest copy + this struct — no stores, no SQLite, no SMTP client.
func NewExtension() *Extension {
	return &Extension{manifest: mailmerge.Manifest()}
}

// Manifest returns the parsed manifest embedded at build time.
func (e *Extension) Manifest() coreapi.Manifest { return e.manifest }

// Register wires the Mail Merge extension's UI surfaces: the Mail Merge
// button in the composer toolbar (covering both inline and detached
// composers since they share Composer.svelte), the Mail Merge rail tab
// for the outbox queue / job history (Phase 2), and the Mail Merge tab
// in the Settings dialog.
//
// Runs once per Aerion process lifetime at App.Startup, regardless of
// enabled state — descriptive registrations persist across enable/disable
// cycles. The frontend filters by enabled state at render time.
func (e *Extension) Register(core coreapi.Core) (coreapi.Unregister, error) {
	unregToolbar, err := core.UI().RegisterComposerToolbar(coreapi.ComposerToolbarRequest{
		ExtensionID: e.manifest.ID,
		Label:       "Mail Merge",
		Icon:        "mdi:email-multiple",
		Component:   "MailMergeButton",
	})
	if err != nil {
		return nil, fmt.Errorf("mail-merge: register composer toolbar: %w", err)
	}

	unregRail, err := core.UI().RegisterRailTab(coreapi.RailTabRequest{
		ExtensionID: e.manifest.ID,
		Label:       "Mail Merge",
		Icon:        "mdi:email-multiple",
		Component:   "MailMergeQueuePane",
		Order:       30,
	})
	if err != nil {
		unregToolbar()
		return nil, fmt.Errorf("mail-merge: register rail tab: %w", err)
	}

	unregSettings, err := core.UI().RegisterSettingsTab(coreapi.SettingsTabRequest{
		ExtensionID: e.manifest.ID,
		Label:       "Mail Merge",
		Component:   "MailMergeSettingsDialog",
	})
	if err != nil {
		unregRail()
		unregToolbar()
		return nil, fmt.Errorf("mail-merge: register settings tab: %w", err)
	}

	return func() {
		unregSettings()
		unregRail()
		unregToolbar()
	}, nil
}

// compile-time check: *Extension satisfies coreapi.Extension
var _ coreapi.Extension = (*Extension)(nil)
