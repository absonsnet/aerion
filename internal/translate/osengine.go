package translate

import (
	"runtime"
)

// Tier 1: the OS on-device translation engine, where the OS exposes
// one.
//
//   - macOS 15+ ships the Translation framework (TranslationSession),
//     which needs a small native (Swift/ObjC) bridge — Daak has no such
//     bridge yet, so macOS reports unavailable with the reason below.
//     The bridge is a named follow-up, not something to fake: the tier
//     is skipped honestly until it exists.
//   - Windows exposes no public on-device translation API Daak can
//     call, and Linux has no OS translation framework at all.
//
// ProbeOSEngine reports whether this build can translate offline
// through the OS. It never claims an engine exists when it does not.
func ProbeOSEngine() OSEngineStatus {
	switch runtime.GOOS {
	case "darwin":
		return OSEngineStatus{
			Available: false,
			Name:      "macOS Translation",
			Reason:    "the macOS Translation framework bridge is not built yet",
		}
	case "windows":
		return OSEngineStatus{
			Available: false,
			Name:      "Windows on-device translation",
			Reason:    "Windows exposes no public on-device translation API",
		}
	default:
		return OSEngineStatus{
			Available: false,
			Name:      "OS translation",
			Reason:    "this OS has no on-device translation framework",
		}
	}
}

// OSEngineStatus is the Tier 1 probe result.
type OSEngineStatus struct {
	Available bool   `json:"available"`
	Name      string `json:"name"`
	Reason    string `json:"reason,omitempty"`
}
