// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ably/ably-go/ably"
	"go.uber.org/zap"
)

const connectorWakeEvent = "connector.wake"

type Event struct {
	Connected bool
	Wake      bool
	Terminal  bool
}

func connectorChannel(apiKeyID string) string {
	return "ratio:connector:" + apiKeyID
}

var tokenHTTPClient = &http.Client{Timeout: 30 * time.Second}

// tokenRequestTimeout bounds a single token fetch. It replaces the caller's
// deadline, which cannot be trusted: see tokenCallback.
const tokenRequestTimeout = 15 * time.Second

// tokenCallback fetches a signed Ably TokenRequest from Ratio.
//
// It exists instead of ably.WithAuthURL because ably-go (1.4.1, and main at the
// time of writing) renews tokens on a server AUTH message (RTN22) by calling
// Authorize with a context it has already cancelled, so that the event loop is
// not blocked. WithAuthURL builds its HTTP request from that context, so every
// in-place renewal failed with "context canceled": the token then expired, the
// connection dropped roughly once an hour, and only the reconnect — which uses
// a live context — obtained a new token.
//
// The callback therefore detaches from the caller's cancellation and applies its
// own timeout. Blocking the event loop for one bounded HTTP round trip is the
// price, and it is far cheaper than a disconnect.
func tokenCallback(
	httpClient *http.Client,
	tokenURL string,
	apiKey string,
) func(context.Context, ably.TokenParams) (ably.Tokener, error) {
	return func(ctx context.Context, _ ably.TokenParams) (ably.Tokener, error) {
		reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tokenRequestTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, tokenURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build token request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request token: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("request token: HTTP %d", resp.StatusCode)
		}

		var tokenRequest ably.TokenRequest
		if err := json.NewDecoder(resp.Body).Decode(&tokenRequest); err != nil {
			return nil, fmt.Errorf("decode token request: %w", err)
		}
		if tokenRequest.KeyName == "" || tokenRequest.Nonce == "" || tokenRequest.MAC == "" {
			return nil, fmt.Errorf("decode token request: response is not a signed TokenRequest")
		}
		return tokenRequest, nil
	}
}

func notify(events chan<- Event, event Event) {
	select {
	case events <- event:
	default:
	}
}

// Run maintains the wake subscription and the connector's presence until ctx
// is cancelled. Failure is non-fatal: the daemon keeps its HTTP fallback.
func Run(
	ctx context.Context,
	baseURL string,
	apiKey string,
	apiKeyID string,
	presenceChannel string,
	events chan<- Event,
	log *zap.Logger,
) {
	client, err := ably.NewRealtime(
		ably.WithAuthCallback(tokenCallback(
			tokenHTTPClient,
			strings.TrimRight(baseURL, "/")+"/api/connector/realtime/token",
			apiKey,
		)),
		ably.WithClientID("connector:"+apiKeyID),
	)
	if err != nil {
		log.Warn("realtime client unavailable", zap.Error(err))
		notify(events, Event{Terminal: true})
		return
	}
	defer client.Close()

	offConnected := client.Connection.On(ably.ConnectionEventConnected, func(ably.ConnectionStateChange) {
		notify(events, Event{Connected: true})
		go enterPresence(ctx, client, presenceChannel, log)
	})
	offDisconnected := client.Connection.On(ably.ConnectionEventDisconnected, func(ably.ConnectionStateChange) {
		notify(events, Event{})
	})
	offSuspended := client.Connection.On(ably.ConnectionEventSuspended, func(ably.ConnectionStateChange) {
		notify(events, Event{})
	})
	offFailed := client.Connection.On(ably.ConnectionEventFailed, func(ably.ConnectionStateChange) {
		notify(events, Event{Terminal: true})
	})
	defer offConnected()
	defer offDisconnected()
	defer offSuspended()
	defer offFailed()

	subscribeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	unsubscribe, err := client.Channels.Get(connectorChannel(apiKeyID)).Subscribe(
		subscribeCtx,
		connectorWakeEvent,
		func(*ably.Message) { notify(events, Event{Connected: true, Wake: true}) },
	)
	cancel()
	if err != nil {
		log.Warn("realtime wake subscription unavailable", zap.Error(err))
		notify(events, Event{Terminal: true})
		return
	}
	defer unsubscribe()

	<-ctx.Done()
}

// enterPresence marks the connector online on its owner's presence channel.
// API keys belong to a user, not to a workspace, so the channel does too: the
// dashboard sees the connector whichever workspace is open, including before
// any bank account is linked. An empty channel means Ratio does not publish
// one yet; the wake subscription still works without presence.
func enterPresence(
	ctx context.Context,
	client *ably.Realtime,
	channelName string,
	log *zap.Logger,
) {
	if channelName == "" {
		return
	}
	enterCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err := client.Channels.Get(channelName).Presence.Enter(
		enterCtx,
		map[string]string{"kind": "bank-connector"},
	)
	cancel()
	if err != nil && ctx.Err() == nil {
		log.Warn("connector presence failed", zap.String("channel", channelName), zap.Error(err))
	}
}
