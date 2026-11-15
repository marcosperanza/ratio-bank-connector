// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package providertemplate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source/sourcetest"
)

func fixture(t *testing.T) *Provider {
	t.Helper()
	path := filepath.Join(t.TempDir(), "movements.json")
	body := `[
	  {"id":"m1","date":"2026-09-14","amount":-12.34,"description":"Spesa","merchant":"Coop"},
	  {"id":"m2","date":"2026-09-15","amount":1850,"currency":"EUR","description":"Stipendio"}
	]`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return New(map[string]Account{"acct": {Path: path}})
}

// Every provider runs the shared suite. This is the line to keep when copying.
func TestConformance(t *testing.T) {
	sourcetest.Run(t, fixture(t), source.Request{Account: "acct", From: "2026-09-01"})
}

func TestOnlyReturnsFromTheRequestedDay(t *testing.T) {
	res, err := fixture(t).Fetch(source.Request{Account: "acct", From: "2026-09-15"})
	if err != nil || len(res.Transactions) != 1 || res.Transactions[0].ID != "m2" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestIsLocalOnceConfigured(t *testing.T) {
	p := fixture(t)
	var _ source.Local = p
	if !p.Configured("acct") || p.Configured("other") {
		t.Fatal("Configured must follow the settings")
	}
}
