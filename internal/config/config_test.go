// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEffectiveArchiveMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "managed defaults off", cfg: Config{Mode: "ratio_managed"}, want: ArchiveOff},
		{name: "standalone remains compatible", cfg: Config{Mode: "standalone"}, want: ArchiveCSVAndRaw},
		{name: "explicit csv", cfg: Config{Mode: "ratio_managed", Sync: SyncConfig{ArchiveMode: ArchiveCSV}}, want: ArchiveCSV},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.cfg.EffectiveArchiveMode(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadFromTheEnvironmentAlone(t *testing.T) {
	t.Setenv("RATIO_API_KEY", "key-1")
	t.Setenv("RATIO_URL", "https://ratio.example")
	t.Setenv("RATIO_ENABLE_BANKING_APP_ID", "app-1")
	t.Setenv("RATIO_ENABLE_BANKING_PRIVATE_KEY_B64", "cGVt")
	t.Setenv("RATIO_SYNC_INTERVAL_HOURS", "12")

	cfg, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "ratio_managed" || cfg.RatioManaged.APIKey != "key-1" || cfg.RatioManaged.URL != "https://ratio.example" {
		t.Fatalf("managed settings: %+v", cfg.RatioManaged)
	}
	if cfg.EnableBanking.AppID != "app-1" || cfg.EnableBanking.PrivateKeyB64 != "cGVt" {
		t.Fatalf("provider settings: %+v", cfg.EnableBanking)
	}
	if cfg.Sync.IntervalHours != 12 || cfg.Sync.LookbackDays != 30 || cfg.RatioManaged.PollIntervalSeconds != 1800 {
		t.Fatalf("defaults or overrides lost: %+v %+v", cfg.Sync, cfg.RatioManaged)
	}
	if got := cfg.EffectiveArchiveMode(); got != ArchiveOff {
		t.Fatalf("a container must not archive plaintext by default, got %q", got)
	}
}

func TestMissingFileWithoutAnAPIKeyIsStillAnError(t *testing.T) {
	t.Setenv("RATIO_API_KEY", "")
	if _, err := Load(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("expected an error: nothing says how to run")
	}
}

func TestEnvironmentOverridesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "mode: ratio_managed\nenable_banking:\n  app_id: from-file\nratio_managed:\n  url: https://file.example\n  api_key: file-key\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RATIO_API_KEY", "env-key")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RatioManaged.APIKey != "env-key" || cfg.EnableBanking.AppID != "from-file" {
		t.Fatalf("got %+v %+v", cfg.RatioManaged, cfg.EnableBanking)
	}
}

func TestLoadReadsFoldersFromAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `mode: ratio_managed
enable_banking:
  app_id: app
ratio_managed:
  url: https://ratio.example
  api_key: key
folders:
  - account: acc-1
    path: ~/Banca/ING
    format: csv
    profile:
      delimiter: ";"
      skip_rows: 2
      encoding: latin1
      date_layout: "02/01/2006"
      decimal: ","
      thousands: "."
      columns: { date: 0, description: 2, amount: 3 }
  - account: acc-2
    path: /data/ofx
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Folders) != 2 {
		t.Fatalf("want 2 folders, got %d", len(cfg.Folders))
	}
	f := cfg.Folders[0]
	if f.Account != "acc-1" || f.Path != "~/Banca/ING" || f.Format != "csv" {
		t.Fatalf("folder: %+v", f)
	}
	p := f.Profile
	if p.Delimiter != ";" || p.SkipRows != 2 || p.Encoding != "latin1" || p.DateLayout != "02/01/2006" || p.Decimal != "," || p.Thousands != "." {
		t.Fatalf("profile: %+v", p)
	}
	if p.Columns["date"] != 0 || p.Columns["description"] != 2 || p.Columns["amount"] != 3 {
		t.Fatalf("columns: %+v", p.Columns)
	}
	if cfg.Folders[1].Format != "" {
		t.Fatalf("an unset format must stay empty so the extension decides: %+v", cfg.Folders[1])
	}
}
