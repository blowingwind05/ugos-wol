#!/usr/bin/env bash
# One-time compatibility for UGOS versions that omit Ugreen-User-Type.
# Applies only to this native app; does not change group membership.
set -euo pipefail
if [[ "$EUID" -ne 0 ]]; then
  echo '请由 NAS 管理员通过 sudo 运行此脚本。' >&2
  exit 1
fi
test -f /var/packages/com.local.wol/config.json || { echo '尚未安装 com.local.wol' >&2; exit 1; }
grep -q '^admin:' /etc/group || { echo '系统管理员组不存在，未更改配置。' >&2; exit 1; }
compat_dir=/etc/systemd/system/com.local.wol.service.d
compat_file="$compat_dir/91-wol-legacy-auth.conf"
install -d -m 0755 "$compat_dir"
printf '[Service]\nBindReadOnlyPaths=/etc/group\n' > "$compat_file"
chmod 0644 "$compat_file"
systemctl daemon-reload
systemctl try-restart com.local.wol.service
echo '已仅为 com.local.wol 添加管理员组只读访问。'
