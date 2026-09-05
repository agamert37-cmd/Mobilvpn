#!/usr/bin/env bash
# Orchestrates a full install: copies this server/ tree to a permanent
# location (/opt/mobilvpn by default — systemd units and the blocklist
# timer reference paths there forever, not wherever you happened to `git
# clone`), then runs every numbered script in scripts/ in order.
#
# Usage:
#   sudo ./install.sh                                   (soru-cevap sihirbazı)
#   sudo ./install.sh --domain vpn.example.com --yes    (sorusuz, otomatik)
#   sudo ./install.sh --domain vpn.example.com --with-ikev2
#
# Hiçbir argüman verilmezse ve bir terminal varsa kurulum sihirbazı çalışır;
# eksik ayarları sorar. --yes ile tamamen sorusuz ilerler.
#
# See README.md for the full list of VPN_* environment variables each
# individual script accepts (ports, subnets, node id/region, etc.) — any of
# them can be set before running this orchestrator.
set -euo pipefail

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${VPN_INSTALL_DIR:-/opt/mobilvpn}"

# usage prints the header comment block only: everything from the line
# after the shebang up to the first line that isn't a comment. `grep '^#'`
# would also dump every explanatory comment further down the file.
usage() {
  awk 'NR == 1 { next } /^#/ { sub(/^# ?/, ""); print; next } { exit }' "$0"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --domain)
      [ $# -ge 2 ] || { echo "--domain bir alan adı bekliyor (ör. --domain vpn.example.com)." >&2; exit 1; }
      VPN_API_DOMAIN="$2"; shift 2 ;;
    --yes|-y) VPN_ASSUME_YES=1; shift ;;
    --skip-firewall) SKIP_FIREWALL=1; shift ;;
    --skip-tls) SKIP_TLS=1; shift ;;
    --with-ikev2) VPN_ENABLE_IKEV2=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *)
      echo "Bilinmeyen seçenek: $1" >&2
      exit 1
      ;;
  esac
done
export VPN_API_DOMAIN VPN_ASSUME_YES VPN_ENABLE_IKEV2

if [ "$(id -u)" -ne 0 ]; then
  echo "Bu betik root olarak çalıştırılmalıdır (sudo ile deneyin)." >&2
  exit 1
fi

# shellcheck source=scripts/lib.sh
. "$SELF_DIR/scripts/lib.sh"

