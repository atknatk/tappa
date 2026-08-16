#!/bin/sh
# Tappa — prove that a RESTORED database is the database that was backed up, including
# the parts that are not rows. Run at the end of every restore; deploy/README.md's
# restore procedure calls it as its last step and does not consider the restore done
# until this exits 0.
#
#   usage:  PGPASSWORD=<owner pw> TAPPA_APP_PASSWORD=<app pw> \
#           scripts/pg-restore-verify.sh <dump.sql.gz> <dump.manifest>
#
# 🔴 WHY THIS FILE EXISTS: A RESTORE THAT LOADS EVERY ROW CAN STILL LOSE THE THINGS
# THAT MAKE THE ROWS SAFE, AND IT DOES SO SILENTLY. Measured on 2026-08-16 by
# restoring a real 2 118 896-row dump into a database built exactly the way
# deploy/k8s/10-postgres.yaml builds one — psql exit 0, zero lines on stderr, every
# table, every row, every policy, every trigger, every index present — and then
# comparing the privileges:
#
#   source database    45 table-level grants to non-owner roles
#   restored database  76      "        "     "   "     "        (31 EXTRA)
#
# THE MECHANISM, because it is not obvious and it will happen again to anyone who
# restores this schema by hand: scripts/db-init/01-roles.sql used to run ALTER DEFAULT
# PRIVILEGES ... GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO tappa_app at initdb.
# During a restore, every CREATE TABLE therefore grants all four automatically.
# pg_dump then emits the ACL it recorded — but it computes that ACL against
# PostgreSQL's BUILT-IN default (owner only), not against a default-privileges setting
# that exists only in the target, so it never emits the REVOKEs that would take the
# surplus away. The dump is right; the target was not empty of policy.
#
# 🔴 AND THE FIRST VERSION OF THIS COMMENT UNDERSTATED THE DAMAGE. It said "§4.3 is
# not breached, the trigger still refuses" and stopped there. A second audit measured
# further and it is worse than §4.3 — it reaches §4.4 and §4.7:
#
#   Two of the extra 31 are TABLE-LEVEL UPDATE and DELETE on public.tags, and tags has
#   no equivalent second belt: tags_counter_monotonic is BEFORE UPDATE only, and its
#   WHEN clause fires only if the new counter is strictly BELOW the old one — so it
#   never fires on INSERT at all. Run as tappa_app over TCP against a real restored
#   schema, on a plaque that has no transactions yet (transactions_tag_fk is
#   ON DELETE RESTRICT, so only those can be deleted):
#
#     A  ctr before                        5100
#     DELETE FROM tags WHERE uid=...    -> DELETE 1     (privilege came ONLY from the residue)
#     INSERT ... last_ctr 0             -> INSERT 0 1   (trigger cannot fire on INSERT)
#     C  ctr AFTER delete+reinsert         0
#     D  a REPLAYED ctr=7 now advances to  7
#     E  aes_key_ref is now                forged-key-ref
#
#   That is CLAUDE.md §4.4's replay protection reset to zero with a stolen tappa_app
#   credential — the exact threat model scripts/db-init/01-roles.sql names — plus §4.7:
#   a TABLE-level UPDATE overrides the column list, and the dump grants UPDATE on tags
#   for five columns that deliberately exclude aes_key_ref, so the wrapped AES keys
#   become writable. Measured has_column_privilege(tappa_app,tags,aes_key_ref,UPDATE):
#   false at the source, TRUE after a restore under the old default privileges.
#   (The trigger's predicate is quoted in prose rather than verbatim on purpose: it is
#   a counter comparison, scripts/redline-check.sh's R4 matches that shape line-locally
#   in scripts/, and a WARN nobody can act on is a WARN everybody learns to skip. Read
#   the real thing with `pg_get_triggerdef`.)
#
# ⚠️ THE PRODUCT STILL DID NOT VISIBLY BREAK, WHICH IS EXACTLY WHY THIS NEEDS A
# MACHINE. On public.transactions the UPDATE was still refused — by the
# transactions_no_mutation TRIGGER instead of by the privilege — so a human spot-check
# "can I tamper with a row? no" passes on a database that has lost a belt on one table
# and lost replay protection outright on another. The difference is only visible in
# the ERROR CODE, and this script asserts on the error code:
#   correct restore  ERROR: permission denied for table transactions        (42501)
#   damaged restore  ERROR: append-only table transactions: UPDATE is forbidden (P0001)
# Both are errors. Only one of them means the privilege is still gone.
#
# ✅ THE SEVERE HALF IS NOW CLOSED STRUCTURALLY, NOT BY INSTRUCTION.
# scripts/db-init/01-roles.sql's default privileges were narrowed to SELECT, INSERT
# (its own comment carries the measurement: the ONLY difference across a full fresh
# install with real goose is on goose_db_version, and `go test -race ./...` is green).
# The same replay chain against a database built that way stops at the first step:
# "ERROR: permission denied for table tags". What narrowing does NOT do is reach zero:
# a restore into a freshly-initialised pod still ends up with 5 extra table grants
# (SELECT/INSERT that the source withholds, including table-level SELECT on
# legal_documents, which overrides ITS column list). Narrowed init + the runbook's
# suspend step measures exactly 45/0/0. So both belts are kept, and this script is
# what makes either of them a fact rather than an instruction someone followed.
#
# POSIX sh: it has to run in postgres:17-alpine as well as on an operator's laptop.
set -eu

