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
#
# `test` M5-09'da EKLENDI (guvenlik denetimi olctu): test/fixtures altinda
# `_test.go` OLMAYAN gercek Go kaynagi var (tagkeys.go, seedkeys/main.go) ve
# fixture SQL'i var; liste onlari icermeyince "make audit temiz" cumlesi
# gorundugunden az sey kanitliyordu. Dokuz desenin dokuzu da `test/` uzerinde
# temiz dondu, sifir yanlis pozitif. DIKKAT: asagidaki `_test.go` muafiyetleri
# (R3/R5, uretim-kodu kurallari) korunur; test/fixtures/*.go `_test.go` DEGILDIR,
# yani gercekten taranir — istenen budur.
SRC=(cmd internal db web/templates web/static/js scripts test)
GEN_EXCLUDE=(
  --glob '!*_templ.go'          # templ ciktisi
  --glob '!internal/store/*.go' # sqlc ciktisi
  --glob '!scripts/redline-check.sh' # bu script desenleri tanimi geregi icerir
)

# 🔴 rg YOKSA BU SCRIPT BASARISIZ OLUR — ESKIDEN `exit 0` VERIYORDU (2026-08-14).
# Olculdu: `env PATH=/usr/bin:/bin ./scripts/redline-check.sh` uyariyi basip
# EXIT=0 donuyordu, yani "tarama atlandi" ile "tarama temiz" cagirana AYNI
# gorunuyordu. `make audit`in ozet satiri bunu daha da gizliyordu: govulncheck
# yesilken cikti "audit: govulncheck exit=0 - redline-check exit=0" ve make exit 0.
#
# CI'DA BU DELIK KAPALIYDI (.github/workflows/ci.yml rg'yi kurar ve `rg --version`
# ile kanitlar), yani UYGULAMA KAPISI tutuyordu; kor olan yerel donguydu -- ve
# kritik olan sey su: agin KENDISI degil, ag HAKKINDAKI IDDIA yanlisti.
#
# EXIT 2 SECILDI, 1 DEGIL. 1 "ihlal bulundu" demek ve alt tarafta $fail ile
# donuluyor; ikisini ayni sayiya bindirmek "tarayamadim"i "ihlal var" gibi
# raporlardi. 2 = ARAC EKSIK; cagiran ucunu ayirt edebilir. Basari yolunda
# davranis degismedi.
# 🔴 `command -v` YETMEZ — BOZUK VE CALISTIRILAMAZ rg DE "TEMIZ" GORUNUYORDU.
# Olculdu: PATH'e bozuk bir `rg` shim'i konunca `command -v rg` BASARILI donuyor,
# scan() rg'nin stderr'ini `2>/dev/null` ile yutuyor ve cikis kodunu hic okumuyor,
# sonuc "✓ mekanik tarama temiz" + EXIT=0. `chmod -x rg` de ayni: bash'in
# `command -v`'si X bitini dogrulamıyor. `rg --version` ikisini de ayirt eder --
# bozuk shim'de exit 2, calistirilamayan dosyada exit 126.
# ⚠️ UCUNCU SONDA `rg --version` DEGIL, GERCEK BIR TARAMA. Bir denetim gercek rg ile
# gosterdi: gecersiz bir bayrak iceren RIPGREP_CONFIG_PATH altinda `rg --version`
# EXIT 0 verir (yapilandirma okunmaz) ama her arama exit 2 doner. Sonda scan()'in
# kullandigi bayrak kumesini kosar, yani "surum yazdirabiliyor" degil "arama
# yapabiliyor" olcusu.
have_rg() {
  command -v rg >/dev/null 2>&1 || return 1
  rg --version >/dev/null 2>&1 || return 1
  rg -n --no-heading "${GEN_EXCLUDE[@]}" -e 'package' scripts >/dev/null 2>&1
  local rc=$?
  [[ $rc -le 1 ]]
}
if ! have_rg; then
  echo "${RED}ATLANDI${OFF}: ripgrep (rg) yok ya da CALISMIYOR — TARAMA KOSMADI." >&2
  echo "  Kur: brew install ripgrep  (CI bunu kurar ve rg --version ile kanitlar)" >&2
  echo "  Bu bir 'temiz' sonucu DEGILDIR." >&2
  exit 2
fi

# 🔴 rg'NIN CIKIS KODU OKUNUR — ESKIDEN OKUNMUYORDU (2026-08-14).
# rg: 0 = eslesme var, 1 = eslesme yok, >=2 = HATA. Eski hali stderr'i yutup kodu
# hic okumuyordu, yani CALISAN ama her cagrisi patlayan bir rg "eslesme yok" gibi
# gorunuyordu. Olculdu (gercek rg, shim degil): gecersiz bir bayrak iceren
# RIPGREP_CONFIG_PATH ile `rg --version` exit 0 verip have_rg'yi geciyor, her scan()
# exit 2 doner, ve script agacta GERCEK bir R7 ihlali dururken "✓ mekanik tarama
# temiz" + exit 0 basiyordu.
#
# 🔴 VE `exit` BURADAN ISE YARAMAZ: scan her zaman `$(...)` icinde cagriliyor, yani
# ALT KABUKTA kosuyor ve `exit 2` yalnizca o alt kabugu bitirir -- olculdu, script
# yine "temiz" + exit 0 basiyordu. Bu yuzden iki mekanizma var: asagidaki ON-UCUS
# (have_rg icinde, ana kabukta) ve her cagrida yazilan SCAN_ERR isaretcisi; ikincisi
# sonda okunuyor.
SCAN_ERR=$(mktemp -t tappa-redline-scan-err)
trap 'rm -f "$SCAN_ERR"' EXIT
scan() {
  local out rc
  out=$(rg -n --no-heading "${GEN_EXCLUDE[@]}" "$@" "${SRC[@]}" 2>&1)
  rc=$?
  if [[ $rc -gt 1 ]]; then
    { echo "${RED}ATLANDI${OFF}: ripgrep taramasi hata verdi (exit $rc) — TARAMA GUVENILIR DEGIL."
      sed 's/^/    /' <<<"$out"
      echo "  Bu bir 'temiz' sonucu DEGILDIR."
    } >&2
    echo "$rc" >>"$SCAN_ERR"
    return 0
  fi
  [[ $rc -eq 0 ]] && printf '%s\n' "$out"
  return 0
}

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
#
# MUAFIYET: TEK IFADE, YOLA SINIRLI, VE HER KOSUDA RAPORLANIR (M7-01, 2026-08-13).
# Tam gerekce, elenen alternatifler ve kabul edilen risk: docs/adr/0012-*.md.
#
# NE OLDU: M7-01 urunun ILK KAMUYA ACIK sayfasini ekledi ve o sayfa, kartin
# istedigi gibi, bir parmak izi terminaliyle KARSILASTIRMA tablosu tasiyor
# (handoff §9). Yani urunde ilk kez, §4.1'i IHLAL ETMEYEN degil, §4.1'i ILAN EDEN
# metin bu terimi kullaniyor. Olculdu: M7-01 oncesi agac bu taramada temizdi.
#
# 🔴 ILK HALI IKI IFADEYI TUM AGACA UYGULUYORDU VE GORUNMEZDI. Bir guvenlik
# denetcisi bunu kirdi: `internal/handler/marketing.go`'ya
# `// PROBE: fingerprint terminal -- keep the webauthn attestation` eklemek taramayi
# exit 0 birakiyordu, R1 hic raporlanmadan. Uc sey degisti:
#
#   1. IKI IFADE DEGIL BIR IFADE. `no biometric` KALDIRILDI cunku GEREKMIYORDU:
#      pages/activate.templ M5-02'den beri "no biometric data of any kind: no
#      fingerprints, no face, no voice" diyor ve bu, AYNI SATIRDAKI `no fingerprints`
#      sayesinde MEVCUT `no.?fingerprint` girdisinden gecer. Pazarlama metni de o
#      kaliba getirildi -> genisletme yariya indi. Geriye kalan `fingerprint
#      terminal` kacinilmazdir: kartin istedigi tablonun OZNESIDIR.
#   2. YOLA SINIRLI. Muafiyet yalnizca pazarlama yuzeyinin dosyalarinda gecerli.
#      Baska her yerde ayni ifade FAIL uretir.
#   3. GORUNUR. R5'in ilkesi (satir ~153: "muafiyet gorunmez olamaz") artik R1'e de
#      uygulaniyor: kullanilan her muafiyet HER KOSUDA WARN olarak basilir.
#
# ⚠️ SINIR, ACIKCA: `grep -v` SATIR bazlidir, yani muaf yollardaki bir dosyada, bu
# ifadeyi tasiyan bir satirdaki GERCEK bir ihlal de muaf kalir. Bu, mevcut
# `not stored` ve `asla` girdilerinin de tasidigi ayni zayifliktir — `asla` Turkce
# yorumlarda cok yaygin oldugu icin ikisinden DAHA GENISTIR. Bu betigin kendi
# basligindaki cumlenin sebebi budur: MEKANIK bir agdir, kanit degildir.
# Derin denetim: agent `tappa-security-auditor`.
#
# ⚠️ MUAF SATIRLAR BURADA SAYILMAZ — asagidaki WARN blogu CANLI LISTEDIR. Bu
# dosyada elle yazilan bir sayi bir kez zaten bayatladi ("kalan dordu sayfanin
# GORUNEN metnidir" derken gercekte dokuz satirdi ve ucu gorunen metin degildi);
# ureten komut `./scripts/redline-check.sh`'in kendisidir.
# TETIKLEYICILER TEK YERDE TANIMLI: hem taramaya hem de muafiyetin "artik baska
# tetikleyici kalmadi mi" kontrolune AYNI ifade gider. Iki kopya olsaydi biri
# digerinden sapabilir ve muafiyet tanimadigi bir terimi sessizce affederdi.
R1_TRIGGERS='fingerprint|biometric|face[_-]?id|touch[_-]?id|webauthn'
R1_WAIVER_PHRASE='fingerprint terminal'
R1_WAIVER_PATHS='^(web/templates/pages/landing[^:]*\.(templ|go)|internal/handler/marketing[^:]*\.go):'

