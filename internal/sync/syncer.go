// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/crypto"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source/ebsource"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/storage"
)

// Syncer orchestrates EB → CSV → [Ratio inbox].
type Syncer struct {
	cfg      *config.Config
	cfgPath  string
	ebClient bankClient
	// sources serve the accounts of managed mode by provider name; linkers
	// handle the approval step for the providers that have one.
	sources      map[string]source.Source
	linkers      map[string]source.Linker
	ratioClient  inboxClient
	masterKey    []byte // standalone mode only (AES symmetric, legacy)
	searchKey    []byte // standalone mode only
	publicKeyJwk string // ratio_managed mode (RSA hybrid encryption)
	log          *zap.Logger
	reportHook   func([]ratio.StatusUpdate) error // test seam for ReportUpdates

	// OnCycle, when set, receives the outcome of every daemon cycle (for the
	// status file the tray and settings UI read).
	OnCycle func(err error)
}

func (s *Syncer) reportUpdates(rm config.RatioManagedConfig, updates []ratio.StatusUpdate) error {
	if s.reportHook != nil {
		return s.reportHook(updates)
	}
	return ratio.ReportUpdates(rm.URL, rm.APIKey, updates)
}

func (s *Syncer) reportCycle(err error) {
	if s.OnCycle != nil {
		s.OnCycle(err)
	}
}

// watchSources lets the sources that have something to say between syncs say it.
func (s *Syncer) watchSources(now time.Time) {
	for _, src := range s.sources {
		if w, ok := src.(source.Watcher); ok {
			w.Watch(now)
		}
	}
}

type bankClient interface {
	GetTransactions(accountID, dateFrom string) ([]enablebanking.Transaction, []json.RawMessage, error)
	InitAuth(bankName, country, redirectURL, state string) (string, error)
	CreateSession(code string) (*enablebanking.Session, error)
}

type inboxClient interface {
	PushInbox(workspaceID string, items []ratio.InboxItem) (*ratio.PushResult, error)
}

// New creates a Syncer. masterKey and searchKey may be nil if Ratio push is disabled.
func New(
	cfg *config.Config,
	cfgPath string,
	ebClient bankClient,
	ratioClient inboxClient,
	masterKey []byte,
	searchKey []byte,
	log *zap.Logger,
) *Syncer {
	s := &Syncer{
		cfg:         cfg,
		cfgPath:     cfgPath,
		ebClient:    ebClient,
		sources:     map[string]source.Source{},
		linkers:     map[string]source.Linker{},
		ratioClient: ratioClient,
		masterKey:   masterKey,
		searchKey:   searchKey,
		log:         log,
	}
	// A connector that only reads files or SimpleFIN has no Enable Banking
	// credentials; accounts that name that provider are then reported as
	// unknown instead of failing on a nil client.
	if ebClient != nil {
		s.sources[DefaultProvider] = ebsource.New(ebClient)
		s.linkers[DefaultProvider] = ebsource.NewLinker(ebClient)
	}
	return s
}

// DefaultProvider serves accounts whose provider the server leaves unnamed.
const DefaultProvider = "enablebanking"

// RegisterSource adds a provider. A source that is also a source.Linker
// takes part in the approval step; one that is a source.Local is ready once
// configured on this machine.
func (s *Syncer) RegisterSource(src source.Source) {
	s.sources[src.Name()] = src
	if l, ok := src.(source.Linker); ok {
		s.linkers[src.Name()] = l
	}
}

func providerName(acc ratio.RemoteBankAccount) string {
	if acc.Provider == "" {
		return DefaultProvider
	}
	return acc.Provider
}

// SyncAll runs one sync cycle.
// In ratio_managed mode: polls Ratio for accounts, processes OAuth handshakes, then syncs.
// In standalone mode: uses local config accounts.
func (s *Syncer) SyncAll() error {
	if s.cfg.Mode == "ratio_managed" {
		return s.syncAllManaged(false)
	}
	return s.syncAllStandalone()
}

// SyncAllForce bypasses the remote last-success gate for an explicit manual
// sync. Daemon scheduling must always use SyncAll instead.
func (s *Syncer) SyncAllForce() error {
	if s.cfg.Mode == "ratio_managed" {
		return s.syncAllManaged(true)
	}
	return s.syncAllStandalone()
}

