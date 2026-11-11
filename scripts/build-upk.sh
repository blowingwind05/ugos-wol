#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ "$(uname -s)" != Linux ]]; then
  echo '正式 UPK 必须在 Linux 中使用官方 ugcli 打包。' >&2
  exit 1
fi
command -v go >/dev/null || { echo '需要 Go 1.26.11' >&2; exit 1; }
if [[ "$(go env GOVERSION)" != go1.26.11 ]]; then
  echo '请使用 Go 1.26.11，其他版本须先验证 UGOS 固件环境兼容性。' >&2
  exit 1
fi
if [[ "${REBUILD_UI:-0}" == 1 ]]; then
  npm --prefix frontend ci --no-audit --no-fund
  npm --prefix frontend run build
fi
(cd backend && GODEBUG=tlskyber=0 go test -count=1 ./...)
bash scripts/build-binaries.sh
bash scripts/pack-prebuilt.sh