r1_raw="$(scan -i -e "$R1_TRIGGERS" \
    | grep -viE 'no.?fingerprint|biyometri|not stored|asla' || true)"

# r1_select <waived|fail>
#
# 🔴 BIR SATIR UC KOSUL BIRDEN SAGLANIRSA MUAFTIR, ve ucuncusu bir guvenlik
# denetcisinin sondasindan ogrenildi:
#   1. yol muaf kumede,
#   2. satir `fingerprint terminal` iceriyor,
#   3. VE O IFADE CIKARILDIGINDA GERIYE HICBIR TETIKLEYICI KALMIYOR.
#
# Ucuncusu olmadan muafiyet SATIRIN TAMAMINI affediyordu, satirin muaf OLAN
# parcasini degil. Denetcinin sondasi tam olarak bunu kullandi:
#   `// PROBE: fingerprint terminal -- keep the webauthn attestation`
# — muaf ifadeyi bir tasiyici olarak kullanip ayni satirda `webauthn` gizliyordu ve
# tarama exit 0 donuyordu. Simdi ifade cikarilir, kalan metin yeniden sinanir ve
# `webauthn` FAIL uretir. Muafiyet artik "bu satiri gormezden gel" degil, "bu
# ifadenin KENDISI bir ihlal degil" demektir.
r1_select() {
  awk -v mode="$1" -v ph="$R1_WAIVER_PHRASE" -v paths="$R1_WAIVER_PATHS" -v trig="$R1_TRIGGERS" '
    length($0) == 0 { next }
    {
      w = 0
      if ($0 ~ paths) {
        rest = tolower($0)
        if (index(rest, ph) > 0) {
          gsub(ph, " ", rest)          # muaf ifadeyi cikar
          if (rest !~ trig) w = 1      # geriye tetikleyici kalmadiysa muaf
        }
      }
      if ((mode == "waived") == w) print
    }' <<<"$r1_raw"
}

report FAIL R1 "Biyometrik veri izi — Tappa biyometri toplamaz/saklamaz" "$(r1_select fail)"
report WARN R1 "R1 muafiyeti kullanildi (yola sinirli, ADR 0012) — muafiyet sessiz kalamaz" \
  "$(r1_select waived)"

# --- R2: surekli konum takibi ------------------------------------------------
report FAIL R2 "Surekli konum takibi — GPS yalnizca tap aninda okunur" \
  "$(scan -e 'watchPosition' -e 'BackgroundGeolocation' -e 'geofence' || true)"

# --- R3: transactions immutability ------------------------------------------
# _test.go MUAFIYETI (DAR — yalniz bu tarama, yalniz test dosyalari): bu kural
# URETIM kodu icindir. RLS izolasyon testi (internal/db/rls_test.go) transactions
# uzerinde UPDATE/DELETE'i BILEREK calistirir — amaci bu ifadelerin REVOKE/trigger
# ile REDDEDILDIGINI kanitlamak (CLAUDE.md §4.3, §8 "RLS testi zorunlu"). Test
# dosyasini taramak yanlis-pozitif uretirdi; uretim kodundaki ihlal hala yakalanir.
# 🔴 SEMA NITELEMESI: DESEN ESKIDEN `public.transactions` YAZIMINI GORMUYORDU ve bu
# olculdu (M8-02 FAZ E denetimi, 2026-08-16):
#   UPDATE transactions SET x=1;          -> eslesti
#   UPDATE public.transactions SET x=1;   -> ESLESMEDI
#   DELETE FROM public.transactions;      -> ESLESMEDI
# Agacta o ana kadar nitelemeli tek bir yazim yoktu, yani kural YANLIS SEBEPLE
# yesildi; ilk nitelemeli mutasyonu bu turun kendi dogrulama araci getirdi. Yeni
# desen istege bagli bir sema onekini ve tirnakli tanimlayiciyi da aliyor. Kontrol
# olarak `transaction_reviews`, `transactions_archive`, `SELECT ... FROM
# transactions` ve `INSERT INTO transactions` DENENDI: dordu de eslesMIYOR.
#
# _test.go MUAFIYETI (DAR — yalniz bu tarama, yalniz test dosyalari): bu kural
# URETIM kodu icindir. RLS izolasyon testi (internal/db/rls_test.go) transactions
# uzerinde UPDATE/DELETE'i BILEREK calistirir — amaci bu ifadelerin REVOKE/trigger
# ile REDDEDILDIGINI kanitlamak (CLAUDE.md §4.3, §8 "RLS testi zorunlu").
#
# ⚠️ IKINCI MUAFIYET, TEK DOSYA ADIYLA: scripts/pg-restore-verify.sh. Ayni gerekce,
# uretim kodu yerine bir OPERATOR ARACI icin: o dosya geri yuklenmis bir veritabaninda
# `UPDATE/DELETE public.transactions ... WHERE false` deneyip reddin SQLSTATE'inin
# 42501 (yetki) oldugunu assert ediyor -- yani bu kuralin korudugu seyi KANITLIYOR.
# `WHERE false` hicbir satira dokunmuyor ve ifadeler ROLLBACK'li bir islemin icinde.
# Muafiyeti dosya ADIYLA vermek, deseni gevsetmekten (ornegin `WHERE false`'u suzmek)
# bilerek tercih edildi: bir suzgec genisletmesi agin kendisini asindirir, dosya adi
# ise bir kez yazilir ve gozden gecirilir. Bedeli sayiliyor: o dosyadan `WHERE false`
# kaldirilirsa R3 ARTIK GORMEZ -- onu goren sey, ayni dosyanin kendi 42501 assert'idir.
report FAIL R3 "transactions tablosuna UPDATE/DELETE — kayitlar immutable" \
  "$(scan -i -e '(UPDATE|DELETE +FROM) +([a-zA-Z_][a-zA-Z0-9_]*\.)?"?transactions"?\b' \
        --glob '!**/*_test.go' --glob '!scripts/pg-restore-verify.sh' || true)"

