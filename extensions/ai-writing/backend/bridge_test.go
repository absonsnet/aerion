package backend

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	coreapi "github.com/hkdb/aerion/internal/core/api/v1"
	"github.com/hkdb/aerion/internal/platform"
)

// --- stubs (same package, so the bridge's unexported fields are reachable) --

type stubSettingsStore struct {
	enabled bool
	err     error
}

func (s *stubSettingsStore) IsExtensionEnabled(id string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	if id != extensionID {
		panic("stubSettingsStore: unexpected id " + id)
	}
	return s.enabled, nil
}

type stubSecrets struct {
	m map[string]string
}

func (s *stubSecrets) Set(key, value string) error {
	if value == "" {
		delete(s.m, key)
		return nil
	}
	s.m[key] = value
	return nil
}

func (s *stubSecrets) Get(key string) (string, error) { return s.m[key], nil }
func (s *stubSecrets) Delete(key string) error {
	delete(s.m, key)
	return nil
}
func (s *stubSecrets) DeleteAll() error {
	s.m = map[string]string{}
	return nil
}

type stubStorage struct {
	secrets coreapi.Secrets
}

func (s *stubStorage) KV(extensionID string) coreapi.KVStore { return nil }
func (s *stubStorage) Secrets(extensionID string) coreapi.Secrets {
	return s.secrets
}
func (s *stubStorage) HostSecrets() coreapi.HostSecrets { return nil }

type stubCore struct {
	coreapi.Core // embedded: all other Core methods nil-panic if called
	storage      coreapi.Storage
}

func (c *stubCore) Storage() coreapi.Storage { return c.storage }

func newTestBridge(t *testing.T, enabled bool) (*AIWritingBridge, *stubSecrets) {
	t.Helper()
	secrets := &stubSecrets{m: map[string]string{}}
	core := &stubCore{storage: &stubStorage{secrets: secrets}}
	b := NewAIWritingBridge(AIWritingBridgeDeps{
		SettingsStore: &stubSettingsStore{enabled: enabled},
		Paths:         &platform.Paths{Data: t.TempDir()},
		Core:          core,
	})
	return b, secrets
}

func TestBridge_DisabledReturnsZeroValueNoError(t *testing.T) {
	b, _ := newTestBridge(t, false)
	dataDir := b.deps.Paths.Data

	if res, err := b.AIWriting_Improve(ImproveRequest{ActionID: "improve", DraftHTML: "<p>x</p>"}); err != nil || res.ImprovedHTML != "" || res.Subjects != nil {
		t.Errorf("Improve disabled: res=%+v err=%v", res, err)
	}
	if acts, err := b.AIWriting_ListActions(); err != nil || acts != nil {
		t.Errorf("ListActions disabled: acts=%v err=%v", acts, err)
	}
	if models, err := b.AIWriting_ListModels(); err != nil || models != nil {
		t.Errorf("ListModels disabled: models=%v err=%v", models, err)
	}
	if st, err := b.AIWriting_GetSettings(); err != nil || !reflect.DeepEqual(st, Settings{}) {
		t.Errorf("GetSettings disabled: st=%+v err=%v", st, err)
	}
	if err := b.AIWriting_SaveSettings(validInput()); err != nil {
		t.Errorf("SaveSettings disabled: err=%v", err)
	}
	if st, err := b.AIWriting_GetStatus(); err != nil || st != (Status{}) {
		t.Errorf("GetStatus disabled: st=%+v err=%v", st, err)
	}
	// Disabled must not have opened the DB at all.
	if _, err := os.Stat(filepath.Join(dataDir, "extensions", "ai-writing", "data.db")); !os.IsNotExist(err) {
		t.Errorf("disabled bridge created data.db")
	}
}

