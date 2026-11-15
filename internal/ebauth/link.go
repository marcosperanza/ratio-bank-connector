// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ebauth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
)

// API is the part of the Enable Banking client the consent needs.
type API interface {
	InitAuth(bankName, country, redirectURL, state string) (string, error)
	CreateSession(code string) (*enablebanking.Session, error)
}

// NewState is the random value that ties a callback to the consent it started.
func NewState() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Begin starts a consent and returns the URL the user must open.
func Begin(c API, bank, country, redirectURL, state string) (string, error) {
	if strings.TrimSpace(redirectURL) == "" {
		return "", errors.New("enable_banking.redirect_url is not set: use the redirect URL registered in your Enable Banking application")
	}
	u, err := c.InitAuth(bank, country, redirectURL, state)
	if err != nil {
		return "", Explain(err, redirectURL)
	}
	return u, nil
}

// Explain adds what Enable Banking said, and the usual reason for a refusal,
// to an error that is shown only on this machine.
func Explain(err error, redirectURL string) error {
	d := enablebanking.Detail(err)
	if d == "" {
		return err
	}
	hint := ""
	low := strings.ToLower(d)
	switch {
	case strings.Contains(low, "redirect") && redirectURL != "":
		hint = " Add " + redirectURL + " to the redirect URLs of your application in the Enable Banking control panel, exactly as written."
	case strings.Contains(low, "aspsp") || strings.Contains(low, "not found"):
		hint = " The bank name or country is not one Enable Banking knows for this application."
	}
	return fmt.Errorf("%w — Enable Banking says: %s.%s", err, d, hint)
}

// Finish trades the callback code for a session and keeps it.
func Finish(c API, store *Store, bank, country, code string, now time.Time) (Session, error) {
	es, err := c.CreateSession(code)
	if err != nil {
		return Session{}, Explain(err, "")
	}
	s := Session{ID: es.SessionID, Bank: bank, Country: strings.ToUpper(country), ValidUntil: es.Expires(), CreatedAt: now}
	for _, a := range es.Accounts {
		s.Accounts = append(s.Accounts, Account{
			UID: a.ID, IBAN: a.AccountID.IBAN, Currency: a.Currency, Name: a.AccountHolder, Product: a.ProductName,
		})
	}
	return s, store.Add(s)
}
