// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package simplefinsource

import (
	"os"
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source/sourcetest"
)

// TestLiveBridge runs the source against a real SimpleFIN server. It is off by
// default so the suite needs no network; SimpleFIN publishes a demo server for
// exactly this:
//
//	SIMPLEFIN_LIVE_URL=https://demo:demo@beta-bridge.simplefin.org/simplefin go test ./internal/source/simplefinsource -run Live -v
//
// SIMPLEFIN_LIVE_ACCOUNT picks the account (default "Demo Checking").
func TestLiveBridge(t *testing.T) {
	access := os.Getenv("SIMPLEFIN_LIVE_URL")
	if access == "" {
		t.Skip("set SIMPLEFIN_LIVE_URL to run against a real SimpleFIN server")
	}
	id := os.Getenv("SIMPLEFIN_LIVE_ACCOUNT")
	if id == "" {
		id = "Demo Checking"
	}

	infos, err := List(nil, access)
	if err != nil || len(infos) == 0 {
		t.Fatalf("list: %v (%d accounts)", err, len(infos))
	}
	t.Logf("accounts: %+v", infos)

	src := New(map[string]Account{"live": {AccessURL: access, ID: id}})
	req := source.Request{Account: "live", From: "2020-01-01"}
	res, err := src.Fetch(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d movements", len(res.Transactions))
	for i, tx := range res.Transactions {
		if i < 3 {
			t.Logf("  %s %s %.2f %s %q", tx.ID, tx.BookingDate, tx.Amount, tx.Currency, tx.Description)
		}
	}
	sourcetest.Run(t, src, req)
}
