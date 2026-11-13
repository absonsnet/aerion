package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
	"github.com/hkdb/aerion/internal/database"
	"github.com/hkdb/aerion/internal/platform"
)

// AIWritingBridge is the Wails-bindable surface for the AI Writing Assistant
// extension. It's embedded into the host `*app.App` struct; Go's
// method-promotion makes every AIWritingBridge method appear on App so
// Wails' reflection-based bind generator picks them up. All AI-Writing
// logic lives here, not in the host. The host's `app/extension_ai_writing.go`
// is reduced to a dozen lines of construction wiring (owned by the wiring
// worker, not this package).
//
// Method naming: all Wails-bound bridge methods use the `AIWriting_`
// prefix so they can't collide with another extension's methods after
// embedding into the same App. See docs/EXT_RULES.md R19.
//
// Lightweight-by-default invariant: when the user has the AI Writing Assistant
// extension disabled, NOTHING is loaded beyond the ~80-byte bridge struct
// itself. The per-extension SQLite + secrets handle are lazy-init via
// sync.Once inside `ensureInit`. The first enabled method call triggers
// init; subsequent calls are fast. See docs/EXT_RULES.md §4.
type AIWritingBridge struct {
	deps AIWritingBridgeDeps

	// Lazy-initialized state. Constructed on the first enabled bridge
	// call so disabled extensions contribute zero work.
	initOnce sync.Once
	initErr  error
	store    *Store
	secrets  coreapi.Secrets

	// caller issues provider requests. Default is the production caller
	// (120s timeout); tests swap it via newTestCaller.
	caller *providerCaller
}

// AIWritingBridgeDeps bundles the host-provided dependencies the bridge needs.
// Grouped into a struct so adding a new dep doesn't churn every call site
// in the host. Per docs/EXT_RULES.md R2, this struct holds NO closures
// wrapping `internal/*` calls — anything the extension needs from the host
// goes through `coreapi.Core` directly.
type AIWritingBridgeDeps struct {
	// SettingsStore is consulted on every bridge call for the enabled
	// gate (lightweight invariant — disabled calls short-circuit before
	// any work).
	SettingsStore SettingsStore

	// Paths gives the bridge access to the OS-appropriate data directory
	// for opening the extension's per-extension SQLite.
	Paths *platform.Paths

	// DB is the shared application database. Not used by the AI Writing Assistant
	// extension — all of its state lives in its own per-extension SQLite
	// (opened via Paths) plus the secret store. Kept here for symmetry
	// with Calendar/Contacts and forward-compat.
	DB *database.DB

	// Core is the coreapi.Core handle. The bridge uses it to reach the
	// host-implemented surfaces — currently `coreapi.Storage.Secrets`
	// for the API key storage.
	Core coreapi.Core
}

// SettingsStore is the narrow interface the bridge needs from the host's
// settings store. Defined here (rather than importing the concrete type)
// so 3rd-party extensions can swap in their own implementation for tests
// and so this file doesn't grow a host-package dependency.
type SettingsStore interface {
	IsExtensionEnabled(id string) (bool, error)
}

// NewAIWritingBridge constructs the bridge with its dependencies. Does NOT
// touch the DB or open any extension state — that's ensureInit's job,
// on the first enabled bridge call.
func NewAIWritingBridge(deps AIWritingBridgeDeps) *AIWritingBridge {
	return &AIWritingBridge{deps: deps, caller: defaultCaller}
}

// extensionID is the key the bridge looks up in settings for the
// enabled-state check, AND the scope passed to coreapi.Storage.Secrets.
// Kept as a const so a typo doesn't silently disable every bridge
// method or store the key in the wrong namespace.
const extensionID = "ai-writing"

// apiKeySecretKey is the key under which the provider API key is stored in
// Secrets("ai-writing"). The key value NEVER leaves the secret store:
// settings blobs, logs, and Wails return values carry only HasAPIKey.
const apiKeySecretKey = "api_key"

// gateEnabled returns true when the extension is currently enabled AND
// the host gave us a SettingsStore. Returns false (silently) when the
// store is nil or when the settings read errors out.
func (b *AIWritingBridge) gateEnabled() bool {
	if b.deps.SettingsStore == nil {
		return false
	}
	enabled, err := b.deps.SettingsStore.IsExtensionEnabled(extensionID)
	if err != nil {
		return false
	}
	return enabled
}

