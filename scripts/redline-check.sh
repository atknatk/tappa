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
#
# `deploy` ve `.github` M8-04'te EKLENDI (guvenlik denetimi olctu, 2026-08-19).
# ONCESINDE HICBIR R KURALI ORALARA BAKMIYORDU ve bu ucu birden ispatlandi: sonda
# dosyalarina `cmac` / `aes_key` / `webauthn` / `watchPosition` konuldu, tarama UC
# YOLDA DA `exit 0` verdi. Bu iki dizin urun manifestlerini, operator script'lerini
# ve SIR SABLONLARINI (deploy/examples/secret.example.yaml) tasiyor — yani §4.7'nin
# konusu tam olarak orada duruyor. M8-02 kartinin C4 maddesi bunu "acik bulgu" diye
# birakmisti.
SRC=(cmd internal db web/templates web/static/js scripts test deploy .github)

# SRC_CODE = URUN KAYNAGI, dagitim yapilandirmasi HARIC. Tek bir kural bunu okur:
# "uygulama migration rolu ile baglaniyor". Sebep olculdu — SRC'ye deploy/.github
# eklenince o kural 15 isabet verdi ve ONBESI DE mesru: migration Job'unun kendisi
# (30-migrate-job.yaml), CI'nin goose adimi (ci.yml), sir SABLONLARININ tanimlamak
# ZORUNDA oldugu anahtar (secret.example.yaml, externalsecret.example.yaml), ve
# degiskenin uygulama pod'una BILEREK verilmedigini anlatan yorumlar (20-app.yaml).
#
# 🔴 VE BU BIR GEVSETME DEGIL, DEGISTIRME: o sinifin mekanik agi artik bir GREP
# DEGIL, ACILIS SONDASI. Iki parca, ve IKISININ ADI AYRI (4. turda duzeltildi —
# onceki hali her ikisini `roleRefusal`a yaziyordu):
#   `internal/db/pool.go`, `func readRole` ...... OLCEN taraf. `roleFactsQuery`i
#       kosar ve DORT olguyu okur: `rolsuper` · `rolbypassrls` · rolun bir RLS'li
#       tablonun SAHIBI olup olamayacagi (sahip FORCE'u geri alabilir) · ve
#       SUPERUSER/BYPASSRLS bir rolun UYESI olup olmadigi (bir SET ROLE uzagi).
#   `internal/db/pool.go`, `func roleRefusal` ... KARAR veren taraf. Yalnizca
#       reddeder; hicbir sey okumaz. Kapinin kendisi `New`'in icindedir, yani bir
#       *DB elde etmenin baska yolu yoktur.
# Bu ayrim asagidaki SINIF -> KAPI tablosunda da ayni sekilde yazilidir; ikisi
# birbirinden sapmamalidir.
#
# ⚠️ BU ATIF BIR KEZ ZATEN BAYATLADI VE 3. TURDA DUZELTILDI. Onceki hali
# "cmd/tappa/main.go rlsRoleRefusal" diyordu; o tanimlayici AYNI TURDA silindi
# (kapi main.go'nun run()'indan constructor'a tasindi, cunku AST tarayan test
# `_ = rlsRoleRefusal(...)` mutasyonunu yesil birakiyordu). Ve "iki olgu" da
# yanlisti — yukaridaki dortlu onun duzeltilmis halidir. Bir manifest'in owner
# DSN'ini uygulama pod'una vermesi, bir env degiskeninin YAZILISINDAN degil GERCEK
# ROLDEN yakalanir — grep'in hic ulasamadigi olcu.
#
# Toplu bir `--glob !deploy` SECILMEDI: diger sekiz kural (R1/R2/R3/R4/R6/R7/R7b/R7c)
# deploy ve .github uzerinde TAM olarak kosar, cunku §4.7'nin konusu (sir sablonlari)
# tam olarak orada.
SRC_CODE=(cmd internal db web/templates web/static/js scripts test)
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
scan() { scan_in SRC "$@"; }

