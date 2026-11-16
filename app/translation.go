package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/absonsnet/daak/internal/credentials"
	"github.com/absonsnet/daak/internal/database"
	"github.com/absonsnet/daak/internal/message"
	"github.com/absonsnet/daak/internal/settings"
	"github.com/absonsnet/daak/internal/translate"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ============================================================================
// Message translation (Phase D) - Wails bindings.
//
// Offline-first, per message: detection runs at read time in the viewer,
// nothing is translated and nothing leaves the machine until the user
// presses Translate on that message. App and ViewerApp (detached popup)
// both expose these methods; the shared work lives on translationDeps
// so the two windows cannot drift.
//
// Engine ladder: Tier 1 OS engine (ProbeOSEngine — unavailable on this
// build, see internal/translate/osengine.go), Tier 2 Bergamot WASM in
// the webview (the frontend runs the engine; the backend manages the
// models), Tier 3 online (Google Translate unofficial endpoint, or
// DeepL with the user's own key) behind a per-provider consent gate.
// ============================================================================

// TranslationSettings is Settings → Translation, as the frontend reads
// it. Consents maps provider ("google", "deepl") to consented.
type TranslationSettings struct {
	Enabled        bool              `json:"enabled"`
	Mode           string            `json:"mode"`
	TargetLanguage string            `json:"targetLanguage"`
	ResolvedTarget string            `json:"resolvedTarget"`
	OnlineProvider string            `json:"onlineProvider"`
	Consents       map[string]bool   `json:"consents"`
	DeepLKeySet    bool              `json:"deeplKeySet"`
	OSEngine       translate.OSEngineStatus `json:"osEngine"`
}

// TranslationTargetLanguage is one entry of the target-language picker.
type TranslationTargetLanguage struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// MessageDetection is what the viewer gets for one message: the
// detected language, whether to offer translation, what the offline
// tiers cover, and any cached translation to reuse.
type MessageDetection struct {
	Enabled bool `json:"enabled"`
	// Detected language ("" when none found with enough confidence).
	Language     string  `json:"language"`
	LanguageName string  `json:"languageName"`
	Confidence   float64 `json:"confidence"`
	Source       string  `json:"source"`
	// ShowBanner is true when the viewer should offer translation.
	ShowBanner bool `json:"showBanner"`
	// Target is the resolved target language.
	Target     string `json:"target"`
	TargetName string `json:"targetName"`
	// IsSecurity is true for OTP/security mail (Phase A extract kind
	// "code"): offline only, never offered online. Phase A has no
	// verification/security category — the code extract is the
	// verification signal.
	IsSecurity bool `json:"isSecurity"`
	// Mode is the current translate_mode.
	Mode string `json:"mode"`
	// OfflineAvailable is true when an offline tier covers the pair
	// right now (OS engine, or all Bergamot legs downloaded).
	OfflineAvailable bool `json:"offlineAvailable"`
	// OfflineEngine names the offline engine that covers the pair
	// ("bergamot" or "os", "" when none).
	OfflineEngine string `json:"offlineEngine"`
	// OfflineNeedsDownload is true when Bergamot covers the pair but
	// the models are not downloaded yet.
	OfflineNeedsDownload bool `json:"offlineNeedsDownload"`
	// OfflineDownloadSize is the download size in bytes when
	// OfflineNeedsDownload is true. It includes the engine download
	// when the engine is not cached yet.
	OfflineDownloadSize int64 `json:"offlineDownloadSize"`
	// OfflineEngineMissing is true when the Bergamot engine itself is
	// not downloaded yet (it is a separate download from the models).
	OfflineEngineMissing bool `json:"offlineEngineMissing"`
	// OfflineSupported is false when no offline tier can ever cover
	// the pair (no Bergamot model for it).
	OfflineSupported bool `json:"offlineSupported"`
	// OnlineAllowed is true when the mode and the message allow an
	// online translation (consent is still checked at send time).
	OnlineAllowed bool `json:"onlineAllowed"`
	// OnlineProvider is the configured online provider.
	OnlineProvider string `json:"onlineProvider"`
	// OnlineConsented is true when the user already consented to the
	// configured online provider.
	OnlineConsented bool `json:"onlineConsented"`
	// Cached is the cached translation for this message + target, if
	// the body has not changed since it was made.
	Cached *translate.CachedTranslation `json:"cached,omitempty"`
}

