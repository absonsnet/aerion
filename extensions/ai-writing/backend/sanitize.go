package backend

import (
	"regexp"
	"strings"
)

// SanitizeLLMResult ports Thunderbird's sanetizeLLMResult (background.js)
// exactly: it strips a single leading code fence from the model's reply.
// The JS uses String.replace with a string first argument, which removes
// only the FIRST occurrence of each pattern — the Go port uses
// strings.Replace with n=1, in the same order, so the behavior matches
// (a fence later in the body is left alone).
func SanitizeLLMResult(s string) string {
	s = strings.Replace(s, "```html", "", 1)
	s = strings.Replace(s, "```", "", 1)
	s = strings.Replace(s, "'''html", "", 1)
	s = strings.Replace(s, "'''", "", 1)
	s = strings.Replace(s, `"""html`, "", 1)
	s = strings.Replace(s, `"""`, "", 1)
	return s
}

// subjectRe matches one <subject>...</subject> tag, case-insensitively,
// non-greedy so adjacent tags parse separately. This is the contract the
// subject instruction in actions.go asks the model to follow.
var subjectRe = regexp.MustCompile(`(?is)<subject>(.*?)</subject>`)

// ParseSubjects extracts every <subject> tag's text from the model's
// reply and returns the subjects plus the reply with those tags removed.
// Subjects are trimmed; empty ones are dropped. The cleaned text is
// trimmed of surrounding whitespace (the tags sit at the very beginning
// of the response, so this removes the gap they leave behind).
func ParseSubjects(s string) (subjects []string, cleaned string) {
	for _, m := range subjectRe.FindAllStringSubmatch(s, -1) {
		if len(m) < 2 {
			continue
		}
		if v := strings.TrimSpace(m[1]); v != "" {
			subjects = append(subjects, v)
		}
	}
	cleaned = strings.TrimSpace(subjectRe.ReplaceAllString(s, ""))
	return subjects, cleaned
}