# --- R4: atomik ctr / replay koruması ---------------------------------------
# Kosulsuz sayac guncellemesi = TOCTOU replay acigi.
#
# SINIR (yanlis-NEGATIF; M6-06 B fazinda olculdu, 2026-08-09) -- BU KURAL SATIR
# YERELDIR. `scan` satir satir okur, yani ayni ifade SATIRLARA BOLUNDUGUNDE bu desen
# onu GORMEZ. Uretim konumlarinda olculdu:
#   tek satirda, db/queries/tags.sql ............... rc=1, YAKALANDI
#   tek satirda, bir Go const'unda ................. rc=1, YAKALANDI
#   `UPDATE tags t` / `SET last_ctr = @ctr` / ...... rc=0, SESSIZ
#   ayni bolunme bir Go ham dizesi icinde .......... rc=0, SESSIZ
# Ucuncu sekil, urunun EN kritik ifadesi olan AdvanceTagCounter'in kendi bicimidir.
#
# COK SATIRLI YAPILMADI -- ORKESTRATOR KARARI (2026-08-09), ve gerekcesi bir sayi:
# `rg -U` ile ayni SRC kumesinde 11 blok / 3 dosya eslesti, GERCEK ihlal 0, ve
# yanlis alarmlarin icinde AdvanceTagCounter'IN KENDISI vardi -- cunku bu satirdaki
# muafiyet suzgeci `last_ctr <` arar, o ifadenin korumasi ise CTE'de
# `prev.old_ctr <` diye yazilidir. Suzgeci de genislemek aga ozel-durum
# biriktirmektir (M6-05 A'da bir tel genislemesi 30 mesru olcumu isaretleyip geri
# alinmisti).
#
# ⚠️ BU YUZDEN "R4 yakalar" CUMLESI TEK BASINA YAZILMAMALIDIR. §4.4'un gercek
# garantisi bu grep degil, db/queries/tags.sql'deki kati `prev.old_ctr < @ctr`
# yuklemi, 00013'un tags_counter_monotonic trigger'i ve onlari kosan yaris
# testleridir. Ayrintili kayit: internal/db/tagsinventory_test.go dosya basligi.
bad_ctr=$(scan -i -e 'UPDATE +tags +SET +last_ctr' | grep -viE 'last_ctr *<' || true)
report FAIL R4 "Kosulsuz last_ctr guncellemesi — 'WHERE last_ctr < \$n' sart" "$bad_ctr"

report WARN R4 "Go tarafinda sayac karsilastirmasi — atomikligi SQL'e birak" \
  "$(scan -e '(LastCtr|last_ctr) *(<|>|>=|<=) ' --glob '!*.sql' || true)"

# --- R5: tenant izolasyonu / RLS --------------------------------------------
# CLAUDE.md §6: her yeni tablo BES unsurla dogar (+ GRANT):
#   1. tenant_id uuid NOT NULL          4. tenant_id uzerinde politika
#   2. ENABLE ROW LEVEL SECURITY           (hem USING hem WITH CHECK)
#   3. FORCE ROW LEVEL SECURITY         5. tenant_id ONDE olan indeks
#   + tappa_app GRANT'i
#
# TETIKLENME KOSULU — yanlis pozitif uretmemek icin dar tutuldu:
#   · yalnizca db/migrations/*.sql taranir (baska hicbir yol);
#   · yalnizca `-- +goose Up` bolumu denetlenir; `-- +goose Down`'dan sonrasi
#     ATILIR — yoksa Down'daki ALTER/POLICY/GRANT satirlari Up'in eksigini
#     kapatiyormus gibi gorunur ve uretimde sifir RLS'li tablo sessizce gecer;
#   · yalnizca `CREATE TABLE [IF NOT EXISTS] [sema.]ad (` ile yaratilan tablolar
#     — TEMP/UNLOGGED tablolar ve goose'un kendi goose_db_version'i disarida;
#   · besinin de AYNI dosyada olmasi beklenir ("tablo bunlarla DOGAR").
#
# `tenants` ISTISNASI (M1-02): o tabloda kapsam sutunu `tenant_id` degil `id`.
# Istisna DAR: yalnizca semasiz ya da `public.` olan `tenants` icin gecerlidir
# (`archive.tenants` normal tablo sayilir) VE istisnanin dayandigi gerekce —
# "kolon/indeks maddelerini PRIMARY KEY karsilar" — ayrica dogrulanir: PK
# gercekten `id` uzerinde degilse bulgu yazilir.
#
# MUAFIYET: gercekten tenant kapsamsiz bir tablo icin (ornegin global katalog)
# migration'a su satir yazilir — SOZDIZIMI KATIDIR ve yalnizca `--` ile baslayan
# GERCEK bir yorum satirindan okunur (string literali ya da /* */ blogu muaf
# edemez), ustelik her taramada WARN olarak basilir; muafiyet gorunmez olamaz:
#       -- redline: no-tenant-scope(tablo_adi) — gerekce
missing_rls=""
waiver_report=""
unverified=""

# `-- +goose Down` ve sonrasini atar: yalnizca Up denetlenir.
goose_up() {
  awk 'tolower($0) ~ /^[ \t]*--[ \t]*[+]goose[ \t]+down/ { exit } { print }' "$1"
}

# SQL sozcuk ayiricisi (stdin → stdout). Uc isi birden yapar:
#   · `--` satir yorumunu ve `/* … */` blok yorumunu siler. Blok yorumu SILINMEZSE
#     icindeki `;` ifade ayirici sayilir ve blogun 2. ifadesinden itibaren her sey
#     gercek SQL gibi gorunur — "RLS'i sonra acarim" diye yorumlanmis bir tablo
#     tam puan alirdi;
#   · string literallerini ve `$$ … $$` govdelerini KORUR, icindeki `;` yerine
#     \001 koyar — `DEFAULT ';'` gibi bir sutun ifadeyi bolmesin (yanlis pozitif),
#     plpgsql govdesi de parcalanmasin;
#   · soldan saga tek gecis: string icindeki `--` yorum, yorum icindeki `'` string
#     sayilmaz.
# SINIR: adlandirilmis dolar tirnagi ($body$ …) bilinmez; `$$` kullanin.
sql_lex() {
  awk '
    BEGIN { inb = 0; ins = 0; ind = 0 }
    {
      line = $0; out = ""; i = 1; n = length(line)
      while (i <= n) {
        c = substr(line, i, 1); c2 = substr(line, i, 2)
        if (inb) {
          if (c2 == "*/") { inb = 0; i += 2 } else { i++ }
        } else if (ind) {
          if (c2 == "$$") { ind = 0; out = out c2; i += 2 }
          else { out = out ((c == ";") ? "\001" : c); i++ }
        } else if (ins) {
          if (c == "\047") { ins = 0; out = out c; i++ }
          else { out = out ((c == ";") ? "\001" : c); i++ }
        } else if (c2 == "/*")   { inb = 1; i += 2 }
        else if (c2 == "--")     { i = n + 1 }
        else if (c2 == "$$")     { ind = 1; out = out c2; i += 2 }
        else if (c == "\047")    { ins = 1; out = out c; i++ }
        else                     { out = out c; i++ }
      }
      print out
    }'
}

