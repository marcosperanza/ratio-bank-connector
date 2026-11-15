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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source/foldersource"
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

type fakeSource struct {
	result *source.Result
	err    error
}

func (fakeSource) Name() string                                   { return "fake" }
func (f fakeSource) Fetch(source.Request) (*source.Result, error) { return f.result, f.err }

type recordingInbox struct{ items []ratio.InboxItem }

func (r *recordingInbox) PushInbox(_ string, items []ratio.InboxItem) (*ratio.PushResult, error) {
	r.items = append(r.items, items...)
	return &ratio.PushResult{Queued: len(items)}, nil
}

func hybridKey(t *testing.T) string {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwk, err := json.Marshal(map[string]string{
		"kty": "RSA",
		"n":   base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privateKey.PublicKey.E)).Bytes()),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(jwk)
}

func TestCoreDeliversWhatAnyOtherSourceProduces(t *testing.T) {
	t.Parallel()

	inbox := &recordingInbox{}
	cfg := &config.Config{Mode: "ratio_managed", Sync: config.SyncConfig{CSVDir: t.TempDir(), LookbackDays: 30}}
	syncer := New(cfg, "", stubBankClient{}, inbox, nil, nil, zap.NewNop())
	syncer.publicKeyJwk = hybridKey(t)
	syncer.sources[DefaultProvider] = fakeSource{result: &source.Result{Transactions: []source.Transaction{
		{ID: "file:abc", BookingDate: "2026-09-14", Amount: -4.5, Currency: "EUR", Description: "caffè"},
	}}}

	updates := syncer.syncRemoteAccount(ratio.RemoteBankAccount{ID: "a", EBAccountID: "acct", WorkspaceID: "w", PushToInbox: true})

	if len(updates) != 1 || updates[0].Type == "SYNC_ERROR" {
		t.Fatalf("updates: %+v", updates)
	}
	if len(inbox.items) != 1 || inbox.items[0].BankTransactionID != "file:abc" || inbox.items[0].EncryptedBlob == "" {
		t.Fatalf("pushed: %+v", inbox.items)
	}
}

func TestCoreRefusesATransactionASourceGotWrong(t *testing.T) {
	t.Parallel()

	inbox := &recordingInbox{}
	cfg := &config.Config{Mode: "ratio_managed", Sync: config.SyncConfig{CSVDir: t.TempDir(), LookbackDays: 30}}
	syncer := New(cfg, "", stubBankClient{}, inbox, nil, nil, zap.NewNop())
	syncer.publicKeyJwk = hybridKey(t)
	syncer.sources[DefaultProvider] = fakeSource{result: &source.Result{Transactions: []source.Transaction{
		{ID: "ok", BookingDate: "2026-09-14", Amount: 1},
		{ID: "bad", BookingDate: "14/09/2026", Amount: 1},
	}}}

	updates := syncer.syncRemoteAccount(ratio.RemoteBankAccount{ID: "a", EBAccountID: "acct", WorkspaceID: "w", PushToInbox: true})

	if len(updates) != 1 || updates[0].Type != "SYNC_ERROR" {
		t.Fatalf("updates: %+v", updates)
	}
	if len(inbox.items) != 0 {
		t.Fatalf("nothing should be sent when one movement is malformed, got %d", len(inbox.items))
	}
}

func TestExpiredAuthorizationFromAnySourceAsksForANewLink(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Mode: "ratio_managed", Sync: config.SyncConfig{CSVDir: t.TempDir(), LookbackDays: 30}}
	syncer := New(cfg, "", stubBankClient{}, nil, nil, nil, zap.NewNop())
	syncer.sources[DefaultProvider] = fakeSource{err: source.ErrAuthExpired}

	updates := syncer.syncRemoteAccount(ratio.RemoteBankAccount{ID: "a", EBAccountID: "acct"})

	if len(updates) != 1 || updates[0].Type != ratio.SessionExpiredUpdate("a").Type {
		t.Fatalf("updates: %+v", updates)
	}
}

// fakeProvider stands in for any non-Enable-Banking provider. It may need an
// approval (linker) or only a local setting (local), like the real ones.
type fakeProvider struct {
	name       string
	configured bool
}

func (p fakeProvider) Name() string                                 { return p.name }
func (p fakeProvider) Fetch(source.Request) (*source.Result, error) { return &source.Result{}, nil }
func (p fakeProvider) Configured(string) bool                       { return p.configured }

type approvalProvider struct{ fakeProvider }

func (approvalProvider) Begin(r source.LinkRequest) (string, error) {
	return "https://provider.example/approve?state=" + r.AccountID, nil
}
func (approvalProvider) Complete(_ source.LinkRequest, code string) (*source.LinkResult, error) {
	return &source.LinkResult{Ref: "ref-" + code, IBAN: "IT60X0542811101000000123456", Currency: "EUR"}, nil
}

func newTestSyncer() *Syncer {
	return New(&config.Config{Mode: "ratio_managed"}, "", stubBankClient{}, nil, nil, nil, zap.NewNop())
}

