#!/usr/bin/env bash
# Tappa -- rebuilds the LOCAL development database from nothing.
#
# WHAT REPLACED WHAT, AND WHY (backlog T22). `make db-reset` used to run
# `goose reset`, i.e. every migration's Down in reverse. Measured on the working
# database: it does not finish. 00013's Down restores 00004's three-value status
# CHECK and location_id's NOT NULL, and accumulated development data violates
# both (unassigned plaques; plaques reported lost while still in stock). 00013's
# own Down block predicted this on 2026-08-09 and prescribed hand-editing the
# offending rows first -- which is a rescue, not a procedure.
#
# 🔴 THE FIX COULD NOT BE "make 00013's Down data-tolerant": an applied migration
# is not edited (CLAUDE.md section 6), and a NEW migration cannot help either,
# because `goose reset` walks Downs from the newest backwards and 00013's is
# still in that walk. So the target stops going through the migrations at all.
# Backlog T22 named exactly these two options; this is the second one.
#
# WHY THE VOLUME AND NOT A DROP SCHEMA. Three candidates were measured against
# each other before this was written:
#
#   (a) goose reset ................. broken, see above.
#   (b) drop every non-extension object in `public` .. works, keeps the container
#       up, and is the fastest -- but it PRESERVES whatever db-init state the
#       database already drifted into, and this machine has drifted: measured,
#       pg_default_acl here is still `tappa_app=arwd/tappa_owner` (the WIDE
#       default), while scripts/db-init/01-roles.sql has declared the NARROW
#       `SELECT, INSERT` since M8-02 phase E. So (b) would hand a developer a
#       database that says "from scratch" and is not -- and the drift it keeps is
#       one where tappa_app gets UPDATE and DELETE on every table a future
#       migration creates. It also has to enumerate object kinds, which goes
#       stale the day a migration adds one it does not list.
#   (c) THIS: remove the container and its volume, let the official postgres
#       entrypoint run scripts/db-init/* again. Complete by construction (there
#       is nothing left to enumerate), duplicates NOTHING from db-init (db-init
#       actually runs, so there is no second copy of the roles, grants, default
#       privileges or extensions to drift), and the result is bit-for-bit the
#       state a new developer or CI gets.
#
# ⚠️ IT IS NARROWER THAN WHAT IT REPLACES -- BUT THE RISK MOVED, IT DID NOT
# DISAPPEAR, AND THE FIRST VERSION OF THIS PARAGRAPH SAID OTHERWISE. It claimed
# "there is no connection string to point somewhere else". MEASURED on 2026-08-19,
# and the measurement is the whole reason the flags below exist: with nothing but
# COMPOSE_FILE inherited from the environment, this script -- started from this
# repository's own directory, and WITHOUT EVER OPENING this repository's
# docker-compose.yml -- stopped a FOREIGN compose project and deleted its data
# volume:
#
#   $ COMPOSE_FILE=<elsewhere>/composeprobe/docker-compose.yml ./scripts/db-reset.sh
#   db-reset: EVERY ROW IN THE LOCAL DATABASE IS ABOUT TO BE DESTROYED.
#    Volume composeprobe_decoy-data Removing
#    Volume composeprobe_decoy-data Removed
#   -> afterwards: ERROR: relation "important" does not exist
#
# `cd "$(dirname "$0")/.."` is no protection at all here: an ABSOLUTE COMPOSE_FILE
# ignores the working directory. Three variables are the same class --
# COMPOSE_FILE (wrong file), COMPOSE_PROJECT_NAME (right file, wrong project),
# and DOCKER_HOST / DOCKER_CONTEXT (right file, wrong DAEMON, i.e. another
# machine's volumes). `goose reset`'s danger was a connection string; this
# target's danger is compose's ambient configuration. Both arrive the same way:
# the Makefile does `-include .env` followed by a bare `export`, so every variable
# in the developer's shell and in .env is in scope for this script.
#
# SO THE TARGET IS PINNED RATHER THAN ASSUMED, in two independent places:
#
#   (1) `-f docker-compose.yml -p tappa` on EVERY compose call. Measured, with
#       COMPOSE_FILE pointing at the decoy and COMPOSE_PROJECT_NAME=evil:
#         with the flags ....... name=tappa, volume tappa-pgdata
#         without the flags .... name=evil,  volume decoy-data
#       `tappa` is not a guess and not the directory name: docker-compose.yml
#       declares `name: tappa`, so the pin here and what `make up` builds cannot
#       drift apart when somebody renames the checkout.
#
#   (2) THE DAEMON MUST BE THIS MACHINE -- AND THAT IS NOW MEASURED INSTEAD OF
#       INFERRED FROM THE ENDPOINT'S SCHEME. `docker context inspect` resolves the
#       EFFECTIVE endpoint -- measured, it honours DOCKER_HOST and DOCKER_CONTEXT
#       and not merely the current context (tcp://198.51.100.7:2375 and
#       unix:///var/run/docker.sock were both reported correctly) -- so tcp:// and
#       ssh:// are refused outright rather than warned about, because a removed
#       volume is not recoverable and a warning is not a decision.
#
#       🔴 BUT THE FIRST VERSION OF THIS PARAGRAPH STOPPED THERE AND CLAIMED "a
#       `unix://` endpoint is by definition a socket on THIS machine". That is
#       true about the SOCKET FILE and false about the DAEMON BEHIND IT, and an
#       audit measured the gap with a stand-in daemon listening on a local unix
#       socket -- exactly what `ssh -nNT -L /tmp/x.sock:/var/run/docker.sock
#       server` leaves behind, which is an ordinary setup and is the third
#       variable this header already names ("right file, wrong DAEMON"):
#
#         DOCKER_HOST=unix:///tmp/forwarded-remote-docker.sock
#           docker context inspect .. unix:///tmp/forwarded-remote-docker.sock
#           the scheme check ........ ACCEPTED
#           the banner .............. printed that endpoint and said the rows were
#                                     about to be destroyed
#           the next line would have been `down --volumes` on ANOTHER MACHINE.
#
#       WHY THIS IS NOT A NAME COMPARISON, measured before it was written. The
#       obvious fix is to compare `docker info --format '{{.Name}}'` with the local
#       hostname. It refuses a LEGITIMATE local daemon here:
#
#         stand-in remote daemon ..... Name = prod-db-01
#         real local Docker Desktop .. Name = docker-desktop
#         hostname ................... Evervas-MacBook-Pro
#
#       so it would break `make db-reset` for every Docker Desktop, colima,
#       OrbStack and Rancher Desktop developer, whose daemon lives in a VM that is
#       not named after the host. A name is a CLAIM in any case, and a forwarded
#       daemon is free to make any claim it likes.
#
#       SO THE GUARD ASKS FOR A FACT ONLY A DAEMON ON THIS FILESYSTEM CAN PRODUCE:
#       it makes the daemon hand back this checkout's own
#       scripts/db-init/01-roles.sql through the bind mount docker-compose.yml
#       already declares, and compares the BYTES with the file on disk. A daemon
#       elsewhere cannot hand that back: the absolute host path is resolved in ITS
#       filesystem. WHAT WAS ACTUALLY MEASURED HERE, and it is less than the full
#       matrix -- there was no second real machine to point at:
#         local daemon, path present ..... the two files are byte-identical
#         local daemon, path absent ...... "mounts denied: The path ... is not
#                                           shared from the host" (Docker Desktop)
#         stand-in daemon on a forwarded
#           unix socket .................. the run fails and the guard refuses
#       ⚠️ NOT MEASURED: a real remote Linux dockerd, which would instead create an
#       empty directory at the missing path and make `cat` fail with "No such file
#       or directory". That is the documented behaviour of a missing bind source,
#       not an observation taken here, and it is written as such.
#       Cost, measured: ~1.6 s and one throwaway container, on the same pinned
#       -f/-p target as everything below -- so what is proven local is the daemon
#       that is ACTUALLY about to be asked to delete tappa-pgdata, not some other
#       one. The image it needs is the image the very next command needs.
#
#       ⚠️ WHAT IT STILL DOES NOT CATCH, said rather than left to be found: a
#       daemon that holds a byte-identical copy of this repository at the same
#       absolute path would pass. That is a far narrower hole than "any unix
#       socket", and it is the honest limit of a filesystem-identity probe.
#
# The same reasoning scripts/seed.sh gives for running psql from the container
# applies here, and it is why no migration-role variable is named in this file.
#
# WHAT IT DESTROYS: everything in the local Postgres volume. That is the
# documented contract of the target ("SIFIRDAN kur -- TUM VERIYI SILER") and the
# whole point: backlog T6 (311 129 tenants rows of test residue), T15 (the
# canonical-uid constraint could not be validated), T26 (contradictory employees
# fixtures) and T52 (audit rows written by mutation runs, unremovable because
# audit_log is append-only) have no other exit.
set -euo pipefail
cd "$(dirname "$0")/.."

