// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package foldersource

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
)

// Profile describes one bank's CSV. Columns are zero-based and named by what
// they hold: date, description, and either amount or debit and credit;
// counterparty, currency and id are optional.
type Profile struct {
	Delimiter  string
	SkipRows   int
	Encoding   string // "utf-8" (default) or "latin1"
	DateLayout string // Go layout, e.g. "02/01/2006"
	Columns    map[string]int
	Decimal    string // default "."
	Thousands  string // default none
	Currency   string // default EUR
}

func (p Profile) col(name string) (int, bool) {
	i, ok := p.Columns[name]
	return i, ok
}

func parseCSV(data []byte, p Profile, account string) ([]source.Transaction, error) {
	if _, ok := p.col("date"); !ok {
		return nil, errors.New("profile has no date column")
	}
	if _, hasAmount := p.col("amount"); !hasAmount {
		_, d := p.col("debit")
		_, c := p.col("credit")
		if !d && !c {
			return nil, errors.New("profile has no amount, debit or credit column")
		}
	}
	if p.DateLayout == "" {
		return nil, errors.New("profile has no date layout")
	}

	text := decode(data, p.Encoding)
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = ','
	if p.Delimiter != "" {
		r.Comma = []rune(p.Delimiter)[0]
	}
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(records) <= p.SkipRows {
		return nil, nil
	}

	currency := p.Currency
	if currency == "" {
		currency = "EUR"
	}
	seen := map[string]int{}
	var out []source.Transaction
	for n, rec := range records[p.SkipRows:] {
		if blank(rec) {
			continue
		}
		line := n + p.SkipRows + 1
		dateStr := field(rec, p.Columns, "date")
		day, err := time.Parse(p.DateLayout, strings.TrimSpace(dateStr))
		if err != nil {
			return nil, fmt.Errorf("line %d: date %q does not match %q", line, dateStr, p.DateLayout)
		}
		amount, err := rowAmount(rec, p)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		desc := strings.TrimSpace(field(rec, p.Columns, "description"))
		cur := strings.TrimSpace(field(rec, p.Columns, "currency"))
		if cur == "" {
			cur = currency
		}
		date := day.Format("2006-01-02")

		tx := source.Transaction{
			BookingDate:  date,
			Amount:       amount,
			Currency:     cur,
			Description:  desc,
			RawText:      strings.Join(rec, p.delimiter()),
			Counterparty: strings.TrimSpace(field(rec, p.Columns, "counterparty")),
		}
		if id := strings.TrimSpace(field(rec, p.Columns, "id")); id != "" {
			tx.ID = "csv:" + account + ":" + id
		} else {
			key := fmt.Sprintf("%s|%.2f|%s|%s", date, amount, cur, strings.ToLower(strings.Join(strings.Fields(desc), " ")))
			tx.ID = source.StableID("csv", account, date, amount, cur, desc, seen[key])
			seen[key]++
		}
		out = append(out, tx)
	}
	return out, nil
}

func (p Profile) delimiter() string {
	if p.Delimiter != "" {
		return p.Delimiter
	}
	return ","
}

func rowAmount(rec []string, p Profile) (float64, error) {
	if i, ok := p.col("amount"); ok {
		return parseAmount(cell(rec, i), p.Decimal, p.Thousands)
	}
	var debit, credit float64
	var err error
	if i, ok := p.col("debit"); ok && strings.TrimSpace(cell(rec, i)) != "" {
		if debit, err = parseAmount(cell(rec, i), p.Decimal, p.Thousands); err != nil {
			return 0, err
		}
	}
	if i, ok := p.col("credit"); ok && strings.TrimSpace(cell(rec, i)) != "" {
		if credit, err = parseAmount(cell(rec, i), p.Decimal, p.Thousands); err != nil {
			return 0, err
		}
	}
	// Debits are listed as positive numbers in their own column.
	if debit < 0 {
		debit = -debit
	}
	return credit - debit, nil
}

// parseAmount reads "1.234,56", "-12,30", "(12.00)", "12,00-" and the like.
func parseAmount(raw, decimal, thousands string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, errors.New("empty amount")
	}
	negative := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		negative, s = true, strings.Trim(s, "()")
	}
	if strings.HasSuffix(s, "-") {
		negative, s = true, strings.TrimSuffix(s, "-")
	}
	if strings.HasPrefix(s, "-") {
		negative, s = !negative, strings.TrimPrefix(s, "-")
	}
	s = strings.TrimPrefix(s, "+")
	for _, symbol := range []string{"€", "$", "£", "EUR", "USD", " ", " "} {
		s = strings.ReplaceAll(s, symbol, "")
	}
	if thousands != "" {
		s = strings.ReplaceAll(s, thousands, "")
	}
	if decimal != "" && decimal != "." {
		s = strings.ReplaceAll(s, decimal, ".")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q is not a number", raw)
	}
	if negative {
		v = -v
	}
	return v, nil
}

func field(rec []string, cols map[string]int, name string) string {
	i, ok := cols[name]
	if !ok {
		return ""
	}
	return cell(rec, i)
}

func cell(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return rec[i]
}

func blank(rec []string) bool {
	for _, c := range rec {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// decode turns the file's bytes into text. Italian banks often export in
// Windows-1252/Latin-1; its bytes map to the first 256 code points.
func decode(data []byte, encoding string) string {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	switch strings.ToLower(encoding) {
	case "latin1", "iso-8859-1", "windows-1252", "cp1252":
		runes := make([]rune, len(data))
		for i, b := range data {
			runes[i] = rune(b)
		}
		return string(runes)
	}
	if !utf8.Valid(data) {
		return decode(data, "latin1")
	}
	return string(data)
}
