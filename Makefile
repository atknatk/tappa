.DEFAULT_GOAL := help
SHELL := /bin/bash
# NOT: .ONESHELL kullanilmaz — macOS'un GNU Make 3.81'i onu sessizce yok sayar
# ve cok satirli tarifler beklenmedik sekilde bolunur. Cok adimli is scripts/
# altina yazilir.

MODULE      := github.com/atknatk/tappa
BIN         := bin/tappa
TOOLS       := .tools
TAILWIND    := $(TOOLS)/tailwindcss

# Pinlenmis tool surumleri — surum yukseltmesi bilincli bir commit olmali.
TAILWIND_VERSION    := v3.4.17
GOOSE_VERSION       := v3.24.1
SQLC_VERSION        := v1.28.0
TEMPL_VERSION       := v0.3.833
GOVULNCHECK_VERSION := v1.6.0

GOOSE       := go run github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
SQLC        := go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)
TEMPL       := go run github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION)
GOVULNCHECK := go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

# Seed fixture yolu — CI veya tek seferlik bir sondaj baska dosya verebilir.
SEED_FILE ?= test/fixtures/seed.sql

-include .env
export

## help: bu listeyi goster
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | awk -F':' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------- toolchain --
## tools: Node'suz tailwind ikilisini indir (tek seferlik)
tools: $(TAILWIND)
$(TAILWIND):
	./scripts/get-tailwind.sh $(TAILWIND_VERSION) $(TAILWIND)

# ------------------------------------------------------------------ codegen --
## gen: templ + sqlc kod uretimi (kaynak degistiyse HER ZAMAN calistir)
gen: templ sqlc

## templ: .templ -> _templ.go
templ:
	$(TEMPL) generate

## sqlc: db/queries/*.sql -> internal/store (tip guvenli Go)
sqlc:
	$(SQLC) generate

## css: tailwind derle (Node YOK)
css: $(TAILWIND)
	$(TAILWIND) -i web/static/css/input.css -o web/static/css/app.css --minify

# --------------------------------------------------------------------- dev --
## up: postgres'i ayaga kaldir
up:
	docker compose up -d db
	@until docker compose exec -T db pg_isready -U tappa_owner -d tappa >/dev/null 2>&1; do sleep 1; done
	@echo "postgres hazir"

## down: dev servislerini durdur
down:
	docker compose down

## dev: gen + css + calistir
dev: gen css
	go run ./cmd/tappa

## build: tek binary (static gomulu)
build: gen css
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/tappa

# ---------------------------------------------------------------- database --
## migrate: bekleyen migration'lari uygula
migrate:
	$(GOOSE) -dir db/migrations postgres "$(DATABASE_MIGRATE_URL)" up

## migrate-down: son migration'i geri al
migrate-down:
	$(GOOSE) -dir db/migrations postgres "$(DATABASE_MIGRATE_URL)" down

## migrate-status: migration durumu
migrate-status:
	$(GOOSE) -dir db/migrations postgres "$(DATABASE_MIGRATE_URL)" status

## migrate-new: yeni migration olustur — make migrate-new name=add_foo
migrate-new:
	@test -n "$(name)" || { echo "kullanim: make migrate-new name=add_foo"; exit 1; }
	$(GOOSE) -dir db/migrations -s create $(name) sql

## seed: KF + KM demo verisini yukle (bkz. skill tappa-seed)
# psql konteynerden calisir — host'ta psql aranmaz (bkz. scripts/seed.sh).
seed:
	./scripts/seed.sh "$(SEED_FILE)"

## db-reset: SIFIRDAN kur — TUM VERIYI SILER
db-reset:
	$(GOOSE) -dir db/migrations postgres "$(DATABASE_MIGRATE_URL)" reset
	$(MAKE) migrate seed

## simulate-day: KF St Julians'ta bir gun uret (skill tappa-seed, M5-09)
# Kayitlari KARAR MOTORU uretir: gercek HTTP, gercek router, gercek Postgres.
# `make seed` yapilmis olmali; degilse test SKIP eder ve sebebini soyler.
# ~65 sn surer — bunun ~62 sn'si BEKLEMEDIR: ADR 0006 debounce'u sunucu saatiyle
# olcer, yani ayni kisinin ardisik tap'leri gercek zaman ister (gerekce:
# internal/handler/seedflow_db_test.go, seedDebounce).
simulate-day:
	go test -race -count=1 -v -run TestSeedDB_ADayAtKFStJulians ./internal/handler

# ------------------------------------------------------------------- kalite --
## test: tum testler + yaris dedektoru (CI bunu kosar — hicbir sey atlanmaz)
test:
	go test -race -count=1 ./...