# Two failures, two messages -- the same rule seed.sh states: "docker missing" and
# "postgres not running" are different problems and one message sends the hunt the
# wrong way. Here the second case is not an error at all: a stopped database is a
# perfectly good starting point for a reset.
if ! command -v docker >/dev/null 2>&1; then
	echo "db-reset: 'docker' is not on PATH -- this target rebuilds the compose database." >&2
	exit 1
fi

# GUARD (2) FROM THE HEADER, FIRST HALF: which SCHEME. Answered before anything is
# printed, because the warning lines below would otherwise describe the wrong
# machine. This half is cheap and gives tcp:// and ssh:// their own message; it
# does NOT establish that the daemon is local -- the probe further down does that,
# and the header records why the scheme alone was not enough.
endpoint="$(docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null || true)"
case "$endpoint" in
unix://*) ;;
"")
	echo "db-reset: cannot determine which docker daemon this would talk to" >&2
	echo "          ('docker context inspect' produced nothing). Refusing: this target" >&2
	echo "          deletes a volume, and an unidentified target is not an acceptable one." >&2
	exit 1
	;;
*)
	echo "db-reset: the docker endpoint is '$endpoint', which is NOT a local unix socket." >&2
	echo "          DOCKER_HOST or DOCKER_CONTEXT is aiming this at another machine, whose" >&2
	echo "          volumes this target would DELETE. Refusing." >&2
	echo "          Unset DOCKER_HOST / DOCKER_CONTEXT (or 'docker context use') and retry." >&2
	exit 1
	;;
