// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// MerchantHash replicates the browser generateMerchantHash:
// HMAC-SHA256(value, searchKey) → hex string.
func MerchantHash(value string, searchKey []byte) string {
	mac := hmac.New(sha256.New, searchKey)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

// DescriptionHash is an alias for MerchantHash used for description blind indexing.
func DescriptionHash(value string, searchKey []byte) string {
	return MerchantHash(value, searchKey)
}
