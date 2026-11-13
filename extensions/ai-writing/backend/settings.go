package backend

import (
	"fmt"
	"strings"
)

// Settings is the non-sensitive AI Writing configuration, stored as one
// JSON blob in the per-extension SQLite (see store.go). JSON tags drive
// the Wails/TS binding shape — keep stable.
//
// There is deliberately NO apiKey field here and NO PromptHTML2Text:
// the API key never leaves the secret store (see secrets.go notes in
// bridge.go — key lives under Secrets("ai-writing") key "api_key"), and
// plain-text conversion is local in the frontend in v1.
type Settings struct {
	SelectedModel          string  `json:"selectedModel"`
	Temperature            float64 `json:"temperature"`
	MaxTokens              int     `json:"maxTokens"`
	CustomAPIEndpoint      string  `json:"customApiEndpoint"`
	CustomModel            string  `json:"customModel"`
	PromptImprove          string  `json:"promptImprove"`
	UseConversationHistory bool    `json:"useConversationHistory"`
	HasAPIKey              bool    `json:"hasApiKey"`
}

// SettingsInput is what the frontend sends to AIWriting_SaveSettings:
// the same fields as Settings, plus APIKey. An empty APIKey means "keep
// the existing stored key" — there is no way to clear the key through
// this method in v1.
type SettingsInput struct {
	SelectedModel          string  `json:"selectedModel"`
	Temperature            float64 `json:"temperature"`
	MaxTokens              int     `json:"maxTokens"`
	CustomAPIEndpoint      string  `json:"customApiEndpoint"`
	CustomModel            string  `json:"customModel"`
	PromptImprove          string  `json:"promptImprove"`
	UseConversationHistory bool    `json:"useConversationHistory"`
	APIKey                 string  `json:"apiKey"`
}

// Status is the lightweight health snapshot AIWriting_GetStatus returns.
type Status struct {
	Enabled       bool   `json:"enabled"`
	HasAPIKey     bool   `json:"hasApiKey"`
	SelectedModel string `json:"selectedModel"`
}

// ImproveRequest is the input to AIWriting_Improve. DraftHTML is the
// user-composed part of the mail body; QuotedHTML is the quoted history
// (may be empty for new mail). IsNewEmail selects the subject-line
// instruction for new mail.
type ImproveRequest struct {
	ActionID   string `json:"actionId"`
	DraftHTML  string `json:"draftHtml"`
	QuotedHTML string `json:"quotedHtml"`
	IsNewEmail bool   `json:"isNewEmail"`
}

// ImproveResult is the output of AIWriting_Improve. ImprovedHTML is the
// revised draft only (quoted history is NOT included — the frontend
// re-appends it). Subjects holds the parsed <subject> suggestions for
// new mail (empty otherwise).
type ImproveResult struct {
	ImprovedHTML string   `json:"improvedHtml"`
	Subjects     []string `json:"subjects"`
}

// Defaults mirror the Thunderbird add-on (options.js / options.html):
// temperature 0.7, maxTokens 2000, custom endpoint Ollama's default,
// conversation history on, first model-list option openai:gpt-5.
func DefaultSettings() Settings {
	return Settings{
		SelectedModel:          "openai:gpt-5",
		Temperature:            0.7,
		MaxTokens:              2000,
		CustomAPIEndpoint:      "http://localhost:11434/api/chat",
		CustomModel:            "",
		PromptImprove:          DefaultPromptImprove,
		UseConversationHistory: true,
		HasAPIKey:              false,
	}
}

// DefaultCustomAPIEndpoint is the Ollama default from options.js, kept
// as a named constant for provider.go.
const DefaultCustomAPIEndpoint = "http://localhost:11434/api/chat"

