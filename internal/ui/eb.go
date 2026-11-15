// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ui

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ebauth"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
)

// EBAPI is what the settings page needs from Enable Banking.
type EBAPI interface {
	ebauth.API
	ListASPSPs(country string) ([]map[string]any, error)
}

func defaultEB(cfg *config.Config) (EBAPI, error) {
	return enablebanking.NewClient(cfg.EnableBanking.AppID, cfg.EnableBanking.PrivateKeyPath, cfg.EnableBanking.PrivateKeyB64)
}

// SessionView is a connected bank as the page shows it.
type SessionView struct {
	ID         string           `json:"id"`
	Bank       string           `json:"bank"`
	Country    string           `json:"country"`
	ValidUntil time.Time        `json:"validUntil"`
	DaysLeft   int              `json:"daysLeft"`
	Expired    bool             `json:"expired"`
	Accounts   []ebauth.Account `json:"accounts"`
}

// ExpiringSoonDays is when the page starts asking the user to renew.
const ExpiringSoonDays = 14

func sessionViews(sessions []ebauth.Session, now time.Time) []SessionView {
	out := []SessionView{}
	for _, s := range sessions {
		days := 0
		if !s.ValidUntil.IsZero() {
			days = int(s.ValidUntil.Sub(now).Hours() / 24)
		}
		out = append(out, SessionView{ID: s.ID, Bank: s.Bank, Country: s.Country, ValidUntil: s.ValidUntil, DaysLeft: days,
			Expired: !s.Valid(now), Accounts: append([]ebauth.Account{}, s.Accounts...)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ValidUntil.Before(out[j].ValidUntil) })
	return out
}

// ebFlow is one consent in progress: either the connector receives the
// callback itself ("auto") or the user pastes the URL ("paste").
type ebFlow struct {
	ID       string       `json:"id"`
	Mode     string       `json:"mode"`   // auto | paste
	Status   string       `json:"status"` // waiting | done | error
	Error    string       `json:"error,omitempty"`
	AuthURL  string       `json:"authUrl"`
	Redirect string       `json:"redirectUrl"`
	Session  *SessionView `json:"session,omitempty"`

	bank, country, state string
	api                  EBAPI
	cancel               context.CancelFunc
	finished             chan struct{}
}

type ebFlows struct {
	mu   sync.Mutex
	last *ebFlow
}

// start begins a consent, replacing any earlier one that never finished.
func (s *Server) ebStart(cfg *config.Config, bank, country string) (*ebFlow, error) {
	bank, country = strings.TrimSpace(bank), strings.ToUpper(strings.TrimSpace(country))
	if bank == "" || country == "" {
		return nil, errors.New("choose a bank")
	}
	newEB := s.NewEB
	if newEB == nil {
		newEB = defaultEB
	}
	api, err := newEB(cfg)
	if err != nil {
		return nil, err
	}
	state, err := ebauth.NewState()
	if err != nil {
		return nil, err
	}

	s.flows.mu.Lock()
	defer s.flows.mu.Unlock()
	if prev := s.flows.last; prev != nil && prev.cancel != nil {
		prev.cancel()
		select {
		case <-prev.finished:
		case <-time.After(3 * time.Second):
		}
	}

	redirect := cfg.EnableBanking.RedirectURL
	f := &ebFlow{ID: state, Status: "waiting", Redirect: redirect, bank: bank, country: country, state: state, api: api, finished: make(chan struct{})}
	var recv *ebauth.Receiver
	if ebauth.CanListen(redirect) {
		f.Mode = "auto"
		if recv, err = ebauth.NewReceiver(redirect, cfg.EnableBanking.CallbackCert, cfg.EnableBanking.CallbackKey, state); err != nil {
			return nil, err
		}
	} else {
		f.Mode = "paste"
	}
	if f.AuthURL, err = ebauth.Begin(api, bank, country, redirect, state); err != nil {
		if recv != nil {
			recv.Close()
		}
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	f.cancel = cancel
	s.flows.last = f
	go func() {
		defer close(f.finished)
		defer cancel()
		if recv == nil {
			<-ctx.Done()
			return
		}
		code, err := recv.Wait(ctx)
		s.ebComplete(f, code, err)
	}()
	return f, nil
}

// ebComplete turns a received code (or a failure) into the flow's final state.
func (s *Server) ebComplete(f *ebFlow, code string, err error) {
	var sess ebauth.Session
	if err == nil {
		sess, err = ebauth.Finish(f.api, s.sessionStore(), f.bank, f.country, code, time.Now())
	}
	s.flows.mu.Lock()
	defer s.flows.mu.Unlock()
	if err != nil {
		f.Status, f.Error = "error", err.Error()
		return
	}
	v := sessionViews([]ebauth.Session{sess}, time.Now())[0]
	f.Status, f.Session = "done", &v
}

func (s *Server) sessionStore() *ebauth.Store {
	if s.Sessions != nil {
		return s.Sessions
	}
	return &ebauth.Store{Path: config.EBSessionsPath()}
}

func (s *Server) ebFlow(id string) *ebFlow {
	s.flows.mu.Lock()
	defer s.flows.mu.Unlock()
	if f := s.flows.last; f != nil && f.ID == id {
		cp := *f
		return &cp
	}
	return nil
}
