// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package foldersource

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/source"
)

var (
	ofxStart    = regexp.MustCompile(`(?i)<STMTTRN>`)
	ofxEnd      = regexp.MustCompile(`(?i)</STMTTRN>|</BANKTRANLIST>`)
	ofxCurrency = regexp.MustCompile(`(?i)<CURDEF>\s*([A-Za-z]{3})`)
)

// parseOFX reads OFX 1.x (SGML, tags often left open) and 2.x (XML) alike: it
// only needs the statement lines and the account currency.
func parseOFX(data []byte, account string) ([]source.Transaction, error) {
	text := decode(data, "")
	currency := "EUR"
	if m := ofxCurrency.FindStringSubmatch(text); m != nil {
		currency = strings.ToUpper(m[1])
	}

	seen := map[string]int{}
	var out []source.Transaction
	// Split on the opening tag: in OFX 1.x the closing one is optional.
	chunks := ofxStart.Split(text, -1)
	for _, chunk := range chunks[1:] {
		block := chunk
		if loc := ofxEnd.FindStringIndex(chunk); loc != nil {
			block = chunk[:loc[0]]
		}
		posted := ofxTag(block, "DTPOSTED")
		if len(posted) < 8 {
			return nil, fmt.Errorf("statement line without a valid DTPOSTED")
		}
		date := posted[0:4] + "-" + posted[4:6] + "-" + posted[6:8]
		amount, err := parseAmount(ofxTag(block, "TRNAMT"), ",", "")
		if err != nil {
			return nil, err
		}
		desc := strings.TrimSpace(ofxTag(block, "NAME") + " " + ofxTag(block, "MEMO"))

		tx := source.Transaction{
			BookingDate:  date,
			Amount:       amount,
			Currency:     currency,
			Description:  desc,
			RawText:      strings.TrimSpace(block),
			Counterparty: ofxTag(block, "NAME"),
		}
		if fit := ofxTag(block, "FITID"); fit != "" {
			tx.ID = "ofx:" + account + ":" + fit
		} else {
			key := fmt.Sprintf("%s|%.2f|%s", date, amount, strings.ToLower(strings.Join(strings.Fields(desc), " ")))
			tx.ID = source.StableID("ofx", account, date, amount, currency, desc, seen[key])
			seen[key]++
		}
		out = append(out, tx)
	}
	return out, nil
}

// ofxTag returns the text after <TAG>, up to the next tag or line end.
func ofxTag(block, tag string) string {
	re := regexp.MustCompile(`(?is)<` + tag + `>\s*([^<\r\n]*)`)
	if m := re.FindStringSubmatch(block); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}
