// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
)

func cmdLink() *cobra.Command {
	var country string

	cmd := &cobra.Command{
		Use:   "link [filter]",
		Short: "Authorize a bank with Enable Banking from this machine",
		Long: `Authorizes a bank with your own Enable Banking application, without Ratio.
The connector keeps the session; create the account in Ratio with the provider
"enablebanking-local" and name it after the IBAN or the bank to link it.

Register https://enablebanking.com/ as a redirect URL in your application once.
After the bank login you land on that page: copy the code (or the whole address)
from the address bar and paste it here. Works on a server with no screen: open
the printed address on any device.

If enable_banking.redirect_url points at this machine (https://localhost:PORT/...)
the connector receives the answer itself instead.

Optional argument filters the bank list (case-insensitive substring match):
  bank-connector link intesa
  bank-connector link "poste"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			l := newLinker(bufio.NewReader(os.Stdin), cmd.OutOrStdout(), cfg)
			return l.Interactive(cmd.Context(), country, filter)
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
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return
	}
	exec.Command(name, args...).Start() //nolint:errcheck
}
