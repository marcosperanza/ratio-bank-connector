// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ebsource

import (
	"fmt"
	"strings"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
)

// AuthClient is the part of the Enable Banking client linking needs.
type AuthClient interface {
	InitAuth(bankName, country, redirectURL, state string) (string, error)
	CreateSession(code string) (*enablebanking.Session, error)
}

// Linker links an account through the user's own Enable Banking application.
type Linker struct{ client AuthClient }

func NewLinker(client AuthClient) *Linker { return &Linker{client: client} }

// callbackPath is the Ratio route Enable Banking sends the user back to. It is
// specific to this provider; others do not share it.
const callbackPath = "/api/connector/eb-callback"

func (l *Linker) Begin(req source.LinkRequest) (string, error) {
	callback := strings.TrimRight(req.RatioURL, "/") + callbackPath
	url, err := l.client.InitAuth(req.BankName, req.Country, callback, req.AccountID)
	if err != nil {
		return "", fmt.Errorf("init auth for %s: %w", req.BankName, err)
	}
	return url, nil
}

func (l *Linker) Complete(_ source.LinkRequest, code string) (*source.LinkResult, error) {
	session, err := l.client.CreateSession(code)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	res := &source.LinkResult{ValidUntil: session.Expires()}
	if len(session.Accounts) > 0 {
		a := session.Accounts[0]
		res.Ref, res.IBAN, res.Currency = a.ID, a.AccountID.IBAN, a.Currency
	}
	return res, nil
}
