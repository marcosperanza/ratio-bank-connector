// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package setup holds what the terminal wizard and the local settings UI
// share: connectivity checks for every source and a validation pass over the
// whole configuration. Neither front end duplicates this logic.
package setup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/prices"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
)

// Result is one check outcome a front end can show as-is.
type Result struct {
	OK      bool     `json:"ok"`
	Message string   `json:"message"`
	Items   []string `json:"items,omitempty"` // e.g. account names found
}

func fail(format string, args ...any) Result { return Result{Message: fmt.Sprintf(format, args...)} }
func ok(msg string, items ...string) Result  { return Result{OK: true, Message: msg, Items: items} }

// ValidateRatioURL accepts HTTPS, or HTTP only for a local Ratio.
func ValidateRatioURL(raw string) error {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	switch {
	case strings.HasPrefix(u, "https://") && len(u) > len("https://"):
		return nil
	case strings.HasPrefix(u, "http://localhost") || strings.HasPrefix(u, "http://127.0.0.1"):
		return nil
	}
	return errors.New("the Ratio URL must start with https:// (http:// is allowed only for localhost)")
}

// CheckRatio asks Ratio for this connector's configuration, which proves the
// URL and API key work, and reports how many bank accounts are waiting.
func CheckRatio(url, apiKey string) Result {
	if err := ValidateRatioURL(url); err != nil {
		return fail("%v", err)
	}
	if strings.TrimSpace(apiKey) == "" {
		return fail("the API key is empty: create one in Ratio under Connector")
	}
	poll, err := ratio.PollConfig(url, apiKey)
	if err != nil {
		return fail("could not reach Ratio with this key: %v", err)
	}
	return ok(fmt.Sprintf("Connected to Ratio (%d bank account(s) configured)", len(poll.BankAccounts)))
}

// CheckEnableBanking validates the credentials locally and, when network is
// true, asks Enable Banking for the Italian institutions.
func CheckEnableBanking(appID, pemPath, pemB64 string, network bool) Result {
	if strings.TrimSpace(appID) == "" {
		return fail("the Enable Banking application id is empty")
	}
	client, err := enablebanking.NewClient(appID, pemPath, pemB64)
	if err != nil {
		return fail("the private key could not be read: %v", err)
	}
	if !network {
		return ok("Enable Banking key is valid")
	}
	banks, err := client.ListASPSPs("IT")
	if err != nil {
		return fail("Enable Banking rejected the credentials: %v", err)
	}
	return ok(fmt.Sprintf("Enable Banking answered (%d Italian institutions)", len(banks)))
}

// CheckPriceProvider makes one real request (the AAPL daily history), so a
// wrong key or an exhausted quota shows up now and not at 3 am.
func CheckPriceProvider(ctx context.Context, name, apiKey string) Result {
	hc := &http.Client{Timeout: 30 * time.Second}
	var p prices.Provider
	switch name {
	case "yahoo":
		p = &prices.Yahoo{BaseURL: "https://query1.finance.yahoo.com", HTTP: hc}
	case "twelvedata":
		p = &prices.TwelveData{BaseURL: "https://api.twelvedata.com", APIKey: apiKey, HTTP: hc}
	case "alphavantage":
		p = &prices.AlphaVantage{BaseURL: "https://www.alphavantage.co", APIKey: apiKey, HTTP: hc}
	default:
		return fail("unknown price provider %q", name)
	}
	if name != "yahoo" && strings.TrimSpace(apiKey) == "" {
		return fail("%s needs an API key", name)
	}
	cands := p.Candidates(prices.ParseTicker("AAPL"))
	if len(cands) == 0 {
		return fail("%s has no spelling for the test ticker", name)
	}
	bars, err := p.Fetch(ctx, cands[0], time.Now().AddDate(0, 0, -10))
	switch {
	case err == nil:
		return ok(fmt.Sprintf("%s answered with %d daily prices", name, len(bars)))
	case errors.Is(err, prices.ErrAuth):
		return fail("%s rejected the API key", name)
	case errors.Is(err, prices.ErrQuota):
		return Result{OK: true, Message: name + " accepted the key but its quota is used up for now"}
	default:
		return fail("%s: %v", name, err)
	}
}
