package backend

import (
	"strings"
	"testing"
)

func TestDefaultActions(t *testing.T) {
	// Exactly these seven actions, in this order (Nov 13 decision).
	wantIDs := []string{"improve", "fix-grammar", "make-formal", "shorten", "expand", "make-friendly", "generate-reply"}
	wantLabels := []string{"Improve Writing", "Fix Grammar", "Make Formal", "Shorten", "Expand", "Make Friendly", "Generate Reply"}
	if len(defaultActions) != len(wantIDs) {
		t.Fatalf("len(defaultActions) = %d, want %d", len(defaultActions), len(wantIDs))
	}
	seen := map[string]bool{}
	for i, a := range defaultActions {
		if a.ID != wantIDs[i] {
			t.Errorf("action %d: ID = %q, want %q", i, a.ID, wantIDs[i])
		}
		if a.Label != wantLabels[i] {
			t.Errorf("action %d: Label = %q, want %q", i, a.Label, wantLabels[i])
		}
		if seen[a.ID] {
			t.Errorf("duplicate action ID %q", a.ID)
		}
		seen[a.ID] = true
		if a.SystemPrompt == "" {
			t.Errorf("action %q: empty SystemPrompt", a.ID)
		}
		// Every prompt names the spaced DRAFT markers (matching the
		// envelope in provider.go) and the never-include-context rule.
		if !strings.Contains(a.SystemPrompt, "<!-- BEGIN DRAFT -->") ||
			!strings.Contains(a.SystemPrompt, "<!-- END DRAFT -->") {
			t.Errorf("action %q: prompt must name the spaced DRAFT markers", a.ID)
		}
		if strings.Contains(a.SystemPrompt, "<!--BEGIN DRAFT-->") ||
			strings.Contains(a.SystemPrompt, "<!--END DRAFT-->") {
			t.Errorf("action %q: prompt must not use unspaced DRAFT markers", a.ID)
		}
		if !strings.Contains(a.SystemPrompt, "Never include the context in your output") {
			t.Errorf("action %q: prompt must contain the never-include-context rule", a.ID)
		}
	}
}

func TestGenerateReplyPrompt(t *testing.T) {
	p := DefaultPromptGenerateReply
	// The empty DRAFT section is the signal to write a reply, not to
	// revise a draft; the reply is written to the conversation in
	// CONTEXT, in the conversation's language, and the context itself
	// never appears in the output.
	for _, want := range []string{
		"<!-- BEGIN DRAFT -->",
		"<!-- END DRAFT -->",
		"<!-- BEGIN CONTEXT -->",
		"<!-- END CONTEXT -->",
		"empty",
		"reply",
		"same language as the conversation",
		"Do not invent facts",
		"Never include the context in your output",
		"output only the new reply",
		"No preamble, no explanation",
		"only fix the capitalization of the closing salutation",
		"check the day is correct for that date",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("generate-reply prompt missing %q", want)
		}
	}
	if strings.Contains(p, "<!--BEGIN DRAFT-->") || strings.Contains(p, "<!--BEGIN CONTEXT-->") {
		t.Errorf("generate-reply prompt must not use unspaced markers")
	}
	// Reply-only: the prompt itself must never ask for subject
	// suggestions (the "no subject line" in rule 4 is an output rule,
	// not a request for subjects).
	if strings.Contains(p, "<subject>") || strings.Contains(p, "generate 3") {
		t.Errorf("generate-reply prompt must not ask for subject suggestions")
	}
}

func TestListActions_Order(t *testing.T) {
	got := ListActions()
	if len(got) != 7 {
		t.Fatalf("len(ListActions()) = %d, want 7", len(got))
	}
	want := []string{"improve", "fix-grammar", "make-formal", "shorten", "expand", "make-friendly", "generate-reply"}
	for i, a := range got {
		if a.ID != want[i] {
			t.Errorf("ListActions()[%d].ID = %q, want %q", i, a.ID, want[i])
		}
	}
	for _, id := range want {
		if findAction(id) == nil {
			t.Errorf("findAction(%q) = nil", id)
		}
	}
	if findAction("not-a-real-action") != nil {
		t.Errorf("findAction for unknown ID must return nil")
	}
}
