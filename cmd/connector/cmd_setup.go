// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/crypto"
)

type ratioAuthResponse struct {
	ConnectorToken     string `json:"connectorToken"`
	WrappedMasterKey   string `json:"wrappedMasterKey"`
	KeySalt            string `json:"keySalt"`
	EncryptedSearchKey string `json:"encryptedSearchKey"`
	Workspaces         []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"workspaces"`
}

var setupHTTPClient = &http.Client{Timeout: 30 * time.Second}

func cmdSetup() *cobra.Command {
	return &cobra.Command{
		Use:   "setup-standalone",
		Short: "Legacy standalone wizard (own passphrase, no Ratio add-on) — creates config.yaml",
		Long:  `Guides you through creating config.yaml with Enable Banking credentials and optional Ratio push.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r := bufio.NewReader(os.Stdin)
			ask := func(prompt string) string {
				fmt.Print(prompt)
				s, _ := r.ReadString('\n')
				return strings.TrimSpace(s)
			}
			confirm := func(prompt string) bool {
				return strings.ToLower(ask(prompt)) == "y"
			}

			fmt.Println("=== Bank Connector Setup ===")
			fmt.Println()
			fmt.Println("You need your Enable Banking App ID and PEM private key from enablebanking.com.")
			fmt.Println()

			appID := ask("Enable Banking App ID: ")
			keyPath := ask("Path to PEM private key file: ")
			keyPath, err := normalizeInputPath(keyPath)
			if err != nil {
				return err
			}

			type configOut struct {
				EnableBanking struct {
					AppID          string `yaml:"app_id"`
					PrivateKeyPath string `yaml:"private_key_path,omitempty"`
				} `yaml:"enable_banking"`
				Sync struct {
					IntervalHours int    `yaml:"interval_hours"`
					LookbackDays  int    `yaml:"lookback_days"`
					CSVDir        string `yaml:"csv_dir"`
					ArchiveMode   string `yaml:"archive_mode"`
				} `yaml:"sync"`
				Ratio struct {
					Enabled            bool   `yaml:"enabled"`
					URL                string `yaml:"url,omitempty"`
					ConnectorToken     string `yaml:"connector_token,omitempty"`
					EncryptedMasterKey string `yaml:"encrypted_master_key,omitempty"`
					MasterKeyIV        string `yaml:"master_key_iv,omitempty"`
					EncryptedSearchKey string `yaml:"encrypted_search_key,omitempty"`
				} `yaml:"ratio"`
				Accounts []struct{} `yaml:"accounts"`
			}

			var out configOut
			out.EnableBanking.AppID = appID
			out.EnableBanking.PrivateKeyPath = keyPath
			out.Sync.IntervalHours = 6
			out.Sync.LookbackDays = 30
			out.Sync.CSVDir = config.DefaultDataDir()
			out.Sync.ArchiveMode = config.ArchiveCSVAndRaw

			if confirm("\nEnable Ratio push? Synced transactions will be encrypted and sent to Ratio. [y/N]: ") {
				ratioURL := ask("Ratio app URL (e.g. https://www.ratio-fin.com): ")
				ratioEmail := ask("Ratio email: ")
				ratioPassword := readPassphrasePrompt("Ratio password: ")

				fmt.Println("\nAuthenticating with Ratio...")
				authResp, err := callRatioAuth(ratioURL, ratioEmail, ratioPassword)
				if err != nil {
					return fmt.Errorf("ratio auth failed: %w", err)
				}
				fmt.Printf("✓ Authenticated. %d workspace(s) found.\n\n", len(authResp.Workspaces))

				fmt.Println("Workspaces:")
				for i, ws := range authResp.Workspaces {
					fmt.Printf("  [%d] %s (%s)  id: %s\n", i+1, ws.Name, ws.Type, ws.ID)
				}
				fmt.Println()

				// Derive KEK from Ratio password + keySalt → unwrap master key
				kek, err := crypto.DeriveKEK(ratioPassword, authResp.KeySalt)
				if err != nil {
					return fmt.Errorf("derive kek: %w", err)
				}
				masterKey, err := crypto.DecryptWrappedMasterKey(authResp.WrappedMasterKey, kek)
				if err != nil {
					return fmt.Errorf("unwrap master key: %w", err)
				}

				// Ask for local passphrase to re-encrypt masterKey at rest
				localPassphrase := readPassphrasePrompt("Local passphrase to protect keys on this machine (remember it!): ")

				// Generate random nonce; use it as both PBKDF2 salt and GCM IV (matches loadRuntime)
				nonce := make([]byte, 12)
				if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
					return fmt.Errorf("generate nonce: %w", err)
				}
				ivB64 := base64.StdEncoding.EncodeToString(nonce)

				localKEK, err := crypto.DeriveKEK(localPassphrase, ivB64)
				if err != nil {
					return fmt.Errorf("derive local kek: %w", err)
				}
				encMasterKey, err := crypto.EncryptGCMWithNonce(masterKey, localKEK, nonce)
				if err != nil {
					return fmt.Errorf("encrypt master key: %w", err)
				}

				out.Ratio.Enabled = true
				out.Ratio.URL = ratioURL
				out.Ratio.ConnectorToken = authResp.ConnectorToken
				out.Ratio.EncryptedMasterKey = base64.StdEncoding.EncodeToString(encMasterKey)
				out.Ratio.MasterKeyIV = ivB64
				out.Ratio.EncryptedSearchKey = authResp.EncryptedSearchKey

				fmt.Println("\n✓ Keys encrypted with local passphrase.")
				fmt.Printf("  Workspaces: copy the workspace ID above into each account's `ratio_workspace_id`.\n")
			}

			// Write config file
			outPath := cfgPath
			data, err := yaml.Marshal(&out)
			if err != nil {
				return fmt.Errorf("marshal config: %w", err)
			}

			header := []byte("# Bank Connector configuration\n# Run `bank-connector link` to add bank accounts.\n\n")
			if err := os.MkdirAll(filepath.Dir(outPath), 0700); err != nil {
				return fmt.Errorf("create config directory: %w", err)
			}
			if err := os.WriteFile(outPath, append(header, data...), 0600); err != nil {
				return fmt.Errorf("write config: %w", err)
			}

			fmt.Printf("\n✓ Config written to %s\n", outPath)
			fmt.Println("\nNext steps:")
			fmt.Println("  1. Run `bank-connector link` to link your bank accounts")
			fmt.Println("  2. Add the account IDs to config.yaml under `accounts:`")
			fmt.Println("  3. Run `bank-connector sync` to fetch transactions")
			fmt.Println("  4. Or run `bank-connector sync --daemon` to sync every 6h")
			return nil
		},
	}
}

func callRatioAuth(baseURL, email, password string) (*ratioAuthResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
		"label":    "bank-connector-" + time.Now().Format("20060102"),
	})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/connector/auth", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := setupHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var r ratioAuthResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
