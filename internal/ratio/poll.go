// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ratio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var pollHTTPClient = &http.Client{Timeout: 30 * time.Second}

// BankAccountStatus mirrors ConnectorBankStatus enum from Ratio.
type BankAccountStatus string

const (
	StatusPending BankAccountStatus = "PENDING"
	StatusLinking BankAccountStatus = "LINKING"
	StatusLinked  BankAccountStatus = "LINKED"
	StatusExpired BankAccountStatus = "EXPIRED"
	StatusError   BankAccountStatus = "ERROR"
)

// RemoteBankAccount is one account slot from GET /api/connector/poll.
type RemoteBankAccount struct {
	ID                string            `json:"id"`
	EBAccountID       string            `json:"ebAccountId"`
	WorkspaceID       string            `json:"workspaceId"`
	BankName          string            `json:"bankName"`
	Country           string            `json:"country"`
	DisplayName       string            `json:"displayName"`
	Status            BankAccountStatus `json:"status"`
	PendingAuthCode   string            `json:"pendingAuthCode"`
	LastSyncAt        string            `json:"lastSyncAt"`
	LastSyncAttemptAt string            `json:"lastSyncAttemptAt"`
	SessionValidUntil string            `json:"sessionValidUntil"`
	PushToInbox       bool              `json:"pushToInbox"`
}

// PollResponse is the response from GET /api/connector/poll.
type PollResponse struct {
	APIKeyID     string `json:"apiKeyId"`
	PublicKeyJwk string `json:"publicKeyJwk"`
	// PresenceChannel is the Ably channel where the connector announces itself
	// online. It belongs to the API key's owner, not to a workspace. Empty
	// when Ratio does not publish one.
	PresenceChannel string              `json:"presenceChannel"`
	BankAccounts    []RemoteBankAccount `json:"bankAccounts"`
}

// StatusUpdate is one update in POST /api/connector/poll/report.
type StatusUpdate struct {
	BankAccountID     string `json:"bankAccountId"`
	Type              string `json:"type"`
	AuthURL           string `json:"authUrl,omitempty"`
	EBAccountID       string `json:"ebAccountId,omitempty"`
	IBAN              string `json:"iban,omitempty"`
	Currency          string `json:"currency,omitempty"`
	SessionValidUntil string `json:"sessionValidUntil,omitempty"`
	TxCount           int    `json:"txCount,omitempty"`
	LastSyncAt        string `json:"lastSyncAt,omitempty"`
	Error             string `json:"error,omitempty"`
}

// PollConfig fetches the connector config from Ratio.
func PollConfig(baseURL, apiKey string) (*PollResponse, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/connector/poll", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := pollHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to Ratio: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("poll failed: HTTP %d", resp.StatusCode)
	}

	var pr PollResponse
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// ReportUpdates posts status updates to Ratio.
func ReportUpdates(baseURL, apiKey string, updates []StatusUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	body, _ := json.Marshal(map[string]any{"updates": updates})
	req, err := http.NewRequest(
		http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/api/connector/poll/report",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := pollHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to Ratio: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("report failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

// AuthURLUpdate builds an AUTH_URL status update.
func AuthURLUpdate(bankAccountID, authURL string) StatusUpdate {
	return StatusUpdate{BankAccountID: bankAccountID, Type: "AUTH_URL", AuthURL: authURL}
}

// SessionCreatedUpdate builds a SESSION_CREATED status update.
func SessionCreatedUpdate(bankAccountID, ebAccountID, iban, currency string, validUntil time.Time) StatusUpdate {
	u := StatusUpdate{
		BankAccountID: bankAccountID,
		Type:          "SESSION_CREATED",
		EBAccountID:   ebAccountID,
		IBAN:          iban,
		Currency:      currency,
	}
	if !validUntil.IsZero() {
		u.SessionValidUntil = validUntil.UTC().Format(time.RFC3339)
	}
	return u
}

// SyncCompleteUpdate builds a SYNC_COMPLETE status update.
func SyncCompleteUpdate(bankAccountID string, txCount int) StatusUpdate {
	return StatusUpdate{
		BankAccountID: bankAccountID,
		Type:          "SYNC_COMPLETE",
		TxCount:       txCount,
		LastSyncAt:    time.Now().UTC().Format(time.RFC3339),
	}
}

// SyncStartedUpdate lets Ratio persist the attempt before the provider call.
// This prevents rapid restarts from consuming the user's provider quota.
func SyncStartedUpdate(bankAccountID string) StatusUpdate {
	return StatusUpdate{BankAccountID: bankAccountID, Type: "SYNC_STARTED"}
}

// SyncErrorUpdate builds a SYNC_ERROR status update.
func SyncErrorUpdate(bankAccountID, errMsg string) StatusUpdate {
	return StatusUpdate{BankAccountID: bankAccountID, Type: "SYNC_ERROR", Error: errMsg}
}

// SessionExpiredUpdate builds a SESSION_EXPIRED status update.
func SessionExpiredUpdate(bankAccountID string) StatusUpdate {
	return StatusUpdate{BankAccountID: bankAccountID, Type: "SESSION_EXPIRED"}
}

// SendHeartbeat posts a heartbeat to Ratio so the UI can show connector online status.
func SendHeartbeat(baseURL, apiKey string) error {
	url := strings.TrimRight(baseURL, "/") + "/api/connector/poll/heartbeat"
	req, err := http.NewRequest("POST", url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := pollHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("heartbeat failed: HTTP %d", resp.StatusCode)
	}
	return nil
}
