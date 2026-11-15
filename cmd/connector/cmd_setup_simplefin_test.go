// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"testing"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ratio"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source/simplefinsource"
)

var (
	one  = []simplefinsource.AccountInfo{{ID: "A1", Name: "Checking"}}
	two  = []simplefinsource.AccountInfo{{ID: "A1", Name: "Checking"}, {ID: "A2", Name: "Savings"}}
	acct = func(id, provider string) ratio.RemoteBankAccount {
		return ratio.RemoteBankAccount{ID: id, Provider: provider}
	}
)

func TestASingleSimpleFINAccountMapsByItself(t *testing.T) {
	got, done := mapSimpleFIN(nil, "https://u:p@h/x", one, []ratio.RemoteBankAccount{acct("r1", "simplefin")}, nil)
	if len(done) != 1 || len(got) != 1 || got[0].Account != "r1" || got[0].SFINAccount != "A1" || got[0].AccessURL != "https://u:p@h/x" {
		t.Fatalf("got %+v done %v", got, done)
	}
}

func TestOnlySimpleFINAccountsAreMapped(t *testing.T) {
	got, done := mapSimpleFIN(nil, "u", one, []ratio.RemoteBankAccount{acct("bank", ""), acct("eb", "enablebanking"), acct("folder", "folder")}, nil)
	if len(done) != 0 || len(got) != 1 || got[0].Account != "" {
		t.Fatalf("other providers must be left alone: %+v", got)
	}
}

func TestSeveralAccountsAskWhichOneAndSkipWhatIsNotChosen(t *testing.T) {
	choose := func(ra ratio.RemoteBankAccount) string {
		if ra.ID == "r1" {
			return "A2"
		}
		return "" // skipped
	}
	got, done := mapSimpleFIN(nil, "u", two, []ratio.RemoteBankAccount{acct("r1", "simplefin"), acct("r2", "simplefin")}, choose)
	if len(done) != 1 || got[0].Account != "r1" || got[0].SFINAccount != "A2" {
		t.Fatalf("got %+v done %v", got, done)
	}
}

func TestAChoiceOutsideTheConnectionIsRefused(t *testing.T) {
	got, done := mapSimpleFIN(nil, "u", two, []ratio.RemoteBankAccount{acct("r1", "simplefin")}, func(ratio.RemoteBankAccount) string { return "ghost" })
	if len(done) != 0 || got[0].Account != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestWithNothingToMapTheConnectionIsKeptForLater(t *testing.T) {
	got, done := mapSimpleFIN(nil, "u", one, nil, nil)
	if len(done) != 0 || len(got) != 1 || got[0].AccessURL != "u" || got[0].Account != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestMappingLaterReplacesTheHolderAndKeepsExistingMappings(t *testing.T) {
	existing := []sfinEntry{{Account: "r1", AccessURL: "u", SFINAccount: "A1"}, {AccessURL: "u"}}
	got, done := mapSimpleFIN(existing, "u", two, []ratio.RemoteBankAccount{acct("r1", "simplefin"), acct("r2", "simplefin")},
		func(ratio.RemoteBankAccount) string { return "A2" })
	if len(done) != 1 || done[0] != "r2" || len(got) != 2 {
		t.Fatalf("got %+v done %v", got, done)
	}
	if got[0].Account != "r1" || got[0].SFINAccount != "A1" || got[1].Account != "r2" || got[1].SFINAccount != "A2" {
		t.Fatalf("the earlier mapping must not change: %+v", got)
	}
}

func TestAnotherConnectionsHolderIsNotTouched(t *testing.T) {
	got, _ := mapSimpleFIN([]sfinEntry{{AccessURL: "other"}}, "u", one, []ratio.RemoteBankAccount{acct("r1", "simplefin")}, nil)
	if len(got) != 2 || got[0].AccessURL != "other" {
		t.Fatalf("got %+v", got)
	}
}

func TestEntriesSurviveTheRoundTripThroughTheConfigMap(t *testing.T) {
	m := map[string]any{}
	in := []sfinEntry{{Account: "r1", AccessURL: "u", SFINAccount: "A1"}, {AccessURLEnv: "SFIN", Account: "r2", SFINAccount: "A2"}}
	sfinEntriesInto(m, in)
	// Through YAML the rows come back as []any of map[string]any.
	rows := make([]any, 0)
	for _, r := range m["simplefin"].([]map[string]any) {
		rows = append(rows, r)
	}
	out := sfinEntriesFrom(map[string]any{"simplefin": rows})
	if len(out) != 2 || out[0] != in[0] || out[1] != in[1] {
		t.Fatalf("got %+v", out)
	}
}