// BergamotLegStatus is one leg of a Bergamot route (direct pair or one
// half of a pivot through English).
type BergamotLegStatus struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Available bool   `json:"available"`
	Supported bool   `json:"supported"`
	Version   string `json:"version,omitempty"`
	SizeBytes int64  `json:"sizeBytes"`
	Missing   []string `json:"missing,omitempty"`
}

// CachedTranslationResult is what GetCachedTranslation returns.
type CachedTranslationResult struct {
	Found   bool   `json:"found"`
	Engine  string `json:"engine,omitempty"`
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body,omitempty"`
}

// translationDeps is everything the translation flow needs from its
// host window. App and ViewerApp both carry these stores, so one
// implementation serves both.
type translationDeps struct {
	db            *database.DB
	messageStore  *message.Store
	settingsStore *settings.Store
	credStore     *credentials.Store
	dataDir       string
	// httpClient performs registry, model and online requests; nil
	// uses a default client with a 30s timeout.
	httpClient *http.Client
}

// downloadCancels holds the cancel func of the in-flight Bergamot
// model download per data directory, so the banner's Cancel button
// (a separate binding call, hence a fresh deps) can stop it. One
// download per data dir at a time: a second download for the same
// data dir replaces the stored entry. The value is a pointer so the
// deferred cleanup can tell whether the entry is still this
// download's (func values are not comparable).
var downloadCancels sync.Map // string -> *downloadCancelEntry

type downloadCancelEntry struct {
	cancel context.CancelFunc
}

