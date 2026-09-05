// Command vpn-api is the management control-plane for the VPN tunnel
// server: it implements the REST contract the Aura VPN Android client
// expects, and drives the real WireGuard/OpenVPN tunnel backends installed
// by ../../scripts on Ubuntu.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vpnapi/internal/config"
	"vpnapi/internal/httpapi"
)

func main() {
	configPath := flag.String("config", envOr("VPN_API_CONFIG", "/etc/vpn-api/config.json"), "path to config.json")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("loading config", "path", *configPath, "error", err)
		os.Exit(1)
	}
	logger.Info("starting vpn-api",
		"version", httpapi.Version,
		"nodeId", cfg.NodeID,
		"listenAddr", cfg.ListenAddr,
		"wireguardEnabled", cfg.WireGuard.Enabled,
		"openvpnEnabled", cfg.OpenVPN.Enabled,
	)

	app, err := httpapi.NewApp(cfg, logger)
	if err != nil {
		logger.Error("initializing application", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.StartBackgroundTasks(ctx)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	if cfg.TLS.Enabled {
		reloader, err := httpapi.NewCertReloader(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			logger.Error("loading TLS certificate", "certFile", cfg.TLS.CertFile, "keyFile", cfg.TLS.KeyFile, "error", err)
			os.Exit(1)
		}
		srv.TLSConfig = &tls.Config{
			GetCertificate: reloader.GetCertificate,
			MinVersion:     tls.VersionTLS12,
		}
		go func() {
			logger.Info("listening (https)", "addr", cfg.ListenAddr)
			serverErr <- srv.ListenAndServeTLS("", "")
		}()
	} else {
		logger.Warn("TLS disabled — serving plain HTTP; only appropriate for local development or behind your own TLS-terminating proxy")
		go func() {
			logger.Info("listening (http)", "addr", cfg.ListenAddr)
			serverErr <- srv.ListenAndServe()
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("http server stopped unexpectedly", "error", err)
		}
	case sig := <-sigCh:
		logger.Info("received shutdown signal", "signal", sig.String())
	}

	cancel() // stop background tasks (fleet polling, idle-session reaper)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful http shutdown failed", "error", err)
	}
	app.Shutdown()
	logger.Info("vpn-api stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
