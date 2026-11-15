// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source/simplefinsource"
)

// sfinEntry is one line of the simplefin section of the config file.
type sfinEntry struct {
	Account      string
	AccessURL    string
	AccessURLEnv string
	SFINAccount  string
}

func (e sfinEntry) toMap() map[string]any {
	m := map[string]any{}
	if e.Account != "" {
		m["account"] = e.Account
	}
	if e.AccessURL != "" {
		m["access_url"] = e.AccessURL
	}
	if e.AccessURLEnv != "" {
		m["access_url_env"] = e.AccessURLEnv
	}
	if e.SFINAccount != "" {
		m["sfin_account"] = e.SFINAccount
	}
	return m
}

func sfinEntriesFrom(m map[string]any) []sfinEntry {
	list, _ := m["simplefin"].([]any)
	var out []sfinEntry
	for _, item := range list {
		row, _ := item.(map[string]any)
		str := func(k string) string { s, _ := row[k].(string); return s }
		out = append(out, sfinEntry{str("account"), str("access_url"), str("access_url_env"), str("sfin_account")})
	}
	return out
}

func sfinEntriesInto(m map[string]any, entries []sfinEntry) {
	rows := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, e.toMap())
	}
	m["simplefin"] = rows
}

// mapSimpleFIN records which SimpleFIN account feeds each Ratio account that
// names the simplefin provider and has no mapping yet. A connection with a
// single account maps by itself; otherwise choose decides (empty leaves the
// Ratio account unmapped). A line with the access URL and no Ratio account
// holds the connection until there is something to map, so the token is not
// asked for again. It returns the new entries and the Ratio accounts it
// mapped.
func mapSimpleFIN(entries []sfinEntry, access string, available []simplefinsource.AccountInfo, remote []ratio.RemoteBankAccount, choose func(ratio.RemoteBankAccount) string) ([]sfinEntry, []string) {
	mapped := map[string]bool{}
	for _, e := range entries {
		if e.Account != "" {
			mapped[e.Account] = true
		}
	}
	valid := map[string]bool{}
	for _, a := range available {
		valid[a.ID] = true
	}

	out := make([]sfinEntry, 0, len(entries)+len(remote))
	for _, e := range entries {
		// The holder for this connection is replaced below once it is used.
		if e.Account == "" && e.AccessURL == access {
			continue
		}
		out = append(out, e)
	}

	var done []string
	for _, ra := range remote {
		if ra.Provider != "simplefin" || mapped[ra.ID] {
			continue
		}
		pick := ""
		if len(available) == 1 {
			pick = available[0].ID
		} else if choose != nil {
			pick = choose(ra)
		}
		if !valid[pick] {
			continue
		}
		out = append(out, sfinEntry{Account: ra.ID, AccessURL: access, SFINAccount: pick})
		done = append(done, ra.ID)
	}
	if len(done) == 0 {
		out = append(out, sfinEntry{AccessURL: access})
	}
	return out, done
}

// cmdSetupSimpleFIN claims a SimpleFIN setup token, stores the access URL in
// the config (owner-only), and maps the SimpleFIN accounts to Ratio accounts.
// Every question has a flag or an environment variable, so an installer can run
// it without a terminal; run again after adding accounts in Ratio to map them.
func cmdSetupSimpleFIN() *cobra.Command {
	var token string
	cmd := &cobra.Command{
		Use:   "setup-simplefin",
		Short: "Connect a SimpleFIN Bridge (US/Canada) and map its accounts to Ratio accounts",
		Long: "Exchanges the setup token from your SimpleFIN Bridge for an access URL, saves it in the config file,\n" +
			"and maps your SimpleFIN accounts to the accounts you added in Ratio with provider \"simplefin\".\n" +
			"The token is read from --token, SIMPLEFIN_SETUP_TOKEN, or asked for. Without a token it reuses the saved connection,\n" +
			"which is how you map accounts you add later.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			interactive := term.IsTerminal(int(os.Stdin.Fd()))
			in := bufio.NewReader(os.Stdin)

			var entries []sfinEntry
			_ = config.UpdateFile(cfgPath, func(m map[string]any) error { entries = sfinEntriesFrom(m); return nil })

			if token == "" {
				token = os.Getenv("SIMPLEFIN_SETUP_TOKEN")
			}
			access := ""
			if token == "" {
				for _, e := range entries {
					if e.AccessURL != "" {
						access = e.AccessURL
						break
					}
				}
			}
			if token == "" && access == "" {
				if !interactive {
					return fmt.Errorf("no setup token: pass --token or set SIMPLEFIN_SETUP_TOKEN")
				}
				fmt.Print("SimpleFIN setup token (from your Bridge account): ")
				raw, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Println()
				if err != nil {
					return err
				}
				token = strings.TrimSpace(string(raw))
			}
			if access == "" {
				var err error
				if access, err = simplefinsource.Claim(nil, token); err != nil {
					return err
				}
			}

			available, err := simplefinsource.List(nil, access)
			if err != nil {
				return err
			}
			fmt.Printf("Found %d SimpleFIN account(s):\n", len(available))
			for i, a := range available {
				fmt.Printf("  %d. %s (%s)\n", i+1, a.Name, a.Currency)
			}

			var remote []ratio.RemoteBankAccount
			cfg, loadErr := config.Load(cfgPath)
			if loadErr == nil && cfg.RatioManaged.APIKey != "" {
				if poll, err := ratio.PollConfig(cfg.RatioManaged.URL, cfg.RatioManaged.APIKey); err == nil {
					remote = poll.BankAccounts
				} else {
					fmt.Fprintf(os.Stderr, "Could not read your Ratio accounts (%v); the connection is saved and you can map them later.\n", err)
				}
			}

			choose := func(ra ratio.RemoteBankAccount) string {
				if !interactive {
					return ""
				}
				name := ra.DisplayName
				if name == "" {
					name = ra.BankName
				}
				fmt.Printf("Which SimpleFIN account feeds %q in Ratio? [1-%d, Enter to skip]: ", name, len(available))
				line, _ := in.ReadString('\n')
				var n int
				if _, err := fmt.Sscanf(strings.TrimSpace(line), "%d", &n); err != nil || n < 1 || n > len(available) {
					return ""
				}
				return available[n-1].ID
			}

			var done []string
			err = config.UpdateFile(cfgPath, func(m map[string]any) error {
				var next []sfinEntry
				next, done = mapSimpleFIN(sfinEntriesFrom(m), access, available, remote, choose)
				sfinEntriesInto(m, next)
				return nil
			})
			if err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			if len(done) > 0 {
				fmt.Printf("✓ Mapped %d account(s). The connector links them on its next cycle.\n", len(done))
			} else {
				fmt.Println("✓ Connection saved. Add an account in Ratio with provider \"simplefin\", then run this command again to map it.")
			}
			fmt.Printf("The access URL is stored in %s (owner-only). Keep that file private.\n", cfgPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "SimpleFIN setup token (or SIMPLEFIN_SETUP_TOKEN)")
	return cmd
}
