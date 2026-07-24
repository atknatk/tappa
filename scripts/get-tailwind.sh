#!/usr/bin/env bash
# Tailwind CSS standalone ikilisini indirir. Node/npm GEREKTIRMEZ.
# Kullanim: scripts/get-tailwind.sh <surum> <hedef-yol>
set -euo pipefail

version=${1:?kullanim: get-tailwind.sh <surum> <hedef>}
dest=${2:?kullanim: get-tailwind.sh <surum> <hedef>}

os=$(uname -s | tr 'A-Z' 'a-z')
arch=$(uname -m)

case "$arch" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64)  arch=x64 ;;
  *) echo "desteklenmeyen mimari: $arch" >&2; exit 1 ;;
esac
case "$os" in
  darwin) os=macos ;;
  linux)  os=linux ;;
  *) echo "desteklenmeyen isletim sistemi: $os" >&2; exit 1 ;;
esac

url="https://github.com/tailwindlabs/tailwindcss/releases/download/${version}/tailwindcss-${os}-${arch}"
echo "indiriliyor: $url"

mkdir -p "$(dirname "$dest")"
tmp="${dest}.tmp"
trap 'rm -f "$tmp"' EXIT

curl -fsSL --retry 3 -o "$tmp" "$url"
chmod +x "$tmp"

# Indirilen sey gercekten calisan bir ikili mi (404 HTML sayfasi degil).
if ! "$tmp" --help >/dev/null 2>&1; then
  echo "indirilen dosya calistirilamadi — surum/asset adi degismis olabilir" >&2
  exit 1
fi

mv "$tmp" "$dest"
trap - EXIT
echo "hazir: $dest ($("$dest" --help 2>&1 | head -1))"
