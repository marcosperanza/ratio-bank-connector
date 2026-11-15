// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package enablebanking

import (
	"encoding/json"
	"time"
)

// AccountIdentification holds the account identifier (IBAN or BBAN).
type AccountIdentification struct {
	IBAN string `json:"iban,omitempty"`
	BBAN string `json:"bban,omitempty"`
}

// Account represents a bank account from Enable Banking API.
type Account struct {
	ID              string                `json:"uid"`
	AccountID       AccountIdentification `json:"account_id,omitempty"`
	Currency        string                `json:"currency,omitempty"`
	AccountHolder   string                `json:"name,omitempty"`
	ProductName     string                `json:"product,omitempty"`
	InstitutionName string                `json:"institution_id,omitempty"`
}

// TransactionAmount is the nested amount object.
type TransactionAmount struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}

// Party represents a creditor or debtor name.
type Party struct {
	Name string `json:"name,omitempty"`
}

// Transaction represents a bank transaction from Enable Banking API.
type Transaction struct {
	EntryReference          string                 `json:"entry_reference,omitempty"`
	BookingDate             string                 `json:"booking_date,omitempty"`
	ValueDate               string                 `json:"value_date,omitempty"`
	TransactionDate         string                 `json:"transaction_date,omitempty"`
	TransactionAmount       TransactionAmount      `json:"transaction_amount"`
	CreditDebitIndicator    string                 `json:"credit_debit_indicator,omitempty"` // "DBIT" | "CRDT"
	Status                  string                 `json:"status,omitempty"`                 // "BOOK" | "PEND"
	RemittanceInfo          []string               `json:"remittance_information,omitempty"`
	Creditor                *Party                 `json:"creditor,omitempty"`
	CreditorAccount         *AccountIdentification `json:"creditor_account,omitempty"`
	Debtor                  *Party                 `json:"debtor,omitempty"`
	DebtorAccount           *AccountIdentification `json:"debtor_account,omitempty"`
	MerchantCategoryCode    *string                `json:"merchant_category_code,omitempty"`
	ReferenceNumber         *string                `json:"reference_number,omitempty"`
	Note                    *string                `json:"note,omitempty"`
	BalanceAfterTransaction *TransactionAmount     `json:"balance_after_transaction,omitempty"`
	TransactionID           string                 `json:"transaction_id,omitempty"`
}

// Description returns remittance_information joined as a single string.
func (t Transaction) Description() string {
	if len(t.RemittanceInfo) == 0 {
		return ""
	}
	out := t.RemittanceInfo[0]
	for _, s := range t.RemittanceInfo[1:] {
		if s != "" {
			out += " " + s
		}
	}
	return out
}

// CreditorName returns the creditor party name, if any.
func (t Transaction) CreditorName() string {
	if t.Creditor != nil {
		return t.Creditor.Name
	}
	return ""
}

// DebtorName returns the debtor party name, if any.
func (t Transaction) DebtorName() string {
	if t.Debtor != nil {
		return t.Debtor.Name
	}
	return ""
}

// CreditorIBAN returns the creditor account IBAN, if any.
func (t Transaction) CreditorIBAN() string {
	if t.CreditorAccount != nil {
		return t.CreditorAccount.IBAN
	}
	return ""
}

// DebtorIBAN returns the debtor account IBAN, if any.
func (t Transaction) DebtorIBAN() string {
	if t.DebtorAccount != nil {
		return t.DebtorAccount.IBAN
	}
	return ""
}

// MCC returns merchant_category_code or empty string.
func (t Transaction) MCC() string {
	if t.MerchantCategoryCode != nil {
		return *t.MerchantCategoryCode
	}
	return ""
}

// RefNumber returns reference_number or empty string.
func (t Transaction) RefNumber() string {
	if t.ReferenceNumber != nil {
		return *t.ReferenceNumber
	}
	return ""
}

// NoteStr returns note or empty string.
func (t Transaction) NoteStr() string {
	if t.Note != nil {
		return *t.Note
	}
	return ""
}

// BalanceAfter returns the balance after transaction as signed string, or empty.
func (t Transaction) BalanceAfter() string {
	if t.BalanceAfterTransaction == nil {
		return ""
	}
	return t.BalanceAfterTransaction.Amount + " " + t.BalanceAfterTransaction.Currency
}

// TransactionsResponse is the Enable Banking GET /accounts/{id}/transactions response.
type TransactionsResponse struct {
	Transactions    []Transaction `json:"transactions"`
	ContinuationKey string        `json:"continuation_key,omitempty"`
}

// TransactionsResponseRaw preserves each transaction as raw JSON for storage.
type TransactionsResponseRaw struct {
	Transactions    []json.RawMessage `json:"transactions"`
	ContinuationKey string            `json:"continuation_key,omitempty"`
}

// AccountsResponse is the Enable Banking GET /sessions/{id}/accounts response.
type AccountsResponse struct {
	Accounts []Account `json:"accounts"`
}

// ASPSPsResponse is the Enable Banking GET /aspsps response.
type ASPSPsResponse struct {
	ASPSPs []map[string]any `json:"aspsps"`
}

// AuthAccess holds the valid_until for the auth request.
type AuthAccess struct {
	ValidUntil string `json:"valid_until"`
}

// AuthAspsp identifies the bank by name + country (no ID).
type AuthAspsp struct {
	Name    string `json:"name"`
	Country string `json:"country"`
}

// AuthRequest starts the OAuth flow — matches the real EB /auth body.
type AuthRequest struct {
	Access      AuthAccess `json:"access"`
	Aspsp       AuthAspsp  `json:"aspsp"`
	State       string     `json:"state"`
	RedirectURL string     `json:"redirect_url"`
	PSUType     string     `json:"psu_type,omitempty"` // "personal" | "business"
}

// AuthInitResponse is the Enable Banking /auth response.
type AuthInitResponse struct {
	URL string `json:"url"`
}

// SessionCreateRequest creates a session after bank redirect — only code needed.
type SessionCreateRequest struct {
	Code string `json:"code"`
}

// Session represents an active Enable Banking session.
type Session struct {
	SessionID  string    `json:"session_id"`
	Accounts   []Account `json:"accounts"`
	ValidUntil time.Time `json:"valid_until"`
	// Access is where the API reports the consent validity; ValidUntil above
	// is kept for responses that carry it at the top level.
	Access struct {
		ValidUntil time.Time `json:"valid_until"`
	} `json:"access"`
}

// Expires is when the consent ends, wherever the API put it.
func (s Session) Expires() time.Time {
	if !s.Access.ValidUntil.IsZero() {
		return s.Access.ValidUntil
	}
	return s.ValidUntil
}
