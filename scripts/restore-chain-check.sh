#!/usr/bin/env bash
# Tappa -- the backup -> restore -> verify chain, end to end, on every CI run (T215; `make restore-check`).
#
# WHY. scripts/pg-restore-verify.sh FAILed every restore from goose 74 on (T214: the dump's
# SELECT (xmin) grants, which information_schema cannot see) and nobody saw it for weeks, because
# nothing ran the chain: CI ran the scripts' SQL in pieces (cmd/tappa and internal/db
# TestRestoreVerify_*), the CronJob dumps but never restores, and the last full rehearsal was T45's
# by hand. A check that only runs when somebody remembers to run it is a check of the day it was
# written. This file runs the whole chain, with the real scripts, against throwaway containers:
#
#   1. a SOURCE built the way development and CI build theirs (scripts/db-init: the roles, the
#      development password, production's `arwd` default ACL), migrated with goose, loaded with
#      test/fixtures/seed.sql and test/fixtures/restore-chain.sql (a row in every append-only table,
#      a feed counter moved);
#   2. scripts/pg-backup.sh, in the CronJob's container shape (deploy/k8s/50-backup.yaml:
#      postgres:17-alpine, `sh /scripts/pg-backup.sh`, read-only root, a writable /staging and /tmp);
#   3. the restore, exactly as deploy/README.md writes it -- operator step 9(d)'s blocks (d2), (d3),
#      (d3b), (d4) and (d5), i.e. B YOLU's B1 init, B2, B3 with its narrowing, B4's counter step and
#      B4's pg-restore-verify.sh -- CUT OUT OF THE RUNBOOK AND RUN, not copied into this file. A
#      runbook edit is therefore tested the day it is made, and the runbook cannot drift from what CI
#      proves. The only edit is the container and network names (tappa-drill -> a per-run name), so
#      two runs on one machine do not collide.
#   4. pg-restore-verify.sh must exit 0; any block that fails stops the chain red.
#
# 9(d) IS B YOLU WITH `docker exec` FOR `kubectl exec` -- 9(d)'s own words ("aynı SQL"). That claim is
# checked here before anything runs: the SQL inside B2's, B3's and B4's here-documents and their
# `sh -c` command lines must be byte-identical to (d3)'s and (d3b)'s, and (d2) must install the two
# init files B1 installs. A runbook whose two copies disagree is refused before a container starts.
#
# WHAT IT DOES NOT TOUCH: the development/CI database. Its source is its own container on a random
# 127.0.0.1 port, the fixture never reaches `make up`'s database (the seeded tests read its counts).
# The restore target is (d2)'s own container. Both are removed on exit, and the dump directory with
# them (it is employees' attendance records in a real drill; here it is fixture rows), and so are
# their anonymous data volumes (`docker rm -f -v`).
#
# NO SECRET IS IN THIS FILE OR IN ITS LOG. The source's owner password is drawn per run
# (openssl rand, as (d2) draws the target's) and handed to docker by NAME (`-e PGPASSWORD`), never
# echoed; goose gets it in its connection string, on this machine's own process list, for a
# container that lives for a minute.
#
# NEEDS: docker, openssl, awk, and GOOSE (the Makefile passes its pinned `go run ...goose@vX`).
# bash 3.2 compatible (macOS): no associative arrays, no mapfile.
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$PWD

: "${GOOSE:?GOOSE is not set: run this through make restore-check, which passes the pinned goose of the Makefile}"
IMAGE=postgres:17-alpine
RUNBOOK=deploy/README.md
SEED=test/fixtures/seed.sql
FIXTURE=test/fixtures/restore-chain.sql
# A watchdog for the runbook's blocks: (d2)'s `until ... pg_isready` has no limit of its own, and a
# target whose init failed would spin there until the CI job's own limit. An ordinary run is ~20 s.
DRILL_DEADLINE=300

say()  { echo "restore-chain: $*"; }
die()  { echo "restore-chain: FAILED: $*" >&2; exit 1; }
for tool in docker openssl awk; do
  command -v "$tool" >/dev/null 2>&1 || die "'$tool' is not on PATH"
