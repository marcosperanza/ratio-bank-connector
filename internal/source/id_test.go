// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package source

import "testing"

func TestStableIDIgnoresCaseAndSpacingOfTheDescription(t *testing.T) {
	a := StableID("csv", "acct", "2026-09-14", -1.2, "eur", "Caffè  BAR Roma", 0)
	b := StableID("csv", "acct", "2026-09-14", -1.2, "EUR", " caffè bar roma ", 0)
	if a != b {
		t.Fatalf("%q != %q", a, b)
	}
}

func TestStableIDSeparatesWhatDiffers(t *testing.T) {
	base := StableID("csv", "acct", "2026-09-14", -1.2, "EUR", "x", 0)
	for name, other := range map[string]string{
		"ordinal":  StableID("csv", "acct", "2026-09-14", -1.2, "EUR", "x", 1),
		"account":  StableID("csv", "other", "2026-09-14", -1.2, "EUR", "x", 0),
		"date":     StableID("csv", "acct", "2026-09-15", -1.2, "EUR", "x", 0),
		"amount":   StableID("csv", "acct", "2026-09-14", -1.3, "EUR", "x", 0),
		"currency": StableID("csv", "acct", "2026-09-14", -1.2, "USD", "x", 0),
		"prefix":   StableID("ofx", "acct", "2026-09-14", -1.2, "EUR", "x", 0),
	} {
		if other == base {
			t.Errorf("%s did not change the id", name)
		}
	}
}
