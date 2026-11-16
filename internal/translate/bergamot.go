package translate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Bergamot model management (Tier 2).
//
// Nothing translation-related is bundled with the app: the engine
// (bergamot-translator.wasm + bergamot-translator.js, release v0.6.0,
// MPL-2.0) and the language models are all optional on-demand
// downloads, cached under <data>/bergamot/ (runtime/ for the engine,
// models/<from>-<to>/<version>/ for the pairs) and served to the
// webview by the asset handler in main.go. The engine downloads the
// first time offline translation is used (or from Settings →
// Translation), and stays cached until removed there.
//
// The language models come from Mozilla's Remote Settings collection
// "translations-models" on first need, are checked against the SHA-256
// in the record, and live under <data>/bergamot/models/<from>-<to>/<version>/.
// One pair is three files: model (.bin, int8 weights), lex (lexical
// shortlist) and vocab (sentencepiece vocabulary).
//
// The engine WASM comes from the "translations-wasm" collection's
// attachment CDN (record version 3.0, release v0.6.0) and the loader
// from the Firefox repo at the pinned commit; both are SHA-256
// verified on download (hashes below were verified live Nov 15, 2026).
//
// Spike result (Nov 15, 2026, verified live): the v0.6.0 runtime
// (WASM record 3.0) + the de→en v2.0 model translate German to English
// correctly in Node in ~2s. The WASM build is single-threaded and
// synchronous — the loader defines a global loadBergamot(Module) and
// takes the WASM bytes directly, with no SharedArrayBuffer, no threads
// and no network/DOM access — so it can run in the Wails webview the
// same way the spellcheck worker already fetches its dictionaries.
// Urdu IS in the catalogue (ur↔en, v2.0), contrary to the plan's
// assumption that its support was limited.

const (
	// BergamotRecordsURL is Mozilla's Remote Settings collection of
	// published translation models.
	BergamotRecordsURL = "https://firefox.settings.services.mozilla.com/v1/buckets/main/collections/translations-models/records"
	// BergamotAttachmentsBase is the CDN the model files are served from.
	BergamotAttachmentsBase = "https://firefox-settings-attachments.cdn.mozilla.net/"
	// BergamotRuntimeVersion is the WASM record version the v0.6.0
	// runtime pairs with.
	BergamotRuntimeVersion = "3.0"
	// BergamotModelVersion is the newest model major version the v0.6.0
	// runtime can run. 1.x models also exist; 2.0 is preferred.
	BergamotModelVersion = "2.0"
	// BergamotRecordsMaxAge is how long a downloaded records.json is
	// trusted before it is fetched again.
	BergamotRecordsMaxAge = 24 * time.Hour
	// BergamotEngineVersion is the engine release the downloads below
	// belong to (Firefox Translations v0.6.0, WASM record 3.0).
	BergamotEngineVersion = "v0.6.0"
	// BergamotEngineWASMURL is the engine WASM on Mozilla's attachment
	// CDN (translations-wasm collection, record
	// 05082c31-aee8-4249-9e01-c1865afd7520). Verified live Nov 15, 2026:
	// 4,960,506 bytes, SHA-256 below.
	BergamotEngineWASMURL = "https://firefox-settings-attachments.cdn.mozilla.net/main-workspace/translations-wasm/05082c31-aee8-4249-9e01-c1865afd7520.wasm"
	// BergamotEngineWASMSHA256 is the SHA-256 of the WASM download.
	BergamotEngineWASMSHA256 = "a3a89d9ad0a4ed8f27bf3e403701b23f5709816f6376438503f2fa5b0182c2dc"
	// BergamotEngineWASMSize is the WASM download size in bytes.
	BergamotEngineWASMSize = 4960506
	// BergamotEngineWASMFile is the file name the WASM is cached under.
	BergamotEngineWASMFile = "bergamot-translator.wasm"
	// BergamotEngineGlueURL is the matching loader from the Firefox
	// repo at the pinned commit (toolkit/components/translations/
	// bergamot-translator/bergamot-translator.js). Verified live Nov 15,
	// 2026: 100,187 bytes, SHA-256 below.
	BergamotEngineGlueURL = "https://raw.githubusercontent.com/mozilla-firefox/firefox/48d55cf7ec80093903e2ef7f58b61a84a22ef716/toolkit/components/translations/bergamot-translator/bergamot-translator.js"
	// BergamotEngineGlueSHA256 is the SHA-256 of the loader download.
	BergamotEngineGlueSHA256 = "faff1ef6285b0d26f01787776fd49299dfb756ecb9688aa990c250e66797b47d"
	// BergamotEngineGlueSize is the loader download size in bytes.
	BergamotEngineGlueSize = 100187
	// BergamotEngineGlueFile is the file name the loader is cached under.
	BergamotEngineGlueFile = "bergamot-translator.js"
)

