// Command vpn-api is the management control-plane for the VPN tunnel
// server: it implements the REST contract the Aura VPN Android client
// expects, and drives the real WireGuard/OpenVPN tunnel backends installed
// by ../../scripts on Ubuntu.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vpnapi/internal/config"
	"vpnapi/internal/httpapi"
)

func main() {
	configPath := flag.String("config", envOr("VPN_API_CONFIG", "/etc/vpn-api/config.json"), "path to config.json")
	printConfig := flag.Bool("print-config", false, "print the effective configuration as shell KEY='value' lines and exit (used by scripts/verify.sh)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("loading config", "path", *configPath, "error", err)
		os.Exit(1)
	}

	if *printConfig {
		// Let shell tooling read the *parsed* configuration rather than
		// re-implementing JSON parsing (and its defaults) in bash.
		emitShellConfig(cfg)
		return
	}
	logger.Info("starting vpn-api",
		"version", httpapi.Version,
		"nodeId", cfg.NodeID,
		"listenAddr", cfg.ListenAddr,
		"wireguardEnabled", cfg.WireGuard.Enabled,
		"openvpnEnabled", cfg.OpenVPN.Enabled,
		"ikev2Enabled", cfg.IKEv2.Enabled,
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

// emitShellConfig prints the effective config as single-quoted shell
// assignments. Values are escaped so a hostile/typo'd config value can't
// break out of the quoting when the caller sources this output.
func emitShellConfig(cfg config.Config) {
	kv := [][2]string{
		{"VPN_CFG_LISTEN_ADDR", cfg.ListenAddr},
		{"VPN_CFG_NODE_ID", cfg.NodeID},
		{"VPN_CFG_PUBLIC_HOST", cfg.PublicEndpointHost},
		{"VPN_CFG_STATE_FILE", cfg.StateFile},
		{"VPN_CFG_TLS_ENABLED", strconv.FormatBool(cfg.TLS.Enabled)},
		{"VPN_CFG_TLS_CERT", cfg.TLS.CertFile},
		{"VPN_CFG_TLS_KEY", cfg.TLS.KeyFile},
		{"VPN_CFG_WG_ENABLED", strconv.FormatBool(cfg.WireGuard.Enabled)},
		{"VPN_CFG_WG_IFACE", cfg.WireGuard.Interface},
		{"VPN_CFG_WG_PORT", strconv.Itoa(cfg.WireGuard.ListenPort)},
		{"VPN_CFG_WG_SUBNET", cfg.WireGuard.SubnetCIDR},
		{"VPN_CFG_WG_GATEWAY", cfg.WireGuard.ServerVirtualIP},
		{"VPN_CFG_WG_KEYDIR", cfg.WireGuard.KeyDir},
		{"VPN_CFG_WG_IPV6_ENABLED", strconv.FormatBool(cfg.WireGuard.IPv6Enabled)},
		{"VPN_CFG_WG_SUBNET_V6", cfg.WireGuard.SubnetCIDRv6},
		{"VPN_CFG_WG_GATEWAY_V6", cfg.WireGuard.ServerVirtualIPv6},
		{"VPN_CFG_OVPN_ENABLED", strconv.FormatBool(cfg.OpenVPN.Enabled)},
		{"VPN_CFG_OVPN_EASYRSA", cfg.OpenVPN.EasyRSADir},
		{"VPN_CFG_OVPN_SERVERDIR", cfg.OpenVPN.ServerDir},
		{"VPN_CFG_OVPN_MGMT_UDP", cfg.OpenVPN.ManagementUDPAddr},
		{"VPN_CFG_OVPN_MGMT_TCP", cfg.OpenVPN.ManagementTCPAddr},
		{"VPN_CFG_OVPN_UDP_PORT", strconv.Itoa(cfg.OpenVPN.UDPPort)},
		{"VPN_CFG_OVPN_TCP_PORT", strconv.Itoa(cfg.OpenVPN.TCPPort)},
		{"VPN_CFG_IKEV2_ENABLED", strconv.FormatBool(cfg.IKEv2.Enabled)},
		{"VPN_CFG_IKEV2_SUBNET", cfg.IKEv2.SubnetCIDR},
		{"VPN_CFG_IKEV2_CONFDIR", cfg.IKEv2.ConfDir},
		{"VPN_CFG_IKEV2_CERT", filepath.Join(cfg.IKEv2.CertDir, cfg.IKEv2.ServerCert)},
		{"VPN_CFG_IKEV2_KEY", filepath.Join(cfg.IKEv2.KeyDir, cfg.IKEv2.ServerKey)},
		{"VPN_CFG_IKEV2_SERVER_ID", cfg.IKEv2.ServerID},
		{"VPN_CFG_DNS_RESOLVER", cfg.DNS.ResolverAddr},
	}
	for _, pair := range kv {
		fmt.Printf("%s='%s'\n", pair[0], strings.ReplaceAll(pair[1], "'", `'\''`))
	}
}
