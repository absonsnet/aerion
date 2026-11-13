package backend

import (
	"strings"
	"testing"
)

func validInput() SettingsInput {
	return SettingsInput{
		SelectedModel:          "openai:gpt-5",
		Temperature:            0.7,
		MaxTokens:              2000,
		CustomAPIEndpoint:      DefaultCustomAPIEndpoint,
		CustomModel:            "",
		PromptImprove:          DefaultPromptImprove,
		UseConversationHistory: true,
		APIKey:                 "sk-test",
	}
}

func TestValidateSettings(t *testing.T) {
	cases := []struct {
		name         string
		mutate       func(*SettingsInput)
		hasStoredKey bool
		wantErr      string // substring; "" = no error
	}{
		{"valid", func(*SettingsInput) {}, false, ""},
		{"valid with stored key, empty api key", func(in *SettingsInput) { in.APIKey = "" }, true, ""},
		{"missing key, none stored", func(in *SettingsInput) { in.APIKey = "" }, false, "API key"},
		{"custom needs no key", func(in *SettingsInput) {
			in.SelectedModel = "custom:"
			in.APIKey = ""
			in.CustomModel = "llama3"
		}, false, ""},
		{"custom missing model", func(in *SettingsInput) {
			in.SelectedModel = "custom:"
			in.CustomModel = ""
		}, false, "custom model"},
		{"custom missing endpoint", func(in *SettingsInput) {
			in.SelectedModel = "custom:"
			in.CustomModel = "llama3"
			in.CustomAPIEndpoint = ""
		}, false, "custom API endpoint"},
		{"empty model", func(in *SettingsInput) { in.SelectedModel = "" }, false, "select a model"},
		{"unknown provider", func(in *SettingsInput) { in.SelectedModel = "anthropic:claude" }, false, "unsupported model"},
		{"empty prompt", func(in *SettingsInput) { in.PromptImprove = "  " }, false, "improve prompt"},
		{"max tokens zero", func(in *SettingsInput) { in.MaxTokens = 0 }, false, "max tokens"},
		{"max tokens too high", func(in *SettingsInput) { in.MaxTokens = 4001 }, false, "max tokens"},
		{"max tokens boundary 4000 ok", func(in *SettingsInput) { in.MaxTokens = 4000 }, false, ""},
		{"max tokens boundary 1 ok", func(in *SettingsInput) { in.MaxTokens = 1 }, false, ""},
		{"temperature negative", func(in *SettingsInput) { in.Temperature = -0.1 }, false, "temperature"},
		{"temperature too high", func(in *SettingsInput) { in.Temperature = 2.1 }, false, "temperature"},
		{"temperature boundary 2 ok", func(in *SettingsInput) { in.Temperature = 2 }, false, ""},
		{"temperature boundary 0 ok", func(in *SettingsInput) { in.Temperature = 0 }, false, ""},
	}
	for _, c := range cases {
		in := validInput()
		c.mutate(&in)
		err := ValidateSettings(in, c.hasStoredKey)
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("%s: unexpected error: %v", c.name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: expected error containing %q", c.name, c.wantErr)
			continue
		}
		if !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: error %q must contain %q", c.name, err, c.wantErr)
		}
	}
}

func TestValidateAPIKeyPrefix(t *testing.T) {
	cases := []struct {
		model string
		key   string
		want  string // expected prefix substring in error; "" = no error
	}{
		{"openai:gpt-5", "sk-abc", ""},
		{"openai:gpt-5", "sk-proj-abc", ""},
		{"openai:gpt-5", "gsk_abc", "sk-"},
		{"groq:openai/gpt-oss-120b", "gsk_abc", ""},
		{"groq:openai/gpt-oss-120b", "sk-abc", "gsk_"},
		{"google:gemini-3.8-flash", "AIzaAbc", ""},
		{"google:gemini-3.8-flash", "sk-abc", "AIza"},
		{"xai:grok-4", "xai-abc", ""},
		{"xai:grok-4", "sk-abc", "xai-"},
		{"custom:", "anything-goes", ""},
		{"custom:", "", ""},
	}
	for _, c := range cases {
		err := ValidateAPIKeyPrefix(c.model, c.key)
		if c.want == "" {
			if err != nil {
				t.Errorf("%s %q: unexpected error: %v", c.model, c.key, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s %q: expected error naming %q", c.model, c.key, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: error %q must name expected prefix %q", c.model, c.key, err, c.want)
		}
	}
}

func TestDefaultSettings(t *testing.T) {
	st := DefaultSettings()
	if st.SelectedModel != "openai:gpt-5" {
		t.Errorf("default model = %q", st.SelectedModel)
	}
	if st.Temperature != 0.7 || st.MaxTokens != 2000 {
		t.Errorf("defaults = %v/%v", st.Temperature, st.MaxTokens)
	}
	if st.CustomAPIEndpoint != "http://localhost:11434/api/chat" {
		t.Errorf("default endpoint = %q", st.CustomAPIEndpoint)
	}
	if st.PromptImprove != DefaultPromptImprove {
		t.Errorf("default prompt is not DefaultPromptImprove")
	}
	if !st.UseConversationHistory {
		t.Errorf("default history must be on")
	}
	if st.HasAPIKey {
		t.Errorf("default HasAPIKey must be false")
	}
}

func TestListModels_MatchesTBList(t *testing.T) {
	got := ListModels()
	if len(got) != 16 {
		t.Fatalf("len = %d, want 16 (13 TB + 2 xAI + 1 custom)", len(got))
	}
	if got[0].ID != "openai:gpt-5" {
		t.Errorf("first model = %q, want openai:gpt-5 (TB default selection)", got[0].ID)
	}
	ids := map[string]bool{}
	for _, m := range got {
		if ids[m.ID] {
			t.Errorf("duplicate model id %q", m.ID)
		}
		ids[m.ID] = true
	}
	for _, want := range []string{
		"openai:gpt-5", "openai:gpt-5-mini", "openai:gpt-5-nano", "openai:gpt-4o",
		"openai:gpt-4o-mini", "openai:o3-mini",
		"groq:openai/gpt-oss-20b", "groq:openai/gpt-oss-120b", "groq:llama-3.3-70b-versatile",
		"google:gemini-3.8-flash", "google:gemini-3.7-flash", "google:gemini-3.6-flash",
		"google:gemini-3.5-flash-lite",
		"xai:grok-4.7", "xai:grok-4",
		"custom:",
	} {
		if !ids[want] {
			t.Errorf("missing model %q", want)
		}
	}
}

func TestListActions_SevenActions(t *testing.T) {
	// Seven actions in registry order (Nov 13 decision).
	acts := ListActions()
	want := []ActionInfo{
		{ID: "improve", Label: "Improve Writing"},
		{ID: "fix-grammar", Label: "Fix Grammar"},
		{ID: "make-formal", Label: "Make Formal"},
		{ID: "shorten", Label: "Shorten"},
		{ID: "expand", Label: "Expand"},
		{ID: "make-friendly", Label: "Make Friendly"},
		{ID: "generate-reply", Label: "Generate Reply"},
	}
	if len(acts) != len(want) {
		t.Fatalf("actions = %+v, want %v", acts, want)
	}
	for i, w := range want {
		if acts[i] != w {
			t.Errorf("actions[%d] = %+v, want %+v", i, acts[i], w)
		}
	}
	if findAction("improve") == nil || findAction("nope") != nil {
		t.Errorf("findAction mismatch")
	}
}
