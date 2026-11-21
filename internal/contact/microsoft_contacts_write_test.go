package contact

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGraphServer answers Graph calls for one known contact. A $filter
// mentioning that email returns one row; anything else returns none.
// Creates are recorded so the test can count them.
type fakeGraphServer struct {
	t          *testing.T
	knownEmail string
	ownerEmail string
	creates    int
	lastCreate map[string]any
	failSearch bool
	failCreate bool
	failMe     bool
}

func (f *fakeGraphServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/me/contacts") && r.Method == http.MethodGet:
		if f.failSearch {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		var value []map[string]string
		if strings.Contains(r.URL.Query().Get("$filter"), f.knownEmail) {
			value = []map[string]string{{"id": "contact-1"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
	case r.URL.Path == "/me/contacts" && r.Method == http.MethodPost:
		if f.failCreate {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&f.lastCreate); err != nil {
			f.t.Errorf("decode create body: %v", err)
		}
		f.creates++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"contact-new"}`))
	case r.URL.Path == "/me" && r.Method == http.MethodGet:
		if f.failMe {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"mail": f.ownerEmail})
	default:
		f.t.Errorf("unexpected Graph request: %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

func newTestMicrosoftSyncer(t *testing.T, fake *fakeGraphServer) *MicrosoftContactsSyncer {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	s := NewMicrosoftContactsSyncer()
	s.SetGraphBaseURL(srv.URL)
	return s
}

func TestMicrosoftContactExists(t *testing.T) {
	fake := &fakeGraphServer{t: t, knownEmail: "known@example.com", ownerEmail: "me@outlook.com"}
	s := newTestMicrosoftSyncer(t, fake)

	exists, err := s.ContactExists("tok", "known@example.com")
	if err != nil {
		t.Fatalf("ContactExists: %v", err)
	}
	if !exists {
		t.Error("ContactExists(known) = false, want true")
	}

	exists, err = s.ContactExists("tok", "new@example.com")
	if err != nil {
		t.Fatalf("ContactExists: %v", err)
	}
	if exists {
		t.Error("ContactExists(unknown) = true, want false")
	}
}

func TestMicrosoftContactExistsServerError(t *testing.T) {
	fake := &fakeGraphServer{t: t, knownEmail: "known@example.com", failSearch: true}
	s := newTestMicrosoftSyncer(t, fake)
	if _, err := s.ContactExists("tok", "new@example.com"); err == nil {
		t.Error("ContactExists on 500: want error, got nil")
	}
}

func TestMicrosoftCreateContact(t *testing.T) {
	fake := &fakeGraphServer{t: t, knownEmail: "known@example.com", ownerEmail: "me@outlook.com"}
	s := newTestMicrosoftSyncer(t, fake)

	if err := s.CreateContact("tok", "John Smith", "new@example.com"); err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if fake.creates != 1 {
		t.Fatalf("creates = %d, want 1", fake.creates)
	}
	if fake.lastCreate["displayName"] != "John Smith" {
		t.Errorf("create displayName = %v, want John Smith", fake.lastCreate["displayName"])
	}
	emails, ok := fake.lastCreate["emailAddresses"].([]any)
	if !ok || len(emails) != 1 {
		t.Fatalf("create emailAddresses = %v, want one entry", fake.lastCreate["emailAddresses"])
	}
	entry, ok := emails[0].(map[string]any)
	if !ok || entry["address"] != "new@example.com" {
		t.Errorf("create email address = %v, want new@example.com", emails[0])
	}

	if err := s.CreateContact("", "Name", "x@example.com"); err == nil {
		t.Error("CreateContact(empty token): want error, got nil")
	}
}

func TestMicrosoftCreateContactForbidden(t *testing.T) {
	fake := &fakeGraphServer{t: t, knownEmail: "known@example.com", failCreate: true}
	s := newTestMicrosoftSyncer(t, fake)
	if err := s.CreateContact("tok", "Name", "new@example.com"); err == nil {
		t.Error("CreateContact on 403: want error, got nil")
	}
}

func TestMicrosoftGetMyEmail(t *testing.T) {
	fake := &fakeGraphServer{t: t, ownerEmail: "me@outlook.com"}
	s := newTestMicrosoftSyncer(t, fake)

	email, err := s.GetMyEmail("tok")
	if err != nil {
		t.Fatalf("GetMyEmail: %v", err)
	}
	if email != "me@outlook.com" {
		t.Errorf("GetMyEmail = %q, want me@outlook.com", email)
	}

	if _, err := s.GetMyEmail(""); err == nil {
		t.Error("GetMyEmail(empty token): want error, got nil")
	}
}
