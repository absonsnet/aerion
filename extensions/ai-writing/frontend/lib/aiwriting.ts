// Shared types, model list and helpers for the ai-writing extension
// frontend. The types mirror the backend contract exactly (Wails method
// signatures on App): AIWriting_Improve / AIWriting_ListActions /
// AIWriting_GetSettings / AIWriting_SaveSettings / AIWriting_GetStatus.
// They are declared locally (rather than imported from the generated
// wailsjs models) so this code type-checks before `make generate` lands
// the AIWriting_* bindings; field names must stay in sync with the
// contract — do not rename them here.

/** Result of AIWriting_Improve. `subjects` is the backend-parsed list of
 *  `<subject>` suggestions (new mail only); empty for replies/forwards. */
export interface AIWritingImproveResult {
  improvedHtml: string
  subjects: string[]
}

/** Settings as returned by AIWriting_GetSettings. NEVER contains the API
 *  key value — only `hasApiKey` says whether one is stored. */
export interface AIWritingSettings {
  selectedModel: string
  temperature: number
  maxTokens: number
  customApiEndpoint: string
  customModel: string
  promptImprove: string
  useConversationHistory: boolean
  hasApiKey: boolean
}

/** Input for AIWriting_SaveSettings: same fields as AIWritingSettings
 *  plus `apiKey`. An empty `apiKey` keeps the existing stored key. */
export interface AIWritingSaveSettingsInput {
  selectedModel: string
  apiKey: string
  temperature: number
  maxTokens: number
  customApiEndpoint: string
  customModel: string
  promptImprove: string
  useConversationHistory: boolean
}

export interface AIWritingStatus {
  enabled: boolean
  hasApiKey: boolean
  selectedModel: string
}

/**
 * One Improve click, snapshotted by AIWritingButton and handed to the
 * review dialog. `nonce` increments per click so the dialog's auto-run
 * effect can tell a fresh request from a re-render. `actionId` /
 * `actionLabel` are the registry action the user picked in the button's
 * menu (see AIWriting_ListActions) — the dialog runs Improve with that
 * action and titles itself with its label.
 */
export interface AIWritingImproveRequest {
  nonce: number
  actionId: string
  actionLabel: string
  draftHtml: string
  quotedHtml: string
  isNewEmail: boolean
  originalSubject: string
  isPlainText: boolean
}

/**
 * One entry of AIWriting_ListActions: the registry id plus the backend's
 * canonical (English) label. The system prompt is backend-internal and
 * never leaves the bridge. The frontend renders whatever the registry
 * returns — it never hardcodes action ids.
 */
export interface AIWritingAction {
  id: string
  label: string
}

/**
 * Registry id of the Generate Reply action (backend actions.go contract).
 * Only the id is named here — the action list itself is always fetched
 * from AIWriting_ListActions, and the label comes from the backend
 * (aiwriting.actions.generate-reply i18n override, else the backend
 * label). Used to pick the generate action out of the registry for the
 * empty-draft menu and the runAction guards.
 */
export const GENERATE_REPLY_ACTION_ID = 'generate-reply'

// ---------------------------------------------------------------------------
// Model list — the Thunderbird options.html list verbatim (values are
// `prefix:model` IDs), plus the two xAI models. Treat as data: model names
// go stale fast, update this list (not the dialog) when they do.
// ---------------------------------------------------------------------------

export interface ModelOption {
  value: string
  label: string
}

export const MODEL_OPTIONS: ModelOption[] = [
  { value: 'openai:gpt-5', label: 'OpenAI: GPT-5' },
  { value: 'openai:gpt-5-mini', label: 'OpenAI: GPT-5 mini' },
  { value: 'openai:gpt-5-nano', label: 'OpenAI: GPT-5 nano' },
  { value: 'openai:gpt-4o', label: 'OpenAI: GPT-4o' },
  { value: 'openai:gpt-4o-mini', label: 'OpenAI: GPT-4o Mini' },
  { value: 'openai:o3-mini', label: 'OpenAI: O3 mini' },
  { value: 'groq:openai/gpt-oss-20b', label: 'Groq: OpenAI GPT-OSS 20B' },
  { value: 'groq:openai/gpt-oss-120b', label: 'Groq: OpenAI GPT-OSS 120B' },
  { value: 'groq:llama-3.3-70b-versatile', label: 'Groq: Llama 3.3 70B Versatile 128k' },
  { value: 'google:gemini-3.8-flash', label: 'Google: Gemini 3.8 Flash' },
  { value: 'google:gemini-3.7-flash', label: 'Google: Gemini 3.7 Flash' },
  { value: 'google:gemini-3.6-flash', label: 'Google: Gemini 3.6 Flash' },
  { value: 'google:gemini-3.5-flash-lite', label: 'Google: Gemini 3.5 Flash Lite' },
  { value: 'xai:grok-4.7', label: 'xAI: Grok 4.7' },
  { value: 'xai:grok-4', label: 'xAI: Grok 4' },
  { value: 'custom:', label: 'custom: Ollama API & Models' },
]

