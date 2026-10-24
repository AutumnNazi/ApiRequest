#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 4 ]]; then
  echo "usage: $0 <amd64|arm64> <asset-version> <app-path> <output-directory>" >&2
  exit 2
fi

architecture="$1"
asset_version="$2"
app_path="$3"
output_directory="$4"

case "$architecture" in
  amd64)
    architecture_label="Amd64"
    macho_arch="x86_64"
    ;;
  arm64)
    architecture_label="Arm64"
    macho_arch="arm64"
    ;;
  *)
    echo "unsupported architecture: $architecture" >&2
    exit 2
    ;;
esac

if [[ ! "$asset_version" =~ ^[0-9A-Za-z][0-9A-Za-z.+-]*$ ]]; then
  echo "invalid asset version: $asset_version" >&2
  exit 2
fi
if [[ ! -d "$app_path" ]]; then
  echo "application bundle not found: $app_path" >&2
  exit 1
fi

binary="$app_path/Contents/MacOS/ApiRequest"
if [[ ! -f "$binary" ]]; then
  echo "application executable not found: $binary" >&2
  exit 1
fi
actual_architecture="$(lipo -archs "$binary" | xargs)"
if [[ "$actual_architecture" != "$macho_arch" ]]; then
  echo "expected a thin $macho_arch application executable, found: $actual_architecture" >&2
  exit 1
fi

mkdir -p "$output_directory"
output_directory="$(cd "$output_directory" && pwd)"
staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

ditto "$app_path" "$staging/ApiRequest.app"
ln -s /Applications "$staging/Applications"

dmg="$output_directory/ApiRequest-$asset_version-MacOS-$architecture_label.dmg"
# hdiutil 在 GitHub Actions macOS runner 上偶发 "Resource busy"（Spotlight/mds
# 扫描临时目录），重试直到成功——连续失败才让构建失败。
hdiutil_ok=0
for attempt in 1 2 3 4 5; do
  if hdiutil create \
    -volname 'ApiRequest' \
    -srcfolder "$staging" \
    -format UDZO \
    -ov \
    "$dmg"; then
    hdiutil_ok=1
    break
  fi
  echo "hdiutil create attempt $attempt failed; retrying in 15s..."
  rm -f "$dmg"
  sleep 15
done
if [ "$hdiutil_ok" -ne 1 ]; then
  echo "hdiutil create failed after 5 attempts" >&2
  exit 1
fi
hdiutil verify "$dmg"

printf '%s\n' "$dmg"
