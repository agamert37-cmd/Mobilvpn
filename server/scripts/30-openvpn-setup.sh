#!/usr/bin/env bash
# Bootstraps an easy-rsa PKI and writes the two OpenVPN server instances
# (UDP/1194 and TCP/443) that vpn-api issues per-session client
# certificates against. Safe to re-run: PKI init/CA/DH/ta.key generation
# are skipped if they already exist.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
# shellcheck source=./lib.sh
. ./lib.sh

require_root

# Bu betik scripts/ içinden çalışır; systemd/ kardeş dizindedir.
REPO_ROOT="$(cd .. && pwd)"
EASYRSA_DIR="${VPN_OVPN_EASYRSA_DIR:-/etc/openvpn/easy-rsa}"
SERVER_DIR="${VPN_OVPN_SERVER_DIR:-/etc/openvpn/server}"
CCD_DIR="$SERVER_DIR/ccd"
UDP_PORT="${VPN_OVPN_UDP_PORT:-1194}"
TCP_PORT="${VPN_OVPN_TCP_PORT:-443}"
OVPN_SUBNET_NET="${VPN_OVPN_SUBNET_NET:-10.77.0.0}"
OVPN_SUBNET_MASK="${VPN_OVPN_SUBNET_MASK:-255.255.255.0}"

install -d -m 755 "$SERVER_DIR" "$CCD_DIR"

if [ ! -x "$EASYRSA_DIR/easyrsa" ]; then
  log_info "easy-rsa çalışma dizini oluşturuluyor: $EASYRSA_DIR"
  make-cadir "$EASYRSA_DIR"
fi

cd "$EASYRSA_DIR"
export EASYRSA_BATCH=1

if [ ! -d pki ]; then
  ./easyrsa init-pki
fi

if [ ! -f pki/ca.crt ]; then
  log_info "CA oluşturuluyor (bu sunucunun VPN kimlik otoritesi)..."
  EASYRSA_REQ_CN="MobilVPN-CA" ./easyrsa build-ca nopass
fi

if [ ! -f pki/issued/server.crt ]; then
  log_info "Sunucu sertifikası imzalanıyor..."
  ./easyrsa build-server-full server nopass
fi

if [ ! -f pki/dh.pem ]; then
  log_info "Diffie-Hellman parametreleri üretiliyor (bu birkaç dakika sürebilir)..."
  ./easyrsa gen-dh
fi

if [ ! -f pki/crl.pem ]; then
  ./easyrsa gen-crl
fi
install -m 644 pki/crl.pem "$SERVER_DIR/crl.pem"

if [ ! -f ta.key ]; then
  log_info "tls-crypt statik anahtarı üretiliyor..."
  umask 077
  openvpn --genkey secret ta.key
