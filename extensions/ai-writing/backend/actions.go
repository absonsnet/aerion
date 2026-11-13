package backend

// Action is one named writing action the user can run on a draft. Actions
// are DATA, not code: each entry is an ID + label + the system prompt the
// provider receives. Adding a new action means adding one entry below plus
// its prompt — no new provider code, no new bridge method.
type Action struct {
	ID           string
	Label        string
	SystemPrompt string
}

// DefaultPromptImprove is the default "Improve Writing" system prompt.
// Copied verbatim from the plan (AI_WRITING_PLAN.md §9) — the rewritten
// prompt that replaces the Thunderbird default. The user edits it in
// Settings (Settings.PromptImprove); this constant is only the default
// applied when no saved value exists.
//
// NOTE (Nov 13 decision): the draft markers are SPACED everywhere in
// this app — "<!-- BEGIN DRAFT -->" / "<!-- END DRAFT -->" in both the
// system prompts below and the request envelope built in provider.go.
// (Thunderbird's envelope used unspaced markers; unified to spaced here.)
const DefaultPromptImprove = `You are revising an email draft. You will receive the draft as HTML, plus quoted conversation history for context.

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

// subjectInstruction is Thunderbird's exact subject-line instruction
// (background.js handleImprove), appended to the action's system prompt
// for new mail only. The model answers with each subject wrapped in
// <subject>...</subject> at the very beginning of its response; the
// bridge parses those tags out in sanitize.go.
const subjectInstruction = "\n\nAlso generate 3 professional, concise, and engaging subject line alternatives for this email following best practices. Provide each subject wrapped exactly in <subject>...</subject> tags at the very beginning of your response."

// DefaultPromptFixGrammar is the "Fix Grammar" system prompt. Same shape
// as DefaultPromptImprove (§9): draft-only revision between the spaced
// markers, context only for understanding. Every action's prompt is
// user-editable in Settings; this constant is the default.
const DefaultPromptFixGrammar = `You are fixing the grammar of an email draft. You will receive the draft as HTML, plus quoted conversation history for context.

Fix ONLY the grammar, spelling, and punctuation of the section between <!-- BEGIN DRAFT --> and <!-- END DRAFT -->. Do not change the wording, tone, or structure beyond what a grammar fix requires.
Use the section between <!-- BEGIN CONTEXT --> and <!-- END CONTEXT --> only to understand the conversation. Never include the context in your output.

Rules:
1. Keep the writer's intent, tone, and all facts exactly as written. Do not add new claims, promises, dates, or numbers that are not in the draft.
2. Write in the same language as the draft. Be clear, polite, and concise. Cut filler, not meaning.
3. Keep every point from the draft, including all bullet points, in full. Do not drop or merge distinct points.
4. Return only the revised email as HTML. No preamble, no explanation, no subject line, no metadata.
5. Keep the HTML structure of the draft (paragraphs, lists, links). Keep all links and their URLs unchanged. Keep placeholders, names, and quoted text exactly as written.
6. Signature: only fix the capitalization of the closing salutation (e.g. Best regards) and the name after it. Change nothing else in the signature.
7. Dates: if the draft states a day of the week together with a date, check the day is correct for that date and silently correct it if wrong.
8. If the draft is already grammatically correct, return it with only minimal fixes. Do not rewrite for the sake of rewriting.`

// DefaultPromptMakeFormal is the "Make Formal" system prompt. Same shape
// as DefaultPromptImprove (§9): draft-only revision between the spaced
// markers, context only for understanding. Every action's prompt is
// user-editable in Settings; this constant is the default.
const DefaultPromptMakeFormal = `You are rewriting an email draft in a formal, professional tone. You will receive the draft as HTML, plus quoted conversation history for context.

Rewrite ONLY the section between <!-- BEGIN DRAFT --> and <!-- END DRAFT --> in a formal, professional tone, suitable for a business or official email.
Use the section between <!-- BEGIN CONTEXT --> and <!-- END CONTEXT --> only to understand the conversation. Never include the context in your output.

