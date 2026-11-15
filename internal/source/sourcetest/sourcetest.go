// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package sourcetest is the conformance suite every provider must pass. It
// checks what the core relies on and a provider is most likely to get wrong:
// that identifiers hold still, that nothing it returns would be refused later,
// and that asking for a day with nothing on it is not an error.
package sourcetest

import (
	"encoding/json"
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
)

// Run fetches twice with req and judges the results. req should point at
// fixtures that produce at least one transaction.
func Run(t *testing.T, src source.Source, req source.Request) {
	t.Helper()

	if src.Name() == "" {
		t.Error("Name is empty: the dashboard and the logs identify the provider by it")
	}

	first, err := src.Fetch(req)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if first == nil || len(first.Transactions) == 0 {
		t.Fatal("the fixtures produced no transactions, so nothing was checked")
	}

	seen := map[string]bool{}
	for i, tx := range first.Transactions {
		if err := tx.Validate(); err != nil {
			t.Errorf("transaction %d: %v", i, err)
		}
		if seen[tx.ID] {
			t.Errorf("transaction %d: id %q appears twice in one fetch; Ratio would keep one", i, tx.ID)
		}
		seen[tx.ID] = true
		if tx.Metadata != nil {
			if _, err := json.Marshal(tx.Metadata); err != nil {
				t.Errorf("transaction %d: Metadata cannot be encoded: %v", i, err)
			}
		}
	}
	if first.Raw != nil && len(first.Raw) != len(first.Transactions) {
		t.Errorf("Raw has %d entries for %d transactions: they must line up", len(first.Raw), len(first.Transactions))
	}

	second, err := src.Fetch(req)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if len(second.Transactions) != len(first.Transactions) {
		t.Fatalf("a second fetch returned %d transactions, the first %d", len(second.Transactions), len(first.Transactions))
	}
	for i := range first.Transactions {
		if first.Transactions[i].ID != second.Transactions[i].ID {
			t.Errorf("transaction %d changed id between fetches: %q then %q. Ratio would take it for a new movement every time",
				i, first.Transactions[i].ID, second.Transactions[i].ID)
		}
	}

	future := req
	future.From = "2999-01-01"
	if _, err := src.Fetch(future); err != nil {
		t.Errorf("asking from a day with nothing on it failed: %v", err)
	}
}
