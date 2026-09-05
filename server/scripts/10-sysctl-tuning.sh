#!/usr/bin/env bash
# Kernel network tuning for tunnel throughput (BBR + larger buffers) and for
# not leaking/accepting things a VPN gateway shouldn't (ICMP redirects,
# source routing, martian logging left off by policy — see docs/SECURITY.md
# for why we deliberately don't turn on kernel-level connection logging).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=./lib.sh
. ./lib.sh

require_root

# Aynı algılamayı 20-wireguard-setup.sh ve 60-vpn-api-service.sh de
# kullanır: üçü ayrışırsa tünelde IPv6 adresi olup yönlendirme olmaz (ya da
# tersi) ve trafik sessizce kaybolur.
ENABLE_IPV6_FORWARDING="${VPN_ENABLE_IPV6_FORWARDING:-$(detect_ipv6_support)}"

SYSCTL_FILE=/etc/sysctl.d/99-vpn-tunnel.conf
log_info "Yazılıyor: $SYSCTL_FILE"

cat > "$SYSCTL_FILE" <<EOF
# Managed by Mobilvpn server/scripts/10-sysctl-tuning.sh — elle düzenlemek
# yerine bu betiği tekrar çalıştırın.

# --- Hız: tünel trafiğini yönlendirebilmek + modern tıkanıklık kontrolü ---
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = $ENABLE_IPV6_FORWARDING
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr

# Yüksek bant genişliği * gecikme çarpımı bağlantılarda darboğazı önlemek
# için soket tampon üst sınırlarını büyüt (WireGuard/OpenVPN UDP taşıması ve
# yönetim API'sinin HTTPS bağlantıları için).
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.core.rmem_default = 1048576
net.core.wmem_default = 1048576
net.core.netdev_max_backlog = 4096
net.core.somaxconn = 4096

# MASQUERADE/NAT altında çok sayıda eşzamanlı istemci bağlantısını
# düşürmemek için conntrack tablosunu büyüt.
net.netfilter.nf_conntrack_max = 262144

# --- Gizlilik/güvenlik: bir VPN ağ geçidinin yapmaması gereken şeyler ---
# ICMP yönlendirmelerini gönderme/kabul etme (MITM/route injection vektörü).
net.ipv4.conf.all.send_redirects = 0
net.ipv4.conf.default.send_redirects = 0
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv6.conf.all.accept_redirects = 0
net.ipv6.conf.default.accept_redirects = 0

# Kaynak yönlendirmeli paketleri kabul etme (spoofing vektörü).
net.ipv4.conf.all.accept_source_route = 0
net.ipv4.conf.default.accept_source_route = 0
net.ipv6.conf.all.accept_source_route = 0
net.ipv6.conf.default.accept_source_route = 0

# Ters yol filtresi: tek varsayılan rotalı basit bir VPN sunucusunda "strict"
# (1) sahte kaynak IP'leri güvenli şekilde eler. Çoklu WAN / politika
# yönlendirmesi kullanıyorsanız 2 (loose) yapın.
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.default.rp_filter = 1

# "Martian" paketleri (sahte/geçersiz kaynaklı) kaydetme — bu, istemci IP
# adreslerini diske yazan bir günlükleme türüdür ve no-logs politikamızla
# çelişir; bırakma kararı sessizce alınır.
net.ipv4.conf.all.log_martians = 0
EOF

# `sysctl --system` reloads every file under /etc/sysctl.d, not just ours;
# some hardened/virtualized kernels (containers, certain minimal VPS
# images) don't expose every knob we'd like (BBR/fq in particular). Don't
# let an optional speed tunable being unavailable abort the whole install —
# but DO hard-fail if ip_forward, the one setting a VPN gateway cannot
# function without, didn't actually take.
SYSCTL_LOG="$(mktemp)"
trap 'rm -f "$SYSCTL_LOG"' EXIT
if ! sysctl --system >"$SYSCTL_LOG" 2>&1; then
  log_warn "Bazı sysctl anahtarları bu çekirdekte uygulanamadı:"
  grep -i "error\|no such" "$SYSCTL_LOG" | sed 's/^/    /' || true
fi

if [ "$(sysctl -n net.ipv4.ip_forward)" != "1" ]; then
  log_err "net.ipv4.ip_forward etkinleştirilemedi — bu olmadan hiçbir tünel trafiği yönlendirilemez."
  exit 1
fi

CC="$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || echo bilinmiyor)"
if [ "$CC" = "bbr" ]; then
  log_ok "sysctl ayarları uygulandı (tıkanıklık kontrolü: BBR)."
else
  log_warn "tcp_congestion_control = $CC (bu çekirdek BBR sağlamıyor olabilir; yönlendirme yine de çalışır, sadece azami verim düşebilir)."
fi

if [ "$ENABLE_IPV6_FORWARDING" = "0" ]; then
  log_warn "Bu ana bilgisayarda global IPv6 tespit edilmedi; tünel yalnızca IPv4 taşıyacak."
  log_warn "İstemci cihazın yerel IPv6 bağlantısı varsa bunu VPN dışından sızdırabilir (docs/SECURITY.md)."
else
  log_ok "IPv6 yönlendirme etkin; tünel çift yığın olacak ve IPv6 sızıntısı kapanacak."
fi
