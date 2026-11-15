// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func spec() Spec {
	return Spec{BinaryPath: "/opt/Ratio Connector/bank-connector", ConfigPath: "/home/u/.config/Ratio/bank-connector/config.yaml", LogDir: "/logs"}
}

func TestRenderedFilesCarryTheCommand(t *testing.T) {
	if p := LaunchAgentPlist(spec()); !strings.Contains(p, "<string>--daemon</string>") || !strings.Contains(p, "/opt/Ratio Connector/bank-connector") {
		t.Fatal(p)
	}
	if u := SystemdUnit(spec()); !strings.Contains(u, `ExecStart="/opt/Ratio Connector/bank-connector" sync --daemon --config`) {
		t.Fatal(u)
	}
	if c := WindowsTaskCommand(spec()); !strings.HasPrefix(c, `"/opt/Ratio Connector/bank-connector" sync --daemon`) {
		t.Fatal(c)
	}
}

func TestPlistEscapesXML(t *testing.T) {
	s := spec()
	s.ConfigPath = "/tmp/a&b/config.yaml"
	if p := LaunchAgentPlist(s); !strings.Contains(p, "a&amp;b") {
		t.Fatal(p)
	}
}

type call struct {
	name string
	args []string
}

func recorder(calls *[]call) Runner {
	return func(name string, args ...string) (string, error) {
		*calls = append(*calls, call{name, args})
		return "", nil
	}
}

func TestInstallOnLinuxWritesUnitAndEnablesIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cfg := filepath.Join(home, "config.yaml")
	_ = os.WriteFile(cfg, []byte("mode: ratio_managed\n"), 0o600)
	src := filepath.Join(home, "src-bin")
	_ = os.WriteFile(src, []byte("binary"), 0o755)

	var calls []call
	m := &Manager{GOOS: "linux", Home: home, UID: 1000, Run: recorder(&calls)}
	s := Spec{Source: src, BinaryPath: filepath.Join(home, ".local", "bin", "bank-connector"), ConfigPath: cfg}
	if err := m.Install(s); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(s.BinaryPath); string(data) != "binary" {
		t.Fatal("binary not installed")
	}
	if _, err := os.Stat(m.unitPath()); err != nil {
		t.Fatal("unit not written")
	}
	var enabled bool
	for _, c := range calls {
		if c.name == "systemctl" && strings.Join(c.args, " ") == "--user enable --now "+LinuxUnit {
			enabled = true
		}
	}
	if !enabled {
		t.Fatalf("service not enabled: %+v", calls)
	}
}

func TestInstallRefusesWithoutConfiguration(t *testing.T) {
	m := &Manager{GOOS: "linux", Home: t.TempDir(), Run: func(string, ...string) (string, error) { return "", nil }}
	if err := m.Install(Spec{ConfigPath: filepath.Join(t.TempDir(), "missing.yaml")}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestUninstallRemovesTheUnitButNotTheConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	var calls []call
	m := &Manager{GOOS: "linux", Home: home, Run: recorder(&calls)}
	_ = os.MkdirAll(filepath.Dir(m.unitPath()), 0o755)
	_ = os.WriteFile(m.unitPath(), []byte("x"), 0o644)
	cfg := filepath.Join(home, "config.yaml")
	_ = os.WriteFile(cfg, []byte("x"), 0o600)
	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.unitPath()); err == nil {
		t.Fatal("unit still there")
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Fatal("config must survive uninstall")
	}
}
