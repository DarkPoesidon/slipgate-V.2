#!/usr/bin/env bash
# SlipGate one-click installer.
set -Eeuo pipefail

REPO="${SLIPGATE_REPO:-DarkPoesidon/slipgate-V.2}"
INSTALL_DIR="${SLIPGATE_INSTALL_DIR:-/usr/local/bin}"
TTY="/dev/tty"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[1;36m'
NC='\033[0m'

info() { echo -e "${GREEN}[+]${NC} $*"; }
warn() { echo -e "${YELLOW}[!]${NC} $*" >&2; }
error() { echo -e "${RED}[-]${NC} $*" >&2; exit 1; }

need_tty() {
    [[ -r "$TTY" && -w "$TTY" ]] || error "No interactive terminal found. Run this installer from an SSH terminal, not from a background job."
}

ask() {
    local prompt="$1"
    local default="${2:-}"
    local value
    if [[ -n "$default" ]]; then
        printf "  %s [%s]: " "$prompt" "$default" >"$TTY"
    else
        printf "  %s: " "$prompt" >"$TTY"
    fi
    IFS= read -r value <"$TTY"
    if [[ -z "$value" ]]; then
        value="$default"
    fi
    printf '%s' "$value"
}

ask_secret() {
    local prompt="$1"
    local value
    printf "  %s (input is hidden; paste it and press Enter): " "$prompt" >"$TTY"
    IFS= read -r -s value <"$TTY"
    printf "\n" >"$TTY"
    printf '%s' "$value"
}

ask_yes_no() {
    local prompt="$1"
    local default="${2:-no}"
    local suffix="[y/N]"
    local value
    if [[ "$default" == "yes" ]]; then
        suffix="[Y/n]"
    fi
    while true; do
        printf "  %s %s: " "$prompt" "$suffix" >"$TTY"
        IFS= read -r value <"$TTY"
        value="${value,,}"
        if [[ -z "$value" ]]; then
            value="$default"
        fi
        case "$value" in
            y|yes|true|1|on) return 0 ;;
            n|no|false|0|off) return 1 ;;
            *) echo "  Please answer yes or no." >"$TTY" ;;
        esac
    done
}

detect_arch() {
    case "$(uname -m)" in
        x86_64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) error "Unsupported architecture: $(uname -m)" ;;
    esac
}

detect_public_ipv4() {
    local ip=""
    if command -v curl >/dev/null 2>&1; then
        ip="$(curl -fsSL --max-time 5 https://api.ipify.org 2>/dev/null || true)"
        [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] && { echo "$ip"; return; }
        ip="$(curl -fsSL --max-time 5 https://ifconfig.me/ip 2>/dev/null || true)"
        [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] && { echo "$ip"; return; }
    fi
    echo ""
}

download_file() {
    local url="$1"
    local dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fL --retry 3 --retry-delay 2 --connect-timeout 15 "$url" -o "$dest"
    elif command -v wget >/dev/null 2>&1; then
        wget -O "$dest" "$url"
    else
        error "Neither curl nor wget found"
    fi
}

cloudflare_preflight() {
    local token="$1"
    local zone="$2"
    local response

    info "Checking Cloudflare token and zone access..."
    response="$(curl -fsS --connect-timeout 15 \
        -H "Authorization: Bearer ${token}" \
        -H "Content-Type: application/json" \
        "https://api.cloudflare.com/client/v4/zones?name=${zone}&status=active" 2>&1)" || {
        error "Cloudflare API check failed. Confirm the token is valid and has Zone:Read for ${zone}. Response: ${response}"
    }

    if [[ "$response" != *'"success":true'* || "$response" != *'"id"'* ]]; then
        error "Cloudflare zone ${zone} was not found or token cannot read it. Make sure the zone is Active and the token has Zone:Read plus DNS:Edit/DNS Write for this specific zone."
    fi
}

print_banner() {
    echo -e "${CYAN}"
    echo "   _____ _ _       _____       _       "
    echo "  / ____| (_)     / ____|     | |      "
    echo " | (___ | |_ _ __| |  __  __ _| |_ ___ "
    echo "  \___ \| | | '_ \ | |_ |/ _\` | __/ _ \\"
    echo "  ____) | | | |_) | |__| | (_| | ||  __/"
    echo " |_____/|_|_| .__/ \_____|\__,_|\__\___|"
    echo "             | |                         "
    echo "             |_|                         "
    echo -e "${NC}"
}

collect_transports() {
    local value="${SLIPGATE_INSTALL_TRANSPORTS:-}"
    if [[ -n "$value" ]]; then
        echo "$value"
        return
    fi

    echo >"$TTY"
    echo "  Which transports do you want to install?" >"$TTY"
    echo >"$TTY"
    echo "  Transports:" >"$TTY"
    echo "    1) DNSTT / NoizDNS - DNS tunnel" >"$TTY"
    echo "    2) Slipstream - QUIC DNS tunnel" >"$TTY"
    echo "    3) VayDNS - KCP DNS tunnel" >"$TTY"
    echo "    4) NaiveProxy - HTTPS proxy with Caddy" >"$TTY"
    echo "    5) StunTLS - SSH over TLS + WebSocket proxy" >"$TTY"
    echo "    6) SSH - Direct SSH tunnel" >"$TTY"
    echo "    7) SOCKS5 - Direct SOCKS5 proxy" >"$TTY"
    echo "    8) All" >"$TTY"
    value="$(ask "Choice (comma-separated, e.g. 1,3,4)" "")"
    [[ -n "$value" ]] || error "No transports selected"
    echo "$value"
}

