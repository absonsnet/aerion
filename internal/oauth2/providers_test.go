package oauth2

import (
	"strings"
	"testing"
)

func TestGoogleProvider(t *testing.T) {
	p := GoogleProvider()

	if p.Name != "google" {
		t.Errorf("Name = %q, want %q", p.Name, "google")
	}
	if !strings.Contains(p.AuthURL, "google") {
		t.Errorf("AuthURL = %q, expected it to contain 'google'", p.AuthURL)
	}
	if len(p.Scopes) == 0 {
		t.Error("Scopes is empty, want at least one scope")
	}
	// The mail link requests the calendar scope too, so the calendar
	// extension reuses the linked account's grant instead of asking for a
	// separate calendar authorization.
	wantCalendar := "https://www.googleapis.com/auth/calendar"
	found := false
	for _, s := range p.Scopes {
		if s == wantCalendar {
			found = true
		}
	}
	if !found {
		t.Errorf("Scopes = %v, want it to contain %q", p.Scopes, wantCalendar)
	}
}

// TestGoogleProviderAuthURLIncludesCalendarScope verifies the authorization
// URL a new Gmail link opens actually asks Google for the calendar scope
// (a UI pass up to the auth URL — no live Google round-trip).
func TestGoogleProviderAuthURLIncludesCalendarScope(t *testing.T) {
	p := GoogleProvider()
	u := buildAuthURL(p, "test-state", "test-challenge", 8080)
	if !strings.Contains(u, "https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fcalendar") {
		t.Errorf("auth URL = %q, want the scope parameter to include the calendar scope", u)
	}
}

func TestMicrosoftProvider(t *testing.T) {
	p := MicrosoftProvider()

	if p.Name != "microsoft" {
		t.Errorf("Name = %q, want %q", p.Name, "microsoft")
	}
	if !strings.Contains(p.AuthURL, "microsoftonline") {
		t.Errorf("AuthURL = %q, expected it to contain 'microsoftonline'", p.AuthURL)
	}
}

func TestGoogleContactsOnlyProvider(t *testing.T) {
	p := GoogleContactsOnlyProvider()

	if p.Name != "google-contacts" {
		t.Errorf("Name = %q, want %q", p.Name, "google-contacts")
	}
}

func TestGetProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{name: "google", provider: "google", wantErr: false},
		{name: "google-mail", provider: "google-mail", wantErr: false},
		{name: "microsoft", provider: "microsoft", wantErr: false},
		{name: "microsoft-mail", provider: "microsoft-mail", wantErr: false},
		{name: "unknown", provider: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GetProvider(tt.provider)
			if tt.wantErr && err == nil {
				t.Errorf("GetProvider(%q) = nil error, want error", tt.provider)
				return
			}
			if !tt.wantErr && err != nil {
				t.Errorf("GetProvider(%q) returned error: %v", tt.provider, err)
			}
		})
	}
}

func TestSupportedProviders(t *testing.T) {
	providers := SupportedProviders()

	if len(providers) != 2 {
		t.Fatalf("SupportedProviders() returned %d providers, want 2", len(providers))
	}

	want := map[string]bool{"google": true, "microsoft": true}
	for _, p := range providers {
		if !want[p] {
			t.Errorf("unexpected provider %q in SupportedProviders()", p)
		}
	}
}
