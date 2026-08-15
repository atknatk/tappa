#!/bin/sh
# Tappa -- the PRODUCTION-ONLY delta over scripts/db-init/01-roles.sql (M8-02).
#
# 🔴 WHY THIS FILE IS FOUR LINES AND NOT A COPY OF THE ROLES SQL. The roles, the
# grants, the default privileges and the extensions are defined ONCE, in
# scripts/db-init/01-roles.sql, and this deployment mounts THAT FILE BYTE FOR BYTE
# (the ConfigMap is built with --from-file over the repository path; see
# deploy/README.md and .github/workflows/deploy.yml). A second, production copy of
# that SQL is exactly the "second representation nothing keeps in sync" defect this
# repository has already paid for three times -- and here the two copies would drift
# on the rules that make RLS mean anything: NOBYPASSRLS, NOSUPERUSER, and which
# tables tappa_resolver may see.
#
# What that file deliberately does NOT do is let anybody in: it creates tappa_app
# NOLOGIN and with no password at all. Opening the door is this script's whole job,
# and it runs after it (docker-entrypoint-initdb.d executes in lexical order).
#
# 🔴 THAT SPLIT IS THE FIX FOR A MEASURED FAIL-OPEN (2026-08-15, security audit).
# Until this round 01-roles.sql created the role with `LOGIN PASSWORD 'tappa'` and
# this script REPLACED the password. If this script failed for any reason — an empty
# TAPPA_APP_PASSWORD, an OOM kill, an eviction, a node reboot mid-init — the
# container exited 1 with PGDATA ALREADY POPULATED. The pod restarts, postgres's
# entrypoint skips initdb and every init script entirely, the server comes up
# healthy, and tappa_app is alive with a password published in this repository —
# permanently, and silently. RLS does not stop that: the policies read
# NULLIF(current_setting('app.tenant_id', true),'')::uuid, and app.tenant_id is a
# session GUC that any role may set for itself.
#
# Now the same failure leaves tappa_app UNABLE TO LOG IN AT ALL. The application
# fails to start (loud) instead of running on a known password (silent), and there
# is no state in which a repository-published credential is live. Measured both
# ways; this task's report carries the before/after runs.
#
# ⚠️ IT RUNS ONLY ON AN EMPTY DATA DIRECTORY. postgres's entrypoint executes
# /docker-entrypoint-initdb.d/* exactly once, when initdb creates the cluster.
# Changing TAPPA_APP_PASSWORD in the Secret afterwards does NOT reach the role;
# that takes an explicit ALTER ROLE (deploy/README.md, rotation).
#
# 🔴 THE PASSWORD IS PASSED AS A psql VARIABLE, NOT INTERPOLATED BY THE SHELL.
# :'app_password' is psql's own quoting: it escapes the value as a string literal,
# so a password containing a quote is a password and not a SQL fragment. The
# here-document is single-quoted ('EOSQL') so the shell expands nothing inside it.
# And the value is never echoed -- it does not reach the container's log.
set -eu

if [ -z "${TAPPA_APP_PASSWORD:-}" ]; then
	echo "02-app-password.sh: TAPPA_APP_PASSWORD is empty. Refusing: tappa_app is created NOLOGIN by 01-roles.sql and only this script opens it, so the database will come up with the application unable to connect at all. That is deliberate — it is the fail-CLOSED half of the fix recorded above. Set the secret key TAPPA_APP_PASSWORD and re-create the volume." >&2
	exit 1
fi

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
	-v app_password="$TAPPA_APP_PASSWORD" <<-'EOSQL'
	ALTER ROLE tappa_app LOGIN PASSWORD :'app_password';
EOSQL

echo "02-app-password.sh: tappa_app can now log in (password not logged)"