export const CUSTOM_MODEL_VALUE = 'custom:'

export const DEFAULT_MODEL = 'openai:gpt-5'
export const DEFAULT_TEMPERATURE = 0.7
export const DEFAULT_MAX_TOKENS = 2000
export const DEFAULT_CUSTOM_API_ENDPOINT = 'http://localhost:11434/api/chat'

// Default Improve prompt — the rewritten draft from AI_WRITING_PLAN.md §9
// (Abid's decision: rewrite the prompt, not the Thunderbird default).
// The backend owns the same default in prompts.go; this copy is only the
// settings dialog's fallback when GetSettings returns an empty prompt.
export const DEFAULT_PROMPT_IMPROVE = `You are revising an email draft. You will receive the draft as HTML, plus quoted conversation history for context.

Revise ONLY the section between <!-- BEGIN DRAFT --> and <!-- END DRAFT -->.
Use the section between <!-- BEGIN CONTEXT --> and <!-- END CONTEXT --> only to understand the conversation. Never include the context in your output.

Rules:
1. Keep the writer's intent, tone, and all facts exactly as written. Do not add new claims, promises, dates, or numbers that are not in the draft.
2. Write in the same language as the draft. Be clear, polite, and concise. Cut filler, not meaning.
3. Keep every point from the draft, including all bullet points, in full. Do not drop or merge distinct points.
4. Return only the revised email as HTML. No preamble, no explanation, no subject line, no metadata.
5. Keep the HTML structure of the draft (paragraphs, lists, links). Keep all links and their URLs unchanged. Keep placeholders, names, and quoted text exactly as written.
6. Signature: only fix the capitalization of the closing salutation (e.g. Best regards) and the name after it. Change nothing else in the signature.
7. Dates: if the draft states a day of the week together with a date, check the day is correct for that date and silently correct it if wrong.
8. If the draft is already well written, return it with only minimal fixes. Do not rewrite for the sake of rewriting.`

// ---------------------------------------------------------------------------
// Subject suggestions
// ---------------------------------------------------------------------------

const SUBJECT_TAG_RE = /<subject>(.*?)<\/subject>/gi

/**
 * Normalize the `subjects` field of an Improve result. The contract says
 * the backend returns an already-parsed string array, but tolerate the
 * Thunderbird shape too (subjects embedded as `<subject>…</subject>` tags
 * in the improved HTML) so a backend that forwards tags through still
 * renders chips instead of leaking tags into the draft.
 * Returns the subject list and the improved HTML with any subject tags
 * stripped.
 */
export function normalizeImproveResult(result: {
  improvedHtml: string
  subjects?: string[] | string | null
}): { improvedHtml: string; subjects: string[] } {
  const subjects = result.subjects
  if (Array.isArray(subjects)) {
    return { improvedHtml: result.improvedHtml, subjects: subjects.map((s) => s.trim()).filter(Boolean) }
  }
  if (typeof subjects === 'string' && subjects) {
    return { improvedHtml: result.improvedHtml, subjects: parseSubjectTags(subjects) }
  }
  // Fall back to tags embedded in the improved HTML itself (TB shape).
  const found = parseSubjectTags(result.improvedHtml)
  if (found.length > 0) {
    return { improvedHtml: stripSubjectTags(result.improvedHtml).trim(), subjects: found }
  }
  return { improvedHtml: result.improvedHtml, subjects: [] }
}

export function parseSubjectTags(html: string): string[] {
  const out: string[] = []
  let match: RegExpExecArray | null
  SUBJECT_TAG_RE.lastIndex = 0
  while ((match = SUBJECT_TAG_RE.exec(html)) !== null) {
    const subject = match[1].trim()
    if (subject) out.push(subject)
  }
  return out
}

export function stripSubjectTags(html: string): string {
  SUBJECT_TAG_RE.lastIndex = 0
  return html.replace(SUBJECT_TAG_RE, '')
}

/** The backend error contract: a missing API key surfaces as an error
 *  whose message contains "API key". */
export function isApiKeyError(message: string): boolean {
  return message.includes('API key')
}
