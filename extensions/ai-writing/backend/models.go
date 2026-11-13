package backend

// ModelInfo is the Wails-visible shape of one selectable model. The ID is
// the `prefix:model` string the provider router in provider.go switches
// on, so settings stay compatible in spirit with the Thunderbird add-on.
type ModelInfo struct {
	ID       string `json:"id"`       // e.g. "openai:gpt-5", "custom:"
	Label    string `json:"label"`    // e.g. "OpenAI: GPT-5"
	Provider string `json:"provider"` // "openai" | "groq" | "google" | "xai" | "custom"
}

// defaultModels is the model list copied verbatim from the Thunderbird
// add-on's options.html (first option, openai:gpt-5, is the default
// selection), plus the two xAI Grok models from the plan (§4 — the TB
// add-on predates the xAI route). Model names go stale fast; treat this
// list as data, not code — a refresh pass is due before release.
var defaultModels = []ModelInfo{
	{ID: "openai:gpt-5", Label: "OpenAI: GPT-5", Provider: "openai"},
	{ID: "openai:gpt-5-mini", Label: "OpenAI: GPT-5 mini", Provider: "openai"},
	{ID: "openai:gpt-5-nano", Label: "OpenAI: GPT-5 nano", Provider: "openai"},
	{ID: "openai:gpt-4o", Label: "OpenAI: GPT-4o", Provider: "openai"},
	{ID: "openai:gpt-4o-mini", Label: "OpenAI: GPT-4o Mini", Provider: "openai"},
	{ID: "openai:o3-mini", Label: "OpenAI: O3 mini", Provider: "openai"},
	{ID: "groq:openai/gpt-oss-20b", Label: "Groq: OpenAI GPT-OSS 20B", Provider: "groq"},
	{ID: "groq:openai/gpt-oss-120b", Label: "Groq: OpenAI GPT-OSS 120B", Provider: "groq"},
	{ID: "groq:llama-3.3-70b-versatile", Label: "Groq: Llama 3.3 70B Versatile 128k", Provider: "groq"},
	{ID: "google:gemini-3.8-flash", Label: "Google: Gemini 3.8 Flash", Provider: "google"},
	{ID: "google:gemini-3.7-flash", Label: "Google: Gemini 3.7 Flash", Provider: "google"},
	{ID: "google:gemini-3.6-flash", Label: "Google: Gemini 3.6 Flash", Provider: "google"},
	{ID: "google:gemini-3.5-flash-lite", Label: "Google: Gemini 3.5 Flash Lite", Provider: "google"},
	{ID: "xai:grok-4.7", Label: "xAI: Grok 4.7", Provider: "xai"},
	{ID: "xai:grok-4", Label: "xAI: Grok 4", Provider: "xai"},
	{ID: "custom:", Label: "custom: Ollama API & Models", Provider: "custom"},
}

// ListModels returns the selectable model list. The frontend reads it via
// AIWriting_ListModels instead of hardcoding its own copy, so the list
// stays in one place.
func ListModels() []ModelInfo {
	out := make([]ModelInfo, len(defaultModels))
	copy(out, defaultModels)
	return out
}
