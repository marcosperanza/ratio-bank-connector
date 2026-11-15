// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package source

import "time"

// The web dashboard owns the lifecycle of a connected account: the user adds
// it, the connector reports an authorization link, the user approves it, the
// connector confirms, and from then on movements flow into the inbox. A
// provider takes part in that lifecycle through one of the two interfaces
// below, and the core translates their answers into the same status updates
// the dashboard already understands. A provider never talks to Ratio itself.

// LinkRequest identifies the account being linked.
type LinkRequest struct {
	AccountID string // Ratio's id for the account
	// AccountName is what the user called the account in Ratio.
	AccountName string
	BankName    string
	Country     string
	// RatioURL is the base address of the user's Ratio. The core knows nothing
	// about how a provider gets its approval back: a provider that returns the
	// user to a Ratio address builds that address itself, and one that needs no
	// return address ignores this.
	RatioURL string
}

// LinkResult is a completed link.
type LinkResult struct {
	// Ref is the reference Fetch will receive as Request.Account.
	Ref      string
	IBAN     string
	Currency string
	// ValidUntil is when the authorization lapses; zero when it does not.
	ValidUntil time.Time
}

// Linker is implemented by providers that need the user's approval: open
// banking consents, OAuth to a mailbox or a cloud drive.
type Linker interface {
	// Begin returns the address the user must open to approve.
	Begin(req LinkRequest) (authURL string, err error)
	// Complete turns the code the provider returned into a usable link.
	Complete(req LinkRequest, code string) (*LinkResult, error)
}

// Local is implemented by providers that need no approval, only a setting on
// this computer (a folder to read, a file to watch). The account is ready as
// soon as the setting exists.
type Local interface {
	// Configured reports whether this machine knows what to do for the account.
	Configured(accountID string) bool
}

// Matcher is implemented by providers whose approval is given on this computer,
// outside Ratio (a consent the user made with the connector itself). The core
// asks it to find that approval for the account instead of showing an address.
type Matcher interface {
	// Match returns the link for the account, or an error saying what is missing
	// (no consent yet, expired): the core tries again on the next cycle.
	Match(req LinkRequest) (*LinkResult, error)
}

// Watcher is implemented by sources that have something to say when no account
// is being synced, such as an authorization that is about to lapse. The core
// calls it once per managed cycle and the source rate-limits its own messages.
type Watcher interface {
	Watch(now time.Time)
}
