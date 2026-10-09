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