// ensureInit lazily opens the per-extension SQLite (via
// extensions.OpenStore, migrations in store.go) and fetches the secrets
// handle from coreapi.Core, pre-scoped to this extension's ID. Runs once
// per process; subsequent calls return the first call's result.
func (b *AIWritingBridge) ensureInit() error {
	b.initOnce.Do(func() {
		if b.deps.Paths == nil {
			b.initErr = errors.New("ai-writing.AIWritingBridge: missing Paths in deps")
			return
		}
		if b.deps.Core == nil {
			b.initErr = errors.New("ai-writing.AIWritingBridge: missing Core in deps")
			return
		}

		store, err := NewStore(b.deps.Paths.Data)
		if err != nil {
			b.initErr = err
			return
		}
		b.store = store
		b.secrets = b.deps.Core.Storage().Secrets(extensionID)
	})
	return b.initErr
}

// loadSettings returns the stored settings overlaid on the defaults, with
// HasAPIKey resolved from the secret store. Callers must have run
// ensureInit first.
func (b *AIWritingBridge) loadSettings() (Settings, error) {
	st, err := b.store.GetSettings()
	if err != nil {
		return Settings{}, err
	}
	// Settings saved by v0.4.0 carry only PromptImprove: fold a
	// customized improve prompt into the per-action map (no-op when the
	// map already has one or the prompt is the default).
	migrateLegacyPrompt(&st)
	st.HasAPIKey = b.hasAPIKey()
	return st, nil
}

// hasAPIKey reports whether a key is stored. A secrets read error is
// treated as "no key" — the Improve path then surfaces the actionable
// "API key" error rather than a storage error.
func (b *AIWritingBridge) hasAPIKey() bool {
	if b.secrets == nil {
		return false
	}
	v, err := b.secrets.Get(apiKeySecretKey)
	return err == nil && v != ""
}

// loadAPIKey returns the stored key ("" when none is stored). The value
// is passed straight to the provider client and never logged.
func (b *AIWritingBridge) loadAPIKey() string {
	if b.secrets == nil {
		return ""
	}
	v, err := b.secrets.Get(apiKeySecretKey)
	if err != nil {
		return ""
	}
	return v
}

// --- Wails-bound surface (AIWriting_*) --------------------------------------
//
// All methods gate on gateEnabled() first, per R16: disabled = zero value
// + nil error, no errors surfaced. ensureInit runs once per process;
// subsequent calls are the cost of one sync.Once check.

// AIWriting_Improve revises the draft with the named action's system
// prompt and returns the improved HTML plus any subject suggestions (new
// mail only). The quoted history is context for the model only — it is
// never part of the returned text; the frontend re-appends it on insert.
func (b *AIWritingBridge) AIWriting_Improve(req ImproveRequest) (ImproveResult, error) {
	if !b.gateEnabled() {
		return ImproveResult{}, nil
	}
	if err := b.ensureInit(); err != nil {
		return ImproveResult{}, err
	}
	actionID := req.ActionID
	if actionID == "" {
		actionID = "improve"
	}
	action := findAction(actionID)
	if action == nil {
		return ImproveResult{}, fmt.Errorf("ai-writing: unknown action %q", actionID)
	}
	// Empty draft is rejected for every action EXCEPT generate-reply
	// with quoted context: an empty draft is the signal to write a
	// reply to the conversation, and the quoted history is what the
	// model replies to. generate-reply with no quoted context has
	// nothing to reply to and still errors.
	if strings.TrimSpace(req.DraftHTML) == "" {
		if action.ID != "generate-reply" {
			return ImproveResult{}, errors.New("ai-writing: draft is empty")
		}
		if strings.TrimSpace(req.QuotedHTML) == "" {
			return ImproveResult{}, errors.New("ai-writing: nothing to reply to — the draft is empty and there is no quoted conversation")
		}
	}

	st, err := b.loadSettings()
	if err != nil {
		return ImproveResult{}, err
	}
	// generate-reply with conversation history off has nothing to reply
	// to: buildUserText omits the CONTEXT block when history is off, so
	// an empty draft would reach the model with no context at all. The
	// history setting is a privacy choice, so the context is NOT forced
	// on — the call is rejected instead, before any provider call, with
	// the fix named in the error. (Sits here rather than with the
	// empty-draft guards above because it needs the loaded setting.)
	if action.ID == "generate-reply" && !st.UseConversationHistory {
		return ImproveResult{}, errors.New("ai-writing: conversation history is off — turn it on in AI Writing Assistant settings to generate a reply")
	}

	// The action's system prompt: a stored custom override when the user
	// edited this action's prompt in Settings, else the registry default.
	systemPrompt := st.PromptFor(action.ID)
	if req.IsNewEmail {
		systemPrompt += subjectInstruction
	}

	userText := buildUserText(req.DraftHTML, req.QuotedHTML, st.UseConversationHistory)
	raw, err := b.caller.improve(context.Background(), st.SelectedModel, systemPrompt, userText, st, b.loadAPIKey())
	if err != nil {
		return ImproveResult{}, err
	}

	subjects, cleaned := ParseSubjects(SanitizeLLMResult(raw))
	return ImproveResult{ImprovedHTML: cleaned, Subjects: subjects}, nil
}

