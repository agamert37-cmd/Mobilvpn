#!/usr/bin/env bash
# Sets up strongSwan for IKEv2/IPsec. Sessions are not configured here:
# vpn-api drops one file per session into swanctl's conf.d and reloads
# (see internal/ikev2). This script only creates the server identity and
# gets the daemon running.
#
# IKEv2 is optional — it earns its place for phones roaming between Wi-Fi
# and cellular (MOBIKE) and for clients that prefer the IKEv2 support built
# into the OS. WireGuard remains the faster default.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=./lib.sh
. ./lib.sh

require_root

DOMAIN="${VPN_API_DOMAIN:-}"
PUBLIC_IP="${VPN_PUBLIC_IP:-}"
SWAN_DIR="${VPN_SWANCTL_DIR:-/etc/swanctl}"

if [ -z "$DOMAIN" ]; then
  log_err "VPN_API_DOMAIN ayarlanmadı. Sertifikanın SAN'ı ve istemcilerin doğrulayacağı sunucu kimliği bu olur."
  exit 1
fi

log_info "strongSwan paketleri kuruluyor..."
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
  strongswan strongswan-swanctl strongswan-pki charon-systemd libcharon-extra-plugins libcharon-extauth-plugins

# Ubuntu ships two ways to run charon and enables the legacy one by
# default. Both would bind UDP 500/4500, so the deprecated starter has to
# go or the modern swanctl daemon fails to start.
if systemctl is-enabled strongswan-starter.service >/dev/null 2>&1; then
  log_info "Eski strongswan-starter servisi devre dışı bırakılıyor (swanctl ile çakışır)."
  systemctl disable --now strongswan-starter.service >/dev/null 2>&1 || true
fi

install -d -m 755 "$SWAN_DIR/x509" "$SWAN_DIR/x509ca" "$SWAN_DIR/conf.d"
install -d -m 700 "$SWAN_DIR/private"

CA_KEY="$SWAN_DIR/private/ca-key.pem"
CA_CERT="$SWAN_DIR/x509ca/ca-cert.pem"
SRV_KEY="$SWAN_DIR/private/server-key.pem"
SRV_CERT="$SWAN_DIR/x509/server-cert.pem"

if [ -s "$CA_CERT" ] && [ -s "$CA_KEY" ]; then
  log_info "Mevcut IKEv2 CA kullanılıyor: $CA_CERT"
else
  log_info "IKEv2 CA oluşturuluyor (OpenVPN PKI'sinden bağımsız, böylece protokoller birbirine bağlı olmaz)..."
  umask 077
  ipsec pki --gen --type rsa --size 3072 --outform pem > "$CA_KEY"
  ipsec pki --self --ca --lifetime 3650 --in "$CA_KEY" \
    --dn "CN=MobilVPN IKEv2 CA" --outform pem > "$CA_CERT"
  chmod 644 "$CA_CERT"
fi

# The server certificate is regenerated whenever the domain changes, since
# a client verifies the server against a SAN in it — a stale SAN means
# every connection fails authentication.
NEEDS_CERT=1
if [ -s "$SRV_CERT" ] && ipsec pki --print --in "$SRV_CERT" 2>/dev/null | grep -q "altNames:.*$DOMAIN"; then
  NEEDS_CERT=0
fi

if [ "$NEEDS_CERT" = "1" ]; then
  log_info "Sunucu sertifikası oluşturuluyor (SAN: $DOMAIN${PUBLIC_IP:+, $PUBLIC_IP})..."
  umask 077
  ipsec pki --gen --type rsa --size 3072 --outform pem > "$SRV_KEY"
  SAN_ARGS=(--san "$DOMAIN")
  [ -n "$PUBLIC_IP" ] && SAN_ARGS+=(--san "$PUBLIC_IP")
  ipsec pki --pub --in "$SRV_KEY" | ipsec pki --issue --lifetime 825 \
    --cacert "$CA_CERT" --cakey "$CA_KEY" --dn "CN=$DOMAIN" "${SAN_ARGS[@]}" \
    --flag serverAuth --flag ikeIntermediate --outform pem > "$SRV_CERT"
  chmod 644 "$SRV_CERT"
else
  log_info "Sunucu sertifikası zaten $DOMAIN için geçerli; yeniden üretilmedi."
fi

# charon needs to hand out DNS to clients; without this a connected client
# has a tunnel but no working name resolution.
cat > "$SWAN_DIR/conf.d/00-mobilvpn-base.conf" <<EOF
# Managed by scripts/35-ikev2-setup.sh.
# Per-session connections/pools/secrets are written by vpn-api alongside
# this file and removed again on disconnect.
EOF

systemctl enable --now strongswan.service >/dev/null
swanctl --load-all --noprompt >/dev/null 2>&1 || log_warn "swanctl --load-all şimdilik başarısız; servis başladıktan sonra tekrar denenecek."

log_ok "strongSwan etkin. Sunucu kimliği (istemcilerin doğrulayacağı): $DOMAIN"
log_info "CA sertifikası: $CA_CERT"
log_info "config.json içinde ikev2.enabled=true olduğundan emin olun."
