// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ui

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ebauth"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
)

type fakeEB struct{ authState string }

func (f *fakeEB) ListASPSPs(string) ([]map[string]any, error) {
	return []map[string]any{{"name": "Zeta"}, {"name": "Alfa"}}, nil
}
func (f *fakeEB) InitAuth(bank, country, redirect, state string) (string, error) {
	f.authState = state
	return "https://bank.example/auth?bank=" + bank, nil
}
func (f *fakeEB) CreateSession(code string) (*enablebanking.Session, error) {
	return &enablebanking.Session{SessionID: "sess-" + code, ValidUntil: time.Now().Add(90 * 24 * time.Hour),
		Accounts: []enablebanking.Account{{ID: "uid-1", AccountID: enablebanking.AccountIdentification{IBAN: "IT60X0542811101000000123456"}, Currency: "EUR"}}}, nil
}

func ebServer(t *testing.T, redirect string) (*Server, http.Handler, *fakeEB) {
	t.Helper()
	s, _ := newServer(t)
	fake := &fakeEB{}
	s.NewEB = func(*config.Config) (EBAPI, error) { return fake, nil }
	s.Sessions = &ebauth.Store{Path: filepath.Join(t.TempDir(), "eb.json")}
	v := validView("k")
	v.EnableBanking.Configured, v.EnableBanking.AppID, v.EnableBanking.PrivateKeyPath, v.EnableBanking.RedirectURL = true, "app", "/x.pem", redirect
	if r := save(t, s.Handler(), v); r.Code != 200 {
		t.Fatalf("save: %d %s", r.Code, r.Body)
	}
	return s, s.Handler(), fake
}

func decode(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("%v in %s", err, body)
	}
	return m
}

func TestRedirectAndCertSurviveASaveRoundTrip(t *testing.T) {
	_, h, _ := ebServer(t, "https://localhost:8443/callback")
	st := decode(t, call(h, "GET", "/api/state", "", nil).Body.String())
	eb := st["config"].(map[string]any)["enableBanking"].(map[string]any)
	if eb["redirectUrl"] != "https://localhost:8443/callback" {
		t.Fatalf("%v", eb)
	}
}

func TestBanksAreListedAlphabetically(t *testing.T) {
	_, h, _ := ebServer(t, "https://localhost:8443/callback")
	got := call(h, "GET", "/api/eb/banks?country=IT", "", nil).Body.String()
	if !strings.Contains(got, `"banks":["Alfa","Zeta"]`) {
		t.Fatal(got)
	}
}

func TestPasteFlowStoresTheSessionAndShowsItInState(t *testing.T) {
	_, h, fake := ebServer(t, "https://my-own-app.example/cb")
	f := decode(t, call(h, "POST", "/api/eb/start", `{"bank":"Alfa","country":"it"}`, nil).Body.String())
	if f["mode"] != "paste" || f["status"] != "waiting" {
		t.Fatalf("%v", f)
	}
	id := f["id"].(string)
	if r := call(h, "POST", "/api/eb/flow/"+id+"/finish", `{"pasted":"https://my-own-app.example/cb?code=C1&state=wrong"}`, nil); r.Code != 422 {
		t.Fatalf("a foreign state must be refused: %d %s", r.Code, r.Body)
	}
	done := decode(t, call(h, "POST", "/api/eb/flow/"+id+"/finish", `{"pasted":"https://my-own-app.example/cb?code=C1&state=`+fake.authState+`"}`, nil).Body.String())
	if done["status"] != "done" {
		t.Fatalf("%v", done)
	}
	st := decode(t, call(h, "GET", "/api/state", "", nil).Body.String())
	sess := st["ebSessions"].([]any)
	if len(sess) != 1 || sess[0].(map[string]any)["bank"] != "Alfa" || sess[0].(map[string]any)["expired"] != false {
		t.Fatalf("%v", sess)
	}
}

func TestAutoFlowReceivesTheCallbackItself(t *testing.T) {
	_, h, fake := ebServer(t, "https://127.0.0.1:38444/callback")
	f := decode(t, call(h, "POST", "/api/eb/start", `{"bank":"Alfa","country":"IT"}`, nil).Body.String())
	if f["mode"] != "auto" {
		t.Fatalf("%v", f)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Get("https://127.0.0.1:38444/callback?code=ZZ&state=" + fake.authState)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	id := f["id"].(string)
	for i := 0; i < 50; i++ {
		got := decode(t, call(h, "GET", "/api/eb/flow/"+id, "", nil).Body.String())
		if got["status"] == "done" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("flow never completed")
}

func TestForgetRemovesASession(t *testing.T) {
	s, h, _ := ebServer(t, "https://x.example/cb")
	_ = s.Sessions.Add(ebauth.Session{ID: "s1", Bank: "Alfa", Country: "IT", ValidUntil: time.Now().Add(time.Hour), CreatedAt: time.Now()})
	if r := call(h, "DELETE", "/api/eb/sessions/s1", "", nil); r.Code != 200 {
		t.Fatalf("%d", r.Code)
	}
	if all, _ := s.Sessions.Load(); len(all) != 0 {
		t.Fatalf("%+v", all)
	}
}

func TestExpiringAndExpiredAreFlagged(t *testing.T) {
	now := time.Now()
	v := sessionViews([]ebauth.Session{
		{ID: "a", ValidUntil: now.Add(5 * 24 * time.Hour)},
		{ID: "b", ValidUntil: now.Add(-time.Hour)},
	}, now)
	if v[0].ID != "b" || !v[0].Expired || v[1].DaysLeft != 4 && v[1].DaysLeft != 5 {
		t.Fatalf("%+v", v)
	}
}