// syncAllStandalone — existing behavior, unchanged.
func (s *Syncer) syncAllStandalone() error {
	for i := range s.cfg.Accounts {
		acc := &s.cfg.Accounts[i]
		if err := s.syncAccount(acc); err != nil {
			s.log.Error("sync failed for account",
				zap.String("account", acc.EBAccountID),
				zap.Error(err),
			)
		}
	}
	return nil
}

// syncAllManaged — polls Ratio, handles OAuth state machine, syncs LINKED accounts.
// Used for one-shot `sync` command (no daemon). Runs both poll and sync cycles.
func (s *Syncer) syncAllManaged(force bool) error {
	_, err := s.managedCycle(force, time.Now())
	return err
}

// ManagedCycle fetches the remote configuration once, then handles both
// control state and any bank sync that is due.
func (s *Syncer) ManagedCycle() (*ratio.PollResponse, error) {
	return s.managedCycle(false, time.Now())
}

func (s *Syncer) managedCycle(force bool, now time.Time) (*ratio.PollResponse, error) {
	s.watchSources(now)
	pr, err := s.fetchManagedConfig()
	if err != nil {
		return nil, err
	}
	if err := s.pollCycle(pr); err != nil {
		return pr, err
	}
	if err := s.syncCycleWithConfig(pr, force, now); err != nil {
		return pr, err
	}
	return pr, nil
}

func (s *Syncer) fetchManagedConfig() (*ratio.PollResponse, error) {
	rm := s.cfg.RatioManaged
	pr, err := ratio.PollConfig(rm.URL, rm.APIKey)
	if err != nil {
		return nil, fmt.Errorf("poll config: %w", err)
	}
	if pr.PublicKeyJwk != "" && pr.PublicKeyJwk != s.publicKeyJwk {
		s.publicKeyJwk = pr.PublicKeyJwk
		s.log.Info("RSA public key refreshed from Ratio")
	}
	return pr, nil
}

// PollCycle handles OAuth state transitions (PENDING → LINKING → LINKED).
// Kept for one-shot callers; the daemon uses ManagedCycle.
func (s *Syncer) PollCycle() error {
	pr, err := s.fetchManagedConfig()
	if err != nil {
		return err
	}
	return s.pollCycle(pr)
}

func (s *Syncer) pollCycle(pr *ratio.PollResponse) error {
	rm := s.cfg.RatioManaged

	var updates []ratio.StatusUpdate
	actionable := 0

	for i := range pr.BankAccounts {
		acc := &pr.BankAccounts[i]
		switch acc.Status {
		case ratio.StatusPending:
			actionable++
			upd, err := s.handlePending(*acc, rm.URL)
			if err != nil {
				s.log.Error("handle pending account failed — reporting error to Ratio",
					zap.String("id", acc.ID),
					zap.String("bank", acc.BankName),
					zap.Error(err),
				)
				errUpd := ratio.SyncErrorUpdate(acc.ID, err.Error())
				updates = append(updates, errUpd)
				continue
			}
			if upd != nil {
				updates = append(updates, *upd)
			}

		case ratio.StatusLinking:
			if acc.PendingAuthCode == "" {
				s.log.Info("waiting for user to authorize bank in browser",
					zap.String("id", acc.ID),
					zap.String("bank", acc.BankName),
				)
				actionable++
				continue
			}
			actionable++
			upd, err := s.handleLinking(*acc)
			if err != nil {
				s.log.Error("handle linking account failed",
					zap.String("id", acc.ID),
					zap.Error(err),
				)
				errUpd := ratio.SyncErrorUpdate(acc.ID, err.Error())
				updates = append(updates, errUpd)
				continue
			}
			if upd != nil {
				updates = append(updates, *upd)
				// ReportUpdates commits this transition before the shared
				// configuration is reused by the sync half of ManagedCycle.
				acc.Status = ratio.StatusLinked
				acc.EBAccountID = upd.EBAccountID
				acc.SessionValidUntil = upd.SessionValidUntil
				acc.PendingAuthCode = ""
			}

		case ratio.StatusExpired, ratio.StatusError:
			if m, ok := s.sources[providerName(*acc)].(source.Matcher); ok && acc.EBAccountID == "" {
				// The user renews the consent on this machine; pick it up
				// without asking for a click in Ratio.
				if upd, err := s.matchLocal(m, *acc); err == nil {
					updates = append(updates, *upd)
					acc.Status = ratio.StatusLinked
					acc.EBAccountID = upd.EBAccountID
					acc.SessionValidUntil = upd.SessionValidUntil
				} else {
					s.log.Info("waiting for a local authorization", zap.String("id", acc.ID), zap.String("reason", err.Error()))
				}
				break
			}
			if acc.Status == ratio.StatusExpired {
				s.log.Warn("bank account session expired, re-link needed",
					zap.String("id", acc.ID),
					zap.String("bank", acc.BankName),
				)
			}
		}
	}

	if actionable == 0 {
		if len(pr.BankAccounts) == 0 {
			s.log.Info("no bank accounts configured — add accounts on the Ratio dashboard to get started")
		} else {
			s.log.Info("all accounts linked — waiting for next sync cycle",
				zap.Int("linked", len(pr.BankAccounts)),
			)
		}
	}

	if len(updates) > 0 {
		if err := s.reportUpdates(rm, updates); err != nil {
			return fmt.Errorf("report control updates: %w", err)
		}
	}

	return nil
}

