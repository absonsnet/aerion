package mailmerge

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hkdb/aerion/internal/contact"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// ParsedData is the common shape every source produces: one row per
// recipient plus the column list (first-row headers for files, contact
// field names for the contacts adapter).
type ParsedData struct {
	Columns []string            `json:"columns"`
	Rows    []map[string]string `json:"rows"`
}

// ParseCSV parses Thunderbird Mail Merge CSV: first row = headers,
// double-quoted fields (embedded delimiters/line-breaks must be quoted,
// embedded quotes doubled), CRLF line endings, an optional `sep=X`
// first line declaring the delimiter, and delimiter auto-detection among
// comma / semicolon / colon / tab when no sep= line is present.
//
// charset selects the input encoding: "" means auto (UTF-8, falling back
// to windows-1252 when the bytes are not valid UTF-8); "utf-8" forces
// UTF-8; "windows-1252" (or "cp1252") forces windows-1252 decoding.
// golang.org/x/text is already a direct dependency, so no new dep.
func ParseCSV(data []byte, charset string) (*ParsedData, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("mailmerge: empty CSV file")
	}
	text, err := decodeCSVBytes(data, charset)
	if err != nil {
		return nil, err
	}
	// sep= metadata in the very first line declares any single-char
	// delimiter, e.g. "sep=#". The line is consumed, not a header.
	delimiter := byte(0)
	if strings.HasPrefix(text, "sep=") {
		if nl := strings.Index(text, "\n"); nl >= 0 {
			first := strings.TrimRight(text[:nl], "\r")
			if len(first) == 5 { // "sep=" + exactly one char
				delimiter = first[4]
				text = text[nl+1:]
			}
		}
	}
	if delimiter == 0 {
		delimiter = detectDelimiter(firstLine(text))
	}

	r := csv.NewReader(strings.NewReader(text))
	r.Comma = rune(delimiter)
	r.FieldsPerRecord = -1 // rows may be short; missing cells become ""
	r.LazyQuotes = false
	r.TrimLeadingSpace = false

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("mailmerge: parse CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("mailmerge: empty CSV file")
	}
	headers := records[0]
	if len(headers) == 0 || (len(headers) == 1 && strings.TrimSpace(headers[0]) == "") {
		return nil, fmt.Errorf("mailmerge: CSV first row must contain column names")
	}
	columns := dedupColumns(headers)
	rows := make([]map[string]string, 0, len(records)-1)
	for _, rec := range records[1:] {
		row := make(map[string]string, len(columns))
		for i, col := range columns {
			if i < len(rec) {
				row[col] = rec[i]
			} else {
				row[col] = ""
			}
		}
		rows = append(rows, row)
	}
	return &ParsedData{Columns: columns, Rows: rows}, nil
}

// decodeCSVBytes applies the charset rule: explicit charset wins, else
// UTF-8 when valid, windows-1252 fallback otherwise. A UTF-8 BOM is
// stripped in all cases.
func decodeCSVBytes(data []byte, charset string) (string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "", "auto":
		if utf8.Valid(data) {
			return string(data), nil
		}
		return decodeWindows1252(data)
	case "utf-8", "utf8":
		if !utf8.Valid(data) {
			return "", fmt.Errorf("mailmerge: CSV is not valid UTF-8 (try windows-1252)")
		}
		return string(data), nil
	case "windows-1252", "windows1252", "cp1252":
		return decodeWindows1252(data)
	default:
		return "", fmt.Errorf("mailmerge: unsupported CSV charset %q", charset)
	}
}

func decodeWindows1252(data []byte) (string, error) {
	out, _, err := transform.String(charmap.Windows1252.NewDecoder(), string(data))
	if err != nil {
		return "", fmt.Errorf("mailmerge: decode windows-1252: %w", err)
	}
	return out, nil
}

// detectDelimiter picks the candidate delimiter with the most
// occurrences outside double quotes on the header line. Ties prefer
// comma, then semicolon, then tab, then colon.
func detectDelimiter(headerLine string) byte {
	candidates := []byte{',', ';', '\t', ':'}
	best := byte(',')
	bestCount := -1
	for _, c := range candidates {
		if n := countUnquoted(headerLine, c); n > bestCount {
			best, bestCount = c, n
		}
	}
	return best
}

