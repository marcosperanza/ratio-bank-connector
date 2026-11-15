// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package prices

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseTicker(t *testing.T) {
	cases := []struct{ in, base, market string }{
		{"AAPL", "AAPL", ""},
		{"brk.b", "BRK.B", ""},
		{"VWCE.DE", "VWCE", "de"},
		{"EQQQ.L", "EQQQ", "uk"},
		{"ENI.MIL", "ENI", "it"},
	}
	for _, c := range cases {
		got := ParseTicker(c.in)
		if got.Base != c.base || got.Market != c.market {
			t.Errorf("%s: got %+v", c.in, got)
		}
	}
}

func TestSuffixAdaptationPerProvider(t *testing.T) {
	tk := ParseTicker("ENI.MI")
	y := (&Yahoo{}).Candidates(tk)
	a := (&AlphaVantage{}).Candidates(tk)
	d := (&TwelveData{}).Candidates(tk)
	// The ticker as written comes first everywhere; the provider's own spelling follows.
	if y[0].Symbol != "ENI.MI" || a[0].Symbol != "ENI.MI" || a[1].Symbol != "ENI.MIL" ||
		d[0] != (Candidate{Symbol: "ENI.MI"}) || d[1] != (Candidate{Symbol: "ENI", Exchange: "MIL"}) {
		t.Fatalf("yahoo=%v alpha=%v twelve=%v", y, a, d)
	}
}

func TestOverrideAndLearnedComeFirst(t *testing.T) {
	ov := Overrides{"VWCE.DE": {"yahoo": "VWCE.MI"}}
	y := &Yahoo{Overrides: ov, Learned: func(string) string { return "VWCE.F" }}
	got := y.Candidates(ParseTicker("VWCE.DE"))
	if len(got) != 3 || got[0].Symbol != "VWCE.MI" || got[1].Symbol != "VWCE.F" || got[2].Symbol != "VWCE.DE" {
		t.Fatalf("got %v", got)
	}
}

func TestStoreMergeByDate(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Merge("AAPL", []Bar{{Date: "2026-01-02", Close: "1"}, {Date: "2026-01-01", Close: "2"}}); err != nil {
		t.Fatal(err)
	}
	n, err := s.Merge("AAPL", []Bar{{Date: "2026-01-02", Close: "9"}, {Date: "2026-01-03", Close: "3"}})
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	data, _ := os.ReadFile(filepath.Join(s.Dir, "AAPL.csv"))
	want := "Date,Open,High,Low,Close,Volume\n2026-01-01,,,,2,\n2026-01-02,,,,9,\n2026-01-03,,,,3,\n"
	if string(data) != want {
		t.Fatalf("got %q", data)
	}
	if got := s.LastDate("AAPL").Format("2006-01-02"); got != "2026-01-03" {
		t.Fatalf("last %s", got)
	}
}

const yahooBody = `{"chart":{"result":[{"timestamp":[1767312000,1767398400],"indicators":{"quote":[{"open":[1,null],"high":[2,null],"low":[0.5,null],"close":[1.5,null],"volume":[10,null]}]}}]}}`

func TestYahooParsesAndSkipsNullRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "ENI.MI") {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, yahooBody)
	}))
	defer srv.Close()
	y := &Yahoo{BaseURL: srv.URL, HTTP: srv.Client()}
	bars, err := y.Fetch(context.Background(), Candidate{Symbol: "ENI.MI"}, time.Time{})
	if err != nil || len(bars) != 1 || bars[0].Close != "1.5" || bars[0].Date != "2026-01-02" {
		t.Fatalf("bars=%v err=%v", bars, err)
	}
	if _, err := y.Fetch(context.Background(), Candidate{Symbol: "NOPE"}, time.Time{}); err != ErrNotFound {
		t.Fatalf("err=%v", err)
	}
}

