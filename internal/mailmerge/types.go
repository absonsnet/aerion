package mailmerge

import "time"

// Template is the merge template: the live composer's draft fields plus
// the merge-specific attachment field. JSON tags are the wire shape
// shared with the frontend (see extensions/mail-merge/backend/bridge.go).
type Template struct {
	AccountID string `json:"accountId"`
	From      string `json:"from"`
	To        string `json:"to"`
	Cc        string `json:"cc"`
	Bcc       string `json:"bcc"`
	ReplyTo   string `json:"replyTo"`
	Subject   string `json:"subject"`
	BodyHTML  string `json:"bodyHtml"`
	BodyText  string `json:"bodyText"`
	// IsPlainText renders only the text body (no HTML part).
	IsPlainText bool `json:"isPlainText"`
	// GlobalAttachments are the composer's normal attachments: the same
	// file for every recipient, sent as base64 by the frontend.
	GlobalAttachments []GlobalAttachment `json:"globalAttachments"`
	// AttachmentField is the template string for the per-row attachment
	// path, e.g. "{{Attachment}}". One file per line after rendering.
	AttachmentField string `json:"attachmentField"`
}

// GlobalAttachment is one composer attachment as sent by the frontend.
type GlobalAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	DataBase64  string `json:"dataBase64"`
}

// RunRequest is everything a run needs. Rows are the recipient table;
// StartRow/StopRow (1-based inclusive, 0 = first/last) select a slice
// of them — the Thunderbird Start/Stop batch controls and the resume
// mechanism.
type RunRequest struct {
	Template      Template            `json:"template"`
	Rows          []map[string]string `json:"rows"`
	SourceKind    string              `json:"sourceKind"`  // "csv" | "json" | "contacts"
	DeliverMode   string              `json:"deliverMode"` // "drafts" | "send"
	PauseSeconds  int                 `json:"pauseSeconds"`
	RandomPause   bool                `json:"randomPause"` // pause is random 0–PauseSeconds
	StartRow      int                 `json:"startRow"`
	StopRow       int                 `json:"stopRow"`
	CapEnabled    bool                `json:"capEnabled"`
	CapLimit      int                 `json:"capLimit"`
	AccountID     string              `json:"accountId"`
	AttachmentDir string              `json:"attachmentDir"`
}

// RowStatus values for one row's outcome.
const (
	RowSent       = "sent"
	RowDraftSaved = "draft-saved"
	RowFailed     = "failed"
	RowSkipped    = "skipped"
	RowHeld       = "held"
)

// RowResult is one row's outcome in a run.
type RowResult struct {
	// Index is the row's position in the request's Rows slice (0-based).
	Index     int       `json:"index"`
	Email     string    `json:"email"`  // first To address, "" when unknown
	Status    string    `json:"status"` // sent | draft-saved | failed | skipped | held
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// RunStatus values for a whole run.
const (
	RunRunning   = "running"
	RunDone      = "done"
	RunCancelled = "cancelled"
	RunHeld      = "held"
)

// Run is a persisted merge run with its per-row log.
type Run struct {
	ID           string      `json:"id"`
	CreatedAt    time.Time   `json:"createdAt"`
	AccountID    string      `json:"accountId"`
	DeliverMode  string      `json:"deliverMode"`
	Status       string      `json:"status"` // running | done | cancelled | held
	TotalRows    int         `json:"totalRows"`
	SentCount    int         `json:"sentCount"`
	FailedCount  int         `json:"failedCount"`
	SkippedCount int         `json:"skippedCount"`
	HeldCount    int         `json:"heldCount"`
	Rows         []RowResult `json:"rows"`
}

// RunSummary is Run without the per-row log, for list views.
type RunSummary struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"createdAt"`
	AccountID    string    `json:"accountId"`
	DeliverMode  string    `json:"deliverMode"`
	Status       string    `json:"status"`
	TotalRows    int       `json:"totalRows"`
	SentCount    int       `json:"sentCount"`
	FailedCount  int       `json:"failedCount"`
	SkippedCount int       `json:"skippedCount"`
	HeldCount    int       `json:"heldCount"`
}

// Summary derives a RunSummary from a Run.
func (r *Run) Summary() RunSummary {
	return RunSummary{
		ID: r.ID, CreatedAt: r.CreatedAt, AccountID: r.AccountID,
		DeliverMode: r.DeliverMode, Status: r.Status, TotalRows: r.TotalRows,
		SentCount: r.SentCount, FailedCount: r.FailedCount,
		SkippedCount: r.SkippedCount, HeldCount: r.HeldCount,
	}
}

// RecomputeCounts recounts the per-status totals from r.Rows.
func (r *Run) RecomputeCounts() {
	r.SentCount, r.FailedCount, r.SkippedCount, r.HeldCount = 0, 0, 0, 0
	for _, row := range r.Rows {
		switch row.Status {
		case RowSent, RowDraftSaved:
			r.SentCount++
		case RowFailed:
			r.FailedCount++
		case RowSkipped:
			r.SkippedCount++
		case RowHeld:
			r.HeldCount++
		}
	}
}

// RenderedMessage is one row's rendered template, for preview.
type RenderedMessage struct {
	RowIndex    int      `json:"rowIndex"`
	To          string   `json:"to"`
	Cc          string   `json:"cc"`
	Bcc         string   `json:"bcc"`
	Subject     string   `json:"subject"`
	BodyHTML    string   `json:"bodyHtml"`
	BodyText    string   `json:"bodyText"`
	Attachments []string `json:"attachments"` // resolved per-row attachment paths
}

// ParsedFile is the result of parsing a CSV/JSON source file.
type ParsedFile struct {
	Columns     []string            `json:"columns"`
	RowCount    int                 `json:"rowCount"`
	PreviewRows []map[string]string `json:"previewRows"`
}

// SliceRows applies the 1-based inclusive StartRow/StopRow range to
// rows (0 = first/last row) and returns the selected rows plus their
// original 0-based indices.
func SliceRows(rows []map[string]string, startRow, stopRow int) ([]map[string]string, []int) {
	start := 0
	if startRow > 1 {
		start = startRow - 1
	}
	stop := len(rows)
	if stopRow > 0 && stopRow < stop {
		stop = stopRow
	}
	if start > len(rows) {
		start = len(rows)
	}
	if stop < start {
		stop = start
	}
	selected := rows[start:stop]
	indices := make([]int, 0, len(selected))
	for i := range selected {
		indices = append(indices, start+i)
	}
	return selected, indices
}
