#!/usr/bin/env bash
# Deployment doctor: checks a live installation end to end and reports
# everything that's wrong, rather than stopping at the first problem.
#
#   sudo /opt/mobilvpn/scripts/verify.sh
#
# Exit code 0 = no hard failures (warnings may still be present), 1 = at
# least one check failed.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=./lib.sh
. ./lib.sh
# lib.sh turns on errexit; a doctor has to keep going after a failed check.
set +e

require_root

PASS=0
FAIL=0
WARN=0

ok()   { printf '  \033[0;32m✓\033[0m %s\n' "$1"; PASS=$((PASS + 1)); }
bad()  { printf '  \033[0;31m✗\033[0m %s\n' "$1"; FAIL=$((FAIL + 1)); }
warn() { printf '  \033[0;33m!\033[0m %s\n' "$1"; WARN=$((WARN + 1)); }
section() { printf '\n\033[1m%s\033[0m\n' "$1"; }

CONFIG_PATH="${VPN_API_CONFIG:-/etc/vpn-api/config.json}"

section "Yapılandırma"
if [ ! -f "$CONFIG_PATH" ]; then
  bad "config.json bulunamadı: $CONFIG_PATH — kurulum tamamlanmamış olabilir."
  echo
  echo "Özet: $PASS geçti, $WARN uyarı, $FAIL başarısız."
  exit 1
fi
if ! command -v vpn-api >/dev/null 2>&1; then
  bad "vpn-api ikili dosyası PATH'te yok (beklenen: /usr/local/bin/vpn-api)."
  exit 1
fi
if ! CFG_OUTPUT="$(vpn-api -print-config -config "$CONFIG_PATH" 2>&1)"; then
  bad "config.json ayrıştırılamadı: $CFG_OUTPUT"
  exit 1
fi
eval "$CFG_OUTPUT"
ok "config.json geçerli (düğüm: ${VPN_CFG_NODE_ID}, genel ad: ${VPN_CFG_PUBLIC_HOST})"

if [ "$(stat -c '%a' "$CONFIG_PATH")" = "600" ]; then
  ok "config.json izinleri 600"
else
  warn "config.json izinleri $(stat -c '%a' "$CONFIG_PATH") — fleet paylaşılan sırrını içerdiği için 600 olmalı"
fi

section "Servisler"
check_service() {
  local unit="$1"
  # stderr bastırılıyor: systemd'siz bir ortamda systemctl kendi hata
  # metnini basıp raporu okunmaz hale getiriyor.
  if systemctl is-active --quiet "$unit" 2>/dev/null; then
    ok "$unit çalışıyor"
  else
    bad "$unit ÇALIŞMIYOR (systemctl status $unit)"
  fi
}
check_service vpn-api
[ "$VPN_CFG_WG_ENABLED" = "true" ] && check_service "wg-quick@${VPN_CFG_WG_IFACE}"
if [ "$VPN_CFG_OVPN_ENABLED" = "true" ]; then
  check_service openvpn-server@server-udp
  check_service openvpn-server@server-tcp
fi
check_service unbound

section "Çekirdek ve yönlendirme"
if [ "$(sysctl -n net.ipv4.ip_forward 2>/dev/null)" = "1" ]; then
  ok "net.ipv4.ip_forward = 1"
else
  bad "net.ipv4.ip_forward = 0 — hiçbir tünel trafiği yönlendirilemez (scripts/10-sysctl-tuning.sh)"
fi
CC="$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null)"
if [ "$CC" = "bbr" ]; then
  ok "tıkanıklık kontrolü: bbr"
else
  warn "tıkanıklık kontrolü: ${CC:-bilinmiyor} (bbr bekleniyordu; hız etkilenebilir)"
fi

