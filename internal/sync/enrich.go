package sync

import (
	"bytes"
	"context"
	"strings"

	"github.com/absonsnet/daak/internal/enrich"
	"github.com/absonsnet/daak/internal/message"
	gomessage "github.com/emersion/go-message"
)

// headerValue parses raw header bytes and returns the named header's value
// ("" when absent or unparseable). Mirrors extractReferences in
// threading.go.
func (e *Engine) headerValue(raw []byte, name string) string {
	reader := bytes.NewReader(raw)
	entity, err := gomessage.Read(reader)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(entity.Header.Get(name))
}

// enrichHeader runs the header-time enrichment for a freshly fetched
// message: List-Unsubscribe parsing + category classification. Extraction
// needs the body, so the body pass fills it in. Overrides are passed in
// (loaded once per header batch); the known-personal-sender check needs a
// per-sender DB lookup and is left to the body pass and the backfill,
// which correct the row if the header pass over-filed it.
func (e *Engine) enrichHeader(m *message.Message, headerBytes []byte, overrides map[string]string) {
	lu := e.headerValue(headerBytes, "List-Unsubscribe")
	post := e.headerValue(headerBytes, "List-Unsubscribe-Post")
	in := enrich.Input{
		FromEmail:       m.FromEmail,
		FromName:        m.FromName,
		Subject:         m.Subject,
		ListUnsubscribe: lu,
		Precedence:      e.headerValue(headerBytes, "Precedence"),
	}
	if cat, ok := overrides[strings.ToLower(strings.TrimSpace(m.FromEmail))]; ok {
		in.OverrideCategory, in.HasOverride = cat, true
	}
	res := enrich.Enrich(in)
	m.ListUnsubscribe = res.ListUnsubscribe
	// RFC 8058 one-click needs BOTH the URI and the Post marker.
	m.ListUnsubscribePost = lu != "" && strings.Contains(strings.ToLower(post), "one-click")
	m.Category = res.Category
	m.SubCategory = res.SubCategory
}

// enrichBody runs the full enrichment once a body is available (subject +
// snippet + body head + stored List-Unsubscribe). It returns nil when the
// stored header can't be loaded, so the caller keeps the header-time row.
// The extracted value is never logged; only enrich.SafeLogFields go to
// the log.
func (e *Engine) enrichBody(messageID, bodyText, snippet string) *enrich.Result {
	h, err := e.messageStore.GetHeaderForEnrichment(messageID)
	if err != nil {
		e.log.Warn().Err(err).Str("messageID", messageID).Msg("Enrichment skipped: header lookup failed")
		return nil
	}
	if h == nil {
		return nil
	}
	in := enrich.Input{
		FromEmail:       h.FromEmail,
		FromName:        h.FromName,
		Subject:         h.Subject,
		Snippet:         snippet,
		BodyHead:        truncateHead(bodyText, 2000),
		ListUnsubscribe: h.ListUnsubscribe,
		ForceAutomated:  h.Category == enrich.CategoryAutomated,
	}
	if cat, ok, err := e.messageStore.GetSenderCategoryOverride(h.FromEmail); err == nil && ok {
		in.OverrideCategory, in.HasOverride = cat, true
	}
	if known, err := e.messageStore.IsKnownPersonalSender(h.FromEmail); err == nil && known {
		in.KnownPersonalSender = true
	}
	res := enrich.Enrich(in)
	fields := enrich.SafeLogFields(res)
	e.log.Debug().
		Str("messageID", messageID).
		Str("category", fields["category"].(string)).
		Str("subCategory", fields["subCategory"].(string)).
		Str("extractKind", fields["extractKind"].(string)).
		Str("extractRule", fields["extractRule"].(string)).
		Int("extractValueLen", fields["extractValueLen"].(int)).
		Msg("Message enriched")
	return &res
}

func truncateHead(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// BackfillEnrichment is the one-off backfill for already-synced mail: it
// fills the enrichment columns for every message whose category is still
// NULL, in batches of 200, oldest first. It runs automatically once after
// the migration (guarded by a settings key in the app layer) and is safe
// to re-run — already-enriched rows are never revisited.
func (e *Engine) BackfillEnrichment(ctx context.Context) (int, error) {
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		batch, err := e.messageStore.ListUnenriched(200)
		if err != nil {
			return total, err
		}
		if len(batch) == 0 {
			break
		}
		for _, m := range batch {
			in := enrich.Input{
				FromEmail: m.FromEmail,
				FromName:  m.FromName,
				Subject:   m.Subject,
				Snippet:   m.Snippet,
				BodyHead:  m.BodyText, // ListUnenriched caps this at 2000 chars
			}
			if cat, ok, err := e.messageStore.GetSenderCategoryOverride(m.FromEmail); err == nil && ok {
				in.OverrideCategory, in.HasOverride = cat, true
			}
			if known, err := e.messageStore.IsKnownPersonalSender(m.FromEmail); err == nil && known {
				in.KnownPersonalSender = true
			}
			res := enrich.Enrich(in)
			if err := e.messageStore.UpdateEnrichment(m.ID, res); err != nil {
				e.log.Warn().Err(err).Str("messageID", m.ID).Msg("Backfill enrichment failed for message")
				continue
			}
			total++
		}
	}
	e.log.Info().Int("enriched", total).Msg("Enrichment backfill complete")
	return total, nil
}
