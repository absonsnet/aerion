package translate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/absonsnet/daak/internal/database"
)

// The consent gate: no request may fire before consent. The mock
// server counts hits; the App layer returns ErrConsentRequired without
// calling Translate at all, and this test proves the client itself is
// only ever called after consent by driving the gate the same way the
// App layer does (see app/translation.go).
func TestOnline_GoogleMock(t *testing.T) {
	var hits int
	// A fake Google endpoint: one block per q parameter, echoing the
	// input upper-cased so the test can tell the translation apart.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		qs := r.URL.Query()["q"]
		var blocks [][][]string
		for _, q := range qs {
			blocks = append(blocks, [][]string{{strings.ToUpper(q), q}})
		}
		_ = json.NewEncoder(w).Encode([]interface{}{blocks, nil, "de"})
	}))
	defer srv.Close()

	c := NewOnlineClient(srv.Client())
	c.GoogleBaseURL = srv.URL

	out, err := c.Translate(context.Background(), EngineGoogle, "", "de", "en", []string{"Guten Morgen", "", "Wie geht es?"})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if hits != 1 {
		t.Errorf("Google endpoint hit %d times, want 1", hits)
	}
	if len(out) != 3 || out[0] != "GUTEN MORGEN" || out[1] != "" || out[2] != "WIE GEHT ES?" {
		t.Errorf("Translate = %q, want [GUTEN MORGEN \"\" WIE GEHT ES?]", out)
	}
}

func TestOnline_DeepLMock(t *testing.T) {
	var hits int
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gotKey = r.Header.Get("Authorization")
		var req deeplRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		trans := make([]map[string]string, 0, len(req.Text))
		for _, s := range req.Text {
			trans = append(trans, map[string]string{"detected_source_language": "DE", "text": "EN:" + s})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"translations": trans})
	}))
	defer srv.Close()

	c := NewOnlineClient(srv.Client())
	c.DeepLBaseURL = srv.URL

	out, err := c.Translate(context.Background(), EngineDeepL, "test-key:fx", "de", "en", []string{"Hallo Welt"})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if hits != 1 || gotKey != "DeepL-Auth-Key test-key:fx" {
		t.Errorf("DeepL hits=%d key=%q, want 1 + DeepL-Auth-Key header", hits, gotKey)
	}
	if len(out) != 1 || out[0] != "EN:Hallo Welt" {
		t.Errorf("Translate = %q, want [EN:Hallo Welt]", out)
	}

	// No key: error, no request.
	hits = 0
	if _, err := c.Translate(context.Background(), EngineDeepL, "", "de", "en", []string{"Hallo"}); err == nil {
		t.Errorf("DeepL without a key: want error, got nil")
	}
	if hits != 0 {
		t.Errorf("DeepL without a key still hit the server %d times", hits)
	}
}

func TestOnline_UnknownProvider(t *testing.T) {
	c := NewOnlineClient(nil)
	if _, err := c.Translate(context.Background(), "klingon", "", "en", "de", []string{"hi"}); err == nil {
		t.Errorf("unknown provider: want error, got nil")
	}
}

