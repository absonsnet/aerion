package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider routing ports Thunderbird's promptAI (background.js) to Go:
// one shared OpenAI-compatible code path for openai/groq/xai/custom, plus
// Gemini's generateContent shape. The backend calls the provider APIs
// directly — no CORS, the API key never reaches the webview, and the key
// never appears in a log line or an error message.
//
// SECURITY: the Gemini key travels in the request URL query string (the
// API's design, same as TB). Nothing in this file logs a request URL, a
// request body, or a key. Error messages carry only the HTTP status and
// the provider's own error text.

// Default endpoint URLs, one per provider prefix.
const (
	openAIEndpoint = "https://api.openai.com/v1/chat/completions"
	groqEndpoint   = "https://api.groq.com/openai/v1/chat/completions"
	xAIEndpoint    = "https://api.x.ai/v1/chat/completions"
	geminiBaseURL  = "https://generativelanguage.googleapis.com"
)

// openAICompatibleEndpoints maps each OpenAI-compatible provider prefix to
// its chat-completions URL.
var openAICompatibleEndpoints = map[string]string{
	"openai": openAIEndpoint,
	"groq":   groqEndpoint,
	"xai":    xAIEndpoint,
}

// providerCaller issues provider requests. The overrides map exists only
// for tests: it points a provider prefix at an httptest server instead of
// the real API. For openai/groq/xai the value replaces the whole endpoint
// URL; for google it replaces the generativelanguage.googleapis.com base;
// custom always uses Settings.CustomAPIEndpoint, so it needs no override.
type providerCaller struct {
	client    *http.Client
	overrides map[string]string
}

// defaultCaller is the production caller: 120s timeout, no retries in v1.
var defaultCaller = &providerCaller{
	client: &http.Client{Timeout: 120 * time.Second},
}

// newTestCaller builds a caller whose provider endpoints point at test
// servers. Tests only.
func newTestCaller(overrides map[string]string) *providerCaller {
	return &providerCaller{client: &http.Client{Timeout: 10 * time.Second}, overrides: overrides}
}