fi
chmod 600 ta.key pki/private/*.key 2>/dev/null || true

# OpenVPN itself warns "STRONGLY discouraged and considered insecure" about
# an unauthenticated TCP management socket (confirmed against a real 2.6.x
# build — see docs/SECURITY.md). Generate one shared password and put it in
# both the management sockets' config and vpn-api's config.json (via
# ManagementPasswordFile) so nothing but the intended process can query
# session stats or kill a tunnel through that socket.
MGMT_PASS_FILE="$SERVER_DIR/mgmt.pass"
if [ ! -s "$MGMT_PASS_FILE" ]; then
  log_info "OpenVPN management arayüzü için parola üretiliyor..."
  umask 077
  openssl rand -base64 24 > "$MGMT_PASS_FILE"
fi
chmod 600 "$MGMT_PASS_FILE"

render_openvpn_server_conf() {
  local proto="$1" dev="$2" port="$3" mgmt_port="$4" out="$5"
  cat > "$out" <<EOF
# Managed by Mobilvpn server/scripts/30-openvpn-setup.sh.
# İstemci sertifikaları burada elle tanımlanmaz: vpn-api, easy-rsa'yı her
# oturum için ayrı bir ortak ad (sessionId) ile çağırarak sertifika üretir
# ve bağlantı kesildiğinde iptal eder (bkz. internal/openvpn).
port $port
proto $proto
dev $dev
dev-type tun
topology subnet
server $OVPN_SUBNET_NET $OVPN_SUBNET_MASK
client-config-dir $CCD_DIR
ca $EASYRSA_DIR/pki/ca.crt
cert $EASYRSA_DIR/pki/issued/server.crt
key $EASYRSA_DIR/pki/private/server.key
dh $EASYRSA_DIR/pki/dh.pem
tls-crypt $EASYRSA_DIR/ta.key
crl-verify $SERVER_DIR/crl.pem
cipher AES-256-GCM
data-ciphers AES-256-GCM:AES-128-GCM
auth SHA256
tls-version-min 1.2
tls-server
management 127.0.0.1 $mgmt_port $MGMT_PASS_FILE
keepalive 10 60
persist-key
persist-tun
user nobody
group nogroup
# verb 0, ölçülmüş bir gizlilik kararıdır — keyfi değil.
#
# Gerçek bir openvpn 2.6.19'a karşı ölçüldü (bkz. docs/SECURITY.md):
# çıktı üreten EN DÜŞÜK seviye olan verb 1 bile istemcinin gerçek genel
# IP'sini 12 satırda logluyor ve bunların biri şu:
#
#   client1/203.0.113.9:53352 MULTI_sva: pool returned IPv4=10.77.0.2
#
# Bu satır gerçek IP'yi atanan tünel IP'siyle zaman damgalı olarak
# eşleştirir; yani kullanıcıyı deanonimleştirmek için gereken kaydın ta
# kendisidir. verb 0 hiçbir istemci satırı yazmaz ama çalışma zamanı
# hatalarını (ör. "Socket bind failed ... Address already in use") hâlâ
# gösterir; birim Type=notify olduğu için systemd hazır-olma tespiti de
# log çıktısına bağlı değildir. İkisi de gerçek süreçle doğrulandı.
verb 0
explicit-exit-notify 1
EOF
  chmod 600 "$out"
}

render_openvpn_server_conf udp4 tun-udp "$UDP_PORT" 7505 "$SERVER_DIR/server-udp.conf"
render_openvpn_server_conf tcp4-server tun-tcp "$TCP_PORT" 7506 "$SERVER_DIR/server-tcp.conf"

# Ubuntu'nun hazır birimi ExecStart içine bir --status dosyası gömer; o
# dosya bağlı her istemcinin gerçek IP'sini tünel IP'siyle eşleştirir.
# Drop-in ExecStart'ı --status olmadan yeniden tanımlar (ayrıntı ve ölçüm
# için dosyanın kendi başlığına bakın).
DROPIN=/etc/systemd/system/openvpn-server@.service.d/no-status.conf
DROPIN_CHANGED=0
install -d -m 755 /etc/systemd/system/openvpn-server@.service.d
if ! cmp -s "$REPO_ROOT/systemd/openvpn-server-no-status.conf" "$DROPIN"; then
  install -m 644 "$REPO_ROOT/systemd/openvpn-server-no-status.conf" "$DROPIN"
  systemctl daemon-reload
  DROPIN_CHANGED=1
fi

for inst in server-udp server-tcp; do
  systemctl enable --now "openvpn-server@$inst" >/dev/null
  # `enable --now` zaten çalışan bir servisi yeniden başlatmaz, dolayısıyla
  # yükseltme sırasında eski süreç --status'lu ExecStart'la çalışmaya devam
  # eder ve dosyayı yeniden yazar. Drop-in yeni geldiyse açıkça restart.
  if [ "$DROPIN_CHANGED" = "1" ]; then
    systemctl restart "openvpn-server@$inst" >/dev/null || \
      log_warn "openvpn-server@$inst yeniden başlatılamadı; --status dosyası kalkması için elle restart edin."
  fi
  rm -f "/run/openvpn-server/status-$inst.log"
done

log_ok "OpenVPN UDP ($UDP_PORT) ve TCP ($TCP_PORT) servisleri etkin."
log_info "PKI dizini: $EASYRSA_DIR — bunun config.json içindeki openvpn.easyRsaDir ile eşleştiğinden emin olun."
log_info "Management parolası: $MGMT_PASS_FILE — config.json içinde openvpn.managementPasswordFile bunu göstermeli."