func TestAProviderWithoutApprovalIsLinkedAsSoonAsThisComputerKnowsIt(t *testing.T) {
	t.Parallel()
	s := newTestSyncer()
	s.RegisterSource(fakeProvider{name: "folder", configured: true})

	upd, err := s.handlePending(ratio.RemoteBankAccount{ID: "acc-1", Provider: "folder"}, "https://ratio.example")

	if err != nil || upd.Type != "SESSION_CREATED" || upd.EBAccountID != "acc-1" || upd.SessionValidUntil != "" {
		t.Fatalf("upd=%+v err=%v", upd, err)
	}
}

func TestAProviderThisComputerDoesNotKnowIsReportedNotLinked(t *testing.T) {
	t.Parallel()
	s := newTestSyncer()
	s.RegisterSource(fakeProvider{name: "folder", configured: false})

	if _, err := s.handlePending(ratio.RemoteBankAccount{ID: "acc-1", Provider: "folder", DisplayName: "ING"}, ""); err == nil {
		t.Fatal("expected an error telling the user to configure it")
	}
}

func TestAnUnknownProviderIsRefusedClearly(t *testing.T) {
	t.Parallel()
	s := newTestSyncer()

	if _, err := s.handlePending(ratio.RemoteBankAccount{ID: "acc-1", Provider: "nope"}, ""); err == nil {
		t.Fatal("expected an error")
	}
	updates := s.syncRemoteAccount(ratio.RemoteBankAccount{ID: "acc-1", Provider: "nope", EBAccountID: "x"})
	if len(updates) != 1 || updates[0].Type != "SYNC_ERROR" {
		t.Fatalf("updates: %+v", updates)
	}
}

func TestAProviderThatNeedsApprovalFollowsTheSameStepsAsBanks(t *testing.T) {
	t.Parallel()
	s := newTestSyncer()
	s.RegisterSource(approvalProvider{fakeProvider{name: "other-bank"}})

	pending, err := s.handlePending(ratio.RemoteBankAccount{ID: "acc-2", Provider: "other-bank"}, "https://ratio.example")
	if err != nil || pending.Type != "AUTH_URL" || pending.AuthURL != "https://provider.example/approve?state=acc-2" {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	linked, err := s.handleLinking(ratio.RemoteBankAccount{ID: "acc-2", Provider: "other-bank", PendingAuthCode: "c1"})
	if err != nil || linked.Type != "SESSION_CREATED" || linked.EBAccountID != "ref-c1" {
		t.Fatalf("linked=%+v err=%v", linked, err)
	}
}

func TestAccountsWithoutAProviderStillMeanEnableBanking(t *testing.T) {
	t.Parallel()
	if got := providerName(ratio.RemoteBankAccount{}); got != "enablebanking" {
		t.Fatalf("got %q", got)
	}
}

func writeCSV(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func folderSyncer(t *testing.T, dir string, inbox inboxClient) *Syncer {
	t.Helper()
	cfg := &config.Config{Mode: "ratio_managed", Sync: config.SyncConfig{CSVDir: t.TempDir(), LookbackDays: 30}}
	s := New(cfg, "", stubBankClient{}, inbox, nil, nil, zap.NewNop())
	s.publicKeyJwk = hybridKey(t)
	s.RegisterSource(foldersource.New(map[string]foldersource.Account{"acc-1": {
		Dir: dir,
		Profile: foldersource.Profile{
			Delimiter: ";", SkipRows: 1, DateLayout: "02/01/2006", Decimal: ",", Thousands: ".",
			Columns: map[string]int{"date": 0, "description": 1, "amount": 2},
		},
	}}))
	return s
}

func TestAFolderAccountGoesFromPendingToInboxLikeABankAccount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeCSV(t, dir, "m.csv", "data;descrizione;importo\n14/09/2026;Caffè;-1,20\n15/09/2026;Stipendio;1.850,00\n")
	inbox := &recordingInbox{}
	s := folderSyncer(t, dir, inbox)
	acc := ratio.RemoteBankAccount{ID: "acc-1", Provider: "folder", WorkspaceID: "w", PushToInbox: true, LastSyncAt: "2026-09-01T00:00:00Z"}

	linked, err := s.handlePending(acc, "https://ratio.example")
	if err != nil || linked.Type != "SESSION_CREATED" || linked.EBAccountID != "acc-1" {
		t.Fatalf("linked=%+v err=%v", linked, err)
	}

	acc.EBAccountID = linked.EBAccountID
	updates := s.syncRemoteAccount(acc)

	if len(updates) != 1 || updates[0].Type != "SYNC_COMPLETE" || updates[0].TxCount != 2 {
		t.Fatalf("updates: %+v", updates)
	}
	if len(inbox.items) != 2 {
		t.Fatalf("pushed %d items", len(inbox.items))
	}
	for _, item := range inbox.items {
		if item.EncryptedBlob == "" || item.EncryptedSessionKey == "" || !strings.HasPrefix(item.BankTransactionID, "csv:") {
			t.Fatalf("item: %+v", item)
		}
	}
}

func TestReadingTheSameFolderTwiceSendsTheSameIDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeCSV(t, dir, "m.csv", "data;descrizione;importo\n14/09/2026;Caffè;-1,20\n14/09/2026;Caffè;-1,20\n")
	inbox := &recordingInbox{}
	s := folderSyncer(t, dir, inbox)
	acc := ratio.RemoteBankAccount{ID: "acc-1", Provider: "folder", EBAccountID: "acc-1", WorkspaceID: "w", PushToInbox: true, LastSyncAt: "2026-09-01T00:00:00Z"}

	s.syncRemoteAccount(acc)
	s.syncRemoteAccount(acc)

	if len(inbox.items) != 4 {
		t.Fatalf("pushed %d items", len(inbox.items))
	}
	ids := map[string]int{}
	for _, item := range inbox.items {
		ids[item.BankTransactionID]++
	}
	if len(ids) != 2 {
		t.Fatalf("two coffees must be two ids, repeated reads must repeat them: %v", ids)
	}
	for id, n := range ids {
		if n != 2 {
			t.Fatalf("id %q was sent %d times, want 2 (the inbox absorbs the repeat)", id, n)
		}
	}
}

