// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ebauth

import (
	"context"
	"crypto/tls"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodeFromInputAcceptsUrlQueryAndBareCode(t *testing.T) {
	for _, in := range []string{
		"https://app.example/cb?state=s1&code=abc123",
		"?code=abc123&state=s1",
		"code=abc123",
		"abc123",
	} {
		if got, err := CodeFromInput(in, "s1"); err != nil || got != "abc123" {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if _, err := CodeFromInput("https://x/cb?code=a&state=other", "s1"); err == nil {
		t.Error("a foreign state must be refused")
	}
	if _, err := CodeFromInput("https://x/cb?error=access_denied", "s1"); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("bank refusal not reported: %v", err)
	}
}

func TestStoreRenewalReplacesTheSameBank(t *testing.T) {
	st := &Store{Path: filepath.Join(t.TempDir(), "s.json")}
	now := time.Now()
	_ = st.Add(Session{ID: "old", Bank: "Intesa", Country: "IT", CreatedAt: now.Add(-time.Hour), Accounts: []Account{{UID: "u1"}}})
	_ = st.Add(Session{ID: "other", Bank: "BPER", Country: "IT", CreatedAt: now.Add(-time.Minute)})
	_ = st.Add(Session{ID: "new", Bank: "intesa", Country: "it", CreatedAt: now, Accounts: []Account{{UID: "u2"}}})
	all, err := st.Load()
	if err != nil || len(all) != 2 || all[0].ID != "new" || all[1].ID != "other" {
		t.Fatalf("%+v %v", all, err)
	}
}

func TestMatchPrefersIbanThenSingleAccountBank(t *testing.T) {
	now := time.Now()
	sessions := []Session{
		{ID: "a", Bank: "Intesa", ValidUntil: now.Add(time.Hour), CreatedAt: now, Accounts: []Account{{UID: "u1", IBAN: "IT60 X054 2811 1010 0000 0123 456"}, {UID: "u2", IBAN: "IT00OTHER"}}},
		{ID: "b", Bank: "BPER", ValidUntil: now.Add(time.Hour), CreatedAt: now, Accounts: []Account{{UID: "u3"}}},
		{ID: "c", Bank: "Old", ValidUntil: now.Add(-time.Hour), Accounts: []Account{{UID: "u4"}}},
	}
	if _, a, err := Match(sessions, []string{"Conto", "it60x0542811101000000123457"}, now); err == nil {
		t.Fatalf("a near-miss IBAN must not match, got %+v", a)
	}
	if _, a, err := Match(sessions, []string{"it60 x0542811101000000123456"}, now); err != nil || a.UID != "u1" {
		t.Fatalf("%+v %v", a, err)
	}
	if _, a, err := Match(sessions, []string{"Mia", "bper"}, now); err != nil || a.UID != "u3" {
		t.Fatalf("%+v %v", a, err)
	}
	if _, _, err := Match(sessions, []string{"Old"}, now); err == nil {
		t.Fatal("an expired session must not match")
	}
	if _, _, err := Match(sessions, []string{"intesa"}, now); err == nil || !strings.Contains(err.Error(), "IT00OTHER") {
		t.Fatalf("an ambiguous bank must list the IBANs: %v", err)
	}
}

func TestWaitForCodeOverSelfSignedHTTPS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	redirect := "https://127.0.0.1:38443/callback"
	got := make(chan string, 1)
	go func() {
		c, _ := WaitForCode(ctx, redirect, "", "", "s1")
		got <- c
	}()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	var err error
	for i := 0; i < 50; i++ {
		var resp *http.Response
		if resp, err = client.Get(redirect + "?code=zzz&state=s1"); err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if c := <-got; c != "zzz" {
		t.Fatalf("code %q", c)
	}
}

func TestExplainShowsProviderReasonOnlyLocally(t *testing.T) {
	apiErr := &enablebanking.APIError{Method: "POST", Path: "/auth", StatusCode: 400, Body: `{"message":"Redirect URI not allowed"}`}
	if strings.Contains(apiErr.Error(), "not allowed") {
		t.Fatal("the provider body must stay out of Error(), which reaches logs and Ratio")
	}
	got := Explain(apiErr, "https://enablebanking.com/").Error()
	if !strings.Contains(got, "Redirect URI not allowed") || !strings.Contains(got, "https://enablebanking.com/") {
		t.Fatal(got)
	}
}