transports_need_dns() {
    local transports="${1,,}"
    local item
    local -a items
    transports="${transports// /}"
    IFS=',' read -r -a items <<<"$transports"
    for item in "${items[@]}"; do
        case "$item" in
            all|8|1|2|3|4|dnstt|slipstream|vaydns|naive) return 0 ;;
        esac
    done
    return 1
}

collect_cloudflare_args() {
    local -n out_args_ref=$1
    local transports="$2"
    local cf_choice="${SLIPGATE_CLOUDFLARE_DNS:-}"
    local cf_zone="${SLIPGATE_CLOUDFLARE_ZONE:-}"
    local cf_ip="${SLIPGATE_CLOUDFLARE_IP:-}"
    local cf_token="${CLOUDFLARE_API_TOKEN:-}"

    if ! transports_need_dns "$transports"; then
        out_args_ref+=(--cloudflare-dns "no")
        return
    fi

    echo >"$TTY"
    echo "  DNS setup mode" >"$TTY"
    echo "    1) Cloudflare automatic - SlipGate generates domains and creates DNS records" >"$TTY"
    echo "    2) Manual DNS - you enter each domain and create records yourself" >"$TTY"
    echo >"$TTY"

    case "${cf_choice,,}" in
        yes|true|1|on) ;;
        no|false|0|off|2)
            out_args_ref+=(--cloudflare-dns "no")
            return
            ;;
        *)
            cf_choice="$(ask "Choice" "1")"
            if [[ "$cf_choice" != "1" ]]; then
                out_args_ref+=(--cloudflare-dns "no")
                return
            fi
            ;;
    esac

    echo >"$TTY"
    echo "  Cloudflare automatic DNS" >"$TTY"
    echo "  SlipGate will generate the required domains and create A and NS records." >"$TTY"
    echo "  Requirements:" >"$TTY"
    echo "    - Domain is added to Cloudflare and shows Active" >"$TTY"
    echo "    - Registrar nameservers point to Cloudflare" >"$TTY"
    echo "    - API token has Zone:Read and DNS:Edit (DNS Write) for this zone" >"$TTY"
    echo "    - Records are DNS only / gray cloud" >"$TTY"
    echo >"$TTY"

    if [[ -z "$cf_zone" ]]; then
        cf_zone="$(ask "Cloudflare zone/root domain (example.com)" "")"
    fi
    [[ -n "$cf_zone" ]] || error "Cloudflare zone/root domain is required for automatic DNS"

    if [[ -z "$cf_ip" ]]; then
        cf_ip="$(ask "Server public IPv4" "$(detect_public_ipv4)")"
    fi
    [[ "$cf_ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || error "A valid public IPv4 is required for Cloudflare DNS"

    if [[ -z "$cf_token" ]]; then
        cf_token="$(ask_secret "Cloudflare API token")"
    fi
    [[ -n "$cf_token" ]] || error "Cloudflare API token is required for automatic DNS"

    cloudflare_preflight "$cf_token" "$cf_zone"

    export CLOUDFLARE_API_TOKEN="$cf_token"
    out_args_ref+=(--cloudflare-dns "yes" --cloudflare-zone "$cf_zone" --cloudflare-ip "$cf_ip" --cloudflare-apply "yes")
}

main() {
    [[ ${EUID:-$(id -u)} -eq 0 ]] || error "This script must be run as root (use sudo)"
    need_tty

    local os arch binary release_tag url tmp_bin
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    [[ "$os" == "linux" ]] || error "SlipGate only supports Linux"
    arch="$(detect_arch)"
    binary="slipgate-${os}-${arch}"
    release_tag="${SLIPGATE_RELEASE_TAG:-}"

    if [[ -n "$release_tag" ]]; then
        url="https://github.com/${REPO}/releases/download/${release_tag}/${binary}"
    else
        url="https://github.com/${REPO}/releases/latest/download/${binary}"
    fi

    print_banner

    tmp_bin="$(mktemp)"
    trap 'rm -f "$tmp_bin"' EXIT

    info "Downloading ${binary} from ${REPO}..."
    download_file "$url" "$tmp_bin" || error "Could not download ${binary}. Checked: ${url}"

    info "Installing slipgate to ${INSTALL_DIR}/slipgate..."
    install -d -m 0755 "$INSTALL_DIR"
    killall slipgate 2>/dev/null || true
    killall dnstt-server 2>/dev/null || true
    killall slipstream-server 2>/dev/null || true
    install -m 0755 "$tmp_bin" "${INSTALL_DIR}/slipgate"

    local transports
    local -a install_args
    transports="$(collect_transports)"
    install_args=(install --transports "$transports")
    collect_cloudflare_args install_args "$transports"

    info "Starting SlipGate installer..."
    if ! SLIPGATE_SIMPLE_PROMPT=1 "${INSTALL_DIR}/slipgate" "${install_args[@]}" <"$TTY" >"$TTY" 2>"$TTY"; then
        error "slipgate install failed. Retry with: sudo slipgate install"
    fi

    info "Done. Run 'sudo slipgate' to open the menu."
}

if [[ "${BASH_SOURCE[0]:-$0}" == "$0" ]]; then
    main "$@"
fi