// countUnquoted counts occurrences of delim outside double-quoted spans
// ("" inside quotes is an escaped quote, not a span end).
func countUnquoted(s string, delim byte) int {
	n := 0
	inQuotes := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			if inQuotes && i+1 < len(s) && s[i+1] == '"' {
				i++ // escaped quote
			} else {
				inQuotes = !inQuotes
			}
		default:
			if s[i] == delim && !inQuotes {
				n++
			}
		}
	}
	return n
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return strings.TrimRight(s[:i], "\r")
	}
	return strings.TrimRight(s, "\r")
}

func dedupColumns(headers []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(headers))
	for _, h := range headers {
		if seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out
}

// ParseJSON parses Thunderbird Mail Merge JSON: UTF-8, an array of arrays
// whose first row is the headers. As a convenience it also accepts an
// array of objects (keys become headers, in first-seen order). Values
// are stringified: strings as-is, numbers without float formatting
// artefacts, booleans as true/false, null as "".
func ParseJSON(data []byte) (*ParsedData, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("mailmerge: empty JSON file")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw []json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("mailmerge: parse JSON: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("mailmerge: empty JSON file")
	}
	// Array-of-arrays (Thunderbird format)?
	var firstArr []json.RawMessage
	if err := json.Unmarshal(raw[0], &firstArr); err == nil {
		return parseJSONArrayOfArrays(raw)
	}
	// Array of objects?
	var firstObj map[string]json.RawMessage
	if err := json.Unmarshal(raw[0], &firstObj); err == nil {
		return parseJSONArrayOfObjects(raw)
	}
	return nil, fmt.Errorf("mailmerge: JSON must be an array of arrays or an array of objects")
}

