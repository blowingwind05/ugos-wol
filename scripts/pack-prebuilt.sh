#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ "$(uname -s)" != Linux || "$(uname -m)" != x86_64 ]]; then
  echo '官方 ugcli 打包主机需为 x86_64 Linux；目标 NAS 可为 amd64 或 arm64。' >&2
  exit 1
fi
source scripts/lib/architectures.sh
UGCLI_TOOL="${UGCLI_TOOL:-ugcli}"
command -v "$UGCLI_TOOL" >/dev/null || { echo '未找到官方 ugcli，请通过 UGCLI_TOOL 指定路径。' >&2; exit 1; }
for target_arch in "${WOL_ARCHES[@]}"; do
  test -s "packaging/rootfs_${target_arch}/bin/wol" || { echo "缺少 $target_arch 程序，请先运行 scripts/build-binaries.sh。" >&2; exit 1; }
  chmod 755 "packaging/rootfs_${target_arch}/bin/wol"
done
chmod 644 packaging/project.yaml packaging/rootfs_common/icon.png packaging/rootfs_common/www/*
(
  cd packaging
  "$UGCLI_TOOL" check
  for target_arch in "${WOL_ARCHES[@]}"; do
    "$UGCLI_TOOL" pack --arch "$target_arch" --product-series nasync --build "${BUILD_NUMBER:-1}"
  done
)
bash scripts/export-upk.sh