# scan_in <dizi-adi> <rg argumanlari...> — scan()'in kapsami secilebilen hali.
# Yalnizca migration-rolu kurali SRC_CODE ile cagirir; gerekcesi SRC_CODE'un
# tanimindadir. Hata isaretcisi ve cikis-kodu okumasi ORTAK, yani dar kapsamli bir
# tarama patlarsa da sonuc "temiz" sayilmaz.
scan_in() {
  local arr=$1; shift
  local out rc
  eval "local -a paths=(\"\${${arr}[@]}\")"
  out=$(rg -n --no-heading "${GEN_EXCLUDE[@]}" "$@" "${paths[@]}" 2>&1)
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

# 🔴 DESEN IKI YONE DE BAKAR — ONCEDEN YALNIZ BIR YONE BAKIYORDU (M8-04, olculdu).
# Eski hali sayaci SOLDA arıyordu, yani operand sirasina duyarliydi:
#
#   if tag.LastCtr < ctr ....... 1 isabet, YAKALANDI
#   if ctr > tag.LastCtr ....... 0 isabet, SESSIZ
#
# Ikincisi, CLAUDE.md §4.4'un "oku-sonra-yaz replay acigidir" diye ADIYLA uyardigi
# yazimdir; yani ag, uyarinin en dogal Go cumlesini gormuyordu. Simdi sayac
# karsilastirmanin HANGI TARAFINDA olursa olsun eslesiyor.
#
# 🔴 VE `lastCtr` (camelCase YEREL DEGISKEN) DE EKLENDI — 2. turda olculdu: eski
# alternasyon `LastCtr|last_ctr` idi, yani `if ctr > t.lastCtr` yazimi SESSIZ
# geciyordu. Pozitif kontrol: o satiri iceren bir sonda dosyasi eski desenle
# rc=1 (isabet yok), yeni desenle YAKALANIYOR. BEDELI OLCULDU VE SIFIR: ucuncu
# alternatifle birlikte agactaki isabet sayisi 9'da KALDI (asagidaki liste), yani
# bu bir gevsetme degil, bedelsiz bir genisletme.
#
# --- WARN'DA BIRAKILDI, VE BU OLCULMUS BIR KARAR (FAIL'e YUKSELTILMEDI) -------
# ⚠️ ASAGIDAKI UC SAYIYI HICBIR KAPI KORUMUYOR: bir test onlari dogrulamiyor, ve
# bir sonraki dosya eklendiginde sessizce bayatlarlar. Bu yuzden TARIHLI yaziliyor
# ve iddia degil OLCUM olarak okunmalidir. (Ilk yazimlarinda ikisi zaten yanlisti:
# "7" ve "5" yaziyordu, gercek 9 ve 4 idi.)
#
# OLCULDU 2026-08-19, ayni SRC/GEN_EXCLUDE kumesinde:
#   yalniz sol taraf ......................... 5 isabet
#   iki taraf (SEVK EDILEN) .................. 9 isabet
#   iki taraf, `_test.go` ve `.md` haric ..... 4 isabet
#
# VE ISABETLERIN TAM DOKUMU (9'un 9'u; "hicbiri gercek bir karsilastirma degil"
# CUMLESI YANLISTI, geri alindi):
#   5 Go YORUMU ......... internal/sun/{advance.go, preview.go, params.go},
#                         internal/sun/preview_test.go (INGILIZCE bir yorum),
#                         internal/db/tagsinventory_test.go:268
#   1 JSON fikstur ACIKLAMASI ... test/fixtures/sun_vectors.json:54
#   1 Turkce BELGE metni ........ deploy/README.md:3082 (bir tane, iki degil)
#   1 t.Fatalf MESAJI ........... internal/db/tagsinventory_test.go:312
#   1 CANLI SQL YUKLEMI ......... internal/db/tagsinventory_test.go:288 —
#       `UPDATE tags SET last_ctr = 501 WHERE … AND last_ctr < 501`, yani bir
#       yorum degil, testin KENDI ifadesi. Ve bu isabet KURALIN ISTEDIGI SEYI
#       yapiyor: kosullu bir sayac guncellemesi. Dogru cumle "hicbiri gercek
#       degil" degil, "GO tarafinda tek bir gercek sayac karsilastirmasi yok"tur.
#
# FAIL yapmak icin bu satirlarin BIREBIR muaf edilmesi gerekirdi -- yani bir
# guvenlik kuralini, cogunlukla yalnizca kendisini ANLATAN metinler icin delmek.
# M0-07'nin kaydettigi sey tam olarak budur: gurultulu bir ag gevsetilir. Denenen
# dar varyant (`if` ile capalanmis) temiz agacta 0 isabet veriyor (olculdu, 2. tur)
# ama KEYFI -- `ok := ctr > last_ctr` yazimi kacar, yani kurali yazan kisi onu
# nasil asacagini da yazmis olur.
#
# ⚠️ VE ASIL SEBEP: WARN/FAIL BU KURALDA YANLIS KOLDUR. §4.4'un mekanik kapisi
# yukaridaki FAIL'dir (kosulsuz `UPDATE tags SET last_ctr`), gercek garantisi ise
# db/queries/tags.sql:33-44'un tek ifadedeki `prev.old_ctr < @ctr` yuklemi,
# 00013'un `tags_counter_monotonic` trigger'i ve 50 goroutine x 5 turluk yaris
# testidir -- ucu de bagimsiz olarak dogrulandi (2026-08-19). Bu satir bir
# HATIRLATMADIR; F8'in bulgusu "WARN yetersiz" degil, "desen bir yone kor" idi ve
# kapatilan o.
report WARN R4 "Go tarafinda sayac karsilastirmasi — atomikligi SQL'e birak" \
  "$(scan -e '(LastCtr|lastCtr|last_ctr) *(<|>|>=|<=)' -e '(<|>|>=|<=) *[A-Za-z0-9_.]*(LastCtr|lastCtr|last_ctr)' --glob '!*.sql' || true)"

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
rls_mutations=""

# `-- +goose Down` ve sonrasini atar: yalnizca Up denetlenir.
goose_up() {
  awk 'tolower($0) ~ /^[ \t]*--[ \t]*[+]goose[ \t]+down/ { exit } { print }' "$1"
}

# `-- +goose Down` ONCESINI atar: yalnizca Down denetlenir. R5b'nin bir kismi
# icin var (asagida, kapsam gerekcesiyle birlikte); tablo besligi hala YALNIZ
# Up'a bakar.
goose_down() {
  awk 'skip { print } tolower($0) ~ /^[ \t]*--[ \t]*[+]goose[ \t]+down/ { skip = 1 }' "$1"
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

# Bir migration'in Up bolumunu tek satirlik, kucuk harfli, NORMALIZE ifadelere
# ayirir. Boylece cok satirli CREATE POLICY / GRANT tek grep ile denetlenebilir.
# NOT: ifade ayirmak icin `tr` kullanilir, `sed s/;/;\n/` DEGIL — BSD sed
# (macOS) degistirme tarafinda \n'i yeni satir saymaz, harfi harfine 'n' yazar
# ve tum dosya tek satira cokerdi.
#
# ==========================================================================
# 🔴 NORMALIZASYON — 4. TURDA EKLENDI, VE DESEN EKLEMEK YERINE EKLENDI.
# ==========================================================================
# Uc turdur her yeni koruma bir sonraki turda SOZLUKSEL bir yazimla atlatildi.
# 4. turda dokuz atlatma olculdu ve DOKUZUNUN DA tek bir ortak sebebi vardi:
# desenler bir bosluk bekliyordu, PostgreSQL beklemiyordu. Olculdu (her biri
# gecici bir migration ile, `./scripts/redline-check.sh` cikis kodu):
#
#   CREATE POLICY zz ON transactions ... USING(true);          exit=0  (bosluklu: 1)
#   ALTER TABLE"transactions"NO FORCE ROW LEVEL SECURITY;      exit=0  (bosluklu: 1)
#   ALTER TABLE"transactions"DISABLE ROW LEVEL SECURITY;       exit=0
#   CREATE VIEW"zz"AS SELECT * FROM transactions;              exit=0
#   CREATE MATERIALIZED VIEW"zz"AS SELECT * FROM transactions; exit=0
#   ALTER ROLE"tappa_app"BYPASSRLS;                            exit=0  (bosluklu: 1)
#   GRANT ALL ON transactions TO"tappa_app";                   exit=0
#   GRANT tappa_owner TO"tappa_app";                           exit=0
#
# PostgreSQL bunlarin hepsini kabul eder; `ALTER TABLE"transactions"NO FORCE …`
# canli olarak kosuldu ve `relforcerowsecurity=false` verdi.
#
# 🔴 DESEN DESEN KAPATMAK YANLIS CEVAPTI: sekiz yazim icin sekiz desen yazmak,
# dokuzuncusunu 5. turda getirir. Sebep tek oldugu icin CARE de tek: ifade
# EslesTIRILMEDEN ONCE noktalama ISARETLERININ ETRAFI BOSLUKLA ACILIR. Boylece
# `USING(true)`, `TABLE"transactions"`, `TO"tappa_app"` HEPSI BIRDEN kapanir ve
# mevcut 21 desenin HICBIRI degismez.
#
# Uc adim, ve ucunun de gerekcesi ayri:
#   (1) `"` -> BOSLUK. Tanimlayici tirnagi bir AYIRICI'dir, bir karakter degil:
#       `TABLE"x"NO` uc token'dir. Silmek `tablexno` uretirdi (daha kotu),
#       bosluk `table x no` uretir. Yan etki: tref desenlerindeki `"?` artik hic
#       eslesmiyor ama zarari yok, cunku tirnak zaten kalmadi.
#   (2) `(` `)` `,` etrafina BOSLUK. `using(true)` -> `using ( true )`.
#   (3) NITELENMIS AD YENIDEN BIRLESTIRILIR (` . ` -> `.`). (1) `"public"."t"`yi
#       ` public . t ` yapardi ve `public.t` bekleyen her desen bozulurdu; bu
#       adim tirnakli ve tirnaksiz yazimi AYNI metne indirger.
#
# ⚠️ SAYILMIS LIMIT: bu bir SOZCUK duzeyi normalizasyonudur, bir ayristirici
# degil. Dize sabitlerinin ICI de normalize edilir (`'a,b'` -> `'a , b'`) —
# eslestirme icin zararsiz, ama bir dize sabitine dayanan bir muafiyet yazilirsa
# bunu bilmek gerekir. Ve normalizasyon SOZDIZIMSEL esanlamlilari (ALTER USER =
# ALTER ROLE gibi) kapatmaz; onlar hala desen desen yazilir.
lex_statements() {
  sql_lex \
    | tr '[:upper:]' '[:lower:]' \
    | tr '\n' ' ' \
    | tr ';' '\n' \
    | tr '\001' ';' \
    | tr '"' ' ' \
    | sed -E 's/([(),])/ \1 /g; s/[[:space:]]+/ /g; s/ ?\. ?/./g; s/^ //; s/ $//' \
    | grep -v '^$'
}
sql_statements() { goose_up "$1" | lex_statements; }
sql_statements_down() { goose_down "$1" | lex_statements; }

# --- R5b'nin iki YAPISAL ayristiricisi ---------------------------------------
#
# 🔴 NEDEN AYRISTIRICI, NEDEN ALT DIZE DEGIL. Iki muafiyet/istisna alt dize
# aramasiyla yazilmisti ve IKISI DE TAKLIT EDILDI (3. tur denetimi, 2026-08-19):
#
#   CREATE VIEW zz AS SELECT 'security_invoker = true' AS n, t.* FROM transactions t;
#     -> ifadenin METNINDE `security_invoker = true` geciyor, ama bu bir DIZE
#        SABITI; view DEFINER haklariyla okunuyor ve muafiyeti kazaniyordu.
#   CREATE POLICY zz_legal_documents_public_bait ON transactions ... USING (true);
#     -> muafiyet `*legal_documents_public*` alt dizesini ariyordu, yani adin
#        ICINE gomunce baska bir tabloya izin veren politika muaf oluyordu.
#
# Cozum ikisinde de ayni sekilde: karari veren seyi AYRISTIR (depolama parametre
# listesi · yuklem agaci), metinde arama.

# opt_clause <ifade> <giris> — bir ifadenin depolama-parametre listesini DENGELI
# parantezle cikarir. <giris> "with (" ya da "set (" olur.
#
# Tanimlayici ve dize ICERIKLERI notrlestirilir: bir view'in ADI bir depolama
# parametresi gibi yazilarak karari degistirememeli (olculdu — `CREATE VIEW
# "as with (security_invoker = true)" AS ...` yazimi ham metinde muaf gorunuyor).
# `with (` icin arama ` as `den ONCE kalan basa sinirlanir: depolama parametreleri
# sozdizimsel olarak yalniz orada durur.
opt_clause() {
  awk -v intro="$2" '
    {
      s = $0
      gsub(/"[^"]*"/, "\"x\"", s)
      gsub(/\047[^\047]*\047/, "\047x\047", s)
      if (intro == "with (") { i = index(s, " as "); if (i > 0) s = substr(s, 1, i - 1) }
      j = index(s, intro)
      if (j == 0) exit
      s = substr(s, j + length(intro))
      d = 1; out = ""
      for (k = 1; k <= length(s); k++) {
        c = substr(s, k, 1)
        if (c == "(") d++
        else if (c == ")") { d--; if (d == 0) break }
        out = out c
      }
      print out
    }' <<<"$1"
}

# invoker_on <parametre-listesi> — `security_invoker` CAGIRAN haklarina ayarlanmis
# mi. Postgres'in tam boolean sozlugu kabul edilir (true/on/1/yes ve CIPLAK ad,
# ki o da true demektir); `= false` ve `reset` bu suzgecten GECMEZ. Kontrol ile
# sunucunun uzerine hareket ettigi deger AYNI olmalidir — bu depo o sinifin
# bedelini uc kez odedi (M5-01/02/03).
invoker_on() {
  grep -qE -e '(^|[,[:space:]])security_invoker[[:space:]]*=[[:space:]]*(true|on|1|yes)([,[:space:]]|$)' \
           -e '(^|[,[:space:]])security_invoker[[:space:]]*(,|$)' <<<"$1"
}

# policy_tautology <ifade> — CREATE/ALTER POLICY'nin USING / WITH CHECK
# yuklemlerinden HER SATIRI eslestiren birini basar (yoksa hicbir sey basmaz).
#
# `(true)` alt dizesini aramak yetmiyordu; olculen dort yazim da tenant yuklemini
# yerine koyuyor ve tabloyu tum tenant'lara aciyordu:
#   USING (true) · USING ((true)) · USING (1=1) · USING (tenant_id = tenant_id)
# Yuklem DENGELI parantezle cikarilir, dis parantezler soyulur, ve bir totoloji
# ya `true`dur ya da UST SEVIYE bir `=`in iki yani BIREBIR ayni metindir.
# `<=`, `>=`, `!=`, `<>` ve parantez icindeki `=` (ornegin gercek politikalarin
# `current_setting('app.tenant_id', true)` cagrisi) ust seviye sayilmaz.
policy_tautology() {
  awk '
    function trim(x) { gsub(/^[ \t]+|[ \t]+$/, "", x); return x }
    function peel(x,   y, d, i, c, ok) {
      x = trim(x)
      while (length(x) > 1 && substr(x, 1, 1) == "(" && substr(x, length(x), 1) == ")") {
        y = substr(x, 2, length(x) - 2); d = 0; ok = 1
        for (i = 1; i <= length(y); i++) {
          c = substr(y, i, 1)
          if (c == "(") d++
          else if (c == ")") { d--; if (d < 0) { ok = 0; break } }
        }
        if (!ok || d != 0) break
        x = trim(y)
      }
      return x
    }
    function tautology(e,   i, c, prev, nxt, l, r, d) {
      e = peel(e)
      if (e == "true") return 1
      # BILESIK TOTOLOJI (4. tur): bir AYRIK BAGLACIN bir dali totolojiyse bagin
      # KENDISI totolojidir -- `USING (true OR tenant_id IS NULL)` tenant yuklemini
      # aynen `USING (true)` gibi yerine koyar ve olculdu: ikisi de exit=0 veriyordu.
      # Bu bir DESEN degil bir CEBIR kurali; UST SEVIYE ` or ` de bolunur, iki yan
      # ozyinelemeyle sinanir, yani `a or b or c` da kapsanir.
      d = 0
      for (i = 1; i <= length(e); i++) {
        c = substr(e, i, 1)
        if (c == "(") { d++; continue }
        if (c == ")") { d--; continue }
        if (d != 0) continue
        if (substr(e, i, 4) == " or ") {
          if (tautology(substr(e, 1, i - 1))) return 1
          return tautology(substr(e, i + 4))
        }
      }
      d = 0
      for (i = 1; i <= length(e); i++) {
        c = substr(e, i, 1)
        if (c == "(") { d++; continue }
        if (c == ")") { d--; continue }
        if (d != 0 || c != "=") continue
        prev = (i > 1) ? substr(e, i - 1, 1) : ""
        nxt = substr(e, i + 1, 1)
        if (prev == "<" || prev == ">" || prev == "!" || nxt == "=") continue
        l = peel(substr(e, 1, i - 1)); r = peel(substr(e, i + 1))
        if (l != "" && l == r) return 1
      }
      return 0
    }
    function balanced(s, start,   d, out, k, c) {
      d = 1; out = ""
      for (k = start; k <= length(s); k++) {
        c = substr(s, k, 1)
        if (c == "(") d++
        else if (c == ")") { d--; if (d == 0) return out }
        out = out c
      }
      return out
    }
    {
      s = $0
      split("using (,with check (", intro, ",")
      for (n = 1; n <= 2; n++) {
        pos = 1
        while ((j = index(substr(s, pos), intro[n])) > 0) {
          at = pos + j - 1 + length(intro[n])
          body = balanced(s, at)
          if (tautology(body)) print intro[n] body ")"
          pos = at
        }
      }
    }' <<<"$1"
}

# APPEND_ONLY — CLAUDE.md §4.3'un bagladigi `transactions` ve onunla ayni
# muameleyi goren bes tablo. Liste `00021`in TRUNCATE korumasindan BIREBIR alindi
# (ayni alti tablo, ayni gerekce); bir yedincisi eklenirse iki yer birden
# guncellenmelidir ve bu cumle o baglantiyi soyluyor.
APPEND_ONLY='(transactions|audit_log|transaction_reviews|policy_versions|billing_periods|legal_documents)'

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
      # `${cols# }` NORMALIZASYON YUZUNDEN VAR (4. tur): lex_statements artik
      # parantezin etrafini bosluklandiriyor, yani sutun listesi ` tenant_id , x`
      # diye basliyor. Olculdu: bu satir olmadan 13 GERCEK migration yanlis pozitif
      # veriyordu ("eksik → tenant_id ONDE olan indeks"). Tirnak yazimi da normalize
      # edildigi icin artik ayri bir kola gerek yok.
      while IFS= read -r s; do
        [[ -n $s ]] || continue
        after=${s#* on }; cols=${after#*\(}; cols=${cols# }
        [[ $cols == tenant_id* ]] && idx_ok=1
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
      # `current_setting *\(` — bosluk 4. TURDA EKLENDI. Normalizasyon parantezin
      # etrafini acar (`current_setting ( 'app.tenant_id' , true )`), yani bosluksuz
      # yazilan eski desen artik HIC eslesmiyordu ve bu kuralin kendisi OLU kalirdi:
      # cikarilmayan cagri aranan `tenant_id` dizesini zaten iceriyor.
      polprobe=$(sed -E "s/current_setting *\([^)]*\)//g" <<<"$polbody" | tr -d '"')
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

  # --- R5b: §4.5'i BOZAN migration ifadeleri (M8-04, 2026-08-19) --------------
  #
  # 🔴 NEDEN VAR: R5 YALNIZ TABLONUN DOGUMUNU DENETLIYORDU, OLUMUNU DEGIL.
  # Yukaridaki besli kontrol `create table` ile BASLAYAN ifadelerden tablo adi
  # cikarir; bir migration mevcut bir tablonun korumasini SOKERSE hicbir tablo
  # "yaratilmadigi" icin dongu hic donmez. Bir guvenlik denetcisi bunu uc
  # mutasyonla gosterdi ve UCU DE `exit 0` verdi (2026-08-19):
  #
  #   ALTER TABLE transactions NO FORCE RLS + DISABLE RLS + GRANT ALL ... -> exit 0
  #   DROP POLICY transactions_tenant_isolation ON transactions;          -> exit 0
  #   ALTER ROLE tappa_app BYPASSRLS;                                     -> exit 0
  #
  # 🔴 VE ILK HALI DORT DESENLE YAZILDI, ONBES SEKIL SESSIZ GECIYORDU (2. tur,
  # 2026-08-19). Ikinci bir denetim onlari saydi. En utandiricisi `ALTER USER`:
  # PostgreSQL'de `ALTER ROLE`un BIREBIR ESANLAMLISI, yani yukaridaki ucuncu
  # mutasyon tek kelime degistirilerek atlatiliyordu -- ve desen bunu tam da
  # "urunun TUM tenant izolasyonunu tek ifadeyle kapatir" diye anarken. Bu turda
  # kapatilanlar ve OLCULEN etkileri (hepsi `BEGIN … ROLLBACK` icinde, yerel
  # gelistirme veritabaninda, 2026-08-19):
  #
  #   ALTER USER … BYPASSRLS / SUPERUSER ....... `role` -> `(role|user)`
  #   GRANT tappa_owner TO tappa_app ........... ayni baglantida 0 -> 327 866 tenant
  #                                              (`SET ROLE tappa_owner` ile)
  #   GRANT tappa_resolver TO tappa_app ........ 0 -> 120 975 plaket / 103 422 tenant,
  #                                              ve 120 975 sarmalanmis AES anahtar
  #                                              referansi (§4.7)
  #   CREATE VIEW … AS SELECT * FROM transactions
  #                        ... AYNI baglantida: tablo 0 satir, view 392 197 satir
  #                            / 78 406 tenant. `WITH (security_invoker = true)`
  #                            ile ayni view 0 satir -- muafiyet OLCULDU, varsayilmadi
  #   CREATE/ALTER POLICY … USING (true) ....... 0 -> 392 197 satir / 78 406 tenant
  #
  # ⚠️ BU SAYILAR TARIHLIDIR VE BUYURLER: test suiti her kosumda satir yaziyor, yani
  # ayni sonda yarin daha buyuk bir sayi verir. Buradaki islev sayinin KENDISI degil,
  # "0 ile karsilastirildiginda ne kadar" oldugudur.
  #   ALTER DEFAULT PRIVILEGES … GRANT ......... sonraki her tabloya kalici yetki
  #   GRANT UPDATE|DELETE ON <append-only> ..... tek basina yetmez, ama ↓ ile
  #   DROP TRIGGER / DISABLE TRIGGER ........... birlikte UPDATE 1 / DELETE 1 (§4.3)
  #   ALTER TABLE … OWNER TO ................... sahip FORCE'u geri alabilir
  #
  # ⚠️ `rg -U` GEREKMEDI, VE BU OLCULDU. M8-03'te R7 tam bu yuzden kordu, ama
  # burada girdi `rg` degil `sql_statements`: cok satirli her ifade zaten TEK
  # satira indirgeniyor (yorumlar silinmis, kucuk harfe cevrilmis, bosluklar
  # daraltilmis). Uc mutasyonun ucu de cok satirli yazildiginda da yakalaniyor.
  #
  # ⚠️ KAPSAM `db/migrations/*.sql` ILE SINIRLI, VE BU BIR SECIM: scripts/db-init/
  # 01-roles.sql `CREATE ROLE tappa_resolver ... BYPASSRLS` iceriyor ve bu MESRU
  # (SECURITY DEFINER cozucunun rolu, ADR 0002). O dosyayi taramak tek bir gercek
  # yanlis pozitif uretirdi; migration'lar ise bu rolu hic yaratmaz.
  #
  # --- OLCULEN BEDEL: 21 MIGRATION, TEK MUAFIYET --------------------------------
  # Her desen temiz agacta tek tek sayildi. 3. TURDA EKLENENLER DE SAYILDI ve
  # tamami sifir yanlis pozitif verdi (2026-08-19; `./scripts/redline-check.sh`
  # temiz agacta exit 0):
  #   rol/kullanici/GRUP BYPASSRLS-SUPERUSER ... up=0 down=0
  #   rol UYELIGI GRANT'i (ON'suz grant) ....... up=0 down=0
  #   rol UYELIGI: ALTER GROUP … ADD USER ...... up=0 down=0   <-- 3. tur
  #   rol UYELIGI: CREATE ROLE … IN ROLE ....... up=0 down=0   <-- 3. tur
  #   ALTER DEFAULT PRIVILEGES … GRANT ......... up=0 down=0
  #   CREATE [MATERIALIZED] VIEW ............... up=0 down=0
  #   ALTER VIEW … security_invoker / OWNER .... up=0 down=0   <-- 3. tur
  #   CREATE RULE .............................. up=0 down=0   <-- 3. tur
  #   append-only tabloya UPDATE/DELETE ........ up=0 down=0
  #   GRANT … ON ALL TABLES IN SCHEMA .......... up=0 down=0   <-- 3. tur
  #   DROP TRIGGER (yalniz Up) ................. up=0 down=8 (hepsi mesru geri alma)
  #   DISABLE/ENABLE ALWAYS TRIGGER ............ up=0 down=0
  #   ALTER TABLE … OWNER TO ................... up=0 down=0
  #   RLS SOKME (disable / no force), Up+Down ... up=0 down=0   <-- kapsam 3. turda
  #   DROP POLICY, Up+Down ..................... up=0 down=0   <-- kapsam 3. turda
  #   TOTOLOJIK politika yuklemi ............... up=1 down=0  <-- TEK YANLIS POZITIF
  # O tek isabet `00020`in `legal_documents_public` politikasidir: o tablo bilerek
  # tenant kapsamsizdir (migration 00020 gerekcesini uzun uzun yaziyor). TAM
  # POLITIKA ADI + TAM TABLO ADI ile muaf edildi, desen gevsetilerek DEGIL -- ayni
  # tabloya YENI bir izin veren politika yazilirsa kural yine ateslenir ve bir karar
  # ister.
  #
  # --- NEDEN FARKLI KAPSAMLAR (Down'da yalniz `drop trigger` muaf) ------------
  # Bir Down bolumu `DROP TRIGGER`i MESRU olarak icerir: Up'ta yaratilan
  # tetikleyiciyi geri alir, ve bedel olculdu (Down'da 8 mesru isabet). `DROP
  # POLICY` ve RLS kapatma icin ayni gerekce ONCEDEN DE YAZILIYDI ama BEDELI HIC
  # OLCULMEMISTI: 107 Down ifadesinde ucunun de isabeti 0, cunku her tablo RLS'i
  # ACIK DOGAR (§6) ve Down onu tabloyu DROP ederek geri alir. Yani yalniz-Up
  # kisiti o iki desen icin uretimde kosacak bir ifadeyi BEDAVA gorunmez
  # kiliyordu. Digerlerinin MESRU BIR TERSI YOKTUR: hicbir Up BYPASSRLS vermiyor,
  # hicbir Up rol uyeligi vermiyor, bir Down'un GRANT ALL yazmasi REVOKE'un
  # tersidir. Rollback'te kosan bir ifade de uretimde kosar.
  #
  # ==========================================================================
  # 🔴 SAYILMIS LIMIT — BU BIR ANAHTAR KELIME TARAMASIDIR VE SQL'IN ESANLAMLI
  # UZAYINI ENUMERATE EDEMEZ.
  # YASAK, UYGULANABILIR HALIYLE (4. turda daraltildi): asagidaki KAPATILANLAR ve
  # KAPATILAMAYANLAR listelerinde bu kuralin KAPSAMI icin "tamamen / bitmis /
  # complete / exhaustive" yazilmaz. Yasak BIR KAPSAM IDDIASI hakkindadir, kelime
  # hakkinda degil: "bu grep'in kapsaminin tamamen disinda" gibi bir cumle
  # kapsamin DAR oldugunu soyler ve serbesttir. Onceki hali ("buraya … YAZILMAZ")
  # dosyanin tamamini kastediyor gibi okunuyordu ve dosyanin kendisi o kelimeyi
  # uc yerde kullaniyordu — yani uygulanamaz bir yasakti.
  # ==========================================================================
  #
  # ==========================================================================
  # 🔴 R5b NE OLDUGU — 4. TURDA DURUSTCE YAZILDI, CUNKU KURALIN DEGERI BUNA BAGLI.
  # ==========================================================================
  # R5b BIR UYARI SISTEMIDIR, BIR KAPI DEGIL. Ucuza, commit'ten once, bir
  # migration'in metnine bakarak ates eder. §4.5'in BAGLAYICI kapisi sinif sinif
  # asagidaki tabloda durur ve o kapilarin hepsi CALISMA ANINDA, gercek bir
  # Postgres'e karsi olcer. Bu ayrimin ampirik dayanagi var: 4. turun guvenlik
  # denetimi SEKIZ kacisi olctu ve YEDISINDE baglayici kapi kirmiziya dondu —
  # yalnizca `NO FORCE ROW LEVEL SECURITY` kapisiz cikti (o da bu turda yazildi,
  # internal/db/rlsforce_test.go). Yani R5b'nin bir yazimi kacirmasi §4.5'i
  # acmiyor; R5b'nin bir sinif icin "kapi" DIYE ANILMASI aciyordu.
  #
  # KAPATILANLAR (her biri pozitif kontrolle: gecici bir migration -> exit=1,
  # kaldirinca -> exit=0; ve 21 gercek migration'da 0 yanlis pozitif):
  #   RLS kapatma/NO FORCE · DROP POLICY · totolojik politika yuklemi (`(true)`,
  #   `((true))`, `1=1`, `x = x`, ve 4. turdan beri BILESIK olani: `true OR …`,
  #   `… OR 1=1`) · BYPASSRLS/SUPERUSER rol/kullanici/GRUP · rol uyeligi (GRANT,
  #   ALTER GROUP … ADD USER, CREATE ROLE … IN ROLE) · ALTER DEFAULT PRIVILEGES ·
  #   CREATE VIEW (WITH cumlesi AYRISTIRILARAK) · ALTER VIEW (security_invoker
  #   geri alma, OWNER TO) · CREATE RULE · append-only tabloya
  #   UPDATE/DELETE/TRUNCATE (istege bagli `TABLE`/`ONLY` oneki dahil) · GRANT …
  #   ON ALL TABLES IN SCHEMA · tetikleyici sokme/kapatma · tablo sahipligi ·
  #   GRANT ALL — VE her birinin BOSLUKSUZ/TIRNAKLI yazimi, cunku eslestirme artik
  #   NORMALIZE metin uzerinde yapiliyor (bkz. lex_statements).
  #
  # ⚠️ VE BU LISTE 3. TURDA FAZLA SEY IDDIA EDIYORDU. `(true)` "kapatildi" diye
  # yaziliydi; olculdu ki `USING(true)` — o dortlunun ILKI, yalnizca boslugu
  # alinmis — exit=0 veriyordu. Liste artik normalizasyona atif yapiyor; iddiayi
  # tasiyan sey bir cumle degil, o adim.
  #
  # KAPATILAMAYANLAR, ADIYLA (bu kural ne kadar genisletilirse genisletilsin):
  #   · dinamik SQL — `EXECUTE format('ALTER TABLE %I NO FORCE …', t)`; ifade
  #     calisma aninda kurulur, dosyada hicbir yerde yazmaz;
  #   · bir fonksiyon govdesinde saklanan DDL — `CREATE FUNCTION … $$ … $$` sonra
  #     `SELECT f()`; lexer `$$` govdesini KORUR ama icini denetlemez;
  #   · `\i` / `\ir` include ve `psql` degiskeni (`:tablo`) — icerik baska dosyada;
  #   · SECURITY DEFINER fonksiyon + GRANT EXECUTE. Bu SEKIL bilerek kapatilmadi:
  #     urunun KENDI cozucusu tam olarak budur (ADR 0002 md. 7) ve temiz agacta 10
  #     `security definer` ifadesi var, ONU DA hepsi mesru. Bir grep mesru cozucuyu
  #     kotu niyetli olandan ayirt edemez; ayirt eden sey kod incelemesidir;
  #   · 🔴 VE ONUN UZERINDEN DOLAYLI VIEW — 4. turda olculdu, hem burada hem
  #     internal/db/viewsecurity_test.go'nun KATALOG testinde sessiz:
  #       CREATE FUNCTION zzf() RETURNS TABLE(id uuid, tenant_id uuid)
  #         SECURITY DEFINER … SELECT id, tenant_id FROM transactions;
  #       CREATE VIEW zzv AS SELECT * FROM zzf();
  #     Ayni transaction'da, `SET LOCAL ROLE tappa_app` + yabanci app.tenant_id ile:
  #     dogrudan 0 satir, view'den 408 188 satir / 81 777 tenant. Ayirt edici nokta
  #     da olculdu: `RETURNS SETOF transactions` YAKALANIR (pg_depend tablo tipine
  #     baglanir), kacan yazim `RETURNS TABLE(...)`;
  #   · 🔴 `COPY … TO/FROM PROGRAM` — olculdu, exit=0. §4.5'i BOZMAZ (RLS yerinde
  #     kalir, `COPY` cagiran rolun haklariyla kosar) ama SONUCUNU bozar: owner
  #     rolüyle kosan bir migration'da `COPY transactions TO PROGRAM 'curl …'` her
  #     tenant'in satirini kutunun disina cikarir, ve `FROM PROGRAM` keyfi kabuk
  #     komutu kosar. Kapatilmadi cunku bu bir SIZDIRMA sinifidir, bir RLS-sokme
  #     sinifi degil; kapisi kod incelemesi ve migration'i kimin kosturdugudur;
  #   · BILESIK olmayan totolojiler — `NOT false`, `tenant_id IS NOT DISTINCT FROM
  #     tenant_id`, `coalesce(true, …)`. Ayristirici `true`, ust seviye `=` ve ust
  #     seviye ` or ` biliyor; SQL'in yuklem cebrini bilmiyor;
  #   · adlandirilmis dolar tirnagi (`$body$ … $body$`) — sql_lex yalnizca `$$` bilir;
  #   · SOZDIZIMSEL ESANLAMLILAR normalizasyonun KAPSAMINDA DEGIL. Normalizasyon
  #     noktalama duzeyindedir; `ALTER USER` = `ALTER ROLE`, `GROUP` = `ROLE` gibi
  #     esanlamlilar hala tek tek yazilmistir ve bir sonraki esanlamli yine sessiz
  #     gecer;
  #   · VE EN GENISI: MIGRATION DISINDA KOSAN HER IFADE. Bu kural yalnizca
  #     `db/migrations/*.sql` okur. Bir operator'un psql oturumu, bir bakim
  #     script'i, bir yonetilen-Postgres konsolu bu taramanin kapsaminda DEGILDIR
  #     — ve ilk uc madde gibi "egzotik" de degil, en olasi yol budur.
  #
  # ==========================================================================
  # 🔴 GERCEK KAPI SINIF SINIF — VE BIR SINIFIN KAPISI YOKSA ONU DA YAZIYORUZ.
  # ==========================================================================
  # Onceki hali "§4.5'in kapisi uc yerde durur: kod incelemesi ·
  # pg-restore-verify.sh · rls_test.go + acilis sondasi" diyordu. Bir denetim
  # olctu ki VIEW sinifi icin UCU DE TUTMUYOR, ve iki gerekce yanlisti:
  #
  #   ⚠️ `scripts/pg-restore-verify.sh` MIGRATION DONGUSUNDE HIC KOSMUYOR.
  #      `.github/workflows/ci.yml`: make up -> migrate -> seed -> check -> audit.
  #      Script'i cagiran tek yer operator'un geri yukleme adimidir (deploy/README.md,
  #      iki yer). Ustelik referans kumesi DUMP'IN KENDISIDIR (§3'un kendi yorumu
  #      "EXPECTED comes from the dump" diyor), yani kaynakta verilmis bir yetki
  #      dump'ta da bulunur ve script "eslesiyor" der. Ve §2'nin sayaclari
  #      `c.relkind='r'` -- bir VIEW hic sayilmaz.
  #   ⚠️ `internal/db/rls_test.go` VIEW SAYMIYOR. TestRLS_ReadIsolation_AllTables
  #      17 TABLO adini elle dolasir; hicbir test pg_views'e bakmiyordu.
  #   ⚠️ `db.New`'in acilis sondasi CAGIRAN ROLUN erisimini olcer; bir view'in
  #      SAHIBININ haklariyla okunmasini gormez.
  #
  # 🔴 VE 4. TURDA HARITANIN ILK SATIRI YANLIS OLCULDU. "RLS kapatma -> izolasyon
  # suiti. Gercek kapi budur" yaziyordu; guvenlik denetimi `NO FORCE ROW LEVEL
  # SECURITY`i sevk edilmis semaya uygulayip olctu:
  #     owner baglantisi, YABANCI bir app.tenant_id altinda: 404 335 satir / 80 961 tenant
  #     internal/db suiti: ok github.com/atknatk/tappa/internal/db 8.802s  <- TAMAMEN YESIL
  # Sebep yapisal: izolasyon suiti `tappa_app` ile kosar, `tappa_app` hicbir
  # tablonun SAHIBI degildir (ADR 0002 md. 1 bunu sart kosar) ve FORCE'un TEK isi
  # politikalari tablonun SAHIBINE de uygulamaktir. Sahibe hic baglanmayan bir
  # suit FORCE'u goremez — ve sahip varsayimsal bir rol degil: migration'lar,
  # `make seed` ve operatorun psql oturumu tappa_owner ile kosar.
  #
  # SINIF -> KAPI (her satir 4. turda BIR MUTASYONLA olculdu; "KIRMIZI" = kapi
  # gercekten dondu, komut ve sonuc yanlarinda):
  #   RLS DISABLE · politika silme/izin verme
  #       -> internal/db/rls_test.go izolasyon suiti (17 tablo, her vaka POZITIF
  #          KONTROLLU). OLCULDU:
  #            ALTER TABLE employee_invites DISABLE ROW LEVEL SECURITY
  #              -> KIRMIZI: "RLS FAILED: B's context read 1 of A's employee_invites rows"
  #            DROP POLICY employee_invites_tenant_isolation ON employee_invites
  #              -> KIRMIZI: fixture INSERT'i 42501 ile reddedildi (politikasiz tablo
  #                 tappa_app icin FAIL-CLOSED'dur; suit yine de kirmiziya doner)
  #   RLS NO FORCE
  #       -> 🔴 internal/db/rlsforce_test.go KATALOG TESTI (4. turda yazildi; ONCESINDE
  #          BU SINIFIN KAPISI YOKTU ve yukaridaki satir yanlislikla kapi gosteriyordu).
  #          17 tenant kapsamli tablonun HEPSI icin relrowsecurity + relforcerowsecurity
  #          okur. LISTEYI CANLI KATALOGDAN TURETIR
  #          (tenantScopedTablesFromCatalogue -- su anda tenant_id TASIYAN her tablo),
  #          db/migrations turetimini ise ANTI-VACUITY CAPRAZ KONTROLU olarak tutar ve
  #          ikisinin ESIT olmasini sart kosar.
  #          ⚠️ BU SATIR 5. TURDA AZ IDDIA EDIYORDU: "listeyi db/migrations'tan
  #          TURETIR" yaziliydi, oysa sevk edilen test tam tersini yapar. Kapi
  #          yazilandan GUCLU -- ve fark bosuna degil, asagidaki ADD COLUMN sinifini
  #          kapsayan sey tam olarak katalog turetimidir. OLCULDU (sevk edilmis semaya
  #          uygulanip geri alinarak):
  #            ALTER TABLE password_resets NO FORCE ROW LEVEL SECURITY  -> KIRMIZI, tabloyu adiyla soyler
  #            ALTER TABLE employee_invites DISABLE ROW LEVEL SECURITY  -> KIRMIZI, "DISABLED" der
  #   ALTER TABLE … ADD COLUMN tenant_id (TABLO SONRADAN KAPSAMA GIRIYOR)
  #       -> 🔴 internal/db/rlsforce_test.go'nun KATALOG turetimi +
  #          TestRLS_TheGateSeesATableScopedAfterItWasCreated (5. turda yazildi).
  #          ⚠️ BU KURAL O SINIFI GORMEZ, ve gormedigi yapisaldir: R5 tablo adlarini
  #          yalnizca `^create table` ile BASLAYAN ifadelerden cikarir (bkz. R5'in
  #          tetiklenme kosulu, yukarida), yani CREATE TABLE govdesinde tenant_id
  #          OLMAYAN bir tablo besligin denetimine hic girmez. cmd/tappa'nin
  #          insertscope_test.go'su da ayni govdeyi okur, izolasyon suiti ise 17 adi
  #          ELLE dolasir. OLCULDU (BEGIN … ROLLBACK icinde, tappa_owner ile):
  #            CREATE TABLE zz_late_scoped (id uuid);
  #            ALTER TABLE zz_late_scoped ADD COLUMN tenant_id uuid NOT NULL;
  #              -> katalog sorgusu tabloyu LISTELER ve
  #                 relrowsecurity=f / relforcerowsecurity=f okur; sevk edilen ret onu
  #                 ADIYLA soyler ("row level security is DISABLED")
  #              -> db/migrations turetimi tabloyu GORMEZ (kor yon; capraz kontrol
  #                 bunun icin var, ve test bu iki yonu ayri ayri iddia eder)
  #   BYPASSRLS/SUPERUSER rol · rol uyeligi
  #       -> internal/db'nin ACILIS SONDASI: readRole dort olguyu okur ve
  #          roleRefusal uretimde havuzu REDDEDER. OLCULDU:
  #            CREATE ROLE zz_probe_bypass NOLOGIN BYPASSRLS; GRANT zz_probe_bypass TO tappa_app
  #              -> KIRMIZI: TestNewRefusesAPrivilegedRoleInProduction'in "uygulama rolu
  #                 uretimde — yine acilir" satiri dustu (New havuzu REDDETTI), ve
  #                 TestReadRoleSeesTheOwnerAndTheApplicationRoleDifferently de dustu.
  #          ⚠️ BOOT-TIME'dir: surec basladiktan SONRA verilen bir uyelik yeniden olculmez.
  #   VIEW (security_invoker)
  #       -> 🔴 internal/db/viewsecurity_test.go KATALOG TESTI (3. turda yazildi):
  #          pg_class.reloptions okur -- ifade metnini DEGIL -- ve listeyi
  #          pg_rewrite/pg_depend ile SEMADAN turetir, elle yazmaz. Pozitif
  #          kontrolu kendi icinde (BEGIN … ROLLBACK), dize-sabiti taklidi dahil.
  #          ⚠️ VE SINIRI 4. TURDA OLCULDU: bir SECURITY DEFINER fonksiyon uzerinden
  #          kurulan view (`RETURNS TABLE(...)` yazimi) o katalog testinde de
  #          GORUNMUYOR — dogrudan 0 satir, view'den 408 188 satir / 81 777 tenant.
  #          O sinir dosyanin kendi basligina da yazildi.
  #   append-only UPDATE/DELETE/TRUNCATE · GRANT ALL · tetikleyici sokme
  #       -> TestRLS_AppCannotMutateTransactions · TestRLS_AppCannotMutatePolicyVersions
  #          · TestRLS_OwnerMutationsHitAppendOnlyTrigger. OLCULDU:
  #            DROP TRIGGER billing_periods_no_mutation ON billing_periods
  #              -> KIRMIZI: "owner UPDATE billing_periods: succeeded, want the
  #                 append-only trigger to block it"
  #            GRANT ALL PRIVILEGES ON policy_versions TO tappa_app
  #              -> KIRMIZI: bekledigi 42501 yerine tetikleyicinin 23001'i geldi, yani
  #                 "yetki reddi" iddiasi tutmadi. (Kritik ayrinti: assertPermissionDenied
  #                 HERHANGI bir hatayi degil 42501'i sart kosuyor; gevsek olsaydi bu
  #                 mutasyon YESIL gecerdi.)
  #   CREATE RULE
  #       -> internal/handler'in §5 satir sayaclari. OLCULDU (3. tur, gercek
  #          veritabaninda): kural `ON INSERT … DO INSTEAD NOTHING` olarak
  #          konunca o ailenin YEDI testinden ALTISI kirmiziya donuyor.
  #          Kirmiziya DONMEYEN tek test
  #          TestCheckinDB_Row3_NoSessionRedirectsAndWritesNOTHING'dir ve donmemeli:
  #          §5 satir 3 zaten "kayit YAZILMAZ" diyen tek satirdir. Ailenin geri
  #          kalani icin ornek:
  #          TestCheckinDB_Row7_NoEvidenceIsFlaggedAndRECORDED.
  #   ALTER TABLE … OWNER TO (UYGULAMA ROLUNE)
  #       -> internal/db'nin ACILIS SONDASI, ve bu 3. turda YANLIS yaziliydi
  #          ("hicbir test bir tablonun SAHIBINI olcmuyor"). readRole'un dorduncu
  #          olgusu tam olarak budur. OLCULDU:
  #            CREATE TABLE zz_probe_owned (tenant_id uuid NOT NULL);
  #            ALTER TABLE zz_probe_owned ENABLE ROW LEVEL SECURITY;
  #            ALTER TABLE zz_probe_owned OWNER TO tappa_app;
  #              -> KIRMIZI: New havuzu reddetti (owns_or_can_become_owner_of_an_rls_
  #                 table=true) ve TestReadRoleSeesTheOwnerAndTheApplicationRoleDifferently
  #                 "ADR 0002 madde 1 says it must not" ile dustu.
  #          ⚠️ SINIRI: sondanin olctugu sey BAGLANAN rolun sahipligidir. Bir tablonun
  #          sahibini UCUNCU bir role tasimak (ornegin `OWNER TO tappa_resolver`)
  #          tappa_app'in olgularini degistirmez ve sessiz gecer.
  #   ALTER DEFAULT PRIVILEGES
  #       -> 🔴 BUNUN MEKANIK BIR KAPISI YOK. Bu grep'ten baska hicbir sey onu
  #          gormuyor: R5'in tablo besligi yeni tablonun kendi GRANT'ini denetler
  #          ama varsayilan yetkinin SONRADAN ekledigini gormez, ve
  #          internal/db/rlsforce_test.go RLS bitlerini okur, yetkileri degil.
  #          Kalan sey kod incelemesi.
  #
  # Bu kural yukaridakilerin yerini almaz; onlardan once ucuza ates eder.
  # 🔴 UP VE DOWN, IKISI DE (3. tur, 2026-08-19 olculdu). Onceki hali bu ikisini
  # YALNIZ Up'ta ariyordu ve gerekce "bir Down'un Up'taki ENABLE/CREATE'i geri
  # almasi mesrudur" idi. Bedel sayildi: 107 Down ifadesinde `disable row level
  # security` 0, `no force row level security` 0, `drop policy` 0 isabet — cunku
  # her tablo RLS'i ACIK DOGAR (§6) ve onu geri alan sey tablonun kendisini
  # DROP etmektir, RLS'i kapatmak degil. Yalniz-Up kisiti bu iki desen icin
  # BEDAVA kaldirilabiliyordu; `drop trigger` icin kaldirilMADI (Down'da 8 mesru
  # isabet, asagida).
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: RLS SOKULUYOR → ${s:0:110}"$'\n'
  done < <(grep -E '^alter table .*(disable|no force) row level security' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: politika SILINIYOR → ${s:0:110}"$'\n'
  done < <(grep -E '^drop policy' <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  # IZIN VEREN POLITIKA: tenant yuklemini bir TOTOLOJIYLE degistirmek tabloyu tum
  # tenant'lara acar. Olculdu: 0 -> 392 197 satir (2026-08-19).
  #
  # 🔴 DESEN DEGIL AYRISTIRICI, VE MUAFIYET TAM AD (3. tur). Eski hali
  # `(using|with check) ?\( ?true ?\)` ariyordu ve UC yazim sessizce geciyordu --
  # `((true))`, `(1=1)`, `(tenant_id = tenant_id)` -- ustelik muafiyet
  # `*legal_documents_public*` ALT DIZESINE bakiyordu, yani o metni ADIN icine
  # gomen bir politika BASKA bir tabloda muaf oluyordu (olculdu: `CREATE POLICY
  # zz_legal_documents_public_bait ON transactions ... USING (true)` -> exit 0).
  # Simdi yuklem policy_tautology ile ayristiriliyor ve muafiyet politikanin TAM
  # ADI + TAM TABLOSU. Ayni tabloya YENI bir izin veren politika yazilirsa kural
  # yine ateslenir ve bir karar ister.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    taut=$(policy_tautology "$s")
    [[ -n $taut ]] || continue
    grep -qE '^create policy legal_documents_public on ("?public"?\.)?"?legal_documents"?[ (]' <<<"$s" && continue
    rls_mutations+="$f: IZIN VEREN politika (tenant yuklemi yerine totoloji: ${taut:0:40}) → ${s:0:110}"$'\n'
  done < <(grep -E '^(create|alter) policy ' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  # `nobypassrls` / `nosuperuser` GUVENLI yondur ve eslesMEMELIDIR: onlerindeki
  # `o` harfi `[^a-z]` sinifina takilmadigi icin desen onlari gormez (olculdu).
  # `user` 2. turda EKLENDI (`ALTER USER` = `ALTER ROLE`), `group` 3. turda:
  # `CREATE GROUP zzg SUPERUSER` iki tur boyunca sessiz geciyordu ve `GROUP` da
  # PostgreSQL'de ROLE'un esanlamlisidir. Down'da da FAIL: bir rollback uretimde
  # kosar ve bu ifadenin mesru tersi yok.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: rol RLS'i ATLAR HALE GETIRILIYOR → ${s:0:110}"$'\n'
  done < <(grep -E '^(alter|create) (role|user|group) .*[^a-z](bypassrls|superuser)' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  # ROL UYELIGI: `GRANT <rol> TO <rol>` -- ` on ` TASIMAYAN bir GRANT bir yetki
  # degil bir UYELIK verir, ve uyelik bir `SET ROLE` uzagindadir. Acilis sondasi
  # bunu calisma aninda da olcer (internal/db, InheritsPrivilege), ama bir
  # migration'in yazdigi uyelik sonda kosmadan ONCE de gorunmelidir.
  #
  # 🔴 UC YAZIM DAHA, 3. TURDA OLCULDU. Uyelik yalniz GRANT ile verilmiyor:
  #   ALTER GROUP tappa_owner ADD USER tappa_app;   -> ayni baglantida 0 -> 331 010 tenant
  #   ALTER GROUP tappa_resolver ADD USER tappa_app; -> 122 012 plaket / 104 307 tenant
  #                                                     + 122 012 sarmalanmis AES referansi (§4.7)
  #   CREATE ROLE zz IN ROLE tappa_owner;            -> ayni kapi, dogum aninda
  # Ucu de asagidaki iki ek desende. `DROP USER`/`DROP ROLE` yonu GUVENLIDIR ve
  # eslesmez.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: ROL UYELIGI veriliyor (SET ROLE bir adim uzakta) → ${s:0:110}"$'\n'
  done < <(grep -E -e '^alter (group|role|user) [a-z0-9_", ]+ add (user|role) ' \
             -e '^create (role|user|group) .* in (role|group) ' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  while IFS= read -r s; do
    [[ -n $s ]] || continue
    [[ $s == *" on "* ]] && continue
    rls_mutations+="$f: ROL UYELIGI veriliyor (SET ROLE bir adim uzakta) → ${s:0:110}"$'\n'
  done < <(grep -E '^grant [a-z0-9_",. ]+ to ' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  # ALTER DEFAULT PRIVILEGES: tek ifade, SONRAKI her tabloya kalici yetki. Bir
  # tablonun dogumunu denetleyen R5, dogumdan sonra otomatik eklenen GRANT'i
  # goremez.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: VARSAYILAN YETKI degistiriliyor (sonraki her tabloyu etkiler) → ${s:0:110}"$'\n'
  done < <(grep -E '^alter default privileges.* grant ' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  # VIEW: sahibinin haklariyla degerlendirilir (`security_invoker` varsayilan
  # OLARAK KAPALI), yani tablo sahibinin actigi bir view RLS'i TAMAMEN atlar.
  # Olculdu, ayni baglantida: tablo 0 satir, view 392 197 satir / 78 406 tenant;
  # `WITH (security_invoker = true)` yazilinca view de 0 satir. MATERIALIZED VIEW
  # muaf edilemez: security_invoker desteklemez.
  #
  # 🔴 MUAFIYET ARTIK IFADE METNINDE ARANMIYOR, `WITH ( … )` CUMLESI AYRISTIRILIYOR
  # (3. tur). Eski hali `security_invoker *= *(true|on)` diye tum ifadede ariyordu
  # ve bir DIZE SABITIYLE taklit ediliyordu (olculdu, `make audit` yesil):
  #   CREATE VIEW zz AS SELECT 'security_invoker = true' AS n, t.* FROM transactions t;
  # opt_clause depolama parametre listesini ` as `den onceki basta, dengeli
  # parantezle cikarir ve tanimlayici/dize ICERIKLERINI notrlestirir.
  #
  # ⚠️ VE BU KURAL YALNIZ MIGRATION'LARI GORUR. Bir view'i migration DISINDA
  # yaratmak (psql, operator script'i, elle) bu grep'in kapsaminin tamamen
  # disindadir; onun kapisi internal/db/viewsecurity_test.go'nun KATALOG testidir
  # (pg_class.reloptions okur, ifade metnini degil) -- sinif tablosu asagida.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    if [[ $s != *"materialized view"* ]] && invoker_on "$(opt_clause "$s" 'with (')"; then
      continue
    fi
    rls_mutations+="$f: VIEW acilıyor (sahibin haklariyla okunur, RLS atlanir) → ${s:0:110}"$'\n'
  done < <(grep -E '^create( or replace)?( temp| temporary| recursive)* (materialized )?view ' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  # VAR OLAN BIR VIEW'IN SONRADAN ACILMASI (3. turda olculdu). `CREATE VIEW … WITH
  # (security_invoker = true)` ile yaratilan bir view, tek bir ALTER ile DEFINER'a
  # geri cevrilebilir -- ve eski kural yalnizca CREATE'e bakiyordu:
  #   ALTER VIEW zz SET (security_invoker = false);
  #   ALTER VIEW zz RESET (security_invoker);        -- varsayilana, yani DEFINER'a
  #   ALTER VIEW zz OWNER TO tappa_owner;            -- definer'i degistirir
  # `SET (security_invoker = true)` bir ONARIMDIR ve eslesMEZ.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    if [[ $s == *"set ("* && $s != *"reset ("* ]] && invoker_on "$(opt_clause "$s" 'set (')"; then
      continue
    fi
    rls_mutations+="$f: VIEW sonradan DEFINER'a cevriliyor → ${s:0:110}"$'\n'
  done < <(grep -E '^alter( materialized)? view .*(security_invoker|owner to )' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  # KURAL (CREATE RULE): §4.6'yi TEK IFADEDE kapatir ve bir tetikleyici degildir,
  # yani tetikleyici kurallarinin hicbiri gormuyordu (3. turda olculdu):
  #   CREATE RULE zz AS ON INSERT TO transactions DO INSTEAD NOTHING;
  # -> INSERT `INSERT 0 0` doner: HATA YOK, SATIR YOK. Yani her tap sessizce
  # kaybolur ve cagiran basarili sanir. §4.6'nin mekanik kapisi bu grep degil,
  # CI'da kosan §5 satir sayaclaridir (internal/handler, ornek:
  # TestCheckinDB_Row7_NoEvidenceIsFlaggedAndRECORDED); dogrulandi (3. tur, gercek
  # veritabaninda): kural konunca o ailenin yedi testinden altisi KIRMIZIYA doner.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: KURAL (RULE) yaratiliyor — DML'i sessizce yeniden yazabilir (§4.6/§4.3) → ${s:0:110}"$'\n'
  done < <(grep -E '^create( or replace)? rule ' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  # APPEND-ONLY TABLOYA UPDATE/DELETE/TRUNCATE GRANT'i (§4.3). `select, insert`
  # MESRU ve eslesMEZ; suzgec yetki listesinde update/delete/truncate arar.
  #
  # 🔴 IKI YAZIM 3. TURDA OLCULDU VE IKISI DE SESSIZ GECIYORDU:
  #   GRANT UPDATE ON TABLE transactions TO tappa_app;   -- `TABLE` istege baglidir
  #   GRANT UPDATE ON ALL TABLES IN SCHEMA public TO tappa_app;  -- ALTI tabloyu birden
  # Ilki icin desen artik istege bagli `table `/`only ` onekini yutuyor; ikincisi
  # ayri bir kural, cunku hicbir tablo adi tasimiyor.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: append-only tabloya UPDATE/DELETE yetkisi (§4.3) → ${s:0:110}"$'\n'
  done < <(grep -E "^grant [a-z_0-9, ()]*(update|delete|truncate)[a-z_0-9, ()]* on (table |only )*[^ ]*($APPEND_ONLY)" \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: SEMA CAPINDA UPDATE/DELETE yetkisi — append-only tablolarin ALTISI da dahil (§4.3) → ${s:0:110}"$'\n'
  done < <(grep -E "^grant [a-z_0-9, ()]*(update|delete|truncate)[a-z_0-9, ()]* on all tables in schema " \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  # TETIKLEYICI SOKME (§4.3'un mekanik kapisi). `drop trigger` YALNIZ Up'ta FAIL:
  # bir Down'un Up'ta yarattigi tetikleyiciyi silmesi mesrudur ve olculdu (8 isabet,
  # hepsi mesru). `disable trigger` ve `enable always/replica` ikisinde de FAIL --
  # bunlarin mesru bir tersi yok.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: TETIKLEYICI siliniyor (§4.3'un kapisi) → ${s:0:110}"$'\n'
  done < <(grep -E '^drop trigger' <<<"$stmts" || true)

  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: TETIKLEYICI kapatiliyor (§4.3'un kapisi) → ${s:0:110}"$'\n'
  done < <(grep -E '^alter table .* (disable|enable always|enable replica) trigger' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  # SAHIPLIK: bir tablonun sahibi FORCE ROW LEVEL SECURITY'yi GERI ALABILIR. FORCE
  # DML'i korur, sahibin FORCE'u KALDIRMA hakkini kaldirmaz -- olculdu.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: TABLO SAHIPLIGI degistiriliyor (sahip FORCE'u geri alabilir) → ${s:0:110}"$'\n'
  done < <(grep -E '^alter table .* owner to ' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)

  # GRANT ALL, tappa_app'e `transactions` uzerinde UPDATE/DELETE de verir (§4.3).
  # Rol adi TAM eslesmeli — yukaridaki GRANT kontrolunun ayni suzgeci.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    roles=${s##* to }
    [[ " ${roles//[!a-z0-9_]/ } " == *" tappa_app "* ]] || continue
    rls_mutations+="$f: uygulama roluna GRANT ALL → ${s:0:110}"$'\n'
  done < <(grep -E '^grant all( privileges)? .* to ' \
             <<<"$stmts"$'\n'"$(sql_statements_down "$f")" || true)
done
report FAIL R5 "Migration'da tablo besligi eksik (tenant_id + indeks + ENABLE/FORCE RLS + politika + GRANT)" "${missing_rls%$'\n'}"

report FAIL R5b "Migration §4.5/§4.3/§4.6'yi SOKUYOR (RLS kapatma · politika silme veya totolojik yuklem · BYPASSRLS-SUPERUSER rol/grup · rol uyeligi · varsayilan yetki · view acma veya definer'a cevirme · KURAL (RULE) · tablo sahipligi · tetikleyici · append-only UPDATE/DELETE · sema capinda UPDATE/DELETE · GRANT ALL)" "${rls_mutations%$'\n'}"

report WARN R5 "Tenant kapsamindan MUAF birakilmis tablo(lar) — muafiyet sessiz kalmamali" "${waiver_report%$'\n'}"

report WARN R5 "Tablo yaratimi R5'in gorus alani disinda — elle denetle" "${unverified%$'\n'}"

# _test.go MUAFIYETI (DAR — yalniz bu tarama, yalniz test dosyalari): bu kural
# URETIM kodu icindir. RLS izolasyon testi (internal/db/rls_test.go) vaka 5'te
# tappa_owner (migrate URL) havuzunu MESRU kullanir — superuser'in bile append-only
# trigger'i asamadigini kanitlamak icin (ADR 0002). Uretim kodundaki migrate-URL
# kullanimi hala yakalanir. NOT: bu muafiyet R5'in yalniz bu (migrate-URL) taramasi
# icindir; migration-besligi (db/migrations) ve SET-LOCAL kontrolu etkilenmez.
report FAIL R5 "Uygulama migration rolu ile baglaniyor — RLS etkisiz kalir" \
  "$(scan_in SRC_CODE -e 'DATABASE_MIGRATE_URL' --glob '!cmd/migrate/*' --glob '!internal/config/*' --glob '!**/*_test.go' || true)"

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
#
# W4 deploy/README.md -- M8-04'te SRC'ye `deploy` eklenince ortaya cikan IKI satir,
#    ve ikisi de W2/W3'un KENDISINI ANLATAN belge satirlari: README'nin R7 muafiyet
#    TABLOSU, muaf edilen Go cagrilarini ALINTILIYOR. Yani tetikleyen metin, bu
#    script'in kendi muafiyetlerinin gerekcesi. Masum belirtecler tek tek yazildi
#    (`password.go` bir DOSYA ADI, `ErrPasswordTooLong` bir SENTINEL,
#    `MinPasswordRunes` bir SABIT) -- dosyaya GERCEK bir `token`/`cmac`/`aes_key`
#    girerse bolge yine FAIL uretir, cunku muafiyet dosyayi degil bu belirtecleri
#    affeder.
#
#    🔴 W4 DARALTILDI (2. tur, 2026-08-19) -- ILK HALI `password` TETIKLEYICISINI
#    BU DOSYA ICIN TAMAMEN ETKISIZ KILIYORDU. Belirtec listesinde CIPLAK
#    `"password"` ve `a[.]password` vardi; ikisi de gercek bir sizinti satirinin
#    icinde de gecer. Olculdu, o hallerinde:
#      log.Info("admin sign-in", "password", pw)             -> exit=0, SESSIZCE MUAF
#      slog.Error("login failed", "a.password", a.Password)  -> exit=0, SESSIZCE MUAF
#    Simdi iki belirtec de MARKDOWN KOD ISARETLERIYLE BIRLIKTE yaziliyor, cunku
#    README onlari her zaman kod araligi olarak ALINTILIYOR; bir log cagrisinda
#    ayni metin isaretsiz gecer ve muaf olmaz. Ayni iki sonda simdi FAIL veriyor
#    (pozitif kontrol asagida degil, gorev raporunda -- burada yalnizca sebep).
#    ⚠️ Bes adlandirilmis §4.7 sirri (token · cmac · aes_key · davet kodu · GPS)
#    daraltmadan ONCE de FAIL veriyordu; delik yalnizca `password` icindi.
#
#    🔴 VE 2. TURUN DARALTMASI YARIMDI — KALAN IKI CIPLAK BELIRTEC 3. TURDA OLCULDU:
#      slog.Info("admin sign-in", "password.go", pw)   -> exit=0, SESSIZCE MUAF
#      fmt.Printf("minpasswordrunes=%s", raw)          -> exit=0, SESSIZCE MUAF
#    Sebep ayni: `password[.]go` ve `minpasswordrunes` CIPLAK yaziliydi. Ikisi de
#    artik README'nin GERCEKTEN kullandigi IKI bicimle sinirli — tek basina kod
#    araligi (`…`) ve daha uzun bir kod araliginin ICINDE, cagri sonundaki tam
#    metinle. `errpasswordtoolong` da ayni sekilde daraltildi (o da ciplakti).
#    ⚠️ NEDEN IKI BICIM: README bu adlari hem "belirtec" sutununda TEK BASINA
#    alintiliyor hem de muaf edilen cagriyi (`fmt.Errorf(…, ErrPasswordTooLong, n)`,
#    `fmt.Sprintf(…, MinPasswordRunes)`) bir butun olarak alintiliyor; ikinci
#    bicimde ad kendi kod isaretlerini TASIMAZ. Tek bicim yazmak mesru satiri
#    kizartirdi (olculdu), her ikisini ciplak birakmak ise deligi acik tutuyordu.
#
#    ==========================================================================
#    🔴 4. TUR: DARALTMA BURADA DURUYOR. KALAN, SAYILMIS LIMIT OLARAK YAZILDI.
#    ==========================================================================
#    Uc turdur ayni sey oluyor: bir belirtec daraltiliyor, bir sonraki tur onun
#    yeni yazimini buluyor. M5-06'nin kurali (agent-brief.md, "Yeni kanal
#    KAPATILMAZ, sayilir") bu noktada devreye girer. Dorduncu kez daraltmak yerine
#    KALAN OLCULDU — on sonda, her biri deploy/README.md'nin SONUNA eklenip
#    `./scripts/redline-check.sh` cikis kodu okunarak (mutasyon her seferinde geri
#    alindi, `cmp` ile dogrulandi):
#
#      log.Info("admin sign-in", "password", pw) ................. exit=1
#      slog.Error("login failed", "a.password", pw) .............. exit=1
#      slog.Info("admin sign-in", "password.go", pw) ............. exit=1
#      fmt.Printf("minpasswordrunes=%s", pw) ..................... exit=1
#      slog.Info("x", "errpasswordtoolong", pw) .................. exit=1
#      log.Info("admin sign-in", `"password"`, pw) ............... exit=0  <- MUAF
#      slog.Info("cfg", `internal/adminauth/password.go`, pw) .... exit=0  <- MUAF
#      slog.Info("cfg", `errpasswordtoolong`, pw) ................ exit=0  <- MUAF
#      slog.Info("cfg", `minpasswordrunes`, pw) .................. exit=0  <- MUAF
#      slog.Info("cfg", `a.password`, pw) ........................ exit=0  <- MUAF
#
#    🔴 SEBEP DARALTMAYLA COZULEMEZ, VE BU KALAN LIMITIN NIYE LIMIT OLDUGUDUR:
#    markdown KOD ISARETI ile Go'nun HAM DIZE sinirlayicisi AYNI KARAKTERDIR (`).
#    Yani "README bunu bir kod araligi olarak alintiliyor" ile "bir Go satiri bunu
#    ham dize olarak yaziyor" BIREBIR AYNI BAYTLARDIR. Ikisini ayirt edebilecek tek
#    sey bir Markdown/Go AYRISTIRICISIDIR; bir grep degil. Belirteci daha da
#    daraltmak (ornegin cevresine tablo boru isareti sartı koymak) bu sefer
#    README'nin gercek satirini kizartirdi — olculdu, 3. turda ayni sekilde oldu.
#
#    KALAN DELIGIN SINIRLARI, OLCUYLE:
#      · YALNIZCA `deploy/README.md`. Ayni on satir `internal/handler/` altindaki
#        bir Go dosyasina konuldugunda ONUNUN DE cikis kodu 1 — muafiyet dosyaya
#        bagli, yani uretim kodunda boyle bir satir hicbir yazimla muaf olamaz.
#      · YALNIZCA `password` TETIKLEYICISI. §4.7'nin bes ADLANDIRILMIS sirri ayni
#        muaf belirteci TASIYAN bir satirda bile FAIL veriyor (olculdu, hepsi exit=1):
#          log.Info("t", `"password"`, sessionToken) ... token   -> exit=1
#          log.Info("t", `"password"`, cmac) ........... cmac    -> exit=1
#          log.Info("t", `"password"`, aes_key) ........ aes_key -> exit=1
#          log.Info("t", `"password"`, inviteCode) ..... davet   -> exit=1
#          log.Info("t", `"password"`, gpsLat, gpsLng) . GPS     -> exit=1
#      · Yani affedilen sey: "deploy/README.md icinde, `password` tetikleyicisiyle,
#        muaf belirtecin kod-isaretli yazimini tasiyan bir bolge". Affedilmeyen sey:
#        ayni dosyadaki bes adlandirilmis sir, ve BASKA HER DOSYA.
#      · Bu bir belge dosyasidir; oradaki bir "sizinti" calisan bir log cagrisi
#        degil, yazilmis bir orneKtir. Delik gercek ama uretim kodunda kosmuyor —
#        ve bunu yazmak, kapatildigini iddia etmekten guvenlidir.
R7_WAIVERS='test/fixtures/seedkeys/main[.]go;;aes_key_ref##internal/adminauth/password[.]go;;ErrPasswordTooLong##internal/domain/signup/signup[.]go;;"password";;MinPasswordRunes##deploy/README[.]md;;`errpasswordtoolong`;;errpasswordtoolong, n\);;`internal/adminauth/password[.]go`;;`minpasswordrunes`;;minpasswordrunes\);;`a[.]password`;;`"password"`'

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