// SyncCycle fetches EB transactions for all LINKED accounts.
// Runs every sync.interval_hours in daemon mode.
func (s *Syncer) SyncCycle() error {
	return s.syncCycle(false, time.Now())
}

func (s *Syncer) syncCycle(force bool, now time.Time) error {
	pr, err := s.fetchManagedConfig()
	if err != nil {
		return err
	}
	return s.syncCycleWithConfig(pr, force, now)
}

func (s *Syncer) syncCycleWithConfig(pr *ratio.PollResponse, force bool, now time.Time) error {
	rm := s.cfg.RatioManaged

	var updates []ratio.StatusUpdate
	for _, acc := range pr.BankAccounts {
		if acc.Status != ratio.StatusLinked && !(acc.Status == ratio.StatusError && acc.EBAccountID != "") {
			continue
		}
		// Proactive expiry check: if session is known-expired, skip EB call
		if acc.SessionValidUntil != "" {
			if exp, err := time.Parse(time.RFC3339, acc.SessionValidUntil); err == nil && now.After(exp) {
				s.log.Warn("EB session expired, requesting re-link",
					zap.String("id", acc.ID),
					zap.String("bank", acc.BankName),
					zap.Time("expiredAt", exp),
				)
				updates = append(updates, ratio.SessionExpiredUpdate(acc.ID))
				continue
			}
		}
		if !force {
			due, nextSyncAt := accountSyncDue(acc.LastSyncAt, acc.LastSyncAttemptAt, s.cfg.Sync.IntervalHours, now)
			if !due {
				s.log.Info("account sync skipped — interval not elapsed",
					zap.String("id", acc.ID),
					zap.Time("nextSyncAt", nextSyncAt),
				)
				continue
			}
		}
		// Persist the attempt before calling the provider. If Ratio is
		// unreachable, do not spend an Enable Banking request that a restart
		// could immediately repeat.
		if err := ratio.ReportUpdates(rm.URL, rm.APIKey, []ratio.StatusUpdate{ratio.SyncStartedUpdate(acc.ID)}); err != nil {
			s.log.Error("could not record sync attempt — provider call skipped", zap.String("id", acc.ID), zap.Error(err))
			continue
		}
		syncUpds := s.syncRemoteAccount(acc)
		updates = append(updates, syncUpds...)
	}

	if len(updates) > 0 {
		if err := ratio.ReportUpdates(rm.URL, rm.APIKey, updates); err != nil {
			s.log.Error("report updates failed", zap.Error(err))
		}
	}

	return nil
}