func (d *translationDeps) client() *http.Client {
	if d.httpClient != nil {
		return d.httpClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// getSettings reads the translation settings block.
func (d *translationDeps) getSettings(uiLanguage string) (*TranslationSettings, error) {
	enabled, err := d.settingsStore.GetTranslateEnabled()
	if err != nil {
		return nil, err
	}
	mode, err := d.settingsStore.GetTranslateMode()
	if err != nil {
		return nil, err
	}
	target, err := d.settingsStore.GetTranslateTargetLanguage()
	if err != nil {
		return nil, err
	}
	provider, err := d.settingsStore.GetTranslateOnlineProvider()
	if err != nil {
		return nil, err
	}
	consents := map[string]bool{}
	for _, p := range []string{translate.EngineGoogle, translate.EngineDeepL} {
		c, err := d.settingsStore.GetTranslateOnlineConsent(p)
		if err != nil {
			return nil, err
		}
		consents[p] = c
	}
	key, err := d.credStore.GetServiceSecret(credentials.ServiceSecretDeepL)
	if err != nil {
		return nil, err
	}
	return &TranslationSettings{
		Enabled:        enabled,
		Mode:           mode,
		TargetLanguage: target,
		ResolvedTarget: translate.ResolveTargetLanguage(target, uiLanguage),
		OnlineProvider: provider,
		Consents:       consents,
		DeepLKeySet:    strings.TrimSpace(key) != "",
		OSEngine:       translate.ProbeOSEngine(),
	}, nil
}

// detectForMessage runs detection for one message and works out what
// the banner may offer. uiLanguage is the frontend's current UI locale
// ("" = English): it resolves a "system" target language.
func (d *translationDeps) detectForMessage(msg *message.Message, uiLanguage string) (*MessageDetection, error) {
	set, err := d.getSettings(uiLanguage)
	if err != nil {
		return nil, err
	}
	det := &MessageDetection{
		Enabled:        set.Enabled,
		Target:         set.ResolvedTarget,
		TargetName:     translate.LanguageName(set.ResolvedTarget),
		Mode:           set.Mode,
		OnlineProvider: set.OnlineProvider,
		OnlineConsented: set.Consents[set.OnlineProvider],
		IsSecurity:     msg.ExtractKind == "code",
	}
	if !set.Enabled {
		return det, nil
	}
	text := msg.BodyText
	if strings.TrimSpace(text) == "" && msg.BodyHTML != "" {
		text = translate.StripHTMLTags(msg.BodyHTML)
	}
	found := translate.Detect(text, set.ResolvedTarget)
	det.Language = found.Language
	det.LanguageName = found.LanguageName
	det.Confidence = found.Confidence
	det.Source = found.Source
	det.ShowBanner = found.ShowBanner
	if !found.ShowBanner {
		return det, nil
	}

	// Cached translation first: re-opening reuses it, no engine work.
	hash := translate.BodyHash(msg.Subject, msg.BodyText, msg.BodyHTML)
	if cached, ok, err := translate.GetCachedAnyEngine(d.db, msg.ID, set.ResolvedTarget, hash, []string{translate.EngineBergamot, translate.EngineOS, translate.EngineGoogle, translate.EngineDeepL}); err == nil && ok {
		det.Cached = cached
	}

	// Offline tiers.
	osEngine := translate.ProbeOSEngine()
	if osEngine.Available {
		det.OfflineAvailable = true
		det.OfflineEngine = translate.EngineOS
		det.OfflineSupported = true
	} else {
		route := translate.BergamotRoute(found.Language, set.ResolvedTarget)
		if route != nil {
			det.OfflineSupported = true
			legs, err := d.bergamotLegs(found.Language, set.ResolvedTarget)
			if err != nil {
				// The catalogue could not be fetched (offline):
				// offline availability is unknown, not available.
				// The banner says so and still offers online where
				// allowed; a downloaded pair is still found below.
				legs = d.bergamotLegsOffline(found.Language, set.ResolvedTarget)
			}
			allAvailable := len(legs) > 0
			var downloadSize int64
			for _, leg := range legs {
				if !leg.Supported {
					det.OfflineSupported = false
				}
				if !leg.Available {
					allAvailable = false
					downloadSize += leg.SizeBytes
				}
			}
			// The engine is a separate download from the models: a
			// downloaded model is unusable without it, and the
			// download size the banner shows includes it.
			engine := translate.CheckEngineStatus(d.dataDir)
			if !engine.Available {
				det.OfflineEngineMissing = true
				downloadSize += engine.SizeBytes
			}
			if allAvailable && engine.Available {
				det.OfflineAvailable = true
				det.OfflineEngine = translate.EngineBergamot
			} else if det.OfflineSupported {
				det.OfflineNeedsDownload = true
				det.OfflineDownloadSize = downloadSize
			}
		}
	}

	// Online: allowed by the mode and the message. Consent is checked
	// again at send time; the banner only gates the button.
	switch set.Mode {
	case translate.ModeOfflineOnly:
		det.OnlineAllowed = false
	default:
		det.OnlineAllowed = !det.IsSecurity
	}
	return det, nil
}

// bergamotLegs returns the status of each leg of the route, from the
// catalogue (fetched, 24h cache) plus what is on disk.
func (d *translationDeps) bergamotLegs(from, to string) ([]BergamotLegStatus, error) {
	records, err := translate.FetchRegistry(context.Background(), d.dataDir, d.client())
	if err != nil {
		return nil, err
	}
	return d.bergamotLegsFromRecords(records, from, to), nil
}

// bergamotLegsOffline answers from disk only, for when the catalogue
// cannot be fetched: a downloaded pair is still recognised.
func (d *translationDeps) bergamotLegsOffline(from, to string) []BergamotLegStatus {
	var legs []BergamotLegStatus
	for _, leg := range translate.BergamotRoute(from, to) {
		// Without the catalogue, PairStatus cannot know the version
		// or size; check the disk directly through the downloaded
		// list.
		available := false
		version := ""
		for _, p := range mustListPairs(d.dataDir) {
			if p.FromLang == leg[0] && p.ToLang == leg[1] {
				available = true
				version = p.Version
			}
		}
		legs = append(legs, BergamotLegStatus{
			From: leg[0], To: leg[1], Available: available,
			Supported: available, Version: version,
		})
	}
	return legs
}

func mustListPairs(dataDir string) []translate.DownloadedPair {
	pairs, _ := translate.ListDownloadedPairs(dataDir)
	return pairs
}

func (d *translationDeps) bergamotLegsFromRecords(records []translate.ModelRecord, from, to string) []BergamotLegStatus {
	var legs []BergamotLegStatus
	for _, leg := range translate.BergamotRoute(from, to) {
		st := translate.CheckPairStatus(d.dataDir, records, leg[0], leg[1])
		legs = append(legs, BergamotLegStatus{
			From: leg[0], To: leg[1], Available: st.Available,
			Supported: st.Supported, Version: st.Version,
			SizeBytes: st.SizeBytes, Missing: st.Missing,
		})
	}
	return legs
}

// translateOnline translates texts for one message through the
// configured online provider. The consent gate runs before any network
// traffic: without consent it returns an error whose text starts with
// "consent required" and the provider is never contacted. Security
// mail (OTP codes) is refused outright.
func (d *translationDeps) translateOnline(msg *message.Message, texts []string, source, target string) ([]string, error) {
	mode, err := d.settingsStore.GetTranslateMode()
	if err != nil {
		return nil, err
	}
	if mode == translate.ModeOfflineOnly {
		return nil, fmt.Errorf("online translation is off (translation mode is offline only)")
	}
	if msg.ExtractKind == "code" {
		return nil, fmt.Errorf("security mail is never sent for online translation")
	}
	provider, err := d.settingsStore.GetTranslateOnlineProvider()
	if err != nil {
		return nil, err
	}
	consented, err := d.settingsStore.GetTranslateOnlineConsent(provider)
	if err != nil {
		return nil, err
	}
	if !consented {
		return nil, fmt.Errorf("consent required: the message text will be sent to %s to translate", providerName(provider))
	}
	apiKey := ""
	if provider == translate.EngineDeepL {
		apiKey, err = d.credStore.GetServiceSecret(credentials.ServiceSecretDeepL)
		if err != nil {
			return nil, err
		}
	}
	client := translate.NewOnlineClient(d.client())
	return client.Translate(context.Background(), provider, apiKey, source, target, texts)
}

func providerName(provider string) string {
	switch provider {
	case translate.EngineDeepL:
		return "DeepL"
	default:
		return "Google Translate"
	}
}

// ensureEngine downloads the Bergamot engine when it is not cached
// yet. Progress reports through the same per-leg callback, with the
// leg ("engine", "").
func (d *translationDeps) ensureEngine(ctx context.Context, progress func(legFrom, legTo string, downloaded, total int64)) error {
	if translate.CheckEngineStatus(d.dataDir).Available {
		return nil
	}
	return translate.DownloadEngine(ctx, d.dataDir, d.client(), func(downloaded, total int64) {
		if progress != nil {
			progress("engine", "", downloaded, total)
		}
	})
}

// downloadBergamotPair downloads every leg of the route, reporting
// progress per leg. The caller supplies the progress callback. The
// download is cancellable via cancelDownload. The engine is downloaded
// first when it is not cached yet: models without the engine cannot
// translate anything.
func (d *translationDeps) downloadBergamotPair(from, to string, progress func(legFrom, legTo string, downloaded, total int64)) error {
	ctx, cancel := context.WithCancel(context.Background())
	entry := &downloadCancelEntry{cancel: cancel}
	downloadCancels.Store(d.dataDir, entry)
	defer func() {
		// Only clear the entry when it is still this download's:
		// a second download for the same data dir may have started.
		if cur, ok := downloadCancels.Load(d.dataDir); ok && cur == entry {
			downloadCancels.Delete(d.dataDir)
		}
		cancel()
	}()
	if err := d.ensureEngine(ctx, progress); err != nil {
		return err
	}
	records, err := translate.FetchRegistry(ctx, d.dataDir, d.client())
	if err != nil {
		return err
	}
	for _, leg := range translate.BergamotRoute(from, to) {
		if err := translate.DownloadPair(ctx, d.dataDir, records, leg[0], leg[1], d.client(), func(downloaded, total int64) {
			if progress != nil {
				progress(leg[0], leg[1], downloaded, total)
			}
		}); err != nil {
			return err
		}
	}
	return nil
}

// downloadBergamotLanguage downloads the engine when needed, then
// every direction the catalogue has for a language.
func (d *translationDeps) downloadBergamotLanguage(lang string, progress func(legFrom, legTo string, downloaded, total int64)) error {
	ctx, cancel := context.WithCancel(context.Background())
	entry := &downloadCancelEntry{cancel: cancel}
	downloadCancels.Store(d.dataDir, entry)
	defer func() {
		if cur, ok := downloadCancels.Load(d.dataDir); ok && cur == entry {
			downloadCancels.Delete(d.dataDir)
		}
		cancel()
	}()
	if err := d.ensureEngine(ctx, progress); err != nil {
		return err
	}
	records, err := translate.FetchRegistry(ctx, d.dataDir, d.client())
	if err != nil {
		return err
	}
	return translate.DownloadLanguage(ctx, d.dataDir, records, lang, d.client(), func(from, to string, downloaded, total int64) {
		if progress != nil {
			progress(from, to, downloaded, total)
		}
	})
}

// cancelDownload cancels the in-flight Bergamot model download, if any.
func (d *translationDeps) cancelDownload() {
	if entry, ok := downloadCancels.Load(d.dataDir); ok {
		entry.(*downloadCancelEntry).cancel()
	}
}

// emitDownloadProgress sends one "bergamot:download-progress" event.
// kind is "engine" for the engine leg, "model" for a model leg, so the
// download manager can tell them apart.
func emitDownloadProgress(ctx context.Context, legFrom, legTo string, downloaded, total int64) {
	kind := "model"
	if legFrom == "engine" {
		kind = "engine"
	}
	wailsRuntime.EventsEmit(ctx, "bergamot:download-progress", map[string]interface{}{
		"kind": kind, "from": legFrom, "to": legTo,
		"downloaded": downloaded, "total": total,
	})
}

// ============================================================================
// App bindings
// ============================================================================

func (a *App) translationDeps() *translationDeps {
	return &translationDeps{
		db:            a.db,
		messageStore:  a.messageStore,
		settingsStore: a.settingsStore,
		credStore:     a.credStore,
		dataDir:       a.paths.Data,
	}
}

// GetTranslationSettings returns Settings → Translation.
func (a *App) GetTranslationSettings(uiLanguage string) (*TranslationSettings, error) {
	return a.translationDeps().getSettings(uiLanguage)
}

// GetTranslationTargetLanguages returns the target-language picker list.
func (a *App) GetTranslationTargetLanguages() []TranslationTargetLanguage {
	out := make([]TranslationTargetLanguage, 0, len(translate.TargetLanguages))
	for _, l := range translate.TargetLanguages {
		out = append(out, TranslationTargetLanguage{Code: l.Code, Name: l.Name})
	}
	return out
}

// SetTranslationEnabled enables or disables translation offers.
func (a *App) SetTranslationEnabled(enabled bool) error {
	return a.settingsStore.SetTranslateEnabled(enabled)
}

// SetTranslationMode sets the translation mode (takes effect for the
// next Translate press, no restart).
func (a *App) SetTranslationMode(mode string) error {
	return a.settingsStore.SetTranslateMode(mode)
}

// SetTranslationTargetLanguage sets the target language ("system" or a
// picker code).
func (a *App) SetTranslationTargetLanguage(language string) error {
	return a.settingsStore.SetTranslateTargetLanguage(language)
}

// SetTranslationOnlineProvider sets the online provider.
func (a *App) SetTranslationOnlineProvider(provider string) error {
	return a.settingsStore.SetTranslateOnlineProvider(provider)
}

// SetTranslationOnlineConsent records or revokes consent for one
// online provider.
func (a *App) SetTranslationOnlineConsent(provider string, granted bool) error {
	return a.settingsStore.SetTranslateOnlineConsent(provider, granted)
}

// SetDeepLAPIKey stores the DeepL API key in the credentials store
// (keyring first, encrypted DB fallback). Empty clears it.
func (a *App) SetDeepLAPIKey(key string) error {
	return a.credStore.SetServiceSecret(credentials.ServiceSecretDeepL, key)
}

// DetectMessageLanguage runs read-time detection for one message and
// reports what the banner may offer. uiLanguage is the frontend's
// current UI locale ("" = English).
func (a *App) DetectMessageLanguage(messageID, uiLanguage string) (*MessageDetection, error) {
	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}
	return a.translationDeps().detectForMessage(msg, uiLanguage)
}

// GetCachedTranslation returns the cached translation for a message +
// target + engine when the body has not changed since it was made.
func (a *App) GetCachedTranslation(messageID, targetLang, engine string) (*CachedTranslationResult, error) {
	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}
	hash := translate.BodyHash(msg.Subject, msg.BodyText, msg.BodyHTML)
	cached, found, err := translate.GetCached(a.db, messageID, translate.NormalizeLang(targetLang), engine, hash)
	if err != nil {
		return nil, err
	}
	if !found {
		return &CachedTranslationResult{Found: false}, nil
	}
	return &CachedTranslationResult{Found: true, Engine: cached.Engine, Subject: cached.TranslatedSubject, Body: cached.TranslatedBody}, nil
}

