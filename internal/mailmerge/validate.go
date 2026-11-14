package mailmerge

import (
	"net/mail"
	"os"
	"path/filepath"
	"strings"
)

// InvalidEmail is one invalid address found during validation.
type InvalidEmail struct {
	Row     int    `json:"row"`
	Address string `json:"address"`
}

// MissingAttachment is one per-row attachment file that does not exist.
type MissingAttachment struct {
	Row  int    `json:"row"`
	Path string `json:"path"`
}

// ValidationResult is the outcome of Validate. An unresolved variable
// BLOCKS the run (the frontend must not allow Start); invalid-email,
// empty-To and missing-attachment rows are SKIPPED and logged, not
// run-blocking.
type ValidationResult struct {
	UnresolvedVariables    []string            `json:"unresolvedVariables"`
	InvalidEmails          []InvalidEmail      `json:"invalidEmails"`
	DuplicateEmails        int                 `json:"duplicateEmails"`
	EmptyToRows            []int               `json:"emptyToRows"`
	MissingAttachmentFiles []MissingAttachment `json:"missingAttachmentFiles"`
	// SigningError is set when signing was requested but the account
	// has no usable signing identity (no certificate/key configured,
	// or the private key is unavailable). Like an unresolved variable
	// it BLOCKS the run — the failure must surface before anything is
	// sent, not mid-run. Empty when signing was not requested or the
	// pre-flight trial sign succeeded.
	SigningError string `json:"signingError,omitempty"`
}

// Validate checks a template + rows before a run. attachmentDir is the
// folder per-row attachment filenames resolve against ("" = resolve
// relative names as given). Rows are checked as passed (no Start/Stop
// slicing — the caller validates the rows it will actually run).
func Validate(tmpl Template, rows []map[string]string, attachmentDir string) ValidationResult {
	var out ValidationResult

	// Unresolved variables: any name referenced by any template field
	// that is not a key in ANY row (columns are the union of row keys).
	referenced := templateVariables(tmpl)
	if len(referenced) > 0 {
		known := map[string]bool{}
		for _, row := range rows {
			for k := range row {
				known[k] = true
			}
		}
		for _, name := range referenced {
			if !known[name] {
				out.UnresolvedVariables = append(out.UnresolvedVariables, name)
			}
		}
	}

	seenTo := map[string]int{}
	for i, row := range rows {
		to, err := Render(tmpl.To, row)
		if err != nil {
			// Missing variable: already reported globally; skip the
			// per-row address checks for this row.
			continue
		}
		to = strings.TrimSpace(to)
		if to == "" {
			out.EmptyToRows = append(out.EmptyToRows, i)
			continue
		}
		if strings.Contains(to, "{{") {
			// A To that still contains {{ after rendering is never
			// sent; report it as an invalid address so it is skipped.
			out.InvalidEmails = append(out.InvalidEmails, InvalidEmail{Row: i, Address: to})
			continue
		}
		addrs, perr := parseAddressList(to)
		if perr != nil || len(addrs) == 0 {
			out.InvalidEmails = append(out.InvalidEmails, InvalidEmail{Row: i, Address: to})
		} else {
			for _, a := range addrs {
				if !validEmail(a.Address) {
					out.InvalidEmails = append(out.InvalidEmails, InvalidEmail{Row: i, Address: a.Address})
				}
			}
			key := strings.ToLower(strings.TrimSpace(addrs[0].Address))
			if _, dup := seenTo[key]; dup {
				out.DuplicateEmails++
			} else {
				seenTo[key] = i
			}
		}
		// Cc / Bcc / ReplyTo: invalid entries are reported (they are
		// dropped at run time, the row still goes out).
		for _, field := range []string{tmpl.Cc, tmpl.Bcc, tmpl.ReplyTo} {
			rendered, err := Render(field, row)
			if err != nil || strings.TrimSpace(rendered) == "" {
				continue
			}
			addrs, err := parseAddressList(rendered)
			if err != nil {
				out.InvalidEmails = append(out.InvalidEmails, InvalidEmail{Row: i, Address: rendered})
				continue
			}
			for _, a := range addrs {
				if !validEmail(a.Address) {
					out.InvalidEmails = append(out.InvalidEmails, InvalidEmail{Row: i, Address: a.Address})
				}
			}
		}
		// Per-row attachments: existence-checked, never run-blocking.
		if strings.TrimSpace(tmpl.AttachmentField) != "" {
			for _, p := range ResolveAttachmentPaths(tmpl.AttachmentField, row, attachmentDir) {
				if _, err := os.Stat(p); err != nil {
					out.MissingAttachmentFiles = append(out.MissingAttachmentFiles, MissingAttachment{Row: i, Path: p})
				}
			}
		}
	}
	return out
}

