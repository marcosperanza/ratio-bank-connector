// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/setup"
)

func passing() wizardChecks {
	okr := setup.Result{OK: true, Message: "fine"}
	return wizardChecks{
		Ratio: func(string, string) setup.Result { return okr },
		EB:    func(string, string) setup.Result { return okr },
		Price: func(string, string) setup.Result { return okr },
	}
}

// runWizardSeeded starts from an existing configuration file, as a user who set up folders or SimpleFIN by hand has.
func runWizardSeeded(t *testing.T, seed, input string, secrets []string, installs *int) (string, *config.Config, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if seed != "" {
		if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	next := 0
	w := &wizard{
		in:      bufio.NewReader(strings.NewReader(input)),
		out:     &out,
		cfgPath: path,
		checks:  passing(),
		secret:  func() string { v := secrets[next]; next++; return v },
		installService: func(string) error {
			*installs++
			return nil
		},
	}
	w.linker = func(cfg *config.Config) *ebLinker { return testLinker(t, w.in, &out, cfg) }
	if err := w.run(); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return out.String(), cfg, path
}

// Answers, in order: Ratio URL; Enable Banking y + app id + pem; interval; lookback; prices y;
// yahoo y; twelvedata y; alphavantage n; tickers file; service y.
func TestWizardConfiguresSeveralSourcesAndPricesAndInstallsTheService(t *testing.T) {
	dir := t.TempDir()
	seed := "folders:\n  - account: acc-2\n    path: " + dir + "\nsimplefin:\n  - account: acc-1\n    access_url_env: SFIN_URL\n"
	input := strings.Join([]string{
		"",                                    // Ratio URL default
		"y", "app-1", "/keys/eb.pem", "", "n", // Enable Banking, redirect default, connect later
		"4", "60", // schedule
		"y",               // prices
		"y", "y", "n", "", // yahoo, twelvedata, alphavantage, tickers file
		"y", // install service
	}, "\n") + "\n"
	installs := 0
	out, cfg, _ := runWizardSeeded(t, seed, input, []string{"ratio-key", "td-key"}, &installs)

	if cfg.RatioManaged.APIKey != "ratio-key" || cfg.Mode != "ratio_managed" {
		t.Fatalf("%+v", cfg.RatioManaged)
	}
	if cfg.EnableBanking.AppID != "app-1" || len(cfg.Folders) != 1 || len(cfg.SimpleFIN) != 1 {
		t.Fatalf("sources not kept: %+v %+v %+v", cfg.EnableBanking, cfg.Folders, cfg.SimpleFIN)
	}
	if cfg.Sync.IntervalHours != 4 || cfg.Sync.LookbackDays != 60 {
		t.Fatalf("%+v", cfg.Sync)
	}
	if !cfg.Prices.Enabled || len(cfg.Prices.Providers) != 2 || cfg.Prices.Providers[1].APIKey != "td-key" || cfg.Prices.Providers[1].DailyLimit != 780 {
		t.Fatalf("%+v", cfg.Prices)
	}
	if installs != 1 {
		t.Fatalf("service installs: %d", installs)
	}
	if cfg.EnableBanking.RedirectURL != DefaultRedirectURL || !strings.Contains(out, DefaultRedirectURL) {
		t.Fatalf("redirect URL not set or shown: %q\n%s", cfg.EnableBanking.RedirectURL, out)
	}
}

func TestWizardRefusesToSaveAnUnusableConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	var out bytes.Buffer
	failing := passing()
	failing.Ratio = func(string, string) setup.Result { return setup.Result{Message: "bad key"} }
	w := &wizard{
		in:      bufio.NewReader(strings.NewReader("\nn\nn\n6\n30\nn\nn\n")),
		out:     &out,
		cfgPath: path,
		checks:  failing,
		secret:  func() string { return "" },
	}
	if err := w.run(); err == nil {
		t.Fatalf("expected a refusal:\n%s", out.String())
	}
}
