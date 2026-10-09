// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package crypto

import (
	"crypto/sha256"
	"encoding/base64"

	"golang.org/x/crypto/pbkdf2"
)

// DeriveKEK replicates the browser PBKDF2 derivation:
// PBKDF2-SHA256, 310000 iterations, 32-byte output.
func DeriveKEK(password, saltB64 string) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		// try raw base64url
		salt, err = base64.RawURLEncoding.DecodeString(saltB64)
		if err != nil {
			return nil, err
		}
	}
	key := pbkdf2.Key([]byte(password), salt, 600000, 32, sha256.New)
	return key, nil
}
