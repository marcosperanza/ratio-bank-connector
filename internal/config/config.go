// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
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

// DefaultDataDir returns persistent per-user storage instead of relying on the
// process working directory, which is unpredictable for background services.
func DefaultDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return "./data"
	}
	return filepath.Join(base, "Ratio", "bank-connector", "data")
}

type Config struct {
	Mode          string              `mapstructure:"mode"` // "standalone" (default) | "ratio_managed" (local add-on controlled from Ratio Web)
	EnableBanking EnableBankingConfig `mapstructure:"enable_banking"`
	Sync          SyncConfig          `mapstructure:"sync"`
	Ratio         RatioConfig         `mapstructure:"ratio"`
	RatioManaged  RatioManagedConfig  `mapstructure:"ratio_managed"`
	Accounts      []AccountConfig     `mapstructure:"accounts"`
}

type EnableBankingConfig struct {
	AppID          string `mapstructure:"app_id"`
	PrivateKeyPath string `mapstructure:"private_key_path"`
	PrivateKeyB64  string `mapstructure:"private_key_b64"` // alternative to path
}

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

// yamlConfig mirrors Config for YAML marshaling (yaml tags).
type yamlConfig struct {
	Mode          string `yaml:"mode,omitempty"`
	EnableBanking struct {
		AppID          string `yaml:"app_id"`
		PrivateKeyPath string `yaml:"private_key_path,omitempty"`
		PrivateKeyB64  string `yaml:"private_key_b64,omitempty"`
	} `yaml:"enable_banking"`
	Sync struct {
		IntervalHours int    `yaml:"interval_hours"`
		LookbackDays  int    `yaml:"lookback_days"`
		CSVDir        string `yaml:"csv_dir"`
		ArchiveMode   string `yaml:"archive_mode,omitempty"`
	} `yaml:"sync"`
	Ratio struct {
		Enabled            bool   `yaml:"enabled"`
		URL                string `yaml:"url,omitempty"`
		ConnectorToken     string `yaml:"connector_token,omitempty"`
		EncryptedMasterKey string `yaml:"encrypted_master_key,omitempty"`
		MasterKeyIV        string `yaml:"master_key_iv,omitempty"`
		EncryptedSearchKey string `yaml:"encrypted_search_key,omitempty"`
		SearchKeyIV        string `yaml:"search_key_iv,omitempty"`
	} `yaml:"ratio"`
	RatioManaged struct {
		URL                 string `yaml:"url,omitempty"`
		APIKey              string `yaml:"api_key,omitempty"`
		PollIntervalSeconds int    `yaml:"poll_interval_seconds,omitempty"`
	} `yaml:"ratio_managed,omitempty"`
	Accounts []struct {
		EBAccountID      string `yaml:"eb_account_id"`
		RatioWorkspaceID string `yaml:"ratio_workspace_id,omitempty"`
		LastSyncAt       string `yaml:"last_sync_at,omitempty"`
	} `yaml:"accounts"`
}

// SaveLastSyncAt persists updated last_sync_at timestamps back to the config file.
func SaveLastSyncAt(path string, cfg *Config) error {
	var y yamlConfig
	y.Mode = cfg.Mode
	y.EnableBanking.AppID = cfg.EnableBanking.AppID
	y.EnableBanking.PrivateKeyPath = cfg.EnableBanking.PrivateKeyPath
	y.EnableBanking.PrivateKeyB64 = cfg.EnableBanking.PrivateKeyB64
	y.Sync.IntervalHours = cfg.Sync.IntervalHours
	y.Sync.LookbackDays = cfg.Sync.LookbackDays
	y.Sync.CSVDir = cfg.Sync.CSVDir
	y.Sync.ArchiveMode = cfg.Sync.ArchiveMode
	y.Ratio.Enabled = cfg.Ratio.Enabled
	y.Ratio.URL = cfg.Ratio.URL
	y.Ratio.ConnectorToken = cfg.Ratio.ConnectorToken
	y.Ratio.EncryptedMasterKey = cfg.Ratio.EncryptedMasterKey
	y.Ratio.MasterKeyIV = cfg.Ratio.MasterKeyIV
	y.Ratio.EncryptedSearchKey = cfg.Ratio.EncryptedSearchKey
	y.Ratio.SearchKeyIV = cfg.Ratio.SearchKeyIV
	y.RatioManaged.URL = cfg.RatioManaged.URL
	y.RatioManaged.APIKey = cfg.RatioManaged.APIKey
	y.RatioManaged.PollIntervalSeconds = cfg.RatioManaged.PollIntervalSeconds
	for _, acc := range cfg.Accounts {
		entry := struct {
			EBAccountID      string `yaml:"eb_account_id"`
			RatioWorkspaceID string `yaml:"ratio_workspace_id,omitempty"`
			LastSyncAt       string `yaml:"last_sync_at,omitempty"`
		}{
			EBAccountID:      acc.EBAccountID,
			RatioWorkspaceID: acc.RatioWorkspaceID,
		}
		if !acc.LastSyncAt.IsZero() {
			entry.LastSyncAt = acc.LastSyncAt.UTC().Format(time.RFC3339)
		}
		y.Accounts = append(y.Accounts, entry)
	}
	data, err := yaml.Marshal(&y)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvPrefix("RATIO")
	v.AutomaticEnv()

	v.SetDefault("sync.interval_hours", 6)
	v.SetDefault("sync.lookback_days", 30)
	v.SetDefault("sync.csv_dir", DefaultDataDir())
	// Above Neon's five-minute autosuspend: only used while realtime is down.
	v.SetDefault("ratio_managed.poll_interval_seconds", 600)

	if err := v.ReadInConfig(); err != nil {
		return nil, err
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
