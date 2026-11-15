package mailmerge

import (
	"reflect"
	"testing"

	"github.com/absonsnet/daak/internal/contact"
	"github.com/xuri/excelize/v2"
)

func TestParseCSVBasic(t *testing.T) {
	data := []byte("FirstName,LastName,Email\r\nJohn,Doe,john@example.com\r\nJane,Smith,jane@example.com\r\n")
	p, err := ParseCSV(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Columns) != 3 || p.Columns[0] != "FirstName" || p.Columns[2] != "Email" {
		t.Fatalf("columns %v", p.Columns)
	}
	if len(p.Rows) != 2 {
		t.Fatalf("rows %d", len(p.Rows))
	}
	if p.Rows[0]["FirstName"] != "John" || p.Rows[1]["Email"] != "jane@example.com" {
		t.Fatalf("rows %v", p.Rows)
	}
}

func TestParseCSVQuotedDelimiters(t *testing.T) {
	data := []byte("Name,Note,Email\n\"Doe, John\",\"says \"\"hi\"\", bye\",john@example.com\n")
	p, err := ParseCSV(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows[0]["Name"] != "Doe, John" {
		t.Fatalf("name %q", p.Rows[0]["Name"])
	}
	if p.Rows[0]["Note"] != `says "hi", bye` {
		t.Fatalf("note %q", p.Rows[0]["Note"])
	}
}

func TestParseCSVEmbeddedNewline(t *testing.T) {
	data := []byte("Name,Note\nJohn,\"line1\nline2\"\n")
	p, err := ParseCSV(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows[0]["Note"] != "line1\nline2" {
		t.Fatalf("note %q", p.Rows[0]["Note"])
	}
}

func TestParseCSVSepDirective(t *testing.T) {
	// sep=# declares an arbitrary delimiter.
	data := []byte("sep=#\nFirstName#Email\nJohn#john@example.com\n")
	p, err := ParseCSV(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Columns) != 2 || p.Columns[0] != "FirstName" {
		t.Fatalf("columns %v", p.Columns)
	}
	if p.Rows[0]["Email"] != "john@example.com" {
		t.Fatalf("row %v", p.Rows[0])
	}
	// sep=; with semicolons.
	data = []byte("sep=;\nA;B\n1;2\n")
	p, err = ParseCSV(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows[0]["B"] != "2" {
		t.Fatalf("row %v", p.Rows[0])
	}
}

func TestParseCSVDelimiterAutoDetect(t *testing.T) {
	for _, tc := range []struct {
		data string
		want string
	}{
		{"A;B;C\n1;2;3\n", "2"},
		{"A\tB\n1\t2\n", "2"},
		{"A:B\n1:2\n", "2"},
		{"A,B\n1,2\n", "2"},
	} {
		p, err := ParseCSV([]byte(tc.data), "")
		if err != nil {
			t.Fatalf("%q: %v", tc.data, err)
		}
		if p.Rows[0]["B"] != tc.want {
			t.Fatalf("%q: row %v", tc.data, p.Rows[0])
		}
	}
}

func TestParseCSVWindows1252(t *testing.T) {
	// 0xE9 is é in windows-1252 (invalid UTF-8 on its own).
	data := []byte("Name,Email\nAndr\xe9,andre@example.com\n")
	p, err := ParseCSV(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows[0]["Name"] != "André" {
		t.Fatalf("name %q", p.Rows[0]["Name"])
	}
	// Explicit charset.
	p, err = ParseCSV(data, "windows-1252")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows[0]["Name"] != "André" {
		t.Fatalf("explicit charset: name %q", p.Rows[0]["Name"])
	}
	// Explicit UTF-8 with invalid bytes errors.
	if _, err := ParseCSV(data, "utf-8"); err == nil {
		t.Fatal("expected UTF-8 error")
	}
	// Quoted-printable-style smart quote 0x93 → “ in windows-1252.
	data = []byte("Note\n\x93hello\x94\n")
	p, err = ParseCSV(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows[0]["Note"] != "“hello”" {
		t.Fatalf("note %q", p.Rows[0]["Note"])
	}
}

func TestParseCSVCaseSensitiveHeaders(t *testing.T) {
	p, err := ParseCSV([]byte("Email,email\nA@b.c,d@e.f\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows[0]["Email"] != "A@b.c" || p.Rows[0]["email"] != "d@e.f" {
		t.Fatalf("row %v", p.Rows[0])
	}
}

func TestParseCSVShortRows(t *testing.T) {
	p, err := ParseCSV([]byte("A,B,C\n1,2\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Rows[0]["C"] != "" {
		t.Fatalf("short row C %q", p.Rows[0]["C"])
	}
}

func TestParseCSVErrors(t *testing.T) {
	for _, data := range []string{"", "   \n", "\n"} {
		if _, err := ParseCSV([]byte(data), ""); err == nil {
			t.Fatalf("%q: expected error", data)
		}
	}
}

func TestParseJSONArrays(t *testing.T) {
	data := []byte(`[["FirstName","LastName","Email","Age"],["John","Doe","john@example.com",42],["Jane","Smith","jane@example.com",null]]`)
	p, err := ParseJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Columns) != 4 || p.Columns[0] != "FirstName" {
		t.Fatalf("columns %v", p.Columns)
	}
	if p.Rows[0]["Age"] != "42" {
		t.Fatalf("age %q", p.Rows[0]["Age"])
	}
	if p.Rows[1]["Age"] != "" {
		t.Fatalf("null age %q", p.Rows[1]["Age"])
	}
}

func TestParseJSONObjects(t *testing.T) {
	data := []byte(`[{"FirstName":"John","Email":"john@example.com","Subscribed":true},{"FirstName":"Jane","Email":"jane@example.com"}]`)
	p, err := ParseJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Columns) != 3 || p.Columns[0] != "FirstName" || p.Columns[1] != "Email" || p.Columns[2] != "Subscribed" {
		t.Fatalf("columns %v", p.Columns)
	}
	if p.Rows[0]["Subscribed"] != "true" {
		t.Fatalf("bool %q", p.Rows[0]["Subscribed"])
	}
	if p.Rows[1]["Subscribed"] != "" {
		t.Fatalf("missing key %q", p.Rows[1]["Subscribed"])
	}
}

func TestParseJSONErrors(t *testing.T) {
	for _, data := range []string{"", "[]", `{"a":1}`, `"hello"`, "not json"} {
		if _, err := ParseJSON([]byte(data)); err == nil {
			t.Fatalf("%q: expected error", data)
		}
	}
}

func TestRecordToRow(t *testing.T) {
	rec := &contact.Record{
		Fn:       "John Doe",
		NGiven:   "John",
		NFamily:  "Doe",
		Nickname: "Johnny",
		Org:      "Acme",
		Title:    "Engineer",
		Note:     "VIP",
		Bday:     "1990-04-15",
		Emails: []contact.RecordEmail{
			{Email: "work@example.com", EmailType: "work"},
			{Email: "john@example.com", EmailType: "home", IsPrimary: true},
		},
		Phones: []contact.RecordPhone{
			{Number: "+1-111", PhoneType: "home"},
			{Number: "+1-222", PhoneType: "work", IsPrimary: true},
			{Number: "+1-333", PhoneType: "cell"},
			{Number: "+1-444", PhoneType: "fax"},
		},
		Addresses: []contact.RecordAddress{
			{AddrType: "home", Street: "1 Main St", City: "Springfield", Region: "IL", Postcode: "62701", Country: "US"},
			{AddrType: "work", Street: "2 Corp Way", City: "Chicago", Region: "IL", Postcode: "60601", Country: "US"},
		},
		URLs: []contact.RecordURL{{URL: "https://example.com"}, {URL: "https://blog.example.com"}},
	}
	row := RecordToRow(rec)
	want := map[string]string{
		"DisplayName": "John Doe", "FirstName": "John", "LastName": "Doe",
		"NickName": "Johnny", "PrimaryEmail": "john@example.com", "SecondEmail": "work@example.com",
		"HomePhone": "+1-111", "WorkPhone": "+1-222", "CellularNumber": "+1-333", "FaxNumber": "+1-444",
		"HomeAddress": "1 Main St", "HomeCity": "Springfield", "HomeState": "IL",
		"HomeZipCode": "62701", "HomeCountry": "US",
		"WorkAddress": "2 Corp Way", "WorkCity": "Chicago",
		"WebPage1": "https://example.com", "WebPage2": "https://blog.example.com",
		"JobTitle": "Engineer", "Company": "Acme", "Notes": "VIP",
		"BirthYear": "1990", "BirthMonth": "4", "BirthDay": "15",
	}
	for k, v := range want {
		if row[k] != v {
			t.Fatalf("%s: got %q want %q", k, row[k], v)
		}
	}
	// Custom1–4 and Anniversary must be ABSENT, never invented.
	for _, k := range []string{"Custom1", "Custom2", "Custom3", "Custom4", "AnniversaryYear", "AnniversaryMonth", "AnniversaryDay", "Department"} {
		if _, ok := row[k]; ok {
			t.Fatalf("%s must be absent", k)
		}
	}
	// Every declared column is present ("" when unknown) so templates
	// referencing any of them validate.
	for _, col := range ContactColumns() {
		if _, ok := row[col]; !ok {
			t.Fatalf("column %s missing", col)
		}
	}
}

func TestRecordToRowMinimal(t *testing.T) {
	row := RecordToRow(&contact.Record{Fn: "Solo", Emails: []contact.RecordEmail{{Email: "solo@example.com"}}})
	if row["PrimaryEmail"] != "solo@example.com" || row["SecondEmail"] != "" {
		t.Fatalf("emails %q %q", row["PrimaryEmail"], row["SecondEmail"])
	}
	if row["BirthYear"] != "" || row["HomePhone"] != "" {
		t.Fatalf("empty fields must be empty strings: %v", row)
	}
	// Nil record yields an all-empty row, not a panic.
	row = RecordToRow(nil)
	if row["DisplayName"] != "" {
		t.Fatalf("nil row %v", row)
	}
}

func TestRecordToRowBdayVariants(t *testing.T) {
	cases := []struct {
		bday, y, m, d string
	}{
		{"1990-04-15", "1990", "4", "15"},
		{"19900415", "1990", "4", "15"},
		{"--04-15", "", "4", "15"},
		{"1990-04", "1990", "4", ""},
		{"not-a-date", "", "", ""},
		{"", "", "", ""},
	}
	for _, c := range cases {
		row := RecordToRow(&contact.Record{Bday: c.bday})
		if row["BirthYear"] != c.y || row["BirthMonth"] != c.m || row["BirthDay"] != c.d {
			t.Fatalf("bday %q: got %q/%q/%q want %q/%q/%q", c.bday,
				row["BirthYear"], row["BirthMonth"], row["BirthDay"], c.y, c.m, c.d)
		}
	}
}

func TestRecordToRowPhoneTypeVariants(t *testing.T) {
	rec := &contact.Record{
		Phones: []contact.RecordPhone{
			{Number: "M1", PhoneType: "mobile"},
			{Number: "H1", PhoneType: "HOME"},
		},
	}
	row := RecordToRow(rec)
	if row["CellularNumber"] != "M1" {
		t.Fatalf("mobile → CellularNumber: %q", row["CellularNumber"])
	}
	if row["HomePhone"] != "H1" {
		t.Fatalf("HOME (case) → HomePhone: %q", row["HomePhone"])
	}
}

// --- XLSX (Phase 2) ------------------------------------------------------
// The test workbooks are generated in-memory with excelize — no binary
// fixture is committed.

type testXLSXSheet struct {
	name string
	rows [][]string
}

// buildTestXLSX builds a workbook with the given sheets, in order.
func buildTestXLSX(t *testing.T, sheets []testXLSXSheet) []byte {
	t.Helper()
	f := excelize.NewFile()
	for i, sh := range sheets {
		name := sh.name
		if i == 0 {
			// NewFile() already has Sheet1; rename it when the test
			// wants a different first sheet name.
			if name != "Sheet1" {
				if err := f.SetSheetName("Sheet1", name); err != nil {
					t.Fatal(err)
				}
			}
		} else {
			if _, err := f.NewSheet(name); err != nil {
				t.Fatal(err)
			}
		}
		for r, rec := range sh.rows {
			for c, v := range rec {
				cell, err := excelize.CoordinatesToCellName(c+1, r+1)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.SetCellValue(name, cell, v); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestParseXLSXIdenticalToCSV(t *testing.T) {
	csvData := []byte("FirstName,LastName,Email\nJohn,Doe,john@example.com\nJane,Smith,jane@example.com\n")
	xlsxData := buildTestXLSX(t, []testXLSXSheet{{
		name: "Recipients",
		rows: [][]string{
			{"FirstName", "LastName", "Email"},
			{"John", "Doe", "john@example.com"},
			{"Jane", "Smith", "jane@example.com"},
		},
	}})
	csvParsed, err := ParseCSV(csvData, "")
	if err != nil {
		t.Fatal(err)
	}
	xlsxParsed, err := ParseXLSX(xlsxData, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(csvParsed.Columns, xlsxParsed.Columns) {
		t.Fatalf("columns differ: csv %v xlsx %v", csvParsed.Columns, xlsxParsed.Columns)
	}
	if !reflect.DeepEqual(csvParsed.Rows, xlsxParsed.Rows) {
		t.Fatalf("rows differ: csv %v xlsx %v", csvParsed.Rows, xlsxParsed.Rows)
	}
	// The rows must also merge identically: same rendered messages.
	tmpl := Template{
		From: "Sender <sender@example.com>", To: "{{FirstName}} {{LastName}} <{{Email}}>",
		Subject: "Hello {{FirstName}}", BodyHTML: "<p>Hi {{FirstName}}.</p>", BodyText: "Hi {{FirstName}}.",
	}
	for i := range csvParsed.Rows {
		a, err := RenderRow(tmpl, csvParsed.Rows[i], "", i)
		if err != nil {
			t.Fatal(err)
		}
		b, err := RenderRow(tmpl, xlsxParsed.Rows[i], "", i)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*a, *b) {
			t.Fatalf("row %d rendered differently: csv %+v xlsx %+v", i, a, b)
		}
	}
}

func TestParseXLSXSheetSelection(t *testing.T) {
	data := buildTestXLSX(t, []testXLSXSheet{
		{name: "List", rows: [][]string{{"Email"}, {"a@example.com"}}},
		{name: "Other", rows: [][]string{{"Email"}, {"b@example.com"}, {"c@example.com"}}},
	})
	sheets, err := ListXLSXSheets(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sheets, []string{"List", "Other"}) {
		t.Fatalf("sheets %v", sheets)
	}
	// Default = first sheet.
	def, err := ParseXLSX(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Rows) != 1 || def.Rows[0]["Email"] != "a@example.com" {
		t.Fatalf("default sheet %+v", def.Rows)
	}
	// Named sheet.
	other, err := ParseXLSX(data, "Other")
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Rows) != 2 || other.Rows[1]["Email"] != "c@example.com" {
		t.Fatalf("other sheet %+v", other.Rows)
	}
	if _, err := ParseXLSX(data, "Nope"); err == nil {
		t.Fatal("expected error for unknown sheet")
	}
}

func TestParseXLSXErrors(t *testing.T) {
	if _, err := ParseXLSX([]byte("this is not a zip file"), ""); err == nil {
		t.Fatal("expected error for non-XLSX bytes")
	}
	if _, err := ParseXLSX([]byte{}, ""); err == nil {
		t.Fatal("expected error for empty input")
	}
	// A workbook whose first sheet has no rows at all.
	empty := buildTestXLSX(t, []testXLSXSheet{{name: "Sheet1", rows: nil}})
	if _, err := ParseXLSX(empty, ""); err == nil {
		t.Fatal("expected error for empty sheet")
	}
	// Header-only sheet parses to zero rows (valid, like an empty CSV
	// with headers only — except CSV requires at least the header row).
	headersOnly := buildTestXLSX(t, []testXLSXSheet{{
		name: "Sheet1", rows: [][]string{{"Email"}},
	}})
	p, err := ParseXLSX(headersOnly, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 0 || len(p.Columns) != 1 {
		t.Fatalf("header-only %+v", p)
	}
}

func TestParseXLSXSkipsEmptyRows(t *testing.T) {
	data := buildTestXLSX(t, []testXLSXSheet{{
		name: "Sheet1",
		rows: [][]string{
			{"Email"},
			{"a@example.com"},
			{"", ""}, // blank row between recipients
			{"b@example.com"},
		},
	}})
	p, err := ParseXLSX(data, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 2 {
		t.Fatalf("rows %v", p.Rows)
	}
}
