// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package source

import (
	"math"
	"testing"
)

func valid() Transaction {
	return Transaction{ID: "t1", BookingDate: "2026-09-14", Amount: -12.34, Currency: "EUR"}
}

func TestValidateAcceptsAWellFormedTransaction(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsWhatCannotBeToldApartOnceEncrypted(t *testing.T) {
	cases := map[string]func(*Transaction){
		"no id":             func(tx *Transaction) { tx.ID = "" },
		"empty date":        func(tx *Transaction) { tx.BookingDate = "" },
		"date with a time":  func(tx *Transaction) { tx.BookingDate = "2026-09-14T10:00:00Z" },
		"day-first date":    func(tx *Transaction) { tx.BookingDate = "14/09/2026" },
		"not a number":      func(tx *Transaction) { tx.Amount = math.NaN() },
		"infinite amount":   func(tx *Transaction) { tx.Amount = math.Inf(1) },
		"negative infinity": func(tx *Transaction) { tx.Amount = math.Inf(-1) },
	}
	for name, mutate := range cases {
		tx := valid()
		mutate(&tx)
		if err := tx.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
