package backend

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hkdb/aerion/internal/extensions"
	hostmm "github.com/hkdb/aerion/internal/mailmerge"
)

// migrations is the per-extension migration sequence for the Mail Merge
// extension's isolated DB at <dataDir>/extensions/mail-merge/data.db.
// v1: the run log (one row per run + one row per recipient outcome)
// plus the settings blob namespace (ext_kv, created automatically by
// extensions.OpenStore).
var migrations = []extensions.Migration{
	{
		Version: 1,
		SQL: `
CREATE TABLE IF NOT EXISTS mailmerge_runs (
	id            TEXT PRIMARY KEY,
	account_id    TEXT NOT NULL,
	deliver_mode  TEXT NOT NULL,
	status        TEXT NOT NULL,
	source_kind   TEXT NOT NULL DEFAULT '',
	total_rows    INTEGER NOT NULL DEFAULT 0,
	sent_count    INTEGER NOT NULL DEFAULT 0,
	failed_count  INTEGER NOT NULL DEFAULT 0,
	skipped_count INTEGER NOT NULL DEFAULT 0,
	held_count    INTEGER NOT NULL DEFAULT 0,
	request_json  TEXT NOT NULL DEFAULT '',
	created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS mailmerge_run_rows (
	run_id    TEXT NOT NULL,
	row_index INTEGER NOT NULL,
	email     TEXT NOT NULL DEFAULT '',
	status    TEXT NOT NULL,
	error     TEXT NOT NULL DEFAULT '',
	timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (run_id, row_index)
);
CREATE INDEX IF NOT EXISTS idx_mailmerge_run_rows_run ON mailmerge_run_rows (run_id);
`,
	},
}

// Store wraps the per-extension DB for the Mail Merge extension. Lives
// in an isolated SQLite file, separate from Aerion's main DB. No tables
// in this file are read or written by core code; cross-extension access
// flows through coreapi only.
type Store struct {
	*extensions.Store
}

// NewStore opens the Mail Merge extension's isolated SQLite DB and
// applies pending migrations. Called lazily from Bridge.ensureInit on
// the first enabled bridge call (per EXT_RULES.md R10/R11) — a disabled
// extension never opens its DB.
func NewStore(dataDir string) (*Store, error) {
	s, err := extensions.OpenStore(dataDir, "mail-merge", migrations)
	if err != nil {
		return nil, err
	}
	return &Store{Store: s}, nil
}

// settingsKVKey is the ext_kv key under which one account's settings
// JSON blob is stored.
func settingsKVKey(accountID string) string {
	return "settings:" + accountID
}

// GetSettings reads one account's stored settings blob. Returns the
// defaults when nothing has been saved yet.
func (s *Store) GetSettings(accountID string) (Settings, error) {
	st := DefaultSettings()
	if accountID == "" {
		return st, nil
	}
	raw, err := s.KV().Get(settingsKVKey(accountID))
	if err != nil {
		return Settings{}, fmt.Errorf("mail-merge: read settings: %w", err)
	}
	if raw == "" {
		return st, nil
	}
	// Decode over the defaults so settings saved by an older version
	// (missing newer fields) still yield sane values for those fields.
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return Settings{}, fmt.Errorf("mail-merge: parse stored settings: %w", err)
	}
	return st, nil
}

// SaveSettings persists one account's settings blob.
func (s *Store) SaveSettings(accountID string, st Settings) error {
	if accountID == "" {
		return fmt.Errorf("mail-merge: account is required")
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("mail-merge: encode settings: %w", err)
	}
	if err := s.KV().Set(settingsKVKey(accountID), string(raw)); err != nil {
		return fmt.Errorf("mail-merge: save settings: %w", err)
	}
	return nil
}