// ValidateSettings checks a SettingsInput the way options.js does on
// save. hasStoredKey reports whether a key is already in the secret
// store (so an empty APIKey on save is fine when one is stored).
// Key-prefix validation runs only on a newly supplied key; see
// ValidateAPIKeyPrefix.
func ValidateSettings(in SettingsInput, hasStoredKey bool) error {
	if strings.TrimSpace(in.SelectedModel) == "" {
		return fmt.Errorf("ai-writing: please select a model")
	}
	provider, _ := splitModelID(in.SelectedModel)
	switch provider {
	case "openai", "groq", "google", "xai", "custom":
	default:
		return fmt.Errorf("ai-writing: unsupported model %q", in.SelectedModel)
	}
	if provider == "custom" {
		if strings.TrimSpace(in.CustomModel) == "" {
			return fmt.Errorf("ai-writing: please enter a custom model")
		}
		if strings.TrimSpace(in.CustomAPIEndpoint) == "" {
			return fmt.Errorf("ai-writing: please enter a custom API endpoint")
		}
	} else if strings.TrimSpace(in.APIKey) == "" && !hasStoredKey {
		// Mirrors TB options.js: non-custom models require a key.
		return fmt.Errorf("ai-writing: please enter an API key")
	}
	if strings.TrimSpace(in.PromptImprove) == "" {
		return fmt.Errorf("ai-writing: please enter the improve prompt")
	}
	if in.MaxTokens < 1 || in.MaxTokens > 4000 {
		return fmt.Errorf("ai-writing: max tokens must be between 1 and 4000")
	}
	if in.Temperature < 0 || in.Temperature > 2 {
		return fmt.Errorf("ai-writing: temperature must be between 0 and 2")
	}
	return nil
}

// keyPrefixes maps each single-prefix provider to the API-key prefix its
// keys start with (plan §4). Custom/Ollama takes no key, so it has none.
// Google is absent: it accepts two prefixes (see ValidateAPIKeyPrefix).
var keyPrefixes = map[string]string{
	"openai": "sk-",
	"groq":   "gsk_",
	"xai":    "xai-",
}

// ValidateAPIKeyPrefix checks a newly supplied key against the expected
// prefix for the selected model's provider. The error names the expected
// prefix so the frontend can show it. Custom models skip the check.
// OpenAI accepts both legacy `sk-` and current project `sk-proj-` keys.
// Google accepts both the older standard `AIza` keys and the `AQ.` auth
// keys AI Studio has issued since 28 May 2026 (the old format is being
// phased out, so new keys will be `AQ.`).
func ValidateAPIKeyPrefix(modelID, key string) error {
	provider, _ := splitModelID(modelID)
	if provider == "custom" {
		return nil
	}
	if provider == "google" {
		if strings.HasPrefix(key, "AIza") || strings.HasPrefix(key, "AQ.") {
			return nil
		}
		return fmt.Errorf("ai-writing: Google API key must start with %q (older keys) or %q (new AI Studio keys)", "AIza", "AQ.")
	}
	want, ok := keyPrefixes[provider]
	if !ok {
		return fmt.Errorf("ai-writing: unsupported model %q", modelID)
	}
	if provider == "openai" {
		if strings.HasPrefix(key, "sk-proj-") || strings.HasPrefix(key, "sk-") {
			return nil
		}
		return fmt.Errorf("ai-writing: OpenAI API key must start with %q (or %q for project keys)", "sk-", "sk-proj-")
	}
	if !strings.HasPrefix(key, want) {
		return fmt.Errorf("ai-writing: API key for %s must start with %q", provider, want)
	}
	return nil
}

// splitModelID splits a `prefix:model` ID on the first colon. "custom:"
// yields provider "custom" and empty model, matching the TB routing.
func splitModelID(id string) (provider, model string) {
	if i := strings.Index(id, ":"); i >= 0 {
		return id[:i], id[i+1:]
	}
	return id, ""
}

// toSettings converts validated input into the storable Settings shape.
// HasAPIKey is filled in by the caller from the secret store.
func (in SettingsInput) toSettings() Settings {
	return Settings{
		SelectedModel:          in.SelectedModel,
		Temperature:            in.Temperature,
		MaxTokens:              in.MaxTokens,
		CustomAPIEndpoint:      in.CustomAPIEndpoint,
		CustomModel:            in.CustomModel,
		PromptImprove:          in.PromptImprove,
		UseConversationHistory: in.UseConversationHistory,
	}
}
