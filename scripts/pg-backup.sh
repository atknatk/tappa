#!/bin/sh
# Tappa — take a logical backup of the production database AND VERIFY ITS CONTENTS.
# Runs as the first working container of deploy/k8s/50-backup.yaml's CronJob; the
# shipping half is scripts/pg-backup-ship.sh. Restoring is deploy/README.md.
#
# 🔴 THE ONE FAILURE THIS FILE EXISTS TO MAKE IMPOSSIBLE: A BACKUP THAT SUCCEEDS AND
# CONTAINS NOTHING. Every tenant-scoped table in this schema carries both ENABLE and
# FORCE ROW LEVEL SECURITY (CLAUDE.md §6) and every policy reads
# NULLIF(current_setting('app.tenant_id', true), '')::uuid. FORCE means the policies
# apply to the table OWNER as well, so a connection that has not set app.tenant_id
# sees ZERO rows in every one of them. Measured on this repository's own database on
# 2026-08-16, with 231832 rows in public.transactions:
#
#   psql as tappa_owner (superuser)          -> tenants 181546, transactions 231832
#   psql as tappa_app,   no app.tenant_id    -> tenants 0,      transactions 0
#
# What pg_dump does with that is the part worth knowing, because it is NOT what the
# obvious guess says. pg_dump sets `row_security = off` for its own session by
# default, and a role that cannot bypass RLS then gets a HARD ERROR rather than an
# empty result:
#
#   pg_dump -U tappa_app --data-only -t transactions
#     -> exit 1, "ERROR: query would be affected by row-level security policy for
#        table \"transactions\"", 0 data rows                       (FAIL-CLOSED ✅)
#
#   pg_dump -U tappa_app --data-only -t transactions --enable-row-security
#     -> EXIT 0, 966 bytes, 0 data rows                        (SILENT EMPTY BACKUP)
#
#   ...same, with app.tenant_id set to one tenant
#     -> EXIT 0, 17262 of 231832 rows                        (SILENT PARTIAL BACKUP)
#
# So the dangerous artifact is real, it is reachable with one extra flag, and exit 0
# does not distinguish it from a good backup. This script closes it three ways, in
# increasing order of strength:
#   (1) it never passes --enable-row-security, so the failure above is a hard error;
#   (2) it REFUSES TO START unless the connected role is rolsuper OR rolbypassrls
#       (step 1 below) — i.e. it will not even reach pg_dump as a blindable role;
#   (3) it counts the rows INSIDE the finished dump and compares them with the live
#       database, so a dump that is empty where the database is not is a red job
#       even if some future change reintroduces the flag.
# (1) and (2) are structural. (3) is the belt that survives someone editing (1).
#
# 🔴 AND THE OPPOSITE MISTAKE IS JUST AS EASY: A HEALTHY SYSTEM IS EMPTY. A fresh
# install has zero rows in every table until the first business signs up, and a check
# that demanded "more than zero rows" would fail every correct backup of a new
# deployment. That is why nothing here asserts a floor; the assertion is EQUALITY OF
# SHAPE with the live database — same tables, and no table that is populated live and
# empty in the dump.
#
# POSIX sh on purpose: this runs in postgres:17-alpine, whose /bin/sh is busybox ash.
# No bashisms, no process substitution, and NO PIPELINES whose exit status matters —
# `set -eu` cannot see into the left-hand side of a pipe without pipefail. Counting is
# done with awk and never with `grep -c`, because grep exits 1 when it matches nothing
# and "matches nothing" is exactly the healthy-empty-database case that would then
# kill the script under `set -e`.
set -eu

PGHOST="${PGHOST:-tappa-postgres}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-tappa_owner}"
PGDATABASE="${PGDATABASE:-tappa}"
STAGE="${BACKUP_STAGE_DIR:-/staging}"
export PGHOST PGPORT PGUSER PGDATABASE
# libpq reads PGPASSWORD from the environment. It is never echoed, never put on a
# command line and never written to the manifest (CLAUDE.md §4.7).
: "${PGPASSWORD:?PGPASSWORD is not set: the CronJob passes it from secret/tappa-secrets key POSTGRES_PASSWORD}"

PSQL="psql -X -q -A -t -v ON_ERROR_STOP=1"