DUMP="${1:-}"
MANIFEST="${2:-}"
PGHOST="${PGHOST:-tappa-postgres}"
PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-tappa_owner}"
PGDATABASE="${PGDATABASE:-tappa}"
export PGHOST PGPORT PGUSER PGDATABASE

TMP="${TMPDIR:-/tmp}/pg-restore-verify.$$"
mkdir -p "$TMP"
trap 'rm -rf "$TMP"' EXIT INT TERM

fails=0
bad()  { echo "  FAIL  $*" >&2; fails=$((fails + 1)); }
ok()   { echo "  ok    $*"; }
note() { echo "  --    $*"; }
die()  { echo "pg-restore-verify: $*" >&2; exit 2; }

[ -n "$DUMP" ] && [ -n "$MANIFEST" ] \
  || die "usage: PGPASSWORD=<owner> TAPPA_APP_PASSWORD=<app> $0 <dump.sql.gz> <dump.manifest>"
[ -f "$DUMP" ]     || die "dump file '$DUMP' does not exist"
[ -f "$MANIFEST" ] || die "manifest '$MANIFEST' does not exist"
: "${PGPASSWORD:?PGPASSWORD (the tappa_owner password) is not set}"

# 🔴 THE APPLICATION PASSWORD IS MANDATORY AND THE REFUSAL IS DELIBERATE. The only
# probe that proves tenant isolation is one that connects the way the application
# connects: a TCP session authenticated as tappa_app. A superuser session with
# `SET ROLE tappa_app` gives the SAME row counts — measured here, 0 rows without the
# GUC — so it is a fair RLS probe, but it exercises neither pg_hba nor the credential,
# and this repository has already been burned once by a measurement taken over a
# loopback socket that libpq trusted. Weaker evidence presented as proof is worse than
# a refusal.
: "${TAPPA_APP_PASSWORD:?TAPPA_APP_PASSWORD is not set. The tenant-isolation probe must authenticate as tappa_app over TCP; a SET ROLE probe from the owner session reports the same row counts but tests neither pg_hba nor the credential, so it is not accepted here.}"

OWNER_PSQL="psql -X -q -A -t -v ON_ERROR_STOP=1"

echo "pg-restore-verify: $PGUSER@$PGHOST:$PGPORT/$PGDATABASE against $(basename "$DUMP")"

