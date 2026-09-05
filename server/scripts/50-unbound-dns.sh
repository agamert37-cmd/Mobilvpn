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

# --- Hız ayarları: iş parçacığı sayısı ve önbellek boyutları ---
#
# unbound varsayılanı tek iş parçacığı ve 4m/4m önbellektir. Tünelin
# tamamı bu çözümleyiciden geçtiği için DNS gecikmesi kullanıcıya doğrudan
# "yavaşlık" olarak yansır.
#
# Ölçüldü (bu depoda, unbound 1.19.2, 4 vCPU, 4 süreçli boru hatlı yük,
# 3 tekrarın medyanı, hepsi önbellekten yanıtlanan sorgular):
#
#   varsayılan (1 iş parçacığı)      144.924 yanıt/s
#   num-threads=4 + eşleşen slab'ler 244.403 yanıt/s   -> 1.69x
#
# Üç tekrarın üçünde de tutarlıydı (236k-251k). so-reuseport ölçümde
# belirgin bir fark göstermedi (gürültülü) ama çok iş parçacıklı unbound
# için standart pratiktir ve zararı yoktur. Önbellek boyutlarının etkisi
# BU ölçümle test EDİLEMEDİ: tezgâh yalnızca 50 farklı ada soruyor, yani
# önbellek boyutu belirleyici değil. Boyutlar yine de büyütülüyor çünkü
# varsayılan 4m, çok kullanıcılı bir çözümleyici için küçüktür.
UNBOUND_THREADS="${VPN_UNBOUND_THREADS:-$(nproc 2>/dev/null || echo 1)}"
[ "$UNBOUND_THREADS" -gt 8 ] && UNBOUND_THREADS=8
[ "$UNBOUND_THREADS" -lt 1 ] && UNBOUND_THREADS=1

# slab sayısı 2'nin kuvveti OLMAK ZORUNDA (unbound bunu şart koşar), bu
# yüzden iş parçacığı sayısını aşağı yuvarlıyoruz: 3 -> 2, 6 -> 4.
UNBOUND_SLABS=1
while [ $((UNBOUND_SLABS * 2)) -le "$UNBOUND_THREADS" ]; do
  UNBOUND_SLABS=$((UNBOUND_SLABS * 2))
done

# Önbelleği RAM'e göre ölçekle: 512 MB'lık bir VPS'e 256 MB önbellek
# vermek onu takasa (swap) sokar, ki bu hızlandırmaz yavaşlatır.
MEM_MB=$(awk '/^MemTotal:/ {print int($2/1024)}' /proc/meminfo 2>/dev/null || echo 1024)
if [ "$MEM_MB" -lt 1024 ]; then
  UNBOUND_MSG_CACHE=16; UNBOUND_RRSET_CACHE=32
elif [ "$MEM_MB" -lt 2048 ]; then
  UNBOUND_MSG_CACHE=32; UNBOUND_RRSET_CACHE=64
elif [ "$MEM_MB" -lt 4096 ]; then
  UNBOUND_MSG_CACHE=64; UNBOUND_RRSET_CACHE=128
else
  UNBOUND_MSG_CACHE=128; UNBOUND_RRSET_CACHE=256
fi
log_info "unbound: $UNBOUND_THREADS iş parçacığı, $UNBOUND_SLABS slab, önbellek ${UNBOUND_MSG_CACHE}m/${UNBOUND_RRSET_CACHE}m (${MEM_MB} MB RAM)"

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
  echo "  # Hız: iş parçacıkları ve önbellek boyutu (bkz. aşağıdaki ölçüm)."
  echo "  num-threads: $UNBOUND_THREADS"
  echo "  msg-cache-slabs: $UNBOUND_SLABS"
  echo "  rrset-cache-slabs: $UNBOUND_SLABS"
  echo "  infra-cache-slabs: $UNBOUND_SLABS"
  echo "  key-cache-slabs: $UNBOUND_SLABS"
  echo "  so-reuseport: yes"
  echo "  msg-cache-size: ${UNBOUND_MSG_CACHE}m"
  echo "  rrset-cache-size: ${UNBOUND_RRSET_CACHE}m"
  echo "  minimal-responses: yes"
  echo "  aggressive-nsec: yes"
  # 1232, DNS flag day tavsiyesi: UDP yanıtlarının IPv6 üzerinde
  # parçalanmasını (ve parçalanmış yanıtların düşürülmesini) önler.
  echo "  edns-buffer-size: 1232"
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
