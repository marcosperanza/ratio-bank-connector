// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
)

// jwkRSAPublicKey is the minimal JWK structure for an RSA public key.
type jwkRSAPublicKey struct {
	Kty string `json:"kty"`
	N   string `json:"n"` // Base64url-encoded modulus
	E   string `json:"e"` // Base64url-encoded exponent
}

// ParseRSAPublicKeyJWK parses a JWK JSON string into an rsa.PublicKey.
func ParseRSAPublicKeyJWK(jwkString string) (*rsa.PublicKey, error) {
	var jwk jwkRSAPublicKey
	if err := json.Unmarshal([]byte(jwkString), &jwk); err != nil {
		return nil, err
	}
	if jwk.Kty != "RSA" {
		return nil, errors.New("JWK is not RSA key type")
	}

	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, err
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

// EncryptSessionKeyRSA wraps a 256-bit session key with RSA-OAEP (SHA-256).
// Returns base64 standard encoded ciphertext.
func EncryptSessionKeyRSA(pub *rsa.PublicKey, sessionKey []byte) (string, error) {
	ct, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, sessionKey, nil)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// EncryptBlobHybrid encrypts a TransactionBlob using RSA+AES hybrid encryption.
// Returns: encryptedSessionKey (RSA-OAEP), encryptedBlob (AES-GCM), encryptionIv (AES-GCM IV).
// The connector sends all three to the Ratio inbox; the web app decrypts with the RSA private key.
func EncryptBlobHybrid(publicKeyJwk string, blob TransactionBlob) (encryptedSessionKey, encryptedBlob, encryptionIv string, err error) {
	pub, err := ParseRSAPublicKeyJWK(publicKeyJwk)
	if err != nil {
		return "", "", "", err
	}

	// Generate random 256-bit AES session key
	sessionKey := make([]byte, 32)
	if _, err = rand.Read(sessionKey); err != nil {
		return "", "", "", err
	}

	// Encrypt session key with RSA public key
	encryptedSessionKey, err = EncryptSessionKeyRSA(pub, sessionKey)
	if err != nil {
		return "", "", "", err
	}

	// Encrypt blob with AES-256-GCM using session key
	encryptedBlob, encryptionIv, err = EncryptBlob(blob, sessionKey)
	if err != nil {
		return "", "", "", err
	}

	return encryptedSessionKey, encryptedBlob, encryptionIv, nil
}
