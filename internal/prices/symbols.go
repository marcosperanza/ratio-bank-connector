// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package prices

import "strings"

// Ticker is a Ratio ticker split into base symbol and market. Ratio writes
// US tickers bare (AAPL, BRK.B) and European ones as BASE.EXCHANGE (VWCE.DE).
type Ticker struct {
	Raw    string
	Base   string
	Market string // canonical market key, "" when the ticker has no known suffix
}

// ratioSuffixes maps the suffixes Ratio and common brokers use to a canonical
// market key.
var ratioSuffixes = map[string]string{
	"L": "uk", "LON": "uk", "LSE": "uk",
	"DE": "de", "DEX": "de", "XETR": "de",
	"PA": "fr", "PAR": "fr",
	"MI": "it", "MIL": "it",
	"AS": "nl", "AMS": "nl",
	"SW": "ch", "SWI": "ch", "SIX": "ch",
	"MC": "es", "MCE": "es",
	"US": "us",
}

// ParseTicker splits on the last dot only when the suffix is a known market;
// BRK.B keeps its class letter in the base symbol.
func ParseTicker(raw string) Ticker {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	t := Ticker{Raw: raw, Base: raw}
	if i := strings.LastIndex(raw, "."); i > 0 {
		if m, ok := ratioSuffixes[raw[i+1:]]; ok {
			t.Base, t.Market = raw[:i], m
		}
	}
	return t
}

// marketSuffixes lists, per provider and market, the spellings to try in
// order. Providers disagree (Yahoo ".MI", Alpha Vantage ".MIL") and some do
// not cover a market at all, which is why this is data and not a rule.
// An empty suffix means the bare symbol.
var marketSuffixes = map[string]map[string][]string{
	"yahoo": {
		"us": {""}, "uk": {".L"}, "de": {".DE"}, "fr": {".PA"},
		"it": {".MI"}, "nl": {".AS"}, "ch": {".SW"}, "es": {".MC"},
	},
	"alphavantage": {
		"us": {""}, "uk": {".LON"}, "de": {".DEX", ".DE"}, "fr": {".PAR"},
		"it": {".MIL"}, "nl": {".AMS"}, "ch": {".SWI"},
	},
}

// twelveDataExchange maps a market to Twelve Data's exchange field.
var twelveDataExchange = map[string][]string{
	"us": {""}, "uk": {"LSE"}, "de": {"XETR"}, "fr": {"Euronext"},
	"it": {"MIL"}, "nl": {"Euronext"}, "ch": {"SIX"}, "es": {"BME"},
}

// Overrides pin a spelling per ticker and provider, e.g. a fund whose
// listing the table gets wrong: {"VWCE.DE": {"yahoo": "VWCE.DE"}}.
type Overrides map[string]map[string]string

// symbolCandidates builds the spellings for suffix-style providers. The
// override goes first, then the learned spelling, then the ticker exactly as
// the user wrote it (the user picked that spelling: LEMA.DE means LEMA.DE),
// and only then the table's adaptations to this provider.
func symbolCandidates(provider string, t Ticker, ov Overrides, learned string) []Candidate {
	var out []Candidate
	seen := map[string]bool{}
	add := func(sym string) {
		if sym != "" && !seen[sym] {
			seen[sym] = true
			out = append(out, Candidate{Symbol: sym})
		}
	}
	add(ov[t.Raw][provider])
	add(learned)
	add(t.Raw)
	if t.Market == "" {
		add(t.Base)
		return out
	}
	for _, suffix := range marketSuffixes[provider][t.Market] {
		add(t.Base + suffix)
	}
	return out
}

// exchangeCandidates is the Twelve Data variant: bare symbol plus exchange.
func exchangeCandidates(t Ticker, ov Overrides, learned string) []Candidate {
	var out []Candidate
	seen := map[string]bool{}
	add := func(c Candidate) {
		if c.Symbol != "" && !seen[c.String()] {
			seen[c.String()] = true
			out = append(out, c)
		}
	}
	if sym, ex, ok := strings.Cut(ov[t.Raw]["twelvedata"], "@"); ok {
		add(Candidate{Symbol: sym, Exchange: ex})
	} else {
		add(Candidate{Symbol: sym})
	}
	if sym, ex, ok := strings.Cut(learned, "@"); ok {
		add(Candidate{Symbol: sym, Exchange: ex})
	} else {
		add(Candidate{Symbol: sym})
	}
	add(Candidate{Symbol: t.Raw})
	if t.Market == "" {
		add(Candidate{Symbol: t.Base})
		return out
	}
	for _, ex := range twelveDataExchange[t.Market] {
		add(Candidate{Symbol: t.Base, Exchange: ex})
	}
	return out
}
