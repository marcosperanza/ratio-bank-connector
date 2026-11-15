// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package eblocal is Enable Banking authorized on the connector's own machine,
// with the user's own application: Ratio takes no part in the consent. Reading
// movements is the ordinary Enable Banking call; what differs is how an account
// is linked (to a session stored here) and how the user is told it is ending.
package eblocal

import (
	"time"

	"go.uber.org/zap"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ebauth"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source/ebsource"
)

// Name is the provider id Ratio stores for these accounts.
const Name = "enablebanking-local"

// ExpiryWarnDays is how long before the end of a consent the connector starts saying so in its log.
const ExpiryWarnDays = 14

type Source struct {
	*ebsource.Source
	sessions *ebauth.Store
	log      *zap.Logger
	lastWarn time.Time
}

func New(client ebsource.Client, sessions *ebauth.Store, log *zap.Logger) *Source {
	return &Source{Source: ebsource.New(client), sessions: sessions, log: log}
}

func (*Source) Name() string { return Name }

// Match links an account to an account of a session this connector authorized: the account uid is what Enable
// Banking calls it, so syncing then follows the ordinary Enable Banking path.
func (s *Source) Match(req source.LinkRequest) (*source.LinkResult, error) {
	sessions, err := s.sessions.Load()
	if err != nil {
		return nil, err
	}
	sess, a, err := ebauth.Match(sessions, []string{req.AccountName, req.BankName}, time.Now())
	if err != nil {
		return nil, err
	}
	return &source.LinkResult{Ref: a.UID, IBAN: a.IBAN, Currency: a.Currency, ValidUntil: sess.ValidUntil}, nil
}

// Watch logs, at most once a day, the consents that are about to end or already ended. There is no screen to show it
// on in Docker, so the log (docker compose logs) is where an unattended server says it.
func (s *Source) Watch(now time.Time) {
	if !s.lastWarn.IsZero() && now.Sub(s.lastWarn) < 24*time.Hour {
		return
	}
	sessions, err := s.sessions.Load()
	if err != nil {
		return
	}
	s.lastWarn = now
	for _, sess := range sessions {
		if sess.ValidUntil.IsZero() {
			continue
		}
		days := int(sess.ValidUntil.Sub(now).Hours() / 24)
		switch {
		case !sess.Valid(now):
			s.log.Warn("Enable Banking consent expired: renew it with `bank-connector sessions --renew`",
				zap.String("bank", sess.Bank), zap.Time("endedAt", sess.ValidUntil))
		case days <= ExpiryWarnDays:
			s.log.Warn("Enable Banking consent ends soon: renew it with `bank-connector sessions --renew`",
				zap.String("bank", sess.Bank), zap.Int("daysLeft", days), zap.Time("endsAt", sess.ValidUntil))
		}
	}
}