done
docker info >/dev/null 2>&1 || die "docker is installed but the daemon does not answer"

# Per run, so a second run on the same machine (another checkout, another agent) cannot collide.
# Lower case, digits and dashes only: the name goes through sed below.
RUN="rc$$$(openssl rand -hex 3)" || die "openssl rand failed"
SRC="tappa-restorechain-src-$RUN"
DRILL="tappa-drill-$RUN"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/tappa-restore-chain.XXXXXX")"
RESTORE="$WORK/restore"
mkdir -m 700 "$RESTORE"
cleanup() {
  # Only the names this run created. `docker rm -f` on a name that does not exist is not an error
  # worth reporting: (d5) has already removed the target on a green run.
  # -v: the source has no --rm, and its anonymous data volume (~50 MB) outlived every run without
  # it (in review: 12 volumes after 9 runs); (d2)'s target has --rm, but not on a red path.
  docker rm -f -v "$SRC" "$DRILL-db" >/dev/null 2>&1 || true
  docker network rm "$DRILL" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT
T0=$(date +%s)
lap() { echo "restore-chain: [$(( $(date +%s) - T0 )) s] $*"; }

# =================================================================================================
# 0. THE RUNBOOK'S BLOCKS -- cut out, and the two copies of the B YOLU SQL compared.
# =================================================================================================
# block MARKER N: the N-th ```bash block after the one line that starts with MARKER, before the
# next step's marker. Exit 3 unless the marker is on exactly one line and the block is found: a
# reworded heading must stop this file loudly, not make it run something else.
block() {
  awk -v m="$1" -v want="$2" '
    index($0, m) == 1 { seen++; armed = 1; n = 0; next }
    inside && $0 == "```" { inside = 0; armed = 0; done++; next }
    inside { print; next }
    armed && (/^\(d[0-9]/ || /^\*\*B[0-9]\. / || /^#/) { armed = 0 }
    armed && $0 == "```bash" { n++; if (n == want) inside = 1 }
    END { if (seen != 1 || done != 1) exit 3 }' "$RUNBOOK"
}
cut_block() { # var marker n
  local text
  text="$(block "$2" "$3")" || die "$RUNBOOK: no single line starting with '$2' followed by ${3}. \`\`\`bash block; the runbook was reworded, and this check runs the runbook's own text, so it stops here"
  [ -n "$text" ] || die "$RUNBOOK: the block after '$2' is empty"
  eval "$1=\$text"
}
cut_block D2  '(d2) '  1
cut_block D3  '(d3) '  1
cut_block D3B '(d3b) ' 1
cut_block D4  '(d4) '  1
cut_block D5  '(d5) '  1
cut_block B1  '**B1. ' 2
cut_block B2  '**B2. ' 1
cut_block B3  '**B3. ' 1
cut_block B4  '**B4. ' 1

# heredocs: every <<'SQL' ... SQL body, each preceded by a separator line.
heredocs() { awk -v q="'" '$0 ~ ("<<" q "SQL" q "$") { inside = 1; print "-- 8< --"; next } inside && $0 == "SQL" { inside = 0; next } inside { print }'; }
# shcmds: the command each `sh -c '...'` runs inside the database's container.
shcmds() { awk -v q="'" '{ i = index($0, "sh -c " q); if (i) { r = substr($0, i + 7); print substr(r, 1, index(r, q) - 1) } }'; }
same() { # what, B-text, drill-text
  if [ "$2" != "$3" ]; then
    echo "restore-chain: B YOLU says:" >&2; printf '%s\n' "$2" | sed 's/^/    /' >&2
    echo "restore-chain: 9(d) says:" >&2;  printf '%s\n' "$3" | sed 's/^/    /' >&2
    die "$RUNBOOK: $1 differ between B YOLU (B2, B3, B4) and its drill 9(d) ((d3), (d3b)). 9(d) is what this check runs; B YOLU is what an operator runs in a disaster. Make them one text again."
  fi
}
B_SQL="$(printf '%s\n%s\n%s\n' "$B2" "$B3" "$B4" | heredocs)"
D_SQL="$(printf '%s\n%s\n' "$D3" "$D3B" | heredocs)"
same "the SQL of the restore's here-documents" "$B_SQL" "$D_SQL"
same "the psql command lines" "$(printf '%s\n%s\n%s\n' "$B2" "$B3" "$B4" | shcmds)" "$(printf '%s\n%s\n' "$D3" "$D3B" | shcmds)"
[ "$(printf '%s\n' "$D_SQL" | awk '/^-- 8< --$/ { n++ } END { print n + 0 }')" -eq 3 ] \
  || die "$RUNBOOK: (d3) and (d3b) should carry three here-documents (B2's suspension, B3's narrowing, B4's counter step)"
for f in scripts/db-init/01-roles.sql deploy/k8s/postgres-init/02-app-password.sh; do
  case "$B1" in *"$f"*) ;; *) die "$RUNBOOK: B1 no longer installs $f; this check cannot say what the init is" ;; esac
  case "$D2" in *"$f"*) ;; *) die "$RUNBOOK: (d2) does not install $f, which B1 does: the drill's target would not be the one B YOLU builds" ;; esac
