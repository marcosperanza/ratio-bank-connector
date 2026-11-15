// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const legacyConfigPath = "./config/config.yaml"

const (
	ArchiveOff       = "off"
	ArchiveCSV       = "csv"
	ArchiveCSVAndRaw = "csv_and_raw"
)

// DefaultConfigPath returns a stable per-user config location on Linux, macOS,
// and Windows. A repository-local legacy config remains discoverable so
// existing source and Docker installations continue to work.
func DefaultConfigPath() string {
	if _, err := os.Stat(legacyConfigPath); err == nil {
		return legacyConfigPath
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return legacyConfigPath
	}
	return filepath.Join(base, "Ratio", "bank-connector", "config.yaml")
}

// DataDirEnv lets a container point all state at a mounted volume without
// writing absolute paths into the config.
const DataDirEnv = "BANK_CONNECTOR_DATA_DIR"

// DefaultDataDir returns persistent per-user storage instead of relying on the
// process working directory, which is unpredictable for background services.
func DefaultDataDir() string {
	if dir := os.Getenv(DataDirEnv); dir != "" {
		return dir
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return "./data"
	}
	return filepath.Join(base, "Ratio", "bank-connector", "data")
}

// EBSessionsPath is where the locally authorized Enable Banking sessions live.
func EBSessionsPath() string { return filepath.Join(DefaultDataDir(), "eb-sessions.json") }

type Config struct {
	Mode          string              `mapstructure:"mode"` // "standalone" (default) | "ratio_managed" (local add-on controlled from Ratio Web)
	EnableBanking EnableBankingConfig `mapstructure:"enable_banking"`
	Sync          SyncConfig          `mapstructure:"sync"`
	Ratio         RatioConfig         `mapstructure:"ratio"`
	RatioManaged  RatioManagedConfig  `mapstructure:"ratio_managed"`
	Accounts      []AccountConfig     `mapstructure:"accounts"`
	// Folders maps accounts created in Ratio to a folder of exports on this
	// computer. The path never leaves it.
	Folders []FolderConfig `mapstructure:"folders"`
	// SimpleFIN maps accounts created in Ratio to an account behind a SimpleFIN
	// access URL. The URL carries credentials and stays on this computer.
	SimpleFIN []SimpleFINConfig `mapstructure:"simplefin"`
	Prices    PricesConfig      `mapstructure:"prices"`
}

// SimpleFINConfig reads one SimpleFIN account for one Ratio account.
type SimpleFINConfig struct {
	Account string `mapstructure:"account"` // Ratio's bank account id
	// AccessURLEnv names the environment variable holding the access URL, so a
	// container never keeps it in a file. AccessURL is the plain alternative.
	AccessURLEnv string `mapstructure:"access_url_env"`
	AccessURL    string `mapstructure:"access_url"`
	// SFINAccount is the account id inside the SimpleFIN connection.
	SFINAccount string `mapstructure:"sfin_account"`
}

// FolderConfig reads the exports a user keeps in a folder for one account.
type FolderConfig struct {
	Account string        `mapstructure:"account"` // Ratio's bank account id
	Path    string        `mapstructure:"path"`
	Format  string        `mapstructure:"format"` // csv | ofx | empty for both
	Profile FolderProfile `mapstructure:"profile"`
}

// FolderProfile describes a bank's CSV; see foldersource.Profile.
type FolderProfile struct {
	Delimiter  string         `mapstructure:"delimiter"`
	SkipRows   int            `mapstructure:"skip_rows"`
	Encoding   string         `mapstructure:"encoding"`
	DateLayout string         `mapstructure:"date_layout"`
	Columns    map[string]int `mapstructure:"columns"`
	Decimal    string         `mapstructure:"decimal"`
	Thousands  string         `mapstructure:"thousands"`
	Currency   string         `mapstructure:"currency"`
}

// PricesConfig drives the connector's own price downloads. Provider keys live
// here, on the user's machine, and never travel to Ratio.
type PricesConfig struct {
	Enabled        bool                  `mapstructure:"enabled"`
	Dir            string                `mapstructure:"dir"`
	TickersFile    string                `mapstructure:"tickers_file"`
	SealingKeyPath string                `mapstructure:"sealing_key_path"`
	IntervalHours  int                   `mapstructure:"interval_hours"`
	Providers      []PriceProviderConfig `mapstructure:"providers"` // fallback order
	Overrides      []PriceOverride       `mapstructure:"overrides"`
}

