// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package prices

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/crypto"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
)

// KindWatchlist is the sealed document kind the web uses for the tickers of
// the open positions.
const KindWatchlist = "watchlist"

// DocumentFetcher is the part of the Ratio client the source needs.
type DocumentFetcher interface {
	SealedDocuments(kind string) ([]ratio.SealedDocument, error)
}

// SealedSource reads the watchlist Ratio keeps for this connector. The server
// holds ciphertext only: the connector opens it with its own private key.
//
// The last good list per workspace is saved locally, so an unreachable Ratio
// or an older envelope never empties the list, and a lower sequence number
// than one already applied is ignored (replay).
type SealedSource struct {
	Fetcher   DocumentFetcher
	Key       *rsa.PrivateKey
	CachePath string
	Log       func(format string, args ...any)
}

// Entry is one open position of the user as the browser sealed it: the ticker the user chose, and what the
// connector needs to hand the prices back (the ISIN and the currency of the security).
type Entry struct {
	Ticker   string `json:"ticker"`
	ISIN     string `json:"isin,omitempty"`
	Currency string `json:"currency,omitempty"`
}

type watchlistPayload struct {
	Tickers []Entry `json:"tickers"`
}

type cachedWatchlist struct {
	Seq     int      `json:"seq"`
	Tickers []string `json:"tickers"`
	Entries []Entry  `json:"entries,omitempty"`
}

func (s *SealedSource) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

func (s *SealedSource) load() map[string]cachedWatchlist {
	cache := map[string]cachedWatchlist{}
	if data, err := os.ReadFile(s.CachePath); err == nil {
		_ = json.Unmarshal(data, &cache)
	}
	return cache
}

func (s *SealedSource) save(cache map[string]cachedWatchlist) {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err == nil {
		err = os.WriteFile(s.CachePath, data, 0o600)
	}
	if err != nil {
		s.logf("prices: cannot save the watchlist cache: %v", err)
	}
}

func (s *SealedSource) Tickers(context.Context) ([]string, error) {
	cache := s.load()
	docs, err := s.Fetcher.SealedDocuments(KindWatchlist)
	if err != nil {
		s.logf("prices: cannot read the Ratio watchlist, using the last one: %v", err)
	} else {
		for _, doc := range docs {
			s.apply(cache, doc)
		}
		s.save(cache)
	}
	seen := map[string]bool{}
	var out []string
	for _, wl := range cache {
		for _, t := range wl.Tickers {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *SealedSource) apply(cache map[string]cachedWatchlist, doc ratio.SealedDocument) {
	var env crypto.SealedEnvelope
	if err := json.Unmarshal([]byte(doc.Envelope), &env); err != nil {
		s.logf("prices: unreadable watchlist envelope for workspace %s", doc.WorkspaceID)
		return
	}
	// The row metadata is the server's claim; the envelope is authenticated.
	if env.WorkspaceID != doc.WorkspaceID || env.Kind != KindWatchlist || env.Seq != doc.Seq {
		s.logf("prices: watchlist row does not match its envelope for workspace %s, ignored", doc.WorkspaceID)
		return
	}
	prev := cache[doc.WorkspaceID]
	if env.Seq < prev.Seq {
		s.logf("prices: older watchlist (seq %d < %d) for workspace %s, ignored", env.Seq, prev.Seq, doc.WorkspaceID)
		return
	}
	plain, err := crypto.OpenSealed(s.Key, env)
	if err != nil {
		// Typically sealed to a previous key: approve the new fingerprint in Ratio.
		s.logf("prices: cannot open the watchlist for workspace %s: %v", doc.WorkspaceID, err)
		return
	}
	var payload watchlistPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		s.logf("prices: malformed watchlist for workspace %s", doc.WorkspaceID)
		return
	}
	var tickers []string
	var entries []Entry
	for _, t := range payload.Tickers {
		if v := strings.ToUpper(strings.TrimSpace(t.Ticker)); v != "" {
			tickers = append(tickers, v)
			entries = append(entries, Entry{Ticker: v, ISIN: strings.ToUpper(strings.TrimSpace(t.ISIN)), Currency: strings.ToUpper(strings.TrimSpace(t.Currency))})
		}
	}
	cache[doc.WorkspaceID] = cachedWatchlist{Seq: env.Seq, Tickers: tickers, Entries: entries}
}

// Entries returns the last good watchlist of every workspace, as saved by Tickers. An entry saved before the
// ISIN and the currency were kept has neither.
func (s *SealedSource) Entries() map[string][]Entry {
	out := map[string][]Entry{}
	for workspace, wl := range s.load() {
		if len(wl.Entries) > 0 {
			out[workspace] = wl.Entries
			continue
		}
		for _, t := range wl.Tickers {
			out[workspace] = append(out[workspace], Entry{Ticker: t})
		}
	}
	return out
}

// UnionSource merges several sources, so a local file and the Ratio
// watchlist can be used together. A failing source is skipped, not fatal,
// unless every source fails.
type UnionSource []TickerSource

func (u UnionSource) Tickers(ctx context.Context) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	var errs []error
	for _, src := range u {
		tickers, err := src.Tickers(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, t := range tickers {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	if len(errs) == len(u) && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}