if [ "$VPN_CFG_WG_ENABLED" = "true" ]; then
  section "WireGuard"
  if ip link show "$VPN_CFG_WG_IFACE" >/dev/null 2>&1; then
    ok "$VPN_CFG_WG_IFACE arayüzü mevcut"

    # En kritik değişmez: istemcilere dağıtılan genel anahtar ile çekirdek
    # arayüzünün gerçekten kullandığı anahtar aynı olmalı. Ayrışırlarsa her
    # el sıkışma sessizce başarısız olur.
    IFACE_PUB="$(wg show "$VPN_CFG_WG_IFACE" public-key 2>/dev/null)"
    FILE_PUB="$(tr -d '[:space:]' < "$VPN_CFG_WG_KEYDIR/server_public.key" 2>/dev/null)"
    if [ -z "$IFACE_PUB" ] || [ -z "$FILE_PUB" ]; then
      bad "sunucu genel anahtarı okunamadı (arayüz: '${IFACE_PUB:-yok}', dosya: '${FILE_PUB:-yok}')"
    elif [ "$IFACE_PUB" = "$FILE_PUB" ]; then
      ok "sunucu anahtarı tutarlı (arayüz == $VPN_CFG_WG_KEYDIR/server_public.key)"
    else
      bad "ANAHTAR UYUŞMAZLIĞI: $VPN_CFG_WG_IFACE arayüzü '$IFACE_PUB' kullanıyor ama API istemcilere '$FILE_PUB' dağıtıyor — hiçbir tünel kurulamaz."
    fi

    IFACE_PORT="$(wg show "$VPN_CFG_WG_IFACE" listen-port 2>/dev/null)"
    if [ "$IFACE_PORT" = "$VPN_CFG_WG_PORT" ]; then
      ok "dinleme portu tutarlı ($IFACE_PORT)"
    else
      bad "port uyuşmazlığı: arayüz $IFACE_PORT dinliyor, config.json $VPN_CFG_WG_PORT diyor"
    fi

    PEER_COUNT="$(wg show "$VPN_CFG_WG_IFACE" peers 2>/dev/null | grep -c .)"
    ok "aktif peer sayısı: $PEER_COUNT"

    # Çift yığın için üç bileşenin (arayüz adresi, çekirdek yönlendirmesi,
    # API'nin reklamı) aynı fikirde olması gerekir. Ayrışırlarsa hiçbiri
    # hata vermez ama istemcinin IPv6 trafiği ya kara deliğe gider ya da
    # tünelin dışından gerçek adresle sızar.
    IFACE_HAS_V6=0
    ip -6 addr show dev "$VPN_CFG_WG_IFACE" scope global 2>/dev/null | grep -q "inet6" && IFACE_HAS_V6=1
    V6_FORWARDING="$(sysctl -n net.ipv6.conf.all.forwarding 2>/dev/null || echo 0)"

    if [ "$VPN_CFG_WG_IPV6_ENABLED" = "true" ]; then
      if [ "$IFACE_HAS_V6" = "1" ]; then
        ok "çift yığın: $VPN_CFG_WG_IFACE arayüzünde global IPv6 adresi var"
      else
        bad "config.json ipv6Enabled=true diyor ama $VPN_CFG_WG_IFACE arayüzünde IPv6 adresi yok — API istemcilere taşınamayan bir ::/0 rotası bildiriyor (scripts/20-wireguard-setup.sh)"
      fi
      if [ "$V6_FORWARDING" = "1" ]; then
        ok "IPv6 yönlendirme etkin"
      else
        bad "ipv6Enabled=true ama net.ipv6.conf.all.forwarding=0 — tünele giren IPv6 trafiği yönlendirilemez (scripts/10-sysctl-tuning.sh)"
      fi
      if nft list ruleset 2>/dev/null | grep -q "ip6 saddr.*masquerade"; then
        ok "IPv6 NAT66 kuralı mevcut"
      else
        bad "ipv6Enabled=true ama nftables'ta IPv6 masquerade kuralı yok (scripts/40-nftables-firewall.sh)"
      fi
    else
      ok "yalnızca IPv4 modu (::/0 reklam edilmiyor)"
      if [ "$IFACE_HAS_V6" = "1" ]; then
        warn "$VPN_CFG_WG_IFACE arayüzünde IPv6 adresi var ama config.json ipv6Enabled=false — istemcilere bildirilmeyen bir adres ailesi açık"
      fi
    fi
  else
    bad "$VPN_CFG_WG_IFACE arayüzü yok (wg-quick@${VPN_CFG_WG_IFACE} başlatılamamış olabilir)"
  fi

  KEYFILE="$VPN_CFG_WG_KEYDIR/server_private.key"
  if [ ! -f "$KEYFILE" ]; then
    bad "sunucu özel anahtarı yok: $KEYFILE (scripts/20-wireguard-setup.sh)"
  elif [ "$(stat -c '%a' "$KEYFILE")" = "600" ]; then
    ok "sunucu özel anahtarı izinleri 600"
  else
    bad "$KEYFILE izinleri $(stat -c '%a' "$KEYFILE") — 600 olmalı"
  fi
