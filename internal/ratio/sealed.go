// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ratio

import "net/url"

// SealedDocument is a row from GET /api/connector/sealed. Envelope is the JSON
// the browser sealed; the server never sees what is inside.
type SealedDocument struct {
	WorkspaceID    string `json:"workspaceId"`
	Kind           string `json:"kind"`
	Seq            int    `json:"seq"`
	KeyFingerprint string `json:"keyFingerprint"`
	Envelope       string `json:"envelope"`
}

// RegisterSealingKey publishes the public half (SPKI DER, base64) of the
// connector's sealing key.
func (c *Client) RegisterSealingKey(spkiBase64 string) error {
	return c.do("PUT", "/api/connector/sealing-key", map[string]string{"publicKey": spkiBase64}, nil)
}

// SealedDocuments lists the latest documents sealed for this connector key.
func (c *Client) SealedDocuments(kind string) ([]SealedDocument, error) {
	path := "/api/connector/sealed"
	if kind != "" {
		path += "?kind=" + url.QueryEscape(kind)
	}
	var out struct {
		Documents []SealedDocument `json:"documents"`
	}
	if err := c.do("GET", path, nil, &out); err != nil {
		return nil, err
	}
	return out.Documents, nil
}
