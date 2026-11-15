// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package enablebanking

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestIsAuthorizationErrorUsesTypedStatus(t *testing.T) {
	t.Parallel()

	if !IsAuthorizationError(&APIError{StatusCode: http.StatusUnauthorized}) {
		t.Fatal("401 must require a new authorization")
	}
	if !IsAuthorizationError(&APIError{StatusCode: http.StatusForbidden}) {
		t.Fatal("403 must require a new authorization")
	}
	if IsAuthorizationError(&APIError{StatusCode: http.StatusTooManyRequests}) {
		t.Fatal("429 must not be treated as expired consent")
	}
	if IsAuthorizationError(errors.New("session processing failed")) {
		t.Fatal("untyped error text must not be treated as expired consent")
	}
}

func TestSessionExpiresReadsAccessValidUntil(t *testing.T) {
	var s Session
	if err := json.Unmarshal([]byte(`{"session_id":"x","access":{"valid_until":"2027-02-01T10:00:00Z"}}`), &s); err != nil {
		t.Fatal(err)
	}
	if got := s.Expires().Format("2006-01-02"); got != "2027-02-01" {
		t.Fatalf("expires %s", got)
	}
}
