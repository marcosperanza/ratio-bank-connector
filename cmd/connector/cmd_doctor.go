// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ebauth"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/prices"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
)

func cmdDoctor() *cobra.Command {
	var network bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate configuration and optionally test provider connectivity",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			if cfg.Mode != "" && cfg.Mode != "standalone" && cfg.Mode != "ratio_managed" {
				return fmt.Errorf("config: unsupported mode %q", cfg.Mode)
			}
			if cfg.Sync.IntervalHours <= 0 || cfg.Sync.LookbackDays <= 0 {
				return fmt.Errorf("config: sync intervals must be positive")
			}
			if !cfg.EnableBankingConfigured() && len(cfg.SimpleFIN) == 0 && len(cfg.Folders) == 0 {
				return fmt.Errorf("config: configure at least one source (enable_banking, simplefin or folders)")
			}
			switch cfg.EffectiveArchiveMode() {
			case config.ArchiveOff, config.ArchiveCSV, config.ArchiveCSVAndRaw:
			default:
				return fmt.Errorf("config: sync.archive_mode must be off, csv, or csv_and_raw")
			}

			var providerClient *enablebanking.Client
			if cfg.EnableBankingConfigured() {
				providerClient, err = enablebanking.NewClient(
					cfg.EnableBanking.AppID,
					cfg.EnableBanking.PrivateKeyPath,
					cfg.EnableBanking.PrivateKeyB64,
				)
				if err != nil {
					return fmt.Errorf("Enable Banking credentials: %w", err)
				}
				fmt.Println("OK Enable Banking private key")
			}
			fmt.Printf("OK config: %s\n", cfgPath)
			if err := doctorSources(cfg); err != nil {
				return err
			}
			fmt.Printf("OK local archive mode: %s\n", cfg.EffectiveArchiveMode())

			if cfg.Mode == "ratio_managed" {
				if cfg.RatioManaged.APIKey == "" {
					return fmt.Errorf("config: ratio_managed.api_key is required")
				}
				if err := validateRatioURL(cfg.RatioManaged.URL); err != nil {
					return err
				}
				fmt.Println("OK Ratio-managed settings")
				if cfg.EnableBankingConfigured() {
					doctorEnableBanking(cfg)
				}
				if network {
					poll, err := ratio.PollConfig(cfg.RatioManaged.URL, cfg.RatioManaged.APIKey)
					if err != nil {
						return fmt.Errorf("Ratio connectivity: %w", err)
					}
					fmt.Printf("OK Ratio connectivity: %d account(s)\n", len(poll.BankAccounts))
				}
			} else if network && providerClient != nil {
				banks, err := providerClient.ListASPSPs("IT")
				if err != nil {
					return fmt.Errorf("Enable Banking connectivity: %w", err)
				}
				fmt.Printf("OK Enable Banking connectivity: %d Italian institution(s)\n", len(banks))
			}

			return doctorPrices(cfg)
		},
	}
	cmd.Flags().BoolVar(&network, "network", false, "perform read-only API connectivity checks")
	return cmd
}

// doctorEnableBanking reports the redirect URL to register and the state of
// the consents kept on this machine.
func doctorEnableBanking(cfg *config.Config) {
	redirect := cfg.EnableBanking.RedirectURL
	if redirect == "" {
		redirect = DefaultRedirectURL
	}
	fmt.Printf("INFO Enable Banking redirect URL to register in your application: %s\n", redirect)
	if !strings.HasPrefix(redirect, "https://") && !ebauth.CanListen(redirect) {
		fmt.Println("WARN Enable Banking accepts only https redirect URLs")
	}
	sessions, _ := (&ebauth.Store{Path: config.EBSessionsPath()}).Load()
	if len(sessions) == 0 {
		fmt.Println("INFO no bank connected yet: run `bank-connector link`")
	}
	now := time.Now()
	for _, s := range sessions {
		switch st, days := StateOf(s, now); st {
		case SessionExpired:
			fmt.Printf("WARN %s: consent expired, renew with `bank-connector sessions --renew`\n", s.Bank)
		case SessionExpiring:
			fmt.Printf("WARN %s: consent ends in %d day(s), renew with `bank-connector sessions --renew`\n", s.Bank, days)
		default:
			fmt.Printf("OK %s: consent valid\n", s.Bank)
		}
	}
}

func validateRatioURL(raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("config: ratio_managed.url is invalid")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLocalHost(parsed.Hostname())) {
		return fmt.Errorf("config: Ratio URL must use HTTPS (HTTP is allowed only for localhost)")
	}
	return nil
}

func isLocalHost(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// doctorPrices reports the price downloader's setup. Keys are never printed.
func doctorPrices(cfg *config.Config) error {
	pc := cfg.Prices
	if !pc.Enabled {
		fmt.Println("INFO prices: disabled (set prices.enabled to download prices on this machine)")
		return nil
	}
	engine, source, err := buildPriceEngine(cfg)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(engine.Slots))
	for _, slot := range engine.Slots {
		names = append(names, slot.Provider.Name())
	}
	fmt.Printf("OK prices: providers in fallback order: %s\n", strings.Join(names, ", "))
	tickers, err := source.Tickers(context.Background())
	if err != nil {
		return fmt.Errorf("prices tickers: %w", err)
	}
	fmt.Printf("OK prices: %d ticker(s), data in %s\n", len(tickers), pc.Dir)
	for _, raw := range tickers {
		t := prices.ParseTicker(raw)
		for _, slot := range engine.Slots {
			if len(slot.Provider.Candidates(t)) == 0 {
				fmt.Printf("WARN prices: %s has no spelling for %s (market not covered)\n", raw, slot.Provider.Name())
			}
		}
	}
	return nil
}

// doctorSources reports each configured source. Secrets are never printed.
func doctorSources(cfg *config.Config) error {
	for _, sf := range cfg.SimpleFIN {
		switch {
		case sf.Account == "":
			fmt.Println("OK SimpleFIN connection saved, no account mapped yet")
		case sf.AccessURL != "" || (sf.AccessURLEnv != "" && os.Getenv(sf.AccessURLEnv) != ""):
			fmt.Printf("OK SimpleFIN access URL is set for account %s\n", sf.Account)
		default:
			fmt.Printf("WARN SimpleFIN account %s: no access URL (set %s or access_url)\n", sf.Account, firstNonEmptyStr(sf.AccessURLEnv, "access_url_env"))
		}
	}
	for _, f := range cfg.Folders {
		entries, err := os.ReadDir(f.Path)
		if err != nil {
			return fmt.Errorf("folder for account %q: %w", f.Account, err)
		}
		files := 0
		for _, e := range entries {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if !e.IsDir() && (ext == ".csv" || ext == ".ofx" || ext == ".qfx") {
				files++
			}
		}
		fmt.Printf("OK folder for account %q: %d export file(s) in %s\n", f.Account, files, f.Path)
		if files == 0 {
			fmt.Printf("WARN folder for account %q has no export files yet\n", f.Account)
		}
	}
	return nil
}
