#!/usr/bin/env bash
# Shared helpers sourced by every script in this directory. Not meant to be
# run directly.

set -euo pipefail

VPN_LIB_COLOR_INFO='\033[0;36m'
VPN_LIB_COLOR_WARN='\033[0;33m'
VPN_LIB_COLOR_ERR='\033[0;31m'
VPN_LIB_COLOR_OK='\033[0;32m'
VPN_LIB_COLOR_RESET='\033[0m'

log_info() { printf '%b[*]%b %s\n' "$VPN_LIB_COLOR_INFO" "$VPN_LIB_COLOR_RESET" "$1"; }
log_warn() { printf '%b[!]%b %s\n' "$VPN_LIB_COLOR_WARN" "$VPN_LIB_COLOR_RESET" "$1"; }
log_err()  { printf '%b[x]%b %s\n' "$VPN_LIB_COLOR_ERR" "$VPN_LIB_COLOR_RESET" "$1" >&2; }
log_ok()   { printf '%b[ok]%b %s\n' "$VPN_LIB_COLOR_OK" "$VPN_LIB_COLOR_RESET" "$1"; }

require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    log_err "Bu betik root olarak çalıştırılmalıdır (sudo ile deneyin)."
    exit 1
  fi
}

require_ubuntu() {
  if [ ! -r /etc/os-release ]; then
    log_warn "/etc/os-release bulunamadı; Ubuntu 22.04/24.04 dışında bir sistemde olabilirsiniz."
    return
  fi
  # shellcheck disable=SC1091
  . /etc/os-release
  if [ "${ID:-}" != "ubuntu" ]; then
    log_warn "Bu betikler Ubuntu için tasarlandı, tespit edilen dağıtım: ${ID:-bilinmiyor}. Devam ediliyor ama sorun çıkabilir."
  fi
}

# confirm prompts the operator before a risky/irreversible step, unless
# VPN_ASSUME_YES=1 is set in the environment (used by install.sh's --yes and
# by CI/non-interactive runs).
confirm() {
  local prompt="$1"
  if [ "${VPN_ASSUME_YES:-0}" = "1" ]; then
    return 0
  fi
  read -r -p "$prompt [y/N] " reply
  case "$reply" in
    y|Y|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

# detect_wan_iface prints the network interface that owns the default
# route — the one NAT/masquerade rules need to reference. Exits with an
# error rather than guessing wrong on a multi-homed box, since a wrong
# egress interface silently breaks every tunnel client's internet access.
detect_wan_iface() {
  local iface
  iface="$(ip -4 route show default 2>/dev/null | awk '/^default/ {for (i=1;i<=NF;i++) if ($i=="dev") print $(i+1)}' | head -n1)"
  if [ -z "$iface" ]; then
    log_err "Varsayılan ağ arayüzü tespit edilemedi. VPN_WAN_IFACE ortam değişkenini elle ayarlayın."
    exit 1
  fi
  echo "$iface"
}

# render_template copies src to dest, substituting @@TOKEN@@ placeholders
# from the environment. Deliberately not a generic templating engine —
# just enough to keep the checked-in .tmpl files readable while letting
# install.sh fill in per-host values (WAN interface, ports, hostnames).
render_template() {
  local src="$1" dest="$2"
  shift 2
  local sed_args=()
  local pair key value
  for pair in "$@"; do
    key="${pair%%=*}"
    value="${pair#*=}"
    sed_args+=(-e "s|@@${key}@@|${value}|g")
  done
  sed "${sed_args[@]}" "$src" > "$dest"
}

package_installed() {
  dpkg -s "$1" >/dev/null 2>&1
}

# detect_ipv6_support echoes 1 when this host actually has usable global
# IPv6 (a global-scope address AND a default route), else 0.
#
# This gates dual-stack tunnelling, and getting it wrong is worse than
# leaving IPv6 off: advertising ::/0 to clients on a host that can't route
# it black-holes their IPv6 traffic, while leaving it off merely leaves the
# documented leak in place. So the test is deliberately strict.
detect_ipv6_support() {
  if [ -n "${VPN_ENABLE_IPV6:-}" ]; then
    echo "$VPN_ENABLE_IPV6"
    return
  fi
  if ip -6 addr show scope global 2>/dev/null | grep -q "inet6" &&
    ip -6 route show default 2>/dev/null | grep -q "default"; then
    echo 1
  else
    echo 0
  fi
}
