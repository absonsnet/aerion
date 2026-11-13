package backend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capturedRequest records what a fake provider server received.
type capturedRequest struct {
	method string
	path   string
	query  string
	auth   string
	body   []byte
}

func startCaptureServer(t *testing.T, status int, respBody string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	cap := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.query = r.URL.RawQuery
		cap.auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		cap.body = b
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

func openAIChatBody(t *testing.T, raw []byte) openAIChatRequest {
	t.Helper()
	var req openAIChatRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal openai request: %v", err)
	}
	return req
}

func testSettings() Settings {
	st := DefaultSettings()
	st.Temperature = 0.7
	st.MaxTokens = 2000
	return st
}

func TestProvider_OpenAI_RequestShape(t *testing.T) {
	srv, cap := startCaptureServer(t, 200, `{"choices":[{"message":{"content":"<p>Hi</p>"}}]}`)
	caller := newTestCaller(map[string]string{"openai": srv.URL})
	out, err := caller.improve(context.Background(), "openai:gpt-5", "sys prompt", "user text", testSettings(), "sk-testkey")
	if err != nil {
		t.Fatalf("improve: %v", err)
	}
	if out != "<p>Hi</p>" {
		t.Fatalf("got %q", out)
	}
	if cap.method != "POST" {
		t.Errorf("method = %q", cap.method)
	}
	if cap.auth != "Bearer sk-testkey" {
		t.Errorf("auth = %q", cap.auth)
	}
	req := openAIChatBody(t, cap.body)
	if req.Model != "gpt-5" {
		t.Errorf("model = %q", req.Model)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[0].Content != "sys prompt" {
		t.Errorf("system message = %+v", req.Messages)
	}
	if len(req.Messages) != 2 || req.Messages[1].Role != "user" || req.Messages[1].Content != "user text" {
		t.Errorf("user message = %+v", req.Messages)
	}
	if req.Temperature != 0.7 || req.MaxTokens != 2000 || req.Stream {
		t.Errorf("sampling/stream = %v/%v/%v", req.Temperature, req.MaxTokens, req.Stream)
	}
}

func TestProvider_Groq_EndpointAndBody(t *testing.T) {
	// Groq shares the OpenAI-compatible path; the only difference is the
	// endpoint host (overridden here to the test server) and the model id.
	srv, cap := startCaptureServer(t, 200, `{"choices":[{"message":{"content":"ok"}}]}`)
	caller := newTestCaller(map[string]string{"groq": srv.URL + "/openai/v1/chat/completions"})
	out, err := caller.improve(context.Background(), "groq:openai/gpt-oss-120b", "sys", "user", testSettings(), "gsk_testkey")
	if err != nil {
		t.Fatalf("improve: %v", err)
	}
	if out != "ok" {
		t.Fatalf("got %q", out)
	}
	if cap.path != "/openai/v1/chat/completions" {
		t.Errorf("path = %q", cap.path)
	}
	if cap.auth != "Bearer gsk_testkey" {
		t.Errorf("auth = %q", cap.auth)
	}
	if req := openAIChatBody(t, cap.body); req.Model != "openai/gpt-oss-120b" {
		t.Errorf("model = %q", req.Model)
	}
}

func TestProvider_XAI_Endpoint(t *testing.T) {
	srv, cap := startCaptureServer(t, 200, `{"choices":[{"message":{"content":"ok"}}]}`)
	caller := newTestCaller(map[string]string{"xai": srv.URL})
	if _, err := caller.improve(context.Background(), "xai:grok-4", "sys", "user", testSettings(), "xai-testkey"); err != nil {
		t.Fatalf("improve: %v", err)
	}
	if cap.auth != "Bearer xai-testkey" {
		t.Errorf("auth = %q", cap.auth)
	}
	if req := openAIChatBody(t, cap.body); req.Model != "grok-4" {
		t.Errorf("model = %q", req.Model)
	}
}

func TestProvider_Gemini_RequestShape(t *testing.T) {
	srv, cap := startCaptureServer(t, 200, `{"candidates":[{"content":{"parts":[{"text":"<p>Gemini</p>"}]}}]}`)
	caller := newTestCaller(map[string]string{"google": srv.URL})
	out, err := caller.improve(context.Background(), "google:gemini-3.8-flash", "sys prompt", "user text", testSettings(), "AIzaTestKey")
	if err != nil {
		t.Fatalf("improve: %v", err)
	}
	if out != "<p>Gemini</p>" {
		t.Fatalf("got %q", out)
	}
	if want := "/v1beta/models/gemini-3.8-flash:generateContent"; cap.path != want {
		t.Errorf("path = %q, want %q", cap.path, want)
	}
	if !strings.Contains(cap.query, "key=AIzaTestKey") {
		t.Errorf("query = %q, want key param", cap.query)
	}
	if cap.auth != "" {
		t.Errorf("gemini must not send Authorization, got %q", cap.auth)
	}
	var req geminiRequest
	if err := json.Unmarshal(cap.body, &req); err != nil {
		t.Fatalf("unmarshal gemini request: %v", err)
	}
	if len(req.SystemInstruction.Parts) != 1 || req.SystemInstruction.Parts[0].Text != "sys prompt" {
		t.Errorf("systemInstruction = %+v", req.SystemInstruction)
	}
	if len(req.Contents) != 1 || req.Contents[0].Role != "user" || len(req.Contents[0].Parts) != 1 || req.Contents[0].Parts[0].Text != "user text" {
		t.Errorf("contents = %+v", req.Contents)
	}
	if req.GenerationConfig.Temperature != 0.7 || req.GenerationConfig.MaxOutputTokens != 2000 {
		t.Errorf("generationConfig = %+v", req.GenerationConfig)
	}
}

func TestProvider_Custom_OllamaShape(t *testing.T) {
	srv, cap := startCaptureServer(t, 200, `{"model":"llama3","message":{"role":"assistant","content":"<p>Ollama</p>"},"done":true}`)
	st := testSettings()
	st.CustomAPIEndpoint = srv.URL
	st.CustomModel = "llama3"
	// No overrides needed: custom uses Settings.CustomAPIEndpoint.
	out, err := newTestCaller(nil).improve(context.Background(), "custom:", "sys", "user", st, "")
	if err != nil {
		t.Fatalf("improve: %v", err)
	}
	if out != "<p>Ollama</p>" {
		t.Fatalf("got %q", out)
	}
	if cap.auth != "" {
		t.Errorf("ollama must not send Authorization, got %q", cap.auth)
	}
	if req := openAIChatBody(t, cap.body); req.Model != "llama3" {
		t.Errorf("model = %q, want custom model from settings", req.Model)
	}
}

func TestProvider_MissingKeyError(t *testing.T) {
	for _, model := range []string{"openai:gpt-5", "groq:openai/gpt-oss-120b", "google:gemini-3.8-flash", "xai:grok-4"} {
		_, err := defaultCaller.improve(context.Background(), model, "sys", "user", testSettings(), "")
		if err == nil {
			t.Errorf("model %s: expected error for missing key", model)
			continue
		}
		if !strings.Contains(err.Error(), "API key") {
			t.Errorf("model %s: error %q must contain %q", model, err, "API key")
		}
	}
	// Custom/Ollama needs no key: a missing key must not produce an
	// "API key" error (it fails later at the network step instead).
	_, err := defaultCaller.improve(context.Background(), "custom:", "sys", "user", testSettings(), "")
	if err != nil && strings.Contains(err.Error(), "API key") {
		t.Errorf("custom: unexpected API key error: %v", err)
	}
}

func TestProvider_UnsupportedModel(t *testing.T) {
	_, err := defaultCaller.improve(context.Background(), "anthropic:claude", "sys", "user", testSettings(), "key")
	if err == nil || !strings.Contains(err.Error(), "unsupported model") {
		t.Errorf("err = %v, want unsupported model", err)
	}
}

func TestProvider_Non2xxErrorDetail(t *testing.T) {
	srv, _ := startCaptureServer(t, 401, `{"error":{"message":"Incorrect API key provided","status":"invalid"}}`)
	caller := newTestCaller(map[string]string{"openai": srv.URL})
	_, err := caller.improve(context.Background(), "openai:gpt-5", "sys", "user", testSettings(), "sk-bad")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Incorrect API key provided") {
		t.Errorf("err = %v, want provider error text", err)
	}
	if strings.Contains(err.Error(), "sk-bad") {
		t.Errorf("err = %v, must not leak the key", err)
	}
}