done
case "$D4" in *"scripts/pg-restore-verify.sh"*) ;; *) die "$RUNBOOK: (d4) no longer runs scripts/pg-restore-verify.sh" ;; esac
# 🔴 THE DRILL BLOCKS MUST NOT BE ABLE TO REACH ANYTHING BUT THEIR OWN CONTAINER. `same()` compares the
# SQL and the `sh -c` lines, not the executor in front of them, so a (d3) edited into a copy of B3
# -- `kubectl -n tappa exec ... statefulset/tappa-postgres` -- would pass it and run B2's
# ALTER DEFAULT PRIVILEGES against whatever cluster the developer's kubeconfig names (read in
# review, not measured), and `env -i` below does not stop a `docker exec -i tappa-db ...` aimed at
# the developer's own database. Three belts, before a container starts:
#   AN ALLOW RULE FOR DOCKER: every line that runs docker names this drill's container or network
#     (`tappa-drill`, renamed per run below), so docker can only reach what (d2) created;
#   AN ALLOW RULE FOR rm: the one rm a block may run is (d5)'s `rm -rf "$RESTORE"`;
#   A DENY LIST for what has no business in a local drill: kubectl, rclone, ssh, curl, wget, nc,
#     the home directory, the rclone config variable, a /Users/ path, docker's -H and --context.
# A full allow list of commands was weighed and not taken: the blocks carry continuation lines and
# here-documents, so "the first word of a line" is not a command and such a list would refuse the
# runbook's own text or need a shell parser. Step 3 also runs the blocks with an emptied environment
# (no KUBECONFIG, no PG*, no DATABASE_URL, HOME a scratch directory).
for b in D2 D3 D3B D4 D5; do
  eval "text=\$$b"
  if printf '%s\n' "$text" | grep -E -q 'kubectl|rclone|(^|[^A-Za-z0-9_-])(ssh|curl|wget|nc)([^A-Za-z0-9_-]|$)|\$HOME|\$\{HOME|\$CONF|\$\{CONF|/Users/|(^|[[:space:]])-H[[:space:]]|--context'; then
    die "$RUNBOOK: the drill block $b names kubectl, rclone, ssh, curl, wget, nc, the home directory, the rclone config variable, a /Users/ path, -H or --context. 9(d)'s (d2)-(d5) run here, against a throwaway container; a block that can reach a cluster, the backup target, another host or the operator's files is not run"
  fi
  bad_docker="$(printf '%s\n' "$text" | awk '/(^|[^A-Za-z0-9_-])docker([^A-Za-z0-9_-]|$)/ && !/tappa-drill/')"
  [ -z "$bad_docker" ] || die "$RUNBOOK: the drill block $b runs docker without naming the drill's own container or network (tappa-drill): '$bad_docker' -- docker could reach another container, the development database included"
  bad_rm="$(printf '%s\n' "$text" | awk '/^[[:space:]]*rm[[:space:]]/ && $0 != "rm -rf \"$RESTORE\""')"
  [ -z "$bad_rm" ] || die "$RUNBOOK: the drill block $b removes something other than \"\$RESTORE\": '$bad_rm'"
