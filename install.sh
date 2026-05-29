#!/bin/bash
# SlipGate installer — download binary and run `slipgate install`
set -e

# Override with SLIPGATE_REPO=owner/repo when testing a fork.
REPO="${SLIPGATE_REPO:-DarkPoesidon/slipgate-V.2}"
INSTALL_DIR="/usr/local/bin"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[1;36m'
NC='\033[0m'

info()    { echo -e "${GREEN}[+]${NC} $1"; }
error()   { echo -e "${RED}[-]${NC} $1"; exit 1; }

# Check root
[[ $EUID -ne 0 ]] && error "This script must be run as root (sudo)"

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *)       error "Unsupported architecture: $ARCH" ;;
esac

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
[[ "$OS" != "linux" ]] && error "SlipGate only supports Linux"

BINARY="slipgate-${OS}-${ARCH}"

# Override with: SLIPGATE_RELEASE_TAG=v1.5.1 bash install.sh
RELEASE_TAG="${SLIPGATE_RELEASE_TAG:-}"
CHANNEL=""  # ← set to "dev" on dev branch, empty on main

if [[ -n "$RELEASE_TAG" ]]; then
    URL="https://github.com/${REPO}/releases/download/${RELEASE_TAG}/${BINARY}"
elif [[ "$CHANNEL" == "dev" ]]; then
    # Find the latest dev pre-release tag via GitHub API
    DEV_TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases" \
        | grep -o '"tag_name": *"[^"]*-dev"' | head -1 | grep -o '"[^"]*-dev"' | tr -d '"')
    if [[ -n "$DEV_TAG" ]]; then
        URL="https://github.com/${REPO}/releases/download/${DEV_TAG}/${BINARY}"
        info "Dev channel: using release ${DEV_TAG}"
    else
        URL="https://github.com/${REPO}/releases/latest/download/${BINARY}"
        info "No dev release found, falling back to latest stable"
    fi
else
    URL="https://github.com/${REPO}/releases/latest/download/${BINARY}"
fi

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

info "Downloading slipgate ($OS/$ARCH)..."
TMP_BIN="$(mktemp)"
trap 'rm -f "$TMP_BIN"' EXIT

if command -v curl &>/dev/null; then
    if ! curl -fsSL "$URL" -o "$TMP_BIN"; then
        error "Could not download $BINARY from ${REPO}. Check that the GitHub release exists and includes this asset: $URL"
    fi
elif command -v wget &>/dev/null; then
    if ! wget -qO "$TMP_BIN" "$URL"; then
        error "Could not download $BINARY from ${REPO}. Check that the GitHub release exists and includes this asset: $URL"
    fi
else
    error "Neither curl nor wget found"
fi

# Kill any running slipgate/tunnel processes only after the replacement binary
# has been downloaded successfully.
killall slipgate 2>/dev/null || true
killall dnstt-server 2>/dev/null || true
killall slipstream-server 2>/dev/null || true
install -m 0755 "$TMP_BIN" "${INSTALL_DIR}/slipgate"
chmod +x "${INSTALL_DIR}/slipgate"

info "Running slipgate install..."
# Route all I/O through /dev/tty so slipgate talks directly to the
# controlling terminal regardless of how this script was invoked
# (e.g. curl | sudo bash, where sudo's use_pty relay can stall output).
if ! SLIPGATE_SIMPLE_PROMPT=1 "${INSTALL_DIR}/slipgate" install </dev/tty >/dev/tty 2>/dev/tty; then
    error "slipgate install failed — run 'sudo slipgate install' to retry"
fi

info "Done! Run 'sudo slipgate' to see the menu."
