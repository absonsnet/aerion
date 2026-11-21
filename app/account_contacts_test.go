package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/absonsnet/daak/internal/account"
	"github.com/absonsnet/daak/internal/carddav"
	"github.com/absonsnet/daak/internal/contact"
	"github.com/absonsnet/daak/internal/credentials"
	"github.com/absonsnet/daak/internal/database"
	"github.com/absonsnet/daak/internal/settings"
	"github.com/absonsnet/daak/internal/smtp"
)

// accountContactsHarness builds a composeOps against a temp DB, with a
// fake People API server standing in for Google Contacts. No live
// accounts, no network beyond the local test server.
type accountContactsHarness struct {
	t      *testing.T
	ops    *composeOps
	people *fakePeople
	server *httptest.Server
	accID  string
}

type fakePeople struct {
	knownEmail string
	creates    int
	searches   int
	failSearch bool
}

func (f *fakePeople) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/v1/people:searchContacts"):
		f.searches++
		if f.failSearch {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		type person struct {
			EmailAddresses []struct {
				Value string `json:"value"`
			} `json:"emailAddresses"`
		}
		var results []struct {
			Person person `json:"person"`
		}
		if strings.Contains(r.URL.Query().Get("query"), f.knownEmail) {
			results = append(results, struct {
				Person person `json:"person"`
			}{Person: person{EmailAddresses: []struct {
				Value string `json:"value"`
			}{{Value: f.knownEmail}}}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	case r.URL.Path == "/v1/people:createContact" && r.Method == http.MethodPost:
		f.creates++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	default:
		http.NotFound(w, r)
	}
}

func newAccountContactsHarness(t *testing.T, authType account.AuthType, email, imapHost string, scopes []string) *accountContactsHarness {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	accID := "acc-1"
	if _, err := db.Exec(`INSERT INTO accounts (id, name, email, imap_host, smtp_host, username, auth_type) VALUES (?, 'Me', ?, ?, 'smtp.example.com', ?, ?)`,
		accID, email, imapHost, email, string(authType)); err != nil {
		t.Fatal(err)
	}

	credStore, err := credentials.NewStore(db.DB, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if authType == account.AuthOAuth2 {
		if err := credStore.SetOAuthTokens(accID, &credentials.OAuthTokens{
			Provider:     "google",
			AccessToken:  "tok-1",
			RefreshToken: "refresh-1",
			ExpiresAt:    time.Now().Add(2 * time.Hour),
			Scopes:       scopes,
		}); err != nil {
			t.Fatal(err)
		}
	}

	people := &fakePeople{knownEmail: "known@example.com"}
	server := httptest.NewServer(people)
	t.Cleanup(server.Close)
	googleClient := contact.NewGoogleContactsClient()
	googleClient.SetBaseURL(server.URL)

	ops := &composeOps{
		accountStore:         account.NewStore(db),
		credStore:            credStore,
		settingsStore:        settings.NewStore(db),
		carddavStore:         carddav.NewStore(db.DB),
		googleContactsClient: googleClient,
	}
	return &accountContactsHarness{t: t, ops: ops, people: people, server: server, accID: accID}
}

func testSentMessage() smtp.ComposeMessage {
	return smtp.ComposeMessage{
		To:  []smtp.Address{{Address: "new@example.com", Name: "New Person"}, {Address: "known@example.com", Name: "Known Person"}},
		Cc:  []smtp.Address{{Address: "NEW@example.com", Name: "Duplicate"}},
		Bcc: []smtp.Address{{Address: "bcc@example.com"}},
	}
}

func TestCollectSendRecipients(t *testing.T) {
	recipients := collectSendRecipients(testSentMessage())
	if len(recipients) != 3 {
		t.Fatalf("recipients = %d, want 3 (To + Cc deduped, Bcc included)", len(recipients))
	}
	if recipients[0].email != "new@example.com" || recipients[0].name != "New Person" {
		t.Errorf("recipients[0] = %+v, want new@example.com / New Person (To name wins over Cc repeat)", recipients[0])
	}
	if recipients[1].email != "known@example.com" {
		t.Errorf("recipients[1] = %+v, want known@example.com", recipients[1])
	}
	if recipients[2].email != "bcc@example.com" || recipients[2].name != "" {
		t.Errorf("recipients[2] = %+v, want bcc@example.com with no name", recipients[2])
	}

	if got := collectSendRecipients(smtp.ComposeMessage{}); len(got) != 0 {
		t.Errorf("empty message: recipients = %d, want 0", len(got))
	}
}

func TestContactsProviderFor(t *testing.T) {
	ops := &composeOps{}
	google := &account.Account{AuthType: account.AuthOAuth2, IMAPHost: "imap.gmail.com"}
	if got := ops.contactsProviderFor(google); got != "google" {
		t.Errorf("gmail OAuth: provider = %q, want google", got)
	}
	outlook := &account.Account{AuthType: account.AuthOAuth2, IMAPHost: "outlook.office365.com"}
	if got := ops.contactsProviderFor(outlook); got != "microsoft" {
		t.Errorf("outlook OAuth: provider = %q, want microsoft", got)
	}
	imap := &account.Account{AuthType: account.AuthPassword, IMAPHost: "imap.gmx.net"}
	if got := ops.contactsProviderFor(imap); got != "carddav" {
		t.Errorf("password IMAP: provider = %q, want carddav", got)
	}
	custom := &account.Account{AuthType: account.AuthOAuth2, IMAPHost: "imap.example.com"}
	if got := ops.contactsProviderFor(custom); got != "carddav" {
		t.Errorf("custom OAuth: provider = %q, want carddav", got)
	}
}

func TestSaveRecipientsToGoogleContacts(t *testing.T) {
	h := newAccountContactsHarness(t, account.AuthOAuth2, "me@gmail.com", "imap.gmail.com",
		[]string{"https://www.googleapis.com/auth/contacts.readonly", "https://www.googleapis.com/auth/contacts"})

	// First send: the new recipient is created, the known one is skipped,
	// the To/Cc duplicate is created once.
	h.ops.saveRecipientsToAccountContacts(context.Background(), h.accID, testSentMessage())
	if h.people.creates != 2 {
		t.Fatalf("creates = %d, want 2 (new + bcc; known skipped, To/Cc duplicate once)", h.people.creates)
	}

	// Second send of the same message: the server now reports both as
	// known (fake server treats them as known after creation is not
	// modelled — instead assert no NEW creates happen for the already
	// created addresses by marking them known).
	h.people.knownEmail = "new@example.com"
	before := h.people.creates
	h.ops.saveRecipientsToAccountContacts(context.Background(), h.accID, smtp.ComposeMessage{
		To: []smtp.Address{{Address: "new@example.com", Name: "New Person"}},
	})
	if h.people.creates != before {
		t.Errorf("creates grew on second send to the same person: %d -> %d, want no duplicate", before, h.people.creates)
	}
}

func TestSaveRecipientsToggleOff(t *testing.T) {
	h := newAccountContactsHarness(t, account.AuthOAuth2, "me@gmail.com", "imap.gmail.com",
		[]string{"https://www.googleapis.com/auth/contacts"})
	if err := h.ops.settingsStore.SetSaveRecipientsToAccountContacts(false); err != nil {
		t.Fatal(err)
	}
	h.ops.saveRecipientsToAccountContacts(context.Background(), h.accID, testSentMessage())
	if h.people.searches != 0 || h.people.creates != 0 {
		t.Errorf("toggle off: searches=%d creates=%d, want 0/0 (local collected only)", h.people.searches, h.people.creates)
	}
}

func TestSaveRecipientsGoogleMissingScope(t *testing.T) {
	// Tokens without the contacts write scope: the provider save is
	// skipped silently, before any API call.
	h := newAccountContactsHarness(t, account.AuthOAuth2, "me@gmail.com", "imap.gmail.com",
		[]string{"https://www.googleapis.com/auth/contacts.readonly"})
	h.ops.saveRecipientsToAccountContacts(context.Background(), h.accID, testSentMessage())
	if h.people.searches != 0 || h.people.creates != 0 {
		t.Errorf("missing write scope: searches=%d creates=%d, want 0/0", h.people.searches, h.people.creates)
	}
}

func TestSaveRecipientsProviderErrorIsSilent(t *testing.T) {
	h := newAccountContactsHarness(t, account.AuthOAuth2, "me@gmail.com", "imap.gmail.com",
		[]string{"https://www.googleapis.com/auth/contacts"})
	h.people.failSearch = true
	// Must not panic and must not fail the send — the function returns
	// normally; the local collected contact already happened upstream.
	h.ops.saveRecipientsToAccountContacts(context.Background(), h.accID, testSentMessage())
	if h.people.creates != 0 {
		t.Errorf("creates = %d, want 0 when the existence check errors", h.people.creates)
	}
}

func TestSaveRecipientsCardDAVSourceAbsent(t *testing.T) {
	// Password IMAP account with no CardDAV source linked: local-only,
	// no error, no attempt to reach any provider.
	h := newAccountContactsHarness(t, account.AuthPassword, "me@gmx.net", "imap.gmx.net", nil)
	h.ops.saveRecipientsToAccountContacts(context.Background(), h.accID, testSentMessage())
	if h.people.searches != 0 || h.people.creates != 0 {
		t.Errorf("no CardDAV source: searches=%d creates=%d, want 0/0", h.people.searches, h.people.creates)
	}
}