## test-short: gelistirici ic dongusu — SIMULE EDILEN GUN + BCRYPT ORNEKLEMI atlanir
# NEDEN IKI HEDEF VAR. Olculdu (-race -count=1, tek makine). Sayilar ARALIKTIR;
# makine yukuyle oynayan tek sey uyku DISINDAKI her seydir:
# ⚠️ ASAGIDAKI SAYILAR DUVAR SAATIDIR (/usr/bin/time -p, ucer kosu), PAKET
# SURELERININ TOPLAMI DEGIL. Bu ayrim onemli: bu blok bir sure "~300 sn" diyordu ve
# o rakam `go test` ciktisindaki paket surelerinin TOPLAMIYDI (ayni agacta 254,5 sn)
# — paketler paralel kostugu icin duvar saati bunun yarisindan az. Bagimsiz bir
# ⚠️ VE HER SAYI BIR YUK KOSULUYLA BIRLIKTE YAZILIR. Dar, tek makineden alinmis
# bir bant bu repoda UC KEZ yanlis cikti: bagimsiz denetciler ayni hedefte
# 92-112 sn, 115,73 sn ve 120,6/149,3/145,6 sn olctu. Sebep makine durumu, kod
# degil — bu yuzden bant artik IKI kosulda olculuyor ve kosul da yaziliyor.
#   make test        113-115 sn BOS MAKINE   (bes kosu: 113,3 / 113,4 / 113,8 /
#                                             114,7 / 114,9)
#                    120-121 sn 4 CEKIRDEK MESGUL (iki kosu: 120,5 / 121,1)
#                    bagimsiz olcumler: 92-112 · 115,7 · 120,6-149,3
#                    -> makine durumuna gore 92-150 sn araliginda gorulur;
#                       bant bir HEDEF degil, bir gozlem kaydidir.
#                                   84,7-138 sn idi; M6-01 B fazi bcrypt getirdi.
#                                   Gunun ~62 sn'si time.Sleep'tir: ADR 0006
#                                   debounce'u SUNUCU saatiyle olcer, yani ayni
#                                   kisinin ardisik tap'leri gercek zaman ister ve
#                                   sikistirilamaz. (Gun testi tek basina
#                                   62,9-64,3 sn.)
#   make test-short  51-74 sn GOZLENEN ARALIK — bant bir HEDEF degil, `make test`
#                    icin oldugu gibi bir GOZLEM KAYDIDIR. Yuk arttikca dogrusal
#                    buyuyor ve makine durumu tek degiskendir:
#                      bos makine       50,9 / 51,4 / 51,9 sn
#                      4 cekirdek mesgul 54,9 / 57,0 / 57,0 sn
#                      8 cekirdek mesgul 73,6 sn
#                      bagimsiz denetci  69,9 / 70,6 / 74,2 sn  (8-cekirdek koluyla
#                                                                birebir ortusuyor)
#                    ⚠️ Bu sayi bu gorevde UC KEZ dar yazildi ve UC KEZ tutmadi
#                    (37-41 -> 50-57 -> 55-62). Dorduncusunu yazmamak icin artik
#                    tek bir dar bant degil GOZLENEN ARALIK yaziliyor.
#                    156 sn idi; M6-01 B fazi once 37-41 sn'ye indirdi, sonra
#                    kendi denetim turlari geri ekledi. 14. turda 66-67 sn'ye
#                    cikmisti; iki degisiklik geri getirdi: (a) flood tablosu
#                    artik adres butcesini ROTA BASINA degil BIR KEZ yakiyor
#                    (tavan 200 -> 3000 oldugu icin ~15000 istek ~3000'e indi),
#                    (b) sahte-DB'li ve KANITLAMAYAN unlookupable testi silindi
#                    (~10,7 sn, yerine gercek Postgres'li ikizi).
#
# ⚠️ -short SUITTE TAM UC SKIP URETIR, ve ucu de burada ADIYLA sayilir. Sayiyi
# dogrulayan komut: `go test -race -count=1 -short -v ./... | grep -c -- "--- SKIP:"`
# -> 3. Dorduncu bir skip eklenirse bu yorum da guncellenmelidir (M5-09'da bu blok
# uc zamanlama sayisini tasiyordu; ayni standart).
#   1. TestSeedDB_ADayAtKFStJulians (internal/handler/day_db_test.go) — simule
#      edilen gun; ~62 sn'si gercek time.Sleep.
#   2. TestAuthenticate_TimingIsFlat (internal/adminauth/manager_timing_test.go) —
#      YALNIZ bcrypt DUVAR SAATI ORNEKLEMI (5 kol x N cost-12 karsilastirma).
#   3. TestPanelE2E_TimingIsFlatOverHTTP (internal/handler/adminlogin_db_test.go) —
#      ayni olcumun HTTP + gercek Postgres uzerindeki ikizi (4 hucre x 2 giris).
#      2. ve 3. M6-01 B fazinda eklendi; -race altinda bir cost-12 karsilastirma
#      ~11 sn.
#      🔴 YUKUMLULUK 2 ATLANMIYOR — ve bu cumle bir sure YANLISTI, o yuzden
#      simdi UC yapisal test sayiyor, iki degil. -short'ta kosanlar:
#        a. TestAuthenticate_DummyIsReallyRun            (kukla KOSUYOR)
#        b. TestCost_MatchesTheDummyDigest
#           + TestSeedDigests_UseTheDeclaredCost         (DOGRU COST'ta)
#        c. TestAuthenticate_OverLongPasswordStillPaysBcrypt
#           (>72 BAYTLIK parola + BILINEN aday da tam bedeli oduyor)
#      ⚠️ (c) 8. turda EKLENDI ve eksikligi bir guvenlik denetcisi olctu: 6. tur
#      53x'lik bir zamanlama kehanetini kapatti ama onu YALNIZ duvar saati
#      testleriyle pinledi ve ayni turda ikisini de -short'tan cikardi. Denetci
#      duzeltmeyi geri cevirip `go test -short ./...` kostu: 14/14 YESIL, delik
#      ACIKKEN. Urun korumasizdi degil (commit kapisi tuttu) ama ic dongu kordu ve
#      bu blok tersini soyluyordu. (c) cost-4 fixture kullanir: durust
#      karsilastirma ~1,4 ms, bozuk dal ~66 ns -> dort buyukluk mertebesi pay,
#      duvar saati ornegi GEREKMEZ.
#      Atlanan yalnizca ISTATISTIKSEL ornektir; (a)-(c) olcum degil TAM kontrol.
#   t.Parallel()     ELENDI         paketin tum ust duzey testlerine eklendi;
#                                   art arda UC kosu, UC FARKLI testte kirmizi.
#                                   Koku tek: bu testler AYNI seed'li plakete
#                                   dokunuyor ve her tap last_ctr+1 okuyor, yani
#                                   paralel kosuda sayaci baskasi ilerletince tap
#                                   REPLAY sayilip reddediliyor. Biri §4.4
#                                   (kirmizi cizgi) testini OLMAYAN bir ihlalle
#                                   kizartti — yanlis alarm ureten bir kurulum.
# VARSAYILAN DEGISMEDI: CI hala `make check` -> `make test` kosar. Bu repoda
# "atlanan bir test gecmis sayilmaz" yazili bir derstir (M5-06 md.4: CI `make
# css` kosmadigi icin iki test DAIMA skip ediyordu). -short yalnizca ELDE, ic
# donguda kullanilir; zincirin gercekten kosuldugu yer `make test`tir.
test-short:
	go test -race -count=1 -short ./...