esac

# GUARD (1) FROM THE HEADER: which FILE and which PROJECT. Every compose call
# below goes through this, so there is one place to read and no call that can be
# forgotten. The flags beat COMPOSE_FILE and COMPOSE_PROJECT_NAME (measured --
# see the header), which `cd` alone does not.
#
# The name is held in ONE variable and printed from the SAME one below. The first
# draft wrote it twice -- once in the flag, once in the announcement -- and the
# announcement was immediately observed lying about which project was about to be
# destroyed. A banner that can disagree with the command is worse than no banner.
project=tappa
COMPOSE=(docker compose -f docker-compose.yml -p "$project")

# GUARD (2) CONTINUED -- THE PART THE SCHEME CHECK CANNOT ANSWER. Runs through the
# SAME pinned COMPOSE array, so the daemon proven local is the one that will be
# asked to delete the volume; and it runs BEFORE the destruction banner, so a
# refused run never announces a destruction that is not going to happen.
#
# The probe file is scripts/db-init/01-roles.sql because docker-compose.yml already
# bind-mounts that directory and this target's whole premise is that the entrypoint
# re-runs it. If it is ever renamed, this guard fails loudly rather than quietly
# passing -- the correct direction for a target that deletes a volume.
probe_tmp="$(mktemp -d)"
trap 'rm -rf "$probe_tmp"' EXIT INT TERM
if ! "${COMPOSE[@]}" run --rm --no-deps --entrypoint cat db \
	/docker-entrypoint-initdb.d/01-roles.sql >"$probe_tmp/seen" 2>"$probe_tmp/err"; then
	echo "db-reset: the docker daemon at '$endpoint' could not read this checkout's" >&2
	echo "          scripts/db-init/01-roles.sql through the compose bind mount, so it is" >&2
	echo "          NOT a daemon on this machine -- or it cannot run the database image at" >&2
	echo "          all. Either way this target would be deleting volumes it cannot" >&2
	echo "          identify. Refusing. What docker said:" >&2
	sed 's/^/            /' "$probe_tmp/err" >&2
	exit 1