# Wizard: asks for whatever wasn't passed on the command line. Runs
# whenever there's a terminal and no --yes, so the piped one-liner install
# (curl ... | sudo bash) is interactive too — the prompts read /dev/tty,
# not stdin, which is where the script itself is arriving from.
if [ "${VPN_ASSUME_YES:-0}" != "1" ] && have_tty; then
  printf '\n\033[1m  Mobilvpn — Sunucu Kurulum Sihirbazı\033[0m\n'
  printf '  Boş bırakırsanız köşeli parantezdeki varsayılan kullanılır.\n\n'

  if [ -z "${VPN_API_DOMAIN:-}" ]; then
    DETECTED_IP="$(detect_public_ip)"
    printf '  Android uygulamasının bağlanacağı alan adı. DNS kaydı bu sunucuya\n'
    printf "  (%s) işaret etmeli; Let's Encrypt sertifikası buna göre alınır.\n\n" "${DETECTED_IP:-genel IP tespit edilemedi}"
    while [ -z "${VPN_API_DOMAIN:-}" ]; do
      ask VPN_API_DOMAIN "Alan adı (ör. vpn.example.com)" ""
      if [ -z "$VPN_API_DOMAIN" ]; then
        printf '  \033[0;33mAlan adı olmadan geçerli bir TLS sertifikası alınamaz.\033[0m\n'
        if ask_yes_no "  Alan adı olmadan devam edilsin mi (TLS atlanır, Android istemcisi bağlanamaz)" "n"; then
          SKIP_TLS=1
          VPN_API_DOMAIN="${DETECTED_IP:-127.0.0.1}"
          printf '  \033[0;33mTLS atlanıyor; sunucu %s üzerinde düz HTTP dinleyecek.\033[0m\n' "$VPN_API_DOMAIN"
          break
        fi
      fi
    done
  fi

  [ -n "${VPN_NODE_REGION:-}" ] || ask VPN_NODE_REGION "Bu düğümün bölge etiketi (uygulamada görünür)" "Unconfigured Region"
  [ -n "${VPN_NODE_ID:-}" ] || ask VPN_NODE_ID "Düğüm kimliği" "$(hostname -s 2>/dev/null || echo vpn_node_01)"

  # Read the port sshd actually uses rather than trusting memory: a wrong
  # answer here is what locks people out when the firewall goes up.
  if [ -z "${VPN_SSH_PORT:-}" ]; then
    DETECTED_SSH="$(detect_ssh_port)"
    printf '\n  Güvenlik duvarı yalnızca bu SSH portunu açık bırakacak.\n'
    printf '  Yapılandırmadan tespit edilen: \033[1m%s\033[0m\n' "$DETECTED_SSH"
    ask VPN_SSH_PORT "SSH portu" "$DETECTED_SSH"
  fi

  printf '\n  Protokoller — WireGuard her zaman kurulur (en hızlısı).\n'
  if [ -z "${VPN_ENABLE_IKEV2:-}" ]; then
    if ask_yes_no "  IKEv2/IPsec de kurulsun mu (mobil dolaşım, işletim sisteminin yerleşik istemcisi)" "n"; then
      VPN_ENABLE_IKEV2=1
    fi
  fi

  printf '\n\033[1m  Özet\033[0m\n'
  printf '    Alan adı      : %s\n' "$VPN_API_DOMAIN"
  printf '    Düğüm         : %s (%s)\n' "${VPN_NODE_ID:-otomatik}" "${VPN_NODE_REGION:-}"
  printf '    SSH portu     : %s\n' "${VPN_SSH_PORT}"
  printf '    Protokoller   : WireGuard, OpenVPN%s\n' "$([ "${VPN_ENABLE_IKEV2:-0}" = "1" ] && echo ', IKEv2' || echo '')"
  printf '    TLS           : %s\n' "$([ "${SKIP_TLS:-0}" = "1" ] && echo 'atlanıyor' || echo "Let's Encrypt (certbot)")"
  printf '    Güvenlik duvarı: %s\n\n' "$([ "${SKIP_FIREWALL:-0}" = "1" ] && echo 'atlanıyor' || echo 'nftables (en sonda uygulanır)')"

  if ! ask_yes_no "Bu ayarlarla kuruluma başlansın mı" "y"; then
    echo "İptal edildi."
    exit 0
  fi
  echo
fi
VPN_SKIP_TLS="${SKIP_TLS:-0}"
export VPN_NODE_ID VPN_NODE_REGION VPN_SSH_PORT VPN_SKIP_TLS

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
if [ "${VPN_ENABLE_IKEV2:-0}" = "1" ]; then
  ./35-ikev2-setup.sh
else
  echo "[*] IKEv2 atlandı (etkinleştirmek için: --with-ikev2). WireGuard ve OpenVPN kuruldu."
fi
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
SCHEME="https"; [ "${SKIP_TLS:-0}" = "1" ] && SCHEME="http"
echo " Sunucu API uç noktası : $SCHEME://$VPN_API_DOMAIN:${VPN_API_PUBLIC_PORT:-8443}/"
echo " WireGuard genel anahtarı: $(cat /etc/wireguard/server_public.key 2>/dev/null || echo '(henüz yok)')"
echo " Servis durumu          : systemctl status vpn-api wg-quick@wg0 openvpn-server@server-udp openvpn-server@server-tcp unbound"
echo " Günlükler               : journalctl -u vpn-api -f"
echo " Kurulumu denetle       : sudo $INSTALL_DIR/scripts/verify.sh"
echo " Tüneli gerçekten test et: sudo $INSTALL_DIR/scripts/test-peer.sh"
echo "=================================================================="
echo
echo "[!] ÖNEMLİ: Kapatmadan önce YENİ bir terminalden SSH erişiminizi doğrulayın."
