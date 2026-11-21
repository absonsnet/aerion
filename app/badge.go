package app

import (
	"github.com/absonsnet/daak/internal/logging"
)

// Unread badge: the unified-inbox unread count on the OS chrome
// (macOS Dock badge, Windows taskbar overlay, tray icon). The count
// source is GetUnifiedInboxUnreadCount — inbox folders of enabled
// accounts only, so Spam and Trash never count.
//
// refreshBadge recomputes the count and pushes it to the tray's badge
// surfaces. The tray only pushes when the count changed (or the toggle
// flipped), so calling this often is cheap: one SUM over the folders
// table plus an integer compare. It never fails the caller — a badge
// must never break a sync or a read flip — so errors are logged and
// the push is retried on the next refresh.
func (a *App) refreshBadge() {
	defer recoverPanic("app", "refresh-badge")
	if a.tray == nil || a.settingsStore == nil {
		return
	}
	log := logging.WithComponent("app")
	enabled, err := a.settingsStore.GetShowUnreadBadge()
	if err != nil {
		log.Error().Err(err).Msg("Unread badge: could not read toggle; leaving badges as they are")
		return
	}
	a.tray.SetBadgeEnabled(enabled)
	if !enabled {
		return
	}
	count, err := a.GetUnifiedInboxUnreadCount()
	if err != nil {
		log.Error().Err(err).Msg("Unread badge: could not count unified inbox unread")
		return
	}
	a.tray.SetBadge(count)
}
