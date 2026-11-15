// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package prices downloads daily price history for the tickers a user tracks
// in Ratio and keeps one CSV per ticker, so the connector (not the browser)
// is the actor that talks to market-data providers and holds their API keys.
package prices

import (
	"context"
	"errors"
	"time"
)

// Bar is one daily candle. Values stay strings so a CSV round-trip never
// changes the digits a provider returned.
type Bar struct {
	Date   string // YYYY-MM-DD
	Open   string
	High   string
	Low    string
	Close  string
	Volume string
}

// Candidate is one way to spell a ticker for a provider.
type Candidate struct {
	Symbol   string
	Exchange string // only used by providers that take it as a separate field
}

func (c Candidate) String() string {
	if c.Exchange == "" {
		return c.Symbol
	}
	return c.Symbol + "@" + c.Exchange
}

// Provider errors drive the engine: a missing symbol tries the next spelling,
// a quota or key problem moves on to the next provider.
var (
	ErrNotFound    = errors.New("symbol not found")
	ErrQuota       = errors.New("provider quota exhausted")
	ErrAuth        = errors.New("provider rejected the API key")
	ErrUnsupported = errors.New("provider has no spelling for this ticker")
)

type Provider interface {
	Name() string
	// Candidates lists the spellings to try, best first. Empty means the
	// provider cannot serve this ticker.
	Candidates(t Ticker) []Candidate
	// Fetch returns daily bars. since is the last stored date (zero = full
	// history); providers may return more than asked, the store merges by date.
	Fetch(ctx context.Context, c Candidate, since time.Time) ([]Bar, error)
}
