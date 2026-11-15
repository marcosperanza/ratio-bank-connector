// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package status

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrackerWritesWhatTheTrayReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	tr := NewTracker(path, "1.2.3")
	tr.BankCycle(nil)
	tr.PricesRun(12, 2, errors.New("boom"))
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.2.3" || !got.Bank.OK || got.Prices.Tickers != 12 || got.Prices.Failed != 2 || got.Prices.Error != "boom" {
		t.Fatalf("%+v", got)
	}
	if !got.Fresh(time.Now()) || got.Fresh(time.Now().Add(5*time.Minute)) {
		t.Fatal("freshness window is wrong")
	}
}

func TestStopAndDeadProcessAreNotAlive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	tr := NewTracker(path, "x")
	tr.BankCycle(nil)
	got, _ := Read(path)
	if !got.Alive(time.Now()) || got.PID != os.Getpid() {
		t.Fatalf("a running daemon must read as alive: %+v", got)
	}
	tr.Stop()
	got, _ = Read(path)
	if got.Alive(time.Now()) {
		t.Fatal("a stopped daemon must not read as alive, even though the file is fresh")
	}
	got.Stopped, got.PID = false, 999999
	if got.Alive(time.Now()) {
		t.Fatal("a recorded process that no longer exists must not read as alive")
	}
}