// SaveCachedTranslation stores a translation in the cache, keyed by
// message + target + engine.
func (a *App) SaveCachedTranslation(messageID, targetLang, engine, subject, body string) error {
	switch engine {
	case translate.EngineBergamot, translate.EngineGoogle, translate.EngineDeepL, translate.EngineOS:
	default:
		return fmt.Errorf("unknown translation engine: %s", engine)
	}
	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return fmt.Errorf("message not found: %s", messageID)
	}
	hash := translate.BodyHash(msg.Subject, msg.BodyText, msg.BodyHTML)
	return translate.SaveCached(a.db, messageID, translate.NormalizeLang(targetLang), engine, subject, body, hash)
}

// TranslateMessageOnline translates text segments for one message
// through the configured online provider, behind the consent gate.
// The frontend sends the subject + body text segments (already split
// from the HTML so tags never reach the provider) and re-injects the
// results; it then caches the translated HTML itself.
func (a *App) TranslateMessageOnline(messageID string, texts []string, sourceLang, targetLang string) ([]string, error) {
	msg, err := a.messageStore.Get(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}
	return a.translationDeps().translateOnline(msg, texts, sourceLang, targetLang)
}

// BergamotModelsResult is what ListBergamotModels returns: the
// downloaded pairs plus their total disk use.
type BergamotModelsResult struct {
	Pairs     []translate.DownloadedPair `json:"pairs"`
	TotalSize int64                      `json:"totalSize"`
}