fi
# THE ANNOUNCEMENT LIVES IN THE SUCCESS BRANCH, NOT IN THE BANNER BELOW -- the same
# rule the paragraph above `project=` states about the project name, applied to this
# claim. Measured while mutation-testing this file: with the announcement written as
# its own line after the check, DELETING THE WHOLE PROBE still left the script saying
# "that daemon handed back this checkout's own 01-roles.sql" -- a banner asserting a
# check that was no longer there. Inside the branch it cannot outlive the check.
if cmp -s scripts/db-init/01-roles.sql "$probe_tmp/seen"; then
	echo "db-reset: that daemon handed back this checkout's own scripts/db-init/01-roles.sql, so it is on this filesystem."
else
	echo "db-reset: the docker daemon at '$endpoint' answered with a DIFFERENT" >&2
	echo "          scripts/db-init/01-roles.sql than the one in this checkout." >&2
	echo "          That is another machine's filesystem (DOCKER_HOST / DOCKER_CONTEXT" >&2
	echo "          forwarding, or a socket forwarded over ssh), and its volumes are not" >&2
	echo "          this target's to delete. Refusing." >&2
	exit 1
fi

echo "db-reset: removing the compose database container AND its volume."
echo "db-reset: target is pinned: project '$project', file $(pwd)/docker-compose.yml, daemon $endpoint"
echo "db-reset: EVERY ROW IN THE LOCAL DATABASE IS ABOUT TO BE DESTROYED."

# --volumes removes the named volume declared by docker-compose.yml
# (tappa-pgdata) as well as the container. Project-scoped: it cannot touch a
# volume this compose file does not declare.
"${COMPOSE[@]}" down --volumes

# The entrypoint re-runs scripts/db-init/* on the now-empty PGDATA: roles,
# schema grants, default privileges, extensions, and the development password.
"${COMPOSE[@]}" up -d db

# READINESS IS NOT pg_isready, AND THE DIFFERENCE IS A RACE THIS SCRIPT WOULD
# OTHERWISE LOSE. While the init scripts run, the entrypoint has a TEMPORARY
# server up with listen_addresses='' -- it answers on the unix socket, so
# pg_isready (which is what `make up` waits on) can report "ready" in the middle
# of initialisation. Two things become true only when the REAL server is serving:
# it accepts TCP, and tappa_app can log in (01-roles.sql creates it NOLOGIN and
# 02-dev-only-password.sh grants LOGIN last). Connecting over TCP AS tappa_app
# tests both in one probe. No password is passed: initdb's default pg_hba trusts
# loopback, which 01-roles.sql already records as a measured fact.
echo -n "db-reset: waiting for db-init to finish"
ready=0
for _ in $(seq 1 90); do
	if "${COMPOSE[@]}" exec -T db \
		psql -h 127.0.0.1 -U tappa_app -d tappa --no-psqlrc -Atc 'SELECT 1' >/dev/null 2>&1; then
		ready=1
		break
	fi
	echo -n "."
	sleep 1
done
echo

if [[ $ready -ne 1 ]]; then
	echo "db-reset: the database did not finish initialising in 90s." >&2
	echo "          'docker compose -p tappa logs db' will say which init script failed;" >&2
	echo "          a failed init leaves tappa_app NOLOGIN, which is fail-closed." >&2
	exit 1
fi

echo "db-reset: empty database ready. Run 'make migrate seed' next (the Makefile target does)."