// templateVariables collects every variable referenced by any template
// field (headers, subject, bodies, attachment field), deduplicated.
func templateVariables(tmpl Template) []string {
	var out []string
	seen := map[string]bool{}
	for _, field := range []string{
		tmpl.From, tmpl.To, tmpl.Cc, tmpl.Bcc, tmpl.ReplyTo,
		tmpl.Subject, tmpl.BodyHTML, tmpl.BodyText, tmpl.AttachmentField,
	} {
		for _, name := range Variables(field) {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// RenderRow renders the template for one row (preview + run path).
// BodyHTML goes through RenderHTML (values escaped); every other field
// renders raw. Attachments are the resolved per-row paths.
func RenderRow(tmpl Template, row map[string]string, attachmentDir string, rowIndex int) (*RenderedMessage, error) {
	render := func(field string) (string, error) { return Render(field, row) }
	to, err := render(tmpl.To)
	if err != nil {
		return nil, err
	}
	cc, err := render(tmpl.Cc)
	if err != nil {
		return nil, err
	}
	bcc, err := render(tmpl.Bcc)
	if err != nil {
		return nil, err
	}
	subject, err := render(tmpl.Subject)
	if err != nil {
		return nil, err
	}
	bodyText, err := render(tmpl.BodyText)
	if err != nil {
		return nil, err
	}
	bodyHTML, err := RenderHTML(tmpl.BodyHTML, row)
	if err != nil {
		return nil, err
	}
	attachments := ResolveAttachmentPaths(tmpl.AttachmentField, row, attachmentDir)
	return &RenderedMessage{
		RowIndex:    rowIndex,
		To:          to,
		Cc:          cc,
		Bcc:         bcc,
		Subject:     subject,
		BodyHTML:    bodyHTML,
		BodyText:    bodyText,
		Attachments: attachments,
	}, nil
}

// ResolveAttachmentPaths renders the attachment-field template for a
// row and resolves each resulting line to a filesystem path: absolute
// paths are used as-is, relative names resolve against attachmentDir.
// Returns nil when the field is empty. A render error yields nil (the
// caller reports the missing variable separately).
func ResolveAttachmentPaths(fieldTemplate string, row map[string]string, attachmentDir string) []string {
	if strings.TrimSpace(fieldTemplate) == "" {
		return nil
	}
	rendered, err := Render(fieldTemplate, row)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(rendered, "\n") {
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		out = append(out, resolveAttachmentPath(p, attachmentDir))
	}
	return out
}

// resolveAttachmentPath resolves one per-row attachment name: absolute
// paths as-is, otherwise joined onto the picked folder (or left
// relative when no folder was picked).
func resolveAttachmentPath(p, attachmentDir string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if attachmentDir != "" {
		return filepath.Join(attachmentDir, p)
	}
	return p
}

// parseAddressList parses "Name <a@b.c>, d@e.f" into addresses. An
// empty string yields no addresses and no error.
func parseAddressList(s string) ([]mail.Address, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parsed, err := mail.ParseAddressList(s)
	if err != nil {
		return nil, err
	}
	out := make([]mail.Address, 0, len(parsed))
	for _, a := range parsed {
		if a != nil {
			out = append(out, *a)
		}
	}
	return out, nil
}

// validEmail is the run-time address check: net/mail parseable and
// containing "@" with a non-empty domain part. It is deliberately
// lenient — the SMTP server is the final arbiter.
func validEmail(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil {
		return false
	}
	at := strings.LastIndex(parsed.Address, "@")
	return at > 0 && at < len(parsed.Address)-1
}

// missingErrorNames extracts the variable names from a Render error, or
// nil when the error is not a MissingError.
func missingErrorNames(err error) []string {
	if me, ok := err.(*MissingError); ok {
		return me.Names
	}
	return nil
}
