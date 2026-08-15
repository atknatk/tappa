#!/bin/sh
# Tappa -- GELISTIRME parolasi. URETIMDE KOSMAZ VE KOSAMAZ.
#
# 01-roles.sql artik tappa_app'i NOLOGIN ve PAROLASIZ yaratiyor (gerekcesi orada:
# olculmus bir fail-open). Girisi acmak iki yerde, ve ikisi de o dosyanin disinda:
#
#   uretim     deploy/k8s/postgres-init/02-app-password.sh  -- Secret'tan gelen deger
#   gelistirme BU DOSYA                                     -- .env.example'daki 'tappa'
#
# ⚠️ NEDEN AYRI BIR DOSYA VE NEDEN URETIM KOPYASI DEGIL. Uretim ConfigMap'i
# .github/workflows/deploy.yml icinde ACIKCA IKI DOSYA ile kuruluyor
# (--from-file=01-roles.sql=... --from-file=02-app-password.sh=...), yani bu dosya
# oraya YAPISAL olarak giremez. Ama birinin ileride dizin kipine gecmesi
# (--from-file=scripts/db-init/) delik acardi ve hicbir sey soylemezdi.
#
# 🔴 BU YUZDEN KORUMA DOSYANIN KENDISINDE: TAPPA_APP_PASSWORD set ise BU SCRIPT
# BASARISIZ OLUR. O degisken YALNIZCA uretim StatefulSet'inde vardir
# (deploy/k8s/10-postgres.yaml), yani yanlislikla mount edilmesi sessiz bir
# gerileme degil, GURULTULU bir init hatasi uretir -- ve PGDATA'da tappa_app
# NOLOGIN kalir, yani sonuc fail-CLOSED olur.
set -eu

if [ -n "${TAPPA_APP_PASSWORD:-}" ]; then
	echo "02-dev-only-password.sh: REFUSING TO RUN. TAPPA_APP_PASSWORD is set, which means this is a PRODUCTION database, and this script would give tappa_app the development password that is written in this repository. Mount deploy/k8s/postgres-init/02-app-password.sh instead." >&2
	exit 1
fi

# Sabit deger, .env.example ve .github/workflows/ci.yml ile birebir. Bu bir sir
# DEGILDIR ve olmasi da beklenmez: yalnizca localhost'taki gelistirme veritabanina
# ve CI'nin gecici konteynerine baglanir.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-'EOSQL'
	ALTER ROLE tappa_app LOGIN PASSWORD 'tappa';
EOSQL

echo "02-dev-only-password.sh: tappa_app can now log in with the DEVELOPMENT password"