# =================================================================================
# 1. THE ARCHIVE ITSELF
# =================================================================================
m() { awk -v k="$1" '$1 == k { print $2; exit }' "$MANIFEST"; }
M_SHA="$(m sha256)"; M_FILE="$(m file)"; M_TABLES="$(m tables)"; M_POLICIES="$(m policies)"
M_FORCE="$(m rls_force)"; M_GOOSE="$(m goose_version)"; M_ROWS="$(m rows_total_dump)"
M_STARTED="$(m started_at)"
[ -n "$M_SHA" ] || die "manifest '$MANIFEST' has no sha256 line; it is not a tappa backup manifest"

case "$DUMP" in
  *.gz)
    gzip -t "$DUMP" || bad "gzip integrity check failed: the archive is corrupt"
    have="$(sha256sum "$DUMP" | awk '{print $1}')"
    ;;
  *) have="$(sha256sum "$DUMP" | awk '{print $1}')" ;;
esac
if [ "$have" = "$M_SHA" ]; then ok "sha256 matches the manifest"
else bad "sha256 mismatch: manifest says $M_SHA, file is $have (manifest is for $M_FILE)"; fi
note "backup instant (what this database has been rewound to): $M_STARTED"

# =================================================================================
# 2. THE CATALOG — shape, not just rows
# =================================================================================
cat_line="$($OWNER_PSQL -c "SELECT concat_ws(' ',
  (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r'),
  (SELECT count(*) FROM pg_policies WHERE schemaname='public'),
  (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r' AND c.relforcerowsecurity),
  (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r' AND c.relrowsecurity),
  (SELECT coalesce(max(version_id)::text,'none') FROM goose_db_version));")" \
  || die "cannot query $PGDATABASE as $PGUSER"
set -- $cat_line
db_tables="$1"; db_policies="$2"; db_force="$3"; db_enable="$4"; db_goose="$5"

[ "$db_tables"   = "$M_TABLES"   ] && ok "tables: $db_tables"                 || bad "tables: manifest $M_TABLES, database $db_tables"
[ "$db_policies" = "$M_POLICIES" ] && ok "row-level-security policies: $db_policies" || bad "policies: manifest $M_POLICIES, database $db_policies"
[ "$db_force"    = "$M_FORCE"    ] && ok "tables with FORCE ROW LEVEL SECURITY: $db_force" || bad "FORCE RLS: manifest $M_FORCE, database $db_force"
[ "$db_enable"   = "$db_force"   ] && ok "every FORCE table also has RLS enabled"  || bad "$db_enable tables have RLS enabled but $db_force have it FORCEd; §6 requires both"
[ "$db_goose"    = "$M_GOOSE"    ] && ok "goose schema version: $db_goose"     || bad "goose version: manifest $M_GOOSE, database $db_goose"

# Row counts, per table, exact. A restore is deterministic — unlike the backup, which
# races a live database — so anything other than equality here is a real difference.
countq="$($OWNER_PSQL -c "SELECT string_agg(format('SELECT %L AS t, count(*) AS n FROM public.%I', c.relname, c.relname), ' UNION ALL ') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r';")"
if [ -n "$countq" ]; then
  $OWNER_PSQL -F' ' -c "$countq" > "$TMP/db.counts"
else
  : > "$TMP/db.counts"
fi
awk '/^table /{print $2, $3}' "$MANIFEST" | sort > "$TMP/want.counts"
sort "$TMP/db.counts" > "$TMP/have.counts"
if cmp -s "$TMP/want.counts" "$TMP/have.counts"; then
  ok "row counts: all $db_tables tables match the manifest ($M_ROWS rows)"
else
  bad "row counts differ from the manifest:"
  diff "$TMP/want.counts" "$TMP/have.counts" | sed 's/^/        /' >&2 || true
fi

# =================================================================================
# 3. PRIVILEGES — the check that catches the ALTER DEFAULT PRIVILEGES residue
#
# EXPECTED comes from the dump, so the reference is the artifact being restored and
# not a list written by hand that would go stale with the next migration.
#
# 🔴 BOTH LEVELS ARE COMPARED, AND THE COLUMN LEVEL IS THE ONE THAT MATTERS MOST.
# An earlier version of this file skipped every parenthesised grant with the excuse
# that ALTER DEFAULT PRIVILEGES cannot create column grants. That is true about the
# MECHANISM and wrong about the EFFECT, and the audit that caught it counted the
# cost: of 91 `ON TABLE` GRANT lines in a real dump, 73 are column-level and were
# being ignored — including the only thing that keeps tappa_app away from a wrapped
# AES key. The dump grants UPDATE on tags for exactly five columns (location_id,
# last_ctr, status, retired_at, replaced_by) and `aes_key_ref` is deliberately not
# among them; a TABLE-level UPDATE overrides that restriction entirely, so a residue
# that adds table-level UPDATE makes aes_key_ref writable while every column-level
# check still looks correct.
#
# HOW THE TWO ARE MADE COMPARABLE: information_schema.column_privileges expands
# table-level grants into one row per column, so the expected set is built the same
# way — the dump's column grants, PLUS its table grants multiplied across that
# table's columns. Measured line shapes in a real dump: 18 table-level (explicit
# comma lists, zero `GRANT ALL ON TABLE`, zero WITH GRANT OPTION) and 73
# column-level, one column and one privilege per parenthesis. Each comma-separated
# token is classified on its own, so a mixed `GRANT SELECT,UPDATE(x)` would still be
# split correctly even though this schema does not currently produce one.
#
# ACTUAL excludes each table's OWNER, derived per table from pg_tables rather than
# assumed to be one name — the owner's privileges are implicit and pg_dump never
# emits GRANTs for them, so including them would report every table as "missing".
# =================================================================================
extract() {
  awk '
    /^GRANT .* ON TABLE public\./ {
      line = $0; sub(/;[ \t]*$/, "", line)
      i = index(line, " ON TABLE public.")
      privs = substr(line, 7, i - 7)
      rest  = substr(line, i + 17)
      j = index(rest, " TO ")
      tbl  = substr(rest, 1, j - 1)
      role = substr(rest, j + 4)
      gsub(/"/, "", tbl); gsub(/"/, "", role)
      n = split(privs, p, /,/)
      for (k = 1; k <= n; k++) {
        tok = p[k]; gsub(/^[ \t]+|[ \t]+$/, "", tok)
        if (tok == "") continue
        o = index(tok, "(")
        if (o == 0) { print "T", tbl, role, tok }
        else {
          priv = substr(tok, 1, o - 1)
          col  = substr(tok, o + 1, length(tok) - o - 1)
          gsub(/[ \t"]/, "", col); gsub(/[ \t]/, "", priv)
          print "C", tbl, role, col, priv
        }
      }
    }'
}
case "$DUMP" in
  *.gz) gzip -dc "$DUMP" | extract > "$TMP/all.grants" ;;
  *)    extract < "$DUMP"          > "$TMP/all.grants" ;;
esac
awk '$1=="T"{print $2, $3, $4}' "$TMP/all.grants" | sort -u > "$TMP/want.grants"
awk '$1=="C"{print $2, $3, $4, $5}' "$TMP/all.grants" | sort -u > "$TMP/want.colgrants.direct"

# 🔴 NOT `psql ... | sort -u > file`. On the left of a pipe `set -eu` is blind, so a
# psql that died would leave an EMPTY file and `comm` would then report every single
# grant the dump declares as "MISSING" — a confident, wrong, alarming diagnosis
# produced by a connection blip. Each query lands in a file whose status is checked,
# and sorting is a separate step.
psql_to() { # target-file, sql
  _t="$1"; shift
  $OWNER_PSQL -F' ' -c "$1" > "$_t.raw" || die "a catalog query failed against $PGDATABASE; nothing below can be trusted, so no verdict is issued"
  sort -u "$_t.raw" > "$_t"
}
psql_to "$TMP/have.grants" "SELECT g.table_name, g.grantee, g.privilege_type
  FROM information_schema.role_table_grants g
  JOIN pg_tables t ON t.schemaname = 'public' AND t.tablename = g.table_name
  WHERE g.table_schema = 'public' AND g.grantee <> t.tableowner AND g.grantee <> 'PUBLIC';"
psql_to "$TMP/columns" "SELECT table_name, column_name FROM information_schema.columns
  WHERE table_schema = 'public';"
psql_to "$TMP/have.colgrants" "SELECT g.table_name, g.grantee, g.column_name, g.privilege_type
  FROM information_schema.column_privileges g
  JOIN pg_tables t ON t.schemaname = 'public' AND t.tablename = g.table_name
  WHERE g.table_schema = 'public' AND g.grantee <> t.tableowner AND g.grantee <> 'PUBLIC';"

# Expand the dump's TABLE grants across that table's columns, then union with the
# dump's own column grants — the same shape information_schema reports.
#
# 🔴 ONLY THE COLUMN-GRANTABLE PRIVILEGES ARE EXPANDED, AND THIS FILTER IS A
# CORRECTION, NOT A PRECAUTION. The first version expanded all of them and produced
# 39 "column privileges the dump declares are MISSING" against a database that was
# byte-for-byte correct — because SQL has no column-level DELETE. Measured on this
# schema: information_schema.column_privileges only ever reports INSERT, REFERENCES,
# SELECT and UPDATE, while role_table_grants also carries DELETE, TRIGGER and
# TRUNCATE. A detector that cries about 39 impossible rows on a healthy restore is a
# detector an operator learns to skip, which is worse than not having one.
awk 'NR==FNR { cols[$1] = cols[$1] " " $2; next }
     $3 != "SELECT" && $3 != "INSERT" && $3 != "UPDATE" && $3 != "REFERENCES" { next }
     { n = split(cols[$1], c, " "); for (i = 1; i <= n; i++) if (c[i] != "") print $1, $2, c[i], $3 }' \
  "$TMP/columns" "$TMP/want.grants" > "$TMP/want.colgrants.expanded"
cat "$TMP/want.colgrants.direct" "$TMP/want.colgrants.expanded" | sort -u > "$TMP/want.colgrants"

report_grants() { # $1=label $2=want $3=have $4=marker-noun
  _extra="$(comm -13 "$2" "$3" | awk 'NF{n++} END{print n+0}')"
  _missing="$(comm -23 "$2" "$3" | awk 'NF{n++} END{print n+0}')"
  _want="$(awk 'NF{n++} END{print n+0}' "$2")"
  if [ "$_extra" -eq 0 ] && [ "$_missing" -eq 0 ]; then
    ok "$1: $_want grants, exactly the set the dump declares"
  else
    [ "$_extra" -eq 0 ] || {
      bad "$_extra $1 exist that the dump does NOT grant — this is the ALTER DEFAULT PRIVILEGES residue, and it is a privilege ESCALATION:"
      comm -13 "$2" "$3" | head -40 | sed 's/^/        + /' >&2
      [ "$_extra" -le 40 ] || echo "        ... and $(( _extra - 40 )) more" >&2
    }
    [ "$_missing" -eq 0 ] || {
      bad "$_missing $1 the dump declares are MISSING — the application will get 'permission denied':"
      comm -23 "$2" "$3" | head -40 | sed 's/^/        - /' >&2
      [ "$_missing" -le 40 ] || echo "        ... and $(( _missing - 40 )) more" >&2
    }
  fi
}
report_grants "table privileges"  "$TMP/want.grants"    "$TMP/have.grants"
report_grants "column privileges" "$TMP/want.colgrants" "$TMP/have.colgrants"

# =================================================================================
# 4. BEHAVIOUR, AS THE APPLICATION SEES IT — one TCP session authenticated as
#    tappa_app. Nothing here writes: the UPDATE and DELETE carry WHERE false, so even
#    if the privilege wrongly exists the statement touches no row, and the whole probe
#    runs inside a transaction that is rolled back.
# =================================================================================
tenant="$($OWNER_PSQL -c "SELECT tenant_id::text FROM public.transactions GROUP BY 1 ORDER BY count(*) DESC LIMIT 1;" 2>/dev/null || true)"

cat > "$TMP/probe.sql" <<'SQL'
\set ON_ERROR_STOP off
\set VERBOSITY terse
\pset format unaligned
\pset tuples_only on
SELECT 'PROBE-RAN';
SELECT 'NOGUC ' || count(*) FROM public.transactions;
SQL
# 🔴 SET LOCAL INSIDE A TRANSACTION, NEVER A BARE SET — CLAUDE.md §6, and
# scripts/redline-check.sh's R5 caught the first draft of this file doing it the
# wrong way. This session is a one-shot psql that exits immediately, so a bare SET
# could not have contaminated a pooled connection here; the rule is still followed
# because a probe that models the application should spell the tenant context the
# way the application spells it, and because a scanner that has to be argued with is
# a scanner that gets ignored.
if [ -n "$tenant" ]; then
  {
    echo "BEGIN;"
    printf "SET LOCAL app.tenant_id = '%s';\n" "$tenant"
    echo "SELECT 'GUC ' || count(*) FROM public.transactions;"
    echo "ROLLBACK;"
  } >> "$TMP/probe.sql"
fi
cat >> "$TMP/probe.sql" <<'SQL'
BEGIN;
UPDATE public.transactions SET note = note WHERE false;
ROLLBACK;
BEGIN;
DELETE FROM public.transactions WHERE false;
ROLLBACK;
SQL

PGPASSWORD="$TAPPA_APP_PASSWORD" psql -X -q -U tappa_app -d "$PGDATABASE" -h "$PGHOST" -p "$PGPORT" \
  -f "$TMP/probe.sql" > "$TMP/probe.out" 2> "$TMP/probe.err" || true

# 🔴 THE GUARD ASKS WHETHER THE PROBE RAN, NOT WHICH ERROR STRING CAME BACK — AND
# THAT IS A CORRECTION. The first version enumerated two strings ("password
# authentication failed", "role ... does not exist") and treated everything else as a
# successful session. Measured against a database where CONNECT had been revoked from
# tappa_app and PUBLIC, psql fails with `FATAL: permission denied for database
# "tappa"`, which is in neither string — so the script sailed past and announced
# "tappa_app sees <no answer> transaction rows ... FORCE row-level security is not in
# effect". A true statement about a session that never happened, pointing the operator
# at the tenant boundary while the real fault was a missing GRANT CONNECT. At 04:00,
# on the tool B YOLU makes mandatory, that sends someone to the wrong file.
#
# The structural form has no list to be incomplete: the probe prints a marker line as
# its first statement, so if that marker is absent the session did not run, whatever
# the reason — auth, CONNECT, pg_hba, DNS, a full disk. An enumerated guard can only
# ever recognise the failures its author had already seen.
# 🔴 THE MARKER IS `SELECT 'PROBE-RAN'` AND NOT THE FIRST REAL MEASUREMENT — A
# CORRECTION. The previous version keyed on the NOGUC line, which is
# `SELECT ... FROM public.transactions`: a marker that depends on the very thing being
# measured is not a structural guard. Measured — with SELECT revoked from tappa_app on
# public.transactions, the session OPENED, psql CONNECTED, and the script still said
# "produced no answer ... Fix the connection first", sending the operator at a network
# problem that did not exist. `SELECT 'PROBE-RAN'` touches no table, no policy and no
# privilege, so its absence means one thing only: the session did not run.
if ! grep -q '^PROBE-RAN$' "$TMP/probe.out"; then
  bad "the tappa_app session never ran, so NOTHING here was measured about tenant isolation or §4.3 — this is neither a pass nor an RLS finding. Fix the connection first. psql said: $(tr '\n' ' ' < "$TMP/probe.err" | cut -c1-300)"
elif ! grep -q '^NOGUC ' "$TMP/probe.out"; then
  bad "tappa_app connected but could not read public.transactions at all, so tenant isolation was NOT measured — this is a privilege or schema fault, not an RLS finding. psql said: $(tr '\n' ' ' < "$TMP/probe.err" | cut -c1-300)"
else
  noguc="$(awk '/^NOGUC /{print $2; exit}' "$TMP/probe.out")"
  guc="$(awk   '/^GUC /{print $2; exit}'   "$TMP/probe.out")"
  if [ "${noguc:-x}" = "0" ]; then ok "tenant isolation: tappa_app with no app.tenant_id sees 0 of $M_ROWS rows"
  else bad "tappa_app sees ${noguc:-<no answer>} transaction rows with NO app.tenant_id set — FORCE row-level security is not in effect"; fi

  if [ -z "$tenant" ]; then
    note "the restored database has no transactions, so the positive half of the isolation probe could not run. That is correct for a backup of a new installation and is NOT evidence that isolation works."
  elif [ "${guc:-0}" -gt 0 ] 2>/dev/null; then
    ok "tenant isolation: the same session with one tenant set sees $guc rows"
  else
    bad "with app.tenant_id set, tappa_app still sees ${guc:-0} rows — the policies are present but admit nothing, so the application would read an empty database"
  fi

  # 🔴 ASSERT WHICH ERROR, NOT MERELY THAT THERE WAS ONE. §4.3 is defended twice on
  # this table: tappa_app is not granted UPDATE/DELETE, AND transactions_no_mutation
  # raises on any attempt. A restore can drop the first without touching the second,
  # and then "can I tamper with a row? no" still answers no. So the probe is built to
  # see ONLY the privilege layer: `WHERE false` matches no row, and the trigger is
  # `BEFORE DELETE OR UPDATE ... FOR EACH ROW` (measured with pg_get_triggerdef), so
  # it cannot fire and cannot mask the answer. The three outcomes are distinct:
  #   42501 permission denied  -> both belts intact                         (correct)
  #   no error, 0 rows         -> the privilege belt is GONE; the trigger is
  #                               untouched and still refuses real writes   (degraded)
  #   anything else            -> unknown, and unknown is not a pass
  if grep -q 'permission denied for table transactions' "$TMP/probe.err"; then
    n_denied="$(awk '/permission denied for table transactions/{n++} END{print n+0}' "$TMP/probe.err")"
    [ "$n_denied" -ge 2 ] \
      && ok "§4.3: UPDATE and DELETE on transactions are both refused by PRIVILEGE (42501)" \
      || bad "only $n_denied of the 2 mutation probes was refused by privilege; read the probe's stderr"
  elif [ ! -s "$TMP/probe.err" ]; then
    bad "tappa_app HOLDS UPDATE/DELETE on public.transactions: both probes ran without error. §4.3 is not breached — transactions_no_mutation (BEFORE ... FOR EACH ROW) still refuses any statement that touches a row, and this probe deliberately touches none — but the privilege belt is gone and this database is one dropped trigger away from mutable attendance records. Cause: the restore skipped the step that suspends ALTER DEFAULT PRIVILEGES — deploy/README.md, 'Yedek ve geri yükleme', the step headed 'CANLI VERITABANININ USTUNE DEGIL, YANINA GERI YUKLE'. Re-run the restore; do not patch it by hand."
  else
    bad "the mutation probe returned something this script does not recognise, and an unrecognised answer is not a pass: $(tr '\n' ' ' < "$TMP/probe.err" | cut -c1-300)"
  fi
fi

echo
if [ "$fails" -eq 0 ]; then
  echo "pg-restore-verify: PASS — the restored database matches $(basename "$DUMP") in rows, schema, policies, and BOTH table- and column-level privileges."
  exit 0
fi
echo "pg-restore-verify: $fails CHECK(S) FAILED — do not put this database into service." >&2
exit 1