# ⚠️ A COUNTED, ACKNOWLEDGED SHAPE IN THIS FILE (audit B5), written rather than
# silently carried: several assignments below take a command substitution under
# `set -eu` with no `|| fail` — SERVER_VERSION, TABLES, LIVE_POLICIES, and the manifest's
# per-table lookup. If one of those commands fails, the shell exits with the command's
# own stderr and WITHOUT one of this script's named messages. They are left as they are
# for one reason and it is a measured one: every one of them runs AFTER the role gate
# has already opened a successful session to this database, so the connection is proven
# before they run; the paths where a first contact can fail (unreachable host, wrong
# password, missing PGPASSWORD, unwritable staging) DO carry named messages, and each
# was exercised. The same shape in scripts/pg-backup-ship.sh was NOT acceptable and was
# fixed, because there it guarded the config file itself — the first thing touched.
# Also acknowledged: the table-existence check below interpolates a table name into a
# grep pattern, i.e. treats it as a regex. Harmless for this schema (every table name
# is [a-z_]+) and it would fail LOUD, not silent, if that ever changed.

fail() { echo "pg-backup: FAILED: $*" >&2; exit 1; }
say()  { echo "pg-backup: $*"; }

# ---------------------------------------------------------------------------------
# 0. Staging directory must exist and be writable BEFORE anything expensive runs.
#    A full or read-only volume otherwise surfaces halfway through a 900 MB dump.
# ---------------------------------------------------------------------------------
[ -d "$STAGE" ] || fail "staging directory $STAGE does not exist"
probe="$STAGE/.writable.$$"
# 🔴 `touch`, NOT `: > "$probe"`. Measured against a read-only mount: `:` is a POSIX
# SPECIAL BUILTIN, and a redirection failure on a special builtin makes the shell exit
# immediately — the `|| fail` never runs and the operator gets busybox's bare
# "can't create /staging/...: Read-only file system" instead of a sentence naming the
# volume. An external command's redirection failure is an ordinary non-zero status.
touch "$probe" 2>/dev/null || fail "staging directory $STAGE is not writable (read-only mount, wrong fsGroup, or the volume is full)"
rm -f "$probe"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
SQL="$STAGE/tappa-$STAMP.sql"
GZ="$SQL.gz"
MANIFEST="$STAGE/tappa-$STAMP.manifest"

# ---------------------------------------------------------------------------------
# 1. THE ROLE GATE. This is the check that makes an empty backup unreachable rather
#    than merely detectable. rolsuper OR rolbypassrls is exactly the set of roles for
#    which FORCE ROW LEVEL SECURITY does not apply — measured in scripts/db-init/
#    01-roles.sql's own comments and re-measured here:
#      tappa_owner    rolsuper=t rolbypassrls=t   -> sees every row
#      tappa_app      rolsuper=f rolbypassrls=f   -> sees zero rows
#      tappa_resolver rolsuper=f rolbypassrls=t   -> NOLOGIN, cannot connect at all
#    If someone ever repoints this job at tappa_app to be "safer", it stops here with
#    a message that says why, instead of writing a 1 KB file every night for a year.
# ---------------------------------------------------------------------------------
role_flags="$($PSQL -c "SELECT rolsuper::text || ' ' || rolbypassrls::text FROM pg_roles WHERE rolname = current_user;")" \
  || fail "cannot reach $PGUSER@$PGHOST:$PGPORT/$PGDATABASE (wrong password, no route, or the database is not up)"
[ -n "$role_flags" ] || fail "current_user is not in pg_roles; refusing to continue"
rolsuper="${role_flags% *}"
rolbypass="${role_flags#* }"
if [ "$rolsuper" != "true" ] && [ "$rolbypass" != "true" ]; then
  fail "role '$PGUSER' has rolsuper=$rolsuper rolbypassrls=$rolbypass. Every tenant table in this schema is FORCE ROW LEVEL SECURITY, so this role sees ZERO rows unless app.tenant_id is set, and a dump taken with it is either a hard error or (with --enable-row-security) a SILENT EMPTY BACKUP. Use tappa_owner."
fi
say "connected as $PGUSER (rolsuper=$rolsuper rolbypassrls=$rolbypass), row_security stays off"

SERVER_VERSION="$($PSQL -c "SHOW server_version;")"
DUMP_VERSION="$(pg_dump --version | awk '{print $NF}')"

