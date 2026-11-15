// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package simplefinsource

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source/sourcetest"
)

const accountsBody = `{
  "errlist": [],
  "connections": [],
  "accounts": [{
    "id": "ACT-1", "name": "Checking", "conn_id": "c", "currency": "USD",
    "balance": "100.00", "balance-date": 1790000000,
    "transactions": [
      {"id": "t1", "posted": 1789300000, "amount": "-12.50", "description": " COFFEE SHOP "},
      {"id": "t2", "posted": 1789400000, "amount": "2500.00", "description": "PAYROLL", "transacted_at": 1789390000},
      {"id": "t3", "posted": 0, "amount": "-3.00", "description": "PENDING", "pending": true}
    ]
  }, {
    "id": "ACT-2", "name": "Other", "conn_id": "c", "currency": "USD", "balance": "0", "balance-date": 1790000000,
    "transactions": [{"id": "x", "posted": 1789300000, "amount": "-1", "description": "NOT MINE"}]
  }]
}`

// server answers /accounts and records what it was asked.
func server(t *testing.T, status int, body string, seen *http.Request) *Source {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = *r.Clone(r.Context())
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	s := New(map[string]Account{"ref": {AccessURL: "https://user:secret@" + strings.TrimPrefix(srv.URL, "https://") + "/simplefin", ID: "ACT-1"}})
	s.client = srv.Client()
	return s
}

func TestFetchNormalizesOnlyTheConfiguredAccount(t *testing.T) {
	var seen http.Request
	s := server(t, 200, accountsBody, &seen)
	res, err := s.Fetch(source.Request{Account: "ref", From: "2026-09-01"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Transactions) != 2 {
		t.Fatalf("want 2 posted movements of ACT-1, got %d", len(res.Transactions))
	}
	first := res.Transactions[0]
	if first.ID != "sfin:ACT-1:t1" || first.Amount != -12.5 || first.Currency != "USD" || first.Description != "COFFEE SHOP" {
		t.Fatalf("unexpected movement: %+v", first)
	}
	if res.Transactions[1].Metadata["transactedAt"] == nil {
		t.Fatal("the transaction date should be kept in metadata")
	}
	for _, tx := range res.Transactions {
		if err := tx.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	user, pass, _ := seen.BasicAuth()
	if user != "user" || pass != "secret" {
		t.Fatalf("credentials must travel as basic auth, got %q/%q", user, pass)
	}
	if seen.URL.Path != "/simplefin/accounts" || seen.URL.Query().Get("account") != "ACT-1" || seen.URL.Query().Get("start-date") == "" {
		t.Fatalf("unexpected request: %s", seen.URL)
	}
	if strings.Contains(seen.URL.String(), "secret") {
		t.Fatal("the password must not appear in the URL")
	}
}

func TestSourceConformance(t *testing.T) {
	sourcetest.Run(t, server(t, 200, accountsBody, nil), source.Request{Account: "ref", From: "2026-09-01"})
}

func TestForbiddenMeansAuthorizationLapsed(t *testing.T) {
	_, err := server(t, 403, "", nil).Fetch(source.Request{Account: "ref", From: "2026-09-01"})
	if !errors.Is(err, source.ErrAuthExpired) {
		t.Fatalf("want ErrAuthExpired, got %v", err)
	}
}

func TestPaymentRequiredIsNotAnExpiry(t *testing.T) {
	_, err := server(t, 402, "", nil).Fetch(source.Request{Account: "ref", From: "2026-09-01"})
	if err == nil || errors.Is(err, source.ErrAuthExpired) {
		t.Fatalf("a payment problem is an error, not an expiry: %v", err)
	}
}

func TestMissingAccountShowsTheServersReason(t *testing.T) {
	body := `{"errlist":[{"code":"con.auth","msg":"Reconnect your bank","account_id":"ACT-1"}],"accounts":[]}`
	_, err := server(t, 200, body, nil).Fetch(source.Request{Account: "ref", From: "2026-09-01"})
	if err == nil || !strings.Contains(err.Error(), "Reconnect your bank") {
		t.Fatalf("want the server's message, got %v", err)
	}
}

func TestInvalidAmountIsRefused(t *testing.T) {
	body := `{"accounts":[{"id":"ACT-1","name":"a","currency":"USD","transactions":[{"id":"t","posted":1789300000,"amount":"abc","description":"x"}]}]}`
	if _, err := server(t, 200, body, nil).Fetch(source.Request{Account: "ref", From: "2026-09-01"}); err == nil {
		t.Fatal("an unreadable amount must not become zero")
	}
}

func TestConfigured(t *testing.T) {
	s := New(map[string]Account{"ok": {AccessURL: "https://u:p@h/x", ID: "a"}, "noid": {AccessURL: "https://u:p@h/x"}})
	if !s.Configured("ok") || s.Configured("noid") || s.Configured("missing") {
		t.Fatal("Configured should need both an access URL and an account id")
	}
}

func TestAccessURLMustBeHTTPSWithCredentials(t *testing.T) {
	for _, bad := range []string{"http://u:p@h/x", "https://h/x", "not a url", ""} {
		if _, _, _, err := splitAccessURL(bad); err == nil {
			t.Fatalf("%q should be refused", bad)
		}
	}
}

func TestClaimExchangesTheTokenOnce(t *testing.T) {
	var method string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		_, _ = w.Write([]byte("https://u:p@bridge.example/simplefin\n"))
	}))
	defer srv.Close()
	token := base64.StdEncoding.EncodeToString([]byte(srv.URL + "/claim/abc"))
	access, err := Claim(srv.Client(), token)
	if err != nil || access != "https://u:p@bridge.example/simplefin" || method != http.MethodPost {
		t.Fatalf("claim: %q %v (method %s)", access, err, method)
	}
}

func TestClaimReportsAUsedToken(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }))
	defer srv.Close()
	_, err := Claim(srv.Client(), base64.StdEncoding.EncodeToString([]byte(srv.URL+"/claim/abc")))
	if err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("want a used-token message, got %v", err)
	}
}

func TestClaimRefusesATokenThatIsNotAnHTTPSAddress(t *testing.T) {
	for _, bad := range []string{"!!!", base64.StdEncoding.EncodeToString([]byte("http://x/claim"))} {
		if _, err := Claim(nil, bad); err == nil {
			t.Fatalf("%q should be refused", bad)
		}
	}
}

func TestListReturnsTheAccountsWithoutMovements(t *testing.T) {
	var seen http.Request
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r.Clone(r.Context())
		_, _ = w.Write([]byte(accountsBody))
	}))
	defer srv.Close()
	got, err := List(srv.Client(), "https://user:secret@"+strings.TrimPrefix(srv.URL, "https://")+"/simplefin")
	if err != nil || len(got) != 2 || got[0].ID != "ACT-1" || got[0].Name != "Checking" || got[0].Currency != "USD" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if seen.URL.Query().Get("balances-only") != "1" {
		t.Fatalf("should ask for balances only: %s", seen.URL)
	}
}

func TestListReportsAnExpiredAccessURL(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }))
	defer srv.Close()
	_, err := List(srv.Client(), "https://u:p@"+strings.TrimPrefix(srv.URL, "https://"))
	if !errors.Is(err, source.ErrAuthExpired) {
		t.Fatalf("got %v", err)
	}
}