func accountSyncDue(lastSyncAt, lastSyncAttemptAt string, intervalHours int, now time.Time) (bool, time.Time) {
	if intervalHours <= 0 {
		intervalHours = 6
	}
	lastActivity := lastSyncAttemptAt
	if lastActivity == "" {
		lastActivity = lastSyncAt
	}
	if lastActivity == "" {
		return true, time.Time{}
	}
	last, err := time.Parse(time.RFC3339, lastActivity)
	if err != nil {
		return true, time.Time{}
	}
	next := last.Add(time.Duration(intervalHours) * time.Hour)
	return !now.Before(next), next
}

// handlePending starts a pending account: an approval address for providers
// that need one, immediate readiness for the ones that only need a setting here.
func (s *Syncer) handlePending(acc ratio.RemoteBankAccount, ratioURL string) (*ratio.StatusUpdate, error) {
	name := providerName(acc)
	if m, ok := s.sources[name].(source.Matcher); ok {
		return s.matchLocal(m, acc)
	}
	if linker, ok := s.linkers[name]; ok {
		url, err := linker.Begin(s.linkRequest(acc, ratioURL))
		if err != nil {
			return nil, err
		}
		s.log.Info("generated auth URL for pending account",
			zap.String("id", acc.ID),
			zap.String("bank", acc.BankName),
			zap.String("provider", name),
		)
		upd := ratio.AuthURLUpdate(acc.ID, url)
		return &upd, nil
	}
	src, ok := s.sources[name]
	if !ok {
		return nil, fmt.Errorf("this connector does not support the %q provider", name)
	}
	local, ok := src.(source.Local)
	if !ok {
		return nil, fmt.Errorf("the %q provider cannot be linked from this connector", name)
	}
	if !local.Configured(acc.ID) {
		return nil, fmt.Errorf("this computer has no setting for the account %q: add it to the connector configuration", acc.DisplayName)
	}
	upd := ratio.SessionCreatedUpdate(acc.ID, acc.ID, "", "", time.Time{})
	return &upd, nil
}

// matchLocal links an account to an approval the user gave on this computer.
func (s *Syncer) matchLocal(m source.Matcher, acc ratio.RemoteBankAccount) (*ratio.StatusUpdate, error) {
	req := s.linkRequest(acc, "")
	res, err := m.Match(req)
	if err != nil {
		return nil, err
	}
	upd := ratio.SessionCreatedUpdate(acc.ID, res.Ref, res.IBAN, res.Currency, res.ValidUntil)
	return &upd, nil
}

// handleLinking completes an approval the user gave in the browser.
func (s *Syncer) handleLinking(acc ratio.RemoteBankAccount) (*ratio.StatusUpdate, error) {
	name := providerName(acc)
	linker, ok := s.linkers[name]
	if !ok {
		return nil, fmt.Errorf("this connector does not support the %q provider", name)
	}
	res, err := linker.Complete(s.linkRequest(acc, ""), acc.PendingAuthCode)
	if err != nil {
		return nil, err
	}
	s.log.Info("session created for account",
		zap.String("id", acc.ID),
		zap.String("ebAccountId", res.Ref),
		zap.String("iban", maskIBAN(res.IBAN)),
	)
	upd := ratio.SessionCreatedUpdate(acc.ID, res.Ref, res.IBAN, res.Currency, res.ValidUntil)
	return &upd, nil
}

func (s *Syncer) linkRequest(acc ratio.RemoteBankAccount, ratioURL string) source.LinkRequest {
	return source.LinkRequest{AccountID: acc.ID, AccountName: acc.DisplayName, BankName: acc.BankName, Country: acc.Country, RatioURL: ratioURL}
}

