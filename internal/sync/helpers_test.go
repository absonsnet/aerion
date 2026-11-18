package sync

import (
	"strings"
	"testing"
)

func TestGenerateSnippet(t *testing.T) {
	longText := "This is a long text that should be truncated when it exceeds the maximum length specified by the caller of the generateSnippet function."
	maxLen := 30

	result := generateSnippet(longText, maxLen)

	// The result should be truncated to maxLen + "..."
	if len(result) > maxLen+3 {
		t.Errorf("snippet length = %d, want at most %d (maxLen + '...')", len(result), maxLen+3)
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("expected truncated snippet to end with '...', got %q", result)
	}
}

func TestGenerateSnippet_Empty(t *testing.T) {
	result := generateSnippet("", 100)

	if result != "" {
		t.Errorf("generateSnippet('', 100) = %q, want empty string", result)
	}
}

func TestGenerateSnippet_QuotedLines(t *testing.T) {
	text := "Hello there\n> This is a quoted line\n> Another quoted line\nActual content here"
	result := generateSnippet(text, 200)

	if strings.Contains(result, "quoted line") {
		t.Errorf("snippet should skip quoted lines, got %q", result)
	}
	if !strings.Contains(result, "Hello there") {
		t.Errorf("snippet should contain non-quoted content 'Hello there', got %q", result)
	}
	if !strings.Contains(result, "Actual content here") {
		t.Errorf("snippet should contain non-quoted content 'Actual content here', got %q", result)
	}
}

func TestStripHTMLTags(t *testing.T) {
	result := stripHTMLTags("<b>bold</b> text")

	if result != "bold text" {
		t.Errorf("stripHTMLTags('<b>bold</b> text') = %q, want 'bold text'", result)
	}
}

func TestStripHTMLTags_Empty(t *testing.T) {
	result := stripHTMLTags("")

	if result != "" {
		t.Errorf("stripHTMLTags('') = %q, want empty string", result)
	}
}

func TestGenerateID_UniqueInTightLoop(t *testing.T) {
	// Regression test: generateID is called in tight loops with no I/O
	// between calls (e.g. decoding a winmail.dat's inner files).
	// Timestamp-based IDs could collide on a coarse clock, and a duplicate
	// fails the attachments.id PRIMARY KEY insert — silently dropping one
	// file from the list.
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := generateID()
		if id == "" {
			t.Fatalf("generateID() returned an empty ID at iteration %d", i)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("generateID() returned duplicate ID %q at iteration %d", id, i)
		}
		seen[id] = struct{}{}
	}
}
