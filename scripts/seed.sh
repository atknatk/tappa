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

if ! docker compose ps --status running --services 2>/dev/null | grep -qx db; then
  echo "seed: postgres ayakta degil — once 'make up'." >&2
  exit 1
fi

# ON_ERROR_STOP: yarim yuklenmis fixture sessizce "basarili" sayilmasin.
docker compose exec -T db \
  psql -v ON_ERROR_STOP=1 --no-psqlrc -U "$DB_USER" -d "$DB_NAME" <"$SEED_FILE"
