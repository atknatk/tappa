# syntax=docker/dockerfile:1

# Tappa — production image (M8-02).
#
# Two deployable artifacts come out of this one file, and they are SEPARATE on
# purpose:
#
#   --target app       ghcr.io/atknatk/tappa           the server
#   --target migrate   ghcr.io/atknatk/tappa-migrate   goose + db/migrations
#
# 🔴 WHY TWO IMAGES AND NOT ONE WITH BOTH BINARIES. M8-01's structural guarantee is
# that the application CANNOT run a migration: goose is not a module dependency
# (`go list -deps ./cmd/tappa | grep -c goose` → 0), so no amount of wiring inside
# cmd/tappa can reach it. Shipping goose beside the server would not break that
# guarantee at the Go level, but it would put a DDL tool one `command:` override
# away from a pod whose whole point is that it connects as tappa_app (NOBYPASSRLS,
# not the table owner — CLAUDE.md §4.5). Two images keep the separation where the
# deployment can see it: the app image contains exactly one executable, and the only
# pod that ever holds DATABASE_MIGRATE_URL is the migration Job.
#
# 🔴 goose IS INSTALLED WITH `go install pkg@version`, WHICH DOES NOT TOUCH go.mod.
# That form has been module-isolated since Go 1.16 — it resolves and builds in its
# own context and cannot add a require line. The guarantee above is therefore still
# mechanical after this file exists, and cmd/tappa/packaging_test.go still proves it.

# The toolchain is pinned, and 1.26.6 rather than CI's 1.26.5 is a DELIBERATE and
# measured difference: backlog T31 records six stdlib advisories that govulncheck
# counts against 1.26.5 (GO-2026-6088, GO-2026-5972, GO-2026-5026 and three more),
# all of which close in 1.26.6. CI's pin is the USER's Go installation and their call
# to move; the SHIPPED binary is this file's call, and shipping a knowingly
# vulnerable stdlib to a pilot with real employees' data is not one of the options.
ARG GO_IMAGE=golang:1.26.6-bookworm

# ---------------------------------------------------------------------- build --
FROM ${GO_IMAGE} AS build

WORKDIR /src

# Module cache as its own layer: go.sum changes rarely, source changes constantly.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 🔴 THE ARTIFACT IS BUILT BY `make build`, NOT BY A go build LINE WRITTEN HERE.
# The Makefile is this repository's single definition of what the artifact is
# (CGO_ENABLED=0, -trimpath, -ldflags "-s -w"), and cmd/tappa/packaging_test.go goes
# out of its way to READ that recipe rather than copy it, precisely so a second
# spelling cannot drift — "a check and the code it protects looking at two different
# representations of the same value" is a defect class this repo has already paid for
# three times (internal/config/config.go, prefixes). A `go build` line in this file
# would be that second spelling at the one place that actually ships.
#
# It also means the image is built FROM SOURCE: `make build` depends on `gen` (templ
# + sqlc) and `css`, so a stale committed *_templ.go or internal/store/*.go cannot be
# what a customer runs. `make check` already enforces that the committed output
# matches; this makes it true of the image independently of that gate.
#
# NODE IS NOT INVOLVED AND CANNOT BE (CLAUDE.md §1). `make css` depends on
# `make tools`, which runs scripts/get-tailwind.sh to fetch the STANDALONE Tailwind
# binary for the builder's own platform (linux/x64 here). There is no npm, no
# package.json and no node_modules in this image or in its context.
#
# ⚠️ .git IS IN THE CONTEXT ON PURPOSE — see .dockerignore. Go's -buildvcs stamps
# vcs.revision/vcs.time/vcs.modified from it, and internal/buildinfo reads them back
# at start-up. Without .git the stamp is silently omitted and the running process can
# no longer name its own commit.
RUN make build

# The migration tool, pinned to the SAME version the Makefile pins for `make migrate`
# — read out of the Makefile rather than repeated here, for the reason above.
RUN CGO_ENABLED=0 go install \
      "github.com/pressly/goose/v3/cmd/goose@$(sed -n 's/^GOOSE_VERSION *:= *//p' Makefile)"

# -------------------------------------------------------------------- migrate --
# Ordered BEFORE `app` so that a plain `docker build` with no --target produces the
# server, which is the artifact anyone typing that command means.
FROM scratch AS migrate

# goose dials Postgres; if a deployment ever moves to a managed instance with
# sslmode=verify-full, the roots have to be here. ~200 KB.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /go/bin/goose /goose
COPY db/migrations /migrations

# 🔴 THE DSN IS NOT AN ARGUMENT. goose reads GOOSE_DBSTRING from the environment, so
# the migration role's password never appears in argv — where `kubectl describe pod`,
# any `ps` and the container runtime's own logs would carry it (CLAUDE.md §4.7).
# GOOSE_DBSTRING itself is supplied by the Job from a Secret; nothing is baked here.
ENV GOOSE_DRIVER=postgres \
    GOOSE_MIGRATION_DIR=/migrations

USER 65532:65532
ENTRYPOINT ["/goose"]
CMD ["up"]

# ------------------------------------------------------------------------ app --
FROM scratch AS app

# 🔴 scratch, NOT alpine OR distroless, AND IT IS MEASURED RATHER THAN ASSUMED.
# The binary is CGO_ENABLED=0 static, every asset and template is compiled in
# (web/embed.go), production code opens no file at runtime (M8-01 measured
# os.ReadFile/os.Open/http.Dir → 0 matches), and cmd/tappa imports time/tzdata
# ITSELF — so the one thing a base image is usually kept around for, /usr/share/
# zoneinfo, is already inside the executable. What remains is the CA bundle, and one
# file is not a reason for a distribution.
#
# WHAT AN EMPTY IMAGE BUYS BEYOND SIZE: there is no shell, no package manager, no
# busybox and no libc, so a command injection or a compromised sidecar has nothing to
# execute. It also costs something, stated rather than implied — `kubectl exec` into
# this pod cannot work, so debugging is `kubectl logs` plus an ephemeral debug
# container (`kubectl debug -it <pod> --image=busybox --target=tappa`).

# The ONLY thing the process needs from a filesystem: the outbound TLS roots. It has
# exactly one outbound HTTPS caller — the VIES VAT check in internal/domain/signup —
# and Q09 made that check BEST EFFORT, so a missing bundle would not crash anything.
# It would do something worse: every registration would silently record "not
# verified" and look like the Commission was down. Measured in this task; see the
# report's ca-certificates probe.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /src/bin/tappa /tappa

# 🔴 NON-ROOT, DECLARED NUMERICALLY BECAUSE THERE IS NO /etc/passwd TO NAME.
# 65532 is the conventional "nonroot" uid (the one distroless uses), and a numeric
# USER is what lets Kubernetes' runAsNonRoot admission check pass without the image
# carrying a passwd database it has no other use for. deploy/k8s/20-app.yaml pins the
# same uid in the pod's securityContext, so the guarantee does not depend on this
# line alone.
USER 65532:65532

# TAPPA_ADDR defaults to :8080 (internal/config).
EXPOSE 8080

ENTRYPOINT ["/tappa"]
