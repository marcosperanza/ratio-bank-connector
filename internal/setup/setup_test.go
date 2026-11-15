// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
)

func TestValidateRatioURL(t *testing.T) {
	for url, wantErr := range map[string]bool{
		"https://www.ratio-fin.com": false, "http://localhost:3000": false,
		"http://ratio.example": true, "ratio.example": true, "": true,
	} {
		if err := ValidateRatioURL(url); (err != nil) != wantErr {
			t.Errorf("%q: %v", url, err)
		}
	}
}

func TestValidateAcceptsSeveralSourcesTogether(t *testing.T) {
	cfg := &config.Config{
		Mode:          "ratio_managed",
		RatioManaged:  config.RatioManagedConfig{URL: "https://www.ratio-fin.com", APIKey: "k"},
		Sync:          config.SyncConfig{IntervalHours: 6, LookbackDays: 30},
		EnableBanking: config.EnableBankingConfig{AppID: "app", PrivateKeyPath: "/k.pem"},
		SimpleFIN:     []config.SimpleFINConfig{{Account: "acc-1", AccessURL: "https://u:p@bridge/simplefin"}},
		Folders:       []config.FolderConfig{{Account: "acc-2", Path: "/data/casa"}},
		Prices:        config.PricesConfig{Enabled: true, Providers: []config.PriceProviderConfig{{Name: "yahoo"}}},
	}
	if p := Validate(cfg); len(p) != 0 {
		t.Fatalf("%+v", p)
	}
}

func TestValidateReportsWhatIsMissing(t *testing.T) {
	cfg := &config.Config{
		Mode:    "ratio_managed",
		Sync:    config.SyncConfig{IntervalHours: 6, LookbackDays: 30},
		Folders: []config.FolderConfig{{Account: "A", Path: "/x"}, {Account: "A", Path: "/y"}},
		Prices:  config.PricesConfig{Enabled: true, Providers: []config.PriceProviderConfig{{Name: "twelvedata"}}},
	}
	problems := Validate(cfg)
	fields := map[string]bool{}
	for _, p := range problems {
		fields[p.Field] = true
	}
	for _, want := range []string{"ratio_managed.url", "ratio_managed.api_key", "folders[1].account", "prices.providers"} {
		if !fields[want] {
			t.Errorf("missing problem for %s: %+v", want, problems)
		}
	}
	if !Blocking(problems) {
		t.Fatal("expected blocking problems")
	}
}
