package contact

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakePeopleServer answers People API calls for one known contact. Any
// search query containing that email returns a hit; anything else returns
// no results. Creates are recorded so the test can count them.
type fakePeopleServer struct {
	t           *testing.T
	knownEmail  string
	creates     int
	lastCreate  googleCreateContactRequest
	searchCalls int
	failSearch  bool
	failCreate  bool
}

func (f *fakePeopleServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/v1/people:searchContacts"):
		f.searchCalls++
		if f.failSearch {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		var results []googleSearchResult
		if strings.Contains(r.URL.Query().Get("query"), f.knownEmail) {
			results = []googleSearchResult{{Person: googlePerson{
				EmailAddresses: []googleEmail{{Value: f.knownEmail}},
			}}}
		}
		_ = json.NewEncoder(w).Encode(googleSearchResponse{Results: results})
	case r.URL.Path == "/v1/people:createContact" && r.Method == http.MethodPost:
		if f.failCreate {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Reset first: Decode leaves absent fields untouched, so a
		// previous create's names would otherwise survive an email-only
		// create.
		f.lastCreate = googleCreateContactRequest{}
		if err := json.NewDecoder(r.Body).Decode(&f.lastCreate); err != nil {
			f.t.Errorf("decode create body: %v", err)
		}
		f.creates++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	default:
		f.t.Errorf("unexpected People request: %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

func newTestGoogleClient(t *testing.T, fake *fakePeopleServer) *GoogleContactsClient {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	c := NewGoogleContactsClient()
	c.SetBaseURL(srv.URL)
	return c
}

func TestGoogleContactExists(t *testing.T) {
	fake := &fakePeopleServer{t: t, knownEmail: "known@example.com"}
	c := newTestGoogleClient(t, fake)

	exists, err := c.ContactExists("tok", "known@example.com")
	if err != nil {
		t.Fatalf("ContactExists: %v", err)
	}
	if !exists {
		t.Error("ContactExists(known) = false, want true")
	}

	exists, err = c.ContactExists("tok", "new@example.com")
	if err != nil {
		t.Fatalf("ContactExists: %v", err)
	}
	if exists {
		t.Error("ContactExists(unknown) = true, want false")
	}

	// Empty email / token short-circuit without a request.
	if _, err := c.ContactExists("tok", ""); err != nil {
		t.Errorf("ContactExists(empty email): %v", err)
	}
	if _, err := c.ContactExists("", "known@example.com"); err != nil {
		t.Errorf("ContactExists(empty token): %v", err)
	}
	if fake.searchCalls != 2 {
		t.Errorf("search calls = %d, want 2 (empty inputs must not hit the API)", fake.searchCalls)
	}
}

func TestGoogleContactExistsServerError(t *testing.T) {
	fake := &fakePeopleServer{t: t, knownEmail: "known@example.com", failSearch: true}
	c := newTestGoogleClient(t, fake)
	if _, err := c.ContactExists("tok", "new@example.com"); err == nil {
		t.Error("ContactExists on 500: want error, got nil")
	}
}

func TestGoogleCreateContact(t *testing.T) {
	fake := &fakePeopleServer{t: t, knownEmail: "known@example.com"}
	c := newTestGoogleClient(t, fake)

	if err := c.CreateContact("tok", "Mary Jane Watson", "new@example.com"); err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if fake.creates != 1 {
		t.Fatalf("creates = %d, want 1", fake.creates)
	}
	if len(fake.lastCreate.EmailAddresses) != 1 || fake.lastCreate.EmailAddresses[0].Value != "new@example.com" {
		t.Errorf("create email = %+v, want new@example.com", fake.lastCreate.EmailAddresses)
	}
	if len(fake.lastCreate.Names) != 1 {
		t.Fatalf("create names = %+v, want one entry", fake.lastCreate.Names)
	}
	if fake.lastCreate.Names[0].GivenName != "Mary Jane" || fake.lastCreate.Names[0].FamilyName != "Watson" {
		t.Errorf("create name = %+v, want Mary Jane / Watson", fake.lastCreate.Names[0])
	}

	// Email-only contact: no names[] entry.
	if err := c.CreateContact("tok", "", "plain@example.com"); err != nil {
		t.Fatalf("CreateContact(email-only): %v", err)
	}
	if len(fake.lastCreate.Names) != 0 {
		t.Errorf("email-only create names = %+v, want none", fake.lastCreate.Names)
	}

	if err := c.CreateContact("", "Name", "x@example.com"); err == nil {
		t.Error("CreateContact(empty token): want error, got nil")
	}
}

func TestGoogleCreateContactForbidden(t *testing.T) {
	fake := &fakePeopleServer{t: t, knownEmail: "known@example.com", failCreate: true}
	c := newTestGoogleClient(t, fake)
	if err := c.CreateContact("tok", "Name", "new@example.com"); err == nil {
		t.Error("CreateContact on 403: want error, got nil")
	}
}
