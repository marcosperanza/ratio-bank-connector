// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ably/ably-go/ably"
)

func signedTokenRequest() ably.TokenRequest {
	return ably.TokenRequest{
		TokenParams: ably.TokenParams{
			TTL:        3_600_000,
			Capability: `{"ratio:connector:key-1":["subscribe","presence"]}`,
			ClientID:   "connector:key-1",
			Timestamp:  1_789_417_223_000,
		},
		KeyName: "app.key",
		Nonce:   "0123456789abcdef",
		MAC:     "signature",
	}
}

func tokenServer(t *testing.T, status int, body any) (*httptest.Server, *string) {
	t.Helper()
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	return server, &authorization
}

// The regression: ably-go renews tokens with a context it has already
// cancelled. The callback must still reach Ratio and return the token.
func TestTokenCallbackSurvivesAnAlreadyCancelledContext(t *testing.T) {
	server, authorization := tokenServer(t, http.StatusOK, signedTokenRequest())
	callback := tokenCallback(server.Client(), server.URL, "secret-key")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tokener, err := callback(ctx, ably.TokenParams{})
	if err != nil {
		t.Fatalf("expected a token despite the cancelled context, got %v", err)
	}
	got, ok := tokener.(ably.TokenRequest)
	if !ok {
		t.Fatalf("expected ably.TokenRequest, got %T", tokener)
	}
	if got.MAC != "signature" || got.ClientID != "connector:key-1" || got.TTL != 3_600_000 {
		t.Fatalf("token request not decoded faithfully: %+v", got)
	}
	if *authorization != "Bearer secret-key" {
		t.Fatalf("expected bearer authorization, got %q", *authorization)
	}
}

func TestTokenCallbackReportsARejectedKey(t *testing.T) {
	server, _ := tokenServer(t, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	callback := tokenCallback(server.Client(), server.URL, "revoked-key")

	_, err := callback(context.Background(), ably.TokenParams{})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("expected an HTTP 401 error, got %v", err)
	}
}

func TestTokenCallbackRefusesAnUnsignedResponse(t *testing.T) {
	// Realtime disabled on Ratio answers {"enabled": false}; that must not be
	// mistaken for a token.
	server, _ := tokenServer(t, http.StatusOK, map[string]bool{"enabled": false})
	callback := tokenCallback(server.Client(), server.URL, "secret-key")

	_, err := callback(context.Background(), ably.TokenParams{})
	if err == nil || !strings.Contains(err.Error(), "not a signed TokenRequest") {
		t.Fatalf("expected an unsigned-response error, got %v", err)
	}
}