## cover: kapsam raporu
cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

## lint: vet + staticcheck
lint:
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...

## fmt: gofmt + templ fmt
fmt:
	gofmt -w -s $$(find . -name '*.go' -not -name '*_templ.go' -not -path './.tools/*')
	$(TEMPL) fmt .

## check: CI'nin calistirdigi her sey
# SIRA OLCULDU, VARSAYILMADI (2026-08-07, kullanici karari; M6-04 9. tur):
#   fmt -> gen   `templ fmt` .templ KAYNAGINI bicimlendirir. Once bicimlendirip
#                SONRA uretmek, uretilen dosyanin daima commit'lenen kaynakla
#                eslesmesini saglar; ters sirada bicimlendirme kaynagi uretimden
#                sonra degistirebilir ve _templ.go bayat kalir.
#   gen -> test  bayat uretimle test kosulmasin diye.
#   Olculdu: sqlc ciktisi zaten `gofmt -s` temiz (gofmt -l -s internal/store/ -> bos).
#   ⚠️ BU BLOK BIR SURE "ve `templ fmt` bugun hicbir .templ'i degistirmiyor, yani bu
#   sira bugun ek bir fark uretmiyor" DIYORDU. 2026-08-12'de yanlis cikti: M6-09 faz
#   B'nin policies.templ'i `templ fmt` ile degisiyordu (changed=1), yani `check`
#   kaynagi degistirip son adimda kendi degisikligine takiliyordu. Sira DOGRU oldugu
#   icin tutuyor -- fmt once kosuyor, uretim ondan sonra -- ama "bugun fark
#   uretmiyor" bir GOZLEMDI ve bayatladi. Yazilmasi gereken kalici cumle: bu sira
#   formatlayicinin kaynagi degistirdigi durumda DA dogru sonucu verir; degistirdigi
#   gun `git diff --exit-code` haber verir, ki 2026-08-12'de tam olarak bu oldu.
#
# NEDEN `gen` BURADA (M6-04 9. tur, KAPSAM GENISLEMESI, kullanici karari 2026-08-07):
# `check` bugune kadar `gen` KOSMUYORDU, ama son adimi "uretilen dosyalar commit
# edilmis mi" diye kontrol ediyordu -- yani hicbir zaman uretmeden dogruluyordu.
# Olculdu: bir .templ degistirilip `make gen` atlanirsa, bayat bir _templ.go ile
# `make check` VE CI yesil kaliyordu ve urun eski markup'i render ediyordu.
# MALIYET OLCULDU -- VE ILK SAYIM YANILTICIYDI, o yuzden kosuluyla birlikte yazili:
#   make gen, sicak onbellek, ayni oturumda   3,34 sn (bir kez) · 9,39 / 13,75 / 14,89 sn (uc kosu)
#   make fmt gen, soguk onbellek              27,86 sn
# Fark araclarin kendisidir: templ ve sqlc `go run <mod>@surum` ile kosuyor, yani
# Go'nun derleme onbellegi bosaldiginda ikililer yeniden derleniyor. Ilk raporlanan
# 3,34 sn EN SICAK okumaydi ve tek basina yanilticiydi; planlanacak sayi ~10-15 sn.
# Cikti DETERMINISTIK (iki ardisik kosunun birlesik shasum'i ayni) ve temiz agacta
# ek diff yok -- yani `git diff --exit-code` sahte kirmizi vermiyor (olculdu).
check: fmt gen lint test
	@git diff --exit-code || { echo "gen/fmt ciktisi commit edilmemis"; exit 1; }

