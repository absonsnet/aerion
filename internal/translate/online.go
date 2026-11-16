package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Online translation (Tier 3) — the last tier, never silent.
//
// Both providers are consent-gated by the caller (the App layer checks
// translate_online_consent for the provider before calling
// TranslateOnline, and returns ErrConsentRequired without any network
// traffic when consent is missing). Online translation is also refused
// for security mail (OTP codes) — see IsSecurityMessage in the App
// layer — and never runs when translate_mode is offline_only.
//
// Providers:
//   - Google Translate, unofficial free endpoint (translate.googleapis.com,
//     client=gtx): no key, widest coverage including Urdu. Unofficial:
//     Google may rate-limit it; failures surface as an error with Retry.
//   - DeepL API (api-free.deepl.com for free keys ending in ":fx",
//     api.deepl.com otherwise): needs the user's own API key, stored in
//     the credentials store, never in settings.

// ErrConsentRequired is returned when online translation is asked for
// before the user consented to that provider. No request has been made.
var ErrConsentRequired = fmt.Errorf("online translation needs consent for this provider first")

// OnlineClient translates text through one online provider. Base URLs
// are fields so tests can point them at a local mock server; the
// defaults are the real provider endpoints.
type OnlineClient struct {
	HTTP *http.Client

	// GoogleBaseURL is the base of the unofficial Google endpoint.
	GoogleBaseURL string
	// DeepLBaseURL overrides DeepL's host ("" = pick api-free or api
	// from the key suffix). Tests set this to a mock server.
	DeepLBaseURL string
}

// NewOnlineClient returns a client with the real provider endpoints.
func NewOnlineClient(httpClient *http.Client) *OnlineClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OnlineClient{
		HTTP:          httpClient,
		GoogleBaseURL: "https://translate.googleapis.com",
	}
}

// Translate translates texts from source to target through provider
// ("google" or "deepl"). apiKey is the DeepL key (ignored for Google).
// Empty texts come back unchanged; the result has one entry per input,
// in order.
func (c *OnlineClient) Translate(ctx context.Context, provider, apiKey, source, target string, texts []string) ([]string, error) {
	out := make([]string, len(texts))
	// Translate only the non-blank texts; blanks pass through.
	var idx []int
	var work []string
	for i, t := range texts {
		out[i] = t
		if strings.TrimSpace(t) != "" {
			idx = append(idx, i)
			work = append(work, t)
		}
	}
	if len(work) == 0 {
		return out, nil
	}
	var translated []string
	var err error
	switch provider {
	case EngineGoogle:
		translated, err = c.translateGoogle(ctx, source, target, work)
	case EngineDeepL:
		translated, err = c.translateDeepL(ctx, apiKey, source, target, work)
	default:
		return nil, fmt.Errorf("unknown online translation provider: %s", provider)
	}
	if err != nil {
		return nil, err
	}
	if len(translated) != len(work) {
		return nil, fmt.Errorf("online translation returned %d texts for %d inputs", len(translated), len(work))
	}
	for k, i := range idx {
		out[i] = translated[k]
	}
	return out, nil
}

// translateGoogle uses the unofficial free endpoint. It takes one q
// parameter per text and returns one translated block per text, in
// order. The response is JSON: [[["translated","original",...]],...]
// per input, plus a detected-language tail we ignore.
func (c *OnlineClient) translateGoogle(ctx context.Context, source, target string, texts []string) ([]string, error) {
	base := strings.TrimSuffix(c.GoogleBaseURL, "/")
	u, err := url.Parse(base + "/translate_a/single")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("client", "gtx")
	q.Set("sl", NormalizeLangForGoogle(source))
	q.Set("tl", NormalizeLangForGoogle(target))
	q.Set("dt", "t")
	for _, t := range texts {
		q.Add("q", t)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Google Translate request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("Google Translate is rate-limiting this device right now; try again later")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google Translate returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read Google Translate response: %w", err)
	}
	// The response is a JSON array whose first element is one entry per
	// input text; each entry is a list of [translated, original, ...]
	// segments that must be joined.
	var parsed []json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse Google Translate response: %w", err)
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("empty Google Translate response")
	}
	var blocks [][][]interface{}
	if err := json.Unmarshal(parsed[0], &blocks); err != nil {
		return nil, fmt.Errorf("failed to parse Google Translate response: %w", err)
	}
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		var sb strings.Builder
		for _, seg := range block {
			if len(seg) > 0 {
				if s, ok := seg[0].(string); ok {
					sb.WriteString(s)
				}
			}
		}
		out = append(out, sb.String())
	}
	return out, nil
}

type deeplRequest struct {
	Text       []string `json:"text"`
	SourceLang string   `json:"source_lang,omitempty"`
	TargetLang string   `json:"target_lang"`
}

type deeplResponse struct {
	Translations []struct {
		DetectedSourceLanguage string `json:"detected_source_language"`
		Text                   string `json:"text"`
	} `json:"translations"`
}

// translateDeepL uses the DeepL API (v2). Free keys end in ":fx" and go
// to api-free.deepl.com; paid keys go to api.deepl.com.
func (c *OnlineClient) translateDeepL(ctx context.Context, apiKey, source, target string, texts []string) ([]string, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("DeepL needs an API key — add one in Settings → Translation")
	}
	base := c.DeepLBaseURL
	if base == "" {
		if strings.HasSuffix(apiKey, ":fx") {
			base = "https://api-free.deepl.com"
		} else {
			base = "https://api.deepl.com"
		}
	}
	payload, err := json.Marshal(deeplRequest{
		Text:       texts,
		SourceLang: strings.ToUpper(NormalizeLang(source)),
		TargetLang: strings.ToUpper(NormalizeLang(target)),
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/v2/translate", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "DeepL-Auth-Key "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DeepL request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read DeepL response: %w", err)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("DeepL rejected the API key (HTTP %d); check it in Settings → Translation", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("DeepL is rate-limiting this key right now; try again later")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DeepL returned HTTP %d", resp.StatusCode)
	}
	var parsed deeplResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse DeepL response: %w", err)
	}
	out := make([]string, 0, len(parsed.Translations))
	for _, t := range parsed.Translations {
		out = append(out, t.Text)
	}
	return out, nil
}
