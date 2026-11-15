// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLastSyncAtKeepsPricesSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	src := `enable_banking:
  app_id: app
sync:
  interval_hours: 6
  lookback_days: 30
prices:
  enabled: true
  tickers_file: /tmp/tickers.txt
  providers:
    - name: yahoo
    - name: twelvedata
      api_key: secret
      daily_limit: 780
      gap_seconds: 8
  overrides:
    - ticker: VWCE.DE
      provider: yahoo
      symbol: VWCE.MI
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveLastSyncAt(path, cfg); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := again.Prices
	if !p.Enabled || len(p.Providers) != 2 || p.Providers[1].APIKey != "secret" || p.Providers[1].DailyLimit != 780 || len(p.Overrides) != 1 || p.Overrides[0] != (PriceOverride{Ticker: "VWCE.DE", Provider: "yahoo", Symbol: "VWCE.MI"}) {
		t.Fatalf("prices lost after save: %+v", p)
	}
	if p.IntervalHours != 24 {
		t.Fatalf("default interval %d", p.IntervalHours)
	}
}
