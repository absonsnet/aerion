package enrich

import (
	"regexp"
	"strings"
	"time"
)

// Attention types (v1 — exactly these four; security alerts and
// "reply needed" are explicitly not in v1).
const (
	AttentionVerificationCode = "verification_code"
	AttentionFlightChanged    = "flight_changed"
	AttentionPaymentFailed    = "payment_failed"
	AttentionDeliveryProblem  = "delivery_problem"
)

// AttentionTypeLabel is the user-facing label for an attention type.
func AttentionTypeLabel(t string) string {
	switch t {
	case AttentionVerificationCode:
		return "Verification code"
	case AttentionFlightChanged:
		return "Flight changed"
	case AttentionPaymentFailed:
		return "Payment failed"
	case AttentionDeliveryProblem:
		return "Delivery problem"
	}
	return t
}

// AttentionInput is the read-time view of a message used to decide whether
// it earns a Needs attention bar. Attention eligibility is derived from
// the stored enrichment + message age — no separate column.
type AttentionInput struct {
	MessageID   string
	AccountID   string
	AccountName string
	Subject     string
	BodyHead    string
	FromEmail   string
	Category    string
	SubCategory string
	ExtractKind string
	Date        time.Time
}

// AttentionItem is one derived bar: its type and when it stops applying.
type AttentionItem struct {
	Type      string
	TypeLabel string
	ExpiresAt time.Time
}

var (
	flightChangeKeywords = []string{
		"cancelled", "canceled", "rescheduled", "delayed", "gate change",
		"flight change", "flight cancelled", "flight canceled",
		"flight delayed", "flight rescheduled", "itinerary change",
		"schedule change", "departure time has changed",
	}
	paymentFailedKeywords = []string{
		"payment failed", "payment declined", "payment overdue",
		"failed payment", "declined", "overdue", "insufficient funds",
		"could not process your payment", "unable to process your payment",
		"card declined", "transaction declined",
	}
	deliveryProblemKeywords = []string{
		"failed delivery", "delivery failed", "delivery exception",
		"held at customs", "address needed", "undeliverable",
		"returned to sender", "delivery attempted", "could not be delivered",
		"customs clearance", "awaiting customs",
	}
)

// EvaluateAttention decides whether a message qualifies for a Needs
// attention bar at time now, and when the bar expires. Rules in v1:
//  1. Verification code — OTP/code extracted AND message < 24h old.
//  2. Flight changed — travel sender/domain + change keywords; expires
//     after the travel date if parseable, else 7 days.
//  3. Payment failed — bank/payment sender + failed/declined/overdue
//     keywords; 14 days.
//  4. Delivery problem — courier sender + exception keywords; 7 days.
//
// A message qualifies for at most one bar: the first matching rule in
// the order above wins.
func EvaluateAttention(in AttentionInput, now time.Time) (AttentionItem, bool) {
	text := strings.ToLower(in.Subject + "\n" + in.BodyHead)

	// 1. Verification code.
	if in.ExtractKind == ExtractCode {
		if now.Sub(in.Date) < 24*time.Hour {
			return AttentionItem{
				Type:      AttentionVerificationCode,
				TypeLabel: AttentionTypeLabel(AttentionVerificationCode),
				ExpiresAt: in.Date.Add(24 * time.Hour),
			}, true
		}
		return AttentionItem{}, false
	}

	// 2. Flight changed (travel changes generally: flights, hotels, bookings).
	if in.SubCategory == "Travel" || IsTravelSender(in.FromEmail) {
		if containsAny(text, flightChangeKeywords) {
			expiry := now.Add(7 * 24 * time.Hour)
			if d, ok := parseTravelDate(in.Subject + "\n" + in.BodyHead); ok {
				// Expire at the end of the travel day.
				expiry = time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, d.Location())
			}
			if now.Before(expiry) {
				return AttentionItem{
					Type:      AttentionFlightChanged,
					TypeLabel: AttentionTypeLabel(AttentionFlightChanged),
					ExpiresAt: expiry,
				}, true
			}
			return AttentionItem{}, false
		}
	}

	// 3. Payment failed.
	if in.SubCategory == "Transactions" || IsTransactionSender(in.FromEmail) {
		if containsAny(text, paymentFailedKeywords) {
			expiry := in.Date.Add(14 * 24 * time.Hour)
			if now.Before(expiry) {
				return AttentionItem{
					Type:      AttentionPaymentFailed,
					TypeLabel: AttentionTypeLabel(AttentionPaymentFailed),
					ExpiresAt: expiry,
				}, true
			}
			return AttentionItem{}, false
		}
	}

	// 4. Delivery problem.
	if in.SubCategory == "Shipping" || IsShippingSender(in.FromEmail) {
		if containsAny(text, deliveryProblemKeywords) {
			expiry := in.Date.Add(7 * 24 * time.Hour)
			if now.Before(expiry) {
				return AttentionItem{
					Type:      AttentionDeliveryProblem,
					TypeLabel: AttentionTypeLabel(AttentionDeliveryProblem),
					ExpiresAt: expiry,
				}, true
			}
			return AttentionItem{}, false
		}
	}

	return AttentionItem{}, false
}

