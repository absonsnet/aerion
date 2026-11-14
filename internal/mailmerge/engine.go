// Package mailmerge implements Mail Merge for Aerion: one personalised
// message per recipient row, rendered from a single template.
//
// Attribution: Mail Merge is Alexander Bergmann's add-on for Thunderbird
// (GPLv3). The {{...}} variable syntax reimplemented here — plain
// substitution, inline conditionals, operators, nesting, empty test —
// comes from that add-on's public documentation, not from its code: no
// source of the add-on was read and none of its code was copied.
//
// This package is host-side code (internal/mailmerge). The extension
// bridge in extensions/mail-merge/backend receives the Service object
// constructed here by the host; the engine, sources and service never
// import the extension package.
//
// Escaping boundary (read this before changing rendering): the engine
// itself does NO HTML escaping — Render substitutes raw values, which is
// correct for headers, subject, plain-text bodies and attachment paths.
// Values substituted into the HTML body MUST be HTML-escaped, and that
// happens in exactly one place: RenderHTML, which escapes each substituted
// VALUE (never the template markup, never the comparison operands) before
// inserting it. A FirstName containing "<" therefore cannot break the
// markup, and the same value in the plain-text body stays literal.
package mailmerge

import (
	"fmt"
	"strconv"
	"strings"
)

// MissingError is returned by Render/RenderHTML when the template
// references a variable that has no matching column in the row. The
// caller must block the run on it — a missing variable is never rendered
// silently (no literal {{...}} may reach a recipient).
type MissingError struct {
	Names []string
}

func (e *MissingError) Error() string {
	return fmt.Sprintf("mailmerge: unresolved variable(s): %s", strings.Join(e.Names, ", "))
}

// Render substitutes the template's {{...}} placeholders with the row's
// values, raw (no escaping). See the package comment for the escaping
// boundary: use RenderHTML for the HTML body.
func Render(template string, row map[string]string) (string, error) {
	return render(template, row, false)
}

// RenderHTML is Render with HTML escaping applied to every substituted
// VALUE. Template markup, conditional literals and comparison operands
// are untouched. Use it for the HTML body only.
func RenderHTML(template string, row map[string]string) (string, error) {
	return render(template, row, true)
}

// Variables returns every variable name referenced by the template, in
// order of first appearance, deduplicated — including names inside
// nested placeholders and inside conditional branches. Callers use it
// for validation (see Validate): a referenced name with no matching
// column blocks the run.
func Variables(template string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, ph := range findPlaceholders(template) {
		collectNames(ph.content, add)
	}
	return out
}

// collectNames adds the variable names referenced by one placeholder's
// content (the text between {{ and }}), recursing into nested
// placeholders in every part.
func collectNames(content string, add func(string)) {
	parts := splitTopLevel(content, '|')
	if len(parts) == 0 {
		return
	}
	name := strings.TrimSpace(parts[0])
	if strings.Contains(name, "{{") {
		for _, ph := range findPlaceholders(name) {
			collectNames(ph.content, add)
		}
	} else {
		add(name)
	}
	for _, part := range parts[1:] {
		for _, ph := range findPlaceholders(part) {
			collectNames(ph.content, add)
		}
	}
}

// placeholder is one {{...}} occurrence with nesting resolved: content
// is the text between the outermost braces.
type placeholder struct {
	start, end int // byte offsets of "{{" and just past "}}"
	content    string
}

// findPlaceholders scans for {{...}} occurrences, matching braces by
// depth so nested placeholders ({{Premium|x|{{Email}}}}) yield one outer
// placeholder. An unclosed "{{" is reported as a placeholder with an
// error at render time (render returns the error rather than dropping
// text silently).
func findPlaceholders(s string) []placeholder {
	var out []placeholder
	for i := 0; i < len(s); {
		open := strings.Index(s[i:], "{{")
		if open < 0 {
			break
		}
		start := i + open
		depth := 1
		j := start + 2
		for j < len(s) && depth > 0 {
			if strings.HasPrefix(s[j:], "{{") {
				depth++
				j += 2
			} else if strings.HasPrefix(s[j:], "}}") {
				depth--
				j += 2
			} else {
				j++
			}
		}
		if depth > 0 {
			// Unclosed: keep the rest as a placeholder so render errors.
			out = append(out, placeholder{start: start, end: len(s), content: s[start+2:]})
			break
		}
		out = append(out, placeholder{start: start, end: j, content: s[start+2 : j-2]})
		i = j
	}
	return out
}

// splitTopLevel splits s on sep, but only separators at brace-depth 0
// (outside any nested {{...}}). A nested conditional's pipes must not
// split the outer placeholder.
func splitTopLevel(s string, sep byte) []string {
	var parts []string
	depth := 0
	last := 0
	for i := 0; i < len(s); i++ {
		if strings.HasPrefix(s[i:], "{{") {
			depth++
			i++
			continue
		}
		if strings.HasPrefix(s[i:], "}}") {
			depth--
			i++
			continue
		}
		if s[i] == sep && depth == 0 {
			parts = append(parts, s[last:i])
			last = i + 1
		}
	}
	return append(parts, s[last:])
}

// render is the shared implementation. escape=true HTML-escapes each
// substituted value (RenderHTML); comparisons always use raw values.
func render(template string, row map[string]string, escape bool) (string, error) {
	phs := findPlaceholders(template)
	if len(phs) == 0 {
		return template, nil
	}
	var b strings.Builder
	var missing []string
	seenMissing := map[string]bool{}
	recordMissing := func(name string) {
		if name != "" && !seenMissing[name] {
			seenMissing[name] = true
			missing = append(missing, name)
		}
	}
	pos := 0
	for _, ph := range phs {
		b.WriteString(template[pos:ph.start])
		// Unclosed placeholder: the content runs to end of string.
		if ph.end > len(template) || !strings.HasSuffix(template[ph.start:ph.end], "}}") {
			return "", fmt.Errorf("mailmerge: unclosed placeholder %q", "{{"+ph.content)
		}
		out, err := renderPlaceholder(ph.content, row, escape, recordMissing)
		if err != nil {
			return "", err
		}
		b.WriteString(out)
		pos = ph.end
	}
	b.WriteString(template[pos:])
	if len(missing) > 0 {
		return "", &MissingError{Names: missing}
	}
	return b.String(), nil
}

