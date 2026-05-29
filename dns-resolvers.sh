#!/bin/sh
# dns-resolvers.sh — show which public DNS resolvers clients are using
# to reach this SlipGate server's DNS-tunneling transports (DNSTT / VayDNS).
#
# Usage:
#   sudo ./dns-resolvers.sh snapshot [seconds]   # capture N seconds (default 60), print top resolvers
#   sudo ./dns-resolvers.sh watch                # live tally, refreshes every 2s, Ctrl-C to stop
#   sudo ./dns-resolvers.sh log [path]           # append-only log of source IPs to a file
#   sudo ./dns-resolvers.sh report [path]        # rank a previously-captured log file
#
# Notes:
#   - Shows public resolver IPs (1.1.1.1, 8.8.8.8, ...), not end-user IPs.
#     This is a property of DNS tunneling, not a limitation of this script.
#   - Excludes 127.0.0.0/8 (slipgate-dnsrouter -> local tunnel backends).
#   - Requires tcpdump. Reverse-DNS lookups use `dig` or `host` if available.

set -eu

IFACE="${IFACE:-any}"
FILTER='udp dst port 53 and not src net 127.0.0.0/8'
DEFAULT_LOG="/var/log/slipgate-dns-clients.log"

need_root() {
    if [ "$(id -u)" -ne 0 ]; then
        echo "error: must run as root (tcpdump needs CAP_NET_RAW)" >&2
        exit 1
    fi
}

need_cmd() {
    command -v "$1" >/dev/null 2>&1 || {
        echo "error: '$1' not found in PATH" >&2
        exit 1
    }
}

# Read tcpdump stdout, extract the source IP from "IP a.b.c.d.port > ..." lines.
extract_src_ip() {
    awk '/^[0-9]/ { print $3 }' | sed 's/\.[0-9]*$//'
}

# Pipe a list of IPs in, get "count  ip  ptr" out, ranked.
rank_with_ptr() {
    sort | uniq -c | sort -rn | while read -r count ip; do
        ptr=""
        if command -v dig >/dev/null 2>&1; then
            ptr=$(dig +short +time=1 +tries=1 -x "$ip" 2>/dev/null | head -1)
        elif command -v host >/dev/null 2>&1; then
            ptr=$(host -W 1 "$ip" 2>/dev/null | awk '/pointer/ {print $NF; exit}')
        fi
        printf "%6d  %-15s  %s\n" "$count" "$ip" "${ptr:-?}"
    done
}

cmd_snapshot() {
    secs="${1:-60}"
    need_root
    need_cmd tcpdump
    echo "capturing inbound DNS on iface=$IFACE for ${secs}s..." >&2
    tmp=$(mktemp)
    trap 'rm -f "$tmp"' EXIT
    timeout "$secs" tcpdump -nn -i "$IFACE" "$FILTER" -l 2>/dev/null \
        | extract_src_ip > "$tmp" || true
    total=$(wc -l < "$tmp" | tr -d ' ')
    uniq_count=$(sort -u "$tmp" | wc -l | tr -d ' ')
    echo "captured $total queries from $uniq_count distinct resolvers" >&2
    echo
    printf "%6s  %-15s  %s\n" "COUNT" "RESOLVER" "PTR"
    printf "%6s  %-15s  %s\n" "-----" "---------------" "---"
    rank_with_ptr < "$tmp"
}

cmd_watch() {
    need_root
    need_cmd tcpdump
    tmp=$(mktemp)
    trap 'rm -f "$tmp"; kill 0 2>/dev/null; exit 0' INT TERM EXIT
    tcpdump -nn -i "$IFACE" "$FILTER" -l 2>/dev/null \
        | extract_src_ip >> "$tmp" &
    while :; do
        clear
        echo "live DNS resolver tally (Ctrl-C to stop) — iface=$IFACE"
        total=$(wc -l < "$tmp" | tr -d ' ')
        echo "queries seen: $total"
        echo
        printf "%6s  %-15s\n" "COUNT" "RESOLVER"
        printf "%6s  %-15s\n" "-----" "---------------"
        sort "$tmp" | uniq -c | sort -rn | head -20
        sleep 2
    done
}

cmd_log() {
    path="${1:-$DEFAULT_LOG}"
    need_root
    need_cmd tcpdump
    echo "logging inbound DNS source IPs to $path (Ctrl-C to stop)" >&2
    tcpdump -nn -i "$IFACE" "$FILTER" -l 2>/dev/null \
        | extract_src_ip >> "$path"
}

cmd_report() {
    path="${1:-$DEFAULT_LOG}"
    [ -r "$path" ] || { echo "error: cannot read $path" >&2; exit 1; }
    total=$(wc -l < "$path" | tr -d ' ')
    uniq_count=$(sort -u "$path" | wc -l | tr -d ' ')
    echo "report from $path: $total queries, $uniq_count distinct resolvers"
    echo
    printf "%6s  %-15s  %s\n" "COUNT" "RESOLVER" "PTR"
    printf "%6s  %-15s  %s\n" "-----" "---------------" "---"
    rank_with_ptr < "$path"
}

usage() {
    sed -n '2,15p' "$0"
    exit 1
}

case "${1:-}" in
    snapshot) shift; cmd_snapshot "$@" ;;
    watch)    shift; cmd_watch ;;
    log)      shift; cmd_log "$@" ;;
    report)   shift; cmd_report "$@" ;;
    ""|-h|--help|help) usage ;;
    *) echo "unknown command: $1" >&2; usage ;;
esac
