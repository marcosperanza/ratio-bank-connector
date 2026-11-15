// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package ui serves the local settings page. It listens on 127.0.0.1 only,
// requires a per-run token, and never sends stored secrets back to the
// browser: it reports whether one is set, and an empty field keeps it.
package ui

import (
	"strings"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
)

type ProviderView struct {
	Name       string  `json:"name"`
	APIKeySet  bool    `json:"apiKeySet"`
	APIKey     *string `json:"apiKey,omitempty"` // update only
	DailyLimit int     `json:"dailyLimit"`
	GapSeconds int     `json:"gapSeconds"`
}

// View is the configuration as the page sees it, and the shape it sends back.
type View struct {
	Ratio struct {
		URL                 string  `json:"url"`
		APIKeySet           bool    `json:"apiKeySet"`
		APIKey              *string `json:"apiKey,omitempty"` // update only
		PollIntervalSeconds int     `json:"pollIntervalSeconds"`
	} `json:"ratio"`
	EnableBanking struct {
		Configured     bool   `json:"configured"`
		AppID          string `json:"appId"`
		PrivateKeyPath string `json:"privateKeyPath"`
		RedirectURL    string `json:"redirectUrl"`
		CallbackCert   string `json:"callbackCert"`
		CallbackKey    string `json:"callbackKey"`
	} `json:"enableBanking"`
	// Sources lists what this machine reads besides Enable Banking: they are set in the configuration file
	// (see docs/PROVIDERS.md), the page only shows how many there are.
	Sources struct {
		SimpleFIN int `json:"simplefin"`
		Folders   int `json:"folders"`
	} `json:"sources"`
	Sync struct {
		IntervalHours int `json:"intervalHours"`
		LookbackDays  int `json:"lookbackDays"`
	} `json:"sync"`
	Prices struct {
		Enabled       bool                   `json:"enabled"`
		TickersFile   string                 `json:"tickersFile"`
		IntervalHours int                    `json:"intervalHours"`
		Providers     []ProviderView         `json:"providers"`
		Overrides     []config.PriceOverride `json:"overrides"`
	} `json:"prices"`
}

// ViewOf hides every secret.
func ViewOf(cfg *config.Config) View {
	var v View
	v.Ratio.URL = cfg.RatioManaged.URL
	v.Ratio.APIKeySet = cfg.RatioManaged.APIKey != ""
	v.Ratio.PollIntervalSeconds = cfg.RatioManaged.PollIntervalSeconds
	v.EnableBanking.Configured = cfg.EnableBankingConfigured()
	v.EnableBanking.AppID = cfg.EnableBanking.AppID
	v.EnableBanking.PrivateKeyPath = cfg.EnableBanking.PrivateKeyPath
	v.EnableBanking.RedirectURL = cfg.EnableBanking.RedirectURL
	v.EnableBanking.CallbackCert = cfg.EnableBanking.CallbackCert
	v.EnableBanking.CallbackKey = cfg.EnableBanking.CallbackKey
	v.Sources.SimpleFIN = len(cfg.SimpleFIN)
	v.Sources.Folders = len(cfg.Folders)
	v.Sync.IntervalHours = cfg.Sync.IntervalHours
	v.Sync.LookbackDays = cfg.Sync.LookbackDays
	v.Prices.Enabled = cfg.Prices.Enabled
	v.Prices.TickersFile = cfg.Prices.TickersFile
	v.Prices.IntervalHours = cfg.Prices.IntervalHours
	v.Prices.Overrides = append([]config.PriceOverride{}, cfg.Prices.Overrides...)
	v.Prices.Providers = []ProviderView{} // JSON [] and not null: the page iterates it
	for _, p := range cfg.Prices.Providers {
		v.Prices.Providers = append(v.Prices.Providers, ProviderView{Name: p.Name, APIKeySet: p.APIKey != "", DailyLimit: p.DailyLimit, GapSeconds: p.GapSeconds})
	}
	return v
}

// Apply merges an update into cfg. A nil or empty secret keeps the stored one. Folders and SimpleFIN are not
// touched: they are not edited on the page.
func Apply(cfg *config.Config, v View) {
	cfg.Mode = "ratio_managed"
	cfg.RatioManaged.URL = strings.TrimSpace(v.Ratio.URL)
	if v.Ratio.APIKey != nil && strings.TrimSpace(*v.Ratio.APIKey) != "" {
		cfg.RatioManaged.APIKey = strings.TrimSpace(*v.Ratio.APIKey)
	}
	if v.Ratio.PollIntervalSeconds > 0 {
		cfg.RatioManaged.PollIntervalSeconds = v.Ratio.PollIntervalSeconds
	}

	if v.EnableBanking.Configured {
		cfg.EnableBanking.AppID = strings.TrimSpace(v.EnableBanking.AppID)
		if p := strings.TrimSpace(v.EnableBanking.PrivateKeyPath); p != "" {
			cfg.EnableBanking.PrivateKeyPath = p
			cfg.EnableBanking.PrivateKeyB64 = ""
		}
		cfg.EnableBanking.RedirectURL = strings.TrimSpace(v.EnableBanking.RedirectURL)
		cfg.EnableBanking.CallbackCert = strings.TrimSpace(v.EnableBanking.CallbackCert)
		cfg.EnableBanking.CallbackKey = strings.TrimSpace(v.EnableBanking.CallbackKey)
	} else {
		cfg.EnableBanking = config.EnableBankingConfig{}
	}

	if v.Sync.IntervalHours > 0 {
		cfg.Sync.IntervalHours = v.Sync.IntervalHours
	}
	if v.Sync.LookbackDays > 0 {
		cfg.Sync.LookbackDays = v.Sync.LookbackDays
	}

	stored := map[string]config.PriceProviderConfig{}
	for _, p := range cfg.Prices.Providers {
		stored[p.Name] = p
	}
	cfg.Prices.Enabled = v.Prices.Enabled
	cfg.Prices.TickersFile = strings.TrimSpace(v.Prices.TickersFile)
	if v.Prices.IntervalHours > 0 {
		cfg.Prices.IntervalHours = v.Prices.IntervalHours
	}
	cfg.Prices.Overrides = append([]config.PriceOverride{}, v.Prices.Overrides...)
	cfg.Prices.Providers = nil
	for _, p := range v.Prices.Providers {
		pc := config.PriceProviderConfig{Name: p.Name, APIKey: stored[p.Name].APIKey, DailyLimit: p.DailyLimit, GapSeconds: p.GapSeconds}
		if p.APIKey != nil && strings.TrimSpace(*p.APIKey) != "" {
			pc.APIKey = strings.TrimSpace(*p.APIKey)
		}
		cfg.Prices.Providers = append(cfg.Prices.Providers, pc)
	}
	if cfg.Prices.Dir == "" {
		cfg.Prices.Dir = config.DefaultDataDir() + "/prices"
	}
	if cfg.Prices.SealingKeyPath == "" {
		cfg.Prices.SealingKeyPath = config.DefaultDataDir() + "/sealing-key.pem"
	}
	if cfg.Prices.IntervalHours <= 0 {
		cfg.Prices.IntervalHours = 24
	}
}
