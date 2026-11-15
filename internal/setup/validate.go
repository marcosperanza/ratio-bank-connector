// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"fmt"
	"strings"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
)

// Problem is a configuration issue; Blocking ones stop the connector.
type Problem struct {
	Field    string `json:"field"`
	Message  string `json:"message"`
	Blocking bool   `json:"blocking"`
}

// Validate checks a configuration offline. It is what both front ends run
// before saving and what the doctor runs on an installed connector.
func Validate(cfg *config.Config) []Problem {
	var out []Problem
	add := func(field, msg string, blocking bool) { out = append(out, Problem{field, msg, blocking}) }

	if cfg.Mode == "ratio_managed" {
		if err := ValidateRatioURL(cfg.RatioManaged.URL); err != nil {
			add("ratio_managed.url", err.Error(), true)
		}
		if cfg.RatioManaged.APIKey == "" {
			add("ratio_managed.api_key", "the Ratio API key is required", true)
		}
	}
	if cfg.Sync.IntervalHours <= 0 || cfg.Sync.LookbackDays <= 0 {
		add("sync", "sync intervals must be positive", true)
	}

	sources := 0
	if cfg.EnableBankingConfigured() {
		sources++
		if cfg.EnableBanking.PrivateKeyPath == "" && cfg.EnableBanking.PrivateKeyB64 == "" {
			add("enable_banking.private_key_path", "Enable Banking needs its private key", true)
		}
	}
	for i, sf := range cfg.SimpleFIN {
		sources++
		field := fmt.Sprintf("simplefin[%d]", i)
		if sf.Account == "" {
			continue // a saved connection waiting for an account to map
		}
		if sf.AccessURL == "" && sf.AccessURLEnv == "" {
			add(field, "a SimpleFIN entry needs access_url or access_url_env", true)
		} else if sf.AccessURL != "" && !strings.HasPrefix(sf.AccessURL, "https://") {
			add(field+".access_url", "the SimpleFIN access URL must use HTTPS", true)
		}
	}
	accounts := map[string]bool{}
	for i, f := range cfg.Folders {
		sources++
		field := fmt.Sprintf("folders[%d]", i)
		switch {
		case strings.TrimSpace(f.Account) == "":
			add(field+".account", "a folder needs the Ratio account id", true)
		case accounts[f.Account]:
			add(field+".account", fmt.Sprintf("two folders read for the account %q", f.Account), true)
		}
		accounts[f.Account] = true
		if f.Path == "" {
			add(field+".path", "a folder needs a path", true)
		}
	}
	if sources == 0 {
		add("sources", "no bank source is configured: add Enable Banking, SimpleFIN or a folder", false)
	}

	if p := cfg.Prices; p.Enabled {
		if len(p.Providers) == 0 {
			add("prices.providers", "prices are on but no provider is configured", true)
		}
		for _, pp := range p.Providers {
			switch pp.Name {
			case "yahoo":
			case "twelvedata", "alphavantage":
				if pp.APIKey == "" {
					add("prices.providers", pp.Name+" needs an API key", true)
				}
			default:
				add("prices.providers", fmt.Sprintf("unknown price provider %q", pp.Name), true)
			}
		}
		if p.TickersFile == "" && cfg.Mode != "ratio_managed" {
			add("prices.tickers_file", "without Ratio, prices need a tickers file", true)
		}
	}
	return out
}

// Blocking reports whether any problem stops the connector from running.
func Blocking(problems []Problem) bool {
	for _, p := range problems {
		if p.Blocking {
			return true
		}
	}
	return false
}