Rules:
1. Keep the writer's intent and all facts exactly as written. Do not add new claims, promises, dates, or numbers that are not in the draft.
2. Write in the same language as the draft. Be clear, polite, and concise. Cut filler, not meaning.
3. Keep every point from the draft, including all bullet points, in full. Do not drop or merge distinct points.
4. Return only the revised email as HTML. No preamble, no explanation, no subject line, no metadata.
5. Keep the HTML structure of the draft (paragraphs, lists, links). Keep all links and their URLs unchanged. Keep placeholders, names, and quoted text exactly as written.
6. Signature: only fix the capitalization of the closing salutation (e.g. Best regards) and the name after it. Change nothing else in the signature.
7. Dates: if the draft states a day of the week together with a date, check the day is correct for that date and silently correct it if wrong.
8. If the draft is already formal, return it with only minimal fixes. Do not rewrite for the sake of rewriting.`

// DefaultPromptShorten is the "Shorten" system prompt. Same shape as
// DefaultPromptImprove (§9): draft-only revision between the spaced
// markers, context only for understanding. Every action's prompt is
// user-editable in Settings; this constant is the default.
const DefaultPromptShorten = `You are shortening an email draft. You will receive the draft as HTML, plus quoted conversation history for context.

Shorten ONLY the section between <!-- BEGIN DRAFT --> and <!-- END DRAFT -->. Cut filler, repetition, and wordiness while keeping every point from the draft.
Use the section between <!-- BEGIN CONTEXT --> and <!-- END CONTEXT --> only to understand the conversation. Never include the context in your output.

Rules:
1. Keep the writer's intent, tone, and all facts exactly as written. Do not add new claims, promises, dates, or numbers that are not in the draft.
2. Write in the same language as the draft. Be clear, polite, and concise. Cut filler, not meaning.
3. Keep every point from the draft, including all bullet points, in full. Do not drop or merge distinct points.
4. Return only the revised email as HTML. No preamble, no explanation, no subject line, no metadata.
5. Keep the HTML structure of the draft (paragraphs, lists, links). Keep all links and their URLs unchanged. Keep placeholders, names, and quoted text exactly as written.
6. Signature: only fix the capitalization of the closing salutation (e.g. Best regards) and the name after it. Change nothing else in the signature.
7. Dates: if the draft states a day of the week together with a date, check the day is correct for that date and silently correct it if wrong.
8. Shorten as much as reasonable without losing any point or fact. If the draft is already concise, return it with only minimal fixes.`

// DefaultPromptExpand is the "Expand" system prompt. Same shape as
// DefaultPromptImprove (§9): draft-only revision between the spaced
// markers, context only for understanding. Every action's prompt is
// user-editable in Settings; this constant is the default.
const DefaultPromptExpand = `You are expanding an email draft with more detail. You will receive the draft as HTML, plus quoted conversation history for context.

Expand ONLY the section between <!-- BEGIN DRAFT --> and <!-- END DRAFT -->. Add helpful detail and clarity to the points already in the draft, without inventing new facts.
Use the section between <!-- BEGIN CONTEXT --> and <!-- END CONTEXT --> only to understand the conversation. Never include the context in your output.

Rules:
1. Keep the writer's intent, tone, and all facts exactly as written. Do not add new claims, promises, dates, or numbers that are not in the draft or the context. Elaborate only on what is already there.
2. Write in the same language as the draft. Be clear, polite, and concise. Add detail, not filler.
3. Keep every point from the draft, including all bullet points, in full. Do not drop or merge distinct points.
4. Return only the revised email as HTML. No preamble, no explanation, no subject line, no metadata.
5. Keep the HTML structure of the draft (paragraphs, lists, links). Keep all links and their URLs unchanged. Keep placeholders, names, and quoted text exactly as written.
6. Signature: only fix the capitalization of the closing salutation (e.g. Best regards) and the name after it. Change nothing else in the signature.
7. Dates: if the draft states a day of the week together with a date, check the day is correct for that date and silently correct it if wrong.
8. If a point cannot be expanded without inventing facts, leave it as written. Do not pad for the sake of length.`

// DefaultPromptMakeFriendly is the "Make Friendly" system prompt. Same
// shape as DefaultPromptImprove (§9): draft-only revision between the
// spaced markers, context only for understanding. Every action's prompt is
// user-editable in Settings; this constant is the default.
const DefaultPromptMakeFriendly = `You are rewriting an email draft in a warm, friendly tone. You will receive the draft as HTML, plus quoted conversation history for context.

Rewrite ONLY the section between <!-- BEGIN DRAFT --> and <!-- END DRAFT --> in a warm, friendly tone, while staying professional and appropriate for email.
Use the section between <!-- BEGIN CONTEXT --> and <!-- END CONTEXT --> only to understand the conversation. Never include the context in your output.

