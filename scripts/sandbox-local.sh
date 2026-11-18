#!/usr/bin/env bash
# Tappa -- `make sandbox-local`: a SANDBOX build's database and configuration on the developer's own
# machine (M11 API-14; deploy/README.md "Sandbox (API-11)" §9, "Yerel sandbox").
#
# WHAT IT DOES, in this order, and nothing else:
#   1. refuses unless both DSNs are on a loopback host, the owner role sees every row (superuser or
#      BYPASSRLS -- scripts/rotate-kek.sh's guard), and SANDBOX_DB is a new tappa_sbx_* name;
#   2. CREATE DATABASE "$SANDBOX_DB" in the same cluster (the roles are the cluster's, from
#      scripts/db-init/01-roles.sql, already there);
#   3. gives the new database the PER-DATABASE half of 01-roles.sql -- CONNECT and USAGE to the
#      application roles, the owner's default privileges, pgcrypto and citext -- and NOTHING of
#      03-dev-production-default-acl.sql: a sandbox is production-shaped (deploy/sandbox/ mounts
#      01-roles.sql alone);
#   4. migrates it (goose up);
#   5. writes .sandbox-local/$SANDBOX_DB.env (mode 600, git-ignored): TAPPA_ENV=sandbox, the
#      three hosts (localhost, api.localhost, ops.localhost -- loopback names, RFC 6761), the app and
#      operator DSNs of the new database (and its owner DSN under SANDBOX_OWNER_DSN, a name the
#      server does not read -- for the seed alone), and SEVEN FRESH KEYS (openssl rand), each its
#      own value, none printed.
#
# IT NEVER DROPS OR OVERWRITES: an existing database or env file is a refusal (pick another name).
# Nothing it writes leaves the machine; no value of a key or a password reaches the terminal.
#
# The DSNs come in as APP_DSN and OWNER_DSN (the Makefile hands it .env's two database variables):
# the compose development cluster, whose tappa_app and tappa_operator both carry the development
# password (scripts/db-init/02-dev-only-password.sh) -- the operator DSN is the app DSN with the
# user swapped, which is true of that cluster and of no deployed one.
set -euo pipefail
cd "${0%/*}/.."

die() {
	echo "sandbox-local: $*" >&2
	exit 1
}

DB=${SANDBOX_DB:-tappa_sbx_local}
PORT=${SANDBOX_PORT:-8090}
GOOSE=${GOOSE:-goose}
[[ $DB =~ ^tappa_sbx_[a-z0-9_]{1,40}$ ]] || die "SANDBOX_DB must be tappa_sbx_ followed by 1-40 of a-z, 0-9, _ (a new database, never the development one)"
[[ $PORT =~ ^[0-9]{4,5}$ ]] || die "SANDBOX_PORT must be a port number"
: "${APP_DSN:?APP_DSN (the application DSN, .env's DATABASE_URL) is required}"
: "${OWNER_DSN:?OWNER_DSN (the owner DSN, .env's migration DSN) is required}"

