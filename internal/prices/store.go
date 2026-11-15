// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package prices

import (
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var csvHeader = []string{"Date", "Open", "High", "Low", "Close", "Volume"}

// Store keeps one CSV per ticker (Date,Open,High,Low,Close,Volume), the same
// shape the folder import reads.
type Store struct{ Dir string }

func (s Store) path(ticker string) string {
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, ticker)
	return filepath.Join(s.Dir, safe+".csv")
}

func (s Store) Read(ticker string) (map[string]Bar, error) {
	f, err := os.Open(s.path(ticker))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Bar{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	out := map[string]Bar{}
	for i, r := range rows {
		if i == 0 || len(r) < 6 {
			continue
		}
		out[r[0]] = Bar{Date: r[0], Open: r[1], High: r[2], Low: r[3], Close: r[4], Volume: r[5]}
	}
	return out, nil
}

// LastDate is the newest stored date, zero when there is no file yet.
func (s Store) LastDate(ticker string) time.Time {
	bars, err := s.Read(ticker)
	if err != nil {
		return time.Time{}
	}
	var last string
	for d := range bars {
		if d > last {
			last = d
		}
	}
	t, _ := time.Parse("2006-01-02", last)
	return t
}

// Merge adds bars by date (new values win) and rewrites the file atomically.
// It returns how many dates were added or changed.
func (s Store) Merge(ticker string, bars []Bar) (int, error) {
	have, err := s.Read(ticker)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, b := range bars {
		if old, ok := have[b.Date]; !ok || old != b {
			changed++
		}
		have[b.Date] = b
	}
	if changed == 0 {
		return 0, nil
	}
	dates := make([]string, 0, len(have))
	for d := range have {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(s.Dir, ".price-*.tmp")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	w := csv.NewWriter(tmp)
	_ = w.Write(csvHeader)
	for _, d := range dates {
		b := have[d]
		_ = w.Write([]string{b.Date, b.Open, b.High, b.Low, b.Close, b.Volume})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	return changed, os.Rename(tmp.Name(), s.path(ticker))
}