// PriceOverride pins a spelling for one ticker on one provider. It is a list,
// not a map keyed by ticker, because Viper splits map keys on dots (VWCE.DE).
type PriceOverride struct {
	Ticker   string `mapstructure:"ticker" json:"ticker"`
	Provider string `mapstructure:"provider" json:"provider"`
	Symbol   string `mapstructure:"symbol" json:"symbol"` // for twelvedata: SYMBOL@EXCHANGE
}

type PriceProviderConfig struct {
	Name       string `mapstructure:"name"` // yahoo | twelvedata | alphavantage
	APIKey     string `mapstructure:"api_key"`
	DailyLimit int    `mapstructure:"daily_limit"`
	GapSeconds int    `mapstructure:"gap_seconds"`
}

type EnableBankingConfig struct {
	AppID          string `mapstructure:"app_id"`
	PrivateKeyPath string `mapstructure:"private_key_path"`
	PrivateKeyB64  string `mapstructure:"private_key_b64"` // alternative to path
	// RedirectURL, CallbackCert and CallbackKey serve `enablebanking-local`: the
	// consent is given on this machine and the bank sends the user back here.
	RedirectURL  string `mapstructure:"redirect_url"`
	CallbackCert string `mapstructure:"callback_cert"`
	CallbackKey  string `mapstructure:"callback_key"`
}

// EnableBankingConfigured reports whether Enable Banking credentials are set.
func (c *Config) EnableBankingConfigured() bool { return c.EnableBanking.AppID != "" }

type SyncConfig struct {
	IntervalHours int    `mapstructure:"interval_hours"`
	LookbackDays  int    `mapstructure:"lookback_days"`
	CSVDir        string `mapstructure:"csv_dir"`
	ArchiveMode   string `mapstructure:"archive_mode"`
}

// EffectiveArchiveMode keeps standalone behavior compatible while making
// managed mode private by default.
func (c *Config) EffectiveArchiveMode() string {
	if c.Sync.ArchiveMode != "" {
		return c.Sync.ArchiveMode
	}
	if c.Mode == "ratio_managed" {
		return ArchiveOff
	}
	return ArchiveCSVAndRaw
}

type RatioConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	URL                string `mapstructure:"url"`
	ConnectorToken     string `mapstructure:"connector_token"`
	EncryptedMasterKey string `mapstructure:"encrypted_master_key"`
	MasterKeyIV        string `mapstructure:"master_key_iv"`
	EncryptedSearchKey string `mapstructure:"encrypted_search_key"`
	SearchKeyIV        string `mapstructure:"search_key_iv"`
}

type RatioManagedConfig struct {
	URL                 string `mapstructure:"url"`
	APIKey              string `mapstructure:"api_key"`
	PollIntervalSeconds int    `mapstructure:"poll_interval_seconds"`
}

type AccountConfig struct {
	EBAccountID      string    `mapstructure:"eb_account_id"`
	RatioWorkspaceID string    `mapstructure:"ratio_workspace_id"`
	LastSyncAt       time.Time `mapstructure:"last_sync_at"`
}

