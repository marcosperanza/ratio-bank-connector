// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package enablebanking

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const baseURL = "https://api.enablebanking.com"

// APIError exposes the HTTP status needed for retry and consent decisions
// without propagating a provider response body into logs or Ratio status data.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	// Body is the provider's explanation, truncated. It is not part of Error():
	// that text reaches logs and Ratio. Local screens (settings page, terminal)
	// may show it through Detail.
	Body string
}

// Detail returns the provider's own explanation of a failed call, or "".
func Detail(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return strings.TrimSpace(apiErr.Body)
	}
	return ""
}

func (e *APIError) Error() string {
	return fmt.Sprintf("enable banking %s %s returned HTTP %d", e.Method, e.Path, e.StatusCode)
}

// IsAuthorizationError reports whether the provider rejected or expired the
// current authorization.
func IsAuthorizationError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden)
}

// Client is the Enable Banking API client.
type Client struct {
	appID      string
	privateKey *rsa.PrivateKey
	http       *http.Client
}

// NewClient creates a client from appID and PEM key file path.
func NewClient(appID, privateKeyPath, privateKeyB64 string) (*Client, error) {
	var pemData []byte
	var err error

	if privateKeyPath != "" {
		pemData, err = os.ReadFile(privateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read private key: %w", err)
		}
	} else if privateKeyB64 != "" {
		pemData, err = base64.StdEncoding.DecodeString(privateKeyB64)
		if err != nil {
			return nil, fmt.Errorf("decode private key b64: %w", err)
		}
	} else {
		return nil, fmt.Errorf("no private key provided")
	}

	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// try PKCS1
		rsaKey, err2 := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("parse private key: %w (pkcs8: %v)", err2, err)
		}
		key = rsaKey
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}

	return &Client{
		appID:      appID,
		privateKey: rsaKey,
		http:       &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// signedJWT creates a minimal JWT signed with RS256 for Enable Banking auth.
func (c *Client) signedJWT() (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"` + c.appID + `","typ":"JWT"}`))
	now := time.Now().Unix()
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		fmt.Sprintf(`{"iss":"enablebanking.com","aud":"api.enablebanking.com","iat":%d,"exp":%d}`, now, now+3600),
	))
	msg := header + "." + payload
	hash := sha256.Sum256([]byte(msg))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return msg + "." + base64.RawURLEncoding.EncodeToString(sig), nil
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

	req, err := http.NewRequest(method, baseURL+path, buf)
	if err != nil {
		return err
	}

	jwt, err := c.signedJWT()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		debugProviderError(method, path, resp.StatusCode, data)
		body := data
		if len(body) > 500 {
			body = body[:500]
		}
		return &APIError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: string(body)}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode Enable Banking response: %w", err)
		}
	}
	return nil
}

// debugProviderError prints a provider error body to the local stderr when
// RATIO_CONNECTOR_DEBUG_PROVIDER_ERRORS=1. It is off by default and never
// reaches logs or Ratio status data, which stay free of provider bodies.
func debugProviderError(method, path string, status int, body []byte) {
	if os.Getenv("RATIO_CONNECTOR_DEBUG_PROVIDER_ERRORS") != "1" {
		return
	}
	if len(body) > 2048 {
		body = body[:2048]
	}
	fmt.Fprintf(os.Stderr, "[debug] enable banking %s %s -> HTTP %d: %s\n", method, path, status, body)
}

// doDouble reads the response body once and unmarshals into two targets.
func (c *Client) doDouble(method, path string, out1, out2 any) error {
	jwt, err := c.signedJWT()
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return &APIError{Method: method, Path: path, StatusCode: resp.StatusCode}
	}
	if err := json.Unmarshal(data, out1); err != nil {
		return fmt.Errorf("decode Enable Banking response: %w", err)
	}
	if err := json.Unmarshal(data, out2); err != nil {
		return fmt.Errorf("decode raw Enable Banking response: %w", err)
	}
	return nil
}

// GetAccounts returns accounts for a session.
func (c *Client) GetAccounts(sessionID string) ([]Account, error) {
	var r AccountsResponse
	err := c.do("GET", "/sessions/"+sessionID+"/accounts", nil, &r)
	return r.Accounts, err
}

// GetTransactions fetches transactions for an account, paginating automatically.
// Returns parsed transactions and raw JSON objects (for verbatim storage).
func (c *Client) GetTransactions(accountID, dateFrom string) ([]Transaction, []json.RawMessage, error) {
	var allTx []Transaction
	var allRaw []json.RawMessage
	contKey := ""
	for {
		path := "/accounts/" + accountID + "/transactions?date_from=" + url.QueryEscape(dateFrom)
		if contKey != "" {
			path += "&continuation_key=" + url.QueryEscape(contKey)
		}
		var parsed TransactionsResponse
		var raw TransactionsResponseRaw
		if err := c.doDouble("GET", path, &parsed, &raw); err != nil {
			return nil, nil, err
		}
		allTx = append(allTx, parsed.Transactions...)
		allRaw = append(allRaw, raw.Transactions...)
		if parsed.ContinuationKey == "" {
			break
		}
		contKey = parsed.ContinuationKey
	}
	return allTx, allRaw, nil
}

// InitAuth starts the OAuth flow and returns the redirect URL.
// bankName is the ASPSP name as returned by ListASPSPs (e.g. "BPER Banca").
func (c *Client) InitAuth(bankName, country, redirectURL, state string) (string, error) {
	req := AuthRequest{
		Access:      AuthAccess{ValidUntil: time.Now().AddDate(0, 0, 90).Format(time.RFC3339)},
		Aspsp:       AuthAspsp{Name: bankName, Country: strings.ToUpper(country)},
		State:       state,
		RedirectURL: redirectURL,
		PSUType:     "personal",
	}
	var r AuthInitResponse
	err := c.do("POST", "/auth", req, &r)
	return r.URL, err
}

// CreateSession exchanges the callback code for a session.
func (c *Client) CreateSession(code string) (*Session, error) {
	req := SessionCreateRequest{Code: code}
	var s Session
	err := c.do("POST", "/sessions", req, &s)
	return &s, err
}

// ListASPSPs returns available banks, optionally filtered by country client-side.
func (c *Client) ListASPSPs(country string) ([]map[string]any, error) {
	var r ASPSPsResponse
	if err := c.do("GET", "/aspsps", nil, &r); err != nil {
		return nil, err
	}
	if country == "" {
		return r.ASPSPs, nil
	}
	upper := strings.ToUpper(country)
	var filtered []map[string]any
	for _, b := range r.ASPSPs {
		if c, ok := b["country"].(string); ok && strings.ToUpper(c) == upper {
			filtered = append(filtered, b)
		}
	}
	return filtered, nil
}
