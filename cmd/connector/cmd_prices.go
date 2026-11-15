// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/crypto"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/prices"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/sealing"
)

func cmdPrices() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prices",
		Short: "Download daily prices for the tracked tickers with the providers configured on this machine",
	}
	var daemon bool
	sync := &cobra.Command{
		Use:   "sync",
		Short: "Download prices once, or on an interval with --daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			engine, source, err := buildPriceEngine(cfg)
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			if daemon {
				engine.RunLoop(ctx, source, time.Duration(cfg.Prices.IntervalHours)*time.Hour)
				return nil
			}
			return runPriceSync(ctx, engine, source)
		},
	}
	sync.Flags().BoolVar(&daemon, "daemon", false, "run continuously on prices.interval_hours")
	check := &cobra.Command{
		Use:   "check",
		Short: "Show which provider spelling each ticker would use, without calling any provider",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			engine, source, err := buildPriceEngine(cfg)
			if err != nil {
				return err
			}
			tickers, err := source.Tickers(cmd.Context())
			if err != nil {
				return err
			}
			for _, raw := range tickers {
				t := prices.ParseTicker(raw)
				fmt.Printf("%s\n", t.Raw)
				for _, slot := range engine.Slots {
					fmt.Printf("  %-13s %v\n", slot.Provider.Name(), slot.Provider.Candidates(t))
				}
			}
			return nil
		},
	}
	fingerprint := &cobra.Command{
		Use:   "fingerprint",
		Short: "Print the fingerprint of this connector's sealing key, to compare in Ratio before approving it",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			key, created, err := sealing.LoadOrCreate(cfg.Prices.SealingKeyPath)
			if err != nil {
				return err
			}
			fp, err := crypto.SealingFingerprint(&key.PublicKey)
			if err != nil {
				return err
			}
			if created {
				fmt.Println("A new sealing key was generated on this machine.")
			}
			fmt.Println(crypto.FormatFingerprint(fp))
			if cfg.Mode == "ratio_managed" {
				if err := registerSealingKey(cfg, key); err != nil {
					return fmt.Errorf("register with Ratio: %w", err)
				}
				fmt.Println("Registered with Ratio. Open Investments in Ratio, compare this fingerprint and approve it.")
			}
			return nil
		},
	}
	cmd.AddCommand(sync, check, fingerprint)
	return cmd
}

func runPriceSync(ctx context.Context, engine *prices.Engine, source prices.TickerSource) error {
	tickers, failed, err := downloadPrices(ctx, engine, source)
	if err != nil {
		return err
	}
	fmt.Printf("prices: %d tickers, %d without data\n", tickers, failed)
	return nil
}

// downloadPrices runs one download of every tracked ticker and says how many there were and how many came back without data.
func downloadPrices(ctx context.Context, engine *prices.Engine, source prices.TickerSource) (tickers, failed int, err error) {
	list, err := source.Tickers(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("tickers: %w", err)
	}
	for _, o := range engine.Run(ctx, list) {
		if o.Err != nil {
			failed++
		}
	}
	return len(list), failed, nil
}

// refreshPricesNow is the forced refresh of the settings page: it reads the configuration as saved at this moment
// and downloads and delivers everything, whatever the interval says.
func refreshPricesNow(ctx context.Context, cfgPath string) (tickers, failed int, err error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return 0, 0, fmt.Errorf("config: %w", err)
	}
	engine, source, err := buildPriceEngine(cfg)
	if err != nil {
		return 0, 0, err
	}
	return downloadPrices(ctx, engine, source)
}

