// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
)

// TransactionBlob matches the browser TransactionBlob interface.
type TransactionBlob struct {
	Description  string  `json:"description"`
	MerchantName string  `json:"merchantName,omitempty"`
	CleanDesc    string  `json:"cleanDescription,omitempty"`
	Category     string  `json:"category,omitempty"`
	Notes        string  `json:"notes,omitempty"`
	IBAN         string  `json:"iban,omitempty"`
	Amount       float64 `json:"amount,omitempty"`
	Currency     string  `json:"currency,omitempty"`
}

// EncryptBlob encrypts a TransactionBlob with AES-256-GCM.
func EncryptBlob(blob TransactionBlob, masterKey []byte) (ciphertextB64, ivB64 string, err error) {
	plaintext, err := json.Marshal(blob)
	if err != nil {
		return "", "", err
	}
	ct, iv, err := encryptGCM(plaintext, masterKey)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(ct), base64.StdEncoding.EncodeToString(iv), nil
}

// EncryptForStorage encrypts plaintext with AES-256-GCM and returns ciphertext+iv in base64.
// Used for storing master key encrypted with local passphrase-derived KEK.
func EncryptForStorage(plaintext, key []byte) (ciphertextB64, ivB64 string, err error) {
	ct, iv, err := encryptGCM(plaintext, key)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(ct), base64.StdEncoding.EncodeToString(iv), nil
}

// DecryptGCM decrypts AES-256-GCM ciphertext.
func DecryptGCM(ciphertext, iv, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, iv, ciphertext, nil)
}

// DecryptSearchKey decrypts encryptedSearchKey (encrypted with masterKey AES-GCM).
// Browser format (wrapSearchKey): base64(IV[12] || AES-GCM-ciphertext)
func DecryptSearchKey(encryptedSearchKeyB64 string, masterKey []byte) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(encryptedSearchKeyB64)
	if err != nil {
		data, err = base64.RawURLEncoding.DecodeString(encryptedSearchKeyB64)
		if err != nil {
			return nil, err
		}
	}
	if len(data) < 13 {
		return nil, errors.New("encrypted search key too short")
	}
	return DecryptGCM(data[12:], data[:12], masterKey)
}

// DecryptWrappedMasterKey decrypts the Ratio-format wrapped master key.
// Format from browser wrapMasterKey(): base64(IV[12] || AES-GCM-ciphertext)
func DecryptWrappedMasterKey(wrappedB64 string, kek []byte) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(wrappedB64)
	if err != nil {
		data, err = base64.RawURLEncoding.DecodeString(wrappedB64)
		if err != nil {
			return nil, err
		}
	}
	if len(data) < 13 {
		return nil, errors.New("wrapped master key too short")
	}
	return DecryptGCM(data[12:], data[:12], kek)
}

// EncryptGCMWithNonce encrypts with a caller-supplied nonce (required for setup: nonce doubles as PBKDF2 salt).
func EncryptGCMWithNonce(plaintext, key, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Seal(nil, nonce, plaintext, nil), nil
}

func encryptGCM(plaintext, key []byte) (ciphertext, iv []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, 12)
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	return ct, nonce, nil
}
