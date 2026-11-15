// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ratio

// PriceInboxItem is one sealed item of the price inbox: the prices downloaded for a ticker, or a problem with it.
// Ratio cannot read it; the user's device opens it, merges the prices and deletes the row.
type PriceInboxItem struct {
	EncryptedBlob       string `json:"encryptedBlob"`
	EncryptionIV        string `json:"encryptionIv"`
	EncryptedSessionKey string `json:"encryptedSessionKey"`
	// DedupHash is an HMAC of the plaintext: the same item sent twice is stored once.
	DedupHash string `json:"dedupHash"`
}

// PushPrices queues sealed price items for a workspace.
func (c *Client) PushPrices(workspaceID string, items []PriceInboxItem) (*PushResult, error) {
	var result PushResult
	body := map[string]any{"workspaceId": workspaceID, "items": items}
	if err := c.do("POST", "/api/connector/price-inbox", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
