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

## test-short: gelistirici ic dongusu — SIMULE EDILEN GUN atlanir
# NEDEN IKI HEDEF VAR. Olculdu (-race -count=1, tek makine, dort oturum). Sayilar
# ARALIKTIR; makine yukuyle oynayan tek sey uyku DISINDAKI her seydir:
#   make test        84,7-138 sn    gunun ~62 sn'si time.Sleep'tir: ADR 0006
#                                   debounce'u SUNUCU saatiyle olcer, yani ayni
#                                   kisinin ardisik tap'leri gercek zaman ister
#                                   ve sikistirilamaz. (Gun testi tek basina
#                                   62,9-64,3 sn.)
#   make test-short  32,9-35 sn     tek fark: TestSeedDB_ADayAtKFStJulians
#                                   (internal/handler/day_db_test.go) atlanir —
#                                   olculdu: suitte TAM 1 skip, baska yok.
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
check: fmt lint test
	@git diff --exit-code || { echo "gen/fmt ciktisi commit edilmemis"; exit 1; }

## audit: guvenlik kirmizi cizgi denetimi (bkz. .claude/agents/tappa-security-auditor.md)
audit:
	$(GOVULNCHECK) ./...
	./scripts/redline-check.sh

.PHONY: help tools gen templ sqlc css up down dev build migrate migrate-down \
        migrate-status migrate-new seed db-reset simulate-day test test-short \
        cover lint fmt check audit
