// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package crypto

import (
	"crypto/aes"
	"encoding/base64"
	"errors"
)

// UnwrapKey implements AES-KW (RFC 3394) key unwrapping.
// Replicates: crypto.subtle.unwrapKey("raw", wrappedKey, kek, "AES-KW", "AES-GCM", ...)
func UnwrapKey(wrappedKeyB64 string, kek []byte) ([]byte, error) {
	wrapped, err := base64.StdEncoding.DecodeString(wrappedKeyB64)
	if err != nil {
		wrapped, err = base64.RawURLEncoding.DecodeString(wrappedKeyB64)
		if err != nil {
			return nil, err
		}
	}
	return aesKeyUnwrap(kek, wrapped)
}

// aesKeyUnwrap is RFC 3394 AES Key Unwrap.
func aesKeyUnwrap(kek, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 24 || len(ciphertext)%8 != 0 {
		return nil, errors.New("aeskw: invalid ciphertext length")
	}

	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}

	n := len(ciphertext)/8 - 1
	r := make([][]byte, n)
	for i := range r {
		r[i] = make([]byte, 8)
		copy(r[i], ciphertext[8*(i+1):8*(i+2)])
	}

	a := make([]byte, 8)
	copy(a, ciphertext[:8])

	buf := make([]byte, 16)
	for j := 5; j >= 0; j-- {
		for i := n; i >= 1; i-- {
			t := uint64(n*j + i)
			for k := 0; k < 8; k++ {
				a[7-k] ^= byte(t >> (8 * k))
			}
			copy(buf[:8], a)
			copy(buf[8:], r[i-1])
			block.Decrypt(buf, buf)
			copy(a, buf[:8])
			copy(r[i-1], buf[8:])
		}
	}

	// Verify integrity value (default IV = 0xA6A6A6A6A6A6A6A6)
	iv := [8]byte{0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6}
	for i := 0; i < 8; i++ {
		if a[i] != iv[i] {
			return nil, errors.New("aeskw: integrity check failed — wrong KEK or wrapped key")
		}
	}

	out := make([]byte, n*8)
	for i, ri := range r {
		copy(out[i*8:], ri)
	}
	return out, nil
}
