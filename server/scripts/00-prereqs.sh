#!/usr/bin/env bash
# Installs every package the rest of the stack depends on. Idempotent:
# safe to re-run after a partial install.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=./lib.sh
. ./lib.sh

require_root
require_ubuntu

log_info "Paket listeleri güncelleniyor..."
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq

PACKAGES=(
  wireguard-tools
  iproute2
  nftables
  openvpn
  easy-rsa
  unbound
  unbound-anchor
  golang-go
  curl
  ca-certificates
  chrony
  # openssl: 30-openvpn-setup.sh and 60-vpn-api-service.sh generate secrets
  # with it. Present on most Ubuntu images but NOT guaranteed on minimal
  # ones, and a missing binary would fail the install halfway through.
  openssl
  # qrencode: used by test-peer.sh to print a scannable WireGuard config,
  # which is how an operator proves the tunnel itself works end to end.
  qrencode
)

log_info "Kuruluyor: ${PACKAGES[*]}"
apt-get install -y --no-install-recommends "${PACKAGES[@]}"

# Go 1.22 from apt is enough to build vpn-api (go.mod requires >= 1.22), but
# if a newer toolchain is already present (e.g. installed manually) prefer
# it — this only installs the apt package as a fallback.
if ! command -v go >/dev/null 2>&1; then
  log_err "go bulunamadı; golang-go paketinin kurulumu başarısız olmuş olabilir."
  exit 1
fi
log_ok "Go sürümü: $(go version)"

# chrony keeps the clock accurate, which matters for TLS certificate
# validation (OpenVPN client certs, Caddy's ACME) and for WireGuard/OpenVPN
# handshake timestamps.
systemctl enable --now chrony >/dev/null 2>&1 || log_warn "chrony etkinleştirilemedi; sistem saati elle kontrol edilmeli."

log_ok "Ön koşul paketleri kuruldu."
