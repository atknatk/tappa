#!/usr/bin/env bash
# Tappa — demo/test verisini yukler (bkz. skill tappa-seed).
#
# NEDEN konteynerdeki psql: host'ta psql kurulu olmayabilir. Bu repo Node'suz
# oldugu gibi gereksiz host araci da istemez (CLAUDE.md §1); `docker compose
# exec -T db psql` yerelde ve CI'da ayni komuttur, surum farki da yaratmaz —
# istemci her zaman sunucuyla ayni imajdan gelir.
#
# Kullanim: scripts/seed.sh [seed-dosyasi]   (varsayilan: test/fixtures/seed.sql)
# Ortam: SEED_DB_USER (varsayilan tappa_owner) · SEED_DB_NAME (varsayilan tappa)
#
# NOT: .env'deki migration baglanti dizesi bu hedefi ARTIK SURMUYOR — baglanti
# konteynerin icinden kuruluyor. Bilincli: o dize host'tan bakan bir adrestir
# (localhost:5432) ve konteynerin icinde anlamsizdir. Rol ayrimi korunuyor
# (tappa_owner), degisen yalnizca adresleme.
# (Degisken adini burada yazmiyoruz: R5 taramasi scripts/ altinda o adi
#  "uygulama migration rolu ile baglaniyor" isareti sayar — dogru davranis.)
set -euo pipefail
cd "$(dirname "$0")/.."

SEED_FILE=${1:-test/fixtures/seed.sql}
# Migration/seed yetkili rolle calisir; uygulama rolu (tappa_app) RLS'e tabidir
# ve seed verisini yazamaz (CLAUDE.md §6).
DB_USER=${SEED_DB_USER:-tappa_owner}
DB_NAME=${SEED_DB_NAME:-tappa}

if [[ ! -s $SEED_FILE ]]; then
  echo "seed: '$SEED_FILE' yok veya bos — once fixture yazilmali (skill tappa-seed)." >&2
  exit 1
fi

# Iki ayri ariza iki ayri mesaj vermeli: "docker yok" ile "postgres kapali"
# ayni sey degildir, tek mesaja indirmek hata avini yanlis yone surer.
if ! command -v docker >/dev/null 2>&1; then
  echo "seed: 'docker' PATH'te yok — bu hedef psql'i konteynerden calistirir." >&2
  exit 1
fi

# HEDEF SABITLENIR, VARSAYILMAZ — scripts/db-reset.sh'in basligindaki olcumun
# ayni ciftiyle (`-f` + `-p`). Sebep orada olculdu: Makefile `-include .env` +
# ciplak `export` yapiyor, yani COMPOSE_FILE ve COMPOSE_PROJECT_NAME gelistiricinin
# kabugundan ya da .env'den bu script'e SIZAR ve `cd` bunu engellemez (mutlak bir
# COMPOSE_FILE calisma dizinini yok sayar). Buradaki patlama yaricapi silme degil:
# olculdu, sabitlenmemis bicimde yabanci bir proje (`project=evil`) secilebiliyor,
# yani yabanci bir veritabanina demo satir YAZILABILIYOR ve yabanci konteynerler
# hedeflenebiliyor. Veri kaybi degil, ama yanlis makineye yazmak da ucuz degil ve
# duzeltmesi iki bayrak. `tappa` adi tahmin degil: docker-compose.yml onu
# `name: tappa` ile DECLARE ediyor, yani buradaki pin ile `make up`in kurdugu proje
# checkout dizini yeniden adlandirilinca ayrisamaz.
COMPOSE=(docker compose -f docker-compose.yml -p tappa)

if ! "${COMPOSE[@]}" ps --status running --services 2>/dev/null | grep -qx db; then
  echo "seed: postgres ayakta degil — once 'make up'." >&2
  exit 1
fi

# ON_ERROR_STOP: yarim yuklenmis fixture sessizce "basarili" sayilmasin.
"${COMPOSE[@]}" exec -T db \
  psql -v ON_ERROR_STOP=1 --no-psqlrc -U "$DB_USER" -d "$DB_NAME" <"$SEED_FILE"

# ADIM 2 — plaketlerin per-tag anahtarlarini KEK ile SARMALA.
#
# NEDEN AYRI BIR ADIM: tags.aes_key_ref bir AES-256-GCM zarfidir (ADR 0003 md.4,
# 44 bayt) ve iki sebeple SQL literali olamaz — operatörün KEK'ine baglidir (KEK
# repoda degil, ortamda) ve sun.Wrap her cagrida taze nonce ceker. Adim 1'in
# yazdigi placeholder duz ASCII'dir: `fixtures.SeedPlaceholderKeyLabel || uid`,
# yani ETIKETIN UZUNLUGU + 14 hex uid. Sayiyi buraya sabitlemiyoruz — bu yorumun
# bir onceki hali "50 bayt (36 karakterlik etiket)" diyordu ve etiket 30 karaktere
# indirilince YANLISLANDI; dogru yer Go sabitinin kendisi ve seed.sql'in yanindaki
# not (ikisi de bugun 30 + 14 = 44 diyor, olculdu: octet_length = 44).
#
# 🔴 UZUNLUK ARTIK SERBEST DEGIL: migration 00021'in
# tags_aes_key_ref_is_kek_envelope kisiti octet_length = 44 dayatiyor, yani
# placeholder 44 disinda bir uzunluga kayarsa adim 1 zaten 23514 ile olur ve
# `make seed` ilk yarisinda durur. 44'te kalirsa da plaket CALISIR SAYILMAZ: bayt
# dizisi DOGRU UZUNLUKTA ama YANLIS ICERIKTEDIR, sun.Unwrap uzunluk kontrolunu
# gecip GCM dogrulamasinda kalir ve her NFC tap yine 500 alir. Adim 2 bu yuzden
# zorunlu. Bu adim ayni psql'e, ayni role (tappa_owner) SQL akitir; Go tarafi
# hicbir yere BAGLANMAZ.
#
# TAPPA_TAG_KEK zorunlu: eksikse burada gurultuyle patlar, sessizce bozuk plaket
# birakmaz.
# Mesaj IKI ariza birden anlatir: degisken kabukta olmayabilir, ya da HIC
# URETILMEMIS olabilir (.env.example onu BOS gonderir). Yalnizca "export et"
# demek, ilk kez kuran gelistiriciyi var olmayan bir degeri aramaya yollar.
if [[ -z ${TAPPA_TAG_KEK:-} ]]; then
  echo "seed: TAPPA_TAG_KEK bos. Uret — openssl rand -base64 32 — ve .env'e yaz;" >&2
  echo "      'make seed' .env'i export eder, ciplak kabuk kendi export etmeli." >&2
  exit 1
fi

go run ./test/fixtures/seedkeys \
  | "${COMPOSE[@]}" exec -T db \
      psql -v ON_ERROR_STOP=1 --no-psqlrc -U "$DB_USER" -d "$DB_NAME"
