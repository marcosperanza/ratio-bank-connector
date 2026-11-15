// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// SealedEnvelope is a document a Ratio browser sealed to this connector's
// key (Ratio to connector). Layout and AAD must match lib/connector-sealed.ts
// in the web repo; a shared test vector pins both.
type SealedEnvelope struct {
	V              int    `json:"v"`
	Kind           string `json:"kind"`
	WorkspaceID    string `json:"workspaceId"`
	Seq            int    `json:"seq"`
	KeyFingerprint string `json:"keyFingerprint"`
	WrappedKey     string `json:"wrappedKey"`
	IV             string `json:"iv"`
	Ciphertext     string `json:"ciphertext"`
}

const sealedVersion = 1

// SealingKeyBits matches the key size Ratio uses for user keys.
const SealingKeyBits = 4096

func GenerateSealingKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, SealingKeyBits)
}

// SealingPublicKeySPKI is the public key in the form Ratio stores: SPKI DER,
// base64.
func SealingPublicKeySPKI(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

// SealingFingerprint is the SHA-256 of the SPKI DER bytes, lowercase hex:
// what the browser shows and the user compares.
func SealingFingerprint(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:]), nil
}

// FormatFingerprint groups the fingerprint by four characters.
func FormatFingerprint(hexFingerprint string) string {
	out := make([]byte, 0, len(hexFingerprint)+len(hexFingerprint)/4)
	for i := 0; i < len(hexFingerprint); i++ {
		if i > 0 && i%4 == 0 {
			out = append(out, ' ')
		}
		out = append(out, hexFingerprint[i])
	}
	return string(out)
}

func sealedAAD(kind, workspaceID string, seq int, fingerprint string) []byte {
	return []byte(fmt.Sprintf("ratio-sealed/v%d|%s|%s|%d|%s", sealedVersion, kind, workspaceID, seq, fingerprint))
}

// OpenSealed decrypts an envelope addressed to priv. It refuses envelopes
// sealed to another key, and any whose kind, workspace, sequence or key
// fingerprint were altered (they are bound as AAD).
func OpenSealed(priv *rsa.PrivateKey, env SealedEnvelope) ([]byte, error) {
	if env.V != sealedVersion {
		return nil, fmt.Errorf("unsupported sealed envelope version %d", env.V)
	}
	own, err := SealingFingerprint(&priv.PublicKey)
	if err != nil {
		return nil, err
	}
	if env.KeyFingerprint != own {
		return nil, errors.New("envelope sealed to a different key")
	}
	wrapped, err := base64.StdEncoding.DecodeString(env.WrappedKey)
	if err != nil {
		return nil, err
	}
	iv, err := base64.StdEncoding.DecodeString(env.IV)
	if err != nil {
		return nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, err
	}
	aesKey, err := rsa.DecryptOAEP(sha256.New(), nil, priv, wrapped, nil)
	if err != nil {
		return nil, err
	}
	return DecryptGCMWithAAD(ct, iv, aesKey, sealedAAD(env.Kind, env.WorkspaceID, env.Seq, env.KeyFingerprint))
}