fi

if [ "$VPN_CFG_OVPN_ENABLED" = "true" ]; then
  section "OpenVPN"
  for f in "pki/ca.crt" "pki/issued/server.crt" "pki/private/server.key" "pki/dh.pem" "ta.key"; do
    if [ -s "$VPN_CFG_OVPN_EASYRSA/$f" ]; then
      ok "PKI dosyası mevcut: $f"
    else
      bad "PKI dosyası eksik: $VPN_CFG_OVPN_EASYRSA/$f (scripts/30-openvpn-setup.sh)"
    fi
  done

  # Süresi dolmuş bir CRL, OpenVPN'in TÜM bağlantıları reddetmesine yol açar
  # ve kolayca gözden kaçar — easy-rsa varsayılan CRL ömrü 180 gündür.
  CRL="$VPN_CFG_OVPN_SERVERDIR/crl.pem"
  if [ -s "$CRL" ]; then
    NEXT_UPDATE="$(openssl crl -in "$CRL" -noout -nextupdate 2>/dev/null | cut -d= -f2)"
    if [ -n "$NEXT_UPDATE" ]; then
      NEXT_EPOCH="$(date -d "$NEXT_UPDATE" +%s 2>/dev/null)"
      NOW_EPOCH="$(date +%s)"
      if [ -n "$NEXT_EPOCH" ] && [ "$NEXT_EPOCH" -le "$NOW_EPOCH" ]; then
        bad "CRL SÜRESİ DOLMUŞ ($NEXT_UPDATE) — OpenVPN tüm istemcileri reddeder. Düzeltme: cd $VPN_CFG_OVPN_EASYRSA && EASYRSA_BATCH=1 ./easyrsa gen-crl && install -m 644 pki/crl.pem $CRL"
      else
        DAYS_LEFT=$(( (NEXT_EPOCH - NOW_EPOCH) / 86400 ))
        if [ "$DAYS_LEFT" -lt 21 ]; then
          warn "CRL $DAYS_LEFT gün içinde doluyor — süresi dolarsa tüm OpenVPN bağlantıları reddedilir."
        else
          ok "CRL geçerli ($DAYS_LEFT gün kaldı)"
        fi
      fi
    else
      warn "CRL okunamadı: $CRL"
    fi
  else
    bad "CRL yok: $CRL — OpenVPN crl-verify ile başlatılamaz"
  fi

  MGMT_PASS_FILE="$VPN_CFG_OVPN_SERVERDIR/mgmt.pass"
  if [ -s "$MGMT_PASS_FILE" ]; then
    if [ "$(stat -c '%a' "$MGMT_PASS_FILE")" = "600" ]; then
      ok "management parola dosyası mevcut ve izinleri 600"
    else
      bad "$MGMT_PASS_FILE izinleri $(stat -c '%a' "$MGMT_PASS_FILE") — 600 olmalı"
    fi
  else
    warn "$MGMT_PASS_FILE yok — management arayüzü kimlik doğrulamasız çalışıyor olabilir (bkz. docs/SECURITY.md)"
  fi

  for addr in "$VPN_CFG_OVPN_MGMT_UDP" "$VPN_CFG_OVPN_MGMT_TCP"; do
    host="${addr%:*}"; port="${addr##*:}"
    if timeout 2 bash -c "echo > /dev/tcp/$host/$port" 2>/dev/null; then
      ok "management soketi erişilebilir: $addr"
    else
      bad "management soketine ulaşılamadı: $addr (telemetri ve zorla kapatma çalışmaz)"
    fi
  done
fi