// ListBergamotModels lists downloaded models and their total disk use.
func (a *App) ListBergamotModels() (*BergamotModelsResult, error) {
	deps := a.translationDeps()
	pairs, err := translate.ListDownloadedPairs(deps.dataDir)
	if err != nil {
		return nil, err
	}
	total, err := translate.TotalModelsSize(deps.dataDir)
	if err != nil {
		return nil, err
	}
	return &BergamotModelsResult{Pairs: pairs, TotalSize: total}, nil
}

// GetBergamotPairStatus returns the per-leg status of a route.
func (a *App) GetBergamotPairStatus(from, to string) ([]BergamotLegStatus, error) {
	return a.translationDeps().bergamotLegs(from, to)
}

// GetBergamotSupportedPairs lists every pair the catalogue offers.
func (a *App) GetBergamotSupportedPairs() ([][2]string, error) {
	records, err := translate.FetchRegistry(context.Background(), a.paths.Data, a.translationDeps().client())
	if err != nil {
		return nil, err
	}
	return translate.SupportedPairs(records), nil
}

// DownloadBergamotModel downloads every leg of a route, with progress
// events ("bergamot:download-progress": {kind, from, to, downloaded,
// total}). The engine downloads first when it is not cached yet.
func (a *App) DownloadBergamotModel(from, to string) error {
	deps := a.translationDeps()
	return deps.downloadBergamotPair(from, to, func(legFrom, legTo string, downloaded, total int64) {
		emitDownloadProgress(a.ctx, legFrom, legTo, downloaded, total)
	})
}

