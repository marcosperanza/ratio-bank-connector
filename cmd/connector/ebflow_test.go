// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ebauth"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
)

type fakeBank struct {
	state string
	codes []string
}

func (f *fakeBank) ListASPSPs(string) ([]map[string]any, error) {
	return []map[string]any{{"name": "Beta Bank"}, {"name": "Alfa Bank"}}, nil
}
func (f *fakeBank) InitAuth(bank, _, _, state string) (string, error) {
	f.state = state
	return "https://bank.example/login?b=" + bank, nil
}
func (f *fakeBank) CreateSession(code string) (*enablebanking.Session, error) {
	f.codes = append(f.codes, code)
	s := &enablebanking.Session{SessionID: "s-" + code, Accounts: []enablebanking.Account{{ID: "uid", AccountID: enablebanking.AccountIdentification{IBAN: "IT00X"}, Currency: "EUR"}}}
	s.Access.ValidUntil = time.Now().Add(90 * 24 * time.Hour)
	return s, nil
}

func testLinker(t *testing.T, in *bufio.Reader, out io.Writer, cfg *config.Config) *ebLinker {
	t.Helper()
	fake := &fakeBank{}
	l := newLinker(in, out, cfg)
	l.store = &ebauth.Store{Path: filepath.Join(t.TempDir(), "eb.json")}
	l.api = func(*config.Config) (linkAPI, error) { return fake, nil }
	l.open = func(string) {}
	return l
}

func TestLinkPicksTheBankAndAcceptsAPastedCode(t *testing.T) {
	var out bytes.Buffer
	// "1" is Alfa Bank (the list is sorted); the pasted text is a bare code.
	l := testLinker(t, bufio.NewReader(strings.NewReader("1\nABC123\n")), &out, &config.Config{})
	if err := l.Interactive(context.Background(), "IT", ""); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	all, _ := l.store.Load()
	if len(all) != 1 || all[0].Bank != "Alfa Bank" || all[0].ValidUntil.IsZero() || all[0].Accounts[0].UID != "uid" {
		t.Fatalf("%+v", all)
	}
	for _, want := range []string{"https://enablebanking.com/", "enablebanking-local", "IT00X"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestStateOfFlagsExpiringAndExpired(t *testing.T) {
	now := time.Now()
	if st, _ := StateOf(ebauth.Session{ValidUntil: now.Add(60 * 24 * time.Hour)}, now); st != SessionOK {
		t.Error("60 days left is fine")
	}
	if st, d := StateOf(ebauth.Session{ValidUntil: now.Add(10*24*time.Hour + time.Hour)}, now); st != SessionExpiring || d != 10 {
		t.Errorf("state %v days %d", st, d)
	}
	if st, _ := StateOf(ebauth.Session{ValidUntil: now.Add(-time.Hour)}, now); st != SessionExpired {
		t.Error("past is expired")
	}
	if st, _ := StateOf(ebauth.Session{}, now); st != SessionOK {
		t.Error("no end date is not flagged")
	}
}

func TestSessionsRenewTouchesOnlyConsentsThatNeedIt(t *testing.T) {
	var out bytes.Buffer
	l := testLinker(t, bufio.NewReader(strings.NewReader("NEWCODE\n")), &out, &config.Config{})
	now := l.now()
	_ = l.store.Add(ebauth.Session{ID: "fine", Bank: "Alfa Bank", Country: "IT", ValidUntil: now.Add(90 * 24 * time.Hour), CreatedAt: now})
	_ = l.store.Add(ebauth.Session{ID: "old", Bank: "Beta Bank", Country: "IT", ValidUntil: now.Add(3 * 24 * time.Hour), CreatedAt: now})
	if _, err := runSessions(context.Background(), l, true, ""); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	all, _ := l.store.Load()
	renewed := map[string]string{}
	for _, s := range all {
		renewed[s.Bank] = s.ID
	}
	if renewed["Alfa Bank"] != "fine" || renewed["Beta Bank"] != "s-NEWCODE" {
		t.Fatalf("%v\n%s", renewed, out.String())
	}
}

func TestWizardCanConnectABankDuringSetup(t *testing.T) {
	var out bytes.Buffer
	var l *ebLinker
	w := &wizard{
		in:     bufio.NewReader(strings.NewReader("app-1\n/k.pem\n\ny\nIT\n\n2\nPASTED\nn\n")),
		out:    &out,
		checks: passing(),
	}
	w.linker = func(cfg *config.Config) *ebLinker { l = testLinker(t, w.in, &out, cfg); return l }
	cfg := &config.Config{}
	w.setupEnableBanking(cfg)
	all, _ := l.store.Load()
	if len(all) != 1 || all[0].Bank != "Beta Bank" || cfg.EnableBanking.RedirectURL != DefaultRedirectURL {
		t.Fatalf("%+v %q\n%s", all, cfg.EnableBanking.RedirectURL, out.String())
	}
}
