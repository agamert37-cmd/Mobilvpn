#!/usr/bin/env bash
# Issues (or renews) a Let's Encrypt certificate for the API's public
# hostname using certbot's standalone HTTP-01 challenge, and wires up
# automatic renewal. vpn-api itself terminates TLS directly with these
# files (see internal/httpapi/tlsreload.go) — no reverse proxy involved.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=./lib.sh
. ./lib.sh

require_root

DOMAIN="${VPN_API_DOMAIN:-}"
EMAIL="${VPN_ACME_EMAIL:-}"

if [ -z "$DOMAIN" ]; then
  log_err "VPN_API_DOMAIN ayarlanmadı. Örnek: VPN_API_DOMAIN=vpn.example.com ./70-tls-certbot.sh"
  log_err "Bu alan adının bu sunucunun genel IP'sine işaret ettiğinden emin olun (HTTP-01 doğrulaması için 80. port geçici olarak açılacak)."
  exit 1
fi

if ! command -v certbot >/dev/null 2>&1; then
  log_info "certbot kuruluyor..."
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends certbot
fi

LIVE_DIR="/etc/letsencrypt/live/$DOMAIN"
if [ -s "$LIVE_DIR/fullchain.pem" ]; then
  log_info "Mevcut sertifika bulundu: $LIVE_DIR — certbot renew ile güncellenecek."
  certbot renew --standalone --non-interactive --pre-hook "true" || true
else
  log_warn "Sertifika alınırken 80. port kısa süreliğine bu betik tarafından dinlenecek."
  log_warn "80. portun başka bir servis tarafından kullanılmadığından emin olun."
  CERTBOT_ARGS=(certonly --standalone --non-interactive --agree-tos -d "$DOMAIN")
  if [ -n "$EMAIL" ]; then
    CERTBOT_ARGS+=(--email "$EMAIL")
  else
    CERTBOT_ARGS+=(--register-unsafely-without-email)
  fi
  certbot "${CERTBOT_ARGS[@]}"
fi

if [ ! -s "$LIVE_DIR/fullchain.pem" ]; then
  log_err "Sertifika üretimi başarısız oldu: $LIVE_DIR/fullchain.pem bulunamadı."
  exit 1
fi

# certbot's systemd timer (installed with the package) handles renewal on
# its own schedule; we just need vpn-api's config to point at the live
# symlinks, which certbot updates in place on every renewal.
log_ok "Sertifika hazır: $LIVE_DIR/{fullchain,privkey}.pem"
log_info "config.json içinde şunu ayarlayın:"
log_info "  \"tls\": { \"enabled\": true, \"certFile\": \"$LIVE_DIR/fullchain.pem\", \"keyFile\": \"$LIVE_DIR/privkey.pem\" }"
log_info "vpn-api bu dosyaları periyodik olarak izler; yenilemeden sonra yeniden başlatma gerekmez."
log_info "(/etc/letsencrypt salt-okunur olarak ProtectSystem=strict altında bile okunabilir; ek izin gerekmez.)"
