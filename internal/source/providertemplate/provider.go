// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package providertemplate is the starting point for a new provider. Copy this
// directory to internal/source/<yourprovider>, rename the package and the type,
// and replace the body of Fetch (and Begin/Complete if the provider needs the
// user's approval). The example reads a JSON file so that the template runs and
// is tested; keep that shape if it fits, or drop it.
//
// What a provider does: fetch from its own service and say what it found, as
// source.Transaction values. What it does not do: encrypt, talk to Ratio, keep
// the user's keys, or decide when to run. See docs/PROVIDERS.md.
package providertemplate

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
)

// Name identifies the provider. It is the value Ratio sends as the account's
// provider, so choose it once.
const Name = "example"

// Account is what this computer knows about one Ratio account: where to look,
// which credentials to use. Read it from the connector configuration; never
// send it to Ratio.
type Account struct {
	Path string
}

type Provider struct {
	accounts map[string]Account
}

func New(accounts map[string]Account) *Provider { return &Provider{accounts: accounts} }

func (*Provider) Name() string { return Name }

// Configured makes this a source.Local provider: the account is ready as soon
// as this computer has a setting for it. A provider that needs the user's
// approval implements source.Linker (Begin, Complete) instead of this.
func (p *Provider) Configured(accountID string) bool {
	_, ok := p.accounts[accountID]
	return ok
}

type record struct {
	ID          string  `json:"id"`
	Date        string  `json:"date"` // YYYY-MM-DD
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Description string  `json:"description"`
	Merchant    string  `json:"merchant"`
}

// Fetch returns the movements from req.From on. Rules the core depends on:
//   - IDs are stable: the same movement has the same ID every time it is read.
//     Use the provider's own identifier; derive one with source.StableID only
//     if it has none.
//   - Amount is signed, negative for money going out.
//   - BookingDate is YYYY-MM-DD.
//   - Returning movements older than req.From is fine; the inbox deduplicates.
//   - An expired authorization is source.ErrAuthExpired, wrapped if you like.
func (p *Provider) Fetch(req source.Request) (*source.Result, error) {
	acct, ok := p.accounts[req.Account]
	if !ok {
		return nil, fmt.Errorf("no setting for account %q", req.Account)
	}
	from, err := time.Parse("2006-01-02", req.From)
	if err != nil {
		return nil, fmt.Errorf("invalid from date %q", req.From)
	}
	data, err := os.ReadFile(acct.Path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", acct.Path, err)
	}
	var records []record
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse %s: %w", acct.Path, err)
	}

	var out []source.Transaction
	for _, r := range records {
		day, err := time.Parse("2006-01-02", r.Date)
		if err != nil || day.Before(from) {
			continue
		}
		currency := r.Currency
		if currency == "" {
			currency = "EUR"
		}
		out = append(out, source.Transaction{
			ID:           r.ID,
			BookingDate:  r.Date,
			Amount:       r.Amount,
			Currency:     currency,
			Description:  r.Description,
			Counterparty: r.Merchant,
			// Anything else the provider says goes here; it is encrypted with
			// the rest and never seen by Ratio.
			Metadata: map[string]any{"provider": map[string]any{"name": Name}},
		})
	}
	return &source.Result{Transactions: out}, nil
}
