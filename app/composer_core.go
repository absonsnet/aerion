package app

import (
	coreapi "github.com/absonsnet/daak/internal/core/api/v1"
	"github.com/absonsnet/daak/internal/credentials"
	extui "github.com/absonsnet/daak/internal/extensions/ui"
)

// composerCore is the coreapi.Core handed to extensions hosted by the
// detached composer window (ComposerApp). The detached composer is a
// separate OS process with its own Wails bindings, so it cannot reuse the
// main window's App-bound Core — but it shares the same database and
// credential store, so it can host the AI Writing extension itself.
//
// Only the surfaces a composer-hosted extension touches are wired:
// Storage (per-extension secrets via the shared credStore) and UI
// (composer-toolbar + settings-tab registration into the composer's own
// registry). Mail, Composer, Contacts, Auth, Notifications, Events and
// Extension are not hosted in this process and return nil — the AI Writing
// bridge and its Register only ever call Storage() and UI(), so those nils
// are never dereferenced here. Log() and HTML() are the same standalone
// implementations the main host uses. Per EXT_RULES R3, the extension gets
// everything it needs through this Core and nothing else.
type composerCore struct {
	credStore   *credentials.Store
	uiRegistry  *extui.Registry
	extensionID string
	manifest    coreapi.Manifest
}

// newComposerCore builds a Core scoped to the given extension for the
// detached composer process. Safe to call once the composer's stores are
// initialized (ComposerApp.Startup).
func newComposerCore(credStore *credentials.Store, uiRegistry *extui.Registry, ext coreapi.Extension) *composerCore {
	m := ext.Manifest()
	return &composerCore{
		credStore:   credStore,
		uiRegistry:  uiRegistry,
		extensionID: m.ID,
		manifest:    m,
	}
}

func (c *composerCore) Mail() coreapi.Mail                   { return nil }
func (c *composerCore) Composer() coreapi.Composer           { return nil }
func (c *composerCore) Contacts() coreapi.Contacts           { return nil }
func (c *composerCore) Auth() coreapi.Auth                   { return nil }
func (c *composerCore) Notifications() coreapi.Notifications { return nil }
func (c *composerCore) UI() coreapi.UI {
	// No URL opener in this process: the AI Writing extension never calls
	// OpenURL, and uiCoreImpl reports a clean error if anything ever does.
	return uiCoreImpl{registry: c.uiRegistry}
}
func (c *composerCore) Storage() coreapi.Storage { return storageCoreImpl{credStore: c.credStore} }
func (c *composerCore) Events() coreapi.EventBus { return nil }
func (c *composerCore) Log() coreapi.Logger      { return loggerCoreImpl{extensionID: c.extensionID} }
func (c *composerCore) HTML() coreapi.HTML       { return htmlCoreImpl{sanitizer: sharedSanitizer} }
func (c *composerCore) Extension(id string) (any, bool) {
	return nil, false
}

// compile-time check: composerCore satisfies coreapi.Core.
var _ coreapi.Core = (*composerCore)(nil)
