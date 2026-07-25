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

# ------------------------------------------------------------------- kalite --
## test: tum testler + yaris dedektoru
test:
	go test -race -count=1 ./...

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
        migrate-status migrate-new seed db-reset test cover lint fmt check audit
