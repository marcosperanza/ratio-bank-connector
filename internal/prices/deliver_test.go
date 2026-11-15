// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package prices

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/crypto"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
)

type fakeEntries map[string][]Entry

func (f fakeEntries) Entries() map[string][]Entry { return f }

type fakeSender struct {
	calls map[string][]ratio.PriceInboxItem
	err   error
}

func (f *fakeSender) PushPrices(w string, items []ratio.PriceInboxItem) (*ratio.PushResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.calls == nil {
		f.calls = map[string][]ratio.PriceInboxItem{}
	}
	f.calls[w] = append(f.calls[w], items...)
	return &ratio.PushResult{Queued: len(items)}, nil
}

func jwkOf(pub *rsa.PublicKey) string {
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	b, _ := json.Marshal(map[string]string{"kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()), "e": e})
	return string(b)
}

// open does what the user's device does with an item.
func open(t *testing.T, priv *rsa.PrivateKey, item ratio.PriceInboxItem) map[string]any {
	t.Helper()
	wrapped, _ := base64.StdEncoding.DecodeString(item.EncryptedSessionKey)
	key, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, wrapped, nil)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := base64.StdEncoding.DecodeString(item.EncryptedBlob)
	iv, _ := base64.StdEncoding.DecodeString(item.EncryptionIV)
	plain, err := crypto.DecryptGCM(blob, iv, key)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(plain, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func setup(t *testing.T, entries fakeEntries) (*Deliverer, *fakeSender, *rsa.PrivateKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	state, err := LoadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{}
	return &Deliverer{
		Entries: entries, Store: Store{Dir: dir}, State: state, Sender: sender,
		PublicKey: func() (string, error) { return jwkOf(&priv.PublicKey), nil }, HMACKey: "api-key",
	}, sender, priv
}

func TestDeliversNewClosesSealedAndOnlyOnce(t *testing.T) {
	d, sender, priv := setup(t, fakeEntries{"w1": {{Ticker: "LEMA.DE", ISIN: "IE00B4L5Y983", Currency: "EUR"}}})
	if _, err := d.Store.Merge("LEMA.DE", []Bar{{Date: "2026-11-12", Close: "10.5"}, {Date: "2026-11-13", Close: "10.7"}}); err != nil {
		t.Fatal(err)
	}
	run := []Outcome{{Ticker: "LEMA.DE", Provider: "yahoo", Added: 2}}
	d.Deliver(context.Background(), run)

	items := sender.calls["w1"]
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	got := open(t, priv, items[0])
	if got["kind"] != "prices" || got["ticker"] != "LEMA.DE" || got["isin"] != "IE00B4L5Y983" || got["currency"] != "EUR" || got["source"] != "yahoo" {
		t.Fatalf("payload %v", got)
	}
	if pts := got["points"].([]any); len(pts) != 2 || pts[1].([]any)[0] != "2026-11-13" || pts[1].([]any)[1] != 10.7 {
		t.Fatalf("points %v", got["points"])
	}
	if len(items[0].DedupHash) != 64 {
		t.Fatalf("dedup hash %q", items[0].DedupHash)
	}

	// Nothing new: nothing sent. A new close: only that one.
	d.Deliver(context.Background(), run)
	if len(sender.calls["w1"]) != 1 {
		t.Fatal("an unchanged ticker must send nothing")
	}
	if _, err := d.Store.Merge("LEMA.DE", []Bar{{Date: "2026-11-14", Close: "10.9"}}); err != nil {
		t.Fatal(err)
	}
	d.Deliver(context.Background(), run)
	if len(sender.calls["w1"]) != 2 {
		t.Fatal("expected one more item")
	}
	if pts := open(t, priv, sender.calls["w1"][1])["points"].([]any); len(pts) != 1 {
		t.Fatalf("points %v", pts)
	}
}

func TestAFailedPushIsRetriedByTheNextRun(t *testing.T) {
	d, sender, _ := setup(t, fakeEntries{"w1": {{Ticker: "A", ISIN: "X", Currency: "EUR"}}})
	if _, err := d.Store.Merge("A", []Bar{{Date: "2026-11-12", Close: "1"}}); err != nil {
		t.Fatal(err)
	}
	sender.err = errors.New("offline")
	run := []Outcome{{Ticker: "A", Provider: "yahoo"}}
	d.Deliver(context.Background(), run)
	sender.err = nil
	d.Deliver(context.Background(), run)
	if len(sender.calls["w1"]) != 1 {
		t.Fatalf("calls %d", len(sender.calls["w1"]))
	}
}

func TestProblemsAreSealedWithTheirCode(t *testing.T) {
	d, sender, priv := setup(t, fakeEntries{"w1": {
		{Ticker: "LEMA.MI", ISIN: "I", Currency: "EUR"},
		{Ticker: "NOISIN", Currency: "EUR"},
		{Ticker: "NOCUR", ISIN: "I"},
		{Ticker: "GOOD", ISIN: "I", Currency: "EUR"},
	}})
	if _, err := d.Store.Merge("NOISIN", []Bar{{Date: "2026-11-12", Close: "1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Store.Merge("NOCUR", []Bar{{Date: "2026-11-12", Close: "1"}}); err != nil {
		t.Fatal(err)
	}
	d.Deliver(context.Background(), []Outcome{
		{Ticker: "LEMA.MI", Err: ErrNotFound},
		{Ticker: "NOISIN", Provider: "yahoo"},
		{Ticker: "NOCUR", Provider: "yahoo"},
		{Ticker: "GOOD", Provider: "yahoo"}, // up to date and empty: says nothing
	})
	var codes []string
	for _, item := range sender.calls["w1"] {
		p := open(t, priv, item)
		if p["kind"] != "problem" {
			t.Fatalf("unexpected %v", p)
		}
		codes = append(codes, p["ticker"].(string)+":"+p["code"].(string))
	}
	want := []string{"LEMA.MI:NOT_FOUND", "NOISIN:NO_ISIN", "NOCUR:NO_CURRENCY"}
	if len(codes) != 3 || codes[0] != want[0] || codes[1] != want[1] || codes[2] != want[2] {
		t.Fatalf("codes %v", codes)
	}
}

func TestNothingToSayMeansNoCall(t *testing.T) {
	d, sender, _ := setup(t, fakeEntries{"w1": {{Ticker: "A", ISIN: "X", Currency: "EUR"}}})
	d.Deliver(context.Background(), []Outcome{{Ticker: "A", Provider: "yahoo"}})
	if len(sender.calls) != 0 {
		t.Fatal("a quiet run must not call Ratio")
	}
}

func TestLongHistoryGoesOutInChunks(t *testing.T) {
	d, sender, priv := setup(t, fakeEntries{"w1": {{Ticker: "A", ISIN: "X", Currency: "EUR"}}})
	start := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	var bars []Bar
	for i := 0; i < maxPointsPerItem+10; i++ {
		bars = append(bars, Bar{Date: start.AddDate(0, 0, i).Format("2006-01-02"), Close: "1"})
	}
	if _, err := d.Store.Merge("A", bars); err != nil {
		t.Fatal(err)
	}
	d.Deliver(context.Background(), []Outcome{{Ticker: "A", Provider: "yahoo"}})
	items := sender.calls["w1"]
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	if n := len(open(t, priv, items[0])["points"].([]any)); n != maxPointsPerItem {
		t.Fatalf("first chunk has %d points", n)
	}
	if n := len(open(t, priv, items[1])["points"].([]any)); n != 10 {
		t.Fatalf("second chunk has %d points", n)
	}
}
