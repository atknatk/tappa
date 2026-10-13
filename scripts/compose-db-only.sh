# shellcheck shell=bash
# Tappa -- SOURCED by scripts/seed.sh and scripts/db-reset.sh, never run on its own.
#
# WHY IT EXISTS (M11 API-1 review, 2026-10-13). Both scripts reach the database through
# `docker compose -f docker-compose.yml -p tappa`, i.e. ALWAYS the compose project's container
# published on localhost:5432 -- neither reads DATABASE_URL. That pin is right (each script's
# header measures why) but it is SILENT: somebody whose environment points DATABASE_URL at
# another database -- a git worktree with its own Postgres, as M11 works -- runs `make seed` or
# `make db-reset` believing it acts on THEIR database, and it writes demo rows into, or deletes
# the volume of, the shared one instead. It happened once already: a seed run meant for M11's
# own database wrote into the shared development database.
#
# SO THE TWO MUST AGREE OR THE SCRIPT REFUSES. DATABASE_URL unset is fine (the scripts never
# needed it); set to the compose address is fine; anything else -- another host or port, a
# keyword DSN this cannot read -- exits 1 before docker is even looked up, so a refused run has
# touched nothing. One definition for both scripts: two copies of an address list drift.
#
# The accepted addresses are what docker-compose.yml publishes ("5432:5432") as .env.example and
# ci.yml spell it. Only DATABASE_URL is read: seed.sh's header says why the migration
# connection variable is not named under scripts/.

# compose_db_only WHO WHAT: refuse unless DATABASE_URL is unset or names the compose database.
# WHO is the script's name, WHAT its verb ("seeds", "rebuilds").
compose_db_only() {
	local who=$1 what=$2 url=${DATABASE_URL:-} rest hostport
	[[ -z $url ]] && return 0
	rest=${url#*://}
	if [[ $rest == "$url" ]]; then
		hostport='not a postgres:// URL'
	else
		rest=${rest%%/*}
		rest=${rest%%\?*}
		hostport=${rest##*@}
	fi
	case $hostport in
	localhost | localhost:5432 | 127.0.0.1 | 127.0.0.1:5432 | '[::1]' | '[::1]:5432') return 0 ;;
	esac
	echo "$who only $what the compose database (localhost:5432); your DATABASE_URL points elsewhere ($hostport)." >&2
	echo "  Refusing, before docker is touched: this script cannot reach that database, and would act on the compose one instead." >&2
	echo "  Unset DATABASE_URL, or point it at localhost:5432, to act on the compose database." >&2
	exit 1
}
