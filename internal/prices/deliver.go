// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package prices

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/crypto"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
)

// maxPointsPerItem keeps one sealed item well under the server's size limit: a first download of twenty years
// of daily closes goes out in a few items, which the device merges by year.
const maxPointsPerItem = 4000

// Problem codes the device understands (lib/connector-price-inbox.ts in Ratio).
const (
	ProblemNotFound    = "NOT_FOUND"
	ProblemQuota       = "QUOTA"
	ProblemAuth        = "AUTH"
	ProblemUnsupported = "UNSUPPORTED"
	ProblemNoISIN      = "NO_ISIN"
	ProblemNoCurrency  = "NO_CURRENCY"
	ProblemError       = "ERROR"
)

// EntrySource says which tickers each workspace holds, with their ISIN and currency.
type EntrySource interface {
	Entries() map[string][]Entry
}

// PriceSender queues sealed items in Ratio.
type PriceSender interface {
	PushPrices(workspaceID string, items []ratio.PriceInboxItem) (*ratio.PushResult, error)
}

// Deliverer hands to Ratio what a download run found. Prices go out as the closes newer than the last one
// delivered for that workspace and ticker (so a failed push is retried by the next run, and an unchanged
// ticker sends nothing); a ticker that could not be served goes out as a problem. Everything is sealed to the
// user's public key: the server learns neither the tickers nor the prices.
type Deliverer struct {
	Entries EntrySource
	Store   Store
	State   *State
	Sender  PriceSender
	// PublicKey returns the user's public key (JWK), which only the owner's devices can open.
	PublicKey func() (string, error)
	// HMACKey keys the dedup hash; the connector's API key does.
	HMACKey string
	Log     func(format string, args ...any)
}

func (d *Deliverer) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(format, args...)
	}
}

type pricesItem struct {
	Kind     string   `json:"kind"`
	Ticker   string   `json:"ticker"`
	ISIN     string   `json:"isin"`
	Currency string   `json:"currency"`
	Source   string   `json:"source"`
	Adjusted bool     `json:"adjusted"`
	Points   [][2]any `json:"points"`
}

type problemItem struct {
	Kind     string `json:"kind"`
	Ticker   string `json:"ticker"`
	Code     string `json:"code"`
	Provider string `json:"provider,omitempty"`
}

// problemCode maps an engine error to what the device shows.
func problemCode(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return ProblemNotFound
	case errors.Is(err, ErrQuota):
		return ProblemQuota
	case errors.Is(err, ErrAuth):
		return ProblemAuth
	case errors.Is(err, ErrUnsupported):
		return ProblemUnsupported
	default:
		return ProblemError
	}
}

func (d *Deliverer) seal(jwk string, value any) (ratio.PriceInboxItem, error) {
	key, blob, iv, plain, err := crypto.EncryptJSONHybrid(jwk, value)
	if err != nil {
		return ratio.PriceInboxItem{}, err
	}
	mac := hmac.New(sha256.New, []byte(d.HMACKey))
	mac.Write(plain)
	return ratio.PriceInboxItem{EncryptedBlob: blob, EncryptionIV: iv, EncryptedSessionKey: key, DedupHash: hex.EncodeToString(mac.Sum(nil))}, nil
}

// Deliver sends the prices and the problems of one run. It never fails the run: what cannot be sent now is
// sent by the next one.
func (d *Deliverer) Deliver(ctx context.Context, outcomes []Outcome) {
	entries := d.Entries.Entries()
	if len(entries) == 0 {
		return
	}
	jwk, err := d.PublicKey()
	if err != nil || jwk == "" {
		d.logf("prices: cannot deliver, no public key from Ratio: %v", err)
		return
	}
	byTicker := map[string]Outcome{}
	for _, o := range outcomes {
		byTicker[o.Ticker] = o
	}

	workspaces := make([]string, 0, len(entries))
	for w := range entries {
		workspaces = append(workspaces, w)
	}
	sort.Strings(workspaces)

	for _, workspace := range workspaces {
		if ctx.Err() != nil {
			return
		}
		var items []ratio.PriceInboxItem
		delivered := map[string]string{} // state key -> newest date, written only if the push succeeds
		add := func(value any) bool {
			item, err := d.seal(jwk, value)
			if err != nil {
				d.logf("prices: cannot seal an item: %v", err)
				return false
			}
			items = append(items, item)
			return true
		}
		problem := func(e Entry, code, provider string) {
			add(problemItem{Kind: "problem", Ticker: e.Ticker, Code: code, Provider: provider})
		}

		for _, e := range entries[workspace] {
			o, ran := byTicker[e.Ticker]
			switch {
			case ran && o.Err != nil:
				problem(e, problemCode(o.Err), o.Provider)
				continue
			case !ran:
				// Not part of this run (the list changed meanwhile): nothing new to say about it.
				continue
			}
			if e.ISIN == "" {
				problem(e, ProblemNoISIN, "")
				continue
			}
			if e.Currency == "" {
				problem(e, ProblemNoCurrency, "")
				continue
			}
			stateKey := workspace + "|" + e.Ticker
			bars, err := d.Store.Read(e.Ticker)
			if err != nil {
				d.logf("prices: %s: cannot read the stored closes: %v", e.Ticker, err)
				continue
			}
			var points [][2]any
			newest := d.State.Delivered[stateKey]
			var dates []string
			for date := range bars {
				if date > d.State.Delivered[stateKey] {
					dates = append(dates, date)
				}
			}
			sort.Strings(dates)
			for _, date := range dates {
				close, err := strconv.ParseFloat(strings.TrimSpace(bars[date].Close), 64)
				if err != nil || close <= 0 {
					continue
				}
				points = append(points, [2]any{date, close})
				newest = date
			}
			for start := 0; start < len(points); start += maxPointsPerItem {
				end := min(start+maxPointsPerItem, len(points))
				add(pricesItem{Kind: "prices", Ticker: e.Ticker, ISIN: e.ISIN, Currency: e.Currency, Source: o.Provider, Points: points[start:end]})
			}
			if len(points) > 0 {
				delivered[stateKey] = newest
			}
		}
		if len(items) == 0 {
			continue
		}
		res, err := d.Sender.PushPrices(workspace, items)
		if err != nil {
			d.logf("prices: cannot hand %d items to Ratio, the next run will try again: %v", len(items), err)
			continue
		}
		for k, v := range delivered {
			d.State.Delivered[k] = v
		}
		d.logf("prices: handed %d items to Ratio (%d queued, %d already there)", len(items), res.Queued, res.Duplicates)
	}
	_ = d.State.Save()
}
