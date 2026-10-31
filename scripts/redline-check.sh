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
# ⚠️ TEK ISTISNA R7d (sir DEGERI, M10 F0-5): o kural SRC'yi DEGIL commit'lenebilir
# HER dosyayi tarar, cunku Olay A-0'in sizintisi tam olarak SRC'nin disinda,
# docs/plan/state.md'deydi. Kapsami ve gerekcesi R7d bolumunde.
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
# 🔴 SABLON `XXXXXX` ILE VE TAM YOLLA (2026-09-25, M10 F0-5 2. turu olctu). Onceki
# `mktemp -t tappa-redline-scan-err` yalniz BSD mktemp'te (macOS) calisiyordu; GNU
# mktemp (Ubuntu CI) X'siz sablonu "too few X's" ile REDDEDER. Olculdu, ubuntu:24.04:
# SCAN_ERR BOS kaliyor, her `echo ... >>"$SCAN_ERR"` "No such file" ile dusuyor ve
# `[[ -s "" ]]` yanlis — yani CI'da yukaridaki "tarama patladi -> exit 2" isaretcisi
# bu satirin yazildigi gunden beri OLUYDU. Artik yaratilamazsa betik baslamaz.
SCAN_ERR=$(mktemp "${TMPDIR:-/tmp}/tappa-redline-scan-err.XXXXXX") || { echo "redline-check: mktemp failed" >&2; exit 2; }
R7D_LIST=$(mktemp "${TMPDIR:-/tmp}/tappa-redline-r7d-files.XXXXXX") || { rm -f "$SCAN_ERR"; echo "redline-check: mktemp failed" >&2; exit 2; }
# R7D_WORKDIR: R7d siniflandiricisinin (secretscan.sh r7d_select) calisma dizini; satir
# metni tasir, SAHIBI bu ana kabuktur (F0-5 CI): kesilen bir tarama onu birakmasin.
# secretscan.sh YUKLENDIKTEN SONRA yaratilir (R7d blogu) — yukleme miras kalan bir
# degeri siler (denetim B2). Tuzaklar sinyali, bash'in on plandaki `$(...)` bittikten
# sonra kosturdugu bir `exit`e cevirir; QUIT dahil (Ctrl-\; denetim B3: tuzaksiz macOS
# bash 3.2 QUIT'le olup EXIT tuzagini kosturmuyor, ubuntu bash 5.2 QUIT'i yok sayiyor —
# olculdu). Pin: cmd/tappa/scripttest
# TestRedline_AnInterruptedScanLeavesNothing.
R7D_WORKDIR=""
trap 'rm -f "$SCAN_ERR" "$R7D_LIST"; [[ -z ${R7D_WORKDIR:-} ]] || rm -rf "$R7D_WORKDIR"' EXIT
trap 'exit 130' INT
trap 'exit 129' HUP
trap 'exit 131' QUIT
trap 'exit 143' TERM
trap 'exit 141' PIPE
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
# 🔴 BIR IFADE DEGIL BES IFADE (2026-09-15, ADR 0012 ek bolumu). Kullanicinin kendi
# cizdigi landing (docs/design/landing-reference-2026-09-12.html) karsilastirma
# tablosunu BASLIK, SUTUN BASLIGI ve IKI HUCRE olarak yaziyor ve dordu de terimi
# `fingerprint terminal` disinda bir sekilde aniyor. Kullanici metni birebir
# istedi; muafiyet bu yuzden KELIMEYE degil CUMLEYE baglandi: her giris, satirin
# tasidigi metnin TAMAMI ya da bir Go string literalinin `"..."` arasindaki tam
# icerigidir (metin landingview.go'da veri olarak yasar, sablon oradan basar).
# Metin bir harf degisirse muafiyet DUSER ve R1 yeniden FAIL verir — dogru yon,
# cunku yeni cumleyi birinin bilerek muaf tutmasi gerekir. Eslesme HARFI HARFINE
# (index), regex degil; `|` ayirici.
R1_WAIVER_PHRASES='fingerprint terminal|retire the fingerprint box.|fingerprint / card devices|"biometric data"|fingerprints stored = gdpr weight'
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
  awk -v mode="$1" -v phs="$R1_WAIVER_PHRASES" -v paths="$R1_WAIVER_PATHS" -v trig="$R1_TRIGGERS" '
    BEGIN {
      n = split(phs, ph, "|")
      # Bos giris (listenin sonunda unutulmus bir `|`): BSD awk index("abc","")=1
      # dondurur ve asagidaki strip dongusu hic bitmez; gawk 0 dondurur ve ayni hata
      # CI icinde sessiz gecer. Iki platformda iki davranis yerine tek, gurultulu hata.
      # (Kesme isareti YOK — bu blok tek tirnak icinde.)
      for (k = 1; k <= n; k++) if (length(ph[k]) == 0) {
        print "redline-check: R1_WAIVER_PHRASES has an empty entry" > "/dev/stderr"; exit 2
      }
    }
    # strip(s, p): p ifadesinin HER harfi harfine gecisini s icinden cikarir.
    # gsub DEGIL: gsub deseni regex sayar, "box." noktasi her karakterle eslesirdi.
    # (Bu blok tek tirnak icinde; yorumlarda kesme isareti KULLANILMAZ — ilk hali
    # tam bu yuzden sessizce bos donuyordu.)
    function strip(s, p,    i) {
      while ((i = index(s, p)) > 0) s = substr(s, 1, i - 1) " " substr(s, i + length(p))
      return s
    }
    length($0) == 0 { next }
    {
      w = 0
      if ($0 ~ paths) {
        rest = tolower($0); hit = 0
        for (k = 1; k <= n; k++) {
          if (index(rest, ph[k]) > 0) { hit = 1; rest = strip(rest, ph[k]) }
        }
        if (hit && rest !~ trig) w = 1   # ifadeler cikarilinca tetikleyici kalmadiysa muaf
      }
      if ((mode == "waived") == w) print
    }' <<<"$r1_raw"
  # awk'in cikis kodu $(...) icinde kaybolur ve bos bir secim "temiz" okunur — tam
  # olarak scan_in'in isaretciyle kapattigi delik. Ayni isaretci burada da yazilir,
  # boylece bir awk hatasi exit 2 ile biter, sessiz bir exit 0 ile degil.
  local rc=${PIPESTATUS[0]}
  if [[ $rc -ne 0 ]]; then
    echo "${RED}ATLANDI${OFF}: R1 muafiyet secimi hata verdi (awk exit $rc) — TARAMA GUVENILIR DEGIL." >&2
    echo "$rc" >>"$SCAN_ERR"
  fi
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
# M11 API-2 (ADR 0028 §4: "R3 tarayıcısının kapsamına girer"): punch_events -- the integrators'
# feed, append-only like transactions -- is the same rule's second table.
report FAIL R3 "transactions / punch_events tablosuna UPDATE/DELETE — kayitlar immutable" \
  "$(scan -i -e '(UPDATE|DELETE +FROM) +([a-zA-Z_][a-zA-Z0-9_]*\.)?"?(transactions|punch_events)"?\b' \
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
# 🔴 M11 API-3 (ADR 0028 §3) -- IKI KOL. İlk turun eki (yalniz `public.` onekli satir-yerel
# desen) BOSA CALISIYORDU (iki mercek olctu): gercek sayac yazmalarinin BESI DE cok satirli ve
# takma adli -- db/queries/tags.sql'in AdvanceTagCounter'i (`UPDATE tags t` / `SET last_ctr`),
# 00043'un holding_advance_tag_counter'i ve 00045'in iki govdesi (`UPDATE public.tags AS t` /
# `SET last_ctr = p_ctr`) -- ve `UPDATE ONLY`, takma adli ve satir bolunmus sondalarin ucu de
# geciyordu. Simdi:
#
#   SATIR KOLU (her dosya, testler dahil) -- eski desen + istege bagli `ONLY`, sema niteligi
#     (`public.`, tirnakli/bosluklu), tirnakli `"tags"`/`"last_ctr"`; suzgec ayni satirdaki
#     `last_ctr <`. Takma ad YOK: satir-yerel suzgec korumayi (`prev.old_ctr <`) goremez.
#   IFADE KOLU (`_test.go` HARIC) -- `rg -U` ile `UPDATE [ONLY] [public.]tags [[AS] t] SET
#     last_ctr` basindan ilk `;` ya da backtick'e kadar IFADENIN TAMAMI; awk her ifadede
#     `<ad>ctr <` bicimli kati bir karsilastirma arar (`<=` sayilmaz) ve yoksa ifadenin ilk
#     satirini basar. Koruma takma adli CTE'de (`prev.old_ctr < @ctr`, `v.old_ctr < p_ctr`)
#     olsa da gorur.
#
# OLCULDU (2026-10-15, ayni SRC/GEN_EXCLUDE): ifade kolu agacta 78 satirlik ham eslesmeden 0
# FAIL; bes gercek ifadenin korumasi silinmis sondada DORDU (tags.sql:39, 00043:257,
# 00045:75 ve :131) FAIL. `_test.go` HARIC, cunku testler
# KASITLI korumasiz yazmalar tasir: tagsinventory_test.go'nun uc tetikleyici sondasi (geri sarma
# 23001 ile reddedilmeli) ve marketing_claims_test.go'nun `<=` mutant dizesi -- testleri dahil
# etmek bu dort satiri FAIL yapardi. Testler SATIR koluyla taranmaya devam eder.
#
# ⚠️ KALAN SINIRLAR, ADIYLA (kanit degildir): (1) Go'da `"UPDATE tags " + "SET last_ctr..."`
# gibi parcalanmis dize ifade basini kurmaz; (2) koruma SOZDIZIMSEL olarak aranir -- `ctr <
# 999999` gibi anlamsiz bir karsilastirma da yeter; (3) ifade `;`/backtick'e kadar okunur,
# PL/pgSQL'de govdenin baska bir parcasindaki `ctr <` korumayi saglamis sayilabilir; (4)
# dinamik SQL (format/EXECUTE) gorunmez. §4.4'un GERCEK garantisi yine yukaridaki uclu:
# kati yuklem, 00013'un tags_counter_monotonic tetikleyicisi ve yaris testleri. Pin:
# cmd/tappa/scripttest TestRedline_R4R7R7bR7dCatchDeliberateViolations.
R4_HEAD='UPDATE\s+(ONLY\s+)?("?public"?\s*\.\s*)?"?tags"?'
bad_ctr=$(scan -i -e "${R4_HEAD}"'\s+SET\s+"?last_ctr' | grep -viE 'last_ctr"? *<' || true)
r4_stmt_raw=$(scan -U -i --glob '!*_test.go' -e "${R4_HEAD}"'(\s+(AS\s+)?[A-Za-z_][A-Za-z0-9_]*)?\s+SET\s+"?last_ctr[^;`]*' || true)
r4_stmt=$(printf '%s\n' "$r4_stmt_raw" | awk '
  # Ardisik ayni-dosya satirlari bir bolge; bolgedeki HER ifade basi ayri yargilanir, yani iki
  # bitisik bolgenin birlesmesi korumasiz ifadeyi komsusunun korumasiyla AFFETMEZ.
  function flush(   t, rest, body, k, bad) {
    if (n == 0) return
    t = tolower(text); bad = 0
    while (match(t, /update[[:space:]]+(only[[:space:]]+)?("?public"?[[:space:]]*\.[[:space:]]*)?"?tags"?([[:space:]]+(as[[:space:]]+)?[a-z_][a-z0-9_]*)?[[:space:]]+set[[:space:]]+"?last_ctr/)) {
      rest = substr(t, RSTART + RLENGTH)
      k = match(rest, /[;`]/); body = (k ? substr(rest, 1, k - 1) : rest)
      if (body !~ /ctr"?[[:space:]]*<[^=]/) bad = 1
      t = rest
    }
    if (bad) print first
    n = 0; text = ""
  }
  length($0) == 0 { next }
  {
    p = $0; sub(/:[0-9]+:.*$/, "", p)
    no = $0; sub(/^[^:]*:/, "", no); sub(/:.*$/, "", no)
    c = $0; sub(/^[^:]*:[0-9]+:/, "", c)
    if (n > 0 && (p != path || no + 0 != prev + 1)) flush()
    if (n == 0) first = $0
    path = p; prev = no + 0; n++; text = text " " c
  }
  END { flush() }')
bad_ctr=$(printf '%s\n%s\n' "$bad_ctr" "$r4_stmt" | grep -v '^$' || true)
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
policy_waivers=""
body_waivers=""

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
#   · blok yorumu BOSLUKLA degistirilir, bosluksuz silinmez (T130 dusuk turu,
#     2026-10-18): PostgreSQL icin `/* */` bir AYIRICI'dir, yani
#     `GRANT/**/DELETE ON public.transactions TO tappa_app` gecerli bir GRANT'tir;
#     bosluksuz silmek onu `grantdelete …` yapiyordu ve `^grant ` bekleyen her
#     kuraldan (append-only, sema capinda, GRANT ALL, R5b muafiyeti) geciyordu
#     (olculdu, exit 0). Kirmizi vaka: cmd/tappa/scripttest TestRedline_ABlockCommentSeparatesTokens;
#   · string literallerini ve dolar tirnakli govdeleri KORUR, icindeki `;` yerine
#     \001 koyar — `DEFAULT ';'` gibi bir sutun ifadeyi bolmesin (yanlis pozitif),
#     plpgsql govdesi de parcalanmasin;
#   · soldan saga tek gecis: string icindeki `--` yorum, yorum icindeki `'` string
#     sayilmaz.
#
# 🔴 T137 (2026-10-19) -- AYIRICI POSTGRESQL'IN SOZCUK KURALLARINA GETIRILDI. Her sapma ayni
# yonde bir kacisti: ayirici bir seyi dize/yorum sanip PostgreSQL onu KOD diye kosarsa, o
# kod her `^grant`/`^alter` kuralinin gozunden duser. Olculdu (17-alpine, 2026-10-19):
#   · `E'…'` kacisli dize: `\'` dizeyi KAPATMAZ (`SELECT E'\''` tek tirnak dondurur). Eski
#     ayirici kapatiyordu; sonraki `'` yeni bir dize aciyor ve bir sonraki satirdaki
#     `GRANT DELETE ON public.transactions TO tappa_app` dizenin icinde kaliyordu (exit 0).
#     `E` oneki yalniz bir tanimlayicinin parcasi DEGILSE sayilir (`name'x'` E dizesi degil);
#     `''` her iki dize biciminde de tirnaktir.
#   · adlandirilmis dolar tirnagi (`$x$ … $x$`) artik bilinir. Bilinmemesi bir govdeden
#     fazlasini gizliyordu: `SELECT $x$/*$x$; GRANT …; SELECT $x$*/$x$;` -- PostgreSQL'e iki
#     dize ve aralarinda bir GRANT, eski ayiriciya tek bir blok yorumu.
#   · `"…"` tanimlayicisi da bir kapsamdir: `SELECT 1 AS "/*"; GRANT …; SELECT 1 AS "*/";`
#     ayni sekilde gizleniyordu. Icindeki `""` tirnaktir.
#   · blok yorumu IC ICE gecer (`/* a /* b */ c */` tek yorumdur; olculdu, `SELECT 1` doner).
#     Ilk `*/`te bitirmek kalan `c */ it's …` metnini koda katiyordu ve oradaki `'` bir
#     dize acip arkasindaki GRANT'i yutuyordu.
#   · `$` bir tanimlayicinin icindeyse dolar tirnagi ACMAZ: `a$$` bir tanimlayicidir
#     (olculdu, `SELECT 1 AS a$$` -> sutun `a$$`). Ust bitli baytlar tanimlayici harfidir;
#     bu yuzden ayirici `LC_ALL=C` ile bayt bayt kosar (gawk'in UTF-8 kipinde araliklar
#     karakter sayilir).
# Sevk edilen 59 migration'da ciktinin BAYT BAYT ayni kaldigi olculdu: her yeni kural
# yalniz bugunku agacta gecmeyen bir yazimda fark eder.
# Kirmizi vakalar: cmd/tappa/scripttest TestRedline_T137_TheLexerReadsWhatPostgresReads.
#   · fix1 (2026-10-19) -- DIZE DEVAMI. Bir dize kapandiktan sonra YALNIZ bosluk (\t \f \v
#     dahil) ve `--` yorumu gelir, aralarinda en az bir satir sonu (\n ya da \r) olur ve yeni
#     bir `'` acilirsa PostgreSQL AYNI dizeyi AYNI kipte surdurur (olculdu, 17: `SELECT E'a'` +
#     satir sonu + `'\''` -> `a'`; blok yorumu araya girerse devam YOKTUR, sozdizimi hatasi).
#     Ayirici devam eden parcayi duz dize sayiyordu: `\'` onu kapatmiyor sanildi ve arkasindaki
#     GRANT dizenin icinde kaldi (exit 0, has_table_privilege t). Artik `pend`/`pnl` devami
#     modeller; sevk edilen agacta 23 duz-dize devami var (hepsi COMMENT ON metni) ve ciktilari
#     degismedi. E/U& dizesinin devami ayrica R5b FAIL'dir (asagida, sql_scan'in `econt`'u).
#     Kirmizi vakalar: TestRedline_T137_AStringContinuesAfterALineBreak.
#
# SINIRLAR (sayili, yanlis-NEGATIF): (1) `standard_conforming_strings = off` ile duz
# `'…'` dizesi de `\'` kacisini tasir; ayirici bunu modellemez, onun yerine R5b o adi
# gecen her migration'i FAIL eder (asagida). (2) `U&'…'` dizesinin icindeki `\0041` bir
# harftir -- dize olarak dogru ayrilir, ama govde olarak okunamaz; R5b E/U& dizeli bir
# DO/fonksiyon govdesini FAIL eder. (3) Govdenin ICI burada KORUNUR, denetlenmez --
# onu sql_scan okur (asagida, R5b'nin govde kurali).
sql_lex() {
  LC_ALL=C awk '
    function idch(ch) { return ch != "" && ch ~ /[A-Za-z0-9_$\200-\377]/ }
    BEGIN { m = ""; depth = 0; tag = ""; pend = ""; pnl = 0 }
    {
      line = $0; out = ""; i = 1; n = length(line); prev = " "; pp = " "
      while (i <= n) {
        c = substr(line, i, 1); c2 = substr(line, i, 2)
        # Dize DEVAMI (fix1): kapanan bir dizeden sonra yalniz bosluk ve `--` yorumu, aralarinda en az
        # bir satir sonu, sonra `\047` -> AYNI dize, AYNI kipte surer (E kipinin `\\` kacisi dahil).
        if (pend != "" && m == "") {
          if (c == " " || c == "\t" || c == "\f" || c == "\v") { out = out c; pp = prev; prev = c; i++; continue }
          if (c == "\r") { pnl = 1; out = out c; pp = prev; prev = c; i++; continue }
          if (c == "\047" && pnl) { m = pend; pend = ""; out = out c; i++; continue }
          if (c2 != "--") pend = ""
        }
        if (m == "b") {
          if (c2 == "*/") { depth--; i += 2; if (depth == 0) m = "" }
          else if (c2 == "/*") { depth++; i += 2 }
          else i++
          prev = " "; pp = " "
          continue
        }
        if (m == "d") {
          if (substr(line, i, length(tag)) == tag) { m = ""; out = out tag; i += length(tag); prev = " "; pp = " " }
          else { out = out ((c == ";") ? "\001" : c); i++ }
          continue
        }
        if (m == "s" || m == "e" || m == "q") {
          q = (m == "q") ? "\"" : "\047"
          if (m == "e" && c == "\\") {
            nx = substr(line, i + 1, 1)
            out = out c ((nx == ";") ? "\001" : nx); i += 2
          } else if (c == q && substr(line, i + 1, 1) == q) { out = out c c; i += 2 }
          else if (c == q) { if (m != "q") { pend = m; pnl = 0 }; m = ""; out = out c; i++; prev = c; pp = " " }
          else { out = out ((c == ";") ? "\001" : c); i++ }
          continue
        }
        if (c2 == "/*") { m = "b"; depth = 1; out = out " "; i += 2; prev = " "; pp = " "; continue }
        if (c2 == "--") { i = n + 1; continue }
        if (c == "$" && !idch(prev) && match(substr(line, i), /^[$]([A-Za-z_\200-\377][A-Za-z0-9_\200-\377]*)?[$]/)) {
          tag = substr(line, i, RLENGTH); m = "d"; out = out tag; i += RLENGTH; continue
        }
        if (c == "\047") {
          m = ((prev == "e" || prev == "E") && !idch(pp)) ? "e" : "s"
          out = out c; i++; continue
        }
        if (c == "\"") { m = "q"; out = out c; i++; continue }
        out = out c; pp = prev; prev = c; i++
      }
      if (pend != "") pnl = 1
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
# muameleyi goren tablolar: `00021`in TRUNCATE korumasindaki alti tablo, 00026'nin
# yedincisi `operator_audit_log` (platform operatorunun append-only izi) ve 00046'nin
# sekizincisi `site_punches` (holding sahasinin izdusumu — M11 API-1c), 00048'in
# dokuzuncusu `punch_events` (dis punch akisi — M11 API-2) ve 00068'in onuncusu ile
# on birincisi `punch_claims`, `punch_claim_decisions` (eksik yari talebi ve karari —
# M15-15). Bu liste,
# scripts/pg-restore-verify.sh'in `trunc_tables` listesi ve migration'lardaki
# tappa_forbid_mutation tetikleyicileri UC temsildir; cmd/tappa/scriptguards_test.go
# kumeyi migration'lardan TURETIP ikisini de karsilastirir, yani bir sekizincisi
# eklenip burada unutulursa test kirmizi verir.
# ⚠️ `operator_audit_log` asagidaki `[^ ]*` onekiyle `audit_log` alt dizgisinden de
# yakalanirdi (tesadufen); acikca yazilir ki liste turetilmis kumeyle esit olsun.
APPEND_ONLY='(transactions|audit_log|transaction_reviews|policy_versions|billing_periods|legal_documents|operator_audit_log|site_punches|punch_events|punch_claims|punch_claim_decisions)'

# R5b'nin 00058 muafiyetinin dosya sekli (asagida, muafiyetin yorumunda gerekcesiyle). 0 = sekil
# sevk edilen dosyanin sekli; baska her sey (sha256 araci yoksa dahil) 1 -- muafiyet yok, FAIL.
R5B_00058_BODIES=4c86b148d9ba9bdb8a5f85fe459b619dd5120fb6fd2016c53cf95fabf5adaf52
r5b_00058_shape() {
  local f=$1 up=$2 down=$3 whole outside hash h hasher="" grants tops
  for h in "sha256sum" "shasum -a 256"; do
    # shellcheck disable=SC2086 # h bilerek bolunur (arac + argumanlari)
    if command -v "${h%% *}" >/dev/null 2>&1 &&
      [[ $(printf abc | $h 2>/dev/null) == ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad* ]]; then
      hasher=$h
      break
    fi
  done
  [[ -n $hasher ]] || return 1
  whole=$(sql_lex <"$f" | tr '[:upper:]' '[:lower:]' | tr '\n' ' ')
  # shellcheck disable=SC2086 # hasher bilerek bolunur
  hash=$(awk -F'[$][$]' '{ for (i = 2; i <= NF; i += 2) print $i }' <<<"$whole" | $hasher)
  [[ ${hash%% *} == "$R5B_00058_BODIES" ]] || return 1
  ! grep -qE '[$][a-z_][a-z0-9_]*[$]|\\' <<<"$whole" || return 1
  ! grep -qE '[$][$]|(^|[^a-z0-9_])(do|execute)([^a-z0-9_]|$)' \
      <<<"$(goose_down "$f" | sql_lex | tr '[:upper:]' '[:lower:]')" || return 1
  outside=$(awk -F'[$][$]' '{ for (i = 1; i <= NF; i += 2) printf "%s ", $i }' <<<"$whole")
  [[ $(grep -oF 'execute function public.webhook_delivery_settle()' <<<"$outside" | wc -l) -eq 1 ]] || return 1
  outside=${outside/execute function public.webhook_delivery_settle()/}
  ! grep -qE '(^|[^a-z0-9_])execute([^a-z0-9_]|$)' <<<"$outside" || return 1
  grants=$(tr -c 'a-z0-9_' '\n' <<<"$outside" | grep -cx grant || true)
  tops=$(grep -c '^grant ' <<<"$up"$'\n'"$down" || true)
  [[ $grants -eq $tops ]] || return 1
  ! grep -E '^grant .* to (group )?([a-z0-9_]+ , (group )?)*public( ,| with |$)' <<<"$up"$'\n'"$down" >/dev/null \
    || return 1
}


# --- sql_scan: R5b'nin govde kurali icin (T137) ------------------------------
#
# sql_scan <dosya> -- bir migration'in TAMAMINI (Up + Down) okur ve iki tur kayit basar:
#   top<TAB><kod>              her ust duzey ifade icin bir satir;
#   body<TAB><tur><TAB><kod>   her GOVDE icin bir satir: `dollar` (ust duzeydeki her dolar
#                              tirnakli dize -- DO, fonksiyon, COMMENT IS $$…$$ ayrimsiz),
#                              `quoted` (`DO '…'` ya da `CREATE FUNCTION|PROCEDURE … AS '…'`
#                              ifadesindeki duz dize, `''` cozulmus) ve `escaped` (ayni yerde bir
#                              `E'…'`/`U&'…'` dizesi -- kacislari bir harf yazabilir, yani metni
#                              govde olarak OKUNAMAZ; kodu yerine ham metin basilir).
#   pin<TAB><tur><TAB><metin>  her `body` kaydinin pin metni: kodu ARTI dize iceriklerini tasir
#                              (R5B_BODY_DDL'nin sha256'si bunlarin ozetidir -- fix1);
#   setcfg<TAB><ad|!>          her `set_config` cagrisi: ilk arguman tek bir duz dize ise o ad
#                              (kucuk harf), degilse `!` (ust duzeyde ve her govdede -- fix1);
#   econt<TAB><n>              E/U& dizesinin satir sonundan sonraki DEVAMI sayisi (fix1);
#   uident<TAB><n>             `U&"…"` tanimlayicisi sayisi (fix1);
#   qsettings<TAB>1            tirnakli `"pg_settings"` tanimlayicisi (kodda `"i"` olur, fix1).
# Dize devami (bkz. sql_lex) burada da modellenir: devam eden parcalar TEK bir dize olarak
# birlestirilir, yani `DO 'BEGIN EXEC'` + satir sonu + `'UTE …'` govdesi `BEGIN EXECUTE …` okunur
# (PostgreSQL'in kostugu metin; olculdu). `COMMENT ON … IS $$…$$` govde sayilmaz (yorum metnidir).
# <kod> = yorumlar atilmis; her dize, dolar dizesi ve tirnakli tanimlayici bir yer tutucuyla
# ('s', $s$, "i") degistirilmis; kucuk harf; tek bosluk. Yani bir govdenin `-- execute` yorumu ya
# da `has_function_privilege(…, 'EXECUTE')` dizesi kod DEGILDIR, `EXECUTE 'GRANT …'` ise koddur
# (`execute 's'`). Sozcuk kurallari sql_lex'inkilerle AYNIDIR (E/U& dizesi, adlandirilmis dolar
# tirnagi, tanimlayici, ic ice blok yorumu) ve ayni sebeple: govdenin ici de PostgreSQL'in
# (PL/pgSQL'in cekirdek ayiricisi) okudugu gibi okunmali, yoksa yorum sanilan bir satir kosar.
sql_scan() {
  LC_ALL=C awk '
    function idch(ch) { return ch != "" && ch ~ /[A-Za-z0-9_$\200-\377]/ }
    function push(k, v) { nseg++; segk[nseg] = k; segv[nseg] = v }
    # lex(s) -> segk/segv: c (kod), s (duz dize, `\047\047` cozulmus), x (E/U& dizesi, ham),
    # d (dolar dizesi), q (tirnakli tanimlayici). Yorum kodda bir bosluk olur.
    function lex(s,   i, n, c, c2, m, d, t, prev, pp, cur, q, j, nl, ch) {
      nseg = 0; cur = ""; i = 1; n = length(s); m = ""; prev = " "; pp = " "
      while (i <= n) {
        c = substr(s, i, 1); c2 = substr(s, i, 2)
        if (m == "b") {
          if (c2 == "*/") { d--; i += 2; if (d == 0) m = "" }
          else if (c2 == "/*") { d++; i += 2 }
          else i++
          continue
        }
        if (m == "l") { if (c == "\n") m = ""; i++; continue }
        if (m == "d") {
          if (substr(s, i, length(t)) == t) { push("d", cur); cur = ""; m = ""; i += length(t); prev = " "; pp = " " }
          else { cur = cur c; i++ }
          continue
        }
        if (m == "s" || m == "e" || m == "u" || m == "q") {
          q = (m == "q") ? "\"" : "\047"
          if (m == "e" && c == "\\") { cur = cur c substr(s, i + 1, 1); i += 2 }
          else if (c == q && substr(s, i + 1, 1) == q) { cur = cur c; i += 2 }
          else if (c == q) {
            # Dize DEVAMI (sql_lex ile ayni): bosluk/`--` yorumu + en az bir satir sonu + `\047` ->
            # ayni dize, ayni kip; parcalar BIRLESTIRILIR (PostgreSQL bu metni kosar).
            if (m != "q") {
              j = i + 1; nl = 0
              while (j <= n) {
                ch = substr(s, j, 1)
                if (ch == " " || ch == "\t" || ch == "\f" || ch == "\v") j++
                else if (ch == "\n" || ch == "\r") { nl = 1; j++ }
                else if (substr(s, j, 2) == "--") { while (j <= n && substr(s, j, 1) != "\n" && substr(s, j, 1) != "\r") j++ }
                else break
              }
              if (nl && substr(s, j, 1) == "\047") { if (m != "s") necont++; i = j + 1; continue }
            }
            push((m == "e" || m == "u") ? "x" : m, cur); cur = ""; m = ""; i++; prev = c; pp = " "
          }
          else { cur = cur c; i++ }
          continue
        }
        if (c2 == "/*") { cur = cur " "; m = "b"; d = 1; i += 2; prev = " "; pp = " "; continue }
        if (c2 == "--") { cur = cur " "; m = "l"; i += 2; continue }
        if (c == "$" && !idch(prev) && match(substr(s, i), /^[$]([A-Za-z_\200-\377][A-Za-z0-9_\200-\377]*)?[$]/)) {
          push("c", cur); cur = ""; t = substr(s, i, RLENGTH); m = "d"; i += RLENGTH; continue
        }
        if (c == "\047") {
          push("c", cur); cur = ""
          if ((prev == "e" || prev == "E") && !idch(pp)) m = "e"
          else if (prev == "&" && (pp == "u" || pp == "U")) m = "u"
          else m = "s"
          i++; continue
        }
        if (c == "\"") {
          if (prev == "&" && (pp == "u" || pp == "U")) nuid++
          push("c", cur); cur = ""; m = "q"; i++; continue
        }
        cur = cur c; pp = prev; prev = c; i++
      }
      # Kapanmamis bir kapsam (dosya sonunda acik dize/govde) KOD sayilir: PostgreSQL onu
      # reddeder, yani gizledigi bir sey kosmaz -- ama ayirici da onu sessizce yutmasin.
      push("c", cur)
    }
    function norm(s) { s = tolower(s); gsub(/[ \t\n\r\f\v]+/, " ", s); sub(/^ /, "", s); sub(/ $/, "", s); return s }
    # cfgscan -- son lex() sonucundaki her `set_config` cagrisi icin bir `setcfg` kaydi: ilk arguman TEK bir duz
    # dize sabitiyse onun kucuk harfli metni, degilse `!` (birlestirme, degisken, E-dizesi, dolar
    # dizesi, tirnakli `"set_config"` -- hepsi). PostgreSQL ayar adini buyuk/kucuk harf duyarsiz okur.
    function cfgscan(   k, v, r, b, a, w) {
      w = "[ \t\n\r\f\v]*"
      for (k = 1; k <= nseg; k++) {
        if (segk[k] == "q" && segv[k] == "set_config") { print "setcfg\t!"; continue }
        if (segk[k] == "q" && segv[k] == "pg_settings") { print "qsettings\t1"; continue }
        if (segk[k] != "c") continue
        v = tolower(segv[k])
        while (match(v, /set_config/)) {
          b = (RSTART > 1) ? substr(v, RSTART - 1, 1) : " "
          a = substr(v, RSTART + 10); v = a
          if (idch(b) || idch(substr(a, 1, 1))) continue
          if (a ~ ("^" w "[(]" w "$") && segk[k + 1] == "s" && segk[k + 2] == "c" && segv[k + 2] ~ ("^" w ",")) {
            r = tolower(segv[k + 1]); gsub(/[\t\n\r]/, " ", r); print "setcfg\t" r
          } else print "setcfg\t!"
        }
      }
    }
    # code_of -- govdenin KODU (dizeler yer tutucuda). Yan urun PIN: ayni govdenin kodu ARTI her
    # dizenin, dolar dizesinin ve tirnakli tanimlayicinin ICERIGI (tur + uzunluk + metin; yorumlar
    # haric) -- R5B_BODY_DDL pini bunun ozetidir, yani pinli bir govdede bir dize sabiti degisince
    # de pin bozulur (fix1).
    function code_of(s,   k, o, v) {
      lex(s); cfgscan(); o = ""; PIN = ""
      for (k = 1; k <= nseg; k++) {
        if (segk[k] == "c") { o = o segv[k]; PIN = PIN " " norm(segv[k]) " "; continue }
        if (segk[k] == "d") o = o " $s$ "
        else if (segk[k] == "q") o = o " \"i\" "
        else o = o " \047s\047 "
        v = segv[k]; gsub(/\n/, "\001", v); gsub(/\t/, "\002", v); gsub(/\r/, "\003", v)
        PIN = PIN segk[k] length(segv[k]) ":" v
      }
      return norm(o)
    }
    { all = all $0 "\n" }
    END {
      lex(all); cfgscan()
      for (k = 1; k <= nseg; k++) { K[k] = segk[k]; V[k] = segv[k] }
      N = nseg; stmt = ""
      for (k = 1; k <= N; k++) {
        if (K[k] == "c") {
          rest = V[k]
          while ((j = index(rest, ";")) > 0) {
            stmt = stmt substr(rest, 1, j - 1)
            if (norm(stmt) != "") print "top\t" norm(stmt)
            stmt = ""; rest = substr(rest, j + 1)
          }
          stmt = stmt rest
        } else if (K[k] == "d") {
          # `COMMENT ON … IS $$…$$` bir govde degil, bir yorumun METNIDIR: hicbir sey onu kosmaz (fix1;
          # icindeki "drop"/"alter" sozcukleri yanlis pozitifti).
          if (norm(stmt) !~ /^comment on /) { print "body\tdollar\t" code_of(V[k]); print "pin\tdollar\t" PIN }
          stmt = stmt " $s$ "
        } else if (K[k] == "q") {
          stmt = stmt " \"i\" "
        } else {
          if (norm(stmt) ~ /^(do|create (or replace )?(function|procedure))([^a-z0-9_$]|$)/) {
            if (K[k] == "s") { print "body\tquoted\t" code_of(V[k]); print "pin\tquoted\t" PIN }
            else print "body\tescaped\t" norm(V[k])
          }
          stmt = stmt " \047s\047 "
        }
      }
      if (norm(stmt) != "") print "top\t" norm(stmt)
      if (necont) print "econt\t" necont
      if (nuid) print "uident\t" nuid
    }' "$1"
}

# sha256_tool -- bilinen bir vektorle sinanmis ilk sha256 araci (r5b_00058_shape'inkiyle ayni
# secim); yoksa bos. Beyaz liste onsuz dogrulanamaz, yani yoklugu FAIL'dir, muafiyet degil.
SHA256_TOOL=""
for h in "sha256sum" "shasum -a 256"; do
  # shellcheck disable=SC2086 # h bilerek bolunur (arac + argumanlari)
  if command -v "${h%% *}" >/dev/null 2>&1 &&
    [[ $(printf abc | $h 2>/dev/null) == ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad* ]]; then
    SHA256_TOOL=$h
    break
  fi
done

# R5B_BODY_DDL -- GOVDESINDE DDL KODU OLAN sevk edilmis migration'lar (T137, 2026-10-19), her biri
# sql_scan'in `pin` kayitlarinin sha256'siyla (fix1: govdenin kodu ARTI dize iceriklerinin -- yalniz
# yorumlar disarida; ilk hali dizeleri `'s'`ye indiriyordu ve pinli bir govdede bir dize sabiti
# degisince yalniz WARN kaliyordu). Dokuzu da ayni sekildir: operator yuzeyinin
# `operator_audit_log` / `operator_read_tickets` CHECK kisitlarini (`*_kind_check`, `*_actor_shape`)
# kosullu yeniden kuran bir DO blogu (`IF EXISTS (…) THEN ALTER TABLE public.… ADD CONSTRAINT …
# NOT VALID; ELSE ALTER TABLE public.… ADD CONSTRAINT …; END IF`) -- statik, tablo adi yazili,
# yetki/RLS/politika/sahiplik degistirmiyor (okundu: 52 `alter`in 52'si `alter table public.<o iki
# tablo> add constraint`). Bugunku agacta hicbir govdede
# `EXECUTE` KODU yok (olculdu: 59 dosyanin 21'inde `execute` sozcugu geciyor, hepsi bir yorumda ya
# da `has_function_privilege(…, 'EXECUTE')` dizesinde). Bir migration deploy olunca degismez
# (CLAUDE.md §3), pin de degismez; YENI bir satir yalniz govdesi okunup gerekcesi yazilarak eklenir.
# 00063 (M10 OP-15, 2026-10-20): onuncu satir, ayni sekil -- Up'ta ve Down'da birer DO blogu,
# govdelerinde 4 `alter`in 4'u `alter table public.operator_audit_log add constraint
# operator_audit_log_kind_check` (NOT VALID / VALIDATED dallari); EXECUTE kodu yok.
# 00077 (M14-15, 2026-10-27): on birinci satir, BASKA bir sekil -- Down'da tek bir DO blogu, govdesinde
# 1 `revoke`: `REVOKE INSERT (<sekiz sutun>) ON public.admin_users FROM tappa_identitydefiner`, yalniz
# `to_regprocedure('public.identity_claim_signup(text,uuid,text)') IS NULL` iken (M14-17'nin 00078'i
# ayni yetkiyi kullanir ve geri almaz -- orkestrator karari). Statik, tablo ve rol adi yazili, yetkiyi
# yalniz DARALTIR; RLS/politika/sahiplik degistirmiyor; EXECUTE kodu yok.
# 00078 (M14-17, 2026-10-27 denetimi ORTA-1): Down'da tek DO blogu, govdesinde 1 `revoke` --
# `REVOKE INSERT (<owner satirinin sekiz sutunu>) ON public.admin_users FROM tappa_identitydefiner`,
# yalniz `identity_accept_invite*` (00077) yokken: paylasilan bir sutun yetkisi referans sayilmaz,
# kucuk numarali dosya (00077) sahibidir. Statik, tablo ve rol adi yazili, yalniz GERI ALIR (yetki
# vermez, RLS/politika/sahiplik degistirmez); EXECUTE kodu yok. Pin dosyanin butun govdelerinin
# (fonksiyonlar dahil) ozetidir.
R5B_BODY_DDL='
00027_move_legal_publishing_to_the_operator.sql ff642e7342eea75e9bd9e5a1f565ed86236a67bce4df45224a1f076ba82d6515
00029_read_tenants_from_the_operator.sql 40dfbcc6552251d0262e558bafe6b232c9002f5aafa380ffd11dc05310258d1b
00030_read_plaques_from_the_operator.sql c1799984dc7fee4729ebc671df19142bf538d1b0faf7023e1568cb1e34876387
00031_read_the_operator_audit_log.sql 57210c7a9626b265ba1d34f5d51dd1fa549d2b42114a4b8995d7bcc68b5449ca
00032_read_billing_from_the_operator.sql b66cd1fe990738486ec79f79059cdc87a4cbe4fb18d4b22b7591b5dfe7f85dff
00033_audit_opadmin_actions.sql bdb16693f87408653d97f73f40cfa368d1cb93a17c53085f30a2d5d2e44c6b03
00034_recheck_a_tenants_vat_from_the_operator.sql acff23b7eb25471c2cef5c005e0341687b02e21a165a7770259fcb3352e28fcb
00039_open_the_api_from_the_operator.sql 2e0f577c880f6814c057d7477e275b1b90ae4596c9492a99799569d49b3f71b7
00042_put_holdings_on_the_operator_surface.sql ebcf1ce2ebe977910e25255dfb91915d7d336b84c808fda722c79031133130a5
00063_suspend_a_tenant_from_the_operator.sql ff003d61db146d20479be927e8cb257f2840f39715c68b07d5b4c81e73e85585
00077_invite_administrators.sql 5cb0b95eefc218083148f9d8c1c13573e3302c5fca5bbfb99f02765ee4349382
00078_verify_signups_by_e_mail.sql c87177d2b719e11b4782e7737c73c2fba68311bfa8ebf029fbfb9094d4b21e4e
'

# R5B_SET_CONFIG -- bir migration'in `set_config` ile kurabilecegi ayarlar (fix1, 2026-10-19). Sevk
# edilen agacin yedi adi (olculdu: 00048 `app.tenant_id`, 00049 `tappa.feed_counters`, 00050
# planlayici ve zaman asimi ayarlari); hicbiri dize ayirimini, rolu ya da RLS'i degistirmez. Yeni bir
# ad yalniz okunup gerekcesi yazilarak eklenir.
R5B_SET_CONFIG="app.tenant_id enable_bitmapscan enable_seqscan enable_sort jit lock_timeout tappa.feed_counters"

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
  #   T137 (2026-10-19): PUBLIC'e her GRANT · bir ifadenin ICINDEKI GRANT (`CREATE SCHEMA
  #   … GRANT`) · govde KODUNDA `EXECUTE`/DDL (R5B_BODY_DDL pini disinda) · E/U& dizeli
  #   govde · `standard_conforming_strings`/`backslash_quote` · goose ENVSUB; ve ayiricinin
  #   `E'…'`, adlandirilmis dolar tirnagi, `"…"` tanimlayicisi, ic ice blok yorumu ve
  #   tanimlayici icindeki `$` sinirlari (bkz. sql_lex).
  #   T137 fix1 (2026-10-19): dize DEVAMI iki ayiricida (satir sonundan sonra `'` ile suren dize ayni
  #   kipte surer; DO govdesi birlestirilmis metniyle okunur) ve E/U& dizesinin devami FAIL ·
  #   `set_config`in ayar adi tek bir duz sabit ve R5B_SET_CONFIG'de olmali · `pg_settings`, `SET
  #   NAMES`, `client_encoding`, `U&"…"` tanimlayicisi FAIL · R5B_BODY_DDL pini dize iceriklerini de
  #   kapsar · `COMMENT ON … IS $$…$$` govde degildir (yanlis pozitif kapandi).
  #
  # ⚠️ VE BU LISTE 3. TURDA FAZLA SEY IDDIA EDIYORDU. `(true)` "kapatildi" diye
  # yaziliydi; olculdu ki `USING(true)` — o dortlunun ILKI, yalnizca boslugu
  # alinmis — exit=0 veriyordu. Liste artik normalizasyona atif yapiyor; iddiayi
  # tasiyan sey bir cumle degil, o adim.
  #
  # KAPATILAMAYANLAR, ADIYLA (bu kural ne kadar genisletilirse genisletilsin):
  #   · AYIRICININ KENDISI. sql_lex/sql_scan PostgreSQL'in sozcuk kurallarinin bir MODELIDIR; T137
  #     uc turda modelin uc kez yanlis okundugunu olctu (E'' kacisi, dize devami, birlestirilmis
  #     ayar adi) ve her seferinde PostgreSQL'in kostugu bir GRANT redline'a gorunmedi. Modelin
  #     disinda kalan bir yazim (bilinmeyen bir dize kipi, dize ayirimini degistiren baska bir
  #     ayar, migration disinda bir rolde/veritabaninda onceden kurulmus `ALTER ROLE … SET`) yine
  #     kacabilir. KALICI AG bu yuzden metinde degil veritabanindadir: internal/db
  #     TestPrivCatalog_TheWholeCatalogueIsTheAllowlist zincir kostuktan sonra her KULLANICI
  #     semasinin TUM yetki katalogunu (her tablo/sutun/dizi/fonksiyon/sema, veritabani, eklenti;
  #     pg_* ve superuser disindaki her rol + PUBLIC; MAINTAIN dahil her fiil ve grant option'i;
  #     ENABLE/FORCE, sahip, definer) tek bir izin listesiyle birebir karsilastirir -- ayiricidan
  #     bagimsiz (T137 fix1, DUSUK turu);
  #   · dinamik SQL'in ICERIGI — `EXECUTE format('ALTER TABLE %I NO FORCE …', t)`; ifade
  #     calisma aninda kurulur, dosyada hicbir yerde yazmaz. T137'den beri (2026-10-19)
  #     `EXECUTE`'un KENDISI bir migration govdesinde FAIL'dir (R5B_BODY_DDL disinda), ama
  #     kurulan metin okunmaz: kayitli bir dosyanin pinlenmis govdesi ne kurarsa kurar, ve
  #     migration DISINDA tanimli bir fonksiyon (eklenti, elle yaratilmis) cagrilabilir;
  #   · govdedeki DML ve yuklem — T137'nin govde kurali DDL/DCL fiillerini ve `EXECUTE`'u
  #     arar (`grant revoke alter drop create truncate reassign`, ic ice `do`); bir
  #     govdenin `UPDATE transactions`'i, `SET ROLE`'u ya da `COPY … PROGRAM`'i onun
  #     sozcukleri degildir. Bu da bir SOZCUK taramasidir: sozcuk listesinin disinda kalan
  #     bir fiil sessiz gecer;
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
  #          ✅ YENIDEN OLCULDU (M8-04 FAZ B3, 2026-08-20) — cunku 3. turun bu
  #          satiri, haritanin OLCUMU TEKRARLANMAYAN tek satiriydi ve ayni
  #          haritanin BASKA iki satiri yanlis cikmisti (RLS kapatma 4. turda,
  #          rlsforce 5. turda). Sonda: `CREATE RULE zz_probe_rule AS ON INSERT TO
  #          transactions DO INSTEAD NOTHING;` sevk edilmis semaya UYGULANDI (bir
  #          RULE'un etkisi baska bir baglantiya gorunur olmak zorunda, yani
  #          BEGIN … ROLLBACK ise yaramaz), sonra DROP edildi:
  #            Row1 FAIL · Row2 FAIL · Row3 PASS · Row4 FAIL · Row5 FAIL ·
  #            Row6 FAIL · Row7 FAIL              -> 7'nin 6'si, PASS eden Row3
  #          Geri alindi ve DOGRULANDI: pg_rules = 0, transactions satir sayisi
  #          degismedi (426 791 -> 426 791). SAYI DOGRU CIKTI; degistirilecek bir
  #          sey yok, degisen tek sey artik IKI kez olculmus olmasi.
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

  # 🔴 TEK MUAFIYET (T130, 2026-10-18): 00058'in Down'i kendi KISITLAYICI (RESTRICTIVE) DELETE
  # politikasini siler. Kisitlayici bir politikayi silmek erisimi DARALTMAZ, GENISLETIR -- ama yalniz
  # fiil hala varsa: ayni Down, politikadan ONCE `tappa_app`'in DELETE'ini geri alir, yani Down'dan
  # sonra o fiil yoktur ve tablo 00048'in hâline doner. Her taramada WARN olarak basilir.
  #
  # 🔴 DUZELTME TURU 1 (T130, 2026-10-18) -- ilk hali muafiyeti Up+Down BIRLESIMINDEN okuyordu ve uc
  # mutant gecti (olculdu, exit 0): (W6) ayni `drop policy`'yi 00058'in UP'ina yazmak -- birlesimde
  # Down'daki REVOKE onu da "onceliyordu"; (W7) Down'da REVOKE'tan sonra DELETE'i yeniden GRANT edip
  # politikayi silmek; (W10) ayni yerde DELETE'i PUBLIC'e vermek. Simdi:
  #   - Up'taki her `drop policy` FAIL'dir -- muafiyet yalniz Down'a bakar;
  #   - Down'da politikadan ONCE gelen ifadeler TAM OLARAK su ikisidir, bu sirayla:
  #     `set local lock_timeout = '1s'` ve `revoke delete on public.webhook_deliveries from tappa_app`
  #     (bir GRANT -- tablo, `all tables in schema`, PUBLIC, rol uyeligi -- ya da bir DO blogu araya
  #     giremez: liste bir beyaz liste, bir yasak listesi degil);
  #   - Down'un HICBIR yerinde (politikadan SONRA da) bir `grant` ifadesi yoktur: politikasiz tabloya
  #     geri verilen DELETE ayni genislemedir;
  #   - politika Down'da bir kez silinir.
  #
  # 🔴 DUSUK TURU (T130, 2026-10-18) -- dort mutant daha gecti (olculdu, exit 0): (V05) Down'da
  # politikadan SONRA `DO $$ … EXECUTE 'GRANT DELETE …' $$` -- ifade `do` ile basliyor, `^grant`
  # onu gormuyordu; (V06) ayni GRANT adlandirilmis dolar tirnagi ($x$) icinde -- ayirici onu
  # bilmez; (V16) Down'da `GRANT/**/DELETE …` -- ayirici blok yorumunu bosluksuz siliyordu
  # (`grantdelete`; sql_lex'te duzeltildi); (G4) Up'ta `GRANT DELETE … TO PUBLIC` -- Down'un REVOKE'u
  # yalniz tappa_app'inkini alir, PUBLIC'inki (her rolun) politikasiz tabloda kalir. Muafiyet artik
  # BUTUN dosyanin su sekilde olmasini da sart kosar (r5b_00058_shape; biri tutmazsa FAIL):
  #   - dosyanin `$$ … $$` govdeleri (ayiricidan gecmis, kucuk harf) TAM OLARAK sevk edilen uc
  #     govdedir (on kosul DO'su, tetikleyici fonksiyonu, son kosul DO'su -- sha256'leri asagida):
  #     PL/pgSQL bir GRANT'i statik de kosabilir, yani "govdede EXECUTE yok" yetmez; govde bir
  #     beyaz liste, bir yasak listesi degil. 00058 deploy olunca degismez (§3), pin de degismez;
  #   - adlandirilmis dolar tirnagi ($x$) ve ters bolu (E'…\'…') yok -- ayirici ikisini T137'den beri
  #     dogru ayirir, ama bu sekil onlari yine istemez;
  #   - Down'da hic `$$`, `do` ya da `execute` yok;
  #   - govdelerin DISINDA `execute` yalniz tetikleyicinin `execute function
  #     public.webhook_delivery_settle()`'idir ve her `grant` sozcugu bir ust duzey ifadenin ilk
  #     sozcugudur (bir dize sabitinde ya da baska bir ifadenin icinde GRANT yok) -- `DO '…'`
  #     (dolar tirnaksiz, dize govdeli DO) bu iki kurala takilir;
  #   - hicbir GRANT'in (Up ya da Down) alicisi `public` degildir.
  # Bu bir SOZCUK kapisidir; ARKADAKI KAPI veritabanidir:
  # internal/db TestWebhookRetention00058And00059_DownGiveBackAndUpTakesItAgain Down'dan sonra
  # gecerli yetkiyi has_table_privilege ile -- PUBLIC ve rol uyeligi dahil -- okur.
  # Kirmizi vakalar: cmd/tappa/scripttest TestRedline_R5bWaiverIsThe00058DownAlone.
  down_stmts=$(sql_statements_down "$f")
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: politika SILINIYOR → ${s:0:110}"$'\n'
  done < <(grep -E '^drop policy' <<<"$stmts" || true)
  r5b_drop='drop policy webhook_deliveries_delete_settled on public.webhook_deliveries'
  r5b_before="set local lock_timeout = '1s'"$'\n''revoke delete on public.webhook_deliveries from tappa_app'
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    if [[ $(basename "$f") == 00058_keep_the_deliveries_ninety_days.sql ]] \
       && [[ $s == "$r5b_drop" ]] \
       && [[ $(grep -cxF "$s" <<<"$down_stmts") == 1 ]] \
       && [[ $(awk -v d="$s" '$0 == d { exit } { print }' <<<"$down_stmts") == "$r5b_before" ]] \
       && ! grep -qE '^grant ' <<<"$down_stmts" \
       && r5b_00058_shape "$f" "$stmts" "$down_stmts"; then
      policy_waivers+="$f: KISITLAYICI politika Down'da siliniyor, DELETE once geri aliniyor → ${s:0:110}"$'\n'
      continue
    fi
    rls_mutations+="$f: politika SILINIYOR → ${s:0:110}"$'\n'
  done < <(grep -E '^drop policy' <<<"$down_stmts" || true)

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

  # --- R5b / T137: PUBLIC, ifadenin icindeki GRANT, govdedeki DDL, okunamayan dize ----------
  # Yukaridaki her kural UST DUZEY bir ifadenin metnini `^grant`/`^alter` ile okur. T137 o
  # gorus alaninin disinda kalan dort sinifi olctu (gecici bir 00099, Up'ta tek ifade, exit 0):
  # bir DO govdesinin EXECUTE'u, PUBLIC'e verilen yetki, ayiricinin yanlis okudugu `E'…'` (sql_lex'te
  # duzeltildi) ve -- bu turda olculdu -- `CREATE SCHEMA zz GRANT DELETE ON public.employees TO
  # tappa_app` (PostgreSQL bir sema ogesi olarak BASKA bir semanin tablosuna GRANT kabul eder;
  # has_table_privilege -> t). Kirmizi ve yesil vakalar: cmd/tappa/scripttest/redline_t137_test.go.
  #
  # (1) PUBLIC'e her GRANT. PUBLIC her roldur: tappa_app, tappa_operator, ileride yaratilacak her
  # rol. Append-only kurali bunu tablo bazinda zaten FAIL ediyordu (`GRANT DELETE ON
  # public.transactions TO PUBLIC` -> exit 1); genel kural yetkiden ve nesneden BAGIMSIZDIR,
  # cunku okuma ve EXECUTE de sinirdan gecer (olculdu, 2026-10-19): `GRANT SELECT ON employees TO
  # PUBLIC` -> tappa_operator SELECT=t (operator `app.tenant_id`'yi kendisi kurabilir ve op_*
  # disindan, iz birakmadan okur -- ADR 0021); `GRANT EXECUTE ON FUNCTION public.op_…() TO PUBLIC`
  # -> tappa_app EXECUTE=t (her op_* fonksiyonunun `REVOKE ALL … FROM PUBLIC`'i tam bunun icin
  # var). Bedel olculdu: 59 migration'da `TO PUBLIC` 0 isabet; `REVOKE … FROM PUBLIC` (40+ satir)
  # eslesmez, cunku ifade `revoke` ile baslar. `"public"` tirnakli yazimi da PUBLIC'tir (olculdu)
  # ve normalizasyon tirnagi zaten bosluga cevirir; `GROUP public` ayni sey.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    roles=${s##* to }; roles=${roles%% with *}; roles=${roles%% granted by *}
    [[ " ${roles//[!a-z0-9_]/ } " == *" public "* ]] || continue
    rls_mutations+="$f: PUBLIC'e yetki (her rol -- tappa_app, tappa_operator ve sonraki her rol) → ${s:0:110}"$'\n'
  done < <(grep -E '^grant .* to ' <<<"$stmts"$'\n'"$down_stmts" || true)

  scan=$(sql_scan "$f")

  # (2) BIR IFADENIN ICINDEKI GRANT. `^grant` kurallari yalniz ifadenin ILK sozcugune bakar; bir
  # sema ogesi olarak yazilan GRANT (`CREATE SCHEMA … GRANT …`) hicbirine gorunmuyordu. Girdi
  # sql_scan'in `top` kodudur (dize ve govde yer tutucuda, yani bir yorum ya da dize sabiti
  # eslesmez). `WITH GRANT OPTION` / `REVOKE GRANT OPTION FOR` bir yetkinin parcasidir;
  # `ALTER DEFAULT PRIVILEGES … GRANT` zaten kendi kuraliyla FAIL'dir. Bedel: 0 isabet.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    t=${s// with grant option/}
    [[ $t =~ ^(grant|revoke|alter\ default\ privileges)\  ]] && continue
    [[ $t =~ (^|[^a-z0-9_$])grant([^a-z0-9_$]|$) ]] || continue
    rls_mutations+="$f: GRANT bir ifadenin ICINDE (ornegin CREATE SCHEMA … GRANT) — ^grant kurallari onu gormez → ${s:0:110}"$'\n'
  done < <(grep -E $'^top\t' <<<"$scan" | cut -f2- || true)

  # (3) GOVDEDEKI KOD: dinamik SQL ve statik DDL. Bir DO ya da fonksiyon govdesinde `EXECUTE`
  # metni calisma aninda kurar (G1: `DO $$ BEGIN EXECUTE 'GRANT DELETE ON public.transactions TO
  # tappa_app'; END $$` -> exit 0 idi), ve `GRANT`/`ALTER`/`DROP`/`CREATE`/`TRUNCATE`/`REVOKE`/
  # `REASSIGN` govdede STATIK de kosar; ic ice `DO '…'`/`DO $x$…$x$` (olculdu: DO icinde DO'nun
  # EXECUTE'u yetkiyi verir) govdenin govdesidir. Ifadenin metni bir grep icin ya yoktur ya da bir
  # dizenin icindedir, yani karar sozcukte: sql_scan govdenin KODUNU verir (yorum ve dize atilmis)
  # ve bu sozcuklerden biri geciyorsa dosya ya R5B_BODY_DDL'de dosya adi + sha256 ile kayitlidir
  # ya da FAIL'dir. Pin govdelerin tamaminin ozetidir: kayitli bir dosyaya eklenen her govde ya da
  # degisen her kod satiri onu bozar. Bedel olculdu: 59 migration'da 9 dosya, hepsi statik `ALTER
  # TABLE … ADD CONSTRAINT` (liste ve gerekce R5B_BODY_DDL'de); `EXECUTE` kodu 0.
  # Bu bir SOZCUK kapisidir; ARKADAKI KAPI veritabanidir (TestRLS_AppCannotMutateTransactions,
  # TestRLS_AppCannotMutatePolicyVersions, TestAppendOnly_AppHasNoTruncatePrivilege ve yetki
  # matrisi testleri gercek yetkiyi `tappa_app` olarak olcer).
  bodies=$(grep -E $'^body\t' <<<"$scan" || true)
  hits=$(cut -f3- <<<"$bodies" \
           | grep -oE "(^|[^a-z0-9_\$])(execute|grant|revoke|alter|drop|create|truncate|reassign)([^a-z0-9_\$]|\$)|(^|[^a-z0-9_\$])do ('s'|[\$]s[\$]|language )" \
           | sed -E 's/^[^a-z]*//; s/[^a-z].*$//' | sort | uniq -c | awk '{ printf "%s%s×%s", (NR > 1 ? ", " : ""), $2, $1 }' || true)
  if [[ -n $hits ]]; then
    pin=$(awk -v n="$(basename "$f")" '$1 == n && $2 ~ /^[0-9a-f]+$/ && length($2) == 64 { print $2 }' <<<"$R5B_BODY_DDL")
    # shellcheck disable=SC2086 # SHA256_TOOL bilerek bolunur
    got=$([[ -n $SHA256_TOOL ]] && grep -E $'^pin\t' <<<"$scan" | $SHA256_TOOL | cut -c1-64)
    if [[ -z $pin ]]; then
      rls_mutations+="$f: GOVDEDE dinamik SQL / DDL kodu ($hits) yeni bir migration'da — inceleme + R5B_BODY_DDL'ye (dosya + sha256) ekleme gerekir"$'\n'
    elif [[ -z $got || $got != "$pin" ]]; then
      rls_mutations+="$f: GOVDEDE DDL kodu ($hits) ve govdeler R5B_BODY_DDL'deki pinle AYNI DEGIL (sha256 ${got:-araci yok}) — sevk edilmis bir migration degismez"$'\n'
    else
      body_waivers+="$f: govdede statik DDL ($hits) — R5B_BODY_DDL'de, govdeler sha256 ile pinli"$'\n'
    fi
  fi

  # (4) OKUNAMAYAN GOVDE VE OKUNAMAYAN DIZE. Bir DO/fonksiyon govdesi `E'…'` ya da `U&'…'` ise
  # kacislari bir harf yazar (`E'\x45XECUTE'`, `U&'\0045XECUTE'`): metni kod olarak okunamaz, yani
  # beyaz listeye de giremez. Ve `standard_conforming_strings`/`backslash_quote` gecen bir migration
  # duz `'…'` dizesinin ayirimini degistirir (kapali iken `\'` dizeyi kapatmaz) -- sql_lex bunu
  # modellemez. Ikisinin de bedeli olculdu: 0 isabet.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: KACISLI dize GOVDE (E'…'/U&'…' bir DO/fonksiyon govdesi) — kod olarak okunamaz → ${s:0:110}"$'\n'
  done < <(grep -E $'^body\tescaped\t' <<<"$bodies" | cut -f3- || true)
  if grep -qE 'standard_conforming_strings|backslash_quote|client_encoding' <<<"$(sql_lex <"$f" | tr '[:upper:]' '[:lower:]')"; then
    rls_mutations+="$f: standard_conforming_strings/backslash_quote/client_encoding — dize ayirimini degistirir, sql_lex onu modellemez"$'\n'
  fi
  # (5) AYAR ADI ADIYLA YAZILMALI (fix1). Yukaridaki kural adi metinde arar; `set_config('standard_' ||
  # 'conforming_strings', 'off', true)` adi hicbir yerde yazmadan ayni seyi yapar (olculdu: ardindan
  # gelen `'x\''` duz dizesi PostgreSQL'de kapanir, ayiricida kapanmaz ve arkasindaki GRANT dizenin
  # icinde kalir -- exit 0, has_table_privilege t). Uc kapi, ucu de sql_scan'in kodundan (ust duzey ve
  # her govde): (a) `set_config`in ilk argumani TEK bir duz dize sabiti olmali VE R5B_SET_CONFIG'de
  # olmali -- sevk edilen agacta kurulan yedi ad (olculdu: 59 migration'da 9 cagri, yedi ad); (b)
  # `pg_settings` (UPDATE'i set_config'tir ve adi bir WHERE dizesinden okur) ve `SET NAMES`
  # (client_encoding'in esanlamlisi) kodda gecemez; (c) `U&"…"` tanimlayicisi gecemez -- kacislari
  # bir ayar adini (`SET U&"standard\005fconforming_strings"`) metinde gorunmeden yazar. SET / SET
  # LOCAL / ALTER … SET / RESET / fonksiyonun SET cumlesi ayar adini bir TANIMLAYICI olarak alir:
  # birlestirilemez, yani yukaridaki ad kurali onlari zaten okur. Bedel olculdu: 0 isabet.
  while IFS=$'\t' read -r _ name; do
    [[ -n $name ]] || continue
    if [[ $name == "!" ]]; then
      rls_mutations+="$f: set_config — ayar adi TEK bir duz dize sabiti degil (birlestirme, degisken, E/dolar dizesi): hangi ayari kurdugu okunamaz"$'\n'
    elif [[ " $R5B_SET_CONFIG " != *" $name "* ]]; then
      rls_mutations+="$f: set_config — '$name' R5B_SET_CONFIG listesinde degil (inceleme + listeye ekleme gerekir)"$'\n'
    fi
  done < <(grep -E $'^setcfg\t' <<<"$scan" || true)
  if grep -qE $'^qsettings\t' <<<"$scan" ||
    grep -E $'^(top|body\t[a-z]+)\t' <<<"$scan" | grep -qE '(^|[^a-z0-9_$])(pg_settings|set (local |session )?names)([^a-z0-9_$]|$)'; then
    rls_mutations+="$f: pg_settings / SET NAMES kodda — bir ayari adini yazmadan degistirir"$'\n'
  fi
  if grep -qE $'^uident\t' <<<"$scan"; then
    rls_mutations+="$f: U&\"…\" tanimlayicisi — kacislari bir adi (ayar, rol, tablo) metinde gorunmeden yazar"$'\n'
  fi
  # (6) KACISLI DIZENIN DEVAMI (fix1). PostgreSQL bir dizeden sonra YALNIZ bosluk/`--` yorumu ve en az
  # bir satir sonu gelip yeni bir `'` aciliyorsa AYNI dizeyi surdurur, AYNI kipte: `E'a'` + yeni satir
  # + `'\''` tek bir E dizesidir ve `\'` onu kapatmaz. Iki ayirici bunu artik modelliyor (sql_lex,
  # sql_scan; duz dizenin devami sevk edilen agacta 23 kez, hepsi COMMENT ON metni); ama E/U& dizesinin
  # devami 0 kez ve tek isi okuyani yaniltmak -- FAIL.
  if grep -qE $'^econt\t' <<<"$scan"; then
    rls_mutations+="$f: KACISLI dizenin DEVAMI (E'…'/U&'…' bir satir sonundan sonra yeni bir '…' ile surer, kacislar devam eden parcada da gecerli)"$'\n'
  fi
  # goose'un ENVSUB'u `${VAR}`i calisma aninda ortamdan yerlestirir: metin dosyada YAZMAZ (`\i`
  # sinifi). Annotasyon bir yorum satiridir, yani ham dosyadan okunur. Bedel: 0 isabet.
  while IFS= read -r s; do
    [[ -n $s ]] || continue
    rls_mutations+="$f: goose ENVSUB — ifade metni calisma aninda ortamdan gelir → ${s:0:110}"$'\n'
  done < <(grep -niE '^[[:space:]]*--[[:space:]]*[+]goose[[:space:]]+envsub' "$f" || true)
done
report FAIL R5 "Migration'da tablo besligi eksik (tenant_id + indeks + ENABLE/FORCE RLS + politika + GRANT)" "${missing_rls%$'\n'}"

report FAIL R5b "Migration §4.5/§4.3/§4.6'yi SOKUYOR (RLS kapatma · politika silme veya totolojik yuklem · BYPASSRLS-SUPERUSER rol/grup · rol uyeligi · varsayilan yetki · view acma veya definer'a cevirme · KURAL (RULE) · tablo sahipligi · tetikleyici · append-only UPDATE/DELETE · sema capinda UPDATE/DELETE · GRANT ALL · PUBLIC'e yetki · ifadenin icinde GRANT · govdede dinamik SQL/DDL · okunamayan dize)" "${rls_mutations%$'\n'}"

report WARN R5 "Tenant kapsamindan MUAF birakilmis tablo(lar) — muafiyet sessiz kalmamali" "${waiver_report%$'\n'}"

report WARN R5b "Down'da silinen KISITLAYICI politika (fiil ayni Down'da once geri aliniyor) — muafiyet sessiz kalmamali" "${policy_waivers%$'\n'}"

report WARN R5b "Govdesinde statik DDL olan, R5B_BODY_DDL'de sha256 ile pinli migration(lar) — muafiyet sessiz kalmamali" "${body_waivers%$'\n'}"

report WARN R5 "Tablo yaratimi R5'in gorus alani disinda — elle denetle" "${unverified%$'\n'}"

# _test.go MUAFIYETI (DAR — yalniz bu tarama, yalniz test dosyalari): bu kural
# URETIM kodu icindir. RLS izolasyon testi (internal/db/rls_test.go) vaka 5'te
# tappa_owner (migrate URL) havuzunu MESRU kullanir — superuser'in bile append-only
# trigger'i asamadigini kanitlamak icin (ADR 0002). Uretim kodundaki migrate-URL
# kullanimi hala yakalanir. NOT: bu muafiyet R5'in yalniz bu (migrate-URL) taramasi
# icindir; migration-besligi (db/migrations) ve SET-LOCAL kontrolu etkilenmez.
report FAIL R5 "Uygulama migration rolu ile baglaniyor — RLS etkisiz kalir" \
  "$(scan_in SRC_CODE -e 'DATABASE_MIGRATE_URL' --glob '!cmd/migrate/*' --glob '!internal/config/*' --glob '!**/*_test.go' || true)"

# BOS DEGER MUAF -- DAR (M11 API-2, 00048): `SET app.tenant_id = ''` bir baglantiya TENANT
# BIRAKMAZ -- birakabilecegi tek sey "baglam yok"tur, yani RLS'li her tablo bos okunur
# (fail-closed). 00048'in feed_open_counter() tetikleyicisi bu bicimi bir FONKSIYON
# OZNITELIGI olarak tasir: PostgreSQL, ozniteligi ayni degiskeni adlandiran bir
# fonksiyonun icinde yapilan SET LOCAL'i fonksiyon donunce geri alir (olculdu) --
# havuzu kirleten bir SET degil, tam tersi. Muafiyet YALNIZ db/migrations altindaki ve
# satirin TAMAMI bu bicim olan satira verilir (API-2 guvenlik denetimi: ilk hali satir
# SONUNDAKI `''`'a bakiyordu, ayni satirda once bir tenant degeri yazan duz SET
# gizlenebiliyordu -- `SET app.tenant_id = 'x'; SET app.tenant_id = ''` muaf sayiliyordu).
# Grep'in `-v`'si satirin tamamini eslestirir: `yol:satir:` oneki + istege bagli bosluk +
# tek ifade + sonda yalniz bosluk. Bir tenant degeri yazan duz SET yine WARN.
report WARN R5 "SET LOCAL degil duz SET — havuzdaki baglantiyi kirletir" \
  "$(scan -e "SET +app\.tenant_id" | grep -viE "SET +LOCAL" | grep -vE "^db/migrations/[^:]+:[0-9]+:[[:space:]]*SET app\.tenant_id = ''[[:space:]]*\$" || true)"

# R5c -- TEK SAYAC KUMESI ISARETI YALNIZ TETIKLEYICININDIR (M11 API-2 3. tur, ADR 0028
# "API-2 uygulama notu"). 00049'un punch_feed/holding_feed'i islemin ILK olayinin sayac
# kumesini islem-yerel `tappa.feed_counters` GUC'una yazar; kume disindaki ikinci olay
# TF001 ile reddedilir. GUC'u CAGIRAN da yazabilir: isareti onceden yazan bir islem kontrolu
# atlatir ve iki ters yonlu holding dokunusu yeniden kilitlenir -- KURBAN atlatan degil,
# BASKA tenant ciftlerinin holding dokunuslaridir (40P01; checkin 40P01'i yeniden denemez,
# NFC kaydi yazilmaz -- iki mercek pgbench ile olctu: 29'da 18, 155'te 88). Bu yuzden adi
# migration disinda HICBIR YERDE gecmez: urun kodu, betik, dagitim, test. (docs/ taranmaz;
# ADR onu anlatir.) Pin: cmd/tappa/scripttest TestRedline_TheCounterSetMarkIsTheTriggersAlone.
# DESEN BICIME DEGIL ADA BAKAR (API-2 4. tur, ucuncu goz olctu): PostgreSQL ayar adini harf
# ayirmadan okur ve tirnakli/boslukli yazilisi da kabul eder -- `TAPPA.FEED_COUNTERS`,
# `Tappa.Feed_Counters`, `"tappa"."feed_counters"`, `tappa . feed_counters` ilk desenden
# kaciyordu. Kapsam SRC + kok Makefile (Makefile'in tarifleri psql koşar ve SRC disindaydi).
# ⚠️ KANIT DEGILDIR: ad PARCALANARAK kurulursa (Go'da `"tappa." + "feed_counters"`, SQL'de
# format()/concat, bir degiskenden okunan ad) hicbir satir-grep'i onu gormez -- bu kuralin pin
# testi kendisi adi tam bu yolla yazar. Kuralin yakaladigi, adin DUZ yazildigi yollardir.
R5C_SCOPE=("${SRC[@]}" Makefile)
report FAIL R5c "tappa.feed_counters migration disinda — TF001'in tek sayac kumesi kontrolunu atlatma yolu" \
  "$(scan_in R5C_SCOPE -i -e 'tappa\s*"?\s*\.\s*"?\s*feed_counters' --glob '!db/migrations/**' || true)"

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
#
# --- M11 API-3 EKLERI (ADR 0028 §3) -- ONCE AGACTA SAYILDI ------------------------------
# Kokler `api_?key` (API anahtari), `bearer` (Authorization semasi) ve `signing_?key`
# (webhook uc noktasinin imza anahtari, API-9) ve bir DAR kalip: log/fmt cagrisinin icinde
# `Header.Get("Authorization")` ya da `Header["Authorization"]` -- baslik DEGERINI arayan
# yazim, kelimeyi degil. Yalin `kek`/`signature`/`authorization` kokleri SECILMEDI: ADR'nin
# 3. tur olcumuyle bugunku agacta 35 satiri kizartirdi ve "kelimeyi degil degeri hedefle"
# ilkesine aykiridir. OLCULDU (2026-10-14, API-3'un kendi agaci, ayni SRC/GEN_EXCLUDE, -U -i):
# yeni dort alternatif 0 isabet. Bu yuzden API-3'un kodu log/hata metinlerinde `apikey:`
# DEGIL `api key:` yazar (Go'nun paket-oneki adeti burada bilerek bozuldu).
# AWK TARAFI: muafiyet karari (r7_select) R7_TRIGGERS'i awk ERE'si olarak okur; baslik
# kalibinin `\(`/`\[` kacislari awk -v'den gecerken bozulurdu, o yuzden awk'a onun yerine
# daha GENIS `authorization` kelimesi verilir (R7_AWK_TRIGGERS) -- muafiyet yolunda bir
# bolgeyi affetmeyi ZORLASTIRIR, kolaylastirmaz.
R7_TRIGGERS='token|cmac|aes_?key|secret|invite_?code|code[_a-z]*hash|password|"code"[[:space:]]*,|api_?key|bearer|signing_?key'
# 2. TUR (2026-10-15, iki mercek): kokler DEGERIN KENDISINI tasiyan iki adi gormuyordu --
# `created.Key.RevealToTheOwnerOnce()` (ham anahtarin TEK cikis kapisi) ve panelin `v.NewKey`
# alani -- ve baslik kalibi `Header.Values("Authorization")`'i. Uc alternatif eklendi
# (R7_VALUE_NAMES, R7_AUTH_HEADER); ONCE AGACTA SAYILDI: log/fmt cagrisi icinde 0 isabet.
# `\b` awk ERE'sinde yoktur (BSD awk onu geri silme sayar), o yuzden awk'a yine daha GENIS
# kelimeler gider (`revealtotheowneronce`, `new_?key`).
R7_VALUE_NAMES='RevealToTheOwnerOnce|\bnew_?key\b'
R7_AUTH_HEADER='Header\.Get\("Authorization"\)|Header\["Authorization"\]|Header\.Values\("Authorization"\)'
R7_AWK_TRIGGERS="$R7_TRIGGERS|authorization|revealtotheowneronce|new_?key"
# Pencere: bir seviye dengeli parantez, tekrar edebilir -> `f(a(), b(), TETIK`.
R7_PATTERN="(slog|log|fmt)\\.[A-Za-z]+\\((?:[^()]|\\([^()]*\\))*($R7_TRIGGERS|$R7_VALUE_NAMES|$R7_AUTH_HEADER)"

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
  awk -v mode="$1" -v waivers="$R7_WAIVERS" -v trig="$R7_AWK_TRIGGERS" '
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
# M11 API-3 (ADR 0028 §3): API-1'in yeni ad alanlari -- `first_name`, `last_name` ve holding
# izdusumunun `employee_name`'i -- hem Go alani (`.FirstName`, `.LastName`, `.EmployeeName`)
# hem yapilandirilmis log ANAHTARI ya da degisken adi (`first_name`, `employeeName`) olarak.
# ONCE AGACTA SAYILDI (2026-10-14): 0 isabet, -i ile de 0.
report FAIL R7b "Kisisel veri (ad/adres) log cagrisinda — §4.7/§7: surec log'unun saklama suresi ve tenant siniri yoktur" \
  "$(scan -U -e '(slog|log|fmt)\.[A-Za-z]+\((?:[^()]|\([^()]*\))*(r\.(Post)?FormValue\(|\.FullName\b|\.Email\b|\bposted\.|\b(f|person|out|p|emp|employee|staff|subject)\.Name\b|\.(FirstName|LastName|EmployeeName)\b|\b(first|last|employee)_?[Nn]ame\b)' || true)"

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

# --- B1: urun adi `Tappa` KULLANICININ OKUDUGU metinde (T73, CLAUDE.md §9) ----
#
# KOD NEDEN B1, R DEGIL: R1-R7 §4'un kirmizi cizgileridir ve tappa-security-auditor
# ile ayni numaralari tasir; marka §4 degil §9'dur. N1 (Node, §1) ile ayni sinif:
# kirmizi cizgi olmayan ama mekanik olarak tutulan bir proje kurali.
#
# NE ARANIR: buyuk T ile `Tappa`, kelime olarak (oncesinde ve sonrasinda harf, rakam,
# `_` yok; oncesinde `.` yok). Kullanici-yuzu marka Taptime; ic kod adi tappa BILINCLI
# olarak degismedi (0738d16). Iki kez ELLE dogrulandi (rebrand ve landing turlarinda);
# bu kural o elle taramayi mekaniklestirir.
#
# KAPSAM — kullaniciya giden yuzeyler (B1_SRC):
#   web/templates   .templ metni ve Go string literal'leri (view dosyalari: landingview.go …)
#   web/static      .html, .js ve .css (gomulu, /static/ altinda servis edilir; fonts/README.md dahil)
#   internal        Go string literal'leri — handler cumleleri, e-posta (internal/mail),
#                   yasal metinler (internal/domain/legal), sandbox openapi.json ve
#                   API'nin openapi.yaml'i (ikisi de servis edilir)
#   docs/api        entegrasyon rehberi, iki dil (GET /v1/guide.md — sitede yayinlaniyor)
#   android/app/src/main   Wording.kt, strings.xml ve tum kaynak
#
# 🔴 internal NEDEN BUTUNUYLE, handler ile sinirli DEGIL — OLCULDU (2026-10-21): kucuk
# harf `tappa` 5.396 kez geciyor, buyuk `Tappa` ise yorumlar ve tanimlayicilar
# disinda, cmd+internal+web'in uretilmemis ve test olmayan Go string literal'lerinin
# TAMAMINDA tek bir yerde: internal/domain/tenant/rulewriter.go ErrNotYours'un cumlesi
# (handler onu "not-yours" koduna cevirir, metni ekrana gitmez). Bir domain hatasinin
# metninin ekrana gidip gitmedigi mekanik olarak bilinemez (signup/tenant hatalari
# bazi yollarda cumle olarak donuyor); tek bulguyu Taptime'a cevirmek, gorunmeyeni
# varsaymaktan ucuzdu. cmd/ KAPSAM DISI: CLI ciktisi Taptime'in kendi operatorune gider.
#
# MUAF (yapisal — muafiyet tablosu YOK, gerekmedi):
#   - Go/JS/Kotlin/CSS kaynakta string literal DISINDAKI her sey: yorumlar (`//`,
#     `/* */`), tanimlayicilar (ComparisonRow.Tappa), import/modul yollari, struct alanlari.
#   - .templ dosyasinin GO BOLGELERI ayni kuralla okunur: govdenin disi (package,
#     import, bildirimler), `{` ile biten `if/for/switch/else` ve `:` ile biten
#     `case/default` satirlari (templ in sartina YAKIN bir bicim kurali; switch
#     baglami izlenmez — SINIRLAR 5), `{ ... }` ifadeleri ve
#     `@cagri(...)` argumanlari — yalniz string literal'leri taranir, `Tappa: "x"` ya da
#     `{ row.Tappa }` gibi tanimlayicilar degil. templ yorumu YALNIZ satirin ilk
#     bosluk-olmayan karakterinden baslayan `//` ya da `/* */`dir; metin dugumu icindeki
#     `//` ve `/* */` metindir (templ onlari ekrana basar).
#   - android/app/src/main altindaki XML'de `<!-- -->` (uygulamaya derlenir, kimseye
#     servis edilmez); muafiyet yola baglidir, baska bir .xml'de yorum metindir.
#   - harf duyarlidir: `tappa-green`, `tappa_app`, `TAPPA_*`, `github.com/atknatk/tappa`
#     hicbir zaman eslesmez.
#   - *_test.go, testdata/, *_templ.go, internal/store (uretilmis), web/static/vendor
#     (ucuncu parti), docs/ altinda docs/api DISINDA her sey (ic belgeler).
#
# 🔴 SERVIS EDILEN YUZEYDE YORUM YOKTUR (denetim, 2026-10-21 — olculdu): templ v0.3.833
# `<!-- -->`yu istemciye yazar (uretilen _templ.go'da WriteString icinde);
# docs/api/*.md `GET /v1/guide.md` ile HAM bayt doner; openapi.yaml ve sandbox
# openapi.json ham servis edilir. Bu yuzden .templ, .html, .md, .yaml, .yml, .json ve
# Android disindaki .xml'de
# `<!-- -->` ve ` #` METINDIR. Ilk hali onlari muaf tutuyordu.
#
# ⚠️ SINIRLAR, ACIKCA: (1) yalniz buyuk-T `Tappa` — kullanici metninde kucuk harf
# `tappa` ya da `TAPPA` yakalanmaz, cunku o yazimlar CSS token'i, rol ve env olarak
# her yerde mesru. (2) Sozcuksel bir tarayicidir, ayristirici degil: JS regex
# literal'i icindeki bir tirnak, ham bir Kotlin `"""` disindaki ic ice sablonlar,
# templ govdesinde satir ici `<script>`/`<style>` icindeki suslu parantez (bugun 0
# satir ici betik) ya da sutun 0 disinda kapanan bir templ govdesi lexer'i
# yaniltabilir — yalniz `Tappa` iceren dosyalar okunur, yani bedel o dosyalarla
# sinirli. (3) Uretilmis dosyalar (_templ.go) taranmaz; kaynaklari (.templ) taranir.
# (4) Bulgu satiri eslesmenin CEVRESINI basar (60 karakter once, 70 sonra), satirin
# basini degil. (5) templ lexer'i switch derinligini izlemez: `switch` DISINDA
# `case …:` ya da `default:` bicimli bir METIN satiri (templ onu ekrana basar,
# olculdu) Go sayilir ve yalniz string literal'leri taranir. Bugun agacta ornegi yok
# (380 case/default satirinin hepsi Go); izleme bilerek eklenmedi. (6) Ayni satirda
# `{` dan SONRA gelen metin (`templ X() { metin`, `} else { metin`,
# `@cagri(...) { metin`) taranmaz — `templ fmt` bu bicimi ayri satira boler ve
# `make check` fmt + `git diff --exit-code` kostugu icin bu bicim CI dan gecemez.
#
# AYNI KALIP: rg dosyalari ON-SECER (scan_in: cikis kodu okunur, eksik yol = tarama
# hatasi), lexer yalniz o dosyalari okur; awk hatasi SCAN_ERR'e yazilir — bos bir
# secim "temiz" okunmasin diye (R1'in r1_select'i ile ayni gerekce).
B1_SRC=(web/templates web/static internal docs/api android/app/src/main)
B1_GLOBS=(
  --glob '*.{go,templ,html,js,css,kt,xml,md,yaml,yml,json}'
  # Sonraki glob oncekini ezer (rg), bu yuzden GEN_EXCLUDE'un iki uretilmis kumesi
  # dahil-etme globundan SONRA yeniden yazilir.
  --glob '!*_templ.go' --glob '!internal/store/*.go'
  --glob '!*_test.go' --glob '!**/testdata/**' --glob '!web/static/vendor/**'
)

# b1_lex <dosya...> — her dosyada kullanicinin okudugu metni cikarir ve `Tappa`
# tasiyan her parcayi `yol:satir: metin` olarak basar.
# (Bu blok tek tirnak icinde; yorumlarda kesme isareti KULLANILMAZ, tek tirnak
# karakteri \047 olarak yazilir.)
b1_lex() {
  awk '
    function hit(s) { return s ~ /(^|[^A-Za-z0-9_.])Tappa([^A-Za-z0-9_]|$)/ }
    # emit: yol:satir ve eslesmenin CEVRESI (oncesinden 60, sonrasindan 70 karakter) --
    # uzun bir satirda bulgu kesilen kuyrukta kalmasin.
    function emit(ln, s,    p, a, b) {
      gsub(/[\t ]+/, " ", s)
      p = match(s, /(^|[^A-Za-z0-9_.])Tappa([^A-Za-z0-9_]|$)/) ? RSTART : 1
      a = p - 60; if (a < 1) a = 1
      b = p + 70
      print FILENAME ":" ln ": " (a > 1 ? "..." : "") substr(s, a, b - a) (length(s) >= b ? "..." : "")
    }
    # lexc: C-benzeri kaynagin BIR satiri (Go, JS, Kotlin, CSS; ve .templ dosyasinin Go
    # bolgeleri). YALNIZ string literal icerigi okunur; durum (st, buf, at) satirlar
    # arasinda tasinir.
    function lexc(line, lang,    n, i, c, c2) {
      n = length(line); i = 1
      while (i <= n) {
        c = substr(line, i, 1); c2 = substr(line, i, 2)
        if (st == "code") {
          if (c2 == "//" && lang != "css") break
          if (c2 == "/*") { st = "block"; i += 2; continue }
          if (lang == "kt" && substr(line, i, 3) == "\"\"\"") { st = "tq"; buf = ""; at = FNR; i += 3; continue }
          if (c == "\"" || c == "\047" || (c == "`" && lang != "kt")) { st = c; buf = ""; at = FNR; i++; continue }
          i++; continue
        }
        if (st == "block") { if (c2 == "*/") { st = "code"; i += 2 } else i++; continue }
        if (st == "tq") {
          if (substr(line, i, 3) == "\"\"\"") { if (hit(buf)) emit(at, buf); st = "code"; i += 3; continue }
          buf = buf c; i++; continue
        }
        if (st == "`") {
          if (c == "`") { if (hit(buf)) emit(at, buf); st = "code"; i++; continue }
          buf = buf c; i++; continue
        }
        if (c == "\\") { buf = buf substr(line, i, 2); i += 2; continue }
        if (c == st) { if (hit(buf)) emit(at, buf); st = "code"; i++; continue }
        buf = buf c; i++
      }
      # Tek satirlik bir literal satir sonunda kapanmamissa (JS, bozuk kaynak) orada biter.
      if (st == "\"" || st == "\047") { if (hit(buf)) emit(at, buf); st = "code" }
      else if (st == "`" || st == "tq") buf = buf " "
    }
    # gostmt: satir bir Go kontrol satiri BICIMINDE mi (templ in sartina yakin, ama
    # switch baglamina bakmaz — basliktaki SINIRLAR 5). Ilk sozcuk yetmez:
    # templ `for every business ...`, `default ...`, `case ...`, `else ...` ile baslayan
    # satiri METIN olarak ekrana basar (olculdu, templ v0.3.833 generate). Blok acan satir
    # (`if/for/switch`, `} else`, `} else if`) `{` ile, `case`/`default` `:` ile biter;
    # sondaki bosluk ve `//` yorumu tolere edilir.
    function gostmt(t,    u) {
      u = t; sub(/[ \t]+\/\/.*$/, "", u); sub(/[ \t]+$/, "", u)
      if (u ~ /^(if|for|switch)[ \t(]/ && u ~ /\{$/) return 1
      if (u ~ /^(\}[ \t]*)?else([ \t]+if[ \t(].*)?[ \t]*\{$/) return 1
      if (u ~ /^case[ \t].*:$/ || u ~ /^default[ \t]*:$/) return 1
      return 0
    }
    # lext: bir templ GOVDESININ satiri. Metin dugumu ve oznitelik degeri METINDIR
    # (`<!-- -->` dahil: templ onu istemciye yazar, olculdu); `{ ... }` ifadeleri ve
    # `@cagri(...)` Go dir -- orada yalniz string literal okunur, tanimlayicilar degil.
    # Yorum yalniz satirin ILK bosluk-olmayan karakterinden baslayan `//` ya da `/* */`.
    function lext(line,    n, i, j, c, out, t, rest) {
      out = ""
      if (tst == "text" && est == "") {
        t = line; sub(/^[ \t]+/, "", t)
        if (substr(t, 1, 2) == "//") return
        if (substr(t, 1, 2) == "/*") { tst = "tblock"; line = substr(t, 3) }
        else if (gostmt(t)) { lexc(line, "go"); return }
      }
      n = length(line); i = 1
      while (i <= n) {
        c = substr(line, i, 1)
        if (tst == "tblock") { if (substr(line, i, 2) == "*/") { tst = "text"; i += 2 } else i++; continue }
        if (tst == "expr") {
          if (est != "") {
            if (est != "`" && c == "\\") { buf = buf substr(line, i, 2); i += 2; continue }
            if (c == est) { if (hit(buf)) emit(at, buf); est = ""; i++; continue }
            buf = buf c; i++; continue
          }
          if (c == "\"" || c == "\047" || c == "`") { est = c; buf = ""; at = FNR; i++; continue }
          if (substr(line, i, 2) == "//") break
          if (c == "{" || c == "(") depth++
          else if (c == "}" || c == ")") {
            depth--
            if (depth == 0) {
              tst = "text"
              # @cagri(...) { : cocuk blogu acar, ifade degil.
              rest = substr(line, i + 1)
              if (fromcall && rest ~ /^[ \t]*\{[ \t]*$/) { fromcall = 0; break }
              fromcall = 0
            }
          }
          i++; continue
        }
        if (c == "{") { tst = "expr"; depth = 1; i++; continue }
        if (c == "@" && substr(line, i + 1, 1) ~ /[A-Za-z_]/ && (i == 1 || substr(line, i - 1, 1) ~ /[ \t>]/)) {
          j = i + 1
          while (j <= n && substr(line, j, 1) ~ /[A-Za-z0-9_.]/) j++
          if (substr(line, j, 1) == "(") { tst = "expr"; depth = 1; fromcall = 1; i = j + 1; continue }
          i = j; continue
        }
        out = out c; i++
      }
      if (est == "\"" || est == "\047") { if (hit(buf)) emit(at, buf); est = "" }
      else if (est == "`") buf = buf " "
      if (hit(out)) emit(FNR, out)
    }
    FNR == 1 {
      st = "code"; buf = ""; at = 0; tst = "text"; est = ""; depth = 0; fromcall = 0; body = 0
      ext = FILENAME; sub(/^.*\./, "", ext)
    }
    ext == "go" || ext == "js" || ext == "kt" || ext == "css" { lexc($0, ext); next }
    # .templ: govdenin disi (package, import, Go bildirimleri) Go dur; `templ X(...) {`
    # ile sutun 0 daki `}` arasi govdedir.
    ext == "templ" {
      if (!body) {
        lexc($0, "go")
        if (st == "code" && $0 ~ /^templ[ \t]/ && $0 ~ /\{[ \t]*$/) { body = 1; tst = "text"; est = ""; depth = 0 }
        next
      }
      if (st != "code") { lexc($0, "go"); next }
      if (tst == "text" && est == "" && $0 ~ /^\}[ \t]*$/) { body = 0; next }
      lext($0)
      next
    }
    # Android XML: `<!-- -->` yorumdur (servis edilmez, uygulamaya derlenir). Muafiyet YOLA
    # bagli: B1 kapsamindaki baska bir .xml servis edilen ham bayttir (asagidaki kol).
    ext == "xml" && FILENAME ~ /^android\/app\/src\/main\// {
      line = $0; out = ""; n = length(line); i = 1
      while (i <= n) {
        if (st == "html") { if (substr(line, i, 3) == "-->") { st = "code"; i += 3 } else i++; continue }
        if (substr(line, i, 4) == "<!--") { st = "html"; i += 4; continue }
        out = out substr(line, i, 1); i++
      }
      if (hit(out)) emit(FNR, out)
      next
    }
    # Servis edilen ham baytlar (.html, .md, .yaml, .yml, .json): yorum YOK, her sey metindir.
    { if (hit($0)) emit(FNR, $0) }' "$@"
}

b1_hits=""
b1_files=()
while IFS= read -r f; do
  [[ -n $f ]] && b1_files+=("$f")
done <<<"$(scan_in B1_SRC -l "${B1_GLOBS[@]}" -e 'Tappa')"
if [[ ${#b1_files[@]} -gt 0 ]]; then
  # awk dosya almazsa stdin okur; bos liste bu yuzden hic cagrilmaz. Cikis kodu
  # $(...) icinde kaybolurdu: `||` onu SCAN_ERR'e yazar.
  b1_hits=$(b1_lex "${b1_files[@]}") || {
    echo "${RED}ATLANDI${OFF}: B1 lexer hata verdi (awk) — TARAMA GUVENILIR DEGIL." >&2
    echo 2 >>"$SCAN_ERR"
  }
fi
report FAIL B1 "Urun adi 'Tappa' kullanicinin okudugu metinde — kullanici yuzu marka Taptime, tappa yalniz ic kod adi (CLAUDE.md §9)" \
  "$b1_hits"

# --- R7d: SIR DEGERI commit'lenebilir bir dosyada (M10 F0-5, Olay A-0) --------
#
# KOD NEDEN R7d, R8 DEGIL: R1-R7 bu dosyada agent `tappa-security-auditor`in R1-R7
# basliklariyla BIREBIR eslesiyor (R7 = "Sir sizintisi" ikisinde de), ve o ajanin
# R8'i "Karar sirasi uyumu" — plan belgelerinde "tappa-security-auditor R6/R8 temiz"
# diye aniliyor (docs altinda 9 kez, olculdu). Burada R8 demek "R8 temiz" cumlesini iki anlamli
# yapardi. Sir DEGERI, R7 ailesinin (R7 log · R7b kisisel veri · R7c GPS) dorduncu
# uyesidir.
#
# 🔴 NEDEN AYRI KAPSAM. R1-R7 SRC'yi (kaynak kod) tarar; A-0'in sizintisi — operator
# panel parolasinin DEGERI bir "rotate edilmeli" notunun icinde — docs/plan/state.md'deydi
# ve DORT satir, UC commit boyunca, depo PUBLIC iken hicbir kural oraya bakmadi.
# R7d'in kapsami bu yuzden `git ls-files --cached --others --exclude-standard`: bir
# `git add -A`'nin alacagi her dosya — docs/ ve .claude/ dahil, .gitignore'daki .env
# HARIC (o dosyanin degerleri hicbir tarama ciktisina girmemeli; zaten commit'lenemez).
# Izlenmeyen ama ignore EDILMEMIS dosyalar da taranir: "commit'lenmeden once" gorulen
# sizinti, "commit'lendikten sonra" gorulenden ucuzdur.
#
# DESENLER, TETIKLEYICILER VE MUAFIYET TABLOSU scripts/secretscan.sh'TE, TEK YERDE —
# scripts/git-hooks/pre-push ayni dosyayi source eder (push edilecek EKLENEN satirlar).
# Olcumler, siniflar ve sayilmis sinirlar orada yazili.
#
# 🔴 BULGU METNI BASILMAZ: cikti `yol:satir: [sinif]`. Eslesen satiri basmak sirri
# CI log'una — public depoda public — ikinci kez yayinlardi.
#
# ⚠️ `--no-config` BILINCLI: RIPGREP_CONFIG_PATH bir `--max-columns` tasirsa rg uzun
# satiri "[Omitted long line]" ile DEGISTIRIR. state.md'nin A-0 satiri binlerce
# karakterlik tek bir satirdi — yani o ayarla ag tam sizintinin ustunde korlesirdi.
# `--sort=path` ciktiyi kosudan kosuya AYNI sirada tutar (rg paralel siralamasi
# degisken; WARN listesi CI log'larinda karsilastirilabilir kalmali). Bedeli tek
# is parcacigi: bu agacta 0,08 sn (siralamasiz 0,02 sn), olculdu.
# ⚠️ Dosya listesi -z ile alinir: -z'siz git, ASCII disi yollari tirnakli sekizli
# kacisla basar ve rg o yolu bulamaz (exit 2). Silinmis-ama-izlenen dosyalar ve
# sembolik baglar atlanir (ilki okunamaz, ikincisi agaci disariya tasir).
# ⚠️ KAYIT AYIRACI `:` DEGIL US (0x1F) — 2. tur (N1): yolu `:` tasiyan bir dosya
# (`docs/plan/a:b.md`) `yol:satir:metin` ayristirmasinda SESSIZCE atlaniyordu. rg
# alanlari `--field-match-separator` ile ayirir; secretscan.sh ayrisamayan her
# kaydi HATA sayar (exit 2), rg'nin ikili dosya bildirimi disinda.
# ⚠️ Kutuphane yuklenemezse asagidaki blok HIC KOSMAZ: `set -u` altinda tanimsiz
# bir R7D_* degiskeni betigi exit 1 ile — "ihlal var" ile ayni sayiyla — dusururdu.
r7d_ok=1
if ! . scripts/secretscan.sh; then
  echo "${RED}ATLANDI${OFF}: scripts/secretscan.sh yuklenemedi — R7d KOSMADI." >&2
  echo 2 >>"$SCAN_ERR"
  r7d_ok=0
elif ! R7D_WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/tappa-r7d.XXXXXX"); then
  R7D_WORKDIR=""
  echo "${RED}ATLANDI${OFF}: R7d calisma dizini yaratilamadi (mktemp) — R7d KOSMADI." >&2
  echo 2 >>"$SCAN_ERR"
  r7d_ok=0
fi
if ! git ls-files -z --cached --others --exclude-standard >"$R7D_LIST"; then
  echo "${RED}ATLANDI${OFF}: R7d dosya listesi alinamadi (git ls-files) — TARAMA KOSMADI." >&2
  echo 2 >>"$SCAN_ERR"
fi
r7d_files=()
while IFS= read -r -d '' f; do
  [[ -f $f && ! -L $f ]] && r7d_files+=("$f")
done <"$R7D_LIST"
r7d_raw=""; r7d_fail=""; r7d_waived=""
if [[ $r7d_ok -eq 1 && ${#r7d_files[@]} -eq 0 ]]; then
  echo "${RED}ATLANDI${OFF}: R7d taranacak dosya bulamadi — bu bir 'temiz' sonucu DEGILDIR." >&2
  echo 2 >>"$SCAN_ERR"
elif [[ $r7d_ok -eq 1 ]]; then
  r7d_raw=$(rg --no-config --sort=path -n --no-heading -H --field-match-separator "$R7D_SEP" \
    -i -e "$R7D_PREFILTER" -- "${r7d_files[@]}")
  r7d_rc=$?
  if [[ $r7d_rc -gt 1 ]]; then
    echo "${RED}ATLANDI${OFF}: R7d ripgrep taramasi hata verdi (exit $r7d_rc) — TARAMA GUVENILIR DEGIL." >&2
    echo "$r7d_rc" >>"$SCAN_ERR"
  fi
  # r7d_select'in cikis kodu $(...) icinde kaybolurdu; `||` onu SCAN_ERR'e yazar.
  r7d_fail=$(r7d_select fail <<<"$r7d_raw") || {
    echo "${RED}ATLANDI${OFF}: R7d siniflandirmasi hata verdi (awk / ayrisamayan kayit / sha256) — TARAMA GUVENILIR DEGIL." >&2
    echo 2 >>"$SCAN_ERR"
  }
  r7d_waived=$(r7d_select waived <<<"$r7d_raw") || echo 2 >>"$SCAN_ERR"
fi
report FAIL R7d "Sir DEGERI commit'lenebilir dosyada (kimlikli URL · AWS anahtari · PEM ozel anahtar · bcrypt ozeti · saglayici belirteci · parola atamasi · A-0 sekli) — metin basilmaz, satiri yerelde ac. Gercekten zararsizsa (ya da muaf bir satir duzenlendiyse, muafiyeti SATIRA bagli oldugu icin duser): bash scripts/secretscan.sh --hash <dosya>:<satir> satirin hash'ini basar, R7D_WAIVERS tablosuna gerekcesiyle eklenir" \
  "$r7d_fail"
report WARN R7d "R7d muafiyeti kullanildi (yola VE satirin sha256'sina bagli, scripts/secretscan.sh) — muafiyet sessiz kalamaz" \
  "$r7d_waived"

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
