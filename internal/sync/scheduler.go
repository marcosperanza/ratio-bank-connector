// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package sync

import (
	"context"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v3/internal/ratio"
	connectorrealtime "github.com/marcosperanza/ratio-bank-connector/v3/internal/realtime"
	"go.uber.org/zap"
)

// Ably delivers wakes immediately, so this cycle only reconciles missed
// wakes and starts due bank syncs (hours apart). Each cycle wakes Ratio's
// database, which on Neon then stays up for five minutes: keep it rare.
const realtimeFallbackInterval = 60 * time.Minute

// Used when realtime is unavailable. Above Neon's five-minute autosuspend,
// so a connector without Ably does not keep the database awake around the
// clock.
const defaultOfflinePollSeconds = 600

// RunDaemon runs SyncAll immediately, then every intervalHours until stopped.
func RunDaemon(s *Syncer, intervalHours int, stop <-chan struct{}) {
	if intervalHours <= 0 {
		intervalHours = 6
	}
	ticker := time.NewTicker(time.Duration(intervalHours) * time.Hour)
	defer ticker.Stop()
	s.log.Info("daemon started", zap.Int("interval_hours", intervalHours))
	if err := s.SyncAll(); err != nil {
		s.log.Error("initial sync failed", zap.Error(err))
	}
	for {
		select {
		case <-stop:
			s.log.Info("daemon stopped")
			return
		case <-ticker.C:
			if err := s.SyncAll(); err != nil {
				s.log.Error("sync failed", zap.Error(err))
			}
		}
	}
}

// RunManagedDaemon uses Ably as the fast path and HTTP as the durable fallback.
// A wake-up performs one remote config read and reuses it for OAuth handling
// and due bank syncs. When realtime is unavailable the legacy poll interval is
// restored automatically, so older Ratio deployments remain functional.
func RunManagedDaemon(s *Syncer, pollIntervalSeconds, intervalHours int, stop <-chan struct{}) {
	if pollIntervalSeconds <= 0 {
		pollIntervalSeconds = defaultOfflinePollSeconds
	}
	if intervalHours <= 0 {
		intervalHours = 6
	}
	offlineFallback := time.Duration(pollIntervalSeconds) * time.Second
	s.log.Info("managed daemon started",
		zap.Duration("offline_poll_interval", offlineFallback),
		zap.Duration("realtime_fallback_interval", realtimeFallbackInterval),
		zap.Int("sync_interval_hours", intervalHours),
	)

	var realtimeCancel context.CancelFunc
	var realtimeEvents <-chan connectorrealtime.Event
	var realtimePresenceChannel string
	startRealtime := func(pr *ratio.PollResponse) {
		if pr == nil || pr.APIKeyID == "" {
			return
		}
		if realtimeEvents != nil && pr.PresenceChannel == realtimePresenceChannel {
			return
		}
		if realtimeCancel != nil {
			realtimeCancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		events := make(chan connectorrealtime.Event, 8)
		realtimeCancel = cancel
		realtimeEvents = events
		realtimePresenceChannel = pr.PresenceChannel
		go connectorrealtime.Run(
			ctx,
			s.cfg.RatioManaged.URL,
			s.cfg.RatioManaged.APIKey,
			pr.APIKeyID,
			pr.PresenceChannel,
			events,
			s.log,
		)
	}
	defer func() {
		if realtimeCancel != nil {
			realtimeCancel()
		}
	}()

	runCycle := func(label string) {
		pr, err := s.ManagedCycle()
		if err != nil {
			s.log.Error(label, zap.Error(err))
			return
		}
		startRealtime(pr)
	}

	runCycle("initial managed cycle failed")
	realtimeConnected := false
	timer := time.NewTimer(offlineFallback)
	defer timer.Stop()
	resetTimer := func() {
		interval := managedPollInterval(realtimeConnected, offlineFallback)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(interval)
	}

	for {
		select {
		case <-stop:
			s.log.Info("managed daemon stopped")
			return
		case <-timer.C:
			runCycle("managed fallback cycle failed")
			resetTimer()
		case event := <-realtimeEvents:
			realtimeConnected = event.Connected
			if event.Terminal {
				realtimeEvents = nil
				realtimePresenceChannel = ""
			}
			if event.Wake {
				runCycle("realtime wake cycle failed")
			}
			resetTimer()
		}
	}
}

func managedPollInterval(realtimeConnected bool, offlineFallback time.Duration) time.Duration {
	if realtimeConnected {
		return realtimeFallbackInterval
	}
	return offlineFallback
}
