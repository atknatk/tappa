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

asset="tailwindcss-${os}-${arch}"
base="https://github.com/tailwindlabs/tailwindcss/releases/download/${version}"
url="${base}/${asset}"
echo "indiriliyor: $url"

mkdir -p "$(dirname "$dest")"
tmp="${dest}.tmp"
sums="${dest}.sha256sums"
trap 'rm -f "$tmp" "$sums"' EXIT

curl -fsSL --retry 3 -o "$tmp" "$url"

# 🔴 SAGLAMA DOGRULAMASI — indirilen sey CALISTIRILMADAN ONCE (M8-02 denetimi, C3).
#
# ONCEKI HALI: dosya indiriliyor, `chmod +x` ediliyor ve dogrulama olarak
# `--help` ile CALISTIRILIYORDU. Yani "bu gercekten bir ikili mi" sorusu, ikiliyi
# kosturarak cevaplaniyordu -- 404 HTML'ini eler ama degistirilmis bir ikiliyi
# elemez, ve o noktada kod zaten kosmustur.
#
# NEDEN ONEMLI: bu script `make css`in bagimligi, `make css` de `make build`in --
# yani bu ikili SEVK EDILEN Go ikilisinin derleme yolunda, Dockerfile'in builder
# asamasinda, `go build`den ONCE kosuyor.
#
# YARICAP (olculdu, 2026-08-15): takipli bir dosyayi degistiren bir sey
# vcs.modified=true uretir ve scripts/verify-image.sh push'u REDDEDER. Geriye
# gitignore'lu ama binary'e GOMULEN tek yuzey kalir: web/static/css/app.css.
# Yani eski durumdaki sinir, tamamen verify-image.sh'in dogru calismasina
# bagliydi -- ve o kapi bu turdan once HER KOSUDA hatali olarak kirmiziydi.
#
# Upstream sha256sums.txt yayinliyor (HTTP 200, olculdu). Bicim standart:
#   <64-hex>  ./tailwindcss-linux-x64
curl -fsSL --retry 3 -o "$sums" "${base}/sha256sums.txt"
want=$(awk -v a="./${asset}" '$2==a {print $1}' "$sums")
if [[ -z $want ]]; then
  echo "sha256sums.txt icinde '${asset}' yok — surum ya da varlik adi degismis olabilir" >&2
  exit 1
fi
# macOS `shasum -a 256`, Linux `sha256sum` — ikisi de ayni ilk alani basar.
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp" | awk '{print $1}')
else
  got=$(shasum -a 256 "$tmp" | awk '{print $1}')
fi
if [[ $got != "$want" ]]; then
  echo "sha256 UYUSMUYOR — indirilen dosya CALISTIRILMADI." >&2
  echo "  beklenen: $want" >&2
  echo "  gelen   : $got" >&2
  exit 1
fi
echo "sha256 dogrulandi: ${want:0:16}…"

chmod +x "$tmp"

# Indirilen sey gercekten calisan bir ikili mi (404 HTML sayfasi degil).
if ! "$tmp" --help >/dev/null 2>&1; then
  echo "indirilen dosya calistirilamadi — surum/asset adi degismis olabilir" >&2
  exit 1
fi

mv "$tmp" "$dest"
rm -f "$sums"
trap - EXIT
echo "hazir: $dest ($("$dest" --help 2>&1 | head -1))"
