#!/usr/bin/env bash
# Keep deliverable UPKs outside the project. Remote builds should use a temporary transfer directory and remove that copy after verified download.
set -euo pipefail
cd "$(dirname "$0")/.."
app_version=$(awk '$1 == "version:" { gsub(/"/, "", $2); print $2; exit }' packaging/project.yaml)
build_suffix=$(printf '%04d' "${BUILD_NUMBER:-1}")
package_name="amd64_nasync_com.local.wol_${app_version}.${build_suffix}.upk"
package_source="$PWD/packaging/build_dir/pkgs/upk/$package_name"
package_output_dir="${UPK_OUTPUT_DIR:-$HOME/Downloads}"
mkdir -p "$package_output_dir"
if [[ -e "$package_output_dir/$package_name" ]]; then
  echo "目标文件已存在，未覆盖：$package_output_dir/$package_name" >&2
  exit 1
fi
mv -- "$package_source" "$package_output_dir/$package_name"
echo "UPK 已保存：$package_output_dir/$package_name"