# Bir migration'in Up bolumunu tek satirlik, kucuk harfli ifadelere ayirir.
# Boylece cok satirli CREATE POLICY / GRANT tek grep ile denetlenebilir.
# NOT: ifade ayirmak icin `tr` kullanilir, `sed s/;/;\n/` DEGIL — BSD sed
# (macOS) degistirme tarafinda \n'i yeni satir saymaz, harfi harfine 'n' yazar
# ve tum dosya tek satira cokerdi.
sql_statements() {
  goose_up "$1" \
    | sql_lex \
    | tr '[:upper:]' '[:lower:]' \
    | tr '\n' ' ' \
    | tr ';' '\n' \
    | tr '\001' ';' \
    | sed -E 's/[[:space:]]+/ /g; s/^ //; s/ $//' \
    | grep -v '^$'
}

for f in db/migrations/*.sql; do
  [[ -e $f ]] || continue
  stmts=$(sql_statements "$f")

  # Muafiyet HAM dosyadan, yalnizca `--` ile baslayan satirlardan ve katı
  # sozdizimiyle okunur (buyuk harf / bosluksuz / string icindeki varyantlar
  # gecmez). Gerekce zorunlu: kapanis parantezinden sonra metin olmali.
  # Muafiyet Up bolumunden okunur (goose_up satir numaralarini korur): Down'a
  # yazilmis bir muafiyet Up'taki tabloyu susturmasin.
  waiver_lines=$(goose_up "$f" | grep -nE '^[[:space:]]*-- redline: no-tenant-scope\([a-z0-9_.]+\) .+' || true)
  waived=""
  if [[ -n $waiver_lines ]]; then
    waived=$(sed -E 's/.*no-tenant-scope\(([a-z0-9_.]+)\).*/\1/' <<<"$waiver_lines" \
               | tr -d '"' | sed -E 's/^public\.//')
    waiver_report+=$(sed "s|^|$f:|" <<<"$waiver_lines")$'\n'
  fi

  tables=$(grep -oE '^create table (if not exists )?("?[a-z0-9_]+"?\.)?"?[a-z0-9_]+"?' <<<"$stmts" \
             | sed -E 's/^create table (if not exists )?//' | tr -d '"' \
             | sed -E 's/^public\.//' | sort -u || true)

  # R5 yalnizca ifadenin BASINDAKI `create table`'i tanir. Bir tablo `DO $$ … $$`
  # ya da `CREATE SCHEMA … CREATE TABLE …` icinden yaratilirsa denetim disinda
  # kalir; sessizce gecmesin diye yuksek sesle "dogrulayamadim" der.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    unverified+="$f: ifade 'create table' ile BASLAMIYOR, R5 denetleyemedi → ${s:0:80}"$'\n'
  done < <(grep -E 'create +table' <<<"$stmts" | grep -vE '^create table ' || true)

  for qual in $tables; do
    t=${qual##*.}
    schema=""; [[ $qual == *.* ]] && schema=${qual%.*}

    if [[ -n $waived ]] && { grep -qxF "$qual" <<<"$waived" || grep -qxF "$t" <<<"$waived"; }; then
      continue
    fi

    # Ifade eslestirmede kullanilacak tablo referansi. Semasiz tanimlanan tablo
    # sonradan `public.` ile nitelenebilir; public DISI bir sema ise birebir
    # istenir — `archive.tenants`, `public.tenants`'in RLS'ini odunc alamaz.
    if [[ -z $schema ]]; then tref="(\"?public\"?\.)?\"?${t}\"?"
    else                     tref="\"?${schema}\"?\.\"?${t}\"?"
    fi

    create=$(grep -E "^create table (if not exists )?${tref}[ (]" <<<"$stmts" || true)

    col=tenant_id
    scoped_by_pk=0
    if [[ $t == tenants && -z $schema ]]; then
      col=id
      scoped_by_pk=1
      # Istisnanin GEREKCESI kontrol edilir, varsayilmaz.
      grep -qE "(^|[(,]) *\"?id\"? +[a-z0-9_]+[^,]*primary key" <<<"$create" \
        || grep -qE "primary key *\( *\"?id\"?[ ,)]" <<<"$create" \
        || missing_rls+="$f [$qual]: 'tenants' istisnasi PRIMARY KEY'in id uzerinde olmasini gerektirir"$'\n'
    fi

    # 1 + 5 — tenants'ta PK zaten NOT NULL ve indeks saglar.
    if (( ! scoped_by_pk )); then
      grep -qE "(^|[(,]) *\"?tenant_id\"? +uuid[^,]*(not null|primary key)" <<<"$create" \
        || missing_rls+="$f [$qual]: eksik → tenant_id uuid NOT NULL"$'\n'

      idx_ok=0
      # (a) CREATE INDEX ... ON <t> (tenant_id, ...)
      while IFS= read -r s; do
        [[ -n $s ]] || continue
        after=${s#* on }; cols=${after#*\(}
        [[ $cols == tenant_id* || $cols == '"tenant_id"'* ]] && idx_ok=1
      done < <(grep -E "^create (unique )?index .* on (only )?${tref}[ (]" <<<"$stmts" || true)
      # (b) tablo govdesindeki PRIMARY KEY / UNIQUE kisiti da indeks yaratir
      grep -qE "(primary key|unique)[^(]*\( *\"?tenant_id\"?[ ,)]" <<<"$create" && idx_ok=1
      (( idx_ok )) || missing_rls+="$f [$qual]: eksik → tenant_id ONDE olan indeks"$'\n'
    fi

    # 2 + 3 — RLS acik ve tablo sahibine de zorunlu.
    # NOT: ${x^^} kullanilmaz — macOS'un bash 3.2'si desteklemez.
    for mode in ENABLE FORCE; do
      lc=$(tr '[:upper:]' '[:lower:]' <<<"$mode")
      grep -qE "^alter table (only )?${tref} ${lc} row level security" <<<"$stmts" \
        || missing_rls+="$f [$qual]: eksik → ${mode} ROW LEVEL SECURITY"$'\n'
    done

    # 4 — politika: kapsam sutununu okumali, hem USING hem WITH CHECK olmali.
    pol=$(grep -E "^create policy .* on (only )?${tref} " <<<"$stmts" || true)
    if [[ -z $pol ]]; then
      missing_rls+="$f [$qual]: eksik → CREATE POLICY"$'\n'
    else
      # Politika ADI ve TABLO adi atilir: `..._tenant_id_iso` gibi bir ad
      # kapsam sutunu araniyormus gibi gorunup denetimi bosa dusurmesin.
      polbody=$(sed -E 's/^create policy [^ ]+ on (only )?([a-z0-9_"]+\.)?[a-z0-9_"]+ //' <<<"$pol")
      # KRITIK: `current_setting(…)` cagrilari atilir. Politikanin cagirmak
      # ZORUNDA oldugu GUC adi `app.tenant_id`, aranan `tenant_id` dizesini zaten
      # icerir; cikarilmazsa kural her gercek migration'da kendiliginden gecer,
      # yani OLU olur. Ayrica sutunun bir KARSILASTIRMANIN operandi olmasi
      # istenir — salt gecmesi yetmez.
      polprobe=$(sed -E "s/current_setting\([^)]*\)//g" <<<"$polbody" | tr -d '"')
      if ! grep -qE "(^|[^a-z0-9_])${col}(::[a-z0-9_]+)? ?(=|<>|!=|in |any )" <<<"$polprobe" \
         && ! grep -qE "(=|<>|!=|in|any) ?\(? ?${col}([^a-z0-9_]|$)" <<<"$polprobe"; then
        missing_rls+="$f [$qual]: politika kapsam sutununu ($col) bir karsilastirmada kullanmiyor"$'\n'
      fi
      # `using` / `with check` daima bir parantezli ifade acar; ciplak alt dize
      # aramak `v_using` gibi bir tablo adiyla yaniltilabilirdi.
      grep -qE '(^| )using ?\('      <<<"$polbody" || missing_rls+="$f [$qual]: eksik → politikada USING"$'\n'
      grep -qE '(^| )with check ?\(' <<<"$polbody" || missing_rls+="$f [$qual]: eksik → politikada WITH CHECK"$'\n'
    fi

    # + GRANT — tappa_app yazamiyorsa tablo uygulamaya gorunmez.
    grant_ok=0
    while IFS= read -r s; do
      [[ -n $s ]] || continue
      # Rol adi TAM eslesmeli: `tappa_apprentice` tappa_app degildir.
      roles=${s##* to }
      [[ " ${roles//[!a-z0-9_]/ } " == *" tappa_app "* ]] || continue
      mid=${s#* on }; mid=${mid%% to *}
      [[ $mid == *"all tables in schema"* ]] && grant_ok=1
      grep -qE "(^|[^a-z0-9_.])${tref}([^a-z0-9_]|$)" <<<"$mid" && grant_ok=1
    done < <(grep -E '^grant .* on .* to ' <<<"$stmts" || true)
    (( grant_ok )) || missing_rls+="$f [$qual]: eksik → tappa_app GRANT'i"$'\n'
  done
done
report FAIL R5 "Migration'da tablo besligi eksik (tenant_id + indeks + ENABLE/FORCE RLS + politika + GRANT)" "${missing_rls%$'\n'}"

report WARN R5 "Tenant kapsamindan MUAF birakilmis tablo(lar) — muafiyet sessiz kalmamali" "${waiver_report%$'\n'}"

report WARN R5 "Tablo yaratimi R5'in gorus alani disinda — elle denetle" "${unverified%$'\n'}"

# _test.go MUAFIYETI (DAR — yalniz bu tarama, yalniz test dosyalari): bu kural
# URETIM kodu icindir. RLS izolasyon testi (internal/db/rls_test.go) vaka 5'te
# tappa_owner (migrate URL) havuzunu MESRU kullanir — superuser'in bile append-only
# trigger'i asamadigini kanitlamak icin (ADR 0002). Uretim kodundaki migrate-URL
# kullanimi hala yakalanir. NOT: bu muafiyet R5'in yalniz bu (migrate-URL) taramasi
# icindir; migration-besligi (db/migrations) ve SET-LOCAL kontrolu etkilenmez.
report FAIL R5 "Uygulama migration rolu ile baglaniyor — RLS etkisiz kalir" \
  "$(scan -e 'DATABASE_MIGRATE_URL' --glob '!cmd/migrate/*' --glob '!internal/config/*' --glob '!**/*_test.go' || true)"

report WARN R5 "SET LOCAL degil duz SET — havuzdaki baglantiyi kirletir" \
  "$(scan -e "SET +app\.tenant_id" | grep -viE 'SET +LOCAL' || true)"

# --- R7: sir sizintisi -------------------------------------------------------
# `code[_a-z]*hash` ve `"code"[[:space:]]*,` (M5-02): davet AKISINDA hash bir
# TASIYICI (bearer) kimlik bilgisidir -- hash -> resolver -> tenant -> tuketim.
# Log'a dusen bir code_hash, kodu hic gormemis birine tam aktivasyon yetenegi
# verir; yani hash'i loglamak kodu loglamakla ayni agirliktadir (CLAUDE.md §4.7).
# M5-02 oncesi desen `invite_?code` disinda hicbirini yakalamiyordu (olculdu).
#
# DESEN DEGERI HEDEFLER, KELIMEYI DEGIL -- bilincli ve olculmus bir secim.
# Ilk deneme ciplak `[^a-z]code[^a-z]` idi; iki MASUM ve cok olasi satiri FAIL
# veriyordu (guvenlik denetcisi olctu): bir log MESAJINDA gecen "code expired" ve
# `fmt.Errorf("... invalid activation code: %w", err)`. Ikisi de sir sizdirmaz.
# Tehlike, agin kendisinin asinmasidir: CI mesru satirlarda kizarirsa baski
# deseni GEVSETMEYE doner ve gercek `code_hash` dali da gider (M0-07: "sessiz
# muafiyet bir kez yazilir, sonsuza dek denetimi susturur"nun tersi).
# Bu yuzden desen artik yalnizca DEGER tasiyan bicimleri arar:
#   * `code[_a-z]*hash` -> code_hash, codeHash, codehash (anahtar VEYA degisken adi)
#   * `"code"[[:space:]]*,` -> yapilandirilmis log ANAHTARI olarak "code",
# Serbest metindeki `code` kelimesi (mesaj, hata metni) artik tetiklemez (olculdu).
#
# `password` M6-01'de EKLENDI (guvenlik denetimi olctu): desen `PasswordHash`'i
# taramiyordu, yani panel parolasinin bcrypt digest'inin loglanmasi mekanik olarak
# YAKALANMIYORDU. Eklendikten sonra ayni SRC/GEN_EXCLUDE ile 0 eslesme ve 0 yanlis
# pozitif -- M5-09'un `test` eklemesiyle ayni sonuc.
#
# DURUSTCE: bu genisleme, denetcinin ResolvedAdmin.PasswordHash uzerinde OLCTUGU
# ALTI sizinti yolunun HICBIRINI yakalamaz -- `%+v`, dilim uzerinde `%v`, `%#v`,
# fmt.Errorf, unexported alan, slog. Altisinda da digest, ADI gecmeden, tasiyici
# yapinin icinden basiliyor; `password` KELIMESI cagri parantezleri icinde hic
# gecmiyor. Ag yine de eklendi cunku ucuz ve gurultusuz (0 yanlis pozitif) ve
# `slog.Info("login", "password_hash", h)` gibi APACIK bir satiri yakalar.
# GERCEK COZUM TIP DUZEYINDEDIR: internal/db/passwordhash.go, digest'i
# session.Token / invite.Code kalibiyla (Format+String+GoString+LogValue+
# MarshalText + isaretci dolaylamasi) sarmalar; kanit
# internal/db/resolve_leak_external_test.go, pozitif kontrolu dahil.
#
# SINIRLAR (hepsi yanlis-NEGATIF, ag mekaniktir, kanit degildir):
#   * baska adlandirma -- `"hash"`, `"c"` gibi bir ANAHTAR altinda gecen deger
#     yakalanmaz (degisken adi codeHash ise yine yakalanir);
#   * sir bir ara degiskene kopyalanip nötr bir adla loglanirsa yakalanmaz;
#   * sir bir YAPININ icinde tasiniyorsa (ResolvedAdmin, store.AdminUser) desen
#     yalnizca yapinin degisken adina bakar -- yukaridaki alti yol tam olarak budur.
#
# 🔴 M8-03 TUR 4: R7 §4.7'NIN EN SERT BES SINIFINI TASIYORDU VE KORDU -- AYNI
# COMMIT'IN R7b VE R7c'YE ODEDIGI BEDELI ONA ODEMEMISTI. Iki eksik vardi:
# `-U` YOKTU (kural SATIR YERELDI) ve pencere `[^)]*` idi (ilk `)` karakterinde
# duruyordu). Bir guvenlik denetcisi bes sinifin BESINI de tek yazimla gecirdi:
#
#   s.log.Log(ctx, decisionLevel(dec.Verdict), EventTapDecision,
#       ...
#       LogEmployeeID, req.EmployeeID.String(),
#       "cmac", req.CMAC,                          <-- YAKALANMADI
#
# Iki eksen ayri ayri olculdu: ayni satir + onunde parantez yok -> yakalaniyordu ·
# ayni satir + bir cagri parantezinden SONRA -> kaciyordu · devam satiri ->
# kaciyordu. Ve bu, `internal/domain/checkin/checkin.go`'daki tap kararinin
# BUGUNKU yazimidir; yani en sert bes sinif icin ag, urunun en hassas log
# cagrisinin uzerinde acikti.
#
# OLCEK -- VE YONTEMIYLE BIRLIKTE, CUNKU SAYININ KENDISI BIR TUR YANLIS YAZILDI.
# "Cok satirli" TANIMI: bir logger cagrisinin ACILIS parantezi ile KAPANIS
# parantezi farkli kaynak satirlarinda. Olcum, bu reponun kendi tarama kapsamiyla
# ayni AST yuruyusudur (`_test.go` · `*_templ.go` · `internal/store/` disarida;
# alici adinda "log" gecen `Info|Warn|Error|Debug|Log|*Context|LogAttrs` cagrilari):
# KART ONCESI agacta 346 cagri yerinin 137'si cok satirlidir, yani argumanlari
# R7'ye TANIMI GEREGI gorunmezdi. (Kart sonrasi agacta 349'un 141'i.)
# ⚠️ Bir tur burada 131 yazdi ve yontemini yazmadigi icin yeniden uretilemedi;
# denenen bes tanimin hicbiri 131 vermiyor (paren-araligi 137 · dugum-araligi 137 ·
# yalnizca internal/ 135 · alicisi log/slog/fmt olanlar 137 · ilk argumani alt
# satirda olanlar 0). 137 olculdu ve yontemi yukarida yaziyor.
#
# --- OLCULEN BEDEL: TEMIZ AGACTA 3 YANLIS POZITIF ---------------------------
# `-U` tek basina 2, `-U` + bir seviye dengeli parantez 3 bolge dondurur. Ucu de
# BIREBIR ADIYLA muaf edilir (asagidaki tablo), toplu bir `--glob !` ile degil, ve
# kullanilan her muafiyet HER KOSUDA WARN olarak basilir -- R1'in 2026-08-13'te
# ogrendigi kural: gorunmeyen muafiyet, olmayan denetimdir.
#
# ⚠️ MUAFIYET "BU SATIRI GORMEZDEN GEL" DEGIL. R1'in sondasindan ogrenilen sekil
# burada da gecerli: bir bolge ancak (1) yolu adiyla listelenmisse, (2) o girdinin
# ADI GECEN masum belirtecleri cikarildiginda (3) geriye HICBIR tetikleyici
# kalmiyorsa muaftir. Yani muaf dosyaya gercek bir `token`/`cmac` eklenirse bolge
# yine FAIL uretir.
#
# --- KALAN SINIR, ADIYLA ----------------------------------------------------
# Pencere BIR seviye dengeli parantez tasir (R7c ile ayni). `log.Info("x", f(g()),
# "cmac", v)` gibi TETIKLEYICIDEN ONCE IKI SEVIYE kapanan bir yazim hala gorunmez.
# Iki seviye denendi ve secilmedi: rg'nin regex motorunda her seviye pencereyi
# ustel olarak buyutuyor ve bu agacta yeni bir sey yakalamiyor (0 ek isabet).
R7_TRIGGERS='token|cmac|aes_?key|secret|invite_?code|code[_a-z]*hash|password|"code"[[:space:]]*,'
# Pencere: bir seviye dengeli parantez, tekrar edebilir -> `f(a(), b(), TETIK`.
R7_PATTERN="(slog|log|fmt)\\.[A-Za-z]+\\((?:[^()]|\\([^()]*\\))*($R7_TRIGGERS)"

# MUAFIYET TABLOSU -- her satir: <yol deseni>;;<masum belirtec>[;;<masum belirtec>...]
#
# W1 test/fixtures/seedkeys/main.go -- `aes_key_ref` bir SUTUN ADIDIR. Bolge bir
#    UPDATE ifadesini `fmt.Fprintf(&b, ...)` ile bir string BUFFER'ina yazar;
#    yazilan deger `hex.EncodeToString(ref)`, yani KEK ile sarmalanmis referans,
#    ham anahtar degil -- ve hedef bir log degil, seed SQL'i. Dosyanin kendi
#    yorumu bu bolgenin R7'den yalnizca SATIR KIRILIMI sayesinde kactigini zaten
#    yaziyordu; bu tur o kazayi kaldirdi, bu yuzden muafiyet ADIYLA yaziliyor.
# W2 internal/adminauth/password.go -- `ErrPasswordTooLong` bir SENTINEL HATA
#    degiskenidir. Cagri `fmt.Errorf("%w (got %d bytes)", ErrPasswordTooLong, n)`:
#    bicimde yalnizca sentinel ve bir UZUNLUK var; parola degeri kasitla `n`'e
#    hoisted edilmis (fonksiyonun kendi yorumu sebebini yaziyor). Deger hicbir
#    bicime girmiyor.
# W3 internal/domain/signup/signup.go -- `"password"` bir FORM ALANI ANAHTARI
#    (hata haritasinin anahtari, kullaniciya gosterilen alan adi) ve
#    `MinPasswordRunes` bir SABIT INT. Eslesen cagri
#    `fmt.Sprintf("Use at least %d characters...", MinPasswordRunes)` -- bicime
#    giren tek deger o sabittir; `a.Password` bu cagriya hic girmiyor.
#
# ⚠️ TEK SATIR, `##` ile ayrilmis: BSD awk (`-v`) deger icinde SATIR SONU KABUL
# ETMEZ ("newline in string" ile patlar ve tarama sessizce yarim kalir -- olculdu,
# bu tur, macOS). R1'in tablosu tek degerli oldugu icin bu sorunu hic gormemisti.
R7_WAIVERS='test/fixtures/seedkeys/main[.]go;;aes_key_ref##internal/adminauth/password[.]go;;ErrPasswordTooLong##internal/domain/signup/signup[.]go;;"password";;MinPasswordRunes'

r7_raw="$(scan -U -i -e "$R7_PATTERN" || true)"

# r7_select <waived|fail>
#
# rg -U bir eslesmenin HER FIZIKSEL SATIRINI ayri ayri `yol:no:metin` olarak
# basar, oysa muafiyet karari BOLGENIN tamamina bakmalidir (tetikleyici bir
# satirda, masum belirtec digerinde olabilir). awk bu yuzden ardisik satir
# numarali ayni-dosya satirlarini once BIR BOLGEDE birlestirir.
# ⚠️ Iki AYRI bolge tesadufen bitisikse birlesirler; bu HATA YONU GUVENLIDIR --
# birlesmis metinde muaf olmayan tetikleyici kalir ve bolgenin tamami FAIL olur.
r7_select() {
  awk -v mode="$1" -v waivers="$R7_WAIVERS" -v trig="$R7_TRIGGERS" '
    function flush(   i, m, rest, w) {
      if (nlines == 0) return
      w = 0
      for (i = 1; i <= wcount; i++) {
        if (path ~ wpath[i]) {
          rest = tolower(text)
          gsub(wtok[i], " ", rest)      # ADI GECEN masum belirtecleri cikar
          if (rest !~ trig) { w = 1; break }
        }
      }
      if ((mode == "waived") == w) for (i = 1; i <= nlines; i++) print lines[i]
      nlines = 0; text = ""
    }
    BEGIN {
      n = split(waivers, W, "##")
      for (i = 1; i <= n; i++) {
        if (W[i] == "") continue
        m = split(W[i], F, ";;")
        wcount++
        wpath[wcount] = F[1]
        for (j = 2; j <= m; j++)
          wtok[wcount] = wtok[wcount] (j == 2 ? "" : "|") tolower(F[j])
      }
      nlines = 0; text = ""
    }
    length($0) == 0 { next }
    {
      # `yol:no:` onekini ayir. En soldaki `:<sayi>:` yolun sonudur; metnin
      # icindeki `bar.go:12:` gibi bir yazim daha sagda kalir ve etkilemez.
      p = $0; sub(/:[0-9]+:.*$/, "", p)
      no = $0; sub(/^[^:]*:/, "", no); sub(/:.*$/, "", no)
      # 🔴 TETIKLEYICI TESTI YALNIZ KAYNAK METNE BAKAR, `yol:no:` onekine DEGIL.
      # Olculdu: onek dahil edilince `internal/adminauth/password.go` YOLUNUN
      # kendisi `password` tetikleyicisini eslestiriyor ve o dosyadaki hicbir
      # muafiyet asla gecerlilesemiyordu -- muafiyet sessizce olu kaliyordu.
      c = $0; sub(/^[^:]*:[0-9]+:/, "", c)
      if (nlines > 0 && (p != path || no + 0 != prevno + 1)) flush()
      path = p; prevno = no + 0
      lines[++nlines] = $0
      text = text " " c
    }
    END { flush() }' <<<"$r7_raw"
}

report FAIL R7 "Sir loglanıyor olabilir (token / cmac / anahtar / davet kodu / kod hash'i / parola hash'i)" \
  "$(r7_select fail)"
report WARN R7 "R7 muafiyeti kullanildi (yola VE belirtece sinirli) — muafiyet sessiz kalamaz" \
  "$(r7_select waived)"

# --- R7c: TAM GPS KOORDINATI LOG'A DUSUYOR OLABILIR --------------------------
#
# 🔴 NEDEN AYRI BIR KURAL VAR: §4.7'NIN ALTI SINIFINDAN BESI ICIN MEKANIZMA
# VARDI, GPS ICIN HICBIRI YOKTU (M8-03, 2026-08-19, olculdu). §4.7 "tam GPS
# koordinati" der; R7'nin deseni token/cmac/aes_key/secret/invite_code/
# code_hash/password sayar ve koordinatla ilgili TEK bir ifade tasimaz. Kanit
# bir mutasyondu: internal/handler/checkin.go'daki bir log cagrisina
# ("gps", gps) ve iki gercek ondalik derece eklendi -> bu script EXIT 0 verdi.
# Yani alti siniftan biri icin "dogrulandi" iddiasinin arkasinda hicbir sey
# yoktu.
#
# 🔴 ASIL MEKANIZMA BU DEGIL, TIP DUZEYIDIR ve o da bu turda kondu:
# geo.Point.LogValue ve tenant.GPS.LogValue koordinati slog uzerinden
# BASILAMAZ yapar (session.Token / invite.Code / db.PasswordHash kalibi).
# Bu desen, tipin ARKASINDAN dolasan tek olculmus yazimi kapatir: eksenleri
# struct'tan cikarip iki ciplak float64 olarak, eksen adli anahtarlarla
# loglamak. LogValue o yolda hic devreye girmez.
#
# --- OLCULEN: TEMIZ AGACTA 0 YANLIS POZITIF (uc desenin ucu de) --------------
# Denenen ve SECILMEYEN dorduncu desen: ciplak `\bgps\b`. Ayni agacta 2 yanlis
# pozitif veriyor (internal/domain/tenant/rulebook.go'da "gps radius %.0f m"
# bir HATA MESAJI, ve venue.go'daki redaksiyonun kendi metni). Gurultulu bir ag
# gevsetilir (M0-07), o yuzden dar ucu secildi ve genisligi burada yaziyor.
#
# --- OLCULEN: NEYI YAKALAMAZ (hepsi yanlis-NEGATIF) -------------------------
#   1. BASKA ANAHTAR ADI. `log.Info("x", "a", p.Lat)` -> "a" tetiklemez; ama
#      `.Lat` ucu bunu yine de yakalar. `log.Info("x","a",v)` (v onceden
#      kopyalanmis) -> gorunmez. R7'nin ara-degisken siniri, ayni sebep.
#   2. YAPI ICINDE TASINAN DEGER — LogValue'nun kapattigi yol tam olarak budur,
#      yani burada bosluk birakilmasi bilincli: iki ag ayni yolu iki kez
#      kapatmaz.
#   3. IC ICE IKI SEVIYE PARANTEZ. Pencere BIR seviye dengeli parantez tasiyor
#      (R7b'nin uzerindeki blok neden degistigini olcumleriyle anlatiyor), yani
#      `log.Info("x", f(g()), "lat", v)` gibi bir satirda tetikleyiciden once iki
#      seviye kapanirsa yine gorunmez. R7 ile ayni sinif, bir seviye ileride.
report FAIL R7c "Tam GPS koordinati log cagrisinda — §4.7: konum yalniz tap aninda okunur ve kaydedilir, loglanmaz" \
  "$(scan -U -i -e '(slog|log|fmt)\.[A-Za-z]+\((?:[^()]|\([^()]*\))*("(gps|lat|lng|latitude|longitude)"[[:space:]]*,|\b(gps_?lat|gps_?lng|latitude|longitude)\b|\.(Lat|Lng)\b)' || true)"

# --- R7b: KISISEL VERI (ad / adres) LOG'A DUSUYOR OLABILIR --------------------
#
# 🔴 NEDEN BU KURAL VAR. CLAUDE.md §4.7'nin "asla loglanmaz" listesi token/CMAC/
# anahtar/davet kodu/tam GPS sayar; §4.1 ve §7 ise bir KISININ adini ve adresini ayni
# muameleye tabi tutar -- surec log'unun saklama suresi yok, tenant siniri yok, ve
# GDPR silme (Q13) `employees` uzerinde bir UPDATE'tir, log'a hic ulasmaz. M6-13'te
# ayni sizinti UC KEZ farkli bir yoldan geri geldi (domain logger'i -> handler'in RET
# satirlari -> handler'in BASARI satiri) ve UCUNDE DE bu script EXIT 0 verdi: §4
# "make audit bu maddeleri mekanik olarak tarar" diyordu, bu madde icin taramiyordu.
#
# 🔴 BU KURALIN CUMLESI BIR KEZ ZATEN YANLISTI VE DUZELTILDI. Ilk hali "KAYNAGI okur,
# yani girdi uzayindan bagimsizdir -- EKSEN KAYMAZ" diyordu. Bir denetim onu TEK
# KOMUTLA curuttu: eksen kaybolmamis, girdi uzayindan SATIR DUZENI ve TANIMLAYICI
# ADLANDIRMASINA tasinmisti. Sekiz mutasyonun BESI exit 0 dondu. Bu yuzden asagidaki
# blok artik ne yakaladigini SAYAR, ve neyi yakalamadigini ADIYLA yazar.
#
# --- OLCULEN: NE YAKALAR (2026-08-17, temiz agacta 0 YANLIS POZITIF) --------------
#   * caller degeri log cagrisinin argumaninda:  r.PostFormValue( / r.FormValue(
#   * kisisel alan adiyla:                       .FullName / .Email / posted.
#   * kisinin ADI, DAR bir alici kumesiyle:      (f|person|out|p|emp|employee|staff|
#                                                 subject).Name
#   * ve bunlarin hepsi COK SATIRLI cagrilarda da (rg -U).
#
# --- OLCULEN: NEYI YAKALAMAZ (hepsi yanlis-NEGATIF; ag mekaniktir, KANIT DEGILDIR) --
#   1. ARA DEGISKEN. `qq := posted.FullName` sonra `log.Info("x","q",qq)` -- gorunmez.
#      Olculdu ve gosterildi; R7'nin kendi listesindeki ayni sinir, ayni sebep.
#   2. TANIMLAYICI BAGIMLILIGI, DARALTILDI AMA YOK EDILMEDI. Kapsam hala YEREL
#      DEGISKEN ADLARININ bir fonksiyonu: `row.Name`, `q.Name`, `x.Name` gecer.
#      ⚠️ CIPLAK `\.Name` OLCULDU VE SECILMEDI: temiz agacta 4 YANLIS POZITIF veriyor
#      (internal/domain/checkin/policyset.go'da `d.Name` bir POLITIKA BELGESININ adi,
#      kisisel veri degil). Gurultulu bir ag gevsetilir (M0-07), o yuzden dar kume
#      secildi ve genisligi burada yaziyor.
#   3. YAPI ICINDE TASINAN DEGER. `log.Info("x", "cmd", c)` -- desen yapinin ADINA
#      bakar, alanlarina degil.
#   4. TETIKLEYICIDEN ONCE KAPANAN PARANTEZ. `[^)]*` ic ice bir cagri kapandiginda
#      durur.
# Bu dort yol icin kalan ag: internal/handler'daki surulen-yol tablosu ve DENETIM.
#
# 🔴 COK SATIRLILIK BEYAN EDILMEMISTI VE KAPATILDI. Onceki hali satir tabanliydi ve
# limit blogu bunu SAYMIYORDU -- oysa ayni dosya R4 icin birebir "BU KURAL SATIR
# YERELDIR" diye yaziyor. Olculdu: `internal/handler` + `internal/domain` icinde
# COK SAYIDA log cagri yeri (sayma sekline gore ~130) zaten sonraki satira devam
# ediyor, ve bunlarin arasinda tarihsel axis-1'in TAM KENDISI vardi.
#
# ⚠️ BURAYA KESIN BIR SAYI YAZILMIYOR, BILINCLI OLARAK. Ilk hali "126" diyordu; bir
# denetim sekiz farkli sayma sekliyle 129-138 aldi. Sayi KARARI ETKILEMIYOR -- `rg -U`
# ile kapatmanin maliyeti sifir olculdu (temiz agacta 0 yanlis pozitif), yani kac tane
# oldugu degil SIFIRDAN COK oldugu onemliydi. Karari tasimayan bir sayiyi yazmak, onu
# bayatlatmaktan baska bir sey yapmaz. Ucuzdu, kapatildi.
#
# 🔴 TIP DUZEYI HALA DOGRU CEVAP VE HALA BU KARTIN ISI DEGIL. Degeri loglanamaz bir
# tipe sarmak (session.Token / invite.Code kalibi) ya da handler'a dar bir logger
# arayuzu vermek bu agin tamamini gereksiz kilar. Olculdu: internal/handler'da 224
# `a.log.*` cagri yeri / 22 dosya, internal/domain'de 46 daha -- paket capinda bir
# donusum. Backlog satiri; bu ag onun YERINE GECMEZ, once gelir.
# 🔴 EŞLEŞME PENCERESİ `[^)]*` DEĞİL, BİR SEVİYE DENGELİ PARANTEZ (M8-03, olculdu).
# NEDEN DEGISTI: eski pencere ilk `)` karakterinde duruyordu, ve bu agactaki log
# cagrilarinin cogunda ARGUMANLARDAN ONCE bir cagri kapaniyor —
# `id.EmployeeID()`, `int(id.State)`, `r.Context()`. Yani tetikleyici bu
# kapanistan SONRA geliyorsa kural onu HIC gormuyordu. Iki mutasyonla olculdu:
# ayni sizinti duz bir ilk argumanla YAKALANIYOR, `r.Context()`in arkasinda
# YAKALANMIYOR. Limit bloğunun 4. maddesi bunu zaten "yanlis-negatif" diye
# yaziyordu; ucuz oldugu olculdukten sonra yazmak yerine kapatildi.
# MALIYET OLCULDU: temiz agacta R7b icin 0, R7c icin 0 yanlis pozitif.
#
# 🔴 [GERI CEKILDI — bu blok bir tur boyunca "R7'YE UYGULANMADI" ve "R7 hala ilk
# kapanan parantezde duruyor" diyordu; IKISI DE ARTIK YANLIS.] R7 4. turda
# GENISLETILDI: yukaridaki R7_PATTERN dengeli parantez penceresini tasiyor ve
# `r7_raw` taramasi `-U` ile kosuyor. Bu blok o degisikligin ONCESINDE yazilmisti
# ve guncellenmedigi icin ayni dosya kendi koduyla celisik kaldi. Dogru hikaye
# tek: "AYNI GENISLETME" IKI PARCALIDIR — (a) dengeli parantez penceresi,
# (b) rg -U cok satirlilik — R7b/R7c'de (b) zaten vardi, R7'de ikisi de yoktu, ve
# 4. turda R7'ye IKISI BIRDEN verildi. Ayri ayri olculdu (2026-08-19, ayni
# SRC/GEN_EXCLUDE):
#     eski hali (dar pencere, -U yok) ..................... 0 yanlis pozitif
#     YALNIZ dengeli parantez penceresi ................... 1
#         internal/adminauth/password.go, ErrPasswordTooLong bolgesi (HATA ADI)
#     pencere + -U (SEVK EDILEN) .......................... 3
#         + test/fixtures/seedkeys/main.go, seed UPDATE'ini yazan fmt.Fprintf
#           bolgesi (aes_key_ref bir SUTUN ADI)
#         + internal/domain/signup/signup.go, MinPasswordRunes'u bicimleyen
#           errs.add("password", ...) bolgesi ("password" bir FORM ALANI adi)
# Yani ucuz yarisi 1'e, pahali yarisi 3'e mal oldu; ucu de R7_WAIVERS'ta ADIYLA
# muaf ve her kosuda WARN olarak basiliyor. Gurultulu bir ag gevsetilir (M0-07),
# ve R7'nin TETIKLEYICILERINI daraltmak bir GUVENLIK kuralinin anlamini
# degistirmektir — M8-04'un isi, bu kartin degil.
# ⚠️ SATIR NUMARASI YAZILMIYOR, SEMBOL YAZILIYOR: bu blogun onceki hali
# `seedkeys/main.go:132` diyordu ve o satir bugun bir YORUM; gercek bolge
# fmt.Fprintf cagrisinda. Satir atfi bu depoda defalarca bayatladi.
# SAYILMIS SINIR, KAPATILMADI: pencere BIR seviye dengeli parantez tasir, iki
# seviye tasimaz (olculdu: iki seviye bu agacta 0 ek isabet, ustel regex buyumesi).
#
# ⚠️ KAPSAM FARKI, SAYILMIS: bu uc kural `_test.go` dosyalarini DA tarar (SRC
# listesi onlari iceriyor, GEN_EXCLUDE yalnizca *_templ.go ve internal/store/*.go
# cikariyor). cmd/tappa'daki TestObservability_EveryLoggerCallSiteIsSpelledLog
# invaryanti ise `_test.go`'lari ATLAR. Yani bir test dosyasindaki
# `a.logger.Error(..., r.PostFormValue(...))` hem bu uc kuralin hem invaryantin
# kor noktasindadir. deploy/README.md §4.7 bolumu bunu sinir olarak yaziyor.
report FAIL R7b "Kisisel veri (ad/adres) log cagrisinda — §4.7/§7: surec log'unun saklama suresi ve tenant siniri yoktur" \
  "$(scan -U -e '(slog|log|fmt)\.[A-Za-z]+\((?:[^()]|\([^()]*\))*(r\.(Post)?FormValue\(|\.FullName\b|\.Email\b|\bposted\.|\b(f|person|out|p|emp|employee|staff|subject)\.Name\b)' || true)"

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

# 🔴 BIR TARAMA PATLADIYSA SONUC "TEMIZ" DE "IHLAL VAR" DA DEGILDIR. Isaretci
# dolduysa hicbir bulgu guvenilir degil -- gorulmeyen ihlal gorulmemis sayilamaz.
if [[ -s "$SCAN_ERR" ]]; then
  echo "${RED}✗${OFF} $(wc -l <"$SCAN_ERR" | tr -d ' ') tarama HATA verdi — sonuc guvenilir degil (bkz. yukarisi)." >&2
  exit 2
fi

if [[ $fail -eq 0 ]]; then
  echo "${GRN}✓${OFF} mekanik tarama temiz. ${DIM}(kanit degil — derin denetim icin tappa-security-auditor)${OFF}"
else
  echo "${RED}✗${OFF} kirmizi cizgi ihlali var. Duzeltmeden commit etme."
fi
exit $fail