func TestBridge_SettingsRoundTrip(t *testing.T) {
	b, secrets := newTestBridge(t, true)

	st, err := b.AIWriting_GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if st.SelectedModel != "openai:gpt-5" || st.HasAPIKey {
		t.Fatalf("defaults: %+v", st)
	}

	in := validInput()
	in.Temperature = 1.25
	in.MaxTokens = 500
	if err := b.AIWriting_SaveSettings(in); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	// Key went to the secret store, not the settings blob.
	if got := secrets.m[apiKeySecretKey]; got != "sk-test" {
		t.Errorf("secret store key = %q", got)
	}

	st, err = b.AIWriting_GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if st.Temperature != 1.25 || st.MaxTokens != 500 {
		t.Errorf("round-trip: %+v", st)
	}
	if !st.HasAPIKey {
		t.Errorf("HasAPIKey must be true after saving a key")
	}

	// Saving again with an empty key keeps the stored key.
	in.APIKey = ""
	in.Temperature = 0.5
	if err := b.AIWriting_SaveSettings(in); err != nil {
		t.Fatalf("SaveSettings keep-key: %v", err)
	}
	if got := secrets.m[apiKeySecretKey]; got != "sk-test" {
		t.Errorf("empty-key save must keep stored key, got %q", got)
	}

	// A bad-prefix key is rejected and the stored key is untouched.
	in.APIKey = "gsk_wrong"
	if err := b.AIWriting_SaveSettings(in); err == nil || !strings.Contains(err.Error(), "sk-") {
		t.Errorf("bad-prefix save: err=%v", err)
	}
	if got := secrets.m[apiKeySecretKey]; got != "sk-test" {
		t.Errorf("rejected save changed stored key to %q", got)
	}

	status, err := b.AIWriting_GetStatus()
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !status.Enabled || !status.HasAPIKey || status.SelectedModel != "openai:gpt-5" {
		t.Errorf("status = %+v", status)
	}
}

func TestBridge_ImproveEndToEnd(t *testing.T) {
	// Fake OpenAI server: capture the request, return a fenced reply with
	// subject tags (new-mail flow).
	var gotBody []byte
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` +
			"```html<p>Improved draft</p>```" +
			`<subject>Subject One</subject><subject>Subject Two</subject>"}}]}`))
	}))
	t.Cleanup(srv.Close)

	b, _ := newTestBridge(t, true)
	b.caller = newTestCaller(map[string]string{"openai": srv.URL})

	in := validInput()
	in.APIKey = "sk-testkey"
	if err := b.AIWriting_SaveSettings(in); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	res, err := b.AIWriting_Improve(ImproveRequest{
		DraftHTML:  "<p>Draft</p>",
		QuotedHTML: "<p>Quoted</p>",
		IsNewEmail: true,
	})
	if err != nil {
		t.Fatalf("Improve: %v", err)
	}
	if res.ImprovedHTML != "<p>Improved draft</p>" {
		t.Errorf("improved = %q", res.ImprovedHTML)
	}
	if len(res.Subjects) != 2 || res.Subjects[0] != "Subject One" || res.Subjects[1] != "Subject Two" {
		t.Errorf("subjects = %v", res.Subjects)
	}
	if gotAuth != "Bearer sk-testkey" {
		t.Errorf("auth = %q", gotAuth)
	}

	// Request body: model from settings, system prompt = stored prompt +
	// subject instruction, user text = TB envelope with draft + history.
	var req openAIChatRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if req.Model != "gpt-5" {
		t.Errorf("model = %q", req.Model)
	}
	if !strings.Contains(req.Messages[0].Content, "You are revising an email draft") {
		t.Errorf("system prompt missing default prompt text")
	}
	if !strings.Contains(req.Messages[0].Content, "Also generate 3 professional") {
		t.Errorf("new-mail system prompt missing subject instruction")
	}
	if !strings.Contains(req.Messages[1].Content, "<!-- BEGIN DRAFT -->\n<p>Draft</p>\n<!-- END DRAFT -->") {
		t.Errorf("user text draft envelope = %q", req.Messages[1].Content)
	}
	if !strings.Contains(req.Messages[1].Content, "<p>Quoted</p>") {
		t.Errorf("user text missing quoted history: %q", req.Messages[1].Content)
	}
}

