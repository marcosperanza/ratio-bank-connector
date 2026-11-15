// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package source is the contract between the connector core and whatever
// produces transactions: a bank API, a folder of exports, a mailbox, a plugin.
// The core schedules, encrypts and delivers; a source only fetches and
// normalizes. Nothing in here may reach the network of Ratio or hold its keys.
package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrAuthExpired is returned by a source whose access has lapsed and needs the
// user to authorize again. The core reports it so the dashboard can ask for it.
var ErrAuthExpired = errors.New("source authorization expired")

// Transaction is one movement in the shape every source produces.
type Transaction struct {
	// ID is stable across fetches of the same movement: Ratio deduplicates the
	// inbox on it, so a source that has no native identifier must derive one.
	ID string
	// BookingDate is a calendar day, YYYY-MM-DD.
	BookingDate string
	// Amount is signed: negative for money going out.
	Amount   float64
	Currency string

	Description string
	// RawText is everything the source said about the movement, for rules
	// that need more than the description.
	RawText string
	// Counterparty is the merchant or the other party's name.
	Counterparty     string
	CounterpartyIBAN string

	// Metadata is kept inside the encrypted blob, never beside it. A source
	// puts its provider-specific evidence under its own key.
	Metadata map[string]any
}

// Request says what to fetch.
type Request struct {
	// Account is the source's own reference for the account or channel.
	Account string
	// From is the first day wanted, YYYY-MM-DD. Sources may return earlier
	// movements; the inbox deduplicates them.
	From string
}

// Archiver writes a local copy of what a fetch returned, for sources that
// keep one. The core decides whether and where; the source decides the format.
type Archiver func(dir, account string, includeRaw bool) (appended int, err error)

// Result is what a fetch returns.
type Result struct {
	Transactions []Transaction
	// Raw is the provider's own representation, same length and order as
	// Transactions, or nil when the source has none.
	Raw []json.RawMessage
	// Archive is nil when the source has nothing to archive.
	Archive Archiver
}

// Source produces transactions for one kind of provider.
type Source interface {
	// Name identifies the source in logs and in Ratio ("enablebanking").
	Name() string
	Fetch(req Request) (*Result, error)
}

// Validate rejects a movement the core must not encrypt and send: Ratio would
// store it, and a wrong amount or date cannot be told apart from a right one
// once it is ciphertext. A source that builds Transactions from files or from
// a plugin's output depends on this being the same for everyone.
func (t Transaction) Validate() error {
	if t.ID == "" {
		return errors.New("transaction has no id")
	}
	if _, err := time.Parse("2006-01-02", t.BookingDate); err != nil {
		return fmt.Errorf("transaction %q has an invalid booking date", t.ID)
	}
	if math.IsNaN(t.Amount) || math.IsInf(t.Amount, 0) {
		return fmt.Errorf("transaction %q has an invalid amount", t.ID)
	}
	return nil
}
