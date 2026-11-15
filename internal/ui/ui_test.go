// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
)

func newServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	s := &Server{ConfigPath: filepath.Join(dir, "config.yaml"), StatusPath: filepath.Join(dir, "status.json"), Version: "test", Token: "tok", Docker: true,
		SealingKeyPath: filepath.Join(dir, "sealing-key.pem")}
	return s, s.Handler()
}

func call(h http.Handler, method, path, body string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:8123"
	req.Header.Set("X-Connector-Token", "tok")
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGuardRefusesWrongTokenHostAndOrigin(t *testing.T) {
	_, h := newServer(t)
	if r := call(h, "GET", "/api/state", "", func(r *http.Request) { r.Header.Set("X-Connector-Token", "nope") }); r.Code != 401 {
		t.Fatalf("token: %d", r.Code)
	}
	if r := call(h, "GET", "/api/state", "", func(r *http.Request) { r.Host = "evil.example:8123" }); r.Code != 403 {
		t.Fatalf("host: %d", r.Code)
	}
	if r := call(h, "PUT", "/api/config", "{}", func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }); r.Code != 403 {
		t.Fatalf("origin: %d", r.Code)
	}
	if r := call(h, "GET", "/", "", func(r *http.Request) { r.Header.Del("X-Connector-Token") }); r.Code != 200 {
		t.Fatalf("page must load without a token (it reads it from the link): %d", r.Code)
	}
}

func save(t *testing.T, h http.Handler, v View) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(v)
	return call(h, "PUT", "/api/config", string(body), nil)
}

func validView(key string) View {
	var v View
	v.Ratio.URL = "https://www.ratio-fin.com"
	v.Ratio.APIKey = &key
	v.Sync.IntervalHours, v.Sync.LookbackDays = 6, 30
	v.Prices.Enabled = true
	k := "td-secret"
	v.Prices.Providers = []ProviderView{{Name: "yahoo"}, {Name: "twelvedata", APIKey: &k, DailyLimit: 780, GapSeconds: 8}}
	return v
}

func TestStateNeverReturnsStoredSecrets(t *testing.T) {
	_, h := newServer(t)
	if r := save(t, h, validView("ratio-secret")); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	body := call(h, "GET", "/api/state", "", nil).Body.String()
	for _, secret := range []string{"ratio-secret", "td-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("state leaks %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, `"apiKeySet":true`) {
		t.Fatalf("state should say a key is set: %s", body)
	}
}

func TestSaveKeepsSecretsWhenFieldsAreEmpty(t *testing.T) {
	s, h := newServer(t)
	save(t, h, validView("ratio-secret"))
	v := validView("")
	v.Ratio.APIKey = nil
	v.Prices.Providers[1].APIKey = nil
	if r := save(t, h, v); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	cfg, err := config.Load(s.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RatioManaged.APIKey != "ratio-secret" || cfg.Prices.Providers[1].APIKey != "td-secret" {
		t.Fatalf("secrets lost: %+v", cfg)
	}
}

func TestSaveRefusesBlockingProblemsAndWritesNothing(t *testing.T) {
	s, h := newServer(t)
	v := validView("k")
	v.Ratio.URL = "http://not-https.example"
	r := save(t, h, v)
	if r.Code != 422 {
		t.Fatalf("%d", r.Code)
	}
	if _, err := config.Load(s.ConfigPath); err == nil {
		t.Fatal("a refused save must not create the file")
	}
}

func TestSeveralSourcesSurviveARoundTrip(t *testing.T) {
	s, h := newServer(t)
	v := validView("k")
	v.EnableBanking.Configured, v.EnableBanking.AppID, v.EnableBanking.PrivateKeyPath = true, "app", "/k.pem"
	if r := save(t, h, v); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	cfg, _ := config.Load(s.ConfigPath)
	if !cfg.EnableBankingConfigured() || cfg.Mode != "ratio_managed" {
		t.Fatalf("%+v", cfg)
	}
}

func TestViewListsAreNeverNull(t *testing.T) {
	raw, _ := json.Marshal(ViewOf(&config.Config{}))
	for _, field := range []string{`"providers":[]`, `"overrides":[]`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("%s missing in %s", field, raw)
		}
	}
}