// syncRemoteAccount syncs a LINKED remote account. Returns status update(s).
func (s *Syncer) syncRemoteAccount(acc ratio.RemoteBankAccount) []ratio.StatusUpdate {
	if acc.EBAccountID == "" {
		return nil
	}

	dateFrom := ""
	if acc.LastSyncAt != "" {
		if t, err := time.Parse(time.RFC3339, acc.LastSyncAt); err == nil {
			dateFrom = t.Format("2006-01-02")
		}
	}
	if dateFrom == "" {
		dateFrom = time.Now().AddDate(0, 0, -s.cfg.Sync.LookbackDays).Format("2006-01-02")
	}

	s.log.Info("syncing remote account",
		zap.String("id", acc.ID),
		zap.String("ebAccountId", acc.EBAccountID),
		zap.String("from", dateFrom),
	)

	src, ok := s.sources[providerName(acc)]
	if !ok {
		return []ratio.StatusUpdate{ratio.SyncErrorUpdate(acc.ID, fmt.Sprintf("this connector does not support the %q provider", providerName(acc)))}
	}
	fetched, err := src.Fetch(source.Request{Account: acc.EBAccountID, From: dateFrom})
	if err != nil {
		if errors.Is(err, source.ErrAuthExpired) {
			s.log.Warn("EB session expired during sync, requesting re-link",
				zap.String("id", acc.ID),
				zap.String("bank", acc.BankName),
			)
			return []ratio.StatusUpdate{ratio.SessionExpiredUpdate(acc.ID)}
		}
		errMsg := fmt.Sprintf("get transactions: %s", err)
		s.log.Error("sync failed", zap.String("id", acc.ID), zap.Error(err))
		return []ratio.StatusUpdate{ratio.SyncErrorUpdate(acc.ID, errMsg)}
	}
	txs := fetched.Transactions

	appended := len(txs)
	archiveMode := s.cfg.EffectiveArchiveMode()
	if archiveMode != config.ArchiveOff && fetched.Archive != nil {
		includeRaw := archiveMode == config.ArchiveCSVAndRaw
		appended, err = fetched.Archive(s.cfg.Sync.CSVDir, acc.EBAccountID, includeRaw)
		if err != nil {
			errMsg := fmt.Sprintf("write local archive: %s", err)
			s.log.Error("local archive write failed", zap.String("id", acc.ID), zap.Error(err))
			return []ratio.StatusUpdate{ratio.SyncErrorUpdate(acc.ID, errMsg)}
		}
		s.log.Info("local archive updated", zap.String("id", acc.ID), zap.String("mode", archiveMode), zap.Int("appended", appended), zap.Int("total", len(txs)))
	}

	// Inbox push — RSA hybrid encryption (ratio_managed mode)
	if acc.PushToInbox {
		if s.ratioClient == nil {
			return []ratio.StatusUpdate{ratio.SyncErrorUpdate(acc.ID, "Ratio inbox client is unavailable")}
		}
		if s.publicKeyJwk == "" {
			return []ratio.StatusUpdate{ratio.SyncErrorUpdate(acc.ID, "Ratio encryption public key is unavailable")}
		}
		items, err := s.buildInboxItemsRSA(txs)
		if err != nil {
			s.log.Error("inbox encryption failed", zap.String("id", acc.ID), zap.Error(err))
			return []ratio.StatusUpdate{ratio.SyncErrorUpdate(acc.ID, "encrypt transactions for Ratio inbox")}
		}
		if len(items) > 0 {
			result, err := s.ratioClient.PushInbox(acc.WorkspaceID, items)
			if err != nil {
				s.log.Error("inbox push failed", zap.String("id", acc.ID), zap.Error(err))
				return []ratio.StatusUpdate{ratio.SyncErrorUpdate(acc.ID, "push transactions to Ratio inbox")}
			}
			s.log.Info("pushed to ratio inbox",
				zap.Int("queued", result.Queued),
				zap.Int("duplicates", result.Duplicates),
			)
		}
	}

	return []ratio.StatusUpdate{ratio.SyncCompleteUpdate(acc.ID, appended)}
}

