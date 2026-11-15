// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package crypto

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

type sealedVector struct {
	PrivateKeyPKCS8 string          `json:"privateKeyPkcs8"`
	Envelope        SealedEnvelope  `json:"envelope"`
	Payload         json.RawMessage `json:"payload"`
}

func loadVector(t *testing.T) (*rsa.PrivateKey, sealedVector) {
	t.Helper()
	data, err := os.ReadFile("testdata/sealed-vector.json")
	if err != nil {
		t.Fatal(err)
	}
	var v sealedVector
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	der, _ := base64.StdEncoding.DecodeString(v.PrivateKeyPKCS8)
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.(*rsa.PrivateKey), v
}

// The vector is produced by the web's sealForConnector, so this is the
// cross-implementation check for layout, OAEP parameters and AAD.
func TestOpenSealedFromWebVector(t *testing.T) {
	key, v := loadVector(t)
	got, err := OpenSealed(key, v.Envelope)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	_ = json.Unmarshal(got, &a)
	_ = json.Unmarshal(v.Payload, &b)
	ga, _ := json.Marshal(a)
	gb, _ := json.Marshal(b)
	if string(ga) != string(gb) {
		t.Fatalf("payload %s != %s", ga, gb)
	}
	fp, _ := SealingFingerprint(&key.PublicKey)
	if fp != v.Envelope.KeyFingerprint {
		t.Fatal("fingerprint differs between web and Go")
	}
}

func TestOpenSealedRejectsTampering(t *testing.T) {
	key, v := loadVector(t)
	for name, mutate := range map[string]func(*SealedEnvelope){
		"seq":       func(e *SealedEnvelope) { e.Seq++ },
		"kind":      func(e *SealedEnvelope) { e.Kind = "other" },
		"workspace": func(e *SealedEnvelope) { e.WorkspaceID = "ws-other" },
		"key":       func(e *SealedEnvelope) { e.KeyFingerprint = "00" + e.KeyFingerprint[2:] },
		"version":   func(e *SealedEnvelope) { e.V = 2 },
	} {
		e := v.Envelope
		mutate(&e)
		if _, err := OpenSealed(key, e); err == nil {
			t.Errorf("%s: tampered envelope opened", name)
		}
	}
}

func TestFormatFingerprint(t *testing.T) {
	if got := FormatFingerprint("abcdef0123"); got != "abcd ef01 23" {
		t.Fatal(got)
	}
}
