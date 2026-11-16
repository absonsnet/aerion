package auth

import (
	"path/filepath"
	"testing"

	coreapi "github.com/absonsnet/daak/internal/core/api/v1"
	"github.com/absonsnet/daak/internal/credentials"
	"github.com/absonsnet/daak/internal/database"
	"github.com/absonsnet/daak/internal/oauth2"
)

// newTestBroker spins up a temp DB + credentials store + OAuth manager for
// broker integration tests. Real OAuth refresh isn't exercised here (that
// requires an httptest.Server stub of provider token endpoints) — the focus
// is the broker's pre-call decisions: scope coverage, client-config routing,
// ErrAdditionalConsentRequired paths.
func newTestBroker(t *testing.T) (*Broker, *credentials.Store, *database.DB) {
	t.Helper()
	tmp := t.TempDir()
	db, err := database.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	credStore, err := credentials.NewStore(db.DB, tmp)
	if err != nil {
		t.Fatalf("credentials.NewStore: %v", err)
	}

	mgr := oauth2.NewManager()
	return NewBroker(credStore, mgr, nil), credStore, db
}

func insertTestAccount(t *testing.T, db *database.DB, id string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO accounts (id, name, email, imap_host, smtp_host, username)
		VALUES (?, 'Test', ?, 'imap.example.com', 'smtp.example.com', ?)
	`, id, id+"@example.com", id+"@example.com")
	if err != nil {
		t.Fatalf("insert account %s: %v", id, err)
	}
}

func TestBrokerHTTPClient_AccountNotFound(t *testing.T) {
	broker, _, _ := newTestBroker(t)
	_, err := broker.HTTPClient("nonexistent-account", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/calendar"},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent account, got nil")
	}
}

func TestBrokerHTTPClient_ScopesUncovered_ReturnsConsentRequired(t *testing.T) {
	broker, credStore, db := newTestBroker(t)
	insertTestAccount(t, db, "acct-a")

	// Account has Mail tokens with mail-only scopes — does NOT cover Calendar.
	err := credStore.SetOAuthTokens("acct-a", &credentials.OAuthTokens{
		Provider:     "google",
		AccessToken:  "access-token-mail",
		RefreshToken: "refresh-token-mail",
		Scopes:       []string{"https://mail.google.com/"},
	})
	if err != nil {
		t.Fatalf("set mail tokens: %v", err)
	}

	_, err = broker.HTTPClient("acct-a", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/calendar"},
	})
	if err == nil {
		t.Fatal("expected ErrAdditionalConsentRequired, got nil")
	}
	consentErr, ok := err.(*coreapi.ErrAdditionalConsentRequired)
	if !ok {
		t.Fatalf("expected *ErrAdditionalConsentRequired, got %T: %v", err, err)
	}
	if consentErr.AccountID != "acct-a" {
		t.Errorf("AccountID: got %q, want %q", consentErr.AccountID, "acct-a")
	}
	if len(consentErr.MissingScopes) != 1 {
		t.Errorf("MissingScopes len: got %d, want 1", len(consentErr.MissingScopes))
	}
}

func TestBrokerHTTPClient_ScopesCovered_ReturnsClient(t *testing.T) {
	broker, credStore, db := newTestBroker(t)
	insertTestAccount(t, db, "acct-b")

	// Account has tokens covering both Mail AND Calendar scopes under the
	// mail config. (Phase 1 without extension config provisioned: the broker
	// falls back to the mail config, so a mail-config token with both scopes
	// represents the "all scopes granted" case.)
	err := credStore.SetOAuthTokens("acct-b", &credentials.OAuthTokens{
		Provider:     "google",
		AccessToken:  "access-token-combined",
		RefreshToken: "refresh-token-combined",
		Scopes: []string{
			"https://mail.google.com/",
			"https://www.googleapis.com/auth/calendar",
		},
	})
	if err != nil {
		t.Fatalf("set combined tokens: %v", err)
	}

	client, err := broker.HTTPClient("acct-b", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/calendar"},
	})
	if err != nil {
		t.Fatalf("HTTPClient returned error for covered scopes: %v", err)
	}
	if client == nil {
		t.Fatal("HTTPClient returned nil client")
	}
	if client.Transport == nil {
		t.Fatal("client has no transport")
	}
}

// HTTPClientForExtension tests: verify the Phase 2b manifest-driven routing.

func TestHTTPClientForExtension_RoutesToCoreForListedScopes(t *testing.T) {
	broker, credStore, db := newTestBroker(t)
	insertTestAccount(t, db, "acct-route-core")

	// Account has mail tokens that include the contacts.readonly scope (which
	// is what mail OAuth includes for autocomplete).
	if err := credStore.SetOAuthTokens("acct-route-core", &credentials.OAuthTokens{
		Provider:     "google",
		AccessToken:  "mail-access",
		RefreshToken: "mail-refresh",
		Scopes: []string{
			"https://mail.google.com/",
			"https://www.googleapis.com/auth/contacts.readonly",
		},
	}); err != nil {
		t.Fatalf("set mail tokens: %v", err)
	}

	// Contacts manifest declares contacts.readonly as core-routed.
	manifest := coreapi.Manifest{
		ID: "contacts",
		OAuth: &coreapi.ManifestOAuth{
			FirstPartyUsesCoreForScopes: []string{
				"https://www.googleapis.com/auth/contacts.readonly",
			},
		},
	}

	client, err := broker.HTTPClientForExtension("contacts", manifest, "acct-route-core", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/contacts.readonly"},
	})
	if err != nil {
		t.Fatalf("expected success routing through mail config, got: %v", err)
	}
	if client == nil {
		t.Fatal("expected *http.Client, got nil")
	}
}

func TestHTTPClientForExtension_RoutesToExtensionForUnlistedScopes(t *testing.T) {
	broker, credStore, db := newTestBroker(t)
	insertTestAccount(t, db, "acct-route-ext")

	// Account has mail tokens but NO write-scope tokens under google-contacts.
	if err := credStore.SetOAuthTokens("acct-route-ext", &credentials.OAuthTokens{
		Provider:     "google",
		AccessToken:  "mail-access",
		RefreshToken: "mail-refresh",
		Scopes:       []string{"https://mail.google.com/"},
	}); err != nil {
		t.Fatalf("set mail tokens: %v", err)
	}

	// Contacts manifest lists only the READ scope as core-routed. The write
	// scope below is NOT listed, so the broker should route to google-contacts.
	manifest := coreapi.Manifest{
		ID: "contacts",
		OAuth: &coreapi.ManifestOAuth{
			FirstPartyUsesCoreForScopes: []string{
				"https://www.googleapis.com/auth/contacts.readonly",
			},
		},
	}

	_, err := broker.HTTPClientForExtension("contacts", manifest, "acct-route-ext", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/contacts"},
	})
	if err == nil {
		t.Fatal("expected ErrAdditionalConsentRequired (no tokens under google-contacts), got nil")
	}
	consentErr, ok := err.(*coreapi.ErrAdditionalConsentRequired)
	if !ok {
		t.Fatalf("expected *ErrAdditionalConsentRequired, got %T: %v", err, err)
	}
	if string(consentErr.ClientConfigID) != "google-contacts" {
		t.Errorf("ClientConfigID: got %q, want google-contacts", consentErr.ClientConfigID)
	}
}

func TestHTTPClientForExtension_RejectsMixedScopes(t *testing.T) {
	broker, credStore, db := newTestBroker(t)
	insertTestAccount(t, db, "acct-mixed")

	if err := credStore.SetOAuthTokens("acct-mixed", &credentials.OAuthTokens{
		Provider:     "google",
		AccessToken:  "mail-access",
		RefreshToken: "mail-refresh",
		Scopes:       []string{"https://mail.google.com/"},
	}); err != nil {
		t.Fatalf("set mail tokens: %v", err)
	}

	manifest := coreapi.Manifest{
		ID: "contacts",
		OAuth: &coreapi.ManifestOAuth{
			FirstPartyUsesCoreForScopes: []string{
				"https://www.googleapis.com/auth/contacts.readonly",
			},
		},
	}

	// Mix of a core-routed scope and an ext-routed scope in one call.
	_, err := broker.HTTPClientForExtension("contacts", manifest, "acct-mixed", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/contacts.readonly"},
		{Resource: "https://www.googleapis.com/auth/contacts"},
	})
	if err == nil {
		t.Fatal("expected error for mixed-scope call, got nil")
	}
}

func TestHTTPClientForExtension_NoManifestOAuthRoutesToExtension(t *testing.T) {
	broker, credStore, db := newTestBroker(t)
	insertTestAccount(t, db, "acct-no-manifest")

	if err := credStore.SetOAuthTokens("acct-no-manifest", &credentials.OAuthTokens{
		Provider:     "google",
		AccessToken:  "mail-access",
		RefreshToken: "mail-refresh",
		Scopes:       []string{"https://mail.google.com/"},
	}); err != nil {
		t.Fatalf("set mail tokens: %v", err)
	}

	// Manifest omits OAuth entirely → all scopes route to extension's own config.
	manifest := coreapi.Manifest{ID: "myext"}

	_, err := broker.HTTPClientForExtension("myext", manifest, "acct-no-manifest", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/anything"},
	})
	if err == nil {
		t.Fatal("expected ErrAdditionalConsentRequired, got nil")
	}
	consentErr, ok := err.(*coreapi.ErrAdditionalConsentRequired)
	if !ok {
		t.Fatalf("expected *ErrAdditionalConsentRequired, got %T", err)
	}
	if string(consentErr.ClientConfigID) != "google-myext" {
		t.Errorf("ClientConfigID: got %q, want google-myext", consentErr.ClientConfigID)
	}
}

func TestBrokerIMAPClient_Unimplemented(t *testing.T) {
	broker, _, _ := newTestBroker(t)
	_, err := broker.IMAPClient("any", nil)
	if err != coreapi.ErrUnimplemented {
		t.Fatalf("expected ErrUnimplemented, got %v", err)
	}
}

func TestBrokerSMTPClient_Unimplemented(t *testing.T) {
	broker, _, _ := newTestBroker(t)
	_, err := broker.SMTPClient("any")
	if err != coreapi.ErrUnimplemented {
		t.Fatalf("expected ErrUnimplemented, got %v", err)
	}
}

// Calendar routing tests: the calendar manifest lists the Google calendar
// scope as core-routed, so it reuses the linked account's mail grant.
//
// TestHTTPClientForExtension_CalendarRoutesToCoreWhenMailTokenCarriesScope
// covers the linked-after-this-change case: the mail token carries the
// calendar scope, so the calendar extension gets a client with no extra
// consent step.
func TestHTTPClientForExtension_CalendarRoutesToCoreWhenMailTokenCarriesScope(t *testing.T) {
	broker, credStore, db := newTestBroker(t)
	insertTestAccount(t, db, "acct-cal-core")

	if err := credStore.SetOAuthTokens("acct-cal-core", &credentials.OAuthTokens{
		Provider:     "google",
		AccessToken:  "mail-access",
		RefreshToken: "mail-refresh",
		Scopes: []string{
			"https://mail.google.com/",
			"https://www.googleapis.com/auth/calendar",
			"https://www.googleapis.com/auth/contacts.readonly",
		},
	}); err != nil {
		t.Fatalf("set mail tokens: %v", err)
	}

	manifest := coreapi.Manifest{
		ID: "calendar",
		OAuth: &coreapi.ManifestOAuth{
			FirstPartyUsesCoreForScopes: []string{
				"https://www.googleapis.com/auth/calendar",
			},
		},
	}

	client, err := broker.HTTPClientForExtension("calendar", manifest, "acct-cal-core", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/calendar"},
	})
	if err != nil {
		t.Fatalf("expected success routing through mail config, got: %v", err)
	}
	if client == nil {
		t.Fatal("expected *http.Client, got nil")
	}
}

// TestHTTPClientForExtension_CalendarPartialGrant is the partial-consent
// case: the user granted Gmail + contacts but NOT calendar (or linked
// before the mail flow requested the calendar scope, or later revoked it).
// Mail and contacts must keep working on the same token, while calendar
// reports exactly the missing scope under the mail client config so the
// extension can offer its own "Connect calendar" step — nothing blocks,
// nothing else errors.
func TestHTTPClientForExtension_CalendarPartialGrant(t *testing.T) {
	broker, credStore, db := newTestBroker(t)
	insertTestAccount(t, db, "acct-cal-partial")

	// Mail-only token: Gmail + contacts, no calendar scope.
	if err := credStore.SetOAuthTokens("acct-cal-partial", &credentials.OAuthTokens{
		Provider:     "google",
		AccessToken:  "mail-access",
		RefreshToken: "mail-refresh",
		Scopes: []string{
			"https://mail.google.com/",
			"https://www.googleapis.com/auth/contacts.readonly",
			"https://www.googleapis.com/auth/contacts.other.readonly",
		},
	}); err != nil {
		t.Fatalf("set mail tokens: %v", err)
	}

	calendarManifest := coreapi.Manifest{
		ID: "calendar",
		OAuth: &coreapi.ManifestOAuth{
			FirstPartyUsesCoreForScopes: []string{
				"https://www.googleapis.com/auth/calendar",
			},
		},
	}

	// Calendar stays unconnected with exactly the missing scope named.
	_, err := broker.HTTPClientForExtension("calendar", calendarManifest, "acct-cal-partial", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/calendar"},
	})
	if err == nil {
		t.Fatal("expected ErrAdditionalConsentRequired for the missing calendar scope, got nil")
	}
	consentErr, ok := err.(*coreapi.ErrAdditionalConsentRequired)
	if !ok {
		t.Fatalf("expected *ErrAdditionalConsentRequired, got %T: %v", err, err)
	}
	if string(consentErr.ClientConfigID) != "google-mail" {
		t.Errorf("ClientConfigID: got %q, want google-mail", consentErr.ClientConfigID)
	}
	if len(consentErr.MissingScopes) != 1 || consentErr.MissingScopes[0].Resource != "https://www.googleapis.com/auth/calendar" {
		t.Errorf("MissingScopes: got %v, want exactly the calendar scope", consentErr.MissingScopes)
	}

	// Contacts keeps working on the same token.
	contactsManifest := coreapi.Manifest{
		ID: "contacts",
		OAuth: &coreapi.ManifestOAuth{
			FirstPartyUsesCoreForScopes: []string{
				"https://www.googleapis.com/auth/contacts.readonly",
			},
		},
	}
	client, err := broker.HTTPClientForExtension("contacts", contactsManifest, "acct-cal-partial", []coreapi.AuthScope{
		{Resource: "https://www.googleapis.com/auth/contacts.readonly"},
	})
	if err != nil {
		t.Fatalf("contacts must keep working on a partial grant, got: %v", err)
	}
	if client == nil {
		t.Fatal("expected *http.Client for contacts, got nil")
	}
}
