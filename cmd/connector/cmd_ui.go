// Copyright (C) 2026 Marco Speranza.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/marcosperanza/ratio-bank-connector/v4/internal/service"
	"github.com/marcosperanza/ratio-bank-connector/v4/internal/ui"
)

func cmdUI() *cobra.Command {
	var listen string
	var noOpen bool
	var idle time.Duration
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open the settings page in your browser",
		Long: `Starts a local settings page (Ratio connection, bank sources, prices, background
service) and opens it in your browser. It listens on 127.0.0.1 only and needs the
one-time token in the printed link. In Docker use --listen 0.0.0.0:8787 and publish
the port on 127.0.0.1 only.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if listen == "" {
				listen = "127.0.0.1:0"
			}
			ln, err := net.Listen("tcp", listen)
			if err != nil {
				return fmt.Errorf("listen: %w", err)
			}
			raw := make([]byte, 24)
			if _, err := rand.Read(raw); err != nil {
				return err
			}
			token := hex.EncodeToString(raw)
			srv := &ui.Server{
				ConfigPath: cfgPath,
				StatusPath: statusPath(),
				Version:    version,
				Token:      token,
				Service:    service.New(),
				Docker:     service.InContainer(),
				RefreshPrices: func(ctx context.Context) (int, int, error) {
					return refreshPricesNow(ctx, cfgPath)
				},
			}
			_, port, _ := net.SplitHostPort(ln.Addr().String())
			link := fmt.Sprintf("http://127.0.0.1:%s/#t=%s", port, token)
			fmt.Println("Settings page:", link)
			fmt.Println("Press Ctrl-C to close it.")
			if !noOpen && !service.InContainer() {
				openBrowser(link)
			}
			// The page exits on its own after a quiet spell, so a forgotten
			// settings window does not leave a listener open.
			var lastHit atomic.Int64
			lastHit.Store(time.Now().UnixNano())
			handler := srv.Handler()
			httpSrv := &http.Server{
				ReadHeaderTimeout: 10 * time.Second,
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					lastHit.Store(time.Now().UnixNano())
					handler.ServeHTTP(w, r)
				}),
			}
			go func() {
				for range time.Tick(time.Minute) {
					if time.Since(time.Unix(0, lastHit.Load())) > idle {
						fmt.Println("Closing the settings page after a period of inactivity.")
						_ = httpSrv.Close()
						return
					}
				}
			}()
			go func() {
				sig := make(chan os.Signal, 1)
				signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
				<-sig
				_ = httpSrv.Close()
			}()
			if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "", "address to listen on (default 127.0.0.1, random port)")
	cmd.Flags().DurationVar(&idle, "idle-timeout", 30*time.Minute, "close the page after this long without a request")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "print the link without opening a browser")
	return cmd
}

func cmdService() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Install, remove or restart the background service",
	}
	mgr := func() (*service.Manager, error) {
		if service.InContainer() {
			return nil, fmt.Errorf("in Docker the container runtime is the service: use `docker compose up -d` / `restart`")
		}
		return service.New(), nil
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Install the connector as a per-user service that starts at login, and start it",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := mgr()
			if err != nil {
				return err
			}
			spec, err := m.DefaultSpec(cfgPath)
			if err != nil {
				return err
			}
			if err := m.Install(spec); err != nil {
				return err
			}
			fmt.Println("Installed and started.", spec.BinaryPath)
			return nil
		},
	}, &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the service (configuration and data are kept)",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := mgr()
			if err != nil {
				return err
			}
			return m.Uninstall()
		},
	}, &cobra.Command{
		Use:   "restart",
		Short: "Restart the service to apply a changed configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := mgr()
			if err != nil {
				return err
			}
			return m.Restart()
		},
	}, &cobra.Command{
		Use:   "status",
		Short: "Show whether the service is installed and running",
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := mgr()
			if err != nil {
				return err
			}
			st := m.Status()
			fmt.Printf("installed: %v\nrunning:   %v\n", st.Installed, st.Running)
			return nil
		},
	})
	return cmd
}