func TestBridge_ActionPrompts(t *testing.T) {
	// A custom prompt for one action applies only to that action; the
	// rest keep their registry defaults. The fake server captures the
	// system prompt actually sent.
	var gotSystem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req openAIChatRequest
		_ = json.Unmarshal(b, &req)
		if len(req.Messages) > 0 {
			gotSystem = req.Messages[0].Content
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<p>Done</p>"}}]}`))
	}))
	t.Cleanup(srv.Close)

	b, _ := newTestBridge(t, true)
	b.caller = newTestCaller(map[string]string{"openai": srv.URL})

	in := validInput()
	in.APIKey = "sk-testkey"
	in.ActionPrompts = map[string]string{"shorten": "Shorten ruthlessly, keep every fact."}
	if err := b.AIWriting_SaveSettings(in); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	rows, err := b.AIWriting_GetActionPrompts()
	if err != nil {
		t.Fatalf("GetActionPrompts: %v", err)
	}
	if len(rows) != 7 {
		t.Fatalf("GetActionPrompts rows = %d, want 7", len(rows))
	}
	byID := map[string]ActionPromptInfo{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if byID["shorten"].CustomPrompt != "Shorten ruthlessly, keep every fact." {
		t.Errorf("shorten custom = %q", byID["shorten"].CustomPrompt)
	}
	if byID["shorten"].DefaultPrompt != DefaultPromptShorten {
		t.Errorf("shorten default mismatch")
	}
	if byID["improve"].CustomPrompt != "" {
		t.Errorf("improve custom = %q, want empty (uses default)", byID["improve"].CustomPrompt)
	}

	// Shorten uses the custom prompt.
	if _, err := b.AIWriting_Improve(ImproveRequest{ActionID: "shorten", DraftHTML: "<p>Draft</p>"}); err != nil {
		t.Fatalf("Improve shorten: %v", err)
	}
	if !strings.Contains(gotSystem, "Shorten ruthlessly, keep every fact.") {
		t.Errorf("shorten system prompt = %q, want the custom text", gotSystem)
	}
	if strings.Contains(gotSystem, "You are shortening an email draft") {
		t.Errorf("shorten system prompt must not contain the default text")
	}

	// Improve still uses its registry default.
	if _, err := b.AIWriting_Improve(ImproveRequest{ActionID: "improve", DraftHTML: "<p>Draft</p>"}); err != nil {
		t.Fatalf("Improve improve: %v", err)
	}
	if !strings.Contains(gotSystem, "You are revising an email draft") {
		t.Errorf("improve system prompt = %q, want the default text", gotSystem)
	}
}

func TestBridge_ActionPromptsLegacyMigration(t *testing.T) {
	// A blob saved by v0.4.0 (PromptImprove only, no map) still yields
	// the customized improve prompt through GetActionPrompts.
	b, _ := newTestBridge(t, true)
	if _, err := b.AIWriting_GetSettings(); err != nil {
		t.Fatalf("GetSettings (init): %v", err)
	}
	raw := `{"selectedModel":"openai:gpt-5","promptImprove":"My legacy improve instructions."}`
	if err := b.store.KV().Set(settingsKVKey, raw); err != nil {
		t.Fatalf("write legacy blob: %v", err)
	}
	rows, err := b.AIWriting_GetActionPrompts()
	if err != nil {
		t.Fatalf("GetActionPrompts: %v", err)
	}
	for _, r := range rows {
		if r.ID == "improve" && r.CustomPrompt != "My legacy improve instructions." {
			t.Errorf("legacy improve custom = %q", r.CustomPrompt)
		}
	}
}

func TestBridge_ImproveErrors(t *testing.T) {
	b, _ := newTestBridge(t, true)

	// Empty draft, before any provider call.
	if _, err := b.AIWriting_Improve(ImproveRequest{DraftHTML: "  "}); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty draft: err=%v", err)
	}
	// Empty draft is still rejected for every revision action, even
	// with quoted context present.
	if _, err := b.AIWriting_Improve(ImproveRequest{ActionID: "shorten", DraftHTML: "", QuotedHTML: "<p>Quoted</p>"}); err == nil || !strings.Contains(err.Error(), "draft is empty") {
		t.Errorf("empty draft for shorten: err=%v", err)
	}
	// Empty draft is rejected for generate-reply with no quoted
	// context: there is nothing to reply to.
	if _, err := b.AIWriting_Improve(ImproveRequest{ActionID: "generate-reply", DraftHTML: "", QuotedHTML: "  "}); err == nil || !strings.Contains(err.Error(), "nothing to reply to") {
		t.Errorf("empty draft + no context for generate-reply: err=%v", err)
	}
	// Unknown action ("shorten" is a registered action since Nov 13).
	if _, err := b.AIWriting_Improve(ImproveRequest{ActionID: "not-a-real-action", DraftHTML: "<p>x</p>"}); err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("unknown action: err=%v", err)
	}
	// No key stored: the provider error must contain "API key".
	if _, err := b.AIWriting_Improve(ImproveRequest{DraftHTML: "<p>x</p>"}); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("missing key: err=%v", err)
	}
}