func TestProvider_Non2xxStringError(t *testing.T) {
	srv, _ := startCaptureServer(t, 500, `{"error":"boom"}`)
	caller := newTestCaller(map[string]string{"openai": srv.URL})
	_, err := caller.improve(context.Background(), "openai:gpt-5", "sys", "user", testSettings(), "sk-x")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want provider error text", err)
	}
}

func TestProvider_EmptyChoicesError(t *testing.T) {
	srv, _ := startCaptureServer(t, 200, `{"choices":[]}`)
	caller := newTestCaller(map[string]string{"openai": srv.URL})
	_, err := caller.improve(context.Background(), "openai:gpt-5", "sys", "user", testSettings(), "sk-x")
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Errorf("err = %v, want no-choices error", err)
	}
}

func TestBuildUserText(t *testing.T) {
	got := buildUserText("<p>draft</p>", "<p>history</p>", true)
	want := "<!-- BEGIN DRAFT -->\n<p>draft</p>\n<!-- END DRAFT -->\n<!-- BEGIN CONTEXT -->\n<p>history</p>\n<!-- END CONTEXT -->"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	// History off: the CONTEXT block is omitted entirely (Nov 13
	// decision — no empty block is sent).
	got = buildUserText("<p>draft</p>", "<p>history</p>", false)
	want = "<!-- BEGIN DRAFT -->\n<p>draft</p>\n<!-- END DRAFT -->"
	if got != want {
		t.Errorf("history-off got %q\nwant %q", got, want)
	}
	if strings.Contains(got, "BEGIN CONTEXT") {
		t.Errorf("history-off output must not contain a CONTEXT block: %q", got)
	}

	// History on but empty: the CONTEXT block is omitted too.
	got = buildUserText("<p>draft</p>", "", true)
	want = "<!-- BEGIN DRAFT -->\n<p>draft</p>\n<!-- END DRAFT -->"
	if got != want {
		t.Errorf("empty-history got %q\nwant %q", got, want)
	}
	if strings.Contains(got, "BEGIN CONTEXT") {
		t.Errorf("empty-history output must not contain a CONTEXT block: %q", got)
	}

	// Long history is trimmed to 3000 chars with an ellipsis.
	long := strings.Repeat("x", 3005)
	got = buildUserText("d", long, true)
	if !strings.Contains(got, strings.Repeat("x", 3000)+"...") {
		t.Errorf("long history not trimmed with ellipsis")
	}
	if strings.Contains(got, strings.Repeat("x", 3001)) {
		t.Errorf("long history not trimmed at 3000")
	}
	if !strings.Contains(got, "<!-- BEGIN DRAFT -->") || !strings.Contains(got, "<!-- END DRAFT -->") {
		t.Errorf("envelope must use spaced DRAFT markers: %q", got)
	}
	if strings.Contains(got, "<!--BEGIN DRAFT-->") || strings.Contains(got, "<!--END DRAFT-->") {
		t.Errorf("envelope must not use unspaced DRAFT markers: %q", got)
	}
}

func TestTrimWithEllipsis(t *testing.T) {
	if got := trimWithEllipsis("abc", 3000); got != "abc" {
		t.Errorf("short: got %q", got)
	}
	if got := trimWithEllipsis("abcdef", 3); got != "abc..." {
		t.Errorf("long: got %q", got)
	}
}