// bergamotRoles are the three file roles of one pair, in the order the
// engine loads them.
var bergamotRoles = []string{"model", "lex", "vocab"}

// ModelRecord is one file record from the translations-models
// collection.
type ModelRecord struct {
	FromLang string
	ToLang   string
	Version  string
	FileType string // "model" | "lex" | "vocab"
	Name     string
	SHA256   string
	Size     int64
	Location string
}

// DownloadedPair is one pair present on disk.
type DownloadedPair struct {
	FromLang string `json:"fromLang"`
	ToLang   string `json:"toLang"`
	Version  string `json:"version"`
	// SizeBytes is the on-disk total of the pair's files.
	SizeBytes int64 `json:"sizeBytes"`
	// Files maps role ("model", "lex", "vocab") to file name.
	Files map[string]string `json:"files"`
}

// PairStatus says whether a pair is ready for the engine.
type PairStatus struct {
	Available bool `json:"available"`
	// Supported is false when the catalogue has no such pair at all.
	Supported bool   `json:"supported"`
	Version   string `json:"version,omitempty"`
	// SizeBytes is the download size when not available, the on-disk
	// size when available.
	SizeBytes int64 `json:"sizeBytes"`
	// Missing lists the roles not yet on disk.
	Missing []string `json:"missing,omitempty"`
}

// ModelsDir returns the directory downloaded models live in.
func ModelsDir(dataDir string) string {
	return filepath.Join(dataDir, "bergamot", "models")
}

// pairDir returns the directory for one pair + version.
func pairDir(dataDir, from, to, version string) string {
	return filepath.Join(ModelsDir(dataDir), from+"-"+to, version)
}

// recordsPath is where the fetched model catalogue is cached.
func recordsPath(dataDir string) string {
	return filepath.Join(ModelsDir(dataDir), "records.json")
}

// FetchRegistry downloads the model catalogue (or reads the cached
// copy when it is fresh) and returns the file records for the
// preferred model version, newest first per pair.
func FetchRegistry(ctx context.Context, dataDir string, client *http.Client) ([]ModelRecord, error) {
	if client == nil {
		client = http.DefaultClient
	}
	raw, err := registryBytes(ctx, dataDir, client)
	if err != nil {
		return nil, err
	}
	return parseRegistry(raw)
}

