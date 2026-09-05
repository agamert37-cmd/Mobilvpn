#!/usr/bin/env bash
# Provisions a real, disposable WireGuard peer through this server's own
# API and prints a ready-to-use client config plus a scannable QR code.
#
# This is how you prove the tunnel itself works — with the official
# WireGuard client, independent of the Android app — because it goes
# through the exact production path (IPAM, `wg set`, the connect response
# contract), not a side channel.
#
#   sudo ./test-peer.sh                     # yeni test peer'ı oluştur
#   sudo ./test-peer.sh --disconnect <id>   # test peer'ını kaldır
#
# It uses the bring-your-own-key path: the private key is generated here
# and never sent to the server (only the public key is).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=./lib.sh
. ./lib.sh

require_root

CONFIG_PATH="${VPN_API_CONFIG:-/etc/vpn-api/config.json}"
if ! CFG_OUTPUT="$(vpn-api -print-config -config "$CONFIG_PATH" 2>&1)"; then
  log_err "config.json okunamadı: $CFG_OUTPUT"
  exit 1
fi
eval "$CFG_OUTPUT"

PORT="${VPN_CFG_LISTEN_ADDR##*:}"
SCHEME="http"
CURL_TLS_OPTS=()
if [ "$VPN_CFG_TLS_ENABLED" = "true" ]; then
  SCHEME="https"
  # Loopback üzerinden konuşuyoruz; sertifika genel ada göre düzenlendiği
  # için ana bilgisayar adı doğrulaması burada anlamlı değil.
  CURL_TLS_OPTS=(-k)
fi
API="$SCHEME://127.0.0.1:$PORT"

json_field() {
  # Yalnızca bu API'nin ürettiği düz JSON için: alan değerleri tırnak
  # içermez, dolayısıyla bu çıkarım güvenli (ve jq bağımlılığı gerektirmez).
  grep -o "\"$1\":\"[^\"]*\"" <<< "$2" | head -1 | cut -d'"' -f4
}

if [ "${1:-}" = "--disconnect" ]; then
  SESSION_ID="${2:-}"
  if [ -z "$SESSION_ID" ]; then
    log_err "Kullanım: $0 --disconnect <sessionId>"
    exit 1
  fi
  RESP="$(curl -s "${CURL_TLS_OPTS[@]}" -X POST "$API/api/v1/disconnect" \
    -H 'Content-Type: application/json' \
    -d "{\"sessionId\":\"$SESSION_ID\",\"reason\":\"TEST_PEER_CLEANUP\"}")"
  log_ok "Sunucu yanıtı: $(json_field message "$RESP")"
  exit 0
fi

if [ "$VPN_CFG_WG_ENABLED" != "true" ]; then
  log_err "Bu düğümde WireGuard etkin değil (config.json)."
  exit 1
fi

log_info "İstemci anahtar çifti üretiliyor (özel anahtar bu makineden çıkmaz)..."
CLIENT_PRIV="$(wg genkey)"
CLIENT_PUB="$(wg pubkey <<< "$CLIENT_PRIV")"

log_info "API üzerinden tünel isteniyor: $API/api/v1/connect"
RESP="$(curl -s "${CURL_TLS_OPTS[@]}" -X POST "$API/api/v1/connect" \
  -H 'Content-Type: application/json' \
  -d "{\"serverId\":\"$VPN_CFG_NODE_ID\",\"protocol\":\"WIREGUARD\",\"clientPublicKey\":\"$CLIENT_PUB\"}")"

if ! grep -q '"success":true' <<< "$RESP"; then
  log_err "Sunucu tüneli reddetti:"
  echo "$RESP"
  exit 1
fi

SESSION_ID="$(json_field sessionId "$RESP")"
VIRTUAL_IP="$(json_field virtualIp "$RESP")"
SERVER_PUB="$(json_field serverPublicKey "$RESP")"
ENDPOINT="$(json_field endpoint "$RESP")"
ALLOWED_IPS="$(json_field allowedIps "$RESP")"
DNS_ADDR="$VPN_CFG_DNS_RESOLVER"
MTU="$(grep -o '"mtu":[0-9]*' <<< "$RESP" | head -1 | cut -d: -f2)"
KEEPALIVE="$(grep -o '"persistentKeepaliveSeconds":[0-9]*' <<< "$RESP" | head -1 | cut -d: -f2)"

CLIENT_CONF="[Interface]
PrivateKey = $CLIENT_PRIV
Address = $VIRTUAL_IP/32
DNS = $DNS_ADDR
MTU = ${MTU:-1420}

[Peer]
PublicKey = $SERVER_PUB
Endpoint = $ENDPOINT
AllowedIPs = $ALLOWED_IPS
PersistentKeepalive = ${KEEPALIVE:-25}"

echo
echo "=============== İSTEMCİ YAPILANDIRMASI ==============="
echo "$CLIENT_CONF"
echo "======================================================"
echo

if command -v qrencode >/dev/null 2>&1; then
  echo "Resmî WireGuard uygulamasıyla taramak için:"
  qrencode -t ansiutf8 <<< "$CLIENT_CONF"
else
  log_warn "qrencode kurulu değil; QR kodu atlandı (apt install qrencode)."
fi

log_ok "Oturum kimliği: $SESSION_ID"
log_info "Sunucu tarafında peer gerçekten eklendi mi:  wg show $VPN_CFG_WG_IFACE"
log_info "Telemetriyi izlemek için: curl -s ${CURL_TLS_OPTS[*]} \"$API/api/v1/telemetry?sessionId=$SESSION_ID\""
log_warn "Test bitince temizleyin: sudo $0 --disconnect $SESSION_ID"
