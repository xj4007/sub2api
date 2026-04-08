#!/usr/bin/env bash
set -euo pipefail

if command -v wg >/dev/null 2>&1 && command -v wg-quick >/dev/null 2>&1; then
  exit 0
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y wireguard wireguard-tools
systemctl enable wg-quick@wg0 >/dev/null 2>&1 || true