// registryBytes returns the catalogue JSON, from cache when fresh.
func registryBytes(ctx context.Context, dataDir string, client *http.Client) ([]byte, error) {
	if b, err := os.ReadFile(recordsPath(dataDir)); err == nil {
		if st, err := os.Stat(recordsPath(dataDir)); err == nil && time.Since(st.ModTime()) < BergamotRecordsMaxAge {
			return b, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BergamotRecordsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Bergamot model catalogue: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Bergamot model catalogue returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read Bergamot model catalogue: %w", err)
	}
	if err := os.MkdirAll(ModelsDir(dataDir), 0o755); err != nil {
		return nil, err
	}
	// Cache the catalogue even if parsing fails below: a corrupt cache
	// is overwritten on the next fetch once it goes stale.
	_ = os.WriteFile(recordsPath(dataDir), raw, 0o644)
	return raw, nil
}

type registryResponse struct {
	Data []struct {
		FromLang   string `json:"fromLang"`
		ToLang     string `json:"toLang"`
		Version    string `json:"version"`
		FileType   string `json:"fileType"`
		Name       string `json:"name"`
		Attachment struct {
			Hash     string `json:"hash"`
			Size     int64  `json:"size"`
			Location string `json:"location"`
		} `json:"attachment"`
	} `json:"data"`
}

// parseRegistry keeps the model/lex/vocab records at the preferred
// model version, falling back to 1.x when a pair has no 2.0 files.
func parseRegistry(raw []byte) ([]ModelRecord, error) {
	var resp registryResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse Bergamot model catalogue: %w", err)
	}
	// Group by pair+role, keeping the highest version per group, but
	// never above the runtime's supported major version.
	best := map[string]ModelRecord{}
	for _, r := range resp.Data {
		switch r.FileType {
		case "model", "lex", "vocab":
		default:
			continue
		}
		if majorVersion(r.Version) > 2 {
			continue
		}
		key := r.FromLang + "|" + r.ToLang + "|" + r.FileType
		if prev, ok := best[key]; !ok || compareVersion(r.Version, prev.Version) > 0 {
			best[key] = ModelRecord{
				FromLang: r.FromLang, ToLang: r.ToLang, Version: r.Version,
				FileType: r.FileType, Name: r.Name,
				SHA256: r.Attachment.Hash, Size: r.Attachment.Size,
				Location: r.Attachment.Location,
			}
		}
	}
	out := make([]ModelRecord, 0, len(best))
	for _, r := range best {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FromLang != out[j].FromLang {
			return out[i].FromLang < out[j].FromLang
		}
		if out[i].ToLang != out[j].ToLang {
			return out[i].ToLang < out[j].ToLang
		}
		return out[i].FileType < out[j].FileType
	})
	return out, nil
}

func majorVersion(v string) int {
	n := 0
	_, _ = fmt.Sscanf(v, "%d", &n)
	return n
}

