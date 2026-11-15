// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
)

func cmdSessions() *cobra.Command {
	var renew, check bool
	cmd := &cobra.Command{
		Use:   "sessions [bank]",
		Short: "List the connected Enable Banking banks and renew them",
		Long: `Lists the banks authorized on this machine with the days left on each consent.

  bank-connector sessions                  list
  bank-connector sessions --check          list; exit status 1 if one is expiring or expired (for cron)
  bank-connector sessions --renew          renew every consent that is expiring or expired
  bank-connector sessions --renew intesa   renew the matching bank, whatever its state

Renewing repeats the bank login. The old consent keeps working until the new
one is in place, and Ratio is relinked by the connector itself.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			l := newLinker(bufio.NewReader(os.Stdin), cmd.OutOrStdout(), cfg)
			filter := ""
			if len(args) > 0 {
				filter = strings.ToLower(args[0])
			}
			attention, err := runSessions(cmd.Context(), l, renew, filter)
			if err != nil {
				return err
			}
			if check && attention {
				cmd.SilenceUsage, cmd.SilenceErrors = true, true
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&renew, "renew", false, "renew expiring or expired consents (or the named bank)")
	cmd.Flags().BoolVar(&check, "check", false, "exit with status 1 when a consent is expiring or expired")
	return cmd
}

// runSessions prints the sessions and, with renew, renews the ones that need it.
// It reports whether any session was expiring or expired when it looked.
func runSessions(ctx context.Context, l *ebLinker, renew bool, filter string) (bool, error) {
	sessions, err := l.store.Load()
	if err != nil {
		return false, err
	}
	now := l.now()
	if len(sessions) == 0 {
		l.say("No bank connected yet: run `bank-connector link`.")
		return false, nil
	}
	attention := 0
	for _, s := range sessions {
		st, days := StateOf(s, now)
		label := map[SessionState]string{SessionOK: "ok", SessionExpiring: "EXPIRING", SessionExpired: "EXPIRED"}[st]
		left := fmt.Sprintf("%d days left", days)
		if s.ValidUntil.IsZero() {
			left = "no end date"
		} else if st == SessionExpired {
			left = "ended " + s.ValidUntil.Format("2006-01-02")
		}
		l.say("%-9s %s (%s) · %s", label, s.Bank, s.Country, left)
		for _, a := range s.Accounts {
			l.say("          %s %s %s", a.IBAN, a.Currency, a.Name)
		}
		if st != SessionOK {
			attention++
		}
	}
	if renew {
		client, err := l.api(l.cfg)
		if err != nil {
			return attention > 0, fmt.Errorf("enable banking client: %w", err)
		}
		renewed := 0
		for _, s := range sessions {
			st, _ := StateOf(s, now)
			match := filter != "" && strings.Contains(strings.ToLower(s.Bank), filter)
			if (filter == "" && st == SessionOK) || (filter != "" && !match) {
				continue
			}
			l.say("\nRenewing %s (%s)", s.Bank, s.Country)
			if _, err := l.Link(ctx, client, s.Bank, s.Country); err != nil {
				return attention > 0, err
			}
			renewed++
		}
		if renewed == 0 {
			l.say("\nNothing to renew.")
		}
	}
	return attention > 0, nil
}