// UpdateFile reads the YAML config as a plain map, lets mutate change it, and
// writes it back with owner-only permissions. Keys it does not know are kept,
// so a section added by a newer version or by hand survives. A missing file
// starts empty.
func UpdateFile(path string, mutate func(map[string]any) error) error {
	m := map[string]any{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if m == nil {
			m = map[string]any{}
		}
	case !os.IsNotExist(err):
		return err
	}
	if err := mutate(m); err != nil {
		return err
	}
	out, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

// SaveLastSyncAt persists updated last_sync_at timestamps back to the config
// file. It rewrites only the accounts list: everything else in the file,
// including folders and simplefin, stays as the user left it.
func SaveLastSyncAt(path string, cfg *Config) error {
	accounts := make([]map[string]any, 0, len(cfg.Accounts))
	for _, acc := range cfg.Accounts {
		entry := map[string]any{"eb_account_id": acc.EBAccountID}
		if acc.RatioWorkspaceID != "" {
			entry["ratio_workspace_id"] = acc.RatioWorkspaceID
		}
		if !acc.LastSyncAt.IsZero() {
			entry["last_sync_at"] = acc.LastSyncAt.UTC().Format(time.RFC3339)
		}
		accounts = append(accounts, entry)
	}
	return UpdateFile(path, func(m map[string]any) error {
		m["accounts"] = accounts
		return nil
	})
}

// envBindings names the environment variable behind each setting a container
// needs. They are explicit because viper's automatic mapping cannot reach a
// nested key such as ratio_managed.api_key, and because RATIO_API_KEY reads
// better than RATIO_RATIO_MANAGED_API_KEY in a hosting dashboard.
var envBindings = map[string]string{
	"mode":                                "RATIO_MODE",
	"ratio_managed.url":                   "RATIO_URL",
	"ratio_managed.api_key":               "RATIO_API_KEY",
	"ratio_managed.poll_interval_seconds": "RATIO_POLL_INTERVAL_SECONDS",
	"enable_banking.app_id":               "RATIO_ENABLE_BANKING_APP_ID",
	"enable_banking.private_key_b64":      "RATIO_ENABLE_BANKING_PRIVATE_KEY_B64",
	"enable_banking.private_key_path":     "RATIO_ENABLE_BANKING_PRIVATE_KEY_PATH",
	"sync.interval_hours":                 "RATIO_SYNC_INTERVAL_HOURS",
	"sync.lookback_days":                  "RATIO_SYNC_LOOKBACK_DAYS",
	"sync.archive_mode":                   "RATIO_ARCHIVE_MODE",
}

// Load reads the config file, with environment variables taking precedence.
// Without a file it runs from the environment alone when RATIO_API_KEY is
// set, which is how a container with no volume is configured: the state that
// matters (last sync, account links) lives in Ratio, not on the disk.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	for key, name := range envBindings {
		_ = v.BindEnv(key, name)
	}

	v.SetDefault("sync.interval_hours", 6)
	v.SetDefault("sync.lookback_days", 30)
	v.SetDefault("sync.csv_dir", DefaultDataDir())
	v.SetDefault("prices.interval_hours", 24)
	v.SetDefault("prices.sealing_key_path", filepath.Join(DefaultDataDir(), "sealing-key.pem"))
	v.SetDefault("prices.dir", filepath.Join(DefaultDataDir(), "prices"))
	// Above Neon's five-minute autosuspend: only used while realtime is down.
	v.SetDefault("ratio_managed.poll_interval_seconds", 1800)

	if err := v.ReadInConfig(); err != nil {
		missing := errors.Is(err, fs.ErrNotExist)
		if !missing || os.Getenv("RATIO_API_KEY") == "" {
			return nil, err
		}
		v.SetDefault("mode", "ratio_managed")
	}

	var cfg Config
	if err := v.Unmarshal(&cfg, viper.DecodeHook(
		mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			stringToTimeHook(time.RFC3339),
			mapstructure.StringToSliceHookFunc(","),
		),
	)); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func stringToTimeHook(layout string) mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data any) (any, error) {
		if f.Kind() != reflect.String || t != reflect.TypeOf(time.Time{}) {
			return data, nil
		}
		s := data.(string)
		if s == "" {
			return time.Time{}, nil
		}
		return time.Parse(layout, s)
	}
}

// ownedKeys are the sections the settings page and the setup wizard write. Save replaces these and leaves every
// other key of the file (folders, simplefin, accounts, anything added by hand or by a newer version) alone.
var ownedKeys = []string{"mode", "enable_banking", "sync", "ratio_managed", "prices"}