func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

var (
	// "15 Nov 2026", "15 November", "Nov 15, 2026".
	dateDMYRe = regexp.MustCompile(`(?i)\b(\d{1,2})\s+(Jan|January|Feb|February|Mar|March|Apr|April|May|Jun|June|Jul|July|Aug|August|Sep|September|Oct|October|Nov|November|Dec|December)\s+(\d{4})?\b`)
	dateMDYRe = regexp.MustCompile(`(?i)\b(Jan|January|Feb|February|Mar|March|Apr|April|May|Jun|June|Jul|July|Aug|August|Sep|September|Oct|October|Nov|November|Dec|December)\s+(\d{1,2})(?:\s*,?\s*(\d{4}))?\b`)
	// ISO "2026-11-20".
	dateISORe = regexp.MustCompile(`\b(20\d{2})-(\d{2})-(\d{2})\b`)
)

var monthNumbers = map[string]time.Month{
	"jan": time.January, "january": time.January,
	"feb": time.February, "february": time.February,
	"mar": time.March, "march": time.March,
	"apr": time.April, "april": time.April,
	"may": time.May,
	"jun": time.June, "june": time.June,
	"jul": time.July, "july": time.July,
	"aug": time.August, "august": time.August,
	"sep": time.September, "september": time.September,
	"oct": time.October, "october": time.October,
	"nov": time.November, "november": time.November,
	"dec": time.December, "december": time.December,
}

// parseTravelDate finds the first parseable travel date in text. A missing
// year means the next occurrence of that month/day (this year, or next
// year if already past). Returns false when no date is found.
func parseTravelDate(text string) (time.Time, bool) {
	now := time.Now()
	if m := dateISORe.FindStringSubmatch(text); m != nil {
		y, _ := time.Parse("2006-01-02", m[0])
		return y, true
	}
	if m := dateDMYRe.FindStringSubmatch(text); m != nil {
		day := atoi(m[1])
		mon := monthNumbers[strings.ToLower(m[2])]
		year := now.Year()
		if m[3] != "" {
			year = atoi(m[3])
		}
		d := time.Date(year, mon, day, 0, 0, 0, 0, time.Local)
		if m[3] == "" && d.Before(now.Add(-24*time.Hour)) {
			d = d.AddDate(1, 0, 0)
		}
		return d, true
	}
	if m := dateMDYRe.FindStringSubmatch(text); m != nil {
		mon := monthNumbers[strings.ToLower(m[1])]
		day := atoi(m[2])
		year := now.Year()
		if m[3] != "" {
			year = atoi(m[3])
		}
		d := time.Date(year, mon, day, 0, 0, 0, 0, time.Local)
		if m[3] == "" && d.Before(now.Add(-24*time.Hour)) {
			d = d.AddDate(1, 0, 0)
		}
		return d, true
	}
	return time.Time{}, false
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