## audit: guvenlik denetimi — IKI TARAMA DA KOSAR, ikisinin de cikisi raporlanir
# 🔴 IKI TARAMA BIRBIRINDEN BAGIMSIZ KOSAR, VE BU BIR HATA DUZELTMESIDIR (2026-08-14).
# Onceki hali iki ayri recipe satiriydi; make bir satir sifirdan farkli donunce DURUR,
# yani govulncheck kirmiziyken `./scripts/redline-check.sh` HIC KOSMUYORDU. Olculdu:
# `make audit` exit 2, ve ciktinin tamaminda "redline"/"mekanik tarama" gecmiyor (0
# esleme) -- kirmizi cizgi taramasinin sonucu hicbir yerde yok.
#
# BUNU TEHLIKELI YAPAN SEY BILINEN KIRMIZI: govulncheck bugun T31 yuzunden zaten
# kirmizi (go1.26.5 -> 1.26.6, kullanicinin isi). "make audit zaten kirmizi"
# aliskanligi, ikinci taramanin hic calismadigini gizliyordu -- yani bir guvenlik
# aginin sessizce kapali olmasi, agin kendisinden daha buyuk bir sorun.
#
# TEK SHELL, IKI CIKIS, BIRLESIK SONUC. Recursive make yerine tek recipe secildi:
# `$(MAKE)` cagrisi her tarama icin bir alt-make surecidir ve `-` onekiyle yazilan
# `-$(GOVULNCHECK)` sonucu YUTAR (make yalnizca uyarir), yani ikisi de raporlanan
# bir cikis vermez. Burada iki cikis da degiskene alinir, ikisi de BASILIR, ve
# recipe ikisinden biri sifir degilse basarisiz olur -- bilgi kaybi yok.
#
# Tek tarama ayri kosulacaksa: `./scripts/redline-check.sh` (tek basina exit 0
# vermeli) ya da `$(GOVULNCHECK) ./...`.
#
# 🔴 VE OZET SATIRI "ATLANDI"YI "TEMIZ"DEN AYIRIR (2026-08-14). Ilk hali yalnizca
# cikis kodunu basiyordu; rg kurulu degilken redline-check `exit 0` verdigi icin
# ozet "redline-check exit=0" diyor ve make yesil doniyordu -- yani KOSMAYAN bir
# taramayi TEMIZ gibi raporluyordu, ustelik basligi "iki tarama da kosar" diyen bir
# hedefte. Script artik o durumda 2 donuyor ve burada AYRI bir kelimeyle basiliyor.
audit:
	@vuln=0; red=0; \
	echo "--- govulncheck ---"; $(GOVULNCHECK) ./... || vuln=$$?; \
	echo "--- redline-check ---"; ./scripts/redline-check.sh || red=$$?; \
	redword="exit=$$red"; \
	if [ $$red -eq 2 ]; then redword="SKIPPED(no rg) exit=2"; fi; \
	echo ""; \
	echo "audit: govulncheck exit=$$vuln - redline-check $$redword"; \
	test $$vuln -eq 0 && test $$red -eq 0

.PHONY: help tools gen templ sqlc css up down dev build migrate migrate-down \
        migrate-status migrate-new seed db-reset simulate-day test test-short \
        cover lint fmt check audit