Rules:
1. Keep the writer's intent and all facts exactly as written. Do not add new claims, promises, dates, or numbers that are not in the draft.
2. Write in the same language as the draft. Be clear, warm, and concise. Cut filler, not meaning.
3. Keep every point from the draft, including all bullet points, in full. Do not drop or merge distinct points.
4. Return only the revised email as HTML. No preamble, no explanation, no subject line, no metadata.
5. Keep the HTML structure of the draft (paragraphs, lists, links). Keep all links and their URLs unchanged. Keep placeholders, names, and quoted text exactly as written.
6. Signature: only fix the capitalization of the closing salutation (e.g. Best regards) and the name after it. Change nothing else in the signature.
7. Dates: if the draft states a day of the week together with a date, check the day is correct for that date and silently correct it if wrong.
8. If the draft is already friendly, return it with only minimal fixes. Do not rewrite for the sake of rewriting.`

// DefaultPromptGenerateReply is the "Generate Reply" system prompt. Same
// shape as DefaultPromptImprove (§9): spaced markers, context only for
// understanding, never include the context in the output. Unlike the
// revision actions, the DRAFT section is EMPTY for this action — that is
// the signal to write a new reply to the conversation in CONTEXT, not to
// revise a draft. Reply-only: no subject instruction is ever appended
// for this action (the bridge's IsNewEmail guard is false whenever
// quoted context exists, and generate-reply requires quoted context).
// Every action's prompt is user-editable in Settings; this constant is the
// default.
const DefaultPromptGenerateReply = `You are writing a reply to an email conversation. You will receive the quoted conversation history for context. The draft section will be empty — that is the signal: write a reply to the conversation, do not revise a draft.

Write a reply to the conversation in the section between <!-- BEGIN CONTEXT --> and <!-- END CONTEXT -->. The section between <!-- BEGIN DRAFT --> and <!-- END DRAFT --> is empty and must stay empty. Never include the context in your output: output only the new reply.

Rules:
1. Reply to the latest message in the conversation, addressing its points and questions directly.
2. Write in the same language as the conversation. Be clear, polite, and concise.
3. Do not add new claims, promises, dates, or numbers that are not in the context. Do not invent facts.
4. Return only the reply as HTML. No preamble, no explanation, no subject line, no metadata.
5. Keep the reply focused on the conversation. Keep placeholders, names, and quoted text exactly as written.
6. Signature: only fix the capitalization of the closing salutation (e.g. Best regards) and the name after it. Change nothing else in the signature.
7. Dates: if the reply states a day of the week together with a date, check the day is correct for that date and silently correct it if wrong.
8. If the conversation needs no reply beyond a brief acknowledgement, keep it brief. Do not pad for the sake of length.`

// defaultActions is the action registry. v1 ships seven actions, in this
// order: improve, fix-grammar, make-formal, shorten, expand,
// make-friendly, generate-reply. Every action's prompt is user-editable
// in Settings (Settings.ActionPrompts overrides); the constants below
// are the defaults used when an action has no override.
var defaultActions = []Action{
	{ID: "improve", Label: "Improve Writing", SystemPrompt: DefaultPromptImprove},
	{ID: "fix-grammar", Label: "Fix Grammar", SystemPrompt: DefaultPromptFixGrammar},
	{ID: "make-formal", Label: "Make Formal", SystemPrompt: DefaultPromptMakeFormal},
	{ID: "shorten", Label: "Shorten", SystemPrompt: DefaultPromptShorten},
	{ID: "expand", Label: "Expand", SystemPrompt: DefaultPromptExpand},
	{ID: "make-friendly", Label: "Make Friendly", SystemPrompt: DefaultPromptMakeFriendly},
	{ID: "generate-reply", Label: "Generate Reply", SystemPrompt: DefaultPromptGenerateReply},
}

// ActionPromptInfo is one row of AIWriting_GetActionPrompts: the
// registry id + label, the built-in default prompt, and the stored
// custom prompt ("" when the action uses its default). The settings
// dialog shows defaultPrompt with the custom override applied, and
// resets by clearing the override.
type ActionPromptInfo struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	DefaultPrompt string `json:"defaultPrompt"`
	CustomPrompt  string `json:"customPrompt"`
}

// ActionInfo is the Wails-visible shape of an Action (ID + label only —
// the system prompt is backend-internal and never leaves the bridge).
type ActionInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ListActions returns the registered actions as ID/label pairs for the
// frontend picker.
func ListActions() []ActionInfo {
	out := make([]ActionInfo, 0, len(defaultActions))
	for _, a := range defaultActions {
		out = append(out, ActionInfo{ID: a.ID, Label: a.Label})
	}
	return out
}

// findAction returns the registered action with the given ID, or nil when
// no action has that ID.
func findAction(id string) *Action {
	for i := range defaultActions {
		if defaultActions[i].ID == id {
			return &defaultActions[i]
		}
	}
	return nil
}