// SaveRun inserts a new run record with its full request JSON (template
// + rows + options, so resume/batch operations can rebuild the run
// without the frontend re-sending the table).
func (s *Store) SaveRun(run *hostmm.Run, req hostmm.RunRequest) error {
	raw, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("mail-merge: encode run request: %w", err)
	}
	_, err = s.DB().Exec(`
		INSERT INTO mailmerge_runs
			(id, account_id, deliver_mode, status, source_kind, total_rows,
			 sent_count, failed_count, skipped_count, held_count, request_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.AccountID, run.DeliverMode, run.Status, req.SourceKind,
		run.TotalRows, run.SentCount, run.FailedCount, run.SkippedCount, run.HeldCount,
		string(raw), run.CreatedAt.UTC().Format(time.RFC3339Nano), run.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("mail-merge: save run: %w", err)
	}
	return nil
}

// UpdateRunStatusAndCounts rewrites a run's status + counts from the
// in-memory Run (after progress or completion).
func (s *Store) UpdateRunStatusAndCounts(run *hostmm.Run) error {
	_, err := s.DB().Exec(`
		UPDATE mailmerge_runs
		SET status = ?, sent_count = ?, failed_count = ?, skipped_count = ?,
			held_count = ?, total_rows = ?, updated_at = ?
		WHERE id = ?
	`, run.Status, run.SentCount, run.FailedCount, run.SkippedCount, run.HeldCount,
		run.TotalRows, time.Now().UTC().Format(time.RFC3339Nano), run.ID)
	if err != nil {
		return fmt.Errorf("mail-merge: update run: %w", err)
	}
	return nil
}

// SaveRowResult upserts one row's outcome (held rows are later updated
// in place by batch operations, hence INSERT ... ON CONFLICT).
func (s *Store) SaveRowResult(runID string, r hostmm.RowResult) error {
	_, err := s.DB().Exec(`
		INSERT INTO mailmerge_run_rows (run_id, row_index, email, status, error, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, row_index) DO UPDATE SET
			email = excluded.email, status = excluded.status,
			error = excluded.error, timestamp = excluded.timestamp
	`, runID, r.Index, r.Email, r.Status, r.Error, r.Timestamp.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("mail-merge: save run row: %w", err)
	}
	return nil
}

// GetRun loads a run with its per-row log. Returns (nil, nil) when the
// run does not exist.
func (s *Store) GetRun(runID string) (*hostmm.Run, error) {
	var run hostmm.Run
	var createdAt, updatedAt string
	err := s.DB().QueryRow(`
		SELECT id, account_id, deliver_mode, status, total_rows,
			sent_count, failed_count, skipped_count, held_count, created_at, updated_at
		FROM mailmerge_runs WHERE id = ?
	`, runID).Scan(&run.ID, &run.AccountID, &run.DeliverMode, &run.Status,
		&run.TotalRows, &run.SentCount, &run.FailedCount, &run.SkippedCount,
		&run.HeldCount, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("mail-merge: get run: %w", err)
	}
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		run.CreatedAt = t
	}
	rows, err := s.DB().Query(`
		SELECT row_index, email, status, error, timestamp
		FROM mailmerge_run_rows WHERE run_id = ? ORDER BY row_index ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("mail-merge: get run rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r hostmm.RowResult
		var ts string
		if err := rows.Scan(&r.Index, &r.Email, &r.Status, &r.Error, &ts); err != nil {
			return nil, fmt.Errorf("mail-merge: scan run row: %w", err)
		}
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			r.Timestamp = t
		}
		run.Rows = append(run.Rows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mail-merge: read run rows: %w", err)
	}
	if run.Rows == nil {
		run.Rows = []hostmm.RowResult{}
	}
	return &run, nil
}

// GetRunRequest loads the stored request JSON for a run (needed by
// resume/batch operations). Returns an error when the run or its
// request is missing.
func (s *Store) GetRunRequest(runID string) (hostmm.RunRequest, error) {
	var raw string
	err := s.DB().QueryRow(`SELECT request_json FROM mailmerge_runs WHERE id = ?`, runID).Scan(&raw)
	if err == sql.ErrNoRows {
		return hostmm.RunRequest{}, errors.New("mail-merge: run not found")
	}
	if err != nil {
		return hostmm.RunRequest{}, fmt.Errorf("mail-merge: get run request: %w", err)
	}
	var req hostmm.RunRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return hostmm.RunRequest{}, fmt.Errorf("mail-merge: parse run request: %w", err)
	}
	return req, nil
}

// ListRuns returns run summaries, newest first.
func (s *Store) ListRuns() ([]hostmm.RunSummary, error) {
	rows, err := s.DB().Query(`
		SELECT id, account_id, deliver_mode, status, total_rows,
			sent_count, failed_count, skipped_count, held_count, created_at
		FROM mailmerge_runs ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("mail-merge: list runs: %w", err)
	}
	defer rows.Close()
	out := []hostmm.RunSummary{}
	for rows.Next() {
		var sum hostmm.RunSummary
		var createdAt string
		if err := rows.Scan(&sum.ID, &sum.AccountID, &sum.DeliverMode, &sum.Status,
			&sum.TotalRows, &sum.SentCount, &sum.FailedCount, &sum.SkippedCount,
			&sum.HeldCount, &createdAt); err != nil {
			return nil, fmt.Errorf("mail-merge: scan run: %w", err)
		}
		if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			sum.CreatedAt = t
		}
		out = append(out, sum)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mail-merge: read runs: %w", err)
	}
	return out, nil
}
