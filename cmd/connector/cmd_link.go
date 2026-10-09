// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/marcosperanza/ratio-bank-connector/v3/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v3/internal/enablebanking"
)

func cmdLink() *cobra.Command {
	var country string

	cmd := &cobra.Command{
		Use:   "link [filter]",
		Short: "Link a bank account via Enable Banking OAuth",
		Long: `Starts an interactive OAuth flow to link a bank account.
The bank redirects to your Ratio app callback URL — paste the full redirect
URL back into the terminal when prompted.

Optional argument filters the bank list (case-insensitive substring match):
  bank-connector link intesa
  bank-connector link "poste"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			ebClient, err := enablebanking.NewClient(
				cfg.EnableBanking.AppID,
				cfg.EnableBanking.PrivateKeyPath,
				cfg.EnableBanking.PrivateKeyB64,
			)
			if err != nil {
				return fmt.Errorf("enable banking client: %w", err)
			}

			// Derive callback URL from Ratio config
			callbackURL := "https://www.ratio-fin.com/api/connector/eb-callback"
			if cfg.Ratio.URL != "" {
				callbackURL = strings.TrimRight(cfg.Ratio.URL, "/") + "/api/connector/eb-callback"
			}

			log.Info("fetching available banks", zap.String("country", strings.ToUpper(country)))
			banks, err := ebClient.ListASPSPs(country)
			if err != nil {
				return fmt.Errorf("list banks: %w", err)
			}

			// Sort alphabetically
			sort.Slice(banks, func(i, j int) bool {
				ni, _ := banks[i]["name"].(string)
				nj, _ := banks[j]["name"].(string)
				return strings.ToLower(ni) < strings.ToLower(nj)
			})

			// Filter by search term if provided
			if len(args) > 0 {
				q := strings.ToLower(args[0])
				var filtered []map[string]any
				for _, b := range banks {
					name, _ := b["name"].(string)
					if strings.Contains(strings.ToLower(name), q) {
						filtered = append(filtered, b)
					}
				}
				banks = filtered
			}

			if len(banks) == 0 {
				return fmt.Errorf("no banks found matching filter")
			}

			fmt.Printf("\nAvailable banks in %s (%d):\n\n", strings.ToUpper(country), len(banks))
			for i, b := range banks {
				fmt.Printf("  [%2d] %v\n", i+1, b["name"])
			}

			reader := bufio.NewReader(os.Stdin)
			fmt.Print("\nSelect bank number: ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)

			n, err2 := strconv.Atoi(input)
			if err2 != nil || n < 1 || n > len(banks) {
				return fmt.Errorf("invalid selection")
			}
			bankName := fmt.Sprint(banks[n-1]["name"])
			if bankName == "" {
				return fmt.Errorf("bank name not found")
			}

			authURL, err := ebClient.InitAuth(bankName, country, callbackURL, "bank-connector-link")
			if err != nil {
				return fmt.Errorf("init auth: %w", err)
			}

			fmt.Printf("\nOpen this URL in your browser to authorize:\n\n  %s\n\n", authURL)
			fmt.Printf("After authorization the bank will redirect to:\n  %s\n\n", callbackURL)
			openBrowser(authURL)

			fmt.Print("Paste the full redirect URL here: ")
			redirectRaw, _ := reader.ReadString('\n')
			redirectRaw = strings.TrimSpace(redirectRaw)
			if redirectRaw == "" {
				return fmt.Errorf("no URL provided")
			}

			parsed, err := url.Parse(redirectRaw)
			if err != nil {
				return fmt.Errorf("parse redirect URL: %w", err)
			}
			code := parsed.Query().Get("code")
			if code == "" {
				return fmt.Errorf("no `code` param in redirect URL — got: %s", redirectRaw)
			}

			log.Info("creating session", zap.String("bank", bankName))
			session, err := ebClient.CreateSession(code)
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}

			accounts := session.Accounts
			fmt.Printf("\n✓ Linked %d account(s):\n\n", len(accounts))
			for _, acc := range accounts {
				fmt.Printf("  Account ID : %s\n", acc.ID)
				fmt.Printf("  IBAN       : %s\n", acc.AccountID.IBAN)
				fmt.Printf("  Currency   : %s\n", acc.Currency)
				fmt.Printf("  Holder     : %s\n\n", acc.AccountHolder)
			}

			fmt.Println("Add these entries to your config.yaml under `accounts:`:")
			fmt.Println()
			for _, acc := range accounts {
				fmt.Printf("  - eb_account_id: \"%s\"      # %s — %s\n", acc.ID, acc.AccountID.IBAN, acc.AccountHolder)
				fmt.Printf("    ratio_workspace_id: \"\"   # fill in your Ratio workspace ID\n")
				fmt.Println()
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&country, "country", "IT", "country code (IT, DE, FR...)")
	return cmd
}

func openBrowser(url string) {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "linux":
		name, args = "xdg-open", []string{url}
	default:
		return
	}
	exec.Command(name, args...).Start() //nolint:errcheck
}
