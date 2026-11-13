package backend

import (
	"fmt"

	aiwriting "github.com/hkdb/aerion/extensions/ai-writing"
	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
)

// Extension is the AI Writing Assistant extension's lifecycle handle. Tiny — just the
// manifest plus the Register handshake. The Wails-bound surface lives on
// Bridge (bridge.go); the provider clients, prompt registry, and settings
// store live in the sibling files of this package.
type Extension struct {
	manifest coreapi.Manifest
}

// NewExtension constructs the Extension lifecycle handle. Allocates only
// a manifest copy + this struct — no stores, no SQLite, no HTTP client.
func NewExtension() *Extension {
	return &Extension{manifest: aiwriting.Manifest()}
}

// Manifest returns the parsed manifest embedded at build time.
func (e *Extension) Manifest() coreapi.Manifest { return e.manifest }

// Register wires the AI Writing Assistant extension's UI surfaces: the Improve
// Writing button in the composer toolbar (next to the plain-text toggle,
// covering both inline and detached composers since they share
// Composer.svelte) and the AI Writing Assistant tab in the Settings dialog.
//
// Runs once per Aerion process lifetime at App.Startup, regardless of
// enabled state — descriptive registrations persist across enable/disable
// cycles. The frontend filters by enabled state at render time.
func (e *Extension) Register(core coreapi.Core) (coreapi.Unregister, error) {
	unregToolbar, err := core.UI().RegisterComposerToolbar(coreapi.ComposerToolbarRequest{
		ExtensionID: e.manifest.ID,
		Label:       "Improve Writing",
		Icon:        "mdi:pencil",
		Component:   "AIWritingButton",
	})
	if err != nil {
		return nil, fmt.Errorf("ai-writing: register composer toolbar: %w", err)
	}

	unregSettings, err := core.UI().RegisterSettingsTab(coreapi.SettingsTabRequest{
		ExtensionID: e.manifest.ID,
		Label:       "AI Writing Assistant",
		Component:   "AIWritingSettingsDialog",
	})
	if err != nil {
		unregToolbar()
		return nil, fmt.Errorf("ai-writing: register settings tab: %w", err)
	}

	return func() {
		unregSettings()
		unregToolbar()
	}, nil
}

// compile-time check: *Extension satisfies coreapi.Extension
var _ coreapi.Extension = (*Extension)(nil)
