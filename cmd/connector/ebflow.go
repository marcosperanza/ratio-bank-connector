// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ebauth"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
)

// DefaultRedirectURL is the page the user registers once in their own Enable
// Banking application. It needs no server: the user copies the code from it.
const DefaultRedirectURL = "https://enablebanking.com/"

// ExpiringSoonDays is when a consent is reported as due for renewal.
const ExpiringSoonDays = 14

type linkAPI interface {
	ebauth.API
	ListASPSPs(country string) ([]map[string]any, error)
}

// ebLinker runs the Enable Banking consent in a terminal. It backs `link`,
// `sessions --renew` and the setup wizard, so all three behave the same.
type ebLinker struct {
	in    *bufio.Reader
	out   io.Writer
	cfg   *config.Config
	store *ebauth.Store
	api   func(*config.Config) (linkAPI, error)
	open  func(url string)
	now   func() time.Time
}

func newLinker(in *bufio.Reader, out io.Writer, cfg *config.Config) *ebLinker {
	return &ebLinker{
		in: in, out: out, cfg: cfg,
		store: &ebauth.Store{Path: config.EBSessionsPath()},
		api: func(c *config.Config) (linkAPI, error) {
			return enablebanking.NewClient(c.EnableBanking.AppID, c.EnableBanking.PrivateKeyPath, c.EnableBanking.PrivateKeyB64)
		},
		open: openBrowser,
		now:  time.Now,
	}
}

func (l *ebLinker) say(format string, args ...any) { fmt.Fprintf(l.out, format+"\n", args...) }

func (l *ebLinker) redirect() string {
	if r := strings.TrimSpace(l.cfg.EnableBanking.RedirectURL); r != "" {
		return r
	}
	return DefaultRedirectURL
}

// pickBank lists the banks of a country (optionally filtered) and asks for one.
func (l *ebLinker) pickBank(client linkAPI, country, filter string) (string, error) {
	banks, err := client.ListASPSPs(country)
	if err != nil {
		return "", fmt.Errorf("list banks: %w", ebauth.Explain(err, ""))
	}
	var names []string
	for _, b := range banks {
		n, _ := b["name"].(string)
		if n != "" && (filter == "" || strings.Contains(strings.ToLower(n), strings.ToLower(filter))) {
			names = append(names, n)
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	if len(names) == 0 {
		return "", fmt.Errorf("no banks found for %s matching %q", strings.ToUpper(country), filter)
	}
	l.say("\nBanks in %s (%d):\n", strings.ToUpper(country), len(names))
	for i, n := range names {
		l.say("  [%2d] %s", i+1, n)
	}
	fmt.Fprint(l.out, "\nBank number: ")
	line, _ := l.in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(names) {
		return "", fmt.Errorf("invalid selection")
	}
	return names[n-1], nil
}

// Link authorizes one bank and stores the session.
func (l *ebLinker) Link(ctx context.Context, client linkAPI, bank, country string) (ebauth.Session, error) {
	redirect := l.redirect()
	state, err := ebauth.NewState()
	if err != nil {
		return ebauth.Session{}, err
	}
	var recv *ebauth.Receiver
	if ebauth.CanListen(redirect) {
		if recv, err = ebauth.NewReceiver(redirect, l.cfg.EnableBanking.CallbackCert, l.cfg.EnableBanking.CallbackKey, state); err != nil {
			return ebauth.Session{}, err
		}
		defer recv.Close()
	}
	authURL, err := ebauth.Begin(client, bank, country, redirect, state)
	if err != nil {
		return ebauth.Session{}, fmt.Errorf("start the authorization: %w", err)
	}
	l.say("\nOpen this address in a browser (on any device) and log in to %s:\n\n  %s\n", bank, authURL)
	if l.open != nil {
		l.open(authURL)
	}

	var code string
	if recv != nil {
		l.say("Waiting for the bank to send you back to %s (Ctrl+C to cancel)...", redirect)
		wctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		if code, err = recv.Wait(wctx); err != nil {
			return ebauth.Session{}, err
		}
	} else {
		l.say("When the bank is done you land on %s", redirect)
		fmt.Fprint(l.out, "Copy the code (or the whole address) from the address bar and paste it here: ")
		raw, _ := l.in.ReadString('\n')
		if code, err = ebauth.CodeFromInput(raw, state); err != nil {
			return ebauth.Session{}, err
		}
	}

	sess, err := ebauth.Finish(client, l.store, bank, country, code, l.now())
	if err != nil {
		return ebauth.Session{}, fmt.Errorf("create the session: %w", err)
	}
	l.say("\n✓ %s connected, valid until %s:\n", bank, validity(sess))
	for _, a := range sess.Accounts {
		l.say("  %s  %s  %s", a.IBAN, a.Currency, a.Name)
	}
	l.say("\nIn Ratio add the account with provider \"enablebanking-local\" and name it after the IBAN")
	l.say("(or the bank name, if it has a single account). To renew later: bank-connector sessions --renew")
	return sess, nil
}

func validity(s ebauth.Session) string {
	if s.ValidUntil.IsZero() {
		return "(the bank gave no end date)"
	}
	return s.ValidUntil.Format("2006-01-02")
}

// Interactive picks a bank and links it.
func (l *ebLinker) Interactive(ctx context.Context, country, filter string) error {
	client, err := l.api(l.cfg)
	if err != nil {
		return fmt.Errorf("enable banking client: %w", err)
	}
	bank, err := l.pickBank(client, country, filter)
	if err != nil {
		return err
	}
	_, err = l.Link(ctx, client, bank, country)
	return err
}

// SessionState says how a stored session stands.
type SessionState int

const (
	SessionOK SessionState = iota
	SessionExpiring
	SessionExpired
)

func StateOf(s ebauth.Session, now time.Time) (SessionState, int) {
	if s.ValidUntil.IsZero() {
		return SessionOK, 0
	}
	days := int(s.ValidUntil.Sub(now).Hours() / 24)
	switch {
	case !s.Valid(now):
		return SessionExpired, days
	case days <= ExpiringSoonDays:
		return SessionExpiring, days
	}
	return SessionOK, days
}