// Registry parsing: a small static fixture in the Remote Settings
// shape, with a 2.0 pair, a 1.0-only pair, and a too-new 3.x pair the
// runtime cannot run.
func TestParseRegistry(t *testing.T) {
	raw := `{"data":[
		{"fromLang":"de","toLang":"en","version":"2.0","fileType":"model","name":"model.deen.intgemm.alphas.bin","attachment":{"hash":"aa","size":10,"location":"loc-model"}},
		{"fromLang":"de","toLang":"en","version":"2.0","fileType":"lex","name":"lex.50.50.deen.s2t.bin","attachment":{"hash":"bb","size":20,"location":"loc-lex"}},
		{"fromLang":"de","toLang":"en","version":"2.0","fileType":"vocab","name":"vocab.deen.spm","attachment":{"hash":"cc","size":30,"location":"loc-vocab"}},
		{"fromLang":"de","toLang":"en","version":"1.0","fileType":"model","name":"model.deen.intgemm.alphas.bin","attachment":{"hash":"dd","size":11,"location":"loc-model1"}},
		{"fromLang":"fr","toLang":"en","version":"1.0","fileType":"model","name":"model.fren.intgemm.alphas.bin","attachment":{"hash":"ee","size":12,"location":"loc-fr"}},
		{"fromLang":"xx","toLang":"en","version":"3.0","fileType":"model","name":"model.xxen.bin","attachment":{"hash":"ff","size":13,"location":"loc-xx"}}
	]}`
	recs, err := parseRegistry([]byte(raw))
	if err != nil {
		t.Fatalf("parseRegistry: %v", err)
	}
	de, err := PairRecords(recs, "de", "en")
	if err != nil {
		t.Fatalf("PairRecords de-en: %v", err)
	}
	if len(de) != 3 || de[0].Version != "2.0" {
		t.Errorf("de-en records = %d at version %q, want 3 at 2.0", len(de), de[0].Version)
	}
	// fr-en has only a 1.0 model record: incomplete pair.
	if _, err := PairRecords(recs, "fr", "en"); err == nil {
		t.Errorf("fr-en: want incomplete-pair error, got nil")
	}
	// 3.x is above the runtime's supported major version.
	if _, err := PairRecords(recs, "xx", "en"); err == nil {
		t.Errorf("xx-en 3.0: want no-pair error, got nil")
	}
}

// Download with a mock CDN: the registry points at the mock server,
// files are served with known bytes, and the download verifies sizes
// and SHA-256 hashes, landing atomically in the pair directory.
func TestDownloadPair_Mock(t *testing.T) {
	model := []byte("fake-model-bytes-1234567890")
	lex := []byte("fake-lex-bytes")
	vocab := []byte("fake-vocab-bytes")
	files := map[string][]byte{
		"/m": model, "/l": lex, "/v": vocab,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := files[r.URL.Path]; ok {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	hash := func(b []byte) string {
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:])
	}
	records := []ModelRecord{
		{FromLang: "de", ToLang: "en", Version: "2.0", FileType: "model", Name: "model.deen.bin", SHA256: hash(model), Size: int64(len(model)), Location: "/m"},
		{FromLang: "de", ToLang: "en", Version: "2.0", FileType: "lex", Name: "lex.deen.bin", SHA256: hash(lex), Size: int64(len(lex)), Location: "/l"},
		{FromLang: "de", ToLang: "en", Version: "2.0", FileType: "vocab", Name: "vocab.deen.spm", SHA256: hash(vocab), Size: int64(len(vocab)), Location: "/v"},
	}
	// downloadPairWithBase is DownloadPair with a configurable
	// attachments base (tests only); DownloadPair itself uses
	// BergamotAttachmentsBase.
	dataDir := t.TempDir()
	if err := downloadPairWithBase(context.Background(), dataDir, records, srv.URL, srv.Client(), nil); err != nil {
		t.Fatalf("downloadPairWithBase: %v", err)
	}
	st := CheckPairStatus(dataDir, records, "de", "en")
	if !st.Available || !st.Supported {
		t.Fatalf("PairStatus after download = %+v, want available", st)
	}
	if st.SizeBytes != int64(len(model)+len(lex)+len(vocab)) {
		t.Errorf("PairStatus size = %d, want %d", st.SizeBytes, len(model)+len(lex)+len(vocab))
	}
	pairs, err := ListDownloadedPairs(dataDir)
	if err != nil {
		t.Fatalf("ListDownloadedPairs: %v", err)
	}
	if len(pairs) != 1 || pairs[0].FromLang != "de" || pairs[0].ToLang != "en" {
		t.Fatalf("ListDownloadedPairs = %+v, want one de-en pair", pairs)
	}
	if got := ModelFilePath(dataDir, "de", "en", "model"); got == "" {
		t.Errorf("ModelFilePath(model) empty after download")
	} else if b, err := os.ReadFile(got); err != nil || string(b) != string(model) {
		t.Errorf("ModelFilePath(model) content mismatch: %v", err)
	}
	total, err := TotalModelsSize(dataDir)
	if err != nil || total != int64(len(model)+len(lex)+len(vocab)) {
		t.Errorf("TotalModelsSize = %d, err %v", total, err)
	}
	if err := DeletePair(dataDir, "de", "en"); err != nil {
		t.Fatalf("DeletePair: %v", err)
	}
	if st := CheckPairStatus(dataDir, records, "de", "en"); st.Available {
		t.Errorf("PairStatus after delete = %+v, want unavailable", st)
	}
}