func parseJSONArrayOfArrays(raw []json.RawMessage) (*ParsedData, error) {
	var rows [][]any
	dec := json.NewDecoder(bytes.NewReader(joinRaw(raw)))
	dec.UseNumber()
	if err := dec.Decode(&rows); err != nil {
		return nil, fmt.Errorf("mailmerge: parse JSON rows: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("mailmerge: empty JSON file")
	}
	headers := make([]string, 0, len(rows[0]))
	for _, h := range rows[0] {
		s, ok := h.(string)
		if !ok {
			return nil, fmt.Errorf("mailmerge: JSON header row must contain strings")
		}
		headers = append(headers, s)
	}
	columns := dedupColumns(headers)
	out := &ParsedData{Columns: columns}
	for _, rec := range rows[1:] {
		row := make(map[string]string, len(columns))
		for i, col := range columns {
			if i < len(rec) {
				row[col] = stringifyJSONValue(rec[i])
			} else {
				row[col] = ""
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// parseJSONArrayOfObjects decodes an array of objects, preserving the
// first object's key order as the column order.
func parseJSONArrayOfObjects(raw []json.RawMessage) (*ParsedData, error) {
	// First pass: ordered keys from the first object.
	keys, err := orderedObjectKeys(raw[0])
	if err != nil {
		return nil, err
	}
	columns := dedupColumns(keys)
	out := &ParsedData{Columns: columns}
	for _, item := range raw {
		var m map[string]any
		dec := json.NewDecoder(bytes.NewReader(item))
		dec.UseNumber()
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("mailmerge: parse JSON object: %w", err)
		}
		row := make(map[string]string, len(columns))
		for _, col := range columns {
			if v, ok := m[col]; ok {
				row[col] = stringifyJSONValue(v)
			} else {
				row[col] = ""
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// orderedObjectKeys returns an object's keys in file order.
func orderedObjectKeys(raw json.RawMessage) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("mailmerge: parse JSON object: %w", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("mailmerge: expected a JSON object")
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("mailmerge: parse JSON object keys: %w", err)
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("mailmerge: expected a JSON object key")
		}
		keys = append(keys, key)
		// Skip the value.
		var skip any
		if err := dec.Decode(&skip); err != nil {
			return nil, fmt.Errorf("mailmerge: parse JSON object: %w", err)
		}
	}
	return keys, nil
}

func joinRaw(raw []json.RawMessage) []byte {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, r := range raw {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(r)
	}
	b.WriteByte(']')
	return b.Bytes()
}

func stringifyJSONValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(raw)
	}
}

// ---------------------------------------------------------------------------
// XLSX source (Phase 2)
// ---------------------------------------------------------------------------
// excelize (github.com/xuri/excelize/v2, BSD-3-Clause, pure Go) is the
// maintained Go spreadsheet library; the module moved from the qax-os
// org to xuri, which is why the import path is xuri/excelize.
//
// ODS is deliberately NOT supported: excelize does not read ODS, and
// the pure-Go ODS options are unmaintained one-person libraries
// (github.com/ksev/ods: "only covers my own needs";
// github.com/multiprocessio/go-openoffice: "rudimentary", last touched
// 2022; the only complete alternative, Aspose.Cells for Go, is a
// commercial CGo/cloud product). An ODS file therefore errors with a
// clear message instead of a half-working parse.

// ListXLSXSheets returns the workbook's sheet names, in order.
func ListXLSXSheets(data []byte) ([]string, error) {
	f, err := openXLSX(data)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.GetSheetList(), nil
}

// ParseXLSX parses an .xlsx workbook the same way ParseCSV parses a
// CSV: the first row of the sheet is the headers, every following row
// is one recipient. sheet selects the sheet by name ("" = the first
// sheet). Fully-empty rows are skipped, matching encoding/csv's
// blank-line behaviour; short rows pad missing cells with "".
// The result is the SAME []map[string]string + columns shape as CSV,
// so XLSX rows merge identically to CSV rows.
func ParseXLSX(data []byte, sheet string) (*ParsedData, error) {
	f, err := openXLSX(data)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("mailmerge: XLSX workbook has no sheets")
	}
	if sheet == "" {
		sheet = sheets[0]
	}
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("mailmerge: read XLSX sheet %q: %w", sheet, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("mailmerge: XLSX sheet %q is empty", sheet)
	}
	headers := rows[0]
	if len(headers) == 0 || (len(headers) == 1 && strings.TrimSpace(headers[0]) == "") {
		return nil, fmt.Errorf("mailmerge: XLSX first row must contain column names")
	}
	columns := dedupColumns(headers)
	out := &ParsedData{Columns: columns}
	for _, rec := range rows[1:] {
		if rowIsEmpty(rec) {
			continue
		}
		row := make(map[string]string, len(columns))
		for i, col := range columns {
			if i < len(rec) {
				row[col] = rec[i]
			} else {
				row[col] = ""
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// openXLSX opens an .xlsx workbook from bytes. A corrupt/empty file or
// a non-XLSX file (e.g. an ODS renamed to .xlsx) errors here.
func openXLSX(data []byte) (*excelize.File, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("mailmerge: empty XLSX file")
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("mailmerge: parse XLSX: %w", err)
	}
	return f, nil
}

// rowIsEmpty reports whether every cell of a spreadsheet row is blank.
func rowIsEmpty(rec []string) bool {
	for _, c := range rec {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Contacts adapter
// ---------------------------------------------------------------------------

// ContactColumns is the Thunderbird variable-name list the contacts
// adapter produces, in a stable order. Every row carries all of these
// keys ("" when the contact has no value), so a template referencing any
// of them validates. Custom1–4 and Anniversary have NO equivalent in
// Aerion's contact.Record and are deliberately ABSENT — never invented.
func ContactColumns() []string {
	return []string{
		"DisplayName", "FirstName", "LastName", "NickName",
		"PrimaryEmail", "SecondEmail",
		"HomePhone", "WorkPhone", "CellularNumber", "FaxNumber", "PagerNumber",
		"HomeAddress", "HomeCity", "HomeState", "HomeZipCode", "HomeCountry",
		"WorkAddress", "WorkCity", "WorkState", "WorkZipCode", "WorkCountry",
		"WebPage1", "WebPage2",
		"JobTitle", "Company", "Notes",
		"BirthYear", "BirthMonth", "BirthDay",
	}
}

// RecordToRow maps one contact.Record onto Thunderbird variable names:
// DisplayName←Fn, FirstName←NGiven, LastName←NFamily, PrimaryEmail←
// primary Emails[] entry, SecondEmail←second Emails[] entry, phones by
// type, Company←Org, JobTitle←Title, Notes←Note, NickName←Nickname,
// BirthYear/Month/Day←Bday parsed best-effort, Home/Work address fields
// ←Addresses[] by type, WebPage1/2←URLs[] in order. The plain names are
// supported in Phase 1; the CardBook `#type` suffix form ({{email#home}})
// is not.
func RecordToRow(r *contact.Record) map[string]string {
	row := make(map[string]string, len(ContactColumns()))
	for _, col := range ContactColumns() {
		row[col] = ""
	}
	if r == nil {
		return row
	}
	row["DisplayName"] = r.Fn
	row["FirstName"] = r.NGiven
	row["LastName"] = r.NFamily
	row["NickName"] = r.Nickname
	row["Company"] = r.Org
	row["JobTitle"] = r.Title
	row["Notes"] = r.Note

	primary, second := splitEmails(r.Emails)
	if primary != "" {
		row["PrimaryEmail"] = primary
	}
	if second != "" {
		row["SecondEmail"] = second
	}
	row["HomePhone"] = phoneByType(r.Phones, "home")
	row["WorkPhone"] = phoneByType(r.Phones, "work")
	row["CellularNumber"] = phoneByType(r.Phones, "cell", "mobile", "cellular")
	row["FaxNumber"] = phoneByType(r.Phones, "fax")
	row["PagerNumber"] = phoneByType(r.Phones, "pager")

	if a := addressByType(r.Addresses, "home"); a != nil {
		row["HomeAddress"] = a.Street
		row["HomeCity"] = a.City
		row["HomeState"] = a.Region
		row["HomeZipCode"] = a.Postcode
		row["HomeCountry"] = a.Country
	}
	if a := addressByType(r.Addresses, "work"); a != nil {
		row["WorkAddress"] = a.Street
		row["WorkCity"] = a.City
		row["WorkState"] = a.Region
		row["WorkZipCode"] = a.Postcode
		row["WorkCountry"] = a.Country
	}
	if len(r.URLs) > 0 {
		row["WebPage1"] = r.URLs[0].URL
	}
	if len(r.URLs) > 1 {
		row["WebPage2"] = r.URLs[1].URL
	}
	if y, m, d := parseBday(r.Bday); y != "" || m != "" || d != "" {
		row["BirthYear"] = y
		row["BirthMonth"] = m
		row["BirthDay"] = d
	}
	return row
}

// splitEmails returns the primary email (IsPrimary, else the first) and
// the second email (the first entry that is not the primary), or "".
func splitEmails(emails []contact.RecordEmail) (primary, second string) {
	if len(emails) == 0 {
		return "", ""
	}
	primaryIdx := 0
	for i, e := range emails {
		if e.IsPrimary {
			primaryIdx = i
			break
		}
	}
	primary = emails[primaryIdx].Email
	for i, e := range emails {
		if i != primaryIdx {
			second = e.Email
			break
		}
	}
	return primary, second
}

// normalizePhoneType lowercases and trims a stored phone type.
func normalizePhoneType(t string) string {
	return strings.ToLower(strings.TrimSpace(t))
}

// phoneByType returns the first phone whose type matches one of the
// wanted types (in wanted order); among matches of the same type the
// primary one wins.
func phoneByType(phones []contact.RecordPhone, wanted ...string) string {
	for _, w := range wanted {
		var fallback string
		for _, p := range phones {
			if normalizePhoneType(p.PhoneType) != w {
				continue
			}
			if p.IsPrimary {
				return p.Number
			}
			if fallback == "" {
				fallback = p.Number
			}
		}
		if fallback != "" {
			return fallback
		}
	}
	return ""
}

func addressByType(addrs []contact.RecordAddress, wanted string) *contact.RecordAddress {
	for i := range addrs {
		if strings.ToLower(strings.TrimSpace(addrs[i].AddrType)) == wanted {
			return &addrs[i]
		}
	}
	return nil
}

// parseBday parses a vCard BDAY string best-effort, returning year,
// month and day as plain numbers ("" for parts that are absent).
// Accepted: 2006-01-02, 20060102, 2006/01/02, 2006-01, --01-02, 01-02.
func parseBday(s string) (year, month, day string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", ""
	}
	layouts := []string{"2006-01-02", "2006-1-2", "20060102", "2006/01/02", "2006-01", "2006/01"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			y := strconv.Itoa(t.Year())
			if layout == "2006-01" || layout == "2006/01" { // year-month only
				return y, strconv.Itoa(int(t.Month())), ""
			}
			return y, strconv.Itoa(int(t.Month())), strconv.Itoa(t.Day())
		}
	}
	// "--MM-DD" (no year) and "MM-DD".
	for _, layout := range []string{"--01-02", "01-02", "--1-2", "1-2"} {
		if t, err := time.Parse(layout, s); err == nil {
			return "", strconv.Itoa(int(t.Month())), strconv.Itoa(int(t.Day()))
		}
	}
	return "", "", ""
}
