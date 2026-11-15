// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
)

func TestSimplefinAccountsPreferTheEnvironmentOverTheFile(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{SimpleFIN: []config.SimpleFINConfig{
		{Account: "a", AccessURLEnv: "SFIN_A", AccessURL: "https://file:x@h/p", SFINAccount: "ACT-1"},
		{Account: "b", AccessURL: "https://file:y@h/p", SFINAccount: "ACT-2"},
		{Account: "c", AccessURLEnv: "UNSET", SFINAccount: "ACT-3"},
		{AccessURL: "https://held:x@h/p"},
	}}
	env := map[string]string{"SFIN_A": "https://env:z@h/p"}

	got := simplefinAccounts(cfg, func(k string) string { return env[k] })

	if got["a"].AccessURL != "https://env:z@h/p" || got["a"].ID != "ACT-1" {
		t.Fatalf("a: %+v", got["a"])
	}
	if got["b"].AccessURL != "https://file:y@h/p" {
		t.Fatalf("b: %+v", got["b"])
	}
	if _, held := got[""]; held {
		t.Fatal("a saved connection with no Ratio account must not become an account")
	}
	if got["c"].AccessURL != "" {
		t.Fatalf("c should be left without a URL: %+v", got["c"])
	}
}
