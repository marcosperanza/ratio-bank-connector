// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import "testing"

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
