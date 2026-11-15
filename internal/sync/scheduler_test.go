// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package sync

import (
	"testing"
	"time"
)

func TestManagedPollIntervalUsesSlowFallbackOnlyWhileRealtimeIsConnected(t *testing.T) {
	t.Parallel()
	offline := 30 * time.Second
	if got := managedPollInterval(false, offline); got != offline {
		t.Fatalf("offline connector should retain compatibility polling, got %s", got)
	}
	if got := managedPollInterval(true, offline); got != 60*time.Minute {
		t.Fatalf("connected connector should use the slow fallback, got %s", got)
	}
}

func TestRealtimeNeedsStartReplacesAClientDownTooLong(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		running   bool
		connected bool
		downSince time.Time
		want      bool
	}{
		{"no client yet", false, false, time.Time{}, true},
		{"connected", true, true, time.Time{}, false},
		{"just started, not connected yet", true, false, now.Add(-time.Minute), false},
		{"down for a while", true, false, now.Add(-realtimeRestartAfter + time.Second), false},
		{"down too long", true, false, now.Add(-realtimeRestartAfter), true},
	}
	for _, c := range cases {
		if got := realtimeNeedsStart(c.running, c.connected, c.downSince, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