if [ "$VPN_CFG_IKEV2_ENABLED" = "true" ]; then
  section "IKEv2 (strongSwan)"
  check_service strongswan

  # Ubuntu enables the legacy starter by default and both bind UDP 500/4500,
  # so a leftover starter silently keeps the swanctl daemon from working.
  if systemctl is-enabled strongswan-starter.service >/dev/null 2>&1; then
    bad "strongswan-starter.service hâlâ etkin — swanctl tabanlı strongswan.service ile 500/4500 portu için çakışır (systemctl disable --now strongswan-starter)"
  else
    ok "eski strongswan-starter servisi devre dışı"
  fi

  for f in "$VPN_CFG_IKEV2_CERT" "$VPN_CFG_IKEV2_KEY"; do
    if [ -s "$f" ]; then
      ok "sertifika/anahtar mevcut: $f"
    else
      bad "eksik: $f (scripts/35-ikev2-setup.sh)"
    fi
  done

  # A client verifies the server against a SAN in this certificate; if the
  # configured identity isn't in there, every connection fails auth.
  if [ -s "$VPN_CFG_IKEV2_CERT" ] && [ -n "$VPN_CFG_IKEV2_SERVER_ID" ]; then
    if ipsec pki --print --in "$VPN_CFG_IKEV2_CERT" 2>/dev/null | grep -q "altNames:.*$VPN_CFG_IKEV2_SERVER_ID"; then
      ok "sunucu sertifikasının SAN'ı yapılandırılmış kimlikle eşleşiyor ($VPN_CFG_IKEV2_SERVER_ID)"
    else
      bad "sertifikanın SAN'ı '$VPN_CFG_IKEV2_SERVER_ID' içermiyor — istemciler sunucuyu doğrulayamaz (35-ikev2-setup.sh'ı doğru VPN_API_DOMAIN ile tekrar çalıştırın)"
    fi
  fi

  if swanctl --stats >/dev/null 2>&1; then
    ok "swanctl daemon'a bağlanabiliyor"
    LOADED="$(swanctl --list-conns --raw 2>/dev/null | grep -o 'sess_[A-Za-z0-9_-]*' | sort -u | wc -l)"
    ok "yüklü oturum bağlantısı: $LOADED"
  else
    bad "swanctl daemon'a ulaşamıyor — vpn-api oturum açamaz/telemetri okuyamaz (journalctl -u strongswan)"
  fi

  if nft list ruleset 2>/dev/null | grep -q "udp dport { 500, 4500 }"; then
    ok "IKEv2 portları (500/4500) güvenlik duvarında açık"
  else
    warn "nftables'ta 500/4500 kuralı görünmüyor — istemciler bağlanamayabilir (scripts/40-nftables-firewall.sh)"
  fi
fi

section "Güvenlik duvarı (nftables)"
RULESET="$(nft list ruleset 2>/dev/null)"
if [ -z "$RULESET" ]; then
  bad "nftables kural seti boş — NAT yok, istemciler internete çıkamaz (scripts/40-nftables-firewall.sh)"
else
  if grep -q "masquerade" <<< "$RULESET"; then
    ok "NAT/masquerade kuralı mevcut"
  else
    bad "masquerade kuralı yok — tünel istemcileri internete çıkamaz"
  fi
  if grep -qE "chain forward" <<< "$RULESET" && grep -qE "hook forward.*policy drop" <<< "$RULESET"; then
    ok "forward zinciri varsayılan-reddet"
  else
    warn "forward zincirinin varsayılan politikası drop değil — istemci izolasyonu zayıflamış olabilir"
  fi
  if grep -q "udp dport 53 drop" <<< "$RULESET"; then
    ok "DNS sızıntısı engeli etkin (tünelden dışarı 53 kapalı)"
  else
    warn "tünel arayüzlerinden dış DNS'e çıkış engellenmiyor — DNS sızıntısı mümkün"
  fi
fi

