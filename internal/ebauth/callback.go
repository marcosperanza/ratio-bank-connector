// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ebauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"html"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CodeFromInput accepts what a user can paste after the bank redirected them:
// the whole URL, just its query string, or the bare code. state, when the
// input carries one, must match the consent this connector started.
func CodeFromInput(raw, wantState string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("nothing pasted")
	}
	q := raw
	if i := strings.Index(raw, "?"); i >= 0 {
		q = raw[i+1:]
	}
	if !strings.Contains(q, "=") {
		return raw, nil // a bare code
	}
	vals, err := url.ParseQuery(strings.TrimPrefix(q, "?"))
	if err != nil {
		return "", fmt.Errorf("cannot read the pasted URL: %w", err)
	}
	return codeFromValues(vals, wantState)
}

func codeFromValues(vals url.Values, wantState string) (string, error) {
	if e := vals.Get("error"); e != "" {
		return "", fmt.Errorf("the bank refused the authorization: %s %s", e, vals.Get("error_description"))
	}
	if st := vals.Get("state"); wantState != "" && st != "" && st != wantState {
		return "", errors.New("the pasted URL belongs to a different authorization (state mismatch)")
	}
	code := vals.Get("code")
	if code == "" {
		return "", errors.New("no `code` in the pasted URL")
	}
	return code, nil
}

// CanListen says whether the redirect URL points at this machine, so the
// connector can receive the callback itself instead of asking for a paste.
func CanListen(redirectURL string) bool {
	u, err := url.Parse(redirectURL)
	if err != nil {
		return false
	}
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// Receiver serves the redirect URL on this machine until the bank sends the
// browser back. With a certificate it serves HTTPS; for an https URL without
// one it makes a self-signed certificate (the browser warns once; a
// certificate from mkcert avoids it). A plain http URL is served as is.
// It listens as soon as it is created, so a busy port shows up before the
// user is sent to the bank.
type Receiver struct {
	srv  *http.Server
	done chan receiverResult
}

type receiverResult struct {
	code string
	err  error
}

func NewReceiver(redirectURL, certFile, keyFile, wantState string) (*Receiver, error) {
	u, err := url.Parse(redirectURL)
	if err != nil || !CanListen(redirectURL) {
		return nil, fmt.Errorf("%q is not a local URL", redirectURL)
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[u.Scheme]
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return nil, fmt.Errorf("cannot listen on %s: %w", u.Host, err)
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	r := &Receiver{done: make(chan receiverResult, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, req *http.Request) {
		code, err := codeFromValues(req.URL.Query(), wantState)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "<p>%s</p>", html.EscapeString(err.Error()))
			select {
			case r.done <- receiverResult{err: err}:
			default:
			}
			return
		}
		fmt.Fprint(w, "<p>Authorization received. You can close this tab and go back to Ratio Connector.</p>")
		select {
		case r.done <- receiverResult{code: code}:
		default:
		}
	})
	r.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if u.Scheme == "https" {
		cfg := &tls.Config{MinVersion: tls.VersionTLS12}
		var cert tls.Certificate
		if certFile != "" {
			cert, err = tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				ln.Close()
				return nil, fmt.Errorf("load callback certificate: %w", err)
			}
		} else if cert, err = selfSigned(); err != nil {
			ln.Close()
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{cert}
		ln = tls.NewListener(ln, cfg)
	}
	go func() { _ = r.srv.Serve(ln) }()
	return r, nil
}

// Wait blocks until the bank's callback arrives or ctx ends, then stops serving.
func (r *Receiver) Wait(ctx context.Context) (string, error) {
	defer r.Close()
	select {
	case res := <-r.done:
		return res.code, res.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (r *Receiver) Close() {
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = r.srv.Shutdown(sctx)
}

// WaitForCode is NewReceiver followed by Wait.
func WaitForCode(ctx context.Context, redirectURL, certFile, keyFile, wantState string) (string, error) {
	r, err := NewReceiver(redirectURL, certFile, keyFile, wantState)
	if err != nil {
		return "", err
	}
	return r.Wait(ctx)
}

func selfSigned() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