// AIWriting_ListActions returns the registered actions (ID + label) for
// the frontend picker. v1 ships seven actions (see defaultActions).
func (b *AIWritingBridge) AIWriting_ListActions() ([]ActionInfo, error) {
	if !b.gateEnabled() {
		return nil, nil
	}
	if err := b.ensureInit(); err != nil {
		return nil, err
	}
	return ListActions(), nil
}

// AIWriting_GetActionPrompts returns every action's prompt state for
// the settings dialog's action picker: the registry id + label, the
// built-in default prompt, and the stored custom prompt ("" when the
// action uses its default).
func (b *AIWritingBridge) AIWriting_GetActionPrompts() ([]ActionPromptInfo, error) {
	if !b.gateEnabled() {
		return nil, nil
	}
	if err := b.ensureInit(); err != nil {
		return nil, err
	}
	st, err := b.loadSettings()
	if err != nil {
		return nil, err
	}
	out := make([]ActionPromptInfo, 0, len(defaultActions))
	for _, a := range defaultActions {
		custom := ""
		if st.ActionPrompts != nil {
			custom = st.ActionPrompts[a.ID]
		}
		out = append(out, ActionPromptInfo{
			ID:            a.ID,
			Label:         a.Label,
			DefaultPrompt: a.SystemPrompt,
			CustomPrompt:  custom,
		})
	}
	return out, nil
}

// AIWriting_ListModels returns the selectable model list (ID + label +
// provider) so the frontend never hardcodes its own copy.
func (b *AIWritingBridge) AIWriting_ListModels() ([]ModelInfo, error) {
	if !b.gateEnabled() {
		return nil, nil
	}
	if err := b.ensureInit(); err != nil {
		return nil, err
	}
	return ListModels(), nil
}

// AIWriting_GetSettings returns the stored settings over defaults.
// HasAPIKey reports whether a key is stored; the key value itself is
// never returned.
func (b *AIWritingBridge) AIWriting_GetSettings() (Settings, error) {
	if !b.gateEnabled() {
		return Settings{}, nil
	}
	if err := b.ensureInit(); err != nil {
		return Settings{}, err
	}
	return b.loadSettings()
}

// AIWriting_SaveSettings validates and persists the settings. A
// non-empty APIKey is prefix-checked for the selected provider and then
// stored in the secret store; an empty APIKey keeps the existing stored
// key. Non-sensitive fields go to the per-extension SQLite as one JSON
// blob (see store.go).
func (b *AIWritingBridge) AIWriting_SaveSettings(in SettingsInput) error {
	if !b.gateEnabled() {
		return nil
	}
	if err := b.ensureInit(); err != nil {
		return err
	}
	hasStored := b.hasAPIKey()
	if err := ValidateSettings(in, hasStored); err != nil {
		return err
	}
	if strings.TrimSpace(in.APIKey) != "" {
		if err := ValidateAPIKeyPrefix(in.SelectedModel, strings.TrimSpace(in.APIKey)); err != nil {
			return err
		}
		if err := b.secrets.Set(apiKeySecretKey, strings.TrimSpace(in.APIKey)); err != nil {
			return fmt.Errorf("ai-writing: save API key: %w", err)
		}
	}
	st := in.toSettings()
	st.HasAPIKey = hasStored || strings.TrimSpace(in.APIKey) != ""
	if err := b.store.SaveSettings(st); err != nil {
		return err
	}
	return nil
}

// AIWriting_GetStatus returns the lightweight health snapshot: enabled,
// whether an API key is stored, and the selected model.
func (b *AIWritingBridge) AIWriting_GetStatus() (Status, error) {
	if !b.gateEnabled() {
		return Status{}, nil
	}
	if err := b.ensureInit(); err != nil {
		return Status{}, err
	}
	st, err := b.loadSettings()
	if err != nil {
		return Status{}, err
	}
	return Status{Enabled: true, HasAPIKey: st.HasAPIKey, SelectedModel: st.SelectedModel}, nil
}