type yamlConfig struct {
	Mode          string `yaml:"mode,omitempty"`
	EnableBanking struct {
		AppID          string `yaml:"app_id"`
		PrivateKeyPath string `yaml:"private_key_path,omitempty"`
		PrivateKeyB64  string `yaml:"private_key_b64,omitempty"`
		RedirectURL    string `yaml:"redirect_url,omitempty"`
		CallbackCert   string `yaml:"callback_cert,omitempty"`
		CallbackKey    string `yaml:"callback_key,omitempty"`
	} `yaml:"enable_banking"`
	Sync struct {
		IntervalHours int    `yaml:"interval_hours"`
		LookbackDays  int    `yaml:"lookback_days"`
		CSVDir        string `yaml:"csv_dir"`
		ArchiveMode   string `yaml:"archive_mode,omitempty"`
	} `yaml:"sync"`
	RatioManaged struct {
		URL                 string `yaml:"url,omitempty"`
		APIKey              string `yaml:"api_key,omitempty"`
		PollIntervalSeconds int    `yaml:"poll_interval_seconds,omitempty"`
	} `yaml:"ratio_managed,omitempty"`
	Prices *yamlPrices `yaml:"prices,omitempty"`
}

type yamlPrices struct {
	Enabled        bool                `yaml:"enabled"`
	Dir            string              `yaml:"dir,omitempty"`
	TickersFile    string              `yaml:"tickers_file,omitempty"`
	SealingKeyPath string              `yaml:"sealing_key_path,omitempty"`
	IntervalHours  int                 `yaml:"interval_hours,omitempty"`
	Providers      []yamlPriceProvider `yaml:"providers,omitempty"`
	Overrides      []yamlPriceOverride `yaml:"overrides,omitempty"`
}

type yamlPriceOverride struct {
	Ticker   string `yaml:"ticker"`
	Provider string `yaml:"provider"`
	Symbol   string `yaml:"symbol"`
}

type yamlPriceProvider struct {
	Name       string `yaml:"name"`
	APIKey     string `yaml:"api_key,omitempty"`
	DailyLimit int    `yaml:"daily_limit,omitempty"`
	GapSeconds int    `yaml:"gap_seconds,omitempty"`
}

// Save writes the sections the settings page owns (mode 0600, directory created) through UpdateFile, so the
// sections it does not own survive.
func Save(path string, cfg *Config) error {
	var y yamlConfig
	y.Mode = cfg.Mode
	y.EnableBanking.AppID = cfg.EnableBanking.AppID
	y.EnableBanking.PrivateKeyPath = cfg.EnableBanking.PrivateKeyPath
	y.EnableBanking.PrivateKeyB64 = cfg.EnableBanking.PrivateKeyB64
	y.EnableBanking.RedirectURL = cfg.EnableBanking.RedirectURL
	y.EnableBanking.CallbackCert = cfg.EnableBanking.CallbackCert
	y.EnableBanking.CallbackKey = cfg.EnableBanking.CallbackKey
	y.Sync.IntervalHours = cfg.Sync.IntervalHours
	y.Sync.LookbackDays = cfg.Sync.LookbackDays
	y.Sync.CSVDir = cfg.Sync.CSVDir
	y.Sync.ArchiveMode = cfg.Sync.ArchiveMode
	y.RatioManaged.URL = cfg.RatioManaged.URL
	y.RatioManaged.APIKey = cfg.RatioManaged.APIKey
	y.RatioManaged.PollIntervalSeconds = cfg.RatioManaged.PollIntervalSeconds
	if p := cfg.Prices; p.Enabled || len(p.Providers) > 0 {
		yp := &yamlPrices{Enabled: p.Enabled, Dir: p.Dir, TickersFile: p.TickersFile, SealingKeyPath: p.SealingKeyPath, IntervalHours: p.IntervalHours}
		for _, pp := range p.Providers {
			yp.Providers = append(yp.Providers, yamlPriceProvider(pp))
		}
		for _, o := range p.Overrides {
			yp.Overrides = append(yp.Overrides, yamlPriceOverride(o))
		}
		y.Prices = yp
	}
	data, err := yaml.Marshal(&y)
	if err != nil {
		return err
	}
	owned := map[string]any{}
	if err := yaml.Unmarshal(data, &owned); err != nil {
		return err
	}
	return UpdateFile(path, func(m map[string]any) error {
		for _, key := range ownedKeys {
			if v, ok := owned[key]; ok {
				m[key] = v
			} else {
				delete(m, key)
			}
		}
		return nil
	})
}
