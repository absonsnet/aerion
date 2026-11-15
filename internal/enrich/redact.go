package enrich

import (
	"fmt"
	"strings"
)

// SafeLogFields returns the log-safe fields for an enrichment result.
// The extracted value itself is NEVER included: one-time codes must never
// be written to sync logs or diagnostics. Log these fields and nothing
// else about the extraction.
func SafeLogFields(r Result) map[string]any {
	return map[string]any{
		"category":           r.Category,
		"subCategory":        r.SubCategory,
		"extractKind":        r.ExtractKind,
		"extractRule":        r.ExtractRule,
		"extractValueLen":    len(r.ExtractValue),
		"hasListUnsubscribe": r.ListUnsubscribe != "",
		"oneClick":           r.ListUnsubscribePost,
	}
}

// FormatSafeLog renders SafeLogFields as a single log line (handy for
// tests asserting a code never appears in logged output).
func FormatSafeLog(r Result) string {
	f := SafeLogFields(r)
	return fmt.Sprintf("enrichment category=%s sub_category=%s extract_kind=%s extract_rule=%s extract_value_len=%d has_list_unsubscribe=%v one_click=%v",
		f["category"], f["subCategory"], f["extractKind"], f["extractRule"],
		f["extractValueLen"], f["hasListUnsubscribe"], f["oneClick"])
}

// RedactValue replaces every occurrence of value in text with
// "[redacted]". No-op for an empty value. Use before logging any text
// that may contain an extracted code (e.g. a subject line in a sync log).
func RedactValue(text, value string) string {
	if value == "" {
		return text
	}
	return strings.ReplaceAll(text, value, "[redacted]")
}
