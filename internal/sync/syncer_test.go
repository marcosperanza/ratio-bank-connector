// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package sync

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/marcosperanza/ratio-bank-connector/v3/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v3/internal/enablebanking"
	"github.com/marcosperanza/ratio-bank-connector/v3/internal/ratio"
)

type stubBankClient struct {
	txs []enablebanking.Transaction
}

func (s stubBankClient) GetTransactions(string, string) ([]enablebanking.Transaction, []json.RawMessage, error) {
	raw := make([]json.RawMessage, len(s.txs))
	for i, tx := range s.txs {
		raw[i], _ = json.Marshal(tx)
	}
	return s.txs, raw, nil
}

func (stubBankClient) InitAuth(string, string, string, string) (string, error) {
	return "", nil
}

func (stubBankClient) CreateSession(string) (*enablebanking.Session, error) {
	return nil, nil
}

type failingInboxClient struct{}

func (failingInboxClient) PushInbox(string, []ratio.InboxItem) (*ratio.PushResult, error) {
	return nil, errors.New("temporary inbox failure")
}

type countingBankClient struct {
	calls int
}

func (s *countingBankClient) GetTransactions(string, string) ([]enablebanking.Transaction, []json.RawMessage, error) {
	s.calls++
	return nil, nil, nil
}

func (*countingBankClient) InitAuth(string, string, string, string) (string, error) {
	return "", nil
}

func (*countingBankClient) CreateSession(string) (*enablebanking.Session, error) {
	return nil, nil
}

type linkingBankClient struct {
	countingBankClient
}

func (*linkingBankClient) CreateSession(string) (*enablebanking.Session, error) {
	return &enablebanking.Session{
		Accounts: []enablebanking.Account{{ID: "provider-account"}},
	}, nil
}

func TestSyncRemoteAccountDoesNotCompleteWhenInboxPushFails(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwkBytes, err := json.Marshal(map[string]string{
		"kty": "RSA",
		"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
	})
	if err != nil {
		t.Fatal(err)
	}

	tx := enablebanking.Transaction{
		EntryReference:       "provider-transaction-1",
		BookingDate:          "2026-09-14",
		TransactionAmount:    enablebanking.TransactionAmount{Amount: "12.34", Currency: "EUR"},
		CreditDebitIndicator: "DBIT",
	}
	cfg := &config.Config{
		Mode: "ratio_managed",
		Sync: config.SyncConfig{CSVDir: t.TempDir(), LookbackDays: 30},
	}
	syncer := New(cfg, "", stubBankClient{txs: []enablebanking.Transaction{tx}}, failingInboxClient{}, nil, nil, zap.NewNop())
	syncer.publicKeyJwk = string(jwkBytes)

	updates := syncer.syncRemoteAccount(ratio.RemoteBankAccount{
		ID:          "ratio-bank-account-1",
		EBAccountID: "provider-account-1",
		WorkspaceID: "workspace-1",
		PushToInbox: true,
	})

	if len(updates) != 1 {
		t.Fatalf("expected one update, got %d", len(updates))
	}
	if updates[0].Type != "SYNC_ERROR" {
		t.Fatalf("expected SYNC_ERROR, got %q", updates[0].Type)
	}
	if updates[0].LastSyncAt != "" {
		t.Fatalf("failed sync must not advance lastSyncAt, got %q", updates[0].LastSyncAt)
	}
}

func TestMaskIBAN(t *testing.T) {
	t.Parallel()
	if got := maskIBAN("IT60X0542811101000000123456"); got != "****3456" {
		t.Fatalf("unexpected masked IBAN: %q", got)
	}
	if got := maskIBAN("123"); got != "****" {
		t.Fatalf("short identifiers must be fully masked: %q", got)
	}
}

func TestSyncCycleSkipsProviderCallUntilAccountIsDue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/connector/poll" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bankAccounts":[{"id":"ratio-account","ebAccountId":"provider-account","workspaceId":"workspace","status":"LINKED","lastSyncAt":"2026-09-13T11:00:00Z","lastSyncAttemptAt":"2026-09-14T11:00:00Z","pushToInbox":true}]}`))
	}))
	defer server.Close()

	bank := &countingBankClient{}
	cfg := &config.Config{
		Mode:         "ratio_managed",
		Sync:         config.SyncConfig{IntervalHours: 6},
		RatioManaged: config.RatioManagedConfig{URL: server.URL, APIKey: "test-key"},
	}
	syncer := New(cfg, "", bank, nil, nil, nil, zap.NewNop())

	if err := syncer.syncCycle(false, now); err != nil {
		t.Fatal(err)
	}
	if bank.calls != 0 {
		t.Fatalf("expected no provider request before next due time, got %d", bank.calls)
	}
}

func TestManagedCycleFetchesRemoteConfigOnce(t *testing.T) {
	t.Parallel()
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/connector/poll" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		polls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiKeyId":"key-id","bankAccounts":[]}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		Mode:         "ratio_managed",
		Sync:         config.SyncConfig{IntervalHours: 6},
		RatioManaged: config.RatioManagedConfig{URL: server.URL, APIKey: "test-key"},
	}
	syncer := New(cfg, "", &countingBankClient{}, nil, nil, nil, zap.NewNop())

	response, err := syncer.ManagedCycle()
	if err != nil {
		t.Fatal(err)
	}
	if response.APIKeyID != "key-id" {
		t.Fatalf("unexpected API key id: %q", response.APIKeyID)
	}
	if polls != 1 {
		t.Fatalf("expected one remote config request, got %d", polls)
	}
}

func TestManagedCycleSyncsImmediatelyAfterCompletingLink(t *testing.T) {
	t.Parallel()
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/connector/poll":
			polls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"apiKeyId":"key-id","bankAccounts":[{"id":"ratio-account","workspaceId":"workspace","status":"LINKING","pendingAuthCode":"code","pushToInbox":false}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/connector/poll/report":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	bank := &linkingBankClient{}
	cfg := &config.Config{
		Mode:         "ratio_managed",
		Sync:         config.SyncConfig{IntervalHours: 6, CSVDir: t.TempDir()},
		RatioManaged: config.RatioManagedConfig{URL: server.URL, APIKey: "test-key"},
	}
	syncer := New(cfg, "", bank, nil, nil, nil, zap.NewNop())

	if _, err := syncer.ManagedCycle(); err != nil {
		t.Fatal(err)
	}
	if polls != 1 {
		t.Fatalf("expected one remote config request, got %d", polls)
	}
	if bank.calls != 1 {
		t.Fatalf("expected immediate provider sync after linking, got %d calls", bank.calls)
	}
}

func TestAccountSyncDue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	if due, _ := accountSyncDue("", "", 6, now); !due {
		t.Fatal("account without a successful sync must be due")
	}
	if due, next := accountSyncDue("2026-09-14T11:00:00Z", "", 6, now); due || !next.Equal(time.Date(2026, 9, 14, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("recent account should wait until 17:00, due=%v next=%s", due, next)
	}
	if due, _ := accountSyncDue("2026-09-14T06:00:00Z", "", 6, now); !due {
		t.Fatal("account is due when the interval boundary is reached")
	}
	if due, _ := accountSyncDue("invalid", "", 6, now); !due {
		t.Fatal("invalid remote timestamp must recover by syncing")
	}
	if due, next := accountSyncDue("2026-09-13T06:00:00Z", "2026-09-14T10:00:00Z", 6, now); due || !next.Equal(time.Date(2026, 9, 14, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("recent failed attempt must throttle restarts, due=%v next=%s", due, next)
	}
}