done
lap "runbook: (d2) (d3) (d3b) (d4) (d5) cut out; B2/B3/B4 and (d3)/(d3b) carry the same SQL and command lines"

# =================================================================================================
# 1. THE SOURCE -- built as development and CI build theirs, migrated, seeded, fixture loaded.
# =================================================================================================
SRC_PW="$(openssl rand -hex 24)"
POSTGRES_PASSWORD="$SRC_PW" docker create --name "$SRC" -p 127.0.0.1::5432 \
  -e POSTGRES_USER=tappa_owner -e POSTGRES_DB=tappa -e POSTGRES_PASSWORD -e TZ=UTC -e PGTZ=UTC \
  "$IMAGE" >/dev/null || die "could not create the source container from $IMAGE"
# docker cp, not a bind mount: (d2)'s note -- on Docker Desktop a bind-mounted 0644 .sh passed the
# entrypoint's [ -x ] test and failed with "bad interpreter" (measured there).
for f in scripts/db-init/*; do
  docker cp "$f" "$SRC:/docker-entrypoint-initdb.d/" >/dev/null || die "could not copy $f into the source container"
done
docker start "$SRC" >/dev/null || die "the source container did not start"
# TCP and not the socket: the entrypoint's temporary init server answers on the socket only (the
# Makefile's `up` measured what a socket probe gets wrong).
n=0
until docker exec "$SRC" pg_isready -q -h 127.0.0.1 -U tappa_owner -d tappa >/dev/null 2>&1; do
  n=$((n + 1)); [ "$n" -le 120 ] || die "the source did not accept TCP connections within 120 s: $(docker logs --tail 20 "$SRC" 2>&1 | tr '\n' ' ')"
  sleep 1
done
PORT="$(docker port "$SRC" 5432/tcp | awk -F: 'NR == 1 { print $NF }')"
case "$PORT" in ''|*[!0-9]*) die "could not read the source's published port (got '$PORT')" ;; esac
lap "source $SRC up on 127.0.0.1:$PORT"

$GOOSE -dir db/migrations postgres "postgres://tappa_owner:$SRC_PW@127.0.0.1:$PORT/tappa?sslmode=disable" up >"$WORK/goose.log" 2>&1 \
  || { tail -20 "$WORK/goose.log" >&2; die "goose could not migrate the source"; }
lap "source migrated: goose $(docker exec "$SRC" psql -X -q -A -t -U tappa_owner -d tappa -c 'SELECT max(version_id) FROM goose_db_version')"
for f in "$SEED" "$FIXTURE"; do
  docker exec -i "$SRC" psql -X -q -v ON_ERROR_STOP=1 -U tappa_owner -d tappa <"$f" >/dev/null \
    || die "could not load $f into the source"
done
lap "source loaded: $SEED and $FIXTURE"

# =================================================================================================
# 2. THE BACKUP -- the real scripts/pg-backup.sh, in the CronJob's container shape. It shares the
#    source's network namespace, so PGHOST is the source's own loopback on every platform.
#    --user is the invoking user so the runner can remove what it wrote (the CronJob's 65532 would
#    leave root-unremovable files on a Linux runner).
# =================================================================================================
# (The owner's password goes in by NAME on a line of its own: scripts/secretscan.sh's a0-token
# shape is a credential word and a quoted token on one line.)
PGPASSWORD="$SRC_PW" \
  docker run --rm --network "container:$SRC" --user "$(id -u):$(id -g)" \
  --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges \
  -v "$RESTORE:/staging" -v "$ROOT/scripts:/scripts:ro" \
  -e PGHOST=127.0.0.1 -e PGPORT=5432 -e PGUSER=tappa_owner -e PGDATABASE=tappa -e BACKUP_STAGE_DIR=/staging \
  -e PGPASSWORD \
  "$IMAGE" /bin/sh /scripts/pg-backup.sh || die "scripts/pg-backup.sh exited non-zero"
set -- "$RESTORE"/tappa-*.sql.gz
[ "$#" -eq 1 ] && [ -f "$1" ] || die "pg-backup.sh did not leave exactly one tappa-*.sql.gz in the staging directory"
STAMP="${1##*/tappa-}"; STAMP="${STAMP%.sql.gz}"
[ -f "$RESTORE/tappa-$STAMP.manifest" ] || die "pg-backup.sh left no manifest next to tappa-$STAMP.sql.gz"
docker rm -f -v "$SRC" >/dev/null 2>&1 || true
# The fixture's job, checked from the manifest: a row in each append-only table (the list is
# pg-restore-verify.sh's own trunc_tables, which TestAppendOnlyTablesAreNamedByBothScripts derives
# from db/migrations) and a feed counter past 0. Without this a fixture that quietly stopped writing
# would leave section 6 and the row counts judging empty tables, green.
TRUNC="$(awk -F'"' '/^trunc_tables="/ { print $2; exit }' scripts/pg-restore-verify.sh)"
[ -n "$TRUNC" ] || die "scripts/pg-restore-verify.sh has no trunc_tables=\"...\" line"
empty=""
for t in $TRUNC; do
  awk -v t="$t" '$1 == "table" && $2 == t && $3 > 0 { found = 1 } END { exit !found }' "$RESTORE/tappa-$STAMP.manifest" \
    || empty="$empty $t"
