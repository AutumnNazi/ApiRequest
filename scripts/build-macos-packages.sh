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
hdiutil create \
  -volname 'ApiRequest' \
  -srcfolder "$staging" \
  -format UDZO \
  -ov \
  "$dmg"
hdiutil verify "$dmg"

printf '%s\n' "$dmg"
