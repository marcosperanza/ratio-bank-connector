// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package prices

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const userAgent = "Mozilla/5.0 (compatible; ratio-bank-connector)"

func get(ctx context.Context, hc *http.Client, u string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return body, resp.StatusCode, err
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// Yahoo uses the public chart endpoint; it needs no key but is unofficial
// and can rate-limit, so it is a convenient default, not a guarantee.
type Yahoo struct {
	BaseURL   string
	HTTP      *http.Client
	Overrides Overrides
	Learned   func(ticker string) string
}

func (y *Yahoo) Name() string { return "yahoo" }

func (y *Yahoo) Candidates(t Ticker) []Candidate {
	return symbolCandidates("yahoo", t, y.Overrides, learnedFor(y.Learned, t))
}

func (y *Yahoo) Fetch(ctx context.Context, c Candidate, since time.Time) ([]Bar, error) {
	from := int64(0)
	if !since.IsZero() {
		from = since.AddDate(0, 0, -7).Unix()
	}
	u := fmt.Sprintf("%s/v8/finance/chart/%s?interval=1d&period1=%d&period2=%d",
		y.BaseURL, url.PathEscape(c.Symbol), from, time.Now().Add(24*time.Hour).Unix())
	body, status, err := get(ctx, y.HTTP, u)
	if err != nil {
		return nil, err
	}
	switch {
	case status == http.StatusNotFound:
		return nil, ErrNotFound
	case status == http.StatusTooManyRequests:
		return nil, ErrQuota
	case status != http.StatusOK:
		return nil, fmt.Errorf("yahoo: HTTP %d", status)
	}
	var r struct {
		Chart struct {
			Result []struct {
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Open   []*float64 `json:"open"`
						High   []*float64 `json:"high"`
						Low    []*float64 `json:"low"`
						Close  []*float64 `json:"close"`
						Volume []*float64 `json:"volume"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("yahoo: %w", err)
	}
	if len(r.Chart.Result) == 0 || len(r.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, ErrNotFound
	}
	res := r.Chart.Result[0]
	q := res.Indicators.Quote[0]
	at := func(s []*float64, i int) string {
		if i < len(s) && s[i] != nil {
			return num(*s[i])
		}
		return ""
	}
	var bars []Bar
	for i, ts := range res.Timestamp {
		if at(q.Close, i) == "" {
			continue // Yahoo emits null rows on holidays
		}
		bars = append(bars, Bar{
			Date: time.Unix(ts, 0).UTC().Format("2006-01-02"),
			Open: at(q.Open, i), High: at(q.High, i), Low: at(q.Low, i),
			Close: at(q.Close, i), Volume: at(q.Volume, i),
		})
	}
	if len(bars) == 0 {
		return nil, ErrNotFound
	}
	return bars, nil
}

// TwelveData needs a key and takes the exchange as a separate field.
type TwelveData struct {
	BaseURL   string
	APIKey    string
	HTTP      *http.Client
	Overrides Overrides
	Learned   func(ticker string) string
}

func (d *TwelveData) Name() string { return "twelvedata" }

func (d *TwelveData) Candidates(t Ticker) []Candidate {
	return exchangeCandidates(t, d.Overrides, learnedFor(d.Learned, t))
}

func (d *TwelveData) Fetch(ctx context.Context, c Candidate, since time.Time) ([]Bar, error) {
	size := 5000
	if !since.IsZero() {
		size = int(time.Since(since).Hours()/24) + 10
	}
	q := url.Values{"symbol": {c.Symbol}, "interval": {"1day"}, "outputsize": {strconv.Itoa(size)}, "apikey": {d.APIKey}}
	if c.Exchange != "" {
		q.Set("exchange", c.Exchange)
	}
	body, _, err := get(ctx, d.HTTP, d.BaseURL+"/time_series?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var r struct {
		Status  string `json:"status"`
		Code    int    `json:"code"`
		Message string `json:"message"`
		Values  []struct {
			Datetime string `json:"datetime"`
			Open     string `json:"open"`
			High     string `json:"high"`
			Low      string `json:"low"`
			Close    string `json:"close"`
			Volume   string `json:"volume"`
		} `json:"values"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("twelvedata: %w", err)
	}
	if r.Status == "error" {
		switch r.Code {
		case 401, 403:
			return nil, ErrAuth
		case 429:
			return nil, ErrQuota
		case 400, 404:
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("twelvedata: %d %s", r.Code, r.Message)
	}
	var bars []Bar
	for _, v := range r.Values {
		bars = append(bars, Bar{Date: v.Datetime[:min(10, len(v.Datetime))], Open: v.Open, High: v.High, Low: v.Low, Close: v.Close, Volume: v.Volume})
	}
	if len(bars) == 0 {
		return nil, ErrNotFound
	}
	return bars, nil
}

// AlphaVantage has a very small free quota (25 calls/day).
type AlphaVantage struct {
	BaseURL   string
	APIKey    string
	HTTP      *http.Client
	Overrides Overrides
	Learned   func(ticker string) string
}

func (a *AlphaVantage) Name() string { return "alphavantage" }

func (a *AlphaVantage) Candidates(t Ticker) []Candidate {
	return symbolCandidates("alphavantage", t, a.Overrides, learnedFor(a.Learned, t))
}

func (a *AlphaVantage) Fetch(ctx context.Context, c Candidate, since time.Time) ([]Bar, error) {
	size := "full"
	if !since.IsZero() && time.Since(since) < 90*24*time.Hour {
		size = "compact"
	}
	q := url.Values{"function": {"TIME_SERIES_DAILY"}, "symbol": {c.Symbol}, "outputsize": {size}, "apikey": {a.APIKey}}
	body, _, err := get(ctx, a.HTTP, a.BaseURL+"/query?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var r struct {
		Series map[string]map[string]string `json:"Time Series (Daily)"`
		Note   string                       `json:"Note"`
		Info   string                       `json:"Information"`
		Err    string                       `json:"Error Message"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("alphavantage: %w", err)
	}
	switch {
	case r.Err != "":
		return nil, ErrNotFound
	case r.Note != "" || r.Info != "":
		if strings.Contains(strings.ToLower(r.Info), "api key") {
			return nil, ErrAuth
		}
		return nil, ErrQuota
	}
	dates := make([]string, 0, len(r.Series))
	for d := range r.Series {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	var bars []Bar
	for _, d := range dates {
		v := r.Series[d]
		bars = append(bars, Bar{Date: d, Open: v["1. open"], High: v["2. high"], Low: v["3. low"], Close: v["4. close"], Volume: v["5. volume"]})
	}
	if len(bars) == 0 {
		return nil, ErrNotFound
	}
	return bars, nil
}

func learnedFor(f func(string) string, t Ticker) string {
	if f == nil {
		return ""
	}
	return f(t.Raw)
}
