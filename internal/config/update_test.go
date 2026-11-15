// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveLastSyncAtKeepsEverythingElseInTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "mode: ratio_managed\nfolders:\n  - account: a1\n    path: /data\nsimplefin:\n  - account: a2\n    access_url_env: SFIN\n    sfin_account: ACT-1\naccounts:\n  - eb_account_id: eb1\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Accounts: []AccountConfig{{EBAccountID: "eb1", LastSyncAt: time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC)}}}

	if err := SaveLastSyncAt(path, cfg); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	for _, want := range []string{"folders:", "path: /data", "simplefin:", "access_url_env: SFIN", "last_sync_at: \"2026-11-05T08:00:00Z\"", "mode: ratio_managed"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("lost %q:\n%s", want, data)
		}
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions %v", info.Mode().Perm())
	}
}

func TestUpdateFileCreatesAMissingFileOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	if err := UpdateFile(path, func(m map[string]any) error { m["mode"] = "x"; return nil }); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stat: %v %v", info, err)
	}
}

func TestUpdateFileLeavesTheFileAloneWhenTheChangeFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte("mode: a\n"), 0o600)
	err := UpdateFile(path, func(m map[string]any) error { m["mode"] = "b"; return os.ErrInvalid })
	data, _ := os.ReadFile(path)
	if err == nil || string(data) != "mode: a\n" {
		t.Fatalf("err=%v data=%q", err, data)
	}
}