// syncAccount is the standalone per-account sync (unchanged from v1).
func (s *Syncer) syncAccount(acc *config.AccountConfig) error {
	dateFrom := acc.LastSyncAt
	if dateFrom.IsZero() {
		dateFrom = time.Now().AddDate(0, 0, -s.cfg.Sync.LookbackDays)
	}
	fromStr := dateFrom.Format("2006-01-02")

	s.log.Info("syncing account",
		zap.String("account", acc.EBAccountID),
		zap.String("from", fromStr),
	)

	txs, rawTxs, err := s.ebClient.GetTransactions(acc.EBAccountID, fromStr)
	if err != nil {
		return fmt.Errorf("get transactions: %w", err)
	}

	appended, err := storage.AppendTransactions(s.cfg.Sync.CSVDir, acc.EBAccountID, txs, rawTxs)
	if err != nil {
		return fmt.Errorf("write csv: %w", err)
	}
	s.log.Info("csv updated", zap.Int("appended", appended), zap.Int("total", len(txs)))

	if s.cfg.Ratio.Enabled && s.ratioClient != nil && s.masterKey != nil {
		items, err := s.buildInboxItems(txs, acc)
		if err != nil {
			return fmt.Errorf("build inbox items: %w", err)
		}
		if len(items) > 0 {
			result, err := s.ratioClient.PushInbox(acc.RatioWorkspaceID, items)
			if err != nil {
				return fmt.Errorf("push inbox: %w", err)
			}
			s.log.Info("pushed to ratio inbox",
				zap.Int("queued", result.Queued),
				zap.Int("duplicates", result.Duplicates),
			)
		}
	}

	acc.LastSyncAt = time.Now()
	if err := config.SaveLastSyncAt(s.cfgPath, s.cfg); err != nil {
		s.log.Warn("failed to persist last_sync_at", zap.Error(err))
	}
	return nil
}

func (s *Syncer) buildInboxItems(txs []enablebanking.Transaction, acc *config.AccountConfig) ([]ratio.InboxItem, error) {
	var items []ratio.InboxItem
	for _, tx := range txs {
		amount, _ := strconv.ParseFloat(tx.TransactionAmount.Amount, 64)
		if tx.CreditDebitIndicator == "DBIT" {
			amount = -amount
		}
		desc := tx.Description()
		creditorName := tx.CreditorName()
		blob := crypto.TransactionBlob{
			Description:  desc,
			MerchantName: creditorName,
			Amount:       amount,
			Currency:     tx.TransactionAmount.Currency,
			IBAN:         tx.CreditorIBAN(),
		}

		ctB64, ivB64, err := crypto.EncryptBlob(blob, s.masterKey)
		if err != nil {
			return nil, err
		}

		bookingDate, _ := time.Parse("2006-01-02", tx.BookingDate)

		var merchantHash, descHash string
		if s.searchKey != nil {
			merchantHash = crypto.MerchantHash(creditorName, s.searchKey)
			descHash = crypto.DescriptionHash(desc, s.searchKey)
		}

		bankTxID := tx.EntryReference
		if bankTxID == "" {
			bankTxID = tx.TransactionID
		}
		if bankTxID == "" {
			bankTxID = fmt.Sprintf("%s_%s_%s", tx.BookingDate, tx.TransactionAmount.Amount, desc)
		}

		items = append(items, ratio.InboxItem{
			EncryptedBlob:     ctB64,
			EncryptionIV:      ivB64,
			MerchantHash:      merchantHash,
			DescriptionHash:   descHash,
			BankTransactionID: bankTxID,
			BookingDate:       bookingDate,
		})
	}
	return items, nil
}

// buildInboxItemsRSA encrypts transactions using RSA hybrid encryption (ratio_managed mode).
// No passphrase or master key required — uses the user's RSA public key from poll.
func (s *Syncer) buildInboxItemsRSA(txs []source.Transaction) ([]ratio.InboxItem, error) {
	var items []ratio.InboxItem
	for _, tx := range txs {
		if err := tx.Validate(); err != nil {
			return nil, err
		}
		blob := crypto.TransactionBlob{
			Description:  tx.Description,
			MerchantName: tx.Counterparty,
			Amount:       tx.Amount,
			Currency:     tx.Currency,
			IBAN:         tx.CounterpartyIBAN,
		}

		encSessionKey, encBlob, encIV, err := crypto.EncryptBlobHybrid(s.publicKeyJwk, blob)
		if err != nil {
			return nil, fmt.Errorf("hybrid encrypt: %w", err)
		}

		bookingDate, _ := time.Parse("2006-01-02", tx.BookingDate)

		items = append(items, ratio.InboxItem{
			EncryptedBlob:       encBlob,
			EncryptionIV:        encIV,
			EncryptedSessionKey: encSessionKey,
			BankTransactionID:   tx.ID,
			BookingDate:         bookingDate,
		})
	}
	return items, nil
}

func maskIBAN(iban string) string {
	if len(iban) <= 4 {
		return "****"
	}
	return "****" + iban[len(iban)-4:]
}
