// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ui

import (
	"net/http"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/crypto"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/sealing"
)

// sealingKeyPath is where the sealing key lives, with the default when the configuration does not say.
func (s *Server) sealingKeyPath(cfg *config.Config) string {
	if s.SealingKeyPath != "" {
		return s.SealingKeyPath
	}
	if cfg.Prices.SealingKeyPath != "" {
		return cfg.Prices.SealingKeyPath
	}
	return config.DefaultDataDir() + "/sealing-key.pem"
}

// existingFingerprint is the fingerprint of the sealing key if there is one already; it never creates a key.
func (s *Server) existingFingerprint(cfg *config.Config) string {
	key, ok := sealing.Load(s.sealingKeyPath(cfg))
	if !ok {
		return ""
	}
	fp, err := crypto.SealingFingerprint(&key.PublicKey)
	if err != nil {
		return ""
	}
	return crypto.FormatFingerprint(fp)
}

// handleFingerprint does what `bank-connector prices fingerprint` does: makes the sealing key on first use, registers its
// public half with Ratio, and returns the fingerprint to compare with the one Ratio shows in Investments.
func (s *Server) handleFingerprint(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.load()
	s.mu.Unlock()
	key, created, err := sealing.LoadOrCreate(s.sealingKeyPath(cfg))
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	fp, err := crypto.SealingFingerprint(&key.PublicKey)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	out := map[string]any{"ok": true, "fingerprint": crypto.FormatFingerprint(fp), "created": created, "registered": false}
	switch {
	case cfg.RatioManaged.APIKey == "" || cfg.RatioManaged.URL == "":
		out["note"] = "no-ratio-key"
	default:
		spki, err := crypto.SealingPublicKeySPKI(&key.PublicKey)
		if err == nil {
			err = ratio.NewClient(cfg.RatioManaged.URL, cfg.RatioManaged.APIKey).RegisterSealingKey(spki)
		}
		if err != nil {
			out["note"] = "register-failed"
			out["error"] = err.Error()
		} else {
			out["registered"] = true
		}
	}
	writeJSON(w, 200, out)
}

// handleRefreshPrices is the "refresh now" button: one download of every tracked ticker, delivered to Ratio.
// One at a time: a second click while the first is running is told so instead of doubling the provider calls.
func (s *Server) handleRefreshPrices(w http.ResponseWriter, r *http.Request) {
	if s.RefreshPrices == nil {
		writeJSON(w, 200, map[string]any{"ok": false, "message": "unavailable"})
		return
	}
	if !s.refreshing.CompareAndSwap(false, true) {
		writeJSON(w, 200, map[string]any{"ok": false, "busy": true, "message": "already running"})
		return
	}
	defer s.refreshing.Store(false)
	tickers, failed, err := s.RefreshPrices(r.Context())
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "tickers": tickers, "failed": failed})
}