# ---------------------------------------------------------------------------------
# 2. WHAT THE LIVE DATABASE CONTAINS. Nothing below is a hard-coded list: the table
#    set, the policy count and the RLS flags are all read from the catalog, so a
#    migration that adds a table extends this check on its own. A detector whose
#    scope is chosen by hand only ever catches the defect its author had in mind.
# ---------------------------------------------------------------------------------
TABLES="$($PSQL -c "SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind = 'r' ORDER BY 1;")"
TABLE_COUNT="$(printf '%s\n' "$TABLES" | awk 'NF{n++} END{print n+0}')"
if [ "$TABLE_COUNT" -eq 0 ]; then
  fail "database '$PGDATABASE' on $PGHOST has NO tables in schema public. If this is a brand-new install whose migration Job has not run yet, that is expected exactly once and this job will pass on the next schedule. Otherwise PGDATABASE/PGHOST point at the wrong database — which is the failure this check exists for, because an empty dump of the wrong database looks identical to a good one."
fi
LIVE_POLICIES="$($PSQL -c "SELECT count(*) FROM pg_policies WHERE schemaname = 'public';")"
LIVE_FORCE_RLS="$($PSQL -c "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relforcerowsecurity;")"
GOOSE_VERSION="$($PSQL -c "SELECT coalesce(max(version_id)::text, 'none') FROM goose_db_version;" 2>/dev/null || echo "unreadable")"

say "live schema: $TABLE_COUNT tables, $LIVE_POLICIES policies, $LIVE_FORCE_RLS tables with FORCE RLS, goose $GOOSE_VERSION"

# ---------------------------------------------------------------------------------
# 3. THE DUMP.
#
#    --format=plain and NOT custom, deliberately: the acceptance criterion for this
#    task is that the CONTENTS are verified, and a plain dump can be counted by
#    streaming it. pg_restore -l lists objects but never row counts. Plain also means
#    a restore needs psql only, with no pg_dump/pg_restore version pairing to get
#    right at 04:00.
#
#    🔴 NO --clean AND NO --if-exists. A dump that begins with DROP TABLE is a loaded
#    gun in a directory an operator reaches for during an incident; the restore
#    procedure in deploy/README.md targets a FRESH, EMPTY database precisely so that
#    the artifact itself is incapable of destroying anything.
#
#    NOT --no-owner and NOT --no-acl either — those are not the defaults and this
#    file wants what the defaults give. Measured on this schema (2026-08-16):
#    the schema-only dump carries 18 ENABLE ROW LEVEL SECURITY, 18 FORCE ROW LEVEL
#    SECURITY, 18 CREATE POLICY, 105 GRANT, 12 REVOKE, 35 OWNER TO, 2 ALTER DEFAULT
#    PRIVILEGES and 2 CREATE EXTENSION statements. What it carries ZERO of is
#    CREATE ROLE — roles are cluster-wide objects and pg_dump never emits them. That
#    single gap is why the restore procedure starts from this repository's own
#    scripts/db-init/01-roles.sql instead of from the dump.
# ---------------------------------------------------------------------------------
STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
say "dumping to $SQL"
pg_dump --format=plain --no-password --file="$SQL" \
  || fail "pg_dump exited non-zero; the dump is incomplete and nothing will be shipped"
FINISHED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# ---------------------------------------------------------------------------------
# 4. VERIFY THE INSIDE OF THE ARTIFACT. Five checks, in the order in which a broken
#    dump most often breaks.
# ---------------------------------------------------------------------------------

# 4a. COMPLETENESS. pg_dump writes this trailer as its very last act, so its absence
#     is the signature of a dump cut short: disk full, OOM kill, node reboot, the
#     connection dropping mid-COPY. Every one of those leaves a large, plausible,
#     unusable file behind, and every one of them also makes pg_dump exit non-zero —
#     but this check costs nothing and does not depend on the exit status surviving a
#     future refactor into a pipeline.
tail -c 200 "$SQL" | grep -q 'PostgreSQL database dump complete' \
  || fail "the dump does not end with PostgreSQL's completion marker: it was truncated"