# hostport DSN: the host[:port] of a postgres:// URL.
hostport() {
	local rest=${1#*://}
	rest=${rest%%/*}
	rest=${rest%%\?*}
	echo "${rest##*@}"
}
# with_db DSN NAME: the same DSN naming another database.
with_db() {
	local dsn=$1 name=$2 head tail
	head=${dsn%%\?*}
	tail=
	[[ $dsn == *\?* ]] && tail="?${dsn#*\?}"
	echo "${head%/*}/$name$tail"
}
# loopback DSN: a postgres:// URL whose ONE host is a loopback name. libpq lets a query parameter
# (host=, hostaddr=) override the authority -- `postgres://u@localhost:1/db?host=10.0.0.5` connects to
# 10.0.0.5 -- and reads a keyword=value string with no authority at all, so both shapes are refused
# rather than parsed, and so is a host list (a,b).
loopback() {
	local dsn=$1 h q
	case $dsn in postgres://* | postgresql://*) ;; *) return 1 ;; esac
	q=
	[[ $dsn == *\?* ]] && q=${dsn#*\?}
	# Case-insensitively and in the shell itself (bash 3.2 has no ${q,,}): an external tool that is
	# missing would leave q empty and open the gate.
	shopt -s nocasematch
	case "&$q" in *'&host='* | *'&hostaddr='*)
		shopt -u nocasematch
		return 1
		;;
	esac
	shopt -u nocasematch
	h=$(hostport "$dsn")
	[[ $h == *,* ]] && return 1
	h=${h%:*}
	case $h in localhost | 127.0.0.1 | '[::1]') return 0 ;; esac
	return 1
}
loopback "$APP_DSN" || die "APP_DSN is not a postgres:// URL on one loopback host (no host= or hostaddr= in its query): a local sandbox lives on this machine only"
loopback "$OWNER_DSN" || die "OWNER_DSN is not a postgres:// URL on one loopback host (no host= or hostaddr= in its query): a local sandbox lives on this machine only"
command -v psql >/dev/null || die "psql is required"
command -v openssl >/dev/null || die "openssl is required"

ENV_DIR=.sandbox-local
ENV_FILE=$ENV_DIR/$DB.env
[[ -e $ENV_FILE ]] && die "$ENV_FILE exists: this name was used before -- pick another SANDBOX_DB (nothing is overwritten)"

[[ $(psql -X "$OWNER_DSN" -At -v ON_ERROR_STOP=1 -c "SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user") == t ]] ||
	die "OWNER_DSN's role is neither a superuser nor BYPASSRLS: it cannot set up a database whose tables carry FORCE ROW LEVEL SECURITY"
[[ -z $(psql -X "$OWNER_DSN" -At -v ON_ERROR_STOP=1 -c "SELECT 1 FROM pg_database WHERE datname = '$DB'") ]] ||
	die "database $DB exists: pick another SANDBOX_DB (nothing is dropped)"
[[ $(psql -X "$OWNER_DSN" -At -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM pg_roles WHERE rolname IN ('tappa_app', 'tappa_operator', 'tappa_resolver', 'tappa_opdefiner', 'tappa_holdingdefiner', 'tappa_feedwriter', 'tappa_identitydefiner')") == 7 ]] ||
	die "the cluster lacks the application roles of scripts/db-init/01-roles.sql: this is not the compose development cluster"

echo "sandbox-local: creating database $DB"
psql -X "$OWNER_DSN" -q -v ON_ERROR_STOP=1 -c "CREATE DATABASE $DB"
NEW_OWNER=$(with_db "$OWNER_DSN" "$DB")
NEW_APP=$(with_db "$APP_DSN" "$DB")
NEW_OPERATOR=${NEW_APP/tappa_app:/tappa_operator:}
[[ $NEW_OPERATOR != "$NEW_APP" ]] || die "APP_DSN does not name tappa_app with a password; cannot derive the operator DSN"

# The per-database half of scripts/db-init/01-roles.sql, for this database.
psql -X "$NEW_OWNER" -q -v ON_ERROR_STOP=1 <<SQL
GRANT CONNECT ON DATABASE $DB TO tappa_app;
GRANT CONNECT ON DATABASE $DB TO tappa_operator;
GRANT USAGE ON SCHEMA public TO tappa_app, tappa_resolver, tappa_operator, tappa_opdefiner, tappa_holdingdefiner, tappa_feedwriter, tappa_identitydefiner;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public GRANT SELECT, INSERT ON TABLES TO tappa_app;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO tappa_app;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;
SQL

echo "sandbox-local: migrating $DB"
read -ra goose <<<"$GOOSE" # the Makefile hands "go run github.com/pressly/goose/v3/cmd/goose@<pin>"
"${goose[@]}" -dir db/migrations postgres "$NEW_OWNER" up >/dev/null

key() { openssl rand -base64 32; }
mkdir -p "$ENV_DIR"
chmod 700 "$ENV_DIR"
(
	umask 077
	cat >"$ENV_FILE" <<ENV
# $ENV_FILE -- written by scripts/sandbox-local.sh. LOCAL ONLY, never committed (.gitignore).
TAPPA_ENV=sandbox
TAPPA_ADDR=127.0.0.1:$PORT
TAPPA_BASE_URL=https://localhost:$PORT
TAPPA_API_HOST=api.localhost
TAPPA_OPERATOR_HOST=ops.localhost
TAPPA_TRUSTED_PROXIES=
TAPPA_WEBHOOK_BLOCKED_ADDRESSES=192.0.2.1
TAPPA_RESET_DELIVERY=none
TAPPA_INVITE_DELIVERY=panel
TAPPA_ADMIN_IDENTITY=on
TAPPA_BUSINESS_DAY_FROM=2026-10-22T16:30:00Z
TAPPA_RETENTION_YEARS=1
TAPPA_GPS_RADIUS_M=150
TAPPA_DEBOUNCE_SECONDS=60
TAPPA_FRESHNESS_SECONDS=180
TAPPA_LOG_LEVEL=info
TAPPA_DEV_TOOLS=
DATABASE_URL=$NEW_APP
# The owner DSN is NOT the server's (it never is -- cmd/tappa's packaging test): the server reads no
# variable of this name; make sandbox-local-seed hands it to the seed as the seed's owner DSN.
SANDBOX_OWNER_DSN=$NEW_OWNER
TAPPA_OPERATOR_DATABASE_URL=$NEW_OPERATOR
TAPPA_SESSION_HMAC_KEY=$(key)
TAPPA_TAG_KEK=$(key)
TAPPA_TAG_KEK_PREVIOUS=
TAPPA_INVITE_HMAC_KEY=$(key)
TAPPA_API_KEY_HMAC_KEY=$(key)
TAPPA_OAUTH_HMAC_KEY=$(key)
TAPPA_WEBHOOK_KEK=$(key)
TAPPA_OPERATOR_TOTP_KEK=$(key)
TAPPA_OPERATOR_TOKEN_HMAC_KEY=$(key)
TAPPA_VAPID_PUBLIC_KEY=
TAPPA_VAPID_PRIVATE_KEY=
ENV
)
cat <<NEXT
sandbox-local: ready -- database $DB, configuration $ENV_FILE (mode 600; no value printed).
  Next (deploy/README.md, "Sandbox (API-11)" §9):
    make sandbox-local-run  SANDBOX_DB=$DB   # builds bin/tappa-sandbox and serves 127.0.0.1:$PORT
    make sandbox-local-seed SANDBOX_DB=$DB TAPPA_TIMESHEET_SEED_TENANT=<business id>
    panel     http://localhost:$PORT/signup
    operator  http://ops.localhost:$PORT/operator
    API       http://api.localhost:$PORT/v1/
NEXT