// Folders and SimpleFIN are set in the file, not on the page: saving the page must leave them as they are.
func TestSavingThePageKeepsTheFoldersAndSimpleFINOfTheFile(t *testing.T) {
	s, h := newServer(t)
	seed := "folders:\n  - account: acc-2\n    path: /data/casa\nsimplefin:\n  - account: acc-1\n    access_url_env: SFIN_URL\nfuture_section:\n  keep: me\n"
	if err := os.WriteFile(s.ConfigPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := save(t, h, validView("k")); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	cfg, err := config.Load(s.ConfigPath)
	if err != nil || len(cfg.Folders) != 1 || len(cfg.SimpleFIN) != 1 {
		t.Fatalf("%v %+v", err, cfg)
	}
	raw, _ := os.ReadFile(s.ConfigPath)
	if !strings.Contains(string(raw), "future_section") {
		t.Fatalf("an unknown section was dropped:\n%s", raw)
	}
	var view View
	unmarshal(t, call(h, "GET", "/api/state", "", nil).Body.Bytes(), &struct{ Config *View }{&view})
	if view.Sources.Folders != 1 || view.Sources.SimpleFIN != 1 {
		t.Fatalf("%+v", view.Sources)
	}
}

func TestFingerprintMakesTheKeyOnceAndRegistersItWithRatio(t *testing.T) {
	var registered int
	ratioServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && r.URL.Path == "/api/connector/sealing-key" && r.Header.Get("Authorization") == "Bearer the-key" {
			registered++
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer ratioServer.Close()

	s, h := newServer(t)
	key := "the-key"
	v := validView(key)
	v.Ratio.URL = ratioServer.URL
	if r := save(t, h, v); r.Code != 200 {
		t.Fatalf("save: %d %s", r.Code, r.Body)
	}
	// Reading the state never creates a key.
	var st struct {
		Fingerprint string `json:"pricesFingerprint"`
	}
	unmarshal(t, call(h, "GET", "/api/state", "", nil).Body.Bytes(), &st)
	if st.Fingerprint != "" {
		t.Fatalf("no key yet, got %q", st.Fingerprint)
	}

	var first, second struct {
		OK          bool   `json:"ok"`
		Fingerprint string `json:"fingerprint"`
		Created     bool   `json:"created"`
		Registered  bool   `json:"registered"`
	}
	unmarshal(t, call(h, "POST", "/api/prices/fingerprint", "", nil).Body.Bytes(), &first)
	unmarshal(t, call(h, "POST", "/api/prices/fingerprint", "", nil).Body.Bytes(), &second)
	if !first.OK || !first.Created || !first.Registered || len(strings.Fields(first.Fingerprint)) != 16 {
		t.Fatalf("first: %+v", first)
	}
	if second.Created || second.Fingerprint != first.Fingerprint || registered != 2 {
		t.Fatalf("second: %+v registered=%d", second, registered)
	}
	unmarshal(t, call(h, "GET", "/api/state", "", nil).Body.Bytes(), &st)
	if st.Fingerprint != first.Fingerprint {
		t.Fatalf("state shows %q, want %q", st.Fingerprint, first.Fingerprint)
	}
	_ = s
}

func TestFingerprintExplainsWhenRatioIsNotConfigured(t *testing.T) {
	_, h := newServer(t)
	var out struct {
		OK         bool   `json:"ok"`
		Registered bool   `json:"registered"`
		Note       string `json:"note"`
	}
	unmarshal(t, call(h, "POST", "/api/prices/fingerprint", "", nil).Body.Bytes(), &out)
	if !out.OK || out.Registered || out.Note != "no-ratio-key" {
		t.Fatalf("%+v", out)
	}
}

func TestRefreshPricesRunsOnceAtATimeAndReportsTheCounts(t *testing.T) {
	s, h := newServer(t)
	started, release := make(chan struct{}), make(chan struct{})
	s.RefreshPrices = func(ctx context.Context) (int, int, error) {
		close(started)
		<-release
		return 5, 2, nil
	}
	type reply struct {
		OK      bool `json:"ok"`
		Busy    bool `json:"busy"`
		Tickers int  `json:"tickers"`
		Failed  int  `json:"failed"`
	}
	first := make(chan reply, 1)
	go func() {
		var r reply
		unmarshal(t, call(h, "POST", "/api/prices/refresh", "", nil).Body.Bytes(), &r)
		first <- r
	}()
	<-started
	var second reply
	unmarshal(t, call(h, "POST", "/api/prices/refresh", "", nil).Body.Bytes(), &second)
	if second.OK || !second.Busy {
		t.Fatalf("a second click must be told a download is running: %+v", second)
	}
	close(release)
	if r := <-first; !r.OK || r.Tickers != 5 || r.Failed != 2 {
		t.Fatalf("%+v", r)
	}
}

func TestRefreshPricesNeedsTheGuardToken(t *testing.T) {
	s, h := newServer(t)
	s.RefreshPrices = func(context.Context) (int, int, error) { t.Fatal("must not run"); return 0, 0, nil }
	rec := call(h, "POST", "/api/prices/refresh", "", func(r *http.Request) { r.Header.Del("X-Connector-Token") })
	if rec.Code == 200 {
		t.Fatalf("unauthenticated refresh answered %d", rec.Code)
	}
}

func unmarshal(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
}