// DeleteBergamotModel removes one downloaded pair from disk.
func (a *App) DeleteBergamotModel(from, to string) error {
	return translate.DeletePair(a.paths.Data, from, to)
}

// CancelBergamotDownload cancels the in-flight Bergamot model
// download, if any.
func (a *App) CancelBergamotDownload() {
	a.translationDeps().cancelDownload()
}

// GetBergamotEngineStatus reports whether the offline engine is
// downloaded and ready, and its size.
func (a *App) GetBergamotEngineStatus() translate.EngineStatus {
	return translate.CheckEngineStatus(a.paths.Data)
}

// DownloadBergamotEngine downloads the offline engine (WASM + loader,
// ~5 MB) into the data folder, with progress events
// ("bergamot:download-progress" with kind "engine").
func (a *App) DownloadBergamotEngine() error {
	return translate.DownloadEngine(context.Background(), a.paths.Data, a.translationDeps().client(), func(downloaded, total int64) {
		emitDownloadProgress(a.ctx, "engine", "", downloaded, total)
	})
}

// DeleteBergamotEngine removes the downloaded engine from disk.
func (a *App) DeleteBergamotEngine() error {
	return translate.DeleteEngine(a.paths.Data)
}

// GetBergamotLanguages lists every language the catalogue has an
// offline model for, with download sizes and downloaded state — the
// Settings → Translation download manager's list. When the catalogue
// cannot be fetched (offline), the built-in snapshot stands in, so the
// list still shows what exists.
func (a *App) GetBergamotLanguages() ([]translate.BergamotLanguage, error) {
	deps := a.translationDeps()
	records, err := translate.FetchRegistry(context.Background(), deps.dataDir, deps.client())
	if err != nil {
		return translate.FallbackBergamotLanguages(deps.dataDir), nil
	}
	return translate.ListBergamotLanguages(deps.dataDir, records), nil
}

