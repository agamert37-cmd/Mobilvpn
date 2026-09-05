#!/usr/bin/env bash
# Builds the vpn-api Go binary (stdlib only — no network access needed to
# fetch modules), installs it, renders config.json from the template, and
# enables the systemd service. Safe to re-run: it rebuilds the binary and
# restarts the service, but never overwrites an existing config.json.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=./lib.sh
. ./lib.sh

require_root

REPO_ROOT="$(cd .. && pwd)"
API_PUBLIC_PORT="${VPN_API_PUBLIC_PORT:-8443}"
NODE_ID="${VPN_NODE_ID:-$(hostname -s 2>/dev/null || echo vpn_node_01)}"
NODE_REGION="${VPN_NODE_REGION:-Unconfigured Region}"
DOMAIN="${VPN_API_DOMAIN:-}"
WG_IFACE="${VPN_WG_IFACE:-wg0}"
WG_PORT="${VPN_WG_PORT:-51820}"
WG_SUBNET="${VPN_WG_SUBNET:-10.66.0.0/16}"
WG_GATEWAY="${VPN_WG_GATEWAY:-10.66.0.1}"
WG_SUBNET_V6="${VPN_WG_SUBNET_V6:-fd00:66::/64}"
WG_GATEWAY_V6="${VPN_WG_GATEWAY_V6:-fd00:66::1}"
# Bu, 20-wireguard-setup.sh'ın wg0'a bir IPv6 adresi verip vermediğiyle
# aynı testtir; ikisi ayrışırsa API, taşınamayan bir rotayı reklam eder.
IPV6_ENABLED="false"
[ "$(detect_ipv6_support)" = "1" ] && IPV6_ENABLED="true"
OVPN_UDP_PORT="${VPN_OVPN_UDP_PORT:-1194}"
OVPN_TCP_PORT="${VPN_OVPN_TCP_PORT:-443}"
OVPN_SUBNET="${VPN_OVPN_SUBNET:-10.77.0.0/16}"
IKEV2_SUBNET="${VPN_IKEV2_SUBNET:-10.88.0.0/16}"
# IKEv2 opt-in: install.sh --with-ikev2 (ya da VPN_ENABLE_IKEV2=1) ile açılır.
# --skip-tls ile kurulduysa config'de de TLS kapalı olmalı; aksi hâlde
# vpn-api var olmayan sertifika dosyalarını açmaya çalışıp açılışta ölür.
TLS_ENABLED="true"
[ "${VPN_SKIP_TLS:-0}" = "1" ] && TLS_ENABLED="false"
IKEV2_ENABLED="${VPN_ENABLE_IKEV2:-0}"
[ "$IKEV2_ENABLED" = "1" ] && IKEV2_ENABLED="true" || IKEV2_ENABLED="false"

if [ -z "$DOMAIN" ]; then
  log_err "VPN_API_DOMAIN ayarlanmadı (ör. VPN_API_DOMAIN=vpn.example.com). Android istemcisinin"
  log_err "bağlanacağı genel ana bilgisayar adı için gereklidir; sertifika da bu ada göre alınır."
  exit 1
fi

log_info "Go ikili dosyası derleniyor..."
BUILD_TMP="$(mktemp)"
( cd "$REPO_ROOT/api" && go build -o "$BUILD_TMP" ./cmd/vpn-api )
install -m 755 "$BUILD_TMP" /usr/local/bin/vpn-api
rm -f "$BUILD_TMP"
log_ok "/usr/local/bin/vpn-api kuruldu."

# NOT: bu betiğin $REPO_ROOT'u (install.sh tarafından $INSTALL_DIR olarak
# kopyalanmış olması beklenir) systemd birimlerinin referans aldığı kalıcı
# konumdur — bkz. systemd/vpn-blocklist-update.service.
install -d -m 755 /etc/vpn-api
CONFIG_PATH=/etc/vpn-api/config.json
if [ -f "$CONFIG_PATH" ]; then
  log_warn "$CONFIG_PATH zaten var; üzerine yazılmadı. Yeniden oluşturmak için önce silin."
else
  FLEET_SECRET="$(openssl rand -hex 32)"
  render_template "$REPO_ROOT/config/config.json.tmpl" "$CONFIG_PATH" \
    "API_PUBLIC_PORT=$API_PUBLIC_PORT" \
    "NODE_ID=$NODE_ID" \
    "NODE_REGION=$NODE_REGION" \
    "DOMAIN=$DOMAIN" \
    "WG_IFACE=$WG_IFACE" \
    "WG_PORT=$WG_PORT" \
    "WG_SUBNET=$WG_SUBNET" \
    "WG_GATEWAY=$WG_GATEWAY" \
    "WG_SUBNET_V6=$WG_SUBNET_V6" \
    "WG_GATEWAY_V6=$WG_GATEWAY_V6" \
    "IPV6_ENABLED=$IPV6_ENABLED" \
    "OVPN_UDP_PORT=$OVPN_UDP_PORT" \
    "OVPN_TCP_PORT=$OVPN_TCP_PORT" \
    "OVPN_SUBNET=$OVPN_SUBNET" \
    "IKEV2_SUBNET=$IKEV2_SUBNET" \
    "IKEV2_ENABLED=$IKEV2_ENABLED" \
    "TLS_ENABLED=$TLS_ENABLED" \
    "FLEET_SECRET=$FLEET_SECRET"
  chmod 600 "$CONFIG_PATH"
  log_ok "Yazıldı: $CONFIG_PATH"
fi

install -m 644 "$REPO_ROOT/systemd/vpn-api.service" /etc/systemd/system/vpn-api.service
systemctl daemon-reload
systemctl enable --now vpn-api

log_ok "vpn-api servisi etkin. Durum: systemctl status vpn-api"
log_info "Android uygulamasındaki Sunucu API Uç Noktası'nı şuna ayarlayın: https://$DOMAIN:$API_PUBLIC_PORT/"
