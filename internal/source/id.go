// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package source

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// StableID derives the identifier of a movement from what it says, for sources
// whose provider gives none. The same movement read twice, from two exports
// that overlap or from the same file on two days, must get the same id, because
// Ratio deduplicates its inbox on it.
//
// ordinal tells apart rows that are identical in everything else on one day:
// two coffees at the same price are two movements. Count them within one
// export, in the order the export lists them.
func StableID(prefix, account, bookingDate string, amount float64, currency, description string, ordinal int) string {
	key := fmt.Sprintf("%s|%s|%s|%.2f|%s|%s|%d",
		prefix, account, bookingDate, amount, strings.ToUpper(currency), normalizeText(description), ordinal)
	sum := sha256.Sum256([]byte(key))
	return prefix + ":" + hex.EncodeToString(sum[:])[:32]
}

func normalizeText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
