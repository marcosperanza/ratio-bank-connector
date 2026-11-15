// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ebsource

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source/sourcetest"
)

type fakeClient struct {
	txs []enablebanking.Transaction
	err error
}

func (f fakeClient) GetTransactions(string, string) ([]enablebanking.Transaction, []json.RawMessage, error) {
	return f.txs, make([]json.RawMessage, len(f.txs)), f.err
}

func TestNormalizeSignsDebitsAndKeepsTheProviderReference(t *testing.T) {
	got, err := Normalize(enablebanking.Transaction{
		EntryReference:       "ref-1",
		BookingDate:          "2026-09-14",
		TransactionAmount:    enablebanking.TransactionAmount{Amount: "12.34", Currency: "EUR"},
		CreditDebitIndicator: "DBIT",
		RemittanceInfo:       []string{"COOP", "MILANO"},
		Creditor:             &enablebanking.Party{Name: "Coop"},
		CreditorAccount:      &enablebanking.AccountIdentification{IBAN: "IT60X0542811101000000123456"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := source.Transaction{
		ID: "ref-1", BookingDate: "2026-09-14", Amount: -12.34, Currency: "EUR",
		Description: "COOP MILANO", Counterparty: "Coop", CounterpartyIBAN: "IT60X0542811101000000123456",
	}
	if got.ID != want.ID || got.Amount != want.Amount || got.Description != want.Description ||
		got.Counterparty != want.Counterparty || got.CounterpartyIBAN != want.CounterpartyIBAN ||
		got.Currency != want.Currency || got.BookingDate != want.BookingDate {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestNormalizeFallsBackToTransactionIDThenToAContentKey(t *testing.T) {
	base := enablebanking.Transaction{BookingDate: "2026-09-14", TransactionAmount: enablebanking.TransactionAmount{Amount: "1.00", Currency: "EUR"}, RemittanceInfo: []string{"x"}}

	withTxID := base
	withTxID.TransactionID = "tid"
	if got, _ := Normalize(withTxID); got.ID != "tid" {
		t.Fatalf("got %q", got.ID)
	}
	if got, _ := Normalize(base); got.ID != "2026-09-14_1.00_x" {
		t.Fatalf("got %q", got.ID)
	}
}

func TestNormalizeRefusesAnAmountItCannotRead(t *testing.T) {
	if _, err := Normalize(enablebanking.Transaction{BookingDate: "2026-09-14", TransactionAmount: enablebanking.TransactionAmount{Amount: "abc"}}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestFetchReportsExpiredAuthorizationAsTheSharedError(t *testing.T) {
	_, err := New(fakeClient{err: &enablebanking.APIError{StatusCode: http.StatusUnauthorized}}).Fetch(source.Request{})
	if !errors.Is(err, source.ErrAuthExpired) {
		t.Fatalf("got %v", err)
	}
}

func TestFetchPassesOtherErrorsThroughUnchanged(t *testing.T) {
	boom := errors.New("boom")
	_, err := New(fakeClient{err: boom}).Fetch(source.Request{})
	if errors.Is(err, source.ErrAuthExpired) || !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestFetchOffersAnArchiveOfWhatItReturned(t *testing.T) {
	res, err := New(fakeClient{}).Fetch(source.Request{})
	if err != nil || res.Archive == nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestConformance(t *testing.T) {
	client := fakeClient{txs: []enablebanking.Transaction{
		{EntryReference: "r1", BookingDate: "2026-09-14", TransactionAmount: enablebanking.TransactionAmount{Amount: "1.00", Currency: "EUR"}, CreditDebitIndicator: "DBIT"},
		{EntryReference: "r2", BookingDate: "2026-09-15", TransactionAmount: enablebanking.TransactionAmount{Amount: "2.00", Currency: "EUR"}, CreditDebitIndicator: "CRDT"},
	}}
	sourcetest.Run(t, New(client), source.Request{Account: "acct", From: "2026-09-01"})
}

type fakeAuth struct {
	url     string
	session *enablebanking.Session
	err     error
	gotArgs []string
}

func (f *fakeAuth) InitAuth(bank, country, redirect, state string) (string, error) {
	f.gotArgs = []string{bank, country, redirect, state}
	return f.url, f.err
}

func (f *fakeAuth) CreateSession(string) (*enablebanking.Session, error) { return f.session, f.err }

func TestLinkerBeginPassesTheAccountAsState(t *testing.T) {
	auth := &fakeAuth{url: "https://bank.example/auth"}

	url, err := NewLinker(auth).Begin(source.LinkRequest{AccountID: "acc-1", BankName: "ING", Country: "IT", RatioURL: "https://ratio.example/"})

	if err != nil || url != "https://bank.example/auth" {
		t.Fatalf("url=%q err=%v", url, err)
	}
	if got := auth.gotArgs; got[0] != "ING" || got[1] != "IT" || got[2] != "https://ratio.example/api/connector/eb-callback" || got[3] != "acc-1" {
		t.Fatalf("args: %v (the state must be the account id, the callback finds the account by it)", got)
	}
}

func TestLinkerBeginNamesTheBankWhenItFails(t *testing.T) {
	_, err := NewLinker(&fakeAuth{err: errors.New("down")}).Begin(source.LinkRequest{BankName: "ING"})
	if err == nil || !strings.Contains(err.Error(), "ING") {
		t.Fatalf("got %v", err)
	}
}

func TestLinkerCompleteReportsTheFirstAccountAndTheConsentEnd(t *testing.T) {
	until := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	session := &enablebanking.Session{
		ValidUntil: until,
		Accounts:   []enablebanking.Account{{ID: "provider-1", Currency: "EUR", AccountID: enablebanking.AccountIdentification{IBAN: "IT60X0542811101000000123456"}}},
	}

	res, err := NewLinker(&fakeAuth{session: session}).Complete(source.LinkRequest{}, "code")

	if err != nil {
		t.Fatal(err)
	}
	if res.Ref != "provider-1" || res.IBAN != "IT60X0542811101000000123456" || res.Currency != "EUR" || !res.ValidUntil.Equal(until) {
		t.Fatalf("result: %+v", res)
	}
}

func TestLinkerCompleteWithoutAccountsStillReturnsTheConsent(t *testing.T) {
	res, err := NewLinker(&fakeAuth{session: &enablebanking.Session{}}).Complete(source.LinkRequest{}, "code")
	if err != nil || res.Ref != "" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestLinkerCompleteFailureIsWrapped(t *testing.T) {
	boom := errors.New("expired code")
	_, err := NewLinker(&fakeAuth{err: boom}).Complete(source.LinkRequest{}, "code")
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}
