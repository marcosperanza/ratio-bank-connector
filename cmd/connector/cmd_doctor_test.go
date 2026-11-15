// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"path/filepath"
	"testing"
)

func TestValidateRatioURL(t *testing.T) {
	t.Parallel()

	for _, valid := range []string{"https://ratio.example", "http://localhost:3000", "http://127.0.0.1:3000"} {
		if err := validateRatioURL(valid); err != nil {
			t.Errorf("expected %q to be valid: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "ratio.example", "http://ratio.example"} {
		if err := validateRatioURL(invalid); err == nil {
			t.Errorf("expected %q to be invalid", invalid)
		}
	}
}

func TestNormalizeInputPath(t *testing.T) {
	t.Parallel()
	got, err := normalizeInputPath("relative/provider.pem")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
}

func TestSetupKeepsAddonAliases(t *testing.T) {
	t.Parallel()
	cmd := cmdSetupWizard()
	if cmd.Name() != "setup" {
		t.Fatalf("expected setup command, got %q", cmd.Name())
	}
	for _, alias := range []string{"setup-addon", "setup-managed"} {
		if !cmd.HasAlias(alias) {
			t.Fatalf("expected backward-compatible alias %s", alias)
		}
	}
}