// DownloadBergamotLanguage downloads every direction the catalogue has
// for a language, with progress events. The engine downloads first
// when it is not cached yet.
func (a *App) DownloadBergamotLanguage(language string) error {
	deps := a.translationDeps()
	return deps.downloadBergamotLanguage(language, func(legFrom, legTo string, downloaded, total int64) {
		emitDownloadProgress(a.ctx, legFrom, legTo, downloaded, total)
	})
}

// DeleteBergamotLanguage removes every downloaded direction of a
// language from disk.
func (a *App) DeleteBergamotLanguage(language string) error {
	return translate.DeleteLanguage(a.paths.Data, language)
}

// ============================================================================
// ViewerApp bindings (detached popup) - mirrors App
// ============================================================================

func (v *ViewerApp) translationDeps() *translationDeps {
	return &translationDeps{
		db:            v.db,
		messageStore:  v.messageStore,
		settingsStore: v.settingsStore,
		credStore:     v.credStore,
		dataDir:       v.paths.Data,
	}
}

// GetTranslationSettings returns Settings → Translation.
func (v *ViewerApp) GetTranslationSettings(uiLanguage string) (*TranslationSettings, error) {
	return v.translationDeps().getSettings(uiLanguage)
}

// GetTranslationTargetLanguages returns the target-language picker list.
func (v *ViewerApp) GetTranslationTargetLanguages() []TranslationTargetLanguage {
	out := make([]TranslationTargetLanguage, 0, len(translate.TargetLanguages))
	for _, l := range translate.TargetLanguages {
		out = append(out, TranslationTargetLanguage{Code: l.Code, Name: l.Name})
	}
	return out
}

// SetTranslationEnabled enables or disables translation offers.
func (v *ViewerApp) SetTranslationEnabled(enabled bool) error {
	return v.settingsStore.SetTranslateEnabled(enabled)
}

// SetTranslationMode sets the translation mode.
func (v *ViewerApp) SetTranslationMode(mode string) error {
	return v.settingsStore.SetTranslateMode(mode)
}

// SetTranslationTargetLanguage sets the target language.
func (v *ViewerApp) SetTranslationTargetLanguage(language string) error {
	return v.settingsStore.SetTranslateTargetLanguage(language)
}

// SetTranslationOnlineProvider sets the online provider.
func (v *ViewerApp) SetTranslationOnlineProvider(provider string) error {
	return v.settingsStore.SetTranslateOnlineProvider(provider)
}

// SetTranslationOnlineConsent records or revokes consent for one
// online provider.
func (v *ViewerApp) SetTranslationOnlineConsent(provider string, granted bool) error {
	return v.settingsStore.SetTranslateOnlineConsent(provider, granted)
}

// SetDeepLAPIKey stores the DeepL API key in the credentials store.
// Empty clears it.
func (v *ViewerApp) SetDeepLAPIKey(key string) error {
	return v.credStore.SetServiceSecret(credentials.ServiceSecretDeepL, key)
}

// DetectMessageLanguage runs read-time detection for one message.
func (v *ViewerApp) DetectMessageLanguage(messageID, uiLanguage string) (*MessageDetection, error) {
	msg, err := v.messageStore.Get(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}
	return v.translationDeps().detectForMessage(msg, uiLanguage)
}

// GetCachedTranslation returns the cached translation for a message +
// target + engine when the body has not changed.
func (v *ViewerApp) GetCachedTranslation(messageID, targetLang, engine string) (*CachedTranslationResult, error) {
	msg, err := v.messageStore.Get(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}
	hash := translate.BodyHash(msg.Subject, msg.BodyText, msg.BodyHTML)
	cached, found, err := translate.GetCached(v.db, messageID, translate.NormalizeLang(targetLang), engine, hash)
	if err != nil {
		return nil, err
	}
	if !found {
		return &CachedTranslationResult{Found: false}, nil
	}
	return &CachedTranslationResult{Found: true, Engine: cached.Engine, Subject: cached.TranslatedSubject, Body: cached.TranslatedBody}, nil
}

// SaveCachedTranslation stores a translation in the cache.
func (v *ViewerApp) SaveCachedTranslation(messageID, targetLang, engine, subject, body string) error {
	switch engine {
	case translate.EngineBergamot, translate.EngineGoogle, translate.EngineDeepL, translate.EngineOS:
	default:
		return fmt.Errorf("unknown translation engine: %s", engine)
	}
	msg, err := v.messageStore.Get(messageID)
	if err != nil {
		return fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return fmt.Errorf("message not found: %s", messageID)
	}
	hash := translate.BodyHash(msg.Subject, msg.BodyText, msg.BodyHTML)
	return translate.SaveCached(v.db, messageID, translate.NormalizeLang(targetLang), engine, subject, body, hash)
}

