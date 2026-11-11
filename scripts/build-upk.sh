#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ "$(uname -s)" != Linux ]]; then
  echo "正式 UPK 必须在 Linux 中使用官方 ugcli 打包。请在 Debian 12 / Linux VM / Linux CI 中运行。" >&2
  exit 1
fi
command -v go >/dev/null || { echo '需要 Go 1.26.11' >&2; exit 1; }
# UGOS currently injects GODEBUG=tlskyber=0 into native apps. Go 1.27
# treats that removed setting as fatal before main. Pin the maintained
# compatible toolchain; do not change the NAS-wide environment to build.
if [[ "$(go env GOVERSION)" != go1.26.11 ]]; then
  echo '请使用 Go 1.26.11，或设置 GOTOOLCHAIN=go1.26.11。其他版本须先验证 UGOS 的 GODEBUG 兼容性。' >&2
  exit 1
fi
command -v ugcli >/dev/null || { echo '需要官方 ugcli >= 1.1.0.25' >&2; exit 1; }
# Built frontend files are included; set REBUILD_UI=1 only when changing the UI.
if [[ "${REBUILD_UI:-0}" == 1 ]]; then
  npm --prefix frontend ci --no-audit --no-fund
  npm --prefix frontend run build
fi
mkdir -p packaging/rootfs_amd64/bin
(
  cd backend
  GODEBUG=tlskyber=0 go test -count=1 ./...
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags='-s -w' -o ../packaging/rootfs_amd64/bin/wol .
)
chmod 755 packaging/rootfs_amd64/bin/wol
chmod 644 packaging/project.yaml packaging/rootfs_common/www/* packaging/rootfs_common/icon.png
(
  cd packaging
  ugcli check
  ugcli pack --arch amd64 --product-series nasync --build "${BUILD_NUMBER:-1}"
)
bash scripts/export-upk.sh
