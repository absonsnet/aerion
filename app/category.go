package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/absonsnet/daak/internal/enrich"
	"github.com/absonsnet/daak/internal/message"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ============================================================================
// Categories and labels (Phase E) - Wails bindings
// ============================================================================

// enrichmentBackfillDoneKey guards the one-off enrichment backfill so it
// runs exactly once (see RunEnrichmentBackfill in attention.go).
const enrichmentBackfillDoneKey = "enrichment_backfill_done_v1"

// GetInboxTabDefault returns the default category tab for Inbox /
// Unified Inbox ("all", "personal" or "automated"; default "all").
func (a *App) GetInboxTabDefault() (string, error) {
	return a.settingsStore.GetInboxTabDefault()
}

// SetInboxTabDefault sets the default category tab for Inbox / Unified
// Inbox.
func (a *App) SetInboxTabDefault(tab string) error {
	return a.settingsStore.SetInboxTabDefault(tab)
}

// GetCategoryCounts returns the tab counts for the current list scope:
// the three category tabs plus, inside Automated, one count per
// sub-category. When unified is true the scope is every inbox of every
// enabled account; otherwise it is the single folderID.
func (a *App) GetCategoryCounts(folderID string, unified bool) (*message.CategoryCounts, error) {
	return a.messageStore.GetCategoryCounts(folderID, unified)
}

// SenderCategoryOverride is one user "wrong tab" correction: mail from
// this sender is always filed under Category, beating every heuristic.
type SenderCategoryOverride struct {
	SenderEmail string `json:"senderEmail"`
	Category    string `json:"category"`
}

// GetSenderCategoryOverrides returns every sender override, sorted by
// sender address, for Settings → Categories.
func (a *App) GetSenderCategoryOverrides() ([]SenderCategoryOverride, error) {
	overrides, err := a.messageStore.ListSenderCategoryOverrides()
	if err != nil {
		return nil, err
	}
	out := make([]SenderCategoryOverride, 0, len(overrides))
	for sender, category := range overrides {
		out = append(out, SenderCategoryOverride{SenderEmail: sender, Category: category})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SenderEmail < out[j].SenderEmail })
	return out, nil
}

// SetSenderCategoryOverride records "Always treat mail from this sender
// as Personal / Automated". The override beats every heuristic for that
// sender from now on, and mail already stored from the sender is
// re-filed under it immediately, so the correction shows in the list
// without waiting for a re-sync. Nothing moves folders: the category is
// a filter over the same folder, so there is nothing to undo.
func (a *App) SetSenderCategoryOverride(senderEmail, category string) error {
	senderEmail = strings.ToLower(strings.TrimSpace(senderEmail))
	if senderEmail == "" {
		return fmt.Errorf("sender email is required")
	}
	switch category {
	case enrich.CategoryPersonal, enrich.CategoryAutomated:
	default:
		return fmt.Errorf("invalid category: %s (must be 'personal' or 'automated')", category)
	}
	if err := a.messageStore.SetSenderCategoryOverride(senderEmail, category); err != nil {
		return err
	}
	if _, err := a.messageStore.ApplySenderCategoryOverride(senderEmail, category); err != nil {
		return err
	}
	wailsRuntime.EventsEmit(a.ctx, "categories:changed", senderEmail)
	return nil
}

// RemoveSenderCategoryOverride removes a sender override. Future mail
// from the sender is classified by the heuristics again; mail already
// stored keeps its stored category until it is re-enriched.
func (a *App) RemoveSenderCategoryOverride(senderEmail string) error {
	senderEmail = strings.ToLower(strings.TrimSpace(senderEmail))
	if senderEmail == "" {
		return fmt.Errorf("sender email is required")
	}
	if err := a.messageStore.DeleteSenderCategoryOverride(senderEmail); err != nil {
		return err
	}
	wailsRuntime.EventsEmit(a.ctx, "categories:changed", senderEmail)
	return nil
}

// BackfillStatus is the one-off enrichment backfill's progress for
// Settings → Categories: how many already-synced messages still have
// no category, and whether the guarded backfill has run.
type BackfillStatus struct {
	Remaining int  `json:"remaining"`
	Done      bool `json:"done"`
}

// GetBackfillStatus returns the enrichment backfill progress. Remaining
// counts messages whose category has never been computed; Done reports
// whether the guarded one-off backfill has run.
func (a *App) GetBackfillStatus() (*BackfillStatus, error) {
	done, err := a.settingsStore.Get(enrichmentBackfillDoneKey)
	if err != nil {
		return nil, err
	}
	remaining, err := a.messageStore.CountUnenriched()
	if err != nil {
		return nil, err
	}
	return &BackfillStatus{Remaining: remaining, Done: done == "true"}, nil
}

// RunEnrichmentBackfillNow runs the enrichment backfill on demand
// (Settings → Categories → "Classify now"). Unlike
// RunEnrichmentBackfill, which runs once at launch, this bypasses the
// once-only guard: the backfill only ever touches rows whose category is
// still NULL, so re-running is safe — it picks up mail that arrived
// unclassified after the launch backfill. Returns how many messages
// were classified.
func (a *App) RunEnrichmentBackfillNow() (int, error) {
	n, err := a.syncEngine.BackfillEnrichment(context.Background())
	if err != nil {
		return n, err
	}
	if err := a.settingsStore.Set(enrichmentBackfillDoneKey, "true"); err != nil {
		return n, err
	}
	wailsRuntime.EventsEmit(a.ctx, "categories:changed", "")
	return n, nil
}
