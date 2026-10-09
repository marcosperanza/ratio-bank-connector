// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ratio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AuthRequest is the POST /api/connector/auth body.
type AuthRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Label    string `json:"label,omitempty"`
}

// AuthResponse from /api/connector/auth.
type AuthResponse struct {
	ConnectorToken     string      `json:"connectorToken"`
	WrappedMasterKey   string      `json:"wrappedMasterKey"`
	KeySalt            string      `json:"keySalt"`
	EncryptedSearchKey string      `json:"encryptedSearchKey"`
	Workspaces         []Workspace `json:"workspaces"`
}

// Workspace is a Ratio workspace.
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Role string `json:"role"`
}

// Authenticate calls /api/connector/auth and returns the full response.
func Authenticate(ratioURL, email, password, label string) (*AuthResponse, error) {
	body, _ := json.Marshal(AuthRequest{Email: email, Password: password, Label: label})
	req, err := http.NewRequest(
		http.MethodPost,
		strings.TrimRight(ratioURL, "/")+"/api/connector/auth",
		strings.NewReader(string(body)),
	)
	if err != nil {
		return nil, fmt.Errorf("create Ratio request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := pollHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to Ratio: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("auth failed: HTTP %d", resp.StatusCode)
	}

	var ar AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return nil, err
	}
	return &ar, nil
}

// PushResult is the response from /api/connector/inbox.
type PushResult struct {
	Queued     int `json:"queued"`
	Duplicates int `json:"duplicates"`
}

// InboxItem is one encrypted transaction sent to Ratio inbox.
type InboxItem struct {
	EncryptedBlob       string    `json:"encryptedBlob"`
	EncryptionIV        string    `json:"encryptionIv"`
	EncryptedSessionKey string    `json:"encryptedSessionKey,omitempty"`
	MerchantHash        string    `json:"merchantHash,omitempty"`
	DescriptionHash     string    `json:"descriptionHash,omitempty"`
	BankTransactionID   string    `json:"bankTransactionId"`
	BookingDate         time.Time `json:"bookingDate"`
}
