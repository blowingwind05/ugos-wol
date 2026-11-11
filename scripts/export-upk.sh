#!/usr/bin/env bash
# Export all selected artifacts only after checking every source/destination.
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/architectures.sh
app_version=$(awk '$1 == "version:" { gsub(/"/, "", $2); print $2; exit }' packaging/project.yaml)
package_id=$(awk '$1 == "app_id:" { gsub(/"/, "", $2); print $2; exit }' packaging/project.yaml)
build_suffix=$(printf '%04d' "${BUILD_NUMBER:-1}")
package_output_dir="${UPK_OUTPUT_DIR:-$HOME/Downloads}"
package_names=()
for target_arch in "${WOL_ARCHES[@]}"; do
  package_name="${target_arch}_nasync_${package_id}_${app_version}.${build_suffix}.upk"
  package_source="$PWD/packaging/build_dir/pkgs/upk/$package_name"
  test -f "$package_source" || { echo "安装包不存在：$package_name" >&2; exit 1; }
  if [[ -e "$package_output_dir/$package_name" || -L "$package_output_dir/$package_name" ]]; then
    echo "目标文件已存在，未覆盖：$package_output_dir/$package_name" >&2
    exit 1
  fi
  package_names+=("$package_name")
done
mkdir -p "$package_output_dir"
for package_name in "${package_names[@]}"; do
  mv -- "$PWD/packaging/build_dir/pkgs/upk/$package_name" "$package_output_dir/$package_name"
  echo "UPK 已保存：$package_output_dir/$package_name"
done
