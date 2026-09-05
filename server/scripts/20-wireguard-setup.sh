#!/usr/bin/env bash
# Bootstraps the server's WireGuard identity and brings up wg0. Peers are
# never listed in wg0.conf — they're added/removed live by the vpn-api
# daemon via `wg set` (see internal/wireguard/manager.go) — so this file
# only ever describes the interface itself.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=./lib.sh
. ./lib.sh

require_root

WG_DIR=/etc/wireguard
WG_IFACE="${VPN_WG_IFACE:-wg0}"
WG_PORT="${VPN_WG_PORT:-51820}"
WG_ADDRESS="${VPN_WG_ADDRESS:-10.66.0.1/16}"
WG_ADDRESS_V6="${VPN_WG_ADDRESS_V6:-fd00:66::1/64}"
IPV6_SUPPORTED="$(detect_ipv6_support)"

install -d -m 700 "$WG_DIR"

PRIV_KEY_FILE="$WG_DIR/server_private.key"
PUB_KEY_FILE="$WG_DIR/server_public.key"

# IMPORTANT: this is the exact filename vpn-api's wireguard.EnsureServerIdentity
# looks for. Generating it here first (instead of letting the Go process
# create it on first run) guarantees the key baked into wg0.conf and the
# key vpn-api hands out to clients as "serverPublicKey" are the same
# keypair — if they ever diverged, clients would get a public key the
# kernel interface doesn't actually hold, and every handshake would fail.
if [ -s "$PRIV_KEY_FILE" ]; then
  log_info "Mevcut sunucu anahtarı kullanılıyor: $PRIV_KEY_FILE"
else
  log_info "Yeni WireGuard sunucu anahtar çifti üretiliyor..."
  umask 077
  wg genkey > "$PRIV_KEY_FILE"
fi
chmod 600 "$PRIV_KEY_FILE"
wg pubkey < "$PRIV_KEY_FILE" > "$PUB_KEY_FILE"
chmod 644 "$PUB_KEY_FILE"

PRIVATE_KEY="$(cat "$PRIV_KEY_FILE")"

# Çift yığın yalnızca ana bilgisayarın gerçekten global IPv6'sı varsa
# açılır: olmayan bir yukarı akışa ::/0 reklam etmek istemcinin IPv6
# trafiğini kara deliğe yollar.
ADDRESS_LINE="Address = $WG_ADDRESS"
if [ "$IPV6_SUPPORTED" = "1" ]; then
  ADDRESS_LINE="Address = $WG_ADDRESS, $WG_ADDRESS_V6"
  log_info "Global IPv6 tespit edildi; tünel çift yığın (dual-stack) kurulacak."
else
  log_warn "Global IPv6 yok; tünel yalnızca IPv4 olacak (bkz. docs/SECURITY.md - IPv6 sızıntısı)."
fi

WG_CONF="$WG_DIR/$WG_IFACE.conf"
log_info "Yazılıyor: $WG_CONF"
cat > "$WG_CONF" <<EOF
# Managed by Mobilvpn server/scripts/20-wireguard-setup.sh.
# Peer'lar buraya elle eklenmez: vpn-api tarafından 'wg set' ile canlı
# olarak yönetilir. NAT/forward kuralları da burada değil,
# scripts/40-nftables-firewall.sh tarafından ayrı yönetilir.
[Interface]
$ADDRESS_LINE
ListenPort = $WG_PORT
PrivateKey = $PRIVATE_KEY
SaveConfig = false
EOF
chmod 600 "$WG_CONF"

systemctl enable --now "wg-quick@${WG_IFACE}" >/dev/null
log_ok "wg-quick@${WG_IFACE} etkin. Sunucu genel anahtarı: $(cat "$PUB_KEY_FILE")"
log_info "Bu değerin config.json içindeki wireguard.keyDir ($WG_DIR) ile eşleştiğinden emin olun."
