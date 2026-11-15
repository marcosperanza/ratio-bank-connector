// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package service installs the connector as a per-user background service:
// a LaunchAgent on macOS, a systemd user unit on Linux and a Scheduled Task
// on Windows. It replaces the shell scripts so the wizard and the settings UI
// can install, restart and remove it without leaving the program.
package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	MacLabel    = "com.ratio.bank-connector"
	LinuxUnit   = "ratio-bank-connector.service"
	WindowsTask = "Ratio Bank Connector"
)

// Spec says what to run. BinaryPath is where the installed copy will live.
type Spec struct {
	Source     string // running executable to copy from
	BinaryPath string
	ConfigPath string
	LogDir     string
}

type Status struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Detail    string `json:"detail,omitempty"`
}

// Runner executes an external command; replaced in tests.
type Runner func(name string, args ...string) (string, error)

func execRunner(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Manager is one platform's implementation.
type Manager struct {
	GOOS string
	Home string
	UID  int
	Run  Runner
}

func New() *Manager {
	home, _ := os.UserHomeDir()
	return &Manager{GOOS: runtime.GOOS, Home: home, UID: os.Getuid(), Run: execRunner}
}

// InContainer reports whether the process runs in Docker, where the container
// runtime (restart policy) is the service and none of this applies.
func InContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return false
}

// DefaultSpec places the installed binary in the per-user location of the OS.
func (m *Manager) DefaultSpec(configPath string) (Spec, error) {
	self, err := os.Executable()
	if err != nil {
		return Spec{}, err
	}
	spec := Spec{Source: self, ConfigPath: configPath}
	switch m.GOOS {
	case "darwin":
		spec.BinaryPath = filepath.Join(m.Home, "Library", "Application Support", "Ratio", "BankConnector", "bank-connector")
		spec.LogDir = filepath.Join(m.Home, "Library", "Logs", "Ratio")
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(m.Home, "AppData", "Local")
		}
		spec.BinaryPath = filepath.Join(local, "Ratio", "BankConnector", "bank-connector.exe")
		spec.LogDir = filepath.Join(local, "Ratio", "BankConnector", "logs")
	default:
		spec.BinaryPath = filepath.Join(m.Home, ".local", "bin", "bank-connector")
	}
	return spec, nil
}

func (m *Manager) plistPath() string {
	return filepath.Join(m.Home, "Library", "LaunchAgents", MacLabel+".plist")
}

func (m *Manager) unitPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(m.Home, ".config")
	}
	return filepath.Join(base, "systemd", "user", LinuxUnit)
}

// LaunchAgentPlist renders the macOS agent.
func LaunchAgentPlist(s Spec) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>sync</string>
    <string>--daemon</string>
    <string>--config</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>ProcessType</key><string>Background</string>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, MacLabel, xmlEscape(s.BinaryPath), xmlEscape(s.ConfigPath),
		xmlEscape(filepath.Join(s.LogDir, "bank-connector.log")), xmlEscape(filepath.Join(s.LogDir, "bank-connector.error.log")))
}

func xmlEscape(v string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(v)
}

// SystemdUnit renders the Linux user unit. Paths with spaces are quoted.
func SystemdUnit(s Spec) string {
	return fmt.Sprintf(`[Unit]
Description=Ratio Bank Connector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart="%s" sync --daemon --config "%s"
Restart=on-failure
RestartSec=30
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=default.target
`, s.BinaryPath, s.ConfigPath)
}

// WindowsTaskCommand is the /TR value of the Scheduled Task.
func WindowsTaskCommand(s Spec) string {
	return fmt.Sprintf(`"%s" sync --daemon --config "%s"`, s.BinaryPath, s.ConfigPath)
}