# 4b. THE TABLE SET. Every table that exists live must have a data section in the
#     dump. An empty table still gets one (a COPY header followed immediately by
#     \.), so this passes on a brand-new install and fails when a table was skipped
#     for want of a privilege.
missing=""
for t in $TABLES; do
  if ! grep -q "^COPY public\.$t " "$SQL"; then
    missing="$missing $t"
  fi
done
[ -z "$missing" ] || fail "the dump has no data section for:$missing"

# 4c. ROW COUNTS INSIDE THE DUMP. Single pass, awk only. In COPY's text format a data
#     value can contain neither a raw newline nor a raw backslash (both are escaped),
#     so one line is exactly one row and a data line can never be the terminator.
#     No pipeline here on purpose: `set -eu` without pipefail cannot see a failure on
#     the left of a pipe, and this repository has paid for that shape before.
#
#     ⚠️ ONE COUNTED LIMIT, WRITTEN RATHER THAN CLOSED: the state machine keys on a
#     line BEGINNING with `COPY public.`. Today no data line can begin that way — the
#     terminator is escaped, and no table in this schema has a free-text FIRST column
#     (every one starts with a uuid, a char(14) uid or an integer). A future migration
#     whose first column is user-supplied text would make a row spelled exactly like a
#     COPY header reachable. The failure would still be LOUD rather than silent: the
#     rest of that table would be attributed to a table name that does not exist, and
#     check 4b reports the real table as having no data section. Closing it properly
#     means following the expected table order instead of the line prefix; not done,
#     and this is the record that it was seen.
awk '
  /^COPY public\./ { name = $2; sub(/^public\./, "", name); inside = 1; rows[name] = 0; next }
  inside && $0 == "\\." { inside = 0; next }
  inside { rows[name]++ }
  END { for (n in rows) printf "%s %d\n", n, rows[n] }
' "$SQL" > "$STAGE/.dumpcounts.$$"

# 4d. LIVE COUNTS, taken AFTER the dump. The dump is a single repeatable-read
#     snapshot from before this moment, so a live count can legitimately be HIGHER
#     (rows written while the dump ran) — and for public.transactions it can only ever
#     be higher, because those rows are immutable and append-only (CLAUDE.md §4.3).
#     The comparison below therefore does NOT demand equality. It demands the one
#     thing drift cannot produce: no table that is populated live and empty in the
#     dump. That is the exact shape of the RLS and privilege failures above.
countq="$($PSQL -c "SELECT string_agg(format('SELECT %L AS t, count(*) AS n FROM public.%I', c.relname, c.relname), ' UNION ALL ') FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind = 'r';")"
$PSQL -F' ' -c "$countq" > "$STAGE/.livecounts.$$"

blind=""
total_dump=0
total_live=0
# Read from a REDIRECT and not from a pipe: a `while read` on the right of a pipe
# runs in a subshell and the totals below would be discarded at the loop's end.
while read -r t live; do
  [ -n "$t" ] || continue
  dump="$(awk -v t="$t" '$1 == t { print $2; found = 1 } END { if (!found) print "-1" }' "$STAGE/.dumpcounts.$$")"
  [ "$dump" != "-1" ] || fail "internal: table $t has no counted data section (4b should have caught this)"
  total_dump=$(( total_dump + dump ))
  total_live=$(( total_live + live ))
  # ⚠️ THE COMPARISON IS GUARDED BECAUSE ITS UNGUARDED FORM FAILS OPEN (audit B6).
  # `[ "$live" -gt 0 ]` with an empty $live is not false — it is an ERROR, and `[`
  # returning 2 makes the `&&` chain skip the table, i.e. a table whose live count
  # could not be read would quietly never be reported as blind. Unreachable today
  # (psql -A -F' ' always emits two fields for these names) and cheap to close, which
  # is the whole argument for closing it.
  case "$live$dump" in
    ''|*[!0-9]*) fail "internal: unparsable counts for table '$t' (live='$live' dump='$dump'); refusing to judge the dump on numbers I cannot read" ;;
  esac
  if [ "$live" -gt 0 ] && [ "$dump" -eq 0 ]; then
    blind="$blind $t(live=$live,dump=0)"
  fi
done < "$STAGE/.livecounts.$$"

if [ -n "$blind" ]; then
  fail "these tables have rows in the database and NONE in the dump:$blind — this is what an RLS-blinded or privilege-starved dump looks like. Do not ship it."
