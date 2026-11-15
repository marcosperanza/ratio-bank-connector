// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package ui

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/config"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ebauth"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/service"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/setup"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/status"
)

//go:embed web/index.html
var webFS embed.FS

// Server is the settings backend.
type Server struct {
	ConfigPath string
	StatusPath string
	Version    string
	Token      string
	Service    *service.Manager
	Docker     bool
	// SealingKeyPath, Sessions and NewEB are set by tests; the defaults use the configured key, the real store and client.
	SealingKeyPath string
	Sessions       *ebauth.Store
	NewEB          func(*config.Config) (EBAPI, error)
	// RefreshPrices downloads and delivers the prices now and reports the tickers tried and those without data.
	RefreshPrices func(ctx context.Context) (tickers, failed int, err error)

	refreshing atomic.Bool
	mu         sync.Mutex
	flows      ebFlows
}

func (s *Server) load() *config.Config {
	if cfg, err := config.Load(s.ConfigPath); err == nil {
		return cfg
	}
	return &config.Config{
		Mode:         "ratio_managed",
		Sync:         config.SyncConfig{IntervalHours: 6, LookbackDays: 30, CSVDir: config.DefaultDataDir(), ArchiveMode: config.ArchiveOff},
		RatioManaged: config.RatioManagedConfig{URL: "https://www.ratio-fin.com", PollIntervalSeconds: 1800},
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

// guard rejects requests that are not from the page this process served:
// wrong Host (DNS rebinding), cross-origin writes, or a wrong token.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || (host != "127.0.0.1" && host != "localhost") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if o := r.Header.Get("Origin"); o != "" {
				if u, err := url.Parse(o); err != nil || u.Host != r.Host {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Connector-Token")), []byte(s.Token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		data, _ := webFS.ReadFile("web/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("PUT /api/config", s.handleSave)
	mux.HandleFunc("POST /api/check/{what}", s.handleCheck)
	mux.HandleFunc("POST /api/service/{action}", s.handleService)
	mux.HandleFunc("POST /api/prices/fingerprint", s.handleFingerprint)
	mux.HandleFunc("POST /api/prices/refresh", s.handleRefreshPrices)
	mux.HandleFunc("GET /api/eb/banks", s.handleEBBanks)
	mux.HandleFunc("POST /api/eb/start", s.handleEBStart)
	mux.HandleFunc("GET /api/eb/flow/{id}", s.handleEBFlow)
	mux.HandleFunc("POST /api/eb/flow/{id}/finish", s.handleEBFinish)
	mux.HandleFunc("DELETE /api/eb/sessions/{id}", s.handleEBForget)
	return s.guard(mux)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.load()
	_, statErr := os.Stat(s.ConfigPath)
	st, _ := status.Read(s.StatusPath)
	svc := service.Status{}
	if !s.Docker && s.Service != nil {
		svc = s.Service.Status()
	}
	writeJSON(w, 200, map[string]any{
		"version":           s.Version,
		"configPath":        s.ConfigPath,
		"configured":        statErr == nil,
		"docker":            s.Docker,
		"config":            ViewOf(cfg),
		"problems":          setup.Validate(cfg),
		"service":           svc,
		"status":            st,
		"alive":             st.Alive(time.Now()),
		"ebSessions":        sessionViews(s.loadSessions(), time.Now()),
		"pricesFingerprint": s.existingFingerprint(cfg),
	})
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	var v View
	if err := readJSON(r, &v); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.load()
	Apply(cfg, v)
	problems := setup.Validate(cfg)
	if setup.Blocking(problems) {
		writeJSON(w, 422, map[string]any{"saved": false, "problems": problems})
		return
	}
	if err := config.Save(s.ConfigPath, cfg); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"saved": true, "problems": problems, "config": ViewOf(cfg)})
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL     string `json:"url"`
		APIKey  string `json:"apiKey"`
		AppID   string `json:"appId"`
		PEMPath string `json:"pemPath"`
		Name    string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	s.mu.Lock()
	cfg := s.load()
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	switch r.PathValue("what") {
	case "ratio":
		key := req.APIKey
		if key == "" {
			key = cfg.RatioManaged.APIKey
		}
		writeJSON(w, 200, setup.CheckRatio(req.URL, key))
	case "enablebanking":
		pem := req.PEMPath
		if pem == "" {
			pem = cfg.EnableBanking.PrivateKeyPath
		}
		writeJSON(w, 200, setup.CheckEnableBanking(req.AppID, pem, "", true))
	case "prices":
		key := req.APIKey
		if key == "" {
			for _, p := range cfg.Prices.Providers {
				if p.Name == req.Name {
					key = p.APIKey
				}
			}
		}
		writeJSON(w, 200, setup.CheckPriceProvider(ctx, req.Name, key))
	default:
		writeJSON(w, 404, map[string]string{"error": "unknown check"})
	}
}

func (s *Server) handleService(w http.ResponseWriter, r *http.Request) {
	if s.Docker || s.Service == nil {
		writeJSON(w, 409, map[string]string{"error": "In Docker the container runtime is the service: use `docker compose restart`."})
		return
	}
	var err error
	switch r.PathValue("action") {
	case "install":
		var spec service.Spec
		if spec, err = s.Service.DefaultSpec(s.ConfigPath); err == nil {
			err = s.Service.Install(spec)
		}
	case "restart":
		err = s.Service.Restart()
	case "uninstall":
		err = s.Service.Uninstall()
	default:
		err = errors.New("unknown action")
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "service": s.Service.Status()})
}

func (s *Server) loadSessions() []ebauth.Session {
	all, _ := s.sessionStore().Load()
	return all
}

func (s *Server) handleEBBanks(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.load()
	s.mu.Unlock()
	newEB := s.NewEB
	if newEB == nil {
		newEB = defaultEB
	}
	api, err := newEB(cfg)
	if err != nil {
		writeJSON(w, 422, map[string]string{"error": err.Error()})
		return
	}
	banks, err := api.ListASPSPs(r.URL.Query().Get("country"))
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	names := []string{}
	for _, b := range banks {
		if n, _ := b["name"].(string); n != "" {
			names = append(names, n)
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	writeJSON(w, 200, map[string]any{"banks": names})
}

func (s *Server) handleEBStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bank    string `json:"bank"`
		Country string `json:"country"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	s.mu.Lock()
	cfg := s.load()
	s.mu.Unlock()
	f, err := s.ebStart(cfg, req.Bank, req.Country)
	if err != nil {
		writeJSON(w, 422, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, s.ebFlow(f.ID))
}

func (s *Server) handleEBFlow(w http.ResponseWriter, r *http.Request) {
	f := s.ebFlow(r.PathValue("id"))
	if f == nil {
		writeJSON(w, 404, map[string]string{"error": "no such authorization"})
		return
	}
	writeJSON(w, 200, f)
}

func (s *Server) handleEBFinish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pasted string `json:"pasted"`
	}
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	s.flows.mu.Lock()
	f := s.flows.last
	s.flows.mu.Unlock()
	if f == nil || f.ID != r.PathValue("id") || f.Mode != "paste" {
		writeJSON(w, 404, map[string]string{"error": "no such authorization"})
		return
	}
	code, err := ebauth.CodeFromInput(req.Pasted, f.state)
	if err == nil {
		s.ebComplete(f, code, nil)
	} else {
		writeJSON(w, 422, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, s.ebFlow(f.ID))
}

func (s *Server) handleEBForget(w http.ResponseWriter, r *http.Request) {
	if err := s.sessionStore().Remove(r.PathValue("id")); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
