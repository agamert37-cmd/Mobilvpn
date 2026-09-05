#!/usr/bin/env bash
# Orchestrates a full install: copies this server/ tree to a permanent
# location (/opt/mobilvpn by default — systemd units and the blocklist
# timer reference paths there forever, not wherever you happened to `git
# clone`), then runs every numbered script in scripts/ in order.
#
# Usage:
#   sudo VPN_API_DOMAIN=vpn.example.com ./install.sh
#   sudo ./install.sh --domain vpn.example.com --yes
#
# See README.md for the full list of VPN_* environment variables each
# individual script accepts (ports, subnets, node id/region, etc.) — any of
# them can be set before running this orchestrator.
set -euo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${VPN_INSTALL_DIR:-/opt/mobilvpn}"

while [ $# -gt 0 ]; do
  case "$1" in
    --domain) VPN_API_DOMAIN="$2"; shift 2 ;;
    --yes|-y) VPN_ASSUME_YES=1; shift ;;
    --skip-firewall) SKIP_FIREWALL=1; shift ;;
    --skip-tls) SKIP_TLS=1; shift ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "Bilinmeyen seçenek: $1" >&2
      exit 1
      ;;
  esac
done
export VPN_API_DOMAIN VPN_ASSUME_YES

if [ "$(id -u)" -ne 0 ]; then
  echo "Bu betik root olarak çalıştırılmalıdır (sudo ile deneyin)." >&2
  exit 1
fi

if [ -z "${VPN_API_DOMAIN:-}" ]; then
  echo "VPN_API_DOMAIN gerekli (ör. --domain vpn.example.com veya VPN_API_DOMAIN=... ortam değişkeni)." >&2
  echo "Bu, Android uygulamasının bağlanacağı ve TLS sertifikasının alınacağı genel ana bilgisayar adıdır." >&2
  exit 1
fi

echo "[*] $SELF_DIR -> $INSTALL_DIR kopyalanıyor..."
mkdir -p "$INSTALL_DIR"
cp -r "$SELF_DIR/." "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR"/scripts/*.sh "$INSTALL_DIR"/install.sh

cd "$INSTALL_DIR/scripts"

./00-prereqs.sh
./10-sysctl-tuning.sh
./20-wireguard-setup.sh
./30-openvpn-setup.sh
./50-unbound-dns.sh
./update-blocklist.sh || echo "[!] İlk engelleme listesi indirmesi başarısız oldu; daha sonra scripts/update-blocklist.sh ile tekrar deneyin."

if [ "${SKIP_TLS:-0}" != "1" ]; then
  ./70-tls-certbot.sh
else
  echo "[!] --skip-tls: TLS sertifikası atlandı. vpn-api'yi başlatmadan önce config.json içindeki tls bölümünü elle ayarlayın."
fi

./60-vpn-api-service.sh

if [ "${SKIP_FIREWALL:-0}" != "1" ]; then
  echo
  echo "[!] Sırada güvenlik duvarı (nftables) var. Bu adım YANLIŞ yapılandırılırsa SSH erişiminizi"
  echo "    kaybedebilirsiniz. Devam etmeden önce SSH portunuzu (VPN_SSH_PORT) doğrulayın."
  ./40-nftables-firewall.sh
else
  echo "[!] --skip-firewall: güvenlik duvarı kuralları atlandı. Tünelin dışarıdan erişilebilir olması için"
  echo "    scripts/40-nftables-firewall.sh dosyasını elle çalıştırmanız gerekir."
fi

install -d -m 755 /etc/systemd/system/vpn-blocklist-update.timer.d 2>/dev/null || true
install -m 644 "$INSTALL_DIR/systemd/vpn-blocklist-update.service" /etc/systemd/system/vpn-blocklist-update.service
install -m 644 "$INSTALL_DIR/systemd/vpn-blocklist-update.timer" /etc/systemd/system/vpn-blocklist-update.timer
systemctl daemon-reload
systemctl enable --now vpn-blocklist-update.timer >/dev/null 2>&1 || true

echo
echo "=================================================================="
echo " Kurulum tamamlandı."
echo " Sunucu API uç noktası : https://$VPN_API_DOMAIN:${VPN_API_PUBLIC_PORT:-8443}/"
echo " WireGuard genel anahtarı: $(cat /etc/wireguard/server_public.key 2>/dev/null || echo '(henüz yok)')"
echo " Servis durumu          : systemctl status vpn-api wg-quick@wg0 openvpn-server@server-udp openvpn-server@server-tcp unbound"
echo " Günlükler               : journalctl -u vpn-api -f"
echo "=================================================================="
echo
echo "[!] ÖNEMLİ: Kapatmadan önce YENİ bir terminalden SSH erişiminizi doğrulayın."
