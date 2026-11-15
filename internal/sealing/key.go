// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

// Package sealing holds the keypair this connector generated for documents
// Ratio seals to it. The private half never leaves the machine.
package sealing

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/crypto"
)

// LoadOrCreate returns the stored key, generating and saving it (0600) on
// first use. created tells the caller a new fingerprint needs approving.
func LoadOrCreate(path string) (key *rsa.PrivateKey, created bool, err error) {
	data, err := os.ReadFile(path)
	if err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, false, errors.New("sealing key file is not PEM")
		}
		parsed, perr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if perr != nil {
			return nil, false, perr
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, false, errors.New("sealing key is not RSA")
		}
		return rsaKey, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	key, err = crypto.GenerateSealingKey()
	if err != nil {
		return nil, false, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		return nil, false, err
	}
	return key, true, nil
}

// Load reads an existing sealing key and never creates one: false when there is none or it cannot be read.
func Load(path string) (*rsa.PrivateKey, bool) {
	if _, err := os.Stat(path); err != nil {
		return nil, false
	}
	key, _, err := LoadOrCreate(path)
	return key, err == nil
}
