// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v3/internal/enablebanking"
)

func TestAppendTransactionsCanOmitRawProviderData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tx := enablebanking.Transaction{
		EntryReference:    "tx-1",
		BookingDate:       "2026-09-14",
		TransactionAmount: enablebanking.TransactionAmount{Amount: "1.00", Currency: "EUR"},
	}
	raw := []json.RawMessage{json.RawMessage(`{"entry_reference":"tx-1","sensitive":"provider payload"}`)}

	if _, err := AppendTransactionsWithOptions(dir, "account-1", []enablebanking.Transaction{tx}, raw, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "account-1", "transactions.csv")); err != nil {
		t.Fatalf("expected normalized CSV: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "account-1", "transactions_raw.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("raw provider archive must not exist, got: %v", err)
	}
}