// TranslateMessageOnline translates text segments for one message
// through the configured online provider, behind the consent gate.
func (v *ViewerApp) TranslateMessageOnline(messageID string, texts []string, sourceLang, targetLang string) ([]string, error) {
	msg, err := v.messageStore.Get(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	if msg == nil {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}
	return v.translationDeps().translateOnline(msg, texts, sourceLang, targetLang)
}

// ListBergamotModels lists downloaded models and their total disk use.
func (v *ViewerApp) ListBergamotModels() (*BergamotModelsResult, error) {
	deps := v.translationDeps()
	pairs, err := translate.ListDownloadedPairs(deps.dataDir)
	if err != nil {
		return nil, err
	}
	total, err := translate.TotalModelsSize(deps.dataDir)
	if err != nil {
		return nil, err
	}
	return &BergamotModelsResult{Pairs: pairs, TotalSize: total}, nil
}

// GetBergamotPairStatus returns the per-leg status of a route.
func (v *ViewerApp) GetBergamotPairStatus(from, to string) ([]BergamotLegStatus, error) {
	return v.translationDeps().bergamotLegs(from, to)
}

// GetBergamotSupportedPairs lists every pair the catalogue offers.
func (v *ViewerApp) GetBergamotSupportedPairs() ([][2]string, error) {
	records, err := translate.FetchRegistry(context.Background(), v.paths.Data, v.translationDeps().client())
	if err != nil {
		return nil, err
	}
	return translate.SupportedPairs(records), nil
}

// DownloadBergamotModel downloads every leg of a route, with progress
// events. The engine downloads first when it is not cached yet.
func (v *ViewerApp) DownloadBergamotModel(from, to string) error {
	deps := v.translationDeps()
	return deps.downloadBergamotPair(from, to, func(legFrom, legTo string, downloaded, total int64) {
		emitDownloadProgress(v.ctx, legFrom, legTo, downloaded, total)
	})
}

// DeleteBergamotModel removes one downloaded pair from disk.
func (v *ViewerApp) DeleteBergamotModel(from, to string) error {
	return translate.DeletePair(v.paths.Data, from, to)
}

// CancelBergamotDownload cancels the in-flight Bergamot model
// download, if any.
func (v *ViewerApp) CancelBergamotDownload() {
	v.translationDeps().cancelDownload()
}

// GetBergamotEngineStatus reports whether the offline engine is
// downloaded and ready, and its size.
func (v *ViewerApp) GetBergamotEngineStatus() translate.EngineStatus {
	return translate.CheckEngineStatus(v.paths.Data)
}

// DownloadBergamotEngine downloads the offline engine (WASM + loader,
// ~5 MB) into the data folder, with progress events.
func (v *ViewerApp) DownloadBergamotEngine() error {
	return translate.DownloadEngine(context.Background(), v.paths.Data, v.translationDeps().client(), func(downloaded, total int64) {
		emitDownloadProgress(v.ctx, "engine", "", downloaded, total)
	})
}

// DeleteBergamotEngine removes the downloaded engine from disk.
func (v *ViewerApp) DeleteBergamotEngine() error {
	return translate.DeleteEngine(v.paths.Data)
}

// GetBergamotLanguages lists every language the catalogue has an
// offline model for, with download sizes and downloaded state.
func (v *ViewerApp) GetBergamotLanguages() ([]translate.BergamotLanguage, error) {
	deps := v.translationDeps()
	records, err := translate.FetchRegistry(context.Background(), deps.dataDir, deps.client())
	if err != nil {
		return translate.FallbackBergamotLanguages(deps.dataDir), nil
	}
	return translate.ListBergamotLanguages(deps.dataDir, records), nil
}

// DownloadBergamotLanguage downloads every direction the catalogue has
// for a language, with progress events. The engine downloads first
// when it is not cached yet.
func (v *ViewerApp) DownloadBergamotLanguage(language string) error {
	deps := v.translationDeps()
	return deps.downloadBergamotLanguage(language, func(legFrom, legTo string, downloaded, total int64) {
		emitDownloadProgress(v.ctx, legFrom, legTo, downloaded, total)
	})
}

// DeleteBergamotLanguage removes every downloaded direction of a
// language from disk.
func (v *ViewerApp) DeleteBergamotLanguage(language string) error {
	return translate.DeleteLanguage(v.paths.Data, language)
}