func TestAFolderThatDisappearedIsReportedAsAnErrorNotSilence(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "gone")
	s := folderSyncer(t, dir, &recordingInbox{})

	updates := s.syncRemoteAccount(ratio.RemoteBankAccount{ID: "acc-1", Provider: "folder", EBAccountID: "acc-1", PushToInbox: true})

	if len(updates) != 1 || updates[0].Type != "SYNC_ERROR" {
		t.Fatalf("updates: %+v", updates)
	}
}

func TestWithoutBankCredentialsEnableBankingAccountsAreRefusedNotCrashed(t *testing.T) {
	t.Parallel()
	s := New(&config.Config{Mode: "ratio_managed"}, "", nil, nil, nil, nil, zap.NewNop())
	s.RegisterSource(fakeProvider{name: "simplefin", configured: true})

	if _, err := s.handlePending(ratio.RemoteBankAccount{ID: "acc-1"}, "https://ratio.example"); err == nil {
		t.Fatal("an Enable Banking account cannot be linked without its credentials")
	}
	upd, err := s.handlePending(ratio.RemoteBankAccount{ID: "acc-2", Provider: "simplefin"}, "")
	if err != nil || upd.Type != "SESSION_CREATED" {
		t.Fatalf("a SimpleFIN account should link without them: %+v %v", upd, err)
	}
}

type localMatcher struct {
	fakeProvider
	res *source.LinkResult
	err error
}

func (m localMatcher) Match(source.LinkRequest) (*source.LinkResult, error) { return m.res, m.err }

func TestAConsentGivenOnThisComputerLinksThePendingAccountWithoutAnAddress(t *testing.T) {
	t.Parallel()
	s := newTestSyncer()
	until := time.Now().AddDate(0, 3, 0)
	s.RegisterSource(localMatcher{fakeProvider: fakeProvider{name: "enablebanking-local"}, res: &source.LinkResult{Ref: "uid-1", IBAN: "IT60X0542811101000000123456", Currency: "EUR", ValidUntil: until}})

	upd, err := s.handlePending(ratio.RemoteBankAccount{ID: "acc-1", Provider: "enablebanking-local", DisplayName: "Casa"}, "https://ratio.example")

	if err != nil || upd.Type != "SESSION_CREATED" || upd.EBAccountID != "uid-1" || upd.SessionValidUntil == "" {
		t.Fatalf("upd=%+v err=%v", upd, err)
	}
}

func TestWithoutAConsentOnThisComputerThePendingAccountIsReportedWithTheReason(t *testing.T) {
	t.Parallel()
	s := newTestSyncer()
	s.RegisterSource(localMatcher{fakeProvider: fakeProvider{name: "enablebanking-local"}, err: errors.New("no authorized session")})
	if _, err := s.handlePending(ratio.RemoteBankAccount{ID: "acc-1", Provider: "enablebanking-local"}, ""); err == nil {
		t.Fatal("expected the reason")
	}
}

func TestAnAccountThatErroredIsLinkedByTheNextCycleOnceTheUserAuthorizedHere(t *testing.T) {
	t.Parallel()
	s := newTestSyncer()
	var sent []ratio.StatusUpdate
	s.reportHook = func(u []ratio.StatusUpdate) error { sent = append(sent, u...); return nil }
	s.cfg.RatioManaged = config.RatioManagedConfig{URL: "https://ratio.example", APIKey: "k"}
	s.RegisterSource(localMatcher{fakeProvider: fakeProvider{name: "enablebanking-local"}, res: &source.LinkResult{Ref: "uid-1"}})
	pr := &ratio.PollResponse{BankAccounts: []ratio.RemoteBankAccount{{ID: "acc-1", Provider: "enablebanking-local", Status: ratio.StatusError}}}

	if err := s.pollCycle(pr); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].EBAccountID != "uid-1" || pr.BankAccounts[0].Status != ratio.StatusLinked {
		t.Fatalf("sent=%+v acc=%+v", sent, pr.BankAccounts[0])
	}
}
