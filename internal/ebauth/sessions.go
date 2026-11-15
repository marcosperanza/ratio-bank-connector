// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package ebauth runs the Enable Banking authorization on this machine: the
// connector starts the bank consent, receives the code and keeps the session
// itself, so Ratio never takes part in it.
package ebauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Account struct {
	UID      string `json:"uid"`
	IBAN     string `json:"iban,omitempty"`
	Currency string `json:"currency,omitempty"`
	Name     string `json:"name,omitempty"`
	Product  string `json:"product,omitempty"`
}

// Session is one authorized bank connection. Account uids belong to the
// session: a renewal produces a new session, and the old one is dropped.
type Session struct {
	ID         string    `json:"id"`
	Bank       string    `json:"bank"`
	Country    string    `json:"country"`
	ValidUntil time.Time `json:"validUntil"`
	CreatedAt  time.Time `json:"createdAt"`
	Accounts   []Account `json:"accounts"`
}

func (s Session) Valid(now time.Time) bool { return s.ValidUntil.IsZero() || now.Before(s.ValidUntil) }

// Store keeps the sessions in a private JSON file next to the other state.
type Store struct {
	Path string
	mu   sync.Mutex
}

func (st *Store) Load() ([]Session, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.load()
}

func (st *Store) load() ([]Session, error) {
	data, err := os.ReadFile(st.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Session
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("read %s: %w", st.Path, err)
	}
	return out, nil
}

// Add stores a session and drops the earlier ones for the same bank, so a
// renewal replaces the expired connection instead of piling up beside it.
func (st *Store) Add(s Session) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	all, err := st.load()
	if err != nil {
		return err
	}
	kept := []Session{s}
	for _, o := range all {
		if o.ID != s.ID && !(strings.EqualFold(o.Bank, s.Bank) && strings.EqualFold(o.Country, s.Country)) {
			kept = append(kept, o)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].CreatedAt.After(kept[j].CreatedAt) })
	data, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.Path), 0o700); err != nil {
		return err
	}
	tmp := st.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.Path)
}

// Match finds the account a Ratio bank account stands for among the valid
// sessions. names are what Ratio calls it (display name, bank name). An IBAN
// or an account name wins; otherwise a bank name with a single account is
// enough. An empty result says why, so the user can fix the name in Ratio.
func Match(sessions []Session, names []string, now time.Time) (Session, Account, error) {
	norm := func(v string) string { return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), " ", "")) }
	var wanted []string
	for _, n := range names {
		if norm(n) != "" {
			wanted = append(wanted, norm(n))
		}
	}
	var live []Session
	for _, s := range sessions {
		if s.Valid(now) {
			live = append(live, s)
		}
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].CreatedAt.After(live[j].CreatedAt) })

	for _, s := range live {
		for _, a := range s.Accounts {
			for _, w := range wanted {
				if (a.IBAN != "" && norm(a.IBAN) == w) || (a.Name != "" && norm(a.Name) == w) {
					return s, a, nil
				}
			}
		}
	}
	for _, s := range live {
		for _, w := range wanted {
			if norm(s.Bank) == w && len(s.Accounts) == 1 {
				return s, s.Accounts[0], nil
			}
		}
	}
	var have []string
	for _, s := range live {
		for _, a := range s.Accounts {
			have = append(have, strings.TrimSpace(s.Bank+" "+a.IBAN))
		}
	}
	if len(have) == 0 {
		return Session{}, Account{}, errors.New("no authorized Enable Banking session on this connector: run `bank-connector link` or authorize the bank in the settings page")
	}
	return Session{}, Account{}, fmt.Errorf("no authorized account matches %q (available: %s): name the Ratio account after its IBAN or bank", strings.Join(names, " / "), strings.Join(have, ", "))
}

// Remove forgets a session. The bank keeps it until it expires; this only
// stops the connector from using it.
func (st *Store) Remove(id string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	all, err := st.load()
	if err != nil {
		return err
	}
	var kept []Session
	for _, s := range all {
		if s.ID != id {
			kept = append(kept, s)
		}
	}
	if kept == nil {
		kept = []Session{}
	}
	data, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(st.Path, data, 0o600)
}
