// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package prices

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/crypto"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
)

type vector struct {
	PrivateKeyPKCS8 string                `json:"privateKeyPkcs8"`
	Envelope        crypto.SealedEnvelope `json:"envelope"`
}

type fakeFetcher struct {
	docs []ratio.SealedDocument
	err  error
}

func (f *fakeFetcher) SealedDocuments(string) ([]ratio.SealedDocument, error) { return f.docs, f.err }

func loadVector(t *testing.T) (*rsa.PrivateKey, vector) {
	t.Helper()
	data, err := os.ReadFile("../crypto/testdata/sealed-vector.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vector
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	der, _ := base64.StdEncoding.DecodeString(v.PrivateKeyPKCS8)
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		t.Fatal(err)
	}
	return k.(*rsa.PrivateKey), v
}

func rowFor(env crypto.SealedEnvelope) ratio.SealedDocument {
	raw, _ := json.Marshal(env)
	return ratio.SealedDocument{WorkspaceID: env.WorkspaceID, Kind: env.Kind, Seq: env.Seq, KeyFingerprint: env.KeyFingerprint, Envelope: string(raw)}
}

func TestSealedSourceOpensAndKeepsLastGoodList(t *testing.T) {
	key, v := loadVector(t)
	f := &fakeFetcher{docs: []ratio.SealedDocument{rowFor(v.Envelope)}}
	src := &SealedSource{Fetcher: f, Key: key, CachePath: filepath.Join(t.TempDir(), "watchlist.json")}

	got, err := src.Tickers(context.Background())
	if err != nil || strings.Join(got, ",") != "AAPL,VWCE.DE" {
		t.Fatalf("%v %v", got, err)
	}

	// Ratio unreachable: the saved list stays in force.
	f.docs, f.err = nil, errors.New("offline")
	got, _ = src.Tickers(context.Background())
	if strings.Join(got, ",") != "AAPL,VWCE.DE" {
		t.Fatalf("lost the list offline: %v", got)
	}

	// An older sequence (replayed envelope) is ignored.
	old := v.Envelope
	old.Seq = 3
	f.docs, f.err = []ratio.SealedDocument{rowFor(old)}, nil
	got, _ = src.Tickers(context.Background())
	if strings.Join(got, ",") != "AAPL,VWCE.DE" {
		t.Fatalf("replay changed the list: %v", got)
	}
}

func TestSealedSourceIgnoresRowThatDisagreesWithEnvelope(t *testing.T) {
	key, v := loadVector(t)
	row := rowFor(v.Envelope)
	row.WorkspaceID = "another-workspace" // the server claims a different workspace
	src := &SealedSource{Fetcher: &fakeFetcher{docs: []ratio.SealedDocument{row}}, Key: key, CachePath: filepath.Join(t.TempDir(), "w.json")}
	got, _ := src.Tickers(context.Background())
	if len(got) != 0 {
		t.Fatalf("accepted a mismatched row: %v", got)
	}
}

func TestUnionSourceMergesAndToleratesOneFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "t.txt")
	_ = os.WriteFile(file, []byte("AAPL\nMSFT\n"), 0o600)
	good := FileSource{Path: file}
	bad := FileSource{Path: filepath.Join(t.TempDir(), "missing.txt")}
	got, err := UnionSource{good, bad}.Tickers(context.Background())
	if err != nil || strings.Join(got, ",") != "AAPL,MSFT" {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := (UnionSource{bad}).Tickers(context.Background()); err == nil {
		t.Fatal("expected an error when every source fails")
	}
}
