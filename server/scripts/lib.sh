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

# have_tty reports whether there's a terminal we can ask questions on.
#
# Note this deliberately checks /dev/tty rather than stdin: the whole point
# of the `curl -fsSL ... | sudo bash` install is that stdin is the *script*,
# not the keyboard, so `read` from stdin would silently consume the script
# itself (or read EOF immediately). Everything interactive below therefore
# goes through /dev/tty.
have_tty() {
  # The 2>/dev/null has to come FIRST: bash applies redirections left to
  # right and reports a failing one on the *current* stderr, so putting it
  # last would let "No such device or address" leak out on every
  # non-interactive run.
  [ -e /dev/tty ] && [ -r /dev/tty ] && : 2>/dev/null >/dev/tty
}

# ask prompts for a value on the terminal, offering a default. Falls back to
# the default without prompting when running unattended.
#   ask VARNAME "Question" "default"
ask() {
  local __var="$1" prompt="$2" default="${3:-}" reply=""
  if [ "${VPN_ASSUME_YES:-0}" = "1" ] || ! have_tty; then
    printf -v "$__var" '%s' "$default"
    return
  fi
  if [ -n "$default" ]; then
    printf '%b?%b %s [%s]: ' "$VPN_LIB_COLOR_INFO" "$VPN_LIB_COLOR_RESET" "$prompt" "$default" >/dev/tty
  else
    printf '%b?%b %s: ' "$VPN_LIB_COLOR_INFO" "$VPN_LIB_COLOR_RESET" "$prompt" >/dev/tty
  fi
  IFS= read -r reply </dev/tty || reply=""
  printf -v "$__var" '%s' "${reply:-$default}"
}

# ask_yes_no returns 0 for yes. default should be "y" or "n".
ask_yes_no() {
  local prompt="$1" default="${2:-n}" reply=""
  if [ "${VPN_ASSUME_YES:-0}" = "1" ] || ! have_tty; then
    [ "$default" = "y" ]
    return
  fi
  local hint="[y/N]"
  [ "$default" = "y" ] && hint="[Y/n]"
  printf '%b?%b %s %s: ' "$VPN_LIB_COLOR_INFO" "$VPN_LIB_COLOR_RESET" "$prompt" "$hint" >/dev/tty
  IFS= read -r reply </dev/tty || reply=""
  reply="${reply:-$default}"
  case "$reply" in
    y | Y | yes | YES | Yes | e | E | evet | EVET) return 0 ;;
    *) return 1 ;;
  esac
}

# confirm prompts the operator before a risky/irreversible step, unless
# VPN_ASSUME_YES=1 is set in the environment (used by install.sh's --yes and
# by CI/non-interactive runs).
confirm() {
  ask_yes_no "$1" "n"
}

# detect_ssh_port reads the port sshd is actually configured for.
#
# Asking the operator to type this from memory is how people lock
# themselves out of a remote box, so we read it from the config (and any
# drop-ins) and only fall back to 22 if nothing is set.
detect_ssh_port() {
  local port=""
  if [ -r /etc/ssh/sshd_config ]; then
    port="$(awk '/^[[:space:]]*Port[[:space:]]+[0-9]+/ {print $2}' /etc/ssh/sshd_config 2>/dev/null | tail -n1)"
  fi
  if [ -z "$port" ] && [ -d /etc/ssh/sshd_config.d ]; then
    port="$(awk '/^[[:space:]]*Port[[:space:]]+[0-9]+/ {print $2}' /etc/ssh/sshd_config.d/*.conf 2>/dev/null | tail -n1)"
  fi
  # A live listener is the most trustworthy source of all.
  if [ -z "$port" ] && command -v ss >/dev/null 2>&1; then
    port="$(ss -lntpH 2>/dev/null | awk '/sshd/ {split($4,a,":"); print a[length(a)]}' | head -n1)"
  fi
  echo "${port:-22}"
}

# detect_public_ip finds this host's public IPv4, preferring a local global
# address and only then asking an external service (which some hardened
# hosts can't reach anyway).
detect_public_ip() {
  local ip=""
  # Skip RFC1918 and CGNAT (100.64/10): a box behind either isn't reachable
  # at that address, so it would be the wrong thing to put in a cert SAN.
  ip="$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 |
    grep -vE '^(10\.|172\.(1[6-9]|2[0-9]|3[01])\.|192\.168\.|100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.)' | head -n1)"
  if [ -z "$ip" ] && command -v curl >/dev/null 2>&1; then
    ip="$(curl -fsS --max-time 5 https://api.ipify.org 2>/dev/null || true)"
  fi
  echo "$ip"
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