// trimWithEllipsis mirrors TB's trimStringWithEllipsis: strings longer
// than n are cut to n characters plus "...". Rune-based so multibyte
// text is never split mid-character (TB counts UTF-16 units; the 3000
// boundary is approximate either way).
func trimWithEllipsis(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// buildUserText builds the user message envelope: the draft between
// spaced <!-- BEGIN DRAFT --> markers and, when history is enabled and
// non-empty, the quoted history between <!-- BEGIN CONTEXT --> markers.
// History is trimmed to 3000 chars with an ellipsis. When useHistory is
// false, or the trimmed history is empty, the CONTEXT block is omitted
// entirely — no empty block is sent. (Nov 13 decision: this diverges
// from Thunderbird, which sent an empty context block when history was
// off. Markers are spaced everywhere in this app, prompt + envelope.)
func buildUserText(draft, history string, useHistory bool) string {
	draftPart := "<!-- BEGIN DRAFT -->\n" + draft + "\n<!-- END DRAFT -->"
	if !useHistory {
		return draftPart
	}
	trimmed := trimWithEllipsis(history, 3000)
	if strings.TrimSpace(trimmed) == "" {
		return draftPart
	}
	return draftPart + "\n<!-- BEGIN CONTEXT -->\n" + trimmed + "\n<!-- END CONTEXT -->"
}

// improve sends one completion request and returns the model's raw reply
// text (still fenced / still carrying <subject> tags — the caller
// sanitizes and parses). st supplies temperature, maxTokens, and the
// custom endpoint/model; apiKey is the stored key ("" when none is
// stored — non-custom providers then fail with an "API key" error the
// frontend keys off to open settings).
func (c *providerCaller) improve(ctx context.Context, modelID, systemPrompt, userText string, st Settings, apiKey string) (string, error) {
	provider, modelName := splitModelID(modelID)

	switch provider {
	case "google":
		if apiKey == "" {
			return "", fmt.Errorf("ai-writing: API key not configured for model %q — set it in AI Writing Assistant settings", modelID)
		}
		return c.callGemini(ctx, modelName, systemPrompt, userText, st, apiKey)
	case "custom":
		endpoint := strings.TrimSpace(st.CustomAPIEndpoint)
		if endpoint == "" {
			endpoint = DefaultCustomAPIEndpoint
		}
		return c.callOpenAICompatible(ctx, endpoint, st.CustomModel, systemPrompt, userText, st, "")
	case "openai", "groq", "xai":
		if apiKey == "" {
			return "", fmt.Errorf("ai-writing: API key not configured for model %q — set it in AI Writing Assistant settings", modelID)
		}
		endpoint := openAICompatibleEndpoints[provider]
		if ov, ok := c.overrides[provider]; ok {
			endpoint = ov
		}
		return c.callOpenAICompatible(ctx, endpoint, modelName, systemPrompt, userText, st, apiKey)
	default:
		return "", fmt.Errorf("ai-writing: unsupported model %q", modelID)
	}
}

// --- OpenAI-compatible path (OpenAI, Groq, xAI, custom/Ollama) -----------

type openAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	Temperature float64             `json:"temperature"`
	MaxTokens   int                 `json:"max_tokens"`
	Stream      bool                `json:"stream"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// ollamaChatResponse is the /api/chat shape: {"message": {"content": ...}}.
// (Ollama is OpenAI-compatible in request shape only — its response nests
// the text under message.content instead of choices[0].message.content.)
type ollamaChatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (c *providerCaller) callOpenAICompatible(ctx context.Context, endpoint, model, systemPrompt, userText string, st Settings, apiKey string) (string, error) {
	body, err := json.Marshal(openAIChatRequest{
		Model: model,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userText},
		},
		Temperature: st.Temperature,
		MaxTokens:   st.MaxTokens,
		Stream:      false,
	})
	if err != nil {
		return "", fmt.Errorf("ai-writing: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ai-writing: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		// Custom/Ollama passes "" here — no auth header, exactly as TB.
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	respBody, err := c.do(req)
	if err != nil {
		return "", err
	}

	// Response shape differs by provider: Ollama nests under
	// message.content, the rest under choices[0].message.content. The
	// custom route is Ollama-shaped; try that first when no key was sent,
	// fall back to the choices shape.
	if apiKey == "" {
		var ollama ollamaChatResponse
		if err := json.Unmarshal(respBody, &ollama); err == nil && ollama.Message.Content != "" {
			return ollama.Message.Content, nil
		}
	}
	var parsed openAIChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("ai-writing: parse provider response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("ai-writing: provider returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// --- Gemini path ------------------------------------------------------------

type geminiRequest struct {
	SystemInstruction geminiParts   `json:"systemInstruction"`
	Contents          []geminiEntry `json:"contents"`
	GenerationConfig  geminiConfig  `json:"generationConfig"`
}

type geminiParts struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiEntry struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func (c *providerCaller) callGemini(ctx context.Context, model, systemPrompt, userText string, st Settings, apiKey string) (string, error) {
	base := geminiBaseURL
	if ov, ok := c.overrides["google"]; ok {
		base = ov
	}
	// The key goes in the query string (Gemini's design, same as TB).
	// It is never logged: do() reports only status + provider error text.
	endpoint := strings.TrimSuffix(base, "/") + "/v1beta/models/" + model + ":generateContent?key=" + url.QueryEscape(apiKey)

	body, err := json.Marshal(geminiRequest{
		SystemInstruction: geminiParts{Parts: []geminiPart{{Text: systemPrompt}}},
		Contents:          []geminiEntry{{Role: "user", Parts: []geminiPart{{Text: userText}}}},
		GenerationConfig:  geminiConfig{Temperature: st.Temperature, MaxOutputTokens: st.MaxTokens},
	})
	if err != nil {
		return "", fmt.Errorf("ai-writing: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ai-writing: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	respBody, err := c.do(req)
	if err != nil {
		return "", err
	}
	var parsed geminiResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("ai-writing: parse provider response: %w", err)
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("ai-writing: provider returned no candidates")
	}
	return parsed.Candidates[0].Content.Parts[0].Text, nil
}

// --- shared HTTP ------------------------------------------------------------

// providerError is the common error envelope providers return:
// {"error": {"message": ..., "status": ...}} (OpenAI/Gemini) or
// {"error": "..."} (Ollama).
type providerError struct {
	Error json.RawMessage `json:"error"`
}

// do executes req, returning the response body on 2xx or a descriptive
// error otherwise. The error carries the HTTP status plus the provider's
// own error text when parseable — never the request URL (it may contain
// the Gemini key) and never the request body.
func (c *providerCaller) do(req *http.Request) ([]byte, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai-writing: API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Cap error-body reads; success bodies are small (a revised email),
	// and a runaway body must not balloon memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("ai-writing: read provider response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai-writing: API request failed: %s", providerErrorDetail(resp.StatusCode, resp.Status, body))
	}
	return body, nil
}

// providerErrorDetail extracts the human-readable detail from a provider
// error body, falling back to the HTTP status line.
func providerErrorDetail(statusCode int, status string, body []byte) string {
	var parsed providerError
	if err := json.Unmarshal(body, &parsed); err == nil && len(parsed.Error) > 0 {
		// Object form: {"message": "...", "status": "..."}.
		var obj struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		}
		if err := json.Unmarshal(parsed.Error, &obj); err == nil {
			if obj.Message != "" {
				return obj.Message
			}
			if obj.Status != "" {
				return obj.Status
			}
		}
		// String form: {"error": "..."}.
		var s string
		if err := json.Unmarshal(parsed.Error, &s); err == nil && s != "" {
			return s
		}
	}
	if status != "" {
		return fmt.Sprintf("HTTP %d %s", statusCode, strings.TrimPrefix(status, fmt.Sprintf("%d ", statusCode)))
	}
	return fmt.Sprintf("HTTP %d", statusCode)
}