fi

# 4e. THE TENANT BOUNDARY ITSELF (CLAUDE.md §4.5/§6). A restored database that has
#     the rows but not the policies is a database with no tenant isolation, and that
#     is a worse outcome than no restore at all. pg_dump does carry these; this check
#     is here so that a future flag change (--no-security-labels, a hand-edited
#     dump, a partial schema) cannot take them away quietly.
dump_enable="$(awk '/ENABLE ROW LEVEL SECURITY/ { n++ } END { print n+0 }' "$SQL")"
dump_force="$(awk '/FORCE ROW LEVEL SECURITY/ { n++ } END { print n+0 }' "$SQL")"
dump_policy="$(awk '/^CREATE POLICY /  { n++ } END { print n+0 }' "$SQL")"
dump_grant="$(awk '/^GRANT /          { n++ } END { print n+0 }' "$SQL")"
[ "$dump_policy" -ge "$LIVE_POLICIES" ] \
  || fail "the database has $LIVE_POLICIES row-level-security policies and the dump carries $dump_policy"
[ "$dump_force" -ge "$LIVE_FORCE_RLS" ] \
  || fail "the database has $LIVE_FORCE_RLS tables with FORCE ROW LEVEL SECURITY and the dump carries $dump_force"
[ "$dump_grant" -gt 0 ] \
  || fail "the dump carries no GRANT statements: restoring it would produce a database tappa_app cannot read"

say "verified: $TABLE_COUNT/$TABLE_COUNT tables present, $total_dump rows in the dump ($total_live live now), $dump_policy policies, $dump_enable ENABLE / $dump_force FORCE RLS, $dump_grant GRANTs"

# ---------------------------------------------------------------------------------
# 5. COMPRESS, CHECKSUM, MANIFEST. gzip after verification rather than before: the
#    checks above read the plain file once each, and a corrupt gzip would hide them.
# ---------------------------------------------------------------------------------
BYTES_SQL="$(wc -c < "$SQL" | tr -d ' ')"
gzip -9 "$SQL" || fail "gzip failed; the staging volume is probably full"
BYTES_GZ="$(wc -c < "$GZ" | tr -d ' ')"
SHA="$(sha256sum "$GZ" | awk '{print $1}')"

{
  echo "tappa-backup-manifest 1"
  echo "file $(basename "$GZ")"
  echo "sha256 $SHA"
  echo "bytes_gz $BYTES_GZ"
  echo "bytes_sql $BYTES_SQL"
  echo "started_at $STARTED_AT"
  echo "finished_at $FINISHED_AT"
  echo "host $PGHOST"
  echo "database $PGDATABASE"
  echo "dumped_as $PGUSER rolsuper=$rolsuper rolbypassrls=$rolbypass row_security=off"
  echo "server_version $SERVER_VERSION"
  echo "pg_dump_version $DUMP_VERSION"
  echo "goose_version $GOOSE_VERSION"
  echo "tables $TABLE_COUNT"
  echo "policies $dump_policy"
  echo "rls_enable $dump_enable"
  echo "rls_force $dump_force"
  echo "grants $dump_grant"
  echo "rows_total_dump $total_dump"
  echo "rows_total_live_after $total_live"
  echo "# per table: name rows_in_dump rows_live_after"
  while read -r t live; do
    [ -n "$t" ] || continue
    dump="$(awk -v t="$t" '$1 == t { print $2 }' "$STAGE/.dumpcounts.$$")"
    echo "table $t $dump $live"
  done < "$STAGE/.livecounts.$$"
} > "$MANIFEST"

rm -f "$STAGE/.dumpcounts.$$" "$STAGE/.livecounts.$$"

say "wrote $(basename "$GZ") ($BYTES_GZ bytes) and $(basename "$MANIFEST")"
say "sha256 $SHA"
# 🔴 THE MANIFEST'S started_at IS THE VALUE AN INCIDENT NEEDS. Restoring this file
# rewinds the database to that instant and destroys every attendance record written
# afterwards, which CLAUDE.md §4.3 makes irreproducible. deploy/README.md's restore
# procedure begins by measuring that window and refuses to proceed without it.
say "backup instant (restore rewinds to here): $STARTED_AT"
