// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/term"

	"github.com/marcosperanza/ratio-bank-connector/v3/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v3/internal/crypto"
	"github.com/marcosperanza/ratio-bank-connector/v3/internal/enablebanking"
	"github.com/marcosperanza/ratio-bank-connector/v3/internal/ratio"
	syncp "github.com/marcosperanza/ratio-bank-connector/v3/internal/sync"
)

var (
	cfgPath   string
	log       *zap.Logger
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	var err error
	cfg := zap.NewProductionConfig()
	cfg.OutputPaths = []string{"stdout"}
	cfg.ErrorOutputPaths = []string{"stdout"}
	log, err = cfg.Build()
	if err != nil {
		panic(err)
	}
	defer log.Sync() //nolint:errcheck

	root := &cobra.Command{
		Use:   "bank-connector",
		Short: "Local Ratio add-on for the user's own open-banking accounts",
	}

	root.PersistentFlags().StringVarP(&cfgPath, "config", "c", config.DefaultConfigPath(), "config file path")
	root.AddCommand(cmdSetup(), cmdSetupManaged(), cmdSync(), cmdLink(), cmdAccounts(), cmdDoctor(), cmdVersion())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func cmdSync() *cobra.Command {
	var daemon bool
	var force bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Fetch, encrypt, and send bank transactions to the user's Ratio inbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemon && force {
				return fmt.Errorf("--force cannot be used with --daemon")
			}
			cfg, ebClient, ratioClient, masterKey, searchKey, err := loadRuntime()
			if err != nil {
				return err
			}

			s := syncp.New(cfg, cfgPath, ebClient, ratioClient, masterKey, searchKey, log)

			if !daemon {
				if force {
					return s.SyncAllForce()
				}
				return s.SyncAll()
			}

			stop := make(chan struct{})
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sig
				close(stop)
			}()
			if cfg.Mode == "ratio_managed" {
				syncp.RunManagedDaemon(s, cfg.RatioManaged.PollIntervalSeconds, cfg.Sync.IntervalHours, stop)
			} else {
				syncp.RunDaemon(s, cfg.Sync.IntervalHours, stop)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&daemon, "daemon", false, "run continuously on interval")
	cmd.Flags().BoolVar(&force, "force", false, "ignore the last successful sync time for this manual run")
	return cmd
}

func cmdAccounts() *cobra.Command {
	return &cobra.Command{
		Use:   "accounts",
		Short: "List available Italian banks on Enable Banking",
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
				return err
			}
			banks, err := ebClient.ListASPSPs("IT")
			if err != nil {
				return err
			}
			fmt.Printf("Available Italian banks (%d):\n", len(banks))
			for _, b := range banks {
				fmt.Printf("  %v — %v\n", b["bic"], b["name"])
			}
			return nil
		},
	}
}

func cmdVersion() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("bank-connector %s (commit %s, built %s)\n", version, commit, buildDate)
		},
	}
}

// loadRuntime loads config, creates clients, derives crypto keys.
func loadRuntime() (*config.Config, *enablebanking.Client, *ratio.Client, []byte, []byte, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("load config: %w", err)
	}

	ebClient, err := enablebanking.NewClient(
		cfg.EnableBanking.AppID,
		cfg.EnableBanking.PrivateKeyPath,
		cfg.EnableBanking.PrivateKeyB64,
	)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("enable banking client: %w", err)
	}

	// ratio_managed: build client using API key — ZK keys resolved later from poll response
	if cfg.Mode == "ratio_managed" {
		rc := ratio.NewClient(cfg.RatioManaged.URL, cfg.RatioManaged.APIKey)
		return cfg, ebClient, rc, nil, nil, nil
	}

	if !cfg.Ratio.Enabled {
		return cfg, ebClient, nil, nil, nil, nil
	}

	passphrase := readPassphrase()

	// Decrypt master key: encrypted with AES-GCM using passphrase-derived KEK
	masterKeyCT, err := base64.StdEncoding.DecodeString(cfg.Ratio.EncryptedMasterKey)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("decode encrypted master key: %w", err)
	}
	masterKeyIV, err := base64.StdEncoding.DecodeString(cfg.Ratio.MasterKeyIV)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("decode master key iv: %w", err)
	}

	localKEK, err := crypto.DeriveKEK(passphrase, base64.StdEncoding.EncodeToString(masterKeyIV))
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("derive local kek: %w", err)
	}

	masterKey, err := crypto.DecryptGCM(masterKeyCT, masterKeyIV, localKEK)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("decrypt master key (wrong passphrase?): %w", err)
	}

	searchKey, err := crypto.DecryptSearchKey(cfg.Ratio.EncryptedSearchKey, masterKey)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("decrypt search key: %w", err)
	}

	ratioClient := ratio.NewClient(cfg.Ratio.URL, cfg.Ratio.ConnectorToken)
	return cfg, ebClient, ratioClient, masterKey, searchKey, nil
}

func readPassphrase() string {
	return readPassphrasePrompt("Enter passphrase: ")
}

func readPassphrasePrompt(prompt string) string {
	if p := os.Getenv("BANK_CONNECTOR_PASSPHRASE"); p != "" {
		return p
	}
	fmt.Print(prompt)
	b, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		r := bufio.NewReader(os.Stdin)
		line, _ := r.ReadString('\n')
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(string(b))
}

func normalizeInputPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("path is required")
	}
	if raw == "~" || strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		if raw == "~" {
			raw = home
		} else {
			raw = filepath.Join(home, raw[2:])
		}
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	return filepath.Clean(abs), nil
}
