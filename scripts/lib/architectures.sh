# Shared target selection. Source after changing to the repository root.
case "${TARGET_ARCH:-all}" in
  all) WOL_ARCHES=(amd64 arm64) ;;
  amd64|arm64) WOL_ARCHES=("$TARGET_ARCH") ;;
  *) echo 'TARGET_ARCH 仅支持 amd64、arm64 或 all。' >&2; exit 1 ;;
esac