section "DNS (unbound)"
if command -v ss >/dev/null 2>&1; then
  DNS_BINDS="$(ss -lunH "sport = :53" 2>/dev/null | awk '{print $4}')"
  if grep -qE '^(0\.0\.0\.0|\*):53$' <<< "$DNS_BINDS"; then
    bad "unbound 0.0.0.0:53 dinliyor — AÇIK ÇÖZÜMLEYİCİ riski (DDoS yansıtma). vpn-tunnel.conf içindeki interface satırlarını kontrol edin."
  elif [ -n "$DNS_BINDS" ]; then
    ok "DNS yalnızca belirli adreslerde dinliyor: $(tr '\n' ' ' <<< "$DNS_BINDS")"
  else
    warn "53/udp dinleyen bulunamadı — unbound çalışmıyor olabilir"
  fi
fi
if command -v dig >/dev/null 2>&1; then
  if dig +short +time=3 +tries=1 "@$VPN_CFG_DNS_RESOLVER" example.com A >/dev/null 2>&1; then
    ok "çözümleyici $VPN_CFG_DNS_RESOLVER yanıt veriyor"
  else
    bad "çözümleyici $VPN_CFG_DNS_RESOLVER yanıt vermiyor — istemcilerin DNS'i çalışmaz (nftables dış DNS'i engellediği için tamamen kopar)"
  fi
else
  warn "dig kurulu değil; çözümleyici canlı testi atlandı (apt install dnsutils)"
fi

section "Yönetim API'si"
SCHEME="http"; CURL_OPTS=(-s -o /dev/null -w '%{http_code}' --max-time 5)
if [ "$VPN_CFG_TLS_ENABLED" = "true" ]; then
  SCHEME="https"
  CURL_OPTS+=(-k) # sertifika geçerliliği aşağıda ayrıca kontrol ediliyor
fi
PORT="${VPN_CFG_LISTEN_ADDR##*:}"
CODE="$(curl "${CURL_OPTS[@]}" "$SCHEME://127.0.0.1:$PORT/api/v1/health" 2>/dev/null)"
if [ "$CODE" = "200" ]; then
  ok "GET /api/v1/health → 200 ($SCHEME://127.0.0.1:$PORT)"
else
  bad "GET /api/v1/health → ${CODE:-yanıt yok} — Android istemcisi bağlanamaz (journalctl -u vpn-api)"
fi

if [ "$VPN_CFG_TLS_ENABLED" = "true" ]; then
  if [ -s "$VPN_CFG_TLS_CERT" ]; then
    END_DATE="$(openssl x509 -in "$VPN_CFG_TLS_CERT" -noout -enddate 2>/dev/null | cut -d= -f2)"
    END_EPOCH="$(date -d "$END_DATE" +%s 2>/dev/null)"
    DAYS_LEFT=$(( (END_EPOCH - $(date +%s)) / 86400 ))
    if [ "$DAYS_LEFT" -lt 0 ]; then
      bad "TLS sertifikasının süresi dolmuş ($END_DATE)"
    elif [ "$DAYS_LEFT" -lt 21 ]; then
      warn "TLS sertifikası $DAYS_LEFT gün içinde doluyor (certbot yenilemeli; vpn-api yeniden başlatma gerektirmez)"
    else
      ok "TLS sertifikası geçerli ($DAYS_LEFT gün kaldı)"
    fi
  else
    bad "TLS etkin ama sertifika dosyası yok: $VPN_CFG_TLS_CERT"
  fi
fi

# --- Hız: bildirilen MTU, arayüzün gerçek MTU'suyla uyuşmalı ---
# wg-quick arayüzü "varsayılan rota MTU'su - 80" ile açar; config.json'daki
# değer ise istemcinin kendi TUN'una uyguladığıdır. Ayrışırlarsa istemci,
# yola sığmayan paketler üretir: parçalanma ya da sessiz düşme, yani
# "bağlanıyor ama yavaş".
if [ "$VPN_CFG_WG_ENABLED" = "true" ]; then
  LIVE_MTU="$(ip -o link show "$VPN_CFG_WG_IFACE" 2>/dev/null |
    awk '{for (i = 1; i <= NF; i++) if ($i == "mtu") { print $(i + 1); exit }}')"
  if [ -z "$LIVE_MTU" ]; then
    warn "$VPN_CFG_WG_IFACE arayüzü yok; MTU karşılaştırması yapılamadı"
  elif [ "$LIVE_MTU" = "$VPN_CFG_WG_MTU" ]; then
    ok "MTU tutarlı: config.json ve $VPN_CFG_WG_IFACE ikisi de $LIVE_MTU"
  else
    bad "MTU uyuşmuyor: config.json istemciye $VPN_CFG_WG_MTU bildiriyor ama $VPN_CFG_WG_IFACE $LIVE_MTU — istemci yola sığmayan paket üretir (VPN_WG_MTU=$LIVE_MTU ile 60-vpn-api-service.sh)"
  fi
