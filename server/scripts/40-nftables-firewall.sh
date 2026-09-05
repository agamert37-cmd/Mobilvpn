#!/usr/bin/env bash
# Renders nft/vpn.nft.tmpl into /etc/nftables.conf with this host's actual
# WAN interface/ports, then applies it. A firewall change on a remote box
# can lock you out permanently, so this asks for confirmation and tells you
# exactly how to verify before closing your current session.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=./lib.sh
. ./lib.sh

require_root

WAN_IFACE="${VPN_WAN_IFACE:-$(detect_wan_iface)}"
SSH_PORT="${VPN_SSH_PORT:-22}"
WG_IFACE="${VPN_WG_IFACE:-wg0}"
WG_PORT="${VPN_WG_PORT:-51820}"
WG_SUBNET="${VPN_WG_SUBNET:-10.66.0.0/16}"
WG_SUBNET_V6="${VPN_WG_SUBNET_V6:-fd00:66::/64}"
OVPN_UDP_PORT="${VPN_OVPN_UDP_PORT:-1194}"
OVPN_TCP_PORT="${VPN_OVPN_TCP_PORT:-443}"
OVPN_UDP_DEV="${VPN_OVPN_UDP_DEV:-tun-udp}"
OVPN_TCP_DEV="${VPN_OVPN_TCP_DEV:-tun-tcp}"
OVPN_SUBNET="${VPN_OVPN_SUBNET:-10.77.0.0/16}"
API_PUBLIC_PORT="${VPN_API_PUBLIC_PORT:-8443}"

log_info "Tespit edilen WAN arayüzü: $WAN_IFACE"
log_warn "Bu betik mevcut güvenlik duvarı kurallarının TAMAMINI değiştirecek (flush ruleset)."
log_warn "SSH portu: $SSH_PORT — bunun sunucunuzun gerçek SSH portuyla eşleştiğinden emin olun,"
log_warn "aksi halde kendinizi dışarıda bırakabilirsiniz."
if ! confirm "Devam edilsin mi?"; then
  log_info "İptal edildi."
  exit 0
fi

TMPL="../nft/vpn.nft.tmpl"
OUT=/etc/nftables.conf
BACKUP="/etc/nftables.conf.bak.$(date +%s)"
[ -f "$OUT" ] && cp "$OUT" "$BACKUP" && log_info "Önceki kural seti yedeklendi: $BACKUP"

render_template "$TMPL" "$OUT" \
  "WAN_IFACE=$WAN_IFACE" \
  "SSH_PORT=$SSH_PORT" \
  "WG_IFACE=$WG_IFACE" \
  "WG_PORT=$WG_PORT" \
  "WG_SUBNET=$WG_SUBNET" \
  "WG_SUBNET_V6=$WG_SUBNET_V6" \
  "OVPN_UDP_PORT=$OVPN_UDP_PORT" \
  "OVPN_TCP_PORT=$OVPN_TCP_PORT" \
  "OVPN_UDP_DEV=$OVPN_UDP_DEV" \
  "OVPN_TCP_DEV=$OVPN_TCP_DEV" \
  "OVPN_SUBNET=$OVPN_SUBNET" \
  "API_PUBLIC_PORT=$API_PUBLIC_PORT"

log_info "Kural seti sözdizimi kontrol ediliyor..."
if ! nft -c -f "$OUT"; then
  log_err "Kural seti geçersiz; hiçbir değişiklik uygulanmadı. Önceki hali: ${BACKUP:-yok}"
  exit 1
fi

log_warn "Kurallar 5 saniye içinde uygulanacak. ŞİMDİ İKİNCİ BİR TERMİNALDEN SSH ERİŞİMİNİ TEST ETMEYE HAZIR OLUN."
sleep 5
nft -f "$OUT"
systemctl enable nftables >/dev/null 2>&1 || true

log_ok "nftables kuralları uygulandı."
log_warn "ÖNEMLİ: Bu terminali kapatmadan önce YENİ bir SSH oturumuyla bağlanabildiğinizi doğrulayın."
log_info "Bir hata olursa: nft -f $BACKUP  (varsa) önceki kurallara döner."
