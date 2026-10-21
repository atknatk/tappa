# Tappa — production image (M8-02).
#
# Two deployable artifacts come out of this one file, and they are SEPARATE on
# purpose:
#
#   --target app       registry.everva.com.tr/tappa/tappa           the server
#   --target migrate   registry.everva.com.tr/tappa/tappa-migrate   goose + db/migrations
#
# ⚠️ THIS REPOSITORY BUILDS AND PUSHES TO ITS OWN HARBOR REGISTRY, A PRIVATE PROJECT (OPS-2);
# the names above are what .github/workflows/deploy.yml publishes and the pod specs pull
# with the operator's `harbor-pull` Secret. Before OPS-2 the two images were public on
# Docker Hub (docker.io/atknatk/tappa and …/tappa-migrate) because the pull credential of
# the time was rewritten by every deploy from a token that died with it and this cluster's
# kubelet verifies recorded credentials (KEP-2535); deploy/k8s/20-app.yaml says why a
# credential nobody rewrites does not bring that back. deploy.yml proves after every push
# that both images are pullable with the CI credential and refused without one.

# 🔴 NO PARSER DIRECTIVE NAMES A FRONTEND, ON PURPOSE (OPS-2 follow-up). The file used to open
# with the `syntax` directive set to docker/dockerfile:1, which makes BuildKit fetch a frontend
# IMAGE from Docker Hub -- by a moving tag, anonymously -- before it reads a single instruction:
# the one Docker Hub pull left in the deploy build after the base image moved to Harbor's proxy.
# Measured 2026-10-19 (docker 29.8.2, BuildKit v0.33.1): with the directive pointed at an
# unreachable registry the build fails at "failed to resolve source metadata" before parsing;
# without it the frontend built into BuildKit parses this file (`--call=targets` lists all five
# stages, `--call=check` reports no warnings) and fetches no frontend. Nothing here needs more:
# multi-stage FROM, ARG before FROM, COPY --from -- Dockerfile syntax older than BuildKit itself.
# Taking the directive out was chosen over pinning it to the proxy by digest (a local build holds
# no Harbor credential) and over a BUILDKIT_SYNTAX build-arg in deploy.yml (a second spelling of
# the same pull, to keep in step with this file). A feature that needs a newer frontend brings
# the question back; cmd/tappa/deployregistry_test.go pins the absence.

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
# 2026-10-09: 1.26.6 -> 1.26.9 for GO-2026-6617 (net/http; govulncheck reports it as
# called from the request path; fixed in 1.26.9). CI and deploy.yml pin the same version.
#
# 🔴 PINNED BY INDEX DIGEST, AND THE DEFAULT IS DOCKER HUB ON PURPOSE (OPS-2 fix round 1).
# deploy.yml overrides GO_IMAGE with the SAME digest through Harbor's private proxy-cache project
# (registry.everva.com.tr/dockerhub/library/golang:...@sha256:<this digest>): the deploy job logs
# in to Harbor, and a pull through the proxy does not spend Docker Hub's anonymous budget. The
# DEFAULT stays Docker Hub's own name because whoever builds without that override -- a
# developer, the sandbox runbook in deploy/README.md -- holds no Harbor credential, and the
# proxy project is private. ci.yml never builds this file (its toolchain is setup-go, pinned to
# the same version), so it needs neither. The digest is the one thing both spellings share: it
# is content-addressed, so the two sources can only serve the same index or fail, and a tag
# re-pushed on Docker Hub changes neither build. Measured 2026-10-19: the index digest of
# golang:1.26.9-bookworm on Docker Hub. A bump moves three lines together (here, deploy.yml's
# GO_BUILD_IMAGE, cmd/tappa/deployregistry_test.go's goImageDigest).
# 2026-10-21: 1.26.9 -> 1.26.10 for GO-2026-6809 (crypto/x509, distrust of compromised
# intermediates; govulncheck reports it as called from the VIES client's certificate check;
# fixed in 1.26.10). Index digest of golang:1.26.10-bookworm on Docker Hub, measured
# 2026-10-21 (lists linux/amd64). CI and deploy.yml pin the same version.
ARG GO_IMAGE=golang:1.26.10-bookworm@sha256:5b1f78db1f54cc8cb46e652c51d3f854441c4310a449161b6fe9fb553edab505

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

# ---------------------------------------------------------------- app-sandbox --
# THE SANDBOX'S SERVER (M11 API-11, ADR 0028 §11, K-3): the same source, built by `make
# build-sandbox` (-tags sandbox), so it contains internal/sandbox -- the tap simulation -- and its
# internal/config starts only with TAPPA_ENV=sandbox. Its own stage and its own target, so the
# production image cannot contain it: `docker build --target app` never runs this stage, and
# scripts/verify-image.sh refuses a production image whose binary carries the tag (and a sandbox
# image whose binary does not). Ordered BEFORE `app` for the reason `migrate` is: a plain `docker
# build` with no --target must produce the production server.
#
# deploy.yml does not build or push it: the sandbox's deploy path comes with its domain (deploy/README.md,
# "Sandbox (API-11)").
FROM build AS sandbox-build
RUN make build-sandbox

FROM scratch AS app-sandbox
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=sandbox-build /src/bin/tappa-sandbox /tappa
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/tappa"]

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
