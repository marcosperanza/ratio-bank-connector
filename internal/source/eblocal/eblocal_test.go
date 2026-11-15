// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package eblocal

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ebauth"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
)

type noClient struct{}

func (noClient) GetTransactions(string, string) ([]enablebanking.Transaction, []json.RawMessage, error) {
	return nil, nil, nil
}

func newSource(t *testing.T, sessions ...ebauth.Session) *Source {
	t.Helper()
	store := &ebauth.Store{Path: filepath.Join(t.TempDir(), "sessions.json")}
	for _, s := range sessions {
		if err := store.Add(s); err != nil {
			t.Fatal(err)
		}
	}
	return New(noClient{}, store, zap.NewNop())
}

func session(bank string, until time.Time, accounts ...ebauth.Account) ebauth.Session {
	return ebauth.Session{ID: bank + "-s", Bank: bank, Country: "IT", ValidUntil: until, CreatedAt: time.Now(), Accounts: accounts}
}

func TestItIsAnEnableBankingSourceThatLinksFromThisMachine(t *testing.T) {
	var _ source.Source = (*Source)(nil)
	var _ source.Matcher = (*Source)(nil)
	var _ source.Watcher = (*Source)(nil)
	if got := newSource(t).Name(); got != "enablebanking-local" {
		t.Fatalf("name %q", got)
	}
}

func TestMatchFindsTheAccountOfASessionAuthorizedHere(t *testing.T) {
	until := time.Now().AddDate(0, 3, 0)
	s := newSource(t, session("Beta Bank", until, ebauth.Account{UID: "uid-1", IBAN: "IT60X0542811101000000123456", Currency: "EUR"}))

	res, err := s.Match(source.LinkRequest{AccountName: "IT60 X054 2811 1010 0000 0123 456", BankName: "Other"})
	if err != nil || res.Ref != "uid-1" || res.IBAN == "" || !res.ValidUntil.Equal(until) {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestMatchSaysThereIsNoConsentYetInsteadOfInventingOne(t *testing.T) {
	if _, err := newSource(t).Match(source.LinkRequest{AccountName: "Casa", BankName: "Beta Bank"}); err == nil {
		t.Fatal("an account was linked without any consent")
	}
}

func TestMatchIgnoresAConsentThatEnded(t *testing.T) {
	s := newSource(t, session("Beta Bank", time.Now().Add(-time.Hour), ebauth.Account{UID: "uid-1"}))
	if _, err := s.Match(source.LinkRequest{BankName: "Beta Bank"}); err == nil {
		t.Fatal("an expired consent was used")
	}
}
