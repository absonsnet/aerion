package app

import (
	extaiwritingbe "github.com/absonsnet/daak/extensions/ai-writing/backend"
)

// initAIWritingExtension wires the AI Writing extension's Bridge into App
// during Startup. All bridge logic lives in extensions/ai-writing/backend/;
// this file exists ONLY so the host can supply the bridge with its
// host-provided dependencies (settings store, paths, db, coreapi handle)
// and so the embedded-field promotion makes the bridge methods Wails-bindable.
//
// Lightweight-by-default invariant: the Bridge struct allocation is the
// entire footprint until the first enabled `AIWriting_*` Wails call. At
// that point, `AIWritingBridge.ensureInit()` opens the per-extension
// SQLite, applies pending migrations, and fetches the secrets handle.
// Disabled extensions contribute zero work.
//
// Per docs/EXT_RULES.md R2, this file holds NO closures wrapping
// `internal/*` calls. The AI Writing extension's only host touchpoints are
// the standard `coreapi.Core` surfaces (Storage.Secrets for the provider
// API key, UI for the composer toolbar button + settings tab).
func (a *App) initAIWritingExtension() {
	aiWritingCore := newCoreForExtension(a, a.aiWritingExt)

	a.AIWritingBridge = extaiwritingbe.NewAIWritingBridge(extaiwritingbe.AIWritingBridgeDeps{
		SettingsStore: a.settingsStore,
		Paths:         a.paths,
		DB:            a.db,
		Core:          aiWritingCore,
	})
}