done
[ -z "$empty" ] || die "the dump has no row in these append-only tables, so the restore would be judged on empty tables:$empty -- $FIXTURE no longer writes them"
awk '$1 == "feed_counter" && $3 > 0 { found = 1 } END { exit !found }' "$RESTORE/tappa-$STAMP.manifest" \
  || die "no feed counter in the dump stands past 0, so pg-restore-verify.sh section 6 would judge counters nothing ever moved -- $FIXTURE no longer writes a punch"
lap "backup: tappa-$STAMP.sql.gz and its manifest (source removed); every append-only table has rows, a feed counter moved"

# =================================================================================================
# 3. THE RESTORE AND THE VERDICT -- the runbook's blocks, in order, in one shell, as an operator
#    pastes them: (d2) target, (d3) B2 + B3 + B3's narrowing, (d3b) B4's counter step, (d4) B4's
#    pg-restore-verify.sh, (d5) cleanup. (d1) is the R2 download and its sha256, replaced here by
#    step 2's own file (pg-restore-verify.sh section 1 checks the sha256 against the manifest).
#    `set -e`: a block that fails stops the chain; the runbook says "do not go on" in prose.
# =================================================================================================
{
  echo 'set -euo pipefail'
  printf '%s\n' "$D2" "$D3" "$D3B" "$D4" "$D5"
} | sed "s/tappa-drill/$DRILL/g" >"$WORK/drill.sh"
# An emptied environment: only what the blocks use. DOCKER_HOST is the endpoint this shell's docker
# talks to (Docker Desktop's context lives under the real HOME, which the blocks do not get).
DOCKER_ENDPOINT="$(docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null || true)"
[ -n "$DOCKER_ENDPOINT" ] || DOCKER_ENDPOINT="${DOCKER_HOST:-unix:///var/run/docker.sock}"
mkdir -m 700 "$WORK/home"
env -i PATH="$PATH" HOME="$WORK/home" KUBECONFIG=/dev/null DOCKER_HOST="$DOCKER_ENDPOINT" \
  RESTORE="$RESTORE" STAMP="$STAMP" bash "$WORK/drill.sh" &
drill=$!
waited=0
while kill -0 "$drill" 2>/dev/null; do
  if [ "$waited" -ge "$DRILL_DEADLINE" ]; then
    kill "$drill" 2>/dev/null || true
    die "the runbook's blocks did not finish within $DRILL_DEADLINE s (an ordinary run takes ~20 s): (d2)'s wait for the target never ends if its init failed -- docker logs $DRILL-db"
  fi
  sleep 1; waited=$((waited + 1))
done
if wait "$drill"; then rc=0; else rc=$?; fi
[ "$rc" -eq 0 ] || die "the runbook's blocks stopped with exit $rc (read the block output above: a FAIL line is pg-restore-verify.sh's, 'restore exit' never printed is (d3)'s restore)"
lap "PASS: backup -> B YOLU restore -> counter step -> pg-restore-verify.sh exit 0"
