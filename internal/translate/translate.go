// Package translate implements Daak's per-message translation (Phase D):
// offline-first language detection at read time, a Bergamot (Firefox
// Translations) WASM engine that runs in the webview, and consent-gated
// online providers as the last tier.
//
// Nothing is translated and nothing leaves the machine until the user
// presses Translate on a message. Detection runs in the viewer, not at
// sync time, so translation costs no sync time and no storage beyond the
// message_translations cache (migration 47).
package translate

// Engine names, as stored in message_translations.engine and shown in the
// banner ("Translated from Urdu · Offline" names the engine that ran).
const (
	EngineBergamot = "bergamot" // Firefox Translations models, offline, in the webview
	EngineGoogle   = "google"   // Google Translate, online, consent-gated
	EngineDeepL    = "deepl"    // DeepL API, online, consent-gated, own key
	EngineOS       = "os"       // OS on-device engine (Tier 1), where one exists
)

// Mode values for the translate_mode setting.
const (
	ModePreferOffline = "prefer_offline" // offline tiers first; online only via an explicit per-message button
	ModeOfflineOnly   = "offline_only"   // Tiers 1-2 only, ever; text never leaves the machine
	ModeOnline        = "online"         // straight to the online provider (consent still applies)
)

// DefaultMode is the default translate_mode.
const DefaultMode = ModePreferOffline

// DefaultTargetLanguage is the default translate_target_language: follow
// the UI language.
const DefaultTargetLanguage = "system"

// DefaultOnlineProvider is the default translate_online_provider.
const DefaultOnlineProvider = "google"

// MinDetectRunes is the shortest body that gets a banner. Shorter text
// ("Ok, kal milte hain") is too ambiguous to name a language for.
const MinDetectRunes = 40

// MinDetectConfidence is the lowest detector confidence that gets a
// banner. Below it the detector is guessing.
const MinDetectConfidence = 0.80