func TestTwelveDataErrorsMapToSentinels(t *testing.T) {
	for code, want := range map[int]error{401: ErrAuth, 429: ErrQuota, 404: ErrNotFound} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"status":"error","code":%d,"message":"x"}`, code)
		}))
		d := &TwelveData{BaseURL: srv.URL, HTTP: srv.Client(), APIKey: "k"}
		_, err := d.Fetch(context.Background(), Candidate{Symbol: "ENI", Exchange: "MIL"}, time.Time{})
		srv.Close()
		if err != want {
			t.Errorf("code %d: got %v want %v", code, err, want)
		}
	}
}

func TestAlphaVantageQuotaAndNotFound(t *testing.T) {
	body := `{"Note":"rate limit"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer srv.Close()
	a := &AlphaVantage{BaseURL: srv.URL, HTTP: srv.Client(), APIKey: "k"}
	if _, err := a.Fetch(context.Background(), Candidate{Symbol: "X"}, time.Time{}); err != ErrQuota {
		t.Fatalf("err=%v", err)
	}
	body = `{"Error Message":"Invalid API call"}`
	if _, err := a.Fetch(context.Background(), Candidate{Symbol: "X"}, time.Time{}); err != ErrNotFound {
		t.Fatalf("err=%v", err)
	}
}

type fake struct {
	name  string
	cands []Candidate
	reply map[string]error // candidate -> error; missing = success
	calls []string
}

func (f *fake) Name() string                  { return f.name }
func (f *fake) Candidates(Ticker) []Candidate { return f.cands }
func (f *fake) Fetch(_ context.Context, c Candidate, _ time.Time) ([]Bar, error) {
	f.calls = append(f.calls, c.String())
	if err := f.reply[c.String()]; err != nil {
		return nil, err
	}
	return []Bar{{Date: "2026-01-02", Close: "1"}}, nil
}

func newEngine(t *testing.T, slots ...Slot) *Engine {
	st, err := LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Engine{Slots: slots, Store: Store{Dir: t.TempDir()}, State: st, Sleep: func(context.Context, time.Duration) error { return nil }}
}

func TestEngineTriesNextSpellingThenLearns(t *testing.T) {
	p := &fake{name: "p", cands: []Candidate{{Symbol: "ENI.MI"}, {Symbol: "ENI.MIL"}}, reply: map[string]error{"ENI.MI": ErrNotFound}}
	e := newEngine(t, Slot{Provider: p})
	out := e.Run(context.Background(), []string{"ENI.MI"})
	if out[0].Err != nil || out[0].Symbol != "ENI.MIL" {
		t.Fatalf("%+v", out[0])
	}
	if e.State.learned("ENI.MI", "p") != "ENI.MIL" {
		t.Fatal("spelling not learned")
	}
}

func TestEngineQuotaFallsBackAndDisablesProvider(t *testing.T) {
	a := &fake{name: "a", cands: []Candidate{{Symbol: "X"}}, reply: map[string]error{"X": ErrQuota}}
	b := &fake{name: "b", cands: []Candidate{{Symbol: "X"}}}
	e := newEngine(t, Slot{Provider: a}, Slot{Provider: b})
	out := e.Run(context.Background(), []string{"X", "Y"})
	if out[0].Provider != "b" || out[1].Provider != "b" {
		t.Fatalf("%+v", out)
	}
	if len(a.calls) != 1 {
		t.Fatalf("provider a called %d times after quota", len(a.calls))
	}
}

func TestEngineDailyLimitPersistsInState(t *testing.T) {
	p := &fake{name: "p", cands: []Candidate{{Symbol: "X"}}}
	e := newEngine(t, Slot{Provider: p, DailyLimit: 1})
	out := e.Run(context.Background(), []string{"X", "Y"})
	if out[0].Err != nil || out[1].Err == nil || len(p.calls) != 1 {
		t.Fatalf("%+v calls=%v", out, p.calls)
	}
}

func TestEngineUnsupportedTickerIsNotAnError(t *testing.T) {
	p := &fake{name: "p"}
	e := newEngine(t, Slot{Provider: p})
	out := e.Run(context.Background(), []string{"X"})
	if out[0].Err != ErrUnsupported {
		t.Fatalf("%+v", out[0])
	}
}

func TestFileSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tickers.txt")
	if err := os.WriteFile(path, []byte("# etf\nvwce.de\n\nAAPL # apple\nAAPL\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := FileSource{Path: path}.Tickers(context.Background())
	if err != nil || strings.Join(got, ",") != "VWCE.DE,AAPL" {
		t.Fatalf("%v %v", got, err)
	}
}
