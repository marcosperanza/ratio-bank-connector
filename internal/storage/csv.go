// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/enablebanking"
)

var csvHeaders = []string{
	"id", "booking_date", "value_date", "description", "amount", "currency",
	"credit_debit_indicator", "status",
	"creditor_name", "creditor_iban",
	"debtor_name", "debtor_iban",
	"merchant_category_code", "reference_number", "note",
	"balance_after", "bank_tx_id",
}

// AppendTransactions appends new transactions to CSV and raw JSONL files.
// Each file has independent dedup — JSONL is backfilled even if CSV already has the entry.
// rawTxs must be same length and order as txs.
func AppendTransactions(csvDir, accountID string, txs []enablebanking.Transaction, rawTxs []json.RawMessage) (int, error) {
	return AppendTransactionsWithOptions(csvDir, accountID, txs, rawTxs, true)
}

// AppendTransactionsWithOptions optionally omits raw provider JSON while
// retaining the normalized CSV archive.
func AppendTransactionsWithOptions(csvDir, accountID string, txs []enablebanking.Transaction, rawTxs []json.RawMessage, includeRaw bool) (int, error) {
	dir := filepath.Join(csvDir, accountID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return 0, err
	}

	csvPath := filepath.Join(dir, "transactions.csv")
	jsonlPath := filepath.Join(dir, "transactions_raw.jsonl")

	csvIsNew := !fileExists(csvPath)
	existingCSV := loadExistingIDs(csvPath)
	existingJSONL := map[string]bool{}
	if includeRaw {
		existingJSONL = loadExistingJSONLIDs(jsonlPath)
	}

	csvFile, err := os.OpenFile(csvPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	defer csvFile.Close()

	var jsonlFile *os.File
	if includeRaw {
		jsonlFile, err = os.OpenFile(jsonlPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return 0, err
		}
		defer jsonlFile.Close()
	}

	w := csv.NewWriter(csvFile)
	defer w.Flush()

	if csvIsNew {
		if err := w.Write(csvHeaders); err != nil {
			return 0, err
		}
	}

	appended := 0
	for i, tx := range txs {
		id := txID(tx)

		// CSV write
		if !existingCSV[id] {
			amount, _ := strconv.ParseFloat(tx.TransactionAmount.Amount, 64)
			if tx.CreditDebitIndicator == "DBIT" {
				amount = -amount
			}
			record := []string{
				id,
				tx.BookingDate,
				tx.ValueDate,
				tx.Description(),
				strconv.FormatFloat(amount, 'f', -1, 64),
				tx.TransactionAmount.Currency,
				tx.CreditDebitIndicator,
				tx.Status,
				tx.CreditorName(),
				tx.CreditorIBAN(),
				tx.DebtorName(),
				tx.DebtorIBAN(),
				tx.MCC(),
				tx.RefNumber(),
				tx.NoteStr(),
				tx.BalanceAfter(),
				tx.TransactionID,
			}
			if err := w.Write(record); err != nil {
				return appended, err
			}
			appended++
		}

		// JSONL write — independent dedup, backfills entries missing from jsonl
		if includeRaw && i < len(rawTxs) && !existingJSONL[id] {
			line := append([]byte(nil), rawTxs[i]...)
			line = append(line, '\n')
			if _, err := jsonlFile.Write(line); err != nil {
				return appended, err
			}
		}
	}
	return appended, nil
}

func txID(tx enablebanking.Transaction) string {
	if tx.EntryReference != "" {
		return tx.EntryReference
	}
	if tx.TransactionID != "" {
		return tx.TransactionID
	}
	return fmt.Sprintf("%s_%s_%s", tx.BookingDate, tx.TransactionAmount.Amount, tx.Description())
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func loadExistingIDs(path string) map[string]bool {
	f, err := os.Open(path)
	if err != nil {
		return map[string]bool{}
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return map[string]bool{}
	}
	ids := make(map[string]bool, len(records))
	for _, row := range records[1:] {
		if len(row) > 0 {
			ids[row[0]] = true
		}
	}
	return ids
}

// loadExistingJSONLIDs reads entry_reference / transaction_id from each JSONL line.
func loadExistingJSONLIDs(path string) map[string]bool {
	f, err := os.Open(path)
	if err != nil {
		return map[string]bool{}
	}
	defer f.Close()

	ids := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var obj struct {
			EntryReference string `json:"entry_reference"`
			TransactionID  string `json:"transaction_id"`
		}
		if json.Unmarshal(scanner.Bytes(), &obj) == nil {
			if obj.EntryReference != "" {
				ids[obj.EntryReference] = true
			} else if obj.TransactionID != "" {
				ids[obj.TransactionID] = true
			}
		}
	}
	return ids
}