fi

section "Gizlilik (no-logs)"
# Bu bölüm ürünün gizlilik vaadini denetler: hiçbir bileşen istemcinin
# gerçek IP'sini kalıcı ya da okunabilir bir yere yazmamalı. Ölçümler ve
# gerekçe için docs/SECURITY.md.

OVPN_SERVER_DIR="${VPN_CFG_OVPN_SERVERDIR:-/etc/openvpn/server}"
for conf in "$OVPN_SERVER_DIR"/server-udp.conf "$OVPN_SERVER_DIR"/server-tcp.conf; do
  [ -f "$conf" ] || continue
  VERB="$(awk '/^[[:space:]]*verb[[:space:]]+[0-9]+/ {print $2}' "$conf" | tail -n1)"
  if [ "${VERB:-3}" = "0" ]; then
    ok "$(basename "$conf"): verb 0 (istemci IP'si loglanmıyor)"
  else
    bad "$(basename "$conf"): verb ${VERB:-ayarsız} — bu seviye istemcinin gerçek IP'sini tünel IP'siyle eşleştirip loglar (verb 0 olmalı)"
  fi
done

# Ubuntu'nun hazır birimi ExecStart'a bir --status dosyası gömer; drop-in
# onu kaldırır. Hem drop-in'i hem de dosyanın gerçekten yokluğunu kontrol
# ediyoruz: biri olmadan diğeri yanıltıcı olurdu.
if [ -f /etc/systemd/system/openvpn-server@.service.d/no-status.conf ]; then
  ok "openvpn --status drop-in'i kurulu"
else
  bad "openvpn --status drop-in'i yok — birim, gerçek IP ↔ tünel IP eşlemesi içeren bir durum dosyası yazıyor olabilir (30-openvpn-setup.sh yeniden çalıştırın)"
fi
LEAKY_STATUS=0
for f in /run/openvpn-server/status-*.log; do
  [ -f "$f" ] || continue
  if grep -qE '^(CLIENT_LIST|ROUTING_TABLE),' "$f" 2>/dev/null; then
    bad "$f bağlı istemcilerin gerçek IP'lerini içeriyor — birim hâlâ --status ile çalışıyor (systemctl restart openvpn-server@server-udp openvpn-server@server-tcp)"
    LEAKY_STATUS=1
  fi
done
[ "$LEAKY_STATUS" -eq 0 ] && ok "gerçek IP içeren OpenVPN durum dosyası yok"

if [ "${VPN_CFG_IKEV2_ENABLED:-false}" = "true" ]; then
  IKE_LVL="$(awk '/^[[:space:]]*default[[:space:]]*=/ {print $3}' /etc/strongswan.d/charon-systemd.conf 2>/dev/null | tail -n1)"
  if [ "${IKE_LVL:-1}" = "-1" ]; then
    ok "charon günlükleme kapalı (eş IP'leri journal'a yazılmıyor)"
  else
    warn "charon günlük seviyesi ${IKE_LVL:-varsayılan 1} — bu seviyede IKE_SA kurulurken eş IP'si loglanır (VPN_IKEV2_LOGLEVEL=-1 ile 35-ikev2-setup.sh)"
  fi
fi

for opt in log-queries log-replies; do
  if grep -rqs "$opt: no" /etc/unbound/unbound.conf.d/; then
    ok "unbound $opt: no"
  else
    bad "unbound $opt kapalı değil — DNS sorguları loglanıyor olabilir"
  fi
done

section "Özet"
printf '  %d geçti, %d uyarı, %d başarısız\n\n' "$PASS" "$WARN" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  log_err "Dağıtımda düzeltilmesi gereken sorunlar var."
  exit 1
fi
log_ok "Tüm kritik kontroller geçti."
