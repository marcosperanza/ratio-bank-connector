// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package foldersource reads the exports a user keeps in a folder: CSV with a
// profile describing the bank's columns, and OFX/QFX. A cloud drive's desktop
// client turns any shared folder into one, so no cloud API is involved.
//
// The source keeps no state. Each fetch reads the folder again and returns the
// movements from the requested day on; the identifiers are derived from the
// content, so the inbox absorbs whatever was seen before.
package foldersource

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
)

// overlapDays is read before the requested day: a bank's export may list a
// movement a few days after the date it carries.
const overlapDays = 7

// Account says where one Ratio account's exports are and how to read them.
type Account struct {
	Dir string
	// Format is "csv", "ofx", or empty to decide by file extension.
	Format  string
	Profile Profile
}

type Source struct {
	accounts map[string]Account
}

// New takes the accounts by the reference Ratio holds for them.
func New(accounts map[string]Account) *Source { return &Source{accounts: accounts} }

func (*Source) Name() string { return "folder" }

// Configured reports whether the reference has a folder, so a link can be
// refused before it is reported as working.
func (s *Source) Configured(ref string) bool {
	_, ok := s.accounts[ref]
	return ok
}

func (s *Source) Fetch(req source.Request) (*source.Result, error) {
	acct, ok := s.accounts[req.Account]
	if !ok {
		return nil, fmt.Errorf("no folder is configured for account %q", req.Account)
	}
	from, err := time.Parse("2006-01-02", req.From)
	if err != nil {
		return nil, fmt.Errorf("invalid from date %q", req.From)
	}
	cutoff := from.AddDate(0, 0, -overlapDays)

	dir, err := expandHome(acct.Dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read folder: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	seen := map[string]bool{}
	var out []source.Transaction
	for _, name := range names {
		kind := acct.kind(name)
		if kind == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		var rows []source.Transaction
		switch kind {
		case "csv":
			rows, err = parseCSV(data, acct.Profile, req.Account)
		case "ofx":
			rows, err = parseOFX(data, req.Account)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for _, tx := range rows {
			day, _ := time.Parse("2006-01-02", tx.BookingDate)
			if day.Before(cutoff) || seen[tx.ID] {
				continue
			}
			seen[tx.ID] = true
			out = append(out, tx)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].BookingDate < out[j].BookingDate })
	return &source.Result{Transactions: out}, nil
}

func (a Account) kind(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv", ".txt":
		if a.Format == "" || a.Format == "csv" {
			return "csv"
		}
	case ".ofx", ".qfx":
		if a.Format == "" || a.Format == "ofx" {
			return "ofx"
		}
	}
	return ""
}

func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
	}
	return p, nil
}
