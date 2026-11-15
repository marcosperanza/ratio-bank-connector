// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
)

func TestFolderAccountsAreKeyedByTheRatioAccount(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Folders: []config.FolderConfig{
		{Account: "acc-1", Path: "/data/ing", Format: "csv", Profile: config.FolderProfile{
			Delimiter: ";", SkipRows: 1, Encoding: "latin1", DateLayout: "02/01/2006",
			Decimal: ",", Thousands: ".", Currency: "EUR", Columns: map[string]int{"date": 0},
		}},
		{Account: "acc-2", Path: "/data/ofx"},
	}}

	got := folderAccounts(cfg)

	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	a := got["acc-1"]
	if a.Dir != "/data/ing" || a.Format != "csv" || a.Profile.Delimiter != ";" || a.Profile.SkipRows != 1 ||
		a.Profile.Encoding != "latin1" || a.Profile.DateLayout != "02/01/2006" || a.Profile.Decimal != "," ||
		a.Profile.Thousands != "." || a.Profile.Currency != "EUR" || a.Profile.Columns["date"] != 0 {
		t.Fatalf("account: %+v", a)
	}
	if got["acc-2"].Dir != "/data/ofx" {
		t.Fatalf("second: %+v", got["acc-2"])
	}
}

func TestNoFoldersMeansNoAccounts(t *testing.T) {
	t.Parallel()
	if got := folderAccounts(&config.Config{}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
