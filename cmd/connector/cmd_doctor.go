// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/marcosperanza/ratio-bank-connector/v3/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v3/internal/enablebanking"
	"github.com/marcosperanza/ratio-bank-connector/v3/internal/ratio"
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
			if cfg.EnableBanking.AppID == "" {
				return fmt.Errorf("config: enable_banking.app_id is required")
			}
			switch cfg.EffectiveArchiveMode() {
			case config.ArchiveOff, config.ArchiveCSV, config.ArchiveCSVAndRaw:
			default:
				return fmt.Errorf("config: sync.archive_mode must be off, csv, or csv_and_raw")
			}

			providerClient, err := enablebanking.NewClient(
				cfg.EnableBanking.AppID,
				cfg.EnableBanking.PrivateKeyPath,
				cfg.EnableBanking.PrivateKeyB64,
			)
			if err != nil {
				return fmt.Errorf("Enable Banking credentials: %w", err)
			}
			fmt.Printf("OK config: %s\n", cfgPath)
			fmt.Println("OK Enable Banking private key")
			fmt.Printf("OK local archive mode: %s\n", cfg.EffectiveArchiveMode())

			if cfg.Mode == "ratio_managed" {
				if cfg.RatioManaged.APIKey == "" {
					return fmt.Errorf("config: ratio_managed.api_key is required")
				}
				if err := validateRatioURL(cfg.RatioManaged.URL); err != nil {
					return err
				}
				fmt.Println("OK Ratio-managed settings")
				if network {
					poll, err := ratio.PollConfig(cfg.RatioManaged.URL, cfg.RatioManaged.APIKey)
					if err != nil {
						return fmt.Errorf("Ratio connectivity: %w", err)
					}
					fmt.Printf("OK Ratio connectivity: %d account(s)\n", len(poll.BankAccounts))
				}
			} else if network {
				banks, err := providerClient.ListASPSPs("IT")
				if err != nil {
					return fmt.Errorf("Enable Banking connectivity: %w", err)
				}
				fmt.Printf("OK Enable Banking connectivity: %d Italian institution(s)\n", len(banks))
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&network, "network", false, "perform read-only API connectivity checks")
	return cmd
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