func isOperator(s string) bool {
	switch s {
	case "*", "^", "$", "==", ">", ">=", "<", "<=":
		return true
	}
	return false
}

// renderPlaceholder evaluates one placeholder's content.
//
//	Forms (Thunderbird Mail Merge, documented):
//	  {{name}}                    plain substitution
//	  {{name|if|then}}            string equality, no else
//	  {{name|if|then|else}}       string equality with else
//	  {{name|OP|if|then}}         operator, no else
//	  {{name|OP|if|then|else}}    operator with else
//
//	OP: * includes, ^ starts-with, $ ends-with, == > >= < <= numeric.
//	Empty if ("{{DisplayName||Empty|NotEmpty}}") tests field-is-empty.
//	then/else/if parts may themselves contain nested {{...}} variables,
//	which resolve first. A name with no matching column is recorded via
//	recordMissing and substitutes as "" (the caller blocks the run).
func renderPlaceholder(content string, row map[string]string, escape bool, recordMissing func(string)) (string, error) {
	parts := splitTopLevel(content, '|')
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", fmt.Errorf("mailmerge: empty variable name in %q", "{{"+content+"}}")
	}
	name := strings.TrimSpace(parts[0])
	if strings.Contains(name, "{{") {
		// A nested placeholder resolves to the variable name itself.
		resolved, err := render(name, row, false)
		if err != nil {
			if me, ok := err.(*MissingError); ok {
				for _, n := range me.Names {
					recordMissing(n)
				}
			} else {
				return "", err
			}
		}
		name = strings.TrimSpace(resolved)
		if name == "" {
			return "", fmt.Errorf("mailmerge: empty variable name in %q", "{{"+content+"}}")
		}
	}
	value, ok := row[name]
	if !ok {
		recordMissing(name)
		value = ""
	}

	// renderOperand resolves nested placeholders in an operand for
	// comparison, always raw (never escaped).
	renderOperand := func(s string) (string, error) {
		out, err := render(s, row, false)
		if err != nil {
			if me, ok := err.(*MissingError); ok {
				for _, n := range me.Names {
					recordMissing(n)
				}
				return "", nil
			}
			return "", err
		}
		return out, nil
	}
	// renderBranch renders a then/else branch, escaping substituted
	// values when the outer render is HTML.
	renderBranch := func(s string) (string, error) {
		out, err := render(s, row, escape)
		if err != nil {
			if me, ok := err.(*MissingError); ok {
				for _, n := range me.Names {
					recordMissing(n)
				}
				return "", nil
			}
			return "", err
		}
		return out, nil
	}
	maybeEscape := func(s string) string {
		if escape {
			return escapeHTMLValue(s)
		}
		return s
	}

	switch len(parts) {
	case 1:
		return maybeEscape(value), nil
	case 3:
		// {{name|if|then}} — string equality, no else.
		operand, err := renderOperand(parts[1])
		if err != nil {
			return "", err
		}
		if value == operand {
			return renderBranch(parts[2])
		}
		return "", nil
	case 4:
		if isOperator(parts[1]) {
			// {{name|OP|if|then}} — operator, no else.
			operand, err := renderOperand(parts[2])
			if err != nil {
				return "", err
			}
			if evalOperator(value, parts[1], operand) {
				return renderBranch(parts[3])
			}
			return "", nil
		}
		// {{name|if|then|else}} — string equality with else.
		operand, err := renderOperand(parts[1])
		if err != nil {
			return "", err
		}
		if value == operand {
			return renderBranch(parts[2])
		}
		return renderBranch(parts[3])
	case 5:
		// {{name|OP|if|then|else}}.
		if !isOperator(parts[1]) {
			return "", fmt.Errorf("mailmerge: unsupported operator %q in %q", parts[1], "{{"+content+"}}")
		}
		operand, err := renderOperand(parts[2])
		if err != nil {
			return "", err
		}
		if evalOperator(value, parts[1], operand) {
			return renderBranch(parts[3])
		}
		return renderBranch(parts[4])
	default:
		return "", fmt.Errorf("mailmerge: malformed placeholder %q", "{{"+content+"}}")
	}
}

// evalOperator applies a conditional operator to the field value.
// Numeric operators parse both sides as floats; an unparseable side
// makes the condition false (never an error).
func evalOperator(value, op, operand string) bool {
	switch op {
	case "*":
		return strings.Contains(value, operand)
	case "^":
		return strings.HasPrefix(value, operand)
	case "$":
		return strings.HasSuffix(value, operand)
	case "==", ">", ">=", "<", "<=":
		fv, err1 := strconv.ParseFloat(strings.TrimSpace(value), 64)
		fo, err2 := strconv.ParseFloat(strings.TrimSpace(operand), 64)
		if err1 != nil || err2 != nil {
			return false
		}
		switch op {
		case "==":
			return fv == fo
		case ">":
			return fv > fo
		case ">=":
			return fv >= fo
		case "<":
			return fv < fo
		case "<=":
			return fv <= fo
		}
	}
	return false
}

// escapeHTMLValue escapes a substituted value for insertion into HTML.
// Only the five characters that can break markup or inject elements.
func escapeHTMLValue(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return r.Replace(s)
}