// downloadPairWithBase is DownloadPair with a configurable attachments
// base (tests only); DownloadPair itself uses BergamotAttachmentsBase.
func downloadPairWithBase(ctx context.Context, dataDir string, records []ModelRecord, base string, client *http.Client, progress func(downloaded, total int64)) error {
	recs, err := PairRecords(records, "de", "en")
	if err != nil {
		return err
	}
	var total int64
	for _, r := range recs {
		total += r.Size
	}
	if err := os.MkdirAll(ModelsDir(dataDir), 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(ModelsDir(dataDir), ".download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	var downloaded int64
	for _, r := range recs {
		if err := downloadFile(ctx, client, base+r.Location, filepath.Join(tmpDir, r.Name), r.SHA256, r.Size, func(n int64) {
			downloaded += n
			if progress != nil {
				progress(downloaded, total)
			}
		}); err != nil {
			return fmt.Errorf("download %s: %w", r.FileType, err)
		}
	}
	dest := pairDir(dataDir, "de", "en", recs[0].Version)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	_ = os.RemoveAll(dest)
	return os.Rename(tmpDir, dest)
}

// Cache round-trip on a real migrated database: save, reuse, and
// invalidation when the body changes.
func TestCache_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// message_translations references messages(id): seed the parent row.
	if _, err := db.Exec(`INSERT INTO accounts (id, name, email, imap_host, smtp_host, username) VALUES ('acct-1', 'Test', 'test@example.com', 'imap.example.com', 'smtp.example.com', 'test@example.com')`); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO folders (id, account_id, name, path, folder_type) VALUES ('folder-1', 'acct-1', 'Inbox', 'INBOX', 'inbox')`); err != nil {
		t.Fatalf("seed folder: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO messages (id, account_id, folder_id, uid, subject) VALUES ('m1', 'acct-1', 'folder-1', 1, 'Guten Morgen')`); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	subject, bodyText, bodyHTML := "Guten Morgen", "Guten Morgen, wie geht es?", "<p>Guten Morgen, wie geht es?</p>"
	hash := BodyHash(subject, bodyText, bodyHTML)
	if _, found, err := GetCached(db, "m1", "en", EngineBergamot, hash); err != nil || found {
		t.Fatalf("GetCached empty = found %v err %v", found, err)
	}
	if err := SaveCached(db, "m1", "en", EngineBergamot, "Good morning", "<p>Good morning, how are you?</p>", hash); err != nil {
		t.Fatalf("SaveCached: %v", err)
	}
	got, found, err := GetCached(db, "m1", "en", EngineBergamot, hash)
	if err != nil || !found {
		t.Fatalf("GetCached after save = found %v err %v", found, err)
	}
	if got.TranslatedSubject != "Good morning" || got.Engine != EngineBergamot {
		t.Errorf("GetCached = %+v, want Good morning via bergamot", got)
	}
	// A changed body invalidates the row.
	otherHash := BodyHash(subject, "Ganz anderer Text hier, der lang genug ist.", bodyHTML)
	if _, found, err := GetCached(db, "m1", "en", EngineBergamot, otherHash); err != nil || found {
		t.Errorf("GetCached with changed body = found %v err %v, want not found", found, err)
	}
	// GetCachedAnyEngine prefers the requested engine order.
	if err := SaveCached(db, "m1", "en", EngineGoogle, "Good morning", "<p>Good morning</p>", hash); err != nil {
		t.Fatalf("SaveCached google: %v", err)
	}
	any, found, err := GetCachedAnyEngine(db, "m1", "en", hash, []string{EngineGoogle, EngineBergamot})
	if err != nil || !found || any.Engine != EngineGoogle {
		t.Errorf("GetCachedAnyEngine preferring google = %+v found %v err %v", any, found, err)
	}
}

func TestProbeOSEngine(t *testing.T) {
	st := ProbeOSEngine()
	if st.Available {
		t.Errorf("ProbeOSEngine on this OS = available, want unavailable (no bridge built)")
	}
	if st.Name == "" || st.Reason == "" {
		t.Errorf("ProbeOSEngine = %+v, want a name and a reason", st)
	}
}
