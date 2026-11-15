// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package ebsource is the Enable Banking source: the provider that was the
// connector's only one before sources existed.
package ebsource

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/storage"
)

// Client is the part of the Enable Banking client a fetch needs.
type Client interface {
	GetTransactions(accountID, dateFrom string) ([]enablebanking.Transaction, []json.RawMessage, error)
}

type Source struct {
	client Client
}

func New(client Client) *Source { return &Source{client: client} }

func (*Source) Name() string { return "enablebanking" }

func (s *Source) Fetch(req source.Request) (*source.Result, error) {
	txs, raw, err := s.client.GetTransactions(req.Account, req.From)
	if err != nil {
		if enablebanking.IsAuthorizationError(err) {
			return nil, fmt.Errorf("%w: %v", source.ErrAuthExpired, err)
		}
		return nil, err
	}
	normalized := make([]source.Transaction, 0, len(txs))
	for _, tx := range txs {
		n, err := Normalize(tx)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, n)
	}
	return &source.Result{
		Transactions: normalized,
		Raw:          raw,
		Archive: func(dir, account string, includeRaw bool) (int, error) {
			return storage.AppendTransactionsWithOptions(dir, account, txs, raw, includeRaw)
		},
	}, nil
}

// Normalize turns an Enable Banking transaction into the shared shape.
func Normalize(tx enablebanking.Transaction) (source.Transaction, error) {
	amount, err := strconv.ParseFloat(tx.TransactionAmount.Amount, 64)
	if err != nil {
		return source.Transaction{}, fmt.Errorf("provider transaction has an invalid amount")
	}
	if tx.CreditDebitIndicator == "DBIT" {
		amount = -amount
	}
	desc := tx.Description()

	id := tx.EntryReference
	if id == "" {
		id = tx.TransactionID
	}
	if id == "" {
		id = fmt.Sprintf("%s_%s_%s", tx.BookingDate, tx.TransactionAmount.Amount, desc)
	}

	return source.Transaction{
		ID:               id,
		BookingDate:      tx.BookingDate,
		Amount:           amount,
		Currency:         tx.TransactionAmount.Currency,
		Description:      desc,
		Counterparty:     tx.CreditorName(),
		CounterpartyIBAN: tx.CreditorIBAN(),
	}, nil
}
