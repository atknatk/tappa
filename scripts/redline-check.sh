#!/usr/bin/env bash
# Tappa — kirmizi cizgi taramasi (CLAUDE.md §4).
#
# Bu script MEKANIK bir agdir, kanit degildir. Temiz gecmesi ihlal olmadigini
# gostermez; derin denetim icin: agent `tappa-security-auditor`.
set -uo pipefail
cd "$(dirname "$0")/.."

RED=$'\033[31m'; YEL=$'\033[33m'; GRN=$'\033[32m'; DIM=$'\033[2m'; OFF=$'\033[0m'
fail=0

# Yalnizca kaynak kod taranir: dokumanlar ve .claude/ kurallari bu terimleri
# mesru olarak icerir, onlari eslestirmek gurultu uretir.
SRC=(cmd internal db web/templates web/static/js scripts)
GEN_EXCLUDE=(
  --glob '!*_templ.go'          # templ ciktisi
  --glob '!internal/store/*.go' # sqlc ciktisi
  --glob '!scripts/redline-check.sh' # bu script desenleri tanimi geregi icerir
)

have_rg() { command -v rg >/dev/null 2>&1; }
if ! have_rg; then
  echo "${YEL}uyari${OFF}: ripgrep (rg) bulunamadi — tarama atlaniyor."
  exit 0
fi

scan() { rg -n --no-heading "${GEN_EXCLUDE[@]}" "$@" "${SRC[@]}" 2>/dev/null; }

report() { # report <seviye> <kod> <baslik> <bulgular>
  local level=$1 code=$2 title=$3 hits=$4
  if [[ -n "$hits" ]]; then
    local color=$RED; [[ $level == WARN ]] && color=$YEL
    printf '%s[%s · %s]%s %s\n' "$color" "$code" "$level" "$OFF" "$title"
    sed 's/^/    /' <<<"$hits"
    echo
    [[ $level == FAIL ]] && fail=1
  fi
}

echo "${DIM}tappa redline-check${OFF}"
echo

# --- R1: biyometrik veri -----------------------------------------------------
report FAIL R1 "Biyometrik veri izi — Tappa biyometri toplamaz/saklamaz" \
  "$(scan -i -e 'fingerprint' -e 'biometric' -e 'face[_-]?id' -e 'touch[_-]?id' -e 'webauthn' \
      | grep -viE 'no.?fingerprint|biyometri|not stored|asla' || true)"

# --- R2: surekli konum takibi ------------------------------------------------
report FAIL R2 "Surekli konum takibi — GPS yalnizca tap aninda okunur" \
  "$(scan -e 'watchPosition' -e 'BackgroundGeolocation' -e 'geofence' || true)"

# --- R3: transactions immutability ------------------------------------------
report FAIL R3 "transactions tablosuna UPDATE/DELETE — kayitlar immutable" \
  "$(scan -i -e '(UPDATE|DELETE +FROM) +transactions\b' || true)"

# --- R4: atomik ctr / replay koruması ---------------------------------------
# Kosulsuz sayac guncellemesi = TOCTOU replay acigi.
bad_ctr=$(scan -i -e 'UPDATE +tags +SET +last_ctr' | grep -viE 'last_ctr *<' || true)
report FAIL R4 "Kosulsuz last_ctr guncellemesi — 'WHERE last_ctr < \$n' sart" "$bad_ctr"

report WARN R4 "Go tarafinda sayac karsilastirmasi — atomikligi SQL'e birak" \
  "$(scan -e '(LastCtr|last_ctr) *(<|>|>=|<=) ' --glob '!*.sql' || true)"

# --- R5: tenant izolasyonu / RLS --------------------------------------------
missing_rls=""
for f in db/migrations/*.sql; do
  [[ -e $f ]] || continue
  # Migration icinde CREATE TABLE varsa, RLS uclusu de olmali.
  if grep -qiE 'CREATE +TABLE' "$f"; then
    for need in 'ENABLE ROW LEVEL SECURITY' 'FORCE ROW LEVEL SECURITY' 'CREATE POLICY' 'WITH CHECK'; do
      grep -qiF "$need" "$f" || missing_rls+="$f: eksik → $need"$'\n'
    done
  fi
done
report FAIL R5 "Migration'da RLS eksik (ENABLE + FORCE + POLICY + WITH CHECK)" "${missing_rls%$'\n'}"

report FAIL R5 "Uygulama migration rolu ile baglaniyor — RLS etkisiz kalir" \
  "$(scan -e 'DATABASE_MIGRATE_URL' --glob '!cmd/migrate/*' --glob '!internal/config/*' || true)"

report WARN R5 "SET LOCAL degil duz SET — havuzdaki baglantiyi kirletir" \
  "$(scan -e "SET +app\.tenant_id" | grep -viE 'SET +LOCAL' || true)"

# --- R7: sir sizintisi -------------------------------------------------------
report FAIL R7 "Sir loglanıyor olabilir (token / cmac / anahtar / davet kodu)" \
  "$(scan -i -e '(slog|log|fmt)\.[A-Za-z]+\([^)]*(token|cmac|aes_?key|secret|invite_?code)' || true)"

report FAIL R7 "Repoda gomulu anahtar dosyasi" \
  "$(git ls-files '*.pem' '*.key' '*.aes' 'secrets/*' 2>/dev/null || true)"

report WARN R7 "Sabit zamanli karsilastirma kullanilmali (subtle.ConstantTimeCompare)" \
  "$(scan -e 'bytes\.Equal\([^)]*([Cc]mac|[Mm]ac|[Tt]oken)' || true)"

# --- R6: kayit kaybi ---------------------------------------------------------
report WARN R6 "Sessizce yutulan hata — kayit kaybina yol acabilir" \
  "$(scan -e '^\s*_ = err\b' -e 'if err != nil \{\s*\}' || true)"

# --- Node yasagi (bkz. CLAUDE.md §1) ----------------------------------------
node_files=$(git ls-files 'package.json' 'package-lock.json' 'pnpm-lock.yaml' 'yarn.lock' 2>/dev/null || true)
report FAIL N1 "Node artefakti — bu repo Node'suzdur" "$node_files"

if [[ $fail -eq 0 ]]; then
  echo "${GRN}✓${OFF} mekanik tarama temiz. ${DIM}(kanit degil — derin denetim icin tappa-security-auditor)${OFF}"
else
  echo "${RED}✗${OFF} kirmizi cizgi ihlali var. Duzeltmeden commit etme."
fi
exit $fail
