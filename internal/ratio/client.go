// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ratio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the Ratio API client (server-to-server).
type Client struct {
	baseURL        string
	connectorToken string
	http           *http.Client
}

func NewClient(baseURL, connectorToken string) *Client {
	return &Client{
		baseURL:        baseURL,
		connectorToken: connectorToken,
		http:           &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, path string, body any, out any) error {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		buf = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.connectorToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Ratio %s %s returned HTTP %d", method, path, resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
