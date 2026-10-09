// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ratio

// PushInbox sends encrypted transactions to the Ratio connector inbox.
func (c *Client) PushInbox(workspaceID string, items []InboxItem) (*PushResult, error) {
	body := map[string]any{
		"workspaceId": workspaceID,
		"items":       items,
	}
	var result PushResult
	if err := c.do("POST", "/api/connector/inbox", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
