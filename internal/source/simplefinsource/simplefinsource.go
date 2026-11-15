// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package simplefinsource reads movements from a SimpleFIN Bridge (or any
// SimpleFIN v2 server). The user claims a one-time setup token once and keeps
// the resulting access URL; nothing else is needed, no redirect and no
// approval step in Ratio, so the source is Local.
//
// Protocol: https://www.simplefin.org/protocol.html
package simplefinsource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
)

// overlapDays is read before the requested day: a bank may post a movement
// after the date it carries, and a pending one gets its final date later.
const overlapDays = 7

// Account says which SimpleFIN account feeds one Ratio account.
type Account struct {
	// AccessURL is https://user:pass@host/path, as returned by Claim.
	AccessURL string
	// ID is the account id inside the SimpleFIN connection.
	ID string
}

type Source struct {
	accounts map[string]Account
	client   *http.Client
}

// New takes the accounts by the reference Ratio holds for them.
func New(accounts map[string]Account) *Source {
	return &Source{accounts: accounts, client: &http.Client{Timeout: 60 * time.Second}}
}

func (*Source) Name() string { return "simplefin" }

// Configured reports whether the reference has an access URL and an account.
func (s *Source) Configured(ref string) bool {
	a, ok := s.accounts[ref]
	return ok && a.AccessURL != "" && a.ID != ""
}

type accountSet struct {
	Errlist  []apiError `json:"errlist"`
	Accounts []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Currency     string `json:"currency"`
		Transactions []struct {
			ID           string          `json:"id"`
			Posted       int64           `json:"posted"`
			Amount       string          `json:"amount"`
			Description  string          `json:"description"`
			Pending      bool            `json:"pending"`
			TransactedAt int64           `json:"transacted_at"`
			Extra        json.RawMessage `json:"extra"`
		} `json:"transactions"`
	} `json:"accounts"`
}

type apiError struct {
	Code      string `json:"code"`
	Msg       string `json:"msg"`
	AccountID string `json:"account_id"`
}

func (s *Source) Fetch(req source.Request) (*source.Result, error) {
	acct, ok := s.accounts[req.Account]
	if !ok || acct.AccessURL == "" || acct.ID == "" {
		return nil, fmt.Errorf("no SimpleFIN account is configured for %q", req.Account)
	}
	from, err := time.Parse("2006-01-02", req.From)
	if err != nil {
		return nil, fmt.Errorf("invalid from date %q", req.From)
	}

	endpoint, user, pass, err := splitAccessURL(acct.AccessURL)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("version", "2")
	q.Set("account", acct.ID)
	q.Set("start-date", strconv.FormatInt(from.AddDate(0, 0, -overlapDays).Unix(), 10))
	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint+"/accounts?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	httpReq.SetBasicAuth(user, pass)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("simplefin request: %w", errors.New(stripURL(err)))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden:
		return nil, fmt.Errorf("%w: SimpleFIN refused the access URL", source.ErrAuthExpired)
	case http.StatusPaymentRequired:
		return nil, errors.New("simplefin: the subscription behind this access URL needs payment")
	default:
		return nil, fmt.Errorf("simplefin: unexpected status %d", resp.StatusCode)
	}

	var set accountSet
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("simplefin: unreadable response: %w", err)
	}
	return normalize(set, acct.ID)
}

func normalize(set accountSet, accountID string) (*source.Result, error) {
	var found bool
	res := &source.Result{}
	for _, a := range set.Accounts {
		if a.ID != accountID {
			continue
		}
		found = true
		for _, t := range a.Transactions {
			// A pending movement has no final date or id worth keeping: it is
			// delivered once it posts.
			if t.Pending || t.Posted == 0 {
				continue
			}
			amount, err := strconv.ParseFloat(strings.TrimSpace(t.Amount), 64)
			if err != nil {
				return nil, fmt.Errorf("simplefin: transaction %q has an invalid amount", t.ID)
			}
			meta := map[string]any{"simplefinAccount": a.Name}
			if t.TransactedAt > 0 {
				meta["transactedAt"] = time.Unix(t.TransactedAt, 0).UTC().Format("2006-01-02")
			}
			res.Transactions = append(res.Transactions, source.Transaction{
				ID:          "sfin:" + a.ID + ":" + t.ID,
				BookingDate: time.Unix(t.Posted, 0).UTC().Format("2006-01-02"),
				Amount:      amount,
				Currency:    a.Currency,
				Description: strings.TrimSpace(t.Description),
				RawText:     strings.TrimSpace(t.Description),
				Metadata:    meta,
			})
		}
	}
	if !found {
		// The server lists what went wrong per account; show that, not a guess.
		for _, e := range set.Errlist {
			if e.AccountID == "" || e.AccountID == accountID {
				return nil, fmt.Errorf("simplefin: %s: %s", e.Code, e.Msg)
			}
		}
		return nil, fmt.Errorf("simplefin: account %q is not in the response", accountID)
	}
	return res, nil
}

// Claim exchanges a setup token for an access URL. The token works once.
func Claim(client *http.Client, setupToken string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(setupToken))
	if err != nil {
		return "", errors.New("the setup token is not valid base64")
	}
	claimURL := strings.TrimSpace(string(raw))
	if u, err := url.Parse(claimURL); err != nil || u.Scheme != "https" || u.Host == "" {
		return "", errors.New("the setup token does not contain an https claim address")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequest(http.MethodPost, claimURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("claim request: %s", stripURL(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode == http.StatusForbidden {
		return "", errors.New("the setup token was already used or does not exist; if you did not use it, disable it in SimpleFIN")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claim failed with status %d", resp.StatusCode)
	}
	access := strings.TrimSpace(string(body))
	if _, _, _, err := splitAccessURL(access); err != nil {
		return "", err
	}
	return access, nil
}

// splitAccessURL separates the credentials from the address so they never
// travel in the URL of a request or an error.
func splitAccessURL(access string) (endpoint, user, pass string, err error) {
	u, err := url.Parse(strings.TrimSpace(access))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User == nil {
		return "", "", "", errors.New("the SimpleFIN access URL must be https://user:password@host/path")
	}
	pass, _ = u.User.Password()
	user = u.User.Username()
	u.User = nil
	return strings.TrimRight(u.String(), "/"), user, pass, nil
}

// stripURL drops the address from a transport error: it can carry a path that
// identifies the connection.
func stripURL(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// AccountInfo names one account behind an access URL, for choosing which one
// feeds a Ratio account.
type AccountInfo struct {
	ID       string
	Name     string
	Currency string
}

// List asks the server for the accounts behind an access URL, without movements.
func List(client *http.Client, accessURL string) ([]AccountInfo, error) {
	endpoint, user, pass, err := splitAccessURL(accessURL)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, endpoint+"/accounts?version=2&balances-only=1", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("simplefin request: %s", stripURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: SimpleFIN refused the access URL", source.ErrAuthExpired)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("simplefin: unexpected status %d", resp.StatusCode)
	}
	var set accountSet
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&set); err != nil {
		return nil, fmt.Errorf("simplefin: unreadable response: %w", err)
	}
	out := make([]AccountInfo, 0, len(set.Accounts))
	for _, a := range set.Accounts {
		out = append(out, AccountInfo{ID: a.ID, Name: a.Name, Currency: a.Currency})
	}
	return out, nil
}
