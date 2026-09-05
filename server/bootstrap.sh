#!/usr/bin/env bash
# One-line installer. Fetches this repository and runs the setup wizard:
#
#   curl -fsSL https://raw.githubusercontent.com/agamert37-cmd/Mobilvpn/main/server/bootstrap.sh | sudo bash
#
# Anything after `-s --` is forwarded to server/install.sh, so a fully
# unattended install is:
#
#   curl -fsSL .../bootstrap.sh | sudo bash -s -- --domain vpn.example.com --yes
#
# Environment:
#   VPN_REPO_URL     repository to clone (default: this project on GitHub)
#   VPN_REPO_BRANCH  branch to install from (default: main)
#   VPN_SRC_DIR      where to keep the checkout (default: /opt/mobilvpn-src)
set -euo pipefail

REPO_URL="${VPN_REPO_URL:-https://github.com/agamert37-cmd/Mobilvpn.git}"
REPO_BRANCH="${VPN_REPO_BRANCH:-main}"
SRC_DIR="${VPN_SRC_DIR:-/opt/mobilvpn-src}"

C_INFO='\033[0;36m'; C_WARN='\033[0;33m'; C_ERR='\033[0;31m'; C_OK='\033[0;32m'; C_OFF='\033[0m'
say()  { printf '%b[*]%b %s\n' "$C_INFO" "$C_OFF" "$1"; }
warn() { printf '%b[!]%b %s\n' "$C_WARN" "$C_OFF" "$1"; }
die()  { printf '%b[x]%b %s\n' "$C_ERR" "$C_OFF" "$1" >&2; exit 1; }
ok()   { printf '%b[ok]%b %s\n' "$C_OK" "$C_OFF" "$1"; }

if [ "$(id -u)" -ne 0 ]; then
  die "Root gerekiyor. Şöyle çalıştırın:
  curl -fsSL https://raw.githubusercontent.com/agamert37-cmd/Mobilvpn/main/server/bootstrap.sh | sudo bash"
fi

if [ -r /etc/os-release ]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  # [[ ]] here because [ ] compares literally and would never match the glob,
  # mislabelling every Debian derivative as unsupported.
  if [ "${ID:-}" != "ubuntu" ] && [[ "${ID_LIKE:-}" != *debian* ]]; then
    warn "Bu kurulum Ubuntu için tasarlandı (tespit edilen: ${PRETTY_NAME:-bilinmiyor}). Devam ediliyor."
  fi
fi

say "Gerekli araçlar kontrol ediliyor (git, curl, ca-certificates)..."
MISSING=()
for pkg in git curl ca-certificates; do
  case "$pkg" in
    ca-certificates) dpkg -s ca-certificates >/dev/null 2>&1 || MISSING+=("$pkg") ;;
    *) command -v "$pkg" >/dev/null 2>&1 || MISSING+=("$pkg") ;;
  esac
done
if [ ${#MISSING[@]} -gt 0 ]; then
  say "Kuruluyor: ${MISSING[*]}"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y --no-install-recommends "${MISSING[@]}"
fi

if [ -d "$SRC_DIR/.git" ]; then
  say "Mevcut kaynak güncelleniyor: $SRC_DIR ($REPO_BRANCH)"
  git -C "$SRC_DIR" remote set-url origin "$REPO_URL"
  git -C "$SRC_DIR" fetch --depth 1 origin "$REPO_BRANCH"
  git -C "$SRC_DIR" checkout -q -B "$REPO_BRANCH" "origin/$REPO_BRANCH"
else
  say "Depo indiriliyor: $REPO_URL ($REPO_BRANCH) -> $SRC_DIR"
  rm -rf "$SRC_DIR"
  git clone --depth 1 --branch "$REPO_BRANCH" "$REPO_URL" "$SRC_DIR" 2>/dev/null ||
    die "Klonlama başarısız. '$REPO_BRANCH' dalı var mı ve bu makinenin GitHub'a erişimi var mı, kontrol edin.
  Farklı bir dal için: VPN_REPO_BRANCH=<dal> ile tekrar çalıştırın."
fi

INSTALLER="$SRC_DIR/server/install.sh"
if [ ! -f "$INSTALLER" ]; then
  die "Bu dalda sunucu kurulumu bulunamadı: $INSTALLER
  Muhtemelen '$REPO_BRANCH' dalında server/ dizini yok. VPN_REPO_BRANCH ile doğru dalı verin."
fi

chmod +x "$INSTALLER" "$SRC_DIR/server/scripts/"*.sh 2>/dev/null || true
ok "Kaynak hazır: $SRC_DIR"
echo

# install.sh asks its own questions on /dev/tty, which keeps working even
# though this script itself arrived on stdin through the curl pipe.
exec "$INSTALLER" "$@"
