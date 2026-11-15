// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/service"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/setup"
)

// wizardChecks are the live checks the wizard runs; tests replace them.
type wizardChecks struct {
	Ratio func(url, key string) setup.Result
	EB    func(appID, pemPath string) setup.Result
	Price func(name, key string) setup.Result
}

func liveChecks() wizardChecks {
	return wizardChecks{
		Ratio: setup.CheckRatio,
		EB: func(appID, pem string) setup.Result {
			return setup.CheckEnableBanking(appID, pem, "", true)
		},
		Price: func(name, key string) setup.Result {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			return setup.CheckPriceProvider(ctx, name, key)
		},
	}
}

type wizard struct {
	in      *bufio.Reader
	out     io.Writer
	secret  func() string // reads a hidden value
	checks  wizardChecks
	cfgPath string
	docker  bool
	// installService is nil when the platform has no service (Docker).
	installService func(configPath string) error
	// linker builds the Enable Banking consent runner; tests replace it.
	linker func(cfg *config.Config) *ebLinker
}

func (w *wizard) newLinker(cfg *config.Config) *ebLinker {
	if w.linker != nil {
		return w.linker(cfg)
	}
	return newLinker(w.in, w.out, cfg)
}

func cmdSetupWizard() *cobra.Command {
	return &cobra.Command{
		Use:     "setup",
		Aliases: []string{"setup-addon", "setup-managed"},
		Short:   "Guided setup: Ratio, bank sources, prices and the background service",
		Long: `Walks through everything the connector needs and checks each step live:
the Ratio connection, Enable Banking (with the bank consent given on this machine),
price downloads, and the background service. Run it again at any time to change
settings; existing values are offered as defaults.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := &wizard{
				in:      bufio.NewReader(os.Stdin),
				out:     os.Stdout,
				cfgPath: cfgPath,
				docker:  service.InContainer(),
				checks:  liveChecks(),
				secret: func() string {
					b, err := term.ReadPassword(int(syscall.Stdin))
					fmt.Println()
					if err != nil {
						line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
						return strings.TrimSpace(line)
					}
					return strings.TrimSpace(string(b))
				},
			}
			if !w.docker {
				w.installService = func(path string) error {
					m := service.New()
					spec, err := m.DefaultSpec(path)
					if err != nil {
						return err
					}
					return m.Install(spec)
				}
			}
			return w.run()
		},
	}
}

func (w *wizard) say(format string, args ...any) { fmt.Fprintf(w.out, format+"\n", args...) }

func (w *wizard) ask(prompt, def string) string {
	if def != "" {
		fmt.Fprintf(w.out, "%s [%s]: ", prompt, def)
	} else {
		fmt.Fprintf(w.out, "%s: ", prompt)
	}
	line, _ := w.in.ReadString('\n')
	if v := strings.TrimSpace(line); v != "" {
		return v
	}
	return def
}

func (w *wizard) askSecret(prompt string, hasCurrent bool) string {
	suffix := ""
	if hasCurrent {
		suffix = " (Enter keeps the current one)"
	}
	fmt.Fprintf(w.out, "%s%s: ", prompt, suffix)
	return w.secret()
}

func (w *wizard) yes(prompt string, def bool) bool {
	d := "y/N"
	if def {
		d = "Y/n"
	}
	fmt.Fprintf(w.out, "%s [%s]: ", prompt, d)
	line, _ := w.in.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "s", "si", "sì":
		return true
	case "n", "no":
		return false
	}
	return def
}

func (w *wizard) show(r setup.Result) bool {
	mark := "✗"
	if r.OK {
		mark = "✓"
	}
	w.say("  %s %s", mark, r.Message)
	for _, item := range r.Items {
		w.say("      %s", item)
	}
	return r.OK
}

// retry runs a step until it passes or the user moves on.
func (w *wizard) retry(step func() bool) {
	for !step() {
		if !w.yes("  Try again?", true) {
			w.say("  Continuing without a passing check: fix it later with `bank-connector setup`.")
			return
		}
	}
}

func (w *wizard) loadExisting() *config.Config {
	if cfg, err := config.Load(w.cfgPath); err == nil {
		return cfg
	}
	return &config.Config{
		Mode:         "ratio_managed",
		Sync:         config.SyncConfig{IntervalHours: 6, LookbackDays: 30, CSVDir: config.DefaultDataDir(), ArchiveMode: config.ArchiveOff},
		RatioManaged: config.RatioManagedConfig{URL: "https://www.ratio-fin.com", PollIntervalSeconds: 1800},
	}
}

func (w *wizard) run() error {
	cfg := w.loadExisting()
	cfg.Mode = "ratio_managed"
	if cfg.Sync.IntervalHours <= 0 {
		cfg.Sync.IntervalHours = 6
	}
	if cfg.Sync.LookbackDays <= 0 {
		cfg.Sync.LookbackDays = 30
	}
	if cfg.Sync.CSVDir == "" {
		cfg.Sync.CSVDir = config.DefaultDataDir()
	}
	if cfg.Sync.ArchiveMode == "" {
		cfg.Sync.ArchiveMode = config.ArchiveOff
	}

	w.say("=== Ratio Bank Connector setup ===")
	w.say("")
	w.say("Everything is checked as you go. Secrets stay on this machine: Ratio never receives")
	w.say("your bank credentials or price-provider keys. Config file: %s", w.cfgPath)
	if w.docker {
		w.say("Running in Docker: files are kept under %s.", config.DefaultDataDir())
	}

	w.stepRatio(cfg)
	w.stepSources(cfg)
	w.stepSync(cfg)
	w.stepPrices(cfg)

	w.say("")
	problems := setup.Validate(cfg)
	for _, p := range problems {
		w.say("  ! %s", p.Message)
	}
	if setup.Blocking(problems) {
		return fmt.Errorf("the configuration has blocking problems: nothing was saved")
	}
	if err := config.Save(w.cfgPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	w.say("✓ Saved %s", w.cfgPath)

	w.finish(cfg)
	return nil
}

func (w *wizard) stepRatio(cfg *config.Config) {
	w.say("")
	w.say("1/4  Ratio")
	cfg.RatioManaged.URL = w.ask("  Ratio URL", firstNonEmptyStr(cfg.RatioManaged.URL, "https://www.ratio-fin.com"))
	w.retry(func() bool {
		key := w.askSecret("  API key (create one in Ratio under Connector)", cfg.RatioManaged.APIKey != "")
		if key == "" {
			key = cfg.RatioManaged.APIKey
		}
		if !w.show(w.checks.Ratio(cfg.RatioManaged.URL, key)) {
			return false
		}
		cfg.RatioManaged.APIKey = key
		return true
	})
}

func (w *wizard) stepSources(cfg *config.Config) {
	w.say("")
	w.say("2/4  Bank sources — you can use several at the same time")
	if w.yes("  Enable Banking (bank connection with consent in the browser)?", cfg.EnableBankingConfigured()) {
		w.setupEnableBanking(cfg)
	}
	for _, sf := range cfg.SimpleFIN {
		w.say("  SimpleFIN account %q is configured.", sf.Account)
	}
	for _, f := range cfg.Folders {
		w.say("  Folder for account %q: %s", f.Account, f.Path)
	}
	w.say("  SimpleFIN and folders are set in the configuration file: `bank-connector setup-simplefin`, docs/PROVIDERS.md.")
}

func (w *wizard) setupEnableBanking(cfg *config.Config) {
	cfg.EnableBanking.AppID = w.ask("  Enable Banking application id", cfg.EnableBanking.AppID)
	def := cfg.EnableBanking.PrivateKeyPath
	if w.docker && def == "" {
		def = "/app/config/enable-banking.pem"
	}
	w.retry(func() bool {
		path, err := normalizeInputPath(w.ask("  Path to the PEM private key", def))
		if err != nil {
			w.show(failResult(err.Error()))
			return false
		}
		if !w.show(w.checks.EB(cfg.EnableBanking.AppID, path)) {
			return false
		}
		cfg.EnableBanking.PrivateKeyPath = path
		cfg.EnableBanking.PrivateKeyB64 = ""
		return true
	})
	w.say("")
	w.say("  In your Enable Banking application add the redirect URL below to its redirect URLs, exactly as written.")
	w.say("  https://enablebanking.com/ needs no server: after the bank login you land there and copy the code.")
	cfg.EnableBanking.RedirectURL = w.ask("  Redirect URL", firstNonEmptyStr(cfg.EnableBanking.RedirectURL, DefaultRedirectURL))
	w.connectBank(cfg)
}

// connectBank offers the bank consent right away; it can be done later with
// `bank-connector link` or from the settings page.
func (w *wizard) connectBank(cfg *config.Config) {
	l := w.newLinker(cfg)
	existing, _ := l.store.Load()
	if !w.yes("  Connect a bank now?", len(existing) == 0) {
		w.say("  Later: `bank-connector link` (or `docker compose run --rm cli link`).")
		return
	}
	for {
		country := w.ask("  Country code", "IT")
		filter := w.ask("  Part of the bank name (Enter lists them all)", "")
		if err := l.Interactive(context.Background(), country, filter); err != nil {
			w.say("  ✗ %v", err)
			if !w.yes("  Try again?", true) {
				w.say("  Later: `bank-connector link`.")
				return
			}
			continue
		}
		if !w.yes("  Connect another bank?", false) {
			return
		}
	}
}

func (w *wizard) stepSync(cfg *config.Config) {
	w.say("")
	w.say("3/4  Schedule")
	cfg.Sync.IntervalHours = atoiOr(w.ask("  Check each bank account every how many hours", strconv.Itoa(cfg.Sync.IntervalHours)), cfg.Sync.IntervalHours)
	cfg.Sync.LookbackDays = atoiOr(w.ask("  First sync reaches back how many days", strconv.Itoa(cfg.Sync.LookbackDays)), cfg.Sync.LookbackDays)
}

func (w *wizard) stepPrices(cfg *config.Config) {
	w.say("")
	w.say("4/4  Prices (investments)")
	cfg.Prices.Enabled = w.yes("  Download prices for the tickers you hold?", cfg.Prices.Enabled)
	if !cfg.Prices.Enabled {
		return
	}
	existing := map[string]config.PriceProviderConfig{}
	for _, p := range cfg.Prices.Providers {
		existing[p.Name] = p
	}
	var providers []config.PriceProviderConfig
	type option struct {
		name       string
		needsKey   bool
		limit, gap int
		hint       string
	}
	for _, o := range []option{
		{"yahoo", false, 0, 0, "no key; unofficial, may rate-limit"},
		{"twelvedata", true, 780, 8, "free key at twelvedata.com, about 780 calls a day"},
		{"alphavantage", true, 25, 15, "free key at alphavantage.co, only 25 calls a day"},
	} {
		_, had := existing[o.name]
		if !w.yes(fmt.Sprintf("  Use %s? (%s)", o.name, o.hint), had || o.name == "yahoo") {
			continue
		}
		p := existing[o.name]
		p.Name, p.DailyLimit, p.GapSeconds = o.name, o.limit, o.gap
		if o.needsKey {
			w.retry(func() bool {
				key := w.askSecret("  "+o.name+" API key", p.APIKey != "")
				if key == "" {
					key = p.APIKey
				}
				if !w.show(w.checks.Price(o.name, key)) {
					return false
				}
				p.APIKey = key
				return true
			})
		} else {
			w.show(w.checks.Price(o.name, ""))
		}
		providers = append(providers, p)
	}
	cfg.Prices.Providers = providers
	w.say("  Providers are tried in the order above; if one is out of quota the next is used.")
	w.say("  The tickers come from your open positions in Ratio (after you approve this connector's")
	w.say("  fingerprint in the Investments page).")
	cfg.Prices.TickersFile = w.ask("  Optional extra tickers file, one per line (empty = none)", cfg.Prices.TickersFile)
	if cfg.Prices.Dir == "" {
		cfg.Prices.Dir = filepath.Join(config.DefaultDataDir(), "prices")
	}
	if cfg.Prices.SealingKeyPath == "" {
		cfg.Prices.SealingKeyPath = filepath.Join(config.DefaultDataDir(), "sealing-key.pem")
	}
	if cfg.Prices.IntervalHours <= 0 {
		cfg.Prices.IntervalHours = 24
	}
}

func (w *wizard) finish(cfg *config.Config) {
	w.say("")
	if cfg.Prices.Enabled {
		w.say("Prices: run `bank-connector prices fingerprint` and compare the fingerprint in Ratio → Investments.")
	}
	if w.docker {
		w.say("Docker: start the connector with `docker compose up -d` (it runs `sync --daemon`).")
		w.say("Config changes need `docker compose restart`.")
	} else if w.installService != nil && w.yes("Install as a background service that starts at login?", true) {
		if err := w.installService(w.cfgPath); err != nil {
			w.say("  ✗ Could not install the service: %v", err)
			w.say("  You can retry with `bank-connector service install`.")
		} else {
			w.say("  ✓ The connector is running in the background.")
		}
	}
	w.say("")
	w.say("Next: in Ratio → Connector add your bank accounts. For SimpleFIN and folder accounts give")
	w.say("the account the same name as configured here. Enable Banking accounts use the provider")
	w.say("\"enablebanking-local\" and are named after the IBAN or the bank; renew with `bank-connector sessions --renew`.")
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
		return n
	}
	return def
}

func failResult(msg string) setup.Result { return setup.Result{Message: msg} }