func copyBinary(src, dst string) error {
	if sameFile(src, dst) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func sameFile(a, b string) bool {
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}

// Install copies the binary to its permanent place, registers the service and
// starts it. Running it again updates the binary and restarts.
func (m *Manager) Install(s Spec) error {
	if _, err := os.Stat(s.ConfigPath); err != nil {
		return fmt.Errorf("configuration not found at %s: run the setup first", s.ConfigPath)
	}
	if err := copyBinary(s.Source, s.BinaryPath); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}
	switch m.GOOS {
	case "darwin":
		if err := os.MkdirAll(s.LogDir, 0o755); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(m.plistPath()), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(m.plistPath(), []byte(LaunchAgentPlist(s)), 0o644); err != nil {
			return err
		}
		target := fmt.Sprintf("gui/%d", m.UID)
		_, _ = m.Run("launchctl", "bootout", target+"/"+MacLabel)
		if out, err := m.Run("launchctl", "bootstrap", target, m.plistPath()); err != nil {
			return fmt.Errorf("launchctl bootstrap: %s", out)
		}
		_, _ = m.Run("launchctl", "kickstart", "-k", target+"/"+MacLabel)
	case "linux":
		if err := os.MkdirAll(filepath.Dir(m.unitPath()), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(m.unitPath(), []byte(SystemdUnit(s)), 0o644); err != nil {
			return err
		}
		if out, err := m.Run("systemctl", "--user", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %s", out)
		}
		if out, err := m.Run("systemctl", "--user", "enable", "--now", LinuxUnit); err != nil {
			return fmt.Errorf("systemctl enable: %s", out)
		}
		_, _ = m.Run("systemctl", "--user", "restart", LinuxUnit)
	case "windows":
		if out, err := m.Run("schtasks", "/Create", "/TN", WindowsTask, "/SC", "ONLOGON", "/TR", WindowsTaskCommand(s), "/RL", "LIMITED", "/F"); err != nil {
			return fmt.Errorf("schtasks create: %s", out)
		}
		_, _ = m.Run("schtasks", "/Run", "/TN", WindowsTask)
	default:
		return fmt.Errorf("no service support for %s", m.GOOS)
	}
	return nil
}

// Uninstall stops and removes the service. Configuration and data stay.
func (m *Manager) Uninstall() error {
	switch m.GOOS {
	case "darwin":
		_, _ = m.Run("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", m.UID, MacLabel))
		if err := os.Remove(m.plistPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	case "linux":
		_, _ = m.Run("systemctl", "--user", "disable", "--now", LinuxUnit)
		if err := os.Remove(m.unitPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		_, _ = m.Run("systemctl", "--user", "daemon-reload")
	case "windows":
		_, _ = m.Run("schtasks", "/End", "/TN", WindowsTask)
		_, _ = m.Run("schtasks", "/Delete", "/TN", WindowsTask, "/F")
	default:
		return fmt.Errorf("no service support for %s", m.GOOS)
	}
	return nil
}

// Restart applies a changed configuration to the running service.
func (m *Manager) Restart() error {
	var out string
	var err error
	switch m.GOOS {
	case "darwin":
		out, err = m.Run("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", m.UID, MacLabel))
	case "linux":
		out, err = m.Run("systemctl", "--user", "restart", LinuxUnit)
	case "windows":
		_, _ = m.Run("schtasks", "/End", "/TN", WindowsTask)
		out, err = m.Run("schtasks", "/Run", "/TN", WindowsTask)
	default:
		return fmt.Errorf("no service support for %s", m.GOOS)
	}
	if err != nil {
		return fmt.Errorf("restart: %s", out)
	}
	return nil
}

// Status reports whether the service is registered and running.
func (m *Manager) Status() Status {
	switch m.GOOS {
	case "darwin":
		if _, err := os.Stat(m.plistPath()); err != nil {
			return Status{}
		}
		out, err := m.Run("launchctl", "print", fmt.Sprintf("gui/%d/%s", m.UID, MacLabel))
		return Status{Installed: true, Running: err == nil && strings.Contains(out, "state = running")}
	case "linux":
		if _, err := os.Stat(m.unitPath()); err != nil {
			return Status{}
		}
		out, _ := m.Run("systemctl", "--user", "is-active", LinuxUnit)
		return Status{Installed: true, Running: out == "active", Detail: out}
	case "windows":
		out, err := m.Run("schtasks", "/Query", "/TN", WindowsTask, "/FO", "LIST")
		if err != nil {
			return Status{}
		}
		return Status{Installed: true, Running: strings.Contains(out, "Running")}
	}
	return Status{}
}