func TestBridge_GenerateReplyEmptyDraft(t *testing.T) {
	// Fake OpenAI server: capture the request, return a plain reply.
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<p>Thanks, will do.</p>"}}]}`))
	}))
	t.Cleanup(srv.Close)

	b, _ := newTestBridge(t, true)
	b.caller = newTestCaller(map[string]string{"openai": srv.URL})
	in := validInput()
	if err := b.AIWriting_SaveSettings(in); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	// Empty draft + quoted context is allowed for generate-reply.
	res, err := b.AIWriting_Improve(ImproveRequest{
		ActionID:   "generate-reply",
		DraftHTML:  "",
		QuotedHTML: "<p>Can you send the report by Friday?</p>",
		IsNewEmail: false,
	})
	if err != nil {
		t.Fatalf("GenerateReply with empty draft: %v", err)
	}
	if res.ImprovedHTML != "<p>Thanks, will do.</p>" {
		t.Errorf("reply = %q", res.ImprovedHTML)
	}
	if len(res.Subjects) != 0 {
		t.Errorf("generate-reply must have no subjects: %v", res.Subjects)
	}

	var req openAIChatRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if !strings.Contains(req.Messages[0].Content, "write a reply to the conversation") {
		t.Errorf("system prompt missing generate-reply text: %q", req.Messages[0].Content)
	}
	// No subject instruction appended for a reply.
	if strings.Contains(req.Messages[0].Content, "Also generate 3 professional") {
		t.Errorf("generate-reply system prompt must not carry the subject instruction")
	}
	// Envelope: empty DRAFT block + CONTEXT block with the history.
	if !strings.Contains(req.Messages[1].Content, "<!-- BEGIN DRAFT -->\n\n<!-- END DRAFT -->") {
		t.Errorf("user text must carry an empty DRAFT block: %q", req.Messages[1].Content)
	}
	if !strings.Contains(req.Messages[1].Content, "<!-- BEGIN CONTEXT -->\n<p>Can you send the report by Friday?</p>\n<!-- END CONTEXT -->") {
		t.Errorf("user text missing CONTEXT block: %q", req.Messages[1].Content)
	}
}

func TestBridge_GenerateReplyHistoryDisabled(t *testing.T) {
	// Fake OpenAI server: fail the test if it is ever hit — the
	// history-off guard must reject before any provider call.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("provider must not be called for generate-reply when conversation history is off")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<p>must not reach here</p>"}}]}`))
	}))
	t.Cleanup(srv.Close)

	b, _ := newTestBridge(t, true)
	b.caller = newTestCaller(map[string]string{"openai": srv.URL})
	in := validInput()
	in.UseConversationHistory = false
	if err := b.AIWriting_SaveSettings(in); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	// Empty draft + quoted context is a valid generate-reply with
	// history on; with history off the CONTEXT block would be omitted
	// and the model would get an empty draft and no context at all,
	// so it must be rejected with the fix named in the error.
	_, err := b.AIWriting_Improve(ImproveRequest{
		ActionID:   "generate-reply",
		DraftHTML:  "",
		QuotedHTML: "<p>Can you send the report by Friday?</p>",
		IsNewEmail: false,
	})
	if err == nil || !strings.Contains(err.Error(), "conversation history is off") {
		t.Errorf("generate-reply with history off: err=%v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "turn it on in AI Writing Assistant settings") {
		t.Errorf("generate-reply history-off error must name the fix: err=%v", err)
	}
}

func TestBridge_ImproveReplyNoSubjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<p>Reply improved</p>"}}]}`))
	}))
	t.Cleanup(srv.Close)

	b, _ := newTestBridge(t, true)
	b.caller = newTestCaller(map[string]string{"openai": srv.URL})
	in := validInput()
	if err := b.AIWriting_SaveSettings(in); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	res, err := b.AIWriting_Improve(ImproveRequest{DraftHTML: "<p>x</p>", IsNewEmail: false})
	if err != nil {
		t.Fatalf("Improve: %v", err)
	}
	if res.ImprovedHTML != "<p>Reply improved</p>" {
		t.Errorf("improved = %q", res.ImprovedHTML)
	}
	if len(res.Subjects) != 0 {
		t.Errorf("reply must have no subjects: %v", res.Subjects)
	}
}
