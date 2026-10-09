// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/marcosperanza/ratio-bank-connector/v3/internal/config"
	"golang.org/x/term"
)

func cmdSetupManaged() *cobra.Command {
	return &cobra.Command{
		Use:     "setup-addon",
		Aliases: []string{"setup-managed"},
		Short:   "Configure the local Ratio add-on with the user's own provider credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			r := bufio.NewReader(os.Stdin)
			ask := func(prompt string) string {
				fmt.Print(prompt)
				s, _ := r.ReadString('\n')
				return strings.TrimSpace(s)
			}

			fmt.Println("=== Ratio Bank Connector Add-on Setup ===")
			fmt.Println()
			fmt.Println("The add-on runs locally with your Enable Banking credentials and your Ratio API key.")
			fmt.Println("Ratio coordinates account linking; bank data is encrypted before it is uploaded.")
			fmt.Println()

			ratioURL := ask("Ratio URL [https://www.ratio-fin.com]: ")
			if ratioURL == "" {
				ratioURL = "https://www.ratio-fin.com"
			}
			fmt.Print("API Key (create one at /dashboard/connector): ")
			apiKeyBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("read API key: %w", err)
			}
			apiKey := strings.TrimSpace(string(apiKeyBytes))
			for i := range apiKeyBytes {
				apiKeyBytes[i] = 0
			}
			if apiKey == "" {
				return fmt.Errorf("API key cannot be empty")
			}
			appID := ask("Enable Banking App ID: ")
			keyPath := ask("Path to PEM private key file: ")
			keyPath, err = normalizeInputPath(keyPath)
			if err != nil {
				return err
			}
			pollStr := ask("Poll interval in seconds [600]: ")
			pollSec := 600
			if pollStr != "" {
				if n, err := strconv.Atoi(pollStr); err == nil && n > 0 {
					pollSec = n
				}
			}

			type managedConfig struct {
				Mode          string `yaml:"mode"`
				EnableBanking struct {
					AppID          string `yaml:"app_id"`
					PrivateKeyPath string `yaml:"private_key_path,omitempty"`
				} `yaml:"enable_banking"`
				Sync struct {
					IntervalHours int    `yaml:"interval_hours"`
					LookbackDays  int    `yaml:"lookback_days"`
					CSVDir        string `yaml:"csv_dir"`
					ArchiveMode   string `yaml:"archive_mode"`
				} `yaml:"sync"`
				RatioManaged struct {
					URL                 string `yaml:"url"`
					APIKey              string `yaml:"api_key"`
					PollIntervalSeconds int    `yaml:"poll_interval_seconds"`
				} `yaml:"ratio_managed"`
			}

			var out managedConfig
			out.Mode = "ratio_managed"
			out.EnableBanking.AppID = appID
			out.EnableBanking.PrivateKeyPath = keyPath
			out.Sync.IntervalHours = 6
			out.Sync.LookbackDays = 30
			out.Sync.CSVDir = config.DefaultDataDir()
			out.Sync.ArchiveMode = config.ArchiveOff
			out.RatioManaged.URL = ratioURL
			out.RatioManaged.APIKey = apiKey
			out.RatioManaged.PollIntervalSeconds = pollSec

			data, err := yaml.Marshal(&out)
			if err != nil {
				return fmt.Errorf("marshal config: %w", err)
			}

			header := []byte("# Bank Connector configuration — ratio_managed mode\n" +
				"# Bank accounts are managed from the Ratio dashboard (/dashboard/connector).\n" +
				"# Transactions are encrypted with the public key supplied by Ratio.\n\n")

			if err := os.MkdirAll(filepath.Dir(cfgPath), 0700); err != nil {
				return fmt.Errorf("create config directory: %w", err)
			}
			if err := os.WriteFile(cfgPath, append(header, data...), 0600); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			if err := os.Chmod(cfgPath, 0600); err != nil {
				return fmt.Errorf("secure config permissions: %w", err)
			}

			fmt.Printf("\n✓ Config written to %s\n", cfgPath)
			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println("  1. Go to /dashboard/connector and add bank accounts")
			fmt.Println("  2. Run `bank-connector sync` — connector will pick up pending accounts and start OAuth")
			fmt.Println("  3. Click the 'Authorize' button in the Ratio dashboard to complete bank linking")
			fmt.Println("  4. Run `bank-connector sync --daemon` for continuous sync")
			return nil
		},
	}
}
