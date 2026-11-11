#!/usr/bin/env bash
# Cross-compile native NAS binaries. UPK packaging is a separate Linux step.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/architectures.sh
command -v go >/dev/null || { echo '需要 Go 1.26.11' >&2; exit 1; }
if [[ "$(go env GOVERSION)" != go1.26.11 ]]; then
  echo '请使用 Go 1.26.11；切换工具链前须验证 UGOS 固件环境兼容性。' >&2
  exit 1
fi
for target_arch in "${WOL_ARCHES[@]}"; do
  mkdir -p "packaging/rootfs_${target_arch}/bin"
  (
    cd backend
    if [[ "$target_arch" == amd64 ]]; then
      CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -ldflags='-s -w' -o ../packaging/rootfs_amd64/bin/wol .
    else
      CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOARM64=v8.0 go build -trimpath -ldflags='-s -w' -o ../packaging/rootfs_arm64/bin/wol .
    fi
  )
  chmod 755 "packaging/rootfs_${target_arch}/bin/wol"
  echo "已构建 Linux $target_arch 程序。"
done
