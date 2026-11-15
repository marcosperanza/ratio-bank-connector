// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package prices

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

// State is what the engine remembers between runs: how many calls each
// provider served today and which spelling worked for each ticker.
type State struct {
	Usage   map[string]Usage             `json:"usage"`   // provider -> today's calls
	Learned map[string]map[string]string `json:"learned"` // ticker -> provider -> candidate
	Last    map[string]string            `json:"last"`    // ticker -> last successful run date
	// Delivered is the newest closing date handed to Ratio, per "workspace|ticker": the next delivery sends what is newer.
	Delivered map[string]string `json:"delivered"`

	mu   sync.Mutex
	path string
}

type Usage struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

func LoadState(path string) (*State, error) {
	s := &State{path: path}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, s); err != nil {
			return nil, err
		}
	}
	if s.Usage == nil {
		s.Usage = map[string]Usage{}
	}
	if s.Learned == nil {
		s.Learned = map[string]map[string]string{}
	}
	if s.Last == nil {
		s.Last = map[string]string{}
	}
	if s.Delivered == nil {
		s.Delivered = map[string]string{}
	}
	return s, nil
}

func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func today(now time.Time) string { return now.UTC().Format("2006-01-02") }

func (s *State) used(provider string, now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.Usage[provider]; u.Date == today(now) {
		return u.Count
	}
	return 0
}

func (s *State) count(provider string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.Usage[provider]
	if u.Date != today(now) {
		u = Usage{Date: today(now)}
	}
	u.Count++
	s.Usage[provider] = u
}

// exhaust marks a provider as out of quota for the rest of the day.
func (s *State) exhaust(provider string, limit int, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Usage[provider] = Usage{Date: today(now), Count: max(limit, s.Usage[provider].Count)}
}

func (s *State) learn(ticker, provider, candidate string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Learned[ticker] == nil {
		s.Learned[ticker] = map[string]string{}
	}
	s.Learned[ticker][provider] = candidate
}

func (s *State) learned(ticker, provider string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Learned[ticker][provider]
}
