// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package foldersource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source/sourcetest"
)

// An export in the shape many Italian banks produce: semicolons, day-first
// dates, a decimal comma, a thousands dot, Latin-1 accents, a header and a
// preamble line.
const italianCSV = "Conto: 1234\n" +
	"Data;Valuta;Descrizione;Importo\n" +
	"14/09/2026;14/09/2026;Caff\xe8 Bar Roma;-1,20\n" +
	"14/09/2026;14/09/2026;Caff\xe8 Bar Roma;-1,20\n" +
	"15/09/2026;15/09/2026;Stipendio;1.850,00\n" +
	";;;\n"

func italianProfile() Profile {
	return Profile{
		Delimiter: ";", SkipRows: 2, Encoding: "latin1", DateLayout: "02/01/2006",
		Decimal: ",", Thousands: ".",
		Columns: map[string]int{"date": 0, "description": 2, "amount": 3},
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fetch(t *testing.T, dir string, p Profile, from string) []source.Transaction {
	t.Helper()
	s := New(map[string]Account{"acct": {Dir: dir, Profile: p}})
	res, err := s.Fetch(source.Request{Account: "acct", From: from})
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range res.Transactions {
		if err := tx.Validate(); err != nil {
			t.Fatalf("source produced an invalid transaction: %v", err)
		}
	}
	return res.Transactions
}

func TestReadsAnItalianBankCSV(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "movimenti.csv", italianCSV)

	got := fetch(t, dir, italianProfile(), "2026-09-01")

	if len(got) != 3 {
		t.Fatalf("want 3 movements, got %d: %+v", len(got), got)
	}
	if got[0].Description != "Caffè Bar Roma" || got[0].Amount != -1.2 || got[0].BookingDate != "2026-09-14" || got[0].Currency != "EUR" {
		t.Fatalf("first row: %+v", got[0])
	}
	if got[2].Amount != 1850 {
		t.Fatalf("thousands separator mishandled: %+v", got[2])
	}
}

func TestTwoIdenticalRowsOnOneDayStayTwoMovements(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "m.csv", italianCSV)

	got := fetch(t, dir, italianProfile(), "2026-09-01")

	if got[0].ID == got[1].ID {
		t.Fatalf("two coffees collapsed into one id %q", got[0].ID)
	}
}

func TestOverlappingExportsDoNotRepeatMovements(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.csv", italianCSV)
	write(t, dir, "b.csv", italianCSV)

	if got := fetch(t, dir, italianProfile(), "2026-09-01"); len(got) != 3 {
		t.Fatalf("the same export twice must read as once, got %d", len(got))
	}
}

func TestSameMovementGetsTheSameIDOnEveryRead(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "m.csv", italianCSV)

	first := fetch(t, dir, italianProfile(), "2026-09-01")
	second := fetch(t, dir, italianProfile(), "2026-09-01")

	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("id changed between reads: %q vs %q", first[i].ID, second[i].ID)
		}
	}
}

func TestOnlyMovementsFromTheRequestedDayAreReturned(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "m.csv", italianCSV)

	// The first two rows are 14 Sept, the third is 15 Sept. With a week of
	// overlap, asking from 30 Sept excludes all of them; from 20 Sept keeps all.
	if got := fetch(t, dir, italianProfile(), "2026-09-30"); len(got) != 0 {
		t.Fatalf("got %d", len(got))
	}
	if got := fetch(t, dir, italianProfile(), "2026-09-20"); len(got) != 3 {
		t.Fatalf("overlap should keep recent rows, got %d", len(got))
	}
}

func TestDebitAndCreditColumns(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "m.csv", "data,desc,dare,avere\n2026-09-14,Affitto,500.00,\n2026-09-15,Rimborso,,20.50\n")
	p := Profile{SkipRows: 1, DateLayout: "2006-01-02", Columns: map[string]int{"date": 0, "description": 1, "debit": 2, "credit": 3}}

	got := fetch(t, dir, p, "2026-09-01")

	if len(got) != 2 || got[0].Amount != -500 || got[1].Amount != 20.5 {
		t.Fatalf("got %+v", got)
	}
}

func TestAProfileThatDoesNotFitTheFileSaysWhere(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "m.csv", "Data;Valuta;Descrizione;Importo\nieri;x;y;1\n")
	p := italianProfile()
	p.SkipRows = 1
	s := New(map[string]Account{"acct": {Dir: dir, Profile: p}})

	_, err := s.Fetch(source.Request{Account: "acct", From: "2026-09-01"})

	if err == nil {
		t.Fatal("expected an error naming the line")
	}
}

const ofxSGML = "OFXHEADER:100\nDATA:OFXSGML\n\n<OFX><BANKMSGSRSV1><STMTTRNRS><STMTRS>\n<CURDEF>EUR\n<BANKTRANLIST>\n" +
	"<STMTTRN>\n<TRNTYPE>DEBIT\n<DTPOSTED>20260914120000\n<TRNAMT>-12.34\n<FITID>A1\n<NAME>COOP\n<MEMO>MILANO\n</STMTTRN>\n" +
	"<STMTTRN>\n<TRNTYPE>CREDIT\n<DTPOSTED>20260915\n<TRNAMT>100.00\n<FITID>A2\n<NAME>BONIFICO\n" +
	"</BANKTRANLIST></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>\n"

func TestReadsOFXWithOpenTagsAndKeepsTheBanksOwnID(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "estratto.ofx", ofxSGML)

	got := fetch(t, dir, Profile{}, "2026-09-01")

	if len(got) != 2 {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	if got[0].ID != "ofx:acct:A1" || got[0].Amount != -12.34 || got[0].Description != "COOP MILANO" || got[0].BookingDate != "2026-09-14" {
		t.Fatalf("first: %+v", got[0])
	}
	if got[1].Amount != 100 || got[1].Currency != "EUR" {
		t.Fatalf("second: %+v", got[1])
	}
}

func TestIgnoresFilesItCannotReadAsExports(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "note.md", "not an export")
	write(t, dir, ".hidden.csv", "garbage")
	write(t, dir, "estratto.ofx", ofxSGML)

	if got := fetch(t, dir, Profile{}, "2026-09-01"); len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
}

func TestAnUnknownAccountIsRefused(t *testing.T) {
	_, err := New(nil).Fetch(source.Request{Account: "nope", From: "2026-09-01"})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseAmount(t *testing.T) {
	cases := []struct {
		in, dec, th string
		want        float64
	}{
		{"1.234,56", ",", ".", 1234.56},
		{"-12,30", ",", ".", -12.3},
		{"(12.00)", ".", "", -12},
		{"12,00-", ",", ".", -12},
		{"+5", ".", "", 5},
		{"€ 1,000.50", ".", ",", 1000.5},
	}
	for _, c := range cases {
		got, err := parseAmount(c.in, c.dec, c.th)
		if err != nil || got != c.want {
			t.Errorf("%q: got %v, %v want %v", c.in, got, err, c.want)
		}
	}
	if _, err := parseAmount("abc", ".", ""); err == nil {
		t.Error("expected an error for text")
	}
}

func TestConformance(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "movimenti.csv", italianCSV)
	write(t, dir, "estratto.ofx", ofxSGML)
	s := New(map[string]Account{"acct": {Dir: dir, Profile: italianProfile()}})
	sourcetest.Run(t, s, source.Request{Account: "acct", From: "2026-09-01"})
}
