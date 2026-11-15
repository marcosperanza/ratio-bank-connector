// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package status writes a small JSON file the daemon keeps current, so the
// tray app and the settings UI can show what the connector is doing without
// talking to the process.
package status

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Cycle struct {
	At    time.Time `json:"at,omitempty"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
}

type Prices struct {
	Cycle
	Tickers int `json:"tickers"`
	Failed  int `json:"failed"`
}

type Status struct {
	Version   string    `json:"version"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Stopped is set by a clean shutdown, so readers do not have to wait for
	// the file to go stale.
	Stopped bool   `json:"stopped,omitempty"`
	Bank    Cycle  `json:"bank"`
	Prices  Prices `json:"prices"`
}

// Fresh reports whether the daemon wrote the file recently enough to be alive.
func (s Status) Fresh(now time.Time) bool { return !s.Stopped && now.Sub(s.UpdatedAt) < 2*time.Minute }

// Alive is Fresh plus a check that the recorded process still exists, which
// catches a daemon that was killed without time to say so.
func (s Status) Alive(now time.Time) bool { return s.Fresh(now) && ProcessExists(s.PID) }

type Tracker struct {
	path string
	mu   sync.Mutex
	s    Status
}

func NewTracker(path, version string) *Tracker {
	now := time.Now().UTC()
	return &Tracker{path: path, s: Status{Version: version, PID: os.Getpid(), StartedAt: now, UpdatedAt: now}}
}

func (t *Tracker) write() {
	t.s.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(t.s, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o700); err != nil {
		return
	}
	tmp := t.path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, t.path)
	}
}

func cycle(err error) Cycle {
	c := Cycle{At: time.Now().UTC(), OK: err == nil}
	if err != nil {
		c.Error = err.Error()
	}
	return c
}

// BankCycle records the outcome of one bank sync cycle.
func (t *Tracker) BankCycle(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.s.Bank = cycle(err)
	t.write()
}

// PricesRun records one price download run.
func (t *Tracker) PricesRun(total, failed int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.s.Prices = Prices{Cycle: cycle(err), Tickers: total, Failed: failed}
	t.write()
}

// Stop records a clean shutdown.
func (t *Tracker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.s.Stopped = true
	t.write()
}

// Heartbeat refreshes the file every interval until ctx ends.
func (t *Tracker) Heartbeat(ctx context.Context, interval time.Duration) {
	t.mu.Lock()
	t.write()
	t.mu.Unlock()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			t.mu.Lock()
			t.write()
			t.mu.Unlock()
		}
	}
}

// Read loads the status file; a missing file is a connector that never ran.
func Read(path string) (Status, error) {
	var s Status
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(data, &s)
}
