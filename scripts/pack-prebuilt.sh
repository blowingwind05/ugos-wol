#!/usr/bin/env bash
# Package the included static Linux binary without installing Go, Node, or Docker.
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ "$(uname -s)" != Linux || "$(uname -m)" != x86_64 ]]; then
  echo '请在 x86_64 Linux 服务器上运行；官方 Linux ugcli 是 amd64 程序。' >&2
  exit 1
fi
UGCLI_TOOL="${UGCLI_TOOL:-ugcli}"
command -v "$UGCLI_TOOL" >/dev/null || { echo '未找到 ugcli，请设置 UGCLI_TOOL=/绝对路径/ugcli-linux-amd64' >&2; exit 1; }
chmod 755 packaging/rootfs_amd64/bin/wol
chmod 644 packaging/project.yaml packaging/rootfs_common/icon.png packaging/rootfs_common/www/*
cd packaging
"$UGCLI_TOOL" check
"$UGCLI_TOOL" pack --arch amd64 --product-series nasync --build "${BUILD_NUMBER:-1}"
cd ..
bash scripts/export-upk.sh
