#!/usr/bin/env bash
# Configures unbound as a recursive, DNS-over-TLS-forwarding resolver bound
# only to the tunnel-facing addresses — never the public WAN interface, so
# this never becomes an open resolver. This is what closes the DNS-leak gap
# nftables' forward-chain port-53 drop opens up: clients MUST use this
# resolver, so it had better not be the ISP's or a third party's.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=./lib.sh
. ./lib.sh

require_root

WG_RESOLVER_IP="${VPN_WG_RESOLVER_IP:-10.66.0.1}"
OVPN_RESOLVER_IP="${VPN_OVPN_RESOLVER_IP:-10.77.0.1}"
WG_SUBNET="${VPN_WG_SUBNET:-10.66.0.0/16}"
WG_SUBNET_V6="${VPN_WG_SUBNET_V6:-fd00:66::/64}"
WG_RESOLVER_IP_V6="${VPN_WG_RESOLVER_IP_V6:-fd00:66::1}"
OVPN_SUBNET="${VPN_OVPN_SUBNET:-10.77.0.0/16}"
IPV6_SUPPORTED="$(detect_ipv6_support)"
UPSTREAM_DOT="${VPN_DOT_UPSTREAMS:-1.1.1.1@853#cloudflare-dns.com 9.9.9.9@853#dns.quad9.net}"

CONF_DIR=/etc/unbound/unbound.conf.d
install -d -m 755 "$CONF_DIR"

log_info "Yazılıyor: $CONF_DIR/vpn-tunnel.conf"
{
  echo "# Managed by Mobilvpn server/scripts/50-unbound-dns.sh."
  echo "server:"
  echo "  interface: $WG_RESOLVER_IP"
  echo "  interface: $OVPN_RESOLVER_IP"
  echo "  interface: 127.0.0.1"
  if [ "$IPV6_SUPPORTED" = "1" ]; then
    # Çift yığın tünelde istemciler çözümleyiciye IPv6 üzerinden de
    # ulaşabilmeli; aksi halde v6-öncelikli bir istemci DNS'siz kalır.
    echo "  interface: $WG_RESOLVER_IP_V6"
  fi
  echo "  port: 53"
  echo "  do-ip4: yes"
  if [ "$IPV6_SUPPORTED" = "1" ]; then
    echo "  do-ip6: yes"
  else
    echo "  do-ip6: no"
  fi
  echo "  access-control: 127.0.0.0/8 allow"
  echo "  access-control: $WG_SUBNET allow"
  echo "  access-control: $OVPN_SUBNET allow"
  echo "  access-control: 0.0.0.0/0 refuse"
  if [ "$IPV6_SUPPORTED" = "1" ]; then
    echo "  access-control: $WG_SUBNET_V6 allow"
  fi
  echo "  access-control: ::/0 refuse"
  echo
  echo "  # Gizlilik: no-logs politikası — hiçbir sorgu diske yazılmaz."
  echo "  verbosity: 0"
  echo "  log-queries: no"
  echo "  log-replies: no"
  echo "  use-syslog: no"
  echo "  hide-identity: yes"
  echo "  hide-version: yes"
  echo
  echo "  # Hız: DNS önbelleklemesi ve önceden getirme (prefetch)."
  echo "  prefetch: yes"
  echo "  prefetch-key: yes"
  echo "  cache-min-ttl: 60"
  echo "  serve-expired: yes"
  echo "  qname-minimisation: yes"
  echo
  echo "  # Bu makine bir açık çözümleyici (open resolver) OLMAMALI."
  echo "  do-not-query-localhost: no"
} > "$CONF_DIR/vpn-tunnel.conf"

log_info "Yazılıyor: $CONF_DIR/forward-dot.conf (DNS-over-TLS yukarı akış)"
{
  echo "# Managed by Mobilvpn server/scripts/50-unbound-dns.sh."
  echo "forward-zone:"
  echo "  name: \".\""
  echo "  forward-tls-upstream: yes"
  for pair in $UPSTREAM_DOT; do
    echo "  forward-addr: $pair"
  done
} > "$CONF_DIR/forward-dot.conf"

# Reklam/izleyici engelleme anahtarı: POST /api/v1/settings ile
# threatProtection açılıp kapatıldığında vpn-api bu dosyayı yeniden yazar
# (bkz. internal/httpapi/dns.go). Baştan devre dışı, boş bir dosya olarak
# oluşturuluyor ki unbound onu include edebilsin.
BLOCKLIST_TOGGLE="$CONF_DIR/blocklist-toggle.conf"
if [ ! -f "$BLOCKLIST_TOGGLE" ]; then
  echo "# threat protection disabled by default until POST /api/v1/settings enables it" > "$BLOCKLIST_TOGGLE"
fi
if ! grep -q "blocklist-toggle.conf" "$CONF_DIR/vpn-tunnel.conf" 2>/dev/null; then
  printf '\ninclude: "%s"\n' "$BLOCKLIST_TOGGLE" >> "$CONF_DIR/vpn-tunnel.conf"
fi

install -d -m 755 /etc/unbound/blocklists
if [ ! -f /etc/unbound/blocklists/blocklist.conf ]; then
  echo "# populated by scripts/update-blocklist.sh" > /etc/unbound/blocklists/blocklist.conf
fi

log_info "Anchor/kök güven noktası doğrulanıyor..."
unbound-anchor -a /var/lib/unbound/root.key 2>/dev/null || true

# Sabit bir /tmp yolu yerine mktemp: root olarak çalıştığımız için, yerel
# bir kullanıcının önceden oluşturduğu sembolik bağ bu yazmayı istediği
# dosyaya yönlendirebilirdi.
CHECKCONF_LOG="$(mktemp)"
if unbound-checkconf >"$CHECKCONF_LOG" 2>&1; then
  log_ok "unbound yapılandırması geçerli."
  rm -f "$CHECKCONF_LOG"
else
  log_err "unbound yapılandırması geçersiz:"
  cat "$CHECKCONF_LOG"
  rm -f "$CHECKCONF_LOG"
  exit 1
fi

install -d -m 755 /etc/systemd/system/unbound.service.d
install -m 644 ../systemd/unbound-after-tunnels.conf /etc/systemd/system/unbound.service.d/vpn-tunnel-order.conf
systemctl daemon-reload

systemctl enable --now unbound >/dev/null

log_ok "unbound $WG_RESOLVER_IP ve $OVPN_RESOLVER_IP üzerinde dinliyor (DoT yukarı akış: $UPSTREAM_DOT)."
log_info "Bu adreslerin var olması wg-quick@wg0 ve openvpn-server@ servislerine bağlıdır;"
log_info "systemd sıralamasının doğru kurulduğundan emin olun (bkz. systemd/unbound-after-tunnels.conf)."
log_info "Reklam/izleyici engelleme listesini indirmek için scripts/update-blocklist.sh çalıştırın."