func compareVersion(a, b string) int {
	pa := strings.SplitN(a, ".", 2)
	pb := strings.SplitN(b, ".", 2)
	na, nb := 0, 0
	_, _ = fmt.Sscanf(pa[0], "%d", &na)
	_, _ = fmt.Sscanf(pb[0], "%d", &nb)
	if na != nb {
		if na < nb {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// PairRecords returns the file records for one pair, or an error when
// the catalogue has no such pair.
func PairRecords(records []ModelRecord, from, to string) ([]ModelRecord, error) {
	var out []ModelRecord
	version := ""
	for _, r := range records {
		if r.FromLang == from && r.ToLang == to {
			if version == "" {
				version = r.Version
			}
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no Bergamot model for %s → %s", from, to)
	}
	if len(out) != len(bergamotRoles) {
		return nil, fmt.Errorf("Bergamot model for %s → %s is incomplete in the catalogue (%d files)", from, to, len(out))
	}
	return out, nil
}

// CheckPairStatus checks one pair (or one pivot leg) on disk: every file
// present at the recorded size. The version is the newest downloaded
// one, or the catalogue version when nothing is downloaded yet.
func CheckPairStatus(dataDir string, records []ModelRecord, from, to string) PairStatus {
	st := PairStatus{}
	recs, err := PairRecords(records, from, to)
	if err != nil {
		return st // Supported stays false: no such pair in the catalogue.
	}
	st.Supported = true
	st.Version = recs[0].Version
	for _, r := range recs {
		st.SizeBytes += r.Size
	}
	// Newest downloaded version wins over the catalogue version: an
	// older pair on disk is still usable.
	downloadedVersion := ""
	entries, _ := os.ReadDir(filepath.Join(ModelsDir(dataDir), from+"-"+to))
	for _, e := range entries {
		if e.IsDir() && compareVersion(e.Name(), downloadedVersion) > 0 {
			downloadedVersion = e.Name()
		}
	}
	if downloadedVersion != "" {
		st.Version = downloadedVersion
	}
	dir := pairDir(dataDir, from, to, st.Version)
	files, _ := os.ReadDir(dir)
	present := map[string]int64{}
	for _, e := range files {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		for _, role := range bergamotRoles {
			if strings.HasPrefix(e.Name(), role+".") || strings.Contains(e.Name(), "."+role+".") {
				present[role] = fi.Size()
			}
		}
	}
	var total int64
	for _, role := range bergamotRoles {
		size, ok := present[role]
		if !ok || size == 0 {
			st.Missing = append(st.Missing, role)
			continue
		}
		total += size
	}
	if len(st.Missing) == 0 {
		st.Available = true
		st.SizeBytes = total
		return st
	}
	// Partial download: report what is on disk, and keep the catalogue
	// size as the download size when nothing is there yet.
	if total == 0 {
		return st
	}
	st.SizeBytes = total
	return st
}

// DownloadPair downloads one pair (model, lex, vocab) with SHA-256
// verification. Files land in a temp directory and are renamed into
// place only when all three verify, so a partial download is never
// mistaken for a ready pair. Progress reports downloaded/total bytes
// across the three files; it may be nil.
func DownloadPair(ctx context.Context, dataDir string, records []ModelRecord, from, to string, client *http.Client, progress func(downloaded, total int64)) error {
	if client == nil {
		client = http.DefaultClient
	}
	recs, err := PairRecords(records, from, to)
	if err != nil {
		return err
	}
	var total int64
	for _, r := range recs {
		total += r.Size
	}
	if err := os.MkdirAll(ModelsDir(dataDir), 0o755); err != nil {
		return fmt.Errorf("failed to stage Bergamot download: %w", err)
	}
	tmpDir, err := os.MkdirTemp(ModelsDir(dataDir), ".download-"+from+"-"+to+"-")
	if err != nil {
		return fmt.Errorf("failed to stage Bergamot download: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	var downloaded int64
	report := func() {
		if progress != nil {
			progress(downloaded, total)
		}
	}
	for _, r := range recs {
		if err := downloadFile(ctx, client, BergamotAttachmentsBase+r.Location, filepath.Join(tmpDir, r.Name), r.SHA256, r.Size, func(n int64) {
			downloaded += n
			report()
		}); err != nil {
			return fmt.Errorf("failed to download Bergamot %s → %s %s: %w", from, to, r.FileType, err)
		}
	}
	dest := pairDir(dataDir, from, to, recs[0].Version)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	// Replace any earlier version of the pair wholesale.
	_ = os.RemoveAll(dest)
	if err := os.Rename(tmpDir, dest); err != nil {
		return fmt.Errorf("failed to install Bergamot %s → %s: %w", from, to, err)
	}
	return nil
}

// downloadFile fetches one URL to a file, checking size and SHA-256.
func downloadFile(ctx context.Context, client *http.Client, url, dest, wantSHA256 string, wantSize int64, onChunk func(n int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	var written int64
	buf := make([]byte, 128*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			h.Write(buf[:n])
			written += int64(n)
			if onChunk != nil {
				onChunk(int64(n))
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return rerr
		}
	}
	if wantSize > 0 && written != wantSize {
		return fmt.Errorf("size mismatch: got %d bytes, want %d", written, wantSize)
	}
	if wantSHA256 != "" && hex.EncodeToString(h.Sum(nil)) != strings.ToLower(wantSHA256) {
		return fmt.Errorf("SHA-256 mismatch for %s", dest)
	}
	return nil
}

// ListDownloadedPairs lists every pair on disk, newest version first.
func ListDownloadedPairs(dataDir string) ([]DownloadedPair, error) {
	base := ModelsDir(dataDir)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return []DownloadedPair{}, nil
		}
		return nil, err
	}
	var out []DownloadedPair
	for _, pairEntry := range entries {
		if !pairEntry.IsDir() {
			continue
		}
		from, to, ok := strings.Cut(pairEntry.Name(), "-")
		if !ok {
			continue
		}
		versions, err := os.ReadDir(filepath.Join(base, pairEntry.Name()))
		if err != nil {
			continue
		}
		// Newest version only.
		version := ""
		for _, v := range versions {
			if v.IsDir() && compareVersion(v.Name(), version) > 0 {
				version = v.Name()
			}
		}
		if version == "" {
			continue
		}
		dir := filepath.Join(base, pairEntry.Name(), version)
		files := map[string]string{}
		var total int64
		for _, e := range mustReadDir(dir) {
			if e.IsDir() {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			total += fi.Size()
			for _, role := range bergamotRoles {
				if strings.Contains(e.Name(), "."+role+".") || strings.HasPrefix(e.Name(), role+".") {
					files[role] = e.Name()
				}
			}
		}
		if len(files) == 0 {
			continue
		}
		out = append(out, DownloadedPair{FromLang: from, ToLang: to, Version: version, SizeBytes: total, Files: files})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FromLang != out[j].FromLang {
			return out[i].FromLang < out[j].FromLang
		}
		return out[i].ToLang < out[j].ToLang
	})
	return out, nil
}

func mustReadDir(dir string) []os.DirEntry {
	entries, _ := os.ReadDir(dir)
	return entries
}

// DeletePair removes one pair from disk (all versions).
func DeletePair(dataDir, from, to string) error {
	dir := filepath.Join(ModelsDir(dataDir), from+"-"+to)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete Bergamot %s → %s: %w", from, to, err)
	}
	return nil
}

// TotalModelsSize returns the on-disk total of all downloaded models.
func TotalModelsSize(dataDir string) (int64, error) {
	pairs, err := ListDownloadedPairs(dataDir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, p := range pairs {
		total += p.SizeBytes
	}
	return total, nil
}

// ModelFilePath returns the on-disk path of one role file of a
// downloaded pair, for the asset handler that serves models to the
// webview. Empty string when the file is not there.
func ModelFilePath(dataDir, from, to, role string) string {
	pairs, err := ListDownloadedPairs(dataDir)
	if err != nil {
		return ""
	}
	for _, p := range pairs {
		if p.FromLang == from && p.ToLang == to {
			name, ok := p.Files[role]
			if !ok {
				return ""
			}
			path := filepath.Join(pairDir(dataDir, from, to, p.Version), name)
			if _, err := os.Stat(path); err != nil {
				return ""
			}
			return path
		}
	}
	return ""
}

// SupportedPairs lists every from→to pair the catalogue offers at the
// preferred version.
func SupportedPairs(records []ModelRecord) [][2]string {
	seen := map[[2]string]bool{}
	var out [][2]string
	for _, r := range records {
		p := [2]string{r.FromLang, r.ToLang}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][0] != out[j][0] {
			return out[i][0] < out[j][0]
		}
		return out[i][1] < out[j][1]
	})
	return out
}

// ============================================================================
// Bergamot engine (WASM runtime) - on-demand download, cached on disk.
// ============================================================================

// EngineStatus says whether the offline engine is ready to run.
type EngineStatus struct {
	Available bool `json:"available"`
	Version   string `json:"version,omitempty"`
	// SizeBytes is the total download size of the engine files.
	SizeBytes int64 `json:"sizeBytes"`
	// DownloadedBytes is how much of the engine is already on disk.
	DownloadedBytes int64 `json:"downloadedBytes"`
}

// RuntimeDir returns the directory the downloaded engine lives in.
func RuntimeDir(dataDir string) string {
	return filepath.Join(dataDir, "bergamot", "runtime")
}

// engineTotalSize is the combined download size of the WASM + loader.
func engineTotalSize() int64 {
	return int64(BergamotEngineWASMSize + BergamotEngineGlueSize)
}

// CheckEngineStatus checks the cached engine: both files present at
// their exact sizes. A file with the wrong size is treated as missing
// (a corrupt or partial download is re-downloaded, never run).
func CheckEngineStatus(dataDir string) EngineStatus {
	st := EngineStatus{Version: BergamotEngineVersion, SizeBytes: engineTotalSize()}
	dir := RuntimeDir(dataDir)
	ready := true
	for _, f := range []struct {
		name string
		size int64
	}{
		{BergamotEngineWASMFile, BergamotEngineWASMSize},
		{BergamotEngineGlueFile, BergamotEngineGlueSize},
	} {
		fi, err := os.Stat(filepath.Join(dir, f.name))
		if err != nil || fi.Size() != f.size {
			ready = false
			continue
		}
		st.DownloadedBytes += fi.Size()
	}
	st.Available = ready
	return st
}

// DownloadEngine downloads the WASM + loader with SHA-256 verification.
// Files land in a temp directory and are renamed into place only when
// both verify, so a partial download is never mistaken for a ready
// engine. Progress reports downloaded/total bytes across both files;
// it may be nil.
func DownloadEngine(ctx context.Context, dataDir string, client *http.Client, progress func(downloaded, total int64)) error {
	if client == nil {
		client = http.DefaultClient
	}
	total := engineTotalSize()
	base := filepath.Join(dataDir, "bergamot")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return fmt.Errorf("failed to stage Bergamot engine download: %w", err)
	}
	tmpDir, err := os.MkdirTemp(base, ".download-runtime-")
	if err != nil {
		return fmt.Errorf("failed to stage Bergamot engine download: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	var downloaded int64
	files := []struct {
		url, name, sha string
		size           int64
	}{
		{BergamotEngineWASMURL, BergamotEngineWASMFile, BergamotEngineWASMSHA256, BergamotEngineWASMSize},
		{BergamotEngineGlueURL, BergamotEngineGlueFile, BergamotEngineGlueSHA256, BergamotEngineGlueSize},
	}
	for _, f := range files {
		if err := downloadFile(ctx, client, f.url, filepath.Join(tmpDir, f.name), f.sha, f.size, func(n int64) {
			downloaded += n
			if progress != nil {
				progress(downloaded, total)
			}
		}); err != nil {
			return fmt.Errorf("failed to download Bergamot engine %s: %w", f.name, err)
		}
	}
	dest := RuntimeDir(dataDir)
	// Replace any earlier engine wholesale.
	_ = os.RemoveAll(dest)
	if err := os.Rename(tmpDir, dest); err != nil {
		return fmt.Errorf("failed to install Bergamot engine: %w", err)
	}
	return nil
}

// DeleteEngine removes the cached engine from disk (all files).
func DeleteEngine(dataDir string) error {
	if err := os.RemoveAll(RuntimeDir(dataDir)); err != nil {
		return fmt.Errorf("failed to delete Bergamot engine: %w", err)
	}
	return nil
}

// ============================================================================
// Per-language catalogue - the download manager's list.
// ============================================================================

// BergamotLanguage is one row of the Settings → Translation download
// manager: a language with an offline model in at least one direction.
// A language is only listed when the live catalogue actually has a
// model for it — languages with no model (Punjabi, Swahili, Nepali and
// the rest) never appear, and one-direction languages (Japanese,
// Maltese, Norwegian Nynorsk and Chinese Simplified are English-bound
// only at the v0.6.0 runtime's model version) say so.
type BergamotLanguage struct {
	Code string `json:"code"`
	Name string `json:"name"`
	// SizeBytes is the download size of the directions that exist.
	SizeBytes int64 `json:"sizeBytes"`
	// Downloaded is true when every existing direction is downloaded.
	Downloaded bool `json:"downloaded"`
	// DownloadedSize is the on-disk size of the downloaded directions.
	DownloadedSize int64 `json:"downloadedSize"`
	// ToEnglish is true when an X→en model exists.
	ToEnglish bool `json:"toEnglish"`
	// FromEnglish is true when an en→X model exists.
	FromEnglish bool `json:"fromEnglish"`
}

// BergamotLanguageName returns the display name for a catalogue
// language code. Chinese keeps its script tag: the two are distinct
// models with different coverage.
func BergamotLanguageName(code string) string {
	switch code {
	case "zh-Hans":
		return "Chinese (Simplified)"
	case "zh-Hant":
		return "Chinese (Traditional)"
	default:
		return LanguageName(code)
	}
}

// ListBergamotLanguages groups the catalogue's pairs by the
// non-English side, with live download sizes and downloaded state
// from disk. Sorted by name.
func ListBergamotLanguages(dataDir string, records []ModelRecord) []BergamotLanguage {
	byCode := map[string]*BergamotLanguage{}
	directions := map[string]int{}
	downloadedDirs := map[string]int{}
	for _, p := range SupportedPairs(records) {
		from, to := p[0], p[1]
		var code string
		var toEn bool
		switch {
		case from == "en":
			code, toEn = to, false
		case to == "en":
			code, toEn = from, true
		default:
			continue // Bergamot pairs always touch English.
		}
		// Only a complete pair (model, lex, vocab in the catalogue)
		// counts as a direction: en→ja, for example, has no vocab
		// file, so it must not be listed as downloadable.
		recs, err := PairRecords(records, from, to)
		if err != nil {
			continue
		}
		e, ok := byCode[code]
		if !ok {
			e = &BergamotLanguage{Code: code, Name: BergamotLanguageName(code)}
			byCode[code] = e
		}
		if toEn {
			e.ToEnglish = true
		} else {
			e.FromEnglish = true
		}
		directions[code]++
		for _, r := range recs {
			e.SizeBytes += r.Size
		}
		if st := CheckPairStatus(dataDir, records, from, to); st.Available {
			downloadedDirs[code]++
			e.DownloadedSize += st.SizeBytes
		}
	}
	out := make([]BergamotLanguage, 0, len(byCode))
	for code, e := range byCode {
		e.Downloaded = directions[code] > 0 && downloadedDirs[code] == directions[code]
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// DownloadLanguage downloads every direction the catalogue has for a
// language (both, when both exist). Progress reports per leg; it may
// be nil.
func DownloadLanguage(ctx context.Context, dataDir string, records []ModelRecord, lang string, client *http.Client, progress func(from, to string, downloaded, total int64)) error {
	route := BergamotLanguageRoute(records, lang)
	if len(route) == 0 {
		return fmt.Errorf("no Bergamot model for %s", lang)
	}
	for _, leg := range route {
		if err := DownloadPair(ctx, dataDir, records, leg[0], leg[1], client, func(downloaded, total int64) {
			if progress != nil {
				progress(leg[0], leg[1], downloaded, total)
			}
		}); err != nil {
			return err
		}
	}
	return nil
}

// BergamotLanguageRoute returns the directions the catalogue has for a
// language: X→en and/or en→X. Only complete pairs count. Empty when
// the catalogue has no model for the language at all.
func BergamotLanguageRoute(records []ModelRecord, lang string) [][2]string {
	var route [][2]string
	for _, p := range SupportedPairs(records) {
		if (p[0] == lang && p[1] == "en") || (p[0] == "en" && p[1] == lang) {
			if _, err := PairRecords(records, p[0], p[1]); err != nil {
				continue
			}
			route = append(route, p)
		}
	}
	return route
}

// DeleteLanguage removes every downloaded direction of a language.
func DeleteLanguage(dataDir, lang string) error {
	for _, leg := range [][2]string{{lang, "en"}, {"en", lang}} {
		if err := DeletePair(dataDir, leg[0], leg[1]); err != nil {
			return err
		}
	}
	return nil
}

// downloadedPairSize returns the on-disk size of a downloaded pair, or
// ok=false when the pair is not fully on disk.
func downloadedPairSize(dataDir, from, to string) (size int64, ok bool) {
	pairs, err := ListDownloadedPairs(dataDir)
	if err != nil {
		return 0, false
	}
	for _, p := range pairs {
		if p.FromLang != from || p.ToLang != to {
			continue
		}
		for _, role := range bergamotRoles {
			if _, has := p.Files[role]; !has {
				return 0, false
			}
		}
		return p.SizeBytes, true
	}
	return 0, false
}

// fallbackLanguage is one row of the catalogue snapshot below.
type fallbackLanguage struct {
	code                   string
	sizeBytes              int64
	toEnglish, fromEnglish bool
}

// fallbackBergamotLanguages is the catalogue snapshot of Nov 15, 2026:
// every language the live translations-models collection had a complete
// model for at the v0.6.0 runtime's model version, with the
// both-direction download size in bytes. It is only used when the live
// catalogue cannot be fetched (offline): the download manager still
// lists what exists, with the snapshot size, instead of going blank.
// The live catalogue is the authority whenever it can be reached, and
// this snapshot must be refreshed if Mozilla publishes new models.
var fallbackBergamotLanguages = []fallbackLanguage{
	{"af", 72018108, true, true},
	{"ar", 72614092, true, true},
	{"az", 72787314, true, true},
	{"be", 72110965, true, true},
	{"bg", 72400406, true, true},
	{"bn", 44245712, true, true},
	{"bs", 73134389, true, true},
	{"ca", 73448144, true, true},
	{"cs", 73316906, true, true},
	{"da", 44227669, true, true},
	{"de", 74037188, true, true},
	{"el", 43225814, true, true},
	{"es", 73590366, true, true},
	{"et", 72873058, true, true},
	{"eu", 71658894, true, true},
	{"fa", 45798693, true, true},
	{"fi", 73446338, true, true},
	{"fr", 73949438, true, true},
	{"gl", 72152899, true, true},
	{"gu", 43589676, true, true},
	{"he", 43674372, true, true},
	{"hi", 44243702, true, true},
	{"hr", 43055172, true, true},
	{"hu", 73055532, true, true},
	{"id", 43394030, true, true},
	{"is", 72531328, true, true},
	{"it", 73595830, true, true},
	{"ja", 54769181, true, false},
	{"kn", 43876998, true, true},
	{"ko", 105711691, true, true},
	{"lt", 58463052, true, true},
	{"lv", 57907975, true, true},
	{"ml", 44051081, true, true},
	{"mr", 72408652, true, true},
	{"ms", 44195057, true, true},
	{"mt", 20958903, true, false},
	{"nb", 44639107, true, true},
	{"nl", 73632026, true, true},
	{"nn", 14997958, true, false},
	{"pl", 73271058, true, true},
	{"pt", 73361858, true, true},
	{"ro", 44316044, true, true},
	{"ru", 72152848, true, true},
	{"sk", 58589480, true, true},
	{"sl", 72478648, true, true},
	{"sq", 43482514, true, true},
	{"sr", 57618090, true, true},
	{"sv", 44462879, true, true},
	{"ta", 58904271, true, true},
	{"te", 44042635, true, true},
	{"th", 72616188, true, true},
	{"tl", 72485226, true, true},
	{"tr", 43815825, true, true},
	{"ug", 70358262, true, true},
	{"uk", 57479809, true, true},
	{"ur", 71395538, true, true},
	{"vi", 58753156, true, true},
	{"zh-Hans", 54556676, true, false},
}

// FallbackBergamotLanguages builds the per-language list from the
// snapshot, with downloaded state read from disk. Sorted by name.
func FallbackBergamotLanguages(dataDir string) []BergamotLanguage {
	out := make([]BergamotLanguage, 0, len(fallbackBergamotLanguages))
	for _, f := range fallbackBergamotLanguages {
		e := BergamotLanguage{
			Code: f.code, Name: BergamotLanguageName(f.code),
			SizeBytes: f.sizeBytes, ToEnglish: f.toEnglish, FromEnglish: f.fromEnglish,
		}
		dirs := 0
		done := 0
		for _, leg := range [][2]string{{f.code, "en"}, {"en", f.code}} {
			if (leg[0] == "en" && !f.fromEnglish) || (leg[1] == "en" && !f.toEnglish) {
				continue
			}
			dirs++
			if size, ok := downloadedPairSize(dataDir, leg[0], leg[1]); ok {
				done++
				e.DownloadedSize += size
			}
		}
		e.Downloaded = dirs > 0 && done == dirs
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Code < out[j].Code
	})
	return out
}