// buildPriceEngine wires the providers in the order the user configured them.
func buildPriceEngine(cfg *config.Config) (*prices.Engine, prices.TickerSource, error) {
	pc := cfg.Prices
	if !pc.Enabled {
		return nil, nil, fmt.Errorf("config: prices.enabled is false")
	}
	if len(pc.Providers) == 0 {
		return nil, nil, fmt.Errorf("config: prices.providers is empty")
	}
	managed := cfg.Mode == "ratio_managed"
	if pc.TickersFile == "" && !managed {
		return nil, nil, fmt.Errorf("config: prices.tickers_file is required (or use ratio_managed mode to read the Ratio watchlist)")
	}
	if err := os.MkdirAll(pc.Dir, 0o700); err != nil {
		return nil, nil, err
	}
	state, err := prices.LoadState(filepath.Join(pc.Dir, "state.json"))
	if err != nil {
		return nil, nil, err
	}
	hc := &http.Client{Timeout: 30 * time.Second}
	ov := prices.Overrides{}
	for _, o := range pc.Overrides {
		t := strings.ToUpper(strings.TrimSpace(o.Ticker))
		if ov[t] == nil {
			ov[t] = map[string]string{}
		}
		ov[t][o.Provider] = o.Symbol
	}
	learned := func(provider string) func(string) string {
		return func(ticker string) string { return state.Learned[ticker][provider] }
	}
	engine := &prices.Engine{
		Store: prices.Store{Dir: pc.Dir},
		State: state,
		Log:   func(f string, a ...any) { log.Sugar().Infof(f, a...) },
	}
	for _, p := range pc.Providers {
		var prov prices.Provider
		switch p.Name {
		case "yahoo":
			prov = &prices.Yahoo{BaseURL: "https://query1.finance.yahoo.com", HTTP: hc, Overrides: ov, Learned: learned("yahoo")}
		case "twelvedata":
			prov = &prices.TwelveData{BaseURL: "https://api.twelvedata.com", APIKey: p.APIKey, HTTP: hc, Overrides: ov, Learned: learned("twelvedata")}
		case "alphavantage":
			prov = &prices.AlphaVantage{BaseURL: "https://www.alphavantage.co", APIKey: p.APIKey, HTTP: hc, Overrides: ov, Learned: learned("alphavantage")}
		default:
			return nil, nil, fmt.Errorf("config: unknown price provider %q", p.Name)
		}
		if p.Name != "yahoo" && p.APIKey == "" {
			return nil, nil, fmt.Errorf("config: provider %s needs api_key", p.Name)
		}
		engine.Slots = append(engine.Slots, prices.Slot{Provider: prov, DailyLimit: p.DailyLimit, MinGap: time.Duration(p.GapSeconds) * time.Second})
	}
	var sources prices.UnionSource
	if pc.TickersFile != "" {
		sources = append(sources, prices.FileSource{Path: pc.TickersFile})
	}
	if managed {
		key, _, err := sealing.LoadOrCreate(pc.SealingKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("sealing key: %w", err)
		}
		sealed := &prices.SealedSource{
			Fetcher:   ratio.NewClient(cfg.RatioManaged.URL, cfg.RatioManaged.APIKey),
			Key:       key,
			CachePath: filepath.Join(pc.Dir, "watchlist.json"),
			Log:       engine.Log,
		}
		sources = append(sources, sealed)
		// What each run finds goes back to the user's devices through the price inbox, sealed to their key.
		engine.OnOutcomes = (&prices.Deliverer{
			Entries: sealed,
			Store:   engine.Store,
			State:   state,
			Sender:  ratio.NewClient(cfg.RatioManaged.URL, cfg.RatioManaged.APIKey),
			PublicKey: func() (string, error) {
				poll, err := ratio.PollConfig(cfg.RatioManaged.URL, cfg.RatioManaged.APIKey)
				if err != nil {
					return "", err
				}
				return poll.PublicKeyJwk, nil
			},
			HMACKey: cfg.RatioManaged.APIKey,
			Log:     engine.Log,
		}).Deliver
		if err := registerSealingKey(cfg, key); err != nil {
			log.Sugar().Warnf("prices: cannot register the sealing key with Ratio yet: %v", err)
		}
	}
	return engine, sources, nil
}

// registerSealingKey publishes the public key so Ratio can offer its
// fingerprint for approval. It is idempotent: Ratio ignores an unchanged key.
func registerSealingKey(cfg *config.Config, key *rsa.PrivateKey) error {
	spki, err := crypto.SealingPublicKeySPKI(&key.PublicKey)
	if err != nil {
		return err
	}
	return ratio.NewClient(cfg.RatioManaged.URL, cfg.RatioManaged.APIKey).RegisterSealingKey(spki)
}
