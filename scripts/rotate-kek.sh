#!/usr/bin/env bash
# Tappa — re-seal every plaque's per-tag AES key under a NEW KEK, so a leaked
# TAPPA_TAG_KEK can be retired without visiting a single wall.
#
#   usage:  OWNER_DSN=<owner dsn, no password>            \
#           TAPPA_TAG_KEK_FILE=/path/old.b64              \
#           TAPPA_TAG_KEK_NEW_FILE=/path/new.b64          \
#           [TAPPA_ROTATE_ALLOW_UNOPENABLE=<exact count>] \
#           scripts/rotate-kek.sh [--apply]     # no argument = DRY RUN
#
#   exit 0  every row read is under the new KEK, and it was applied
#   exit 3  applied what it could, but rows remain under the OLD KEK
#           -> the old KEK CANNOT be destroyed; do not run the "drop" step
#   exit 1  refused or failed; nothing was applied
#
# 🔴 WHY THIS IS A FILE AND NOT A BLOCK IN THE RUNBOOK. The procedure used to live
# as copy-paste blocks in deploy/README.md, and a runbook is a program written in
# an unspecified language: the operator's shell. Measured on a real pty with
# `zsh -f -i` (no user rc, zsh's own defaults), pasting those blocks:
#
#   * a `#` comment containing backticks EXECUTED the backticked text — including
#     one comment whose text was the path of the very binary it warned about, so
#     the warning ran the attacker's binary it was warning against;
#   * a `#` comment containing an odd number of apostrophes put zsh into `quote>`
#     and SWALLOWED THE NEXT REAL COMMAND — in one case the `install -m 600` line
#     that another finding had just added;
#   * bash 3.2 did neither, so "identical in bash and zsh" was true of the tested
#     block and false of the comments around it.
#
# A file removes the category: bash interprets it, nobody pastes it, and the shell
# is now SPECIFIED. Everything below that used to depend on the operator's
# environment — the psql flag that makes the transaction fail closed, the temp
# directory, the cleanup trap — is now inside a process this repository controls.
#
# It follows the shape of scripts/pg-backup.sh, pg-backup-ship.sh and
# pg-restore-verify.sh: probe what the platform actually does, branch on the
# measurement, and make the exit code the interface.
#
# ⚠️ BASH IS REQUIRED AND DECLARED. Unlike the phase E scripts (`#!/bin/sh`, which
# pass both `sh -n` and `bash -n`), this one uses `set -o pipefail` and `[[ ]]`,
# so it is bash-only and says so in its shebang. `bash -n scripts/rotate-kek.sh`
# is part of the suite; `sh -n` is NOT claimed.
#
# 🔴 WHAT THIS ROTATES: the KEK. The same 16-byte per-tag key is re-sealed under a
# new 32-byte KEK, so tags.aes_key_ref changes and THE CHIP ON THE WALL DOES NOT.
# It does NOT rotate the per-tag key itself — that is burned into the chip
# (ADR 0003 madde 5, "Anahtar döndürme", which names retire+replace as the
# normative path and leaves in-place ChangeKey outside the MVP).

set -euo pipefail

# 🔴 NO TRACING, EVER. `bash -x`, or an inherited SHELLOPTS=xtrace, prints every
# expanded command to stderr — including the two lines that read the KEK files —
# and the runbook tells the operator to KEEP this stderr. Measured: both keys
# appeared in plaintext. There is no legitimate reason to trace a key-handling
# script, so it refuses rather than silently downgrading.
if [[ "$-" == *x* ]] || [[ "${SHELLOPTS:-}" == *xtrace* ]]; then
  printf '%s\n' "rotate-kek: REFUSING to run under xtrace: tracing prints the KEKs in plaintext to stderr." >&2
  exit 1
fi
set +x

log()  { printf '%s\n' "$*" >&2; }
die()  { log "rotate-kek: $*"; exit 1; }

# ---------------------------------------------------------------- argv --
# 🔴 THE DESTRUCTIVE BRANCH IS OPT-IN, AND THE PREVIOUS SHAPE WAS THE OPPOSITE.
# This used to be one full-string comparison:
#
#     DRY_RUN=0
#     [[ "${1:-}" == "--dry-run" ]] && DRY_RUN=1
#
# so EVERY input that was not exactly "--dry-run" selected the branch that rewrites
# the whole park. Measured on a throwaway 75 662-row copy, park checksum before
# and after: --dryrun · --help · -n · --dry-run=true · "x --dry-run" ALL returned 0
# and ALL REWROTE THE PARK. A rotation cannot be undone without the old KEK and a
# second run, and if the servers are not yet holding both keys the result is every
# tap answering 500 with no attendance row written — §4.6 failing underneath the
# product, from `--help`.
#
# TWO READINGS WERE WEIGHED AND THE SECOND WAS CHOSEN:
#
#   (a) keep the destructive default and parse strictly. After a correct `case`
#       exactly one input still writes — the EMPTY one — so the safe outcome
#       depends on the parser continuing to be right. The fallback of any future
#       parsing mistake is "rewrite the park".
#   (b) invert it: writing requires the explicit token --apply. Exactly one input
#       writes, it is an affirmative word, and the fallback of ANY mistake — a
#       typo, an unknown flag, a forgotten argument, a parser bug — is the safe
#       branch.
#
# Both leave one destructive input; they differ in where the mistakes land, and for
# an irreversible operation that is the whole question. This is (b).
usage() {
  cat >&2 <<'USAGE'
usage: scripts/rotate-kek.sh [--apply]

  (no arguments)  DRY RUN — reads the park, plans the rotation, applies NOTHING.
  --apply         Apply the rotation. This rewrites tags.aes_key_ref for the whole
                  park and cannot be undone without the old KEK and a second run.

required environment:
  OWNER_DSN                        owner DSN WITHOUT a password (use PGPASSFILE)
  TAPPA_TAG_KEK_FILE               file holding the CURRENT (old) KEK, base64
  TAPPA_TAG_KEK_NEW_FILE           file holding the NEW KEK, base64
optional:
  TAPPA_ROTATE_ALLOW_UNOPENABLE    exact count of rows that open under neither KEK

exit: 0 = done · 1 = refused/failed, nothing applied · 3 = applied, park NOT fully
rotated (the old KEK cannot be destroyed)
USAGE
}

APPLY=0
case "$#" in
  0) ;;
  1)
    case "$1" in
      --apply)   APPLY=1 ;;
      --dry-run) APPLY=0 ;;   # accepted as a no-op: it is what the old runbook said
      -h|--help) usage; exit 0 ;;
      *) log "rotate-kek: unknown argument: $1"; usage; exit 1 ;;
    esac
    ;;
  *) log "rotate-kek: expected at most one argument, got $#"; usage; exit 1 ;;
esac
DRY_RUN=$(( APPLY == 1 ? 0 : 1 ))

# ---------------------------------------------------------------- inputs --
: "${OWNER_DSN:?OWNER_DSN is required (owner DSN WITHOUT a password; put the password in ~/.pgpass)}"
: "${TAPPA_TAG_KEK_FILE:?TAPPA_TAG_KEK_FILE is required (file holding the CURRENT/OLD KEK, base64)}"
: "${TAPPA_TAG_KEK_NEW_FILE:?TAPPA_TAG_KEK_NEW_FILE is required (file holding the NEW KEK, base64)}"

[[ -r "$TAPPA_TAG_KEK_FILE"     ]] || die "cannot read $TAPPA_TAG_KEK_FILE"
[[ -r "$TAPPA_TAG_KEK_NEW_FILE" ]] || die "cannot read $TAPPA_TAG_KEK_NEW_FILE"
[[ -s "$TAPPA_TAG_KEK_FILE"     ]] || die "$TAPPA_TAG_KEK_FILE is EMPTY. A zero-byte key file is what a
  broken interactive read leaves behind, and it looks exactly like a stored key."
[[ -s "$TAPPA_TAG_KEK_NEW_FILE" ]] || die "$TAPPA_TAG_KEK_NEW_FILE is EMPTY."

# The KEKs never reach argv (ps and shell history read argv); they go from file to
# environment, which is what cmd/rotatekek reads.
# 🔴 THE KEKS ARE NOT EXPORTED. They are read into shell variables here and handed
# to the ONE process that needs them via an assignment prefix, below. `export` put
# both keys into the environment of every child — `go build` and its toolchain, and
# all four psql invocations — which is how a `\!` line in a stray ~/.psqlrc could
# have read them (measured: a psqlrc `\!` runs even on a plain `psql -c`).
# 🔴 THE libpq ENVIRONMENT IS PART OF THE INPUT, AND TWO CHANNELS SURVIVE -X.
#
#   PGOPTIONS  reaches the SERVER session and -X does not stop it. Measured:
#              PGOPTIONS="-c app.tenant_id=…" set the GUC, the generated SQL's
#              FIRST guard fired, psql returned 3 and this script returned 1 —
#              fail-closed, nothing written. It is cleared anyway: defence in
#              depth costs one line, and relying on a guard to catch what an
#              input could simply not carry is the shape this task keeps fixing.
#   PGSERVICE  can redirect a connection, but only for a bare `dbname=` DSN;
#              measured ineffective against the URI form the runbook uses. The
#              runbook's shape protected it; this script did not. Now both do.
#
# PGSYSCONFDIR / PSQLRC / ~/.psqlrc are already closed by -X (measured: rc=3 on
# all three).
unset PGOPTIONS PGSERVICE PGSERVICEFILE PSQLRC PGSYSCONFDIR 2>/dev/null || true

KEK_OLD_VALUE="$(cat "$TAPPA_TAG_KEK_FILE")"
KEK_NEW_VALUE="$(cat "$TAPPA_TAG_KEK_NEW_FILE")"

# --------------------------------------------------------------- workspace --
# 🔴 mktemp -d, NOT A PREDICTABLE PATH. Measured: with a symlink, a directory, or an
# unwritable directory at a fixed path, `go build -o <path>` returns 0 — the symlink
# is followed, the binary lands inside the directory, or a stale binary survives and
# stays executable. The next step exports BOTH KEKs and runs whatever is at that
# path, then feeds its stdout to a superuser psql. The build's exit code is gated
# too, because a build failure that is not checked is the same hole.
WORK="$(mktemp -d)" || die "mktemp -d failed"
chmod 700 "$WORK"
cleanup() {
  # Overwrite-then-delete IF the platform actually supports it — see the probe.
  if [[ -n "${SHRED_CMD:-}" ]]; then
    for f in "$WORK"/*; do [[ -f "$f" ]] && $SHRED_CMD "$f" >/dev/null 2>&1 || true; done
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

# 🔴 rm -P IS A DOCUMENTED NO-OP ON THIS PLATFORM AND STILL RETURNS 0. Darwin's
# `man rm` says "-P  This flag has no effect." — measured: rc=0, file removed, NOT
# overwritten. Because it returns 0, an `rm -P … || shred -u …` chain never reaches
# the fallback, so a runbook that wrote one was claiming an overwrite it never did.
# The capability is PROBED once and the cleanup branches on the result instead of
# on an assumption.
SHRED_CMD=""
if command -v shred >/dev/null 2>&1; then
  SHRED_CMD="shred -u -n1"
elif rm -P /dev/null 2>/dev/null && [[ "$(uname -s)" != "Darwin" ]]; then
  SHRED_CMD="rm -P"
fi
if [[ -z "$SHRED_CMD" ]]; then
  log "rotate-kek: NOTE — no overwriting delete on this platform (no shred; rm -P is a no-op here)."
  log "rotate-kek:        Temporary files are unlinked, not overwritten. Use an encrypted volume"
  log "rotate-kek:        or a RAM disk for \$TMPDIR if that matters to you."
fi

# ------------------------------------------------------------------ build --
command -v go >/dev/null 2>&1 || die "no Go toolchain; this script builds cmd/rotatekek"
REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
go build -o "$WORK/rotatekek" "$REPO_ROOT/cmd/rotatekek" || die "building cmd/rotatekek FAILED"
[[ -f "$WORK/rotatekek" && -x "$WORK/rotatekek" ]] || die "no executable was produced"

# ------------------------------------------------------------- connection --
# The session must genuinely bypass RLS. The generated SQL enforces this too; asking
# here turns a late abort into an early, readable refusal.
psql -X "$OWNER_DSN" -At -v ON_ERROR_STOP=1 -c "SELECT 1" >/dev/null \
  || die "cannot connect with OWNER_DSN (password belongs in ~/.pgpass, not the DSN)"
# 🔴 THE PROBE'S EXIT CODE IS MAPPED TOO. This was a BARE command substitution under
# `set -e`, so psql's status became the SCRIPT's status — including 3, which this
# file publishes as "applied, but the park is NOT fully rotated; the old KEK cannot
# be destroyed". Measured with a stub psql: probe exit 3 -> script 3, 2 -> 2, 4 -> 4,
# 127 -> 127, none with a word of explanation. The failure happens BEFORE the park
# is read and before anything is applied, so reporting "wrote and incomplete" sends
# the operator to investigate rows that were never touched. `psql -c` with
# ON_ERROR_STOP=1 returns 3 on any SQL error and 2 on a dropped connection, so this
# is not a hypothetical shape.
set +e
BYPASS="$(psql -X "$OWNER_DSN" -At -v ON_ERROR_STOP=1 \
  -c "SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user")"
PROBE_RC=$?
set -e
if [[ "$PROBE_RC" -ne 0 ]]; then
  die "the RLS-bypass probe FAILED (psql exit $PROBE_RC) before the park was read and before anything
  was applied. Reported as exit 1: nothing was written. Reconnect and re-run -- do NOT read this as
  exit 3, which means the rotation DID write and is incomplete."
fi
[[ "$BYPASS" == "t" ]] || die "the connected role neither is a superuser nor has BYPASSRLS. tags carries
  FORCE ROW LEVEL SECURITY, so this session cannot see or write the whole park -- it would rotate only
  the rows its policy admits and report success. (On managed Postgres this is the documented dead end:
  see deploy/README.md, 'MANAGED POSTGRES'.)"

# ------------------------------------------------------------------- read --
install -m 600 /dev/null "$WORK/park.tsv"
psql -X "$OWNER_DSN" -At -v ON_ERROR_STOP=1 \
  -c "COPY (SELECT uid, tenant_id, encode(aes_key_ref,'hex') FROM tags) TO STDOUT" \
  > "$WORK/park.tsv" || die "reading the park FAILED; nothing was written"
ROWS="$(wc -l < "$WORK/park.tsv" | tr -d ' ')"
log "rotate-kek: read $ROWS rows"

# ------------------------------------------------------------------ plan --
# 🔴 NO PIPE INTO psql. refuse() deliberately emits a poison statement; psql then
# exits 3, `pipefail` reports the RIGHTMOST non-zero status, and the tool's own 1 is
# masked — measured: the tool alone returned 1 while the literal pipeline returned 3,
# so the published code for "REFUSED, nothing written" was UNREACHABLE and three
# distinct outcomes collapsed onto one number. Run it, gate ITS code, then apply.
install -m 600 /dev/null "$WORK/rotate.sql"
set +e
TAPPA_TAG_KEK="$KEK_NEW_VALUE" \
TAPPA_TAG_KEK_PREVIOUS="$KEK_OLD_VALUE" \
  "$WORK/rotatekek" < "$WORK/park.tsv" > "$WORK/rotate.sql"
TOOL_RC=$?
set -e

case "$TOOL_RC" in
  0) log "rotate-kek: tool exit 0 — every row read is accounted for under the new KEK" ;;
  3) log "rotate-kek: tool exit 3 — rows remain under the OLD KEK (see the report above)" ;;
  *) die "tool exit $TOOL_RC — REFUSED, nothing was written" ;;
esac

if [[ "$DRY_RUN" == "1" ]]; then
  log "rotate-kek: DRY RUN — nothing applied. Planned SQL: $(wc -c < "$WORK/rotate.sql" | tr -d ' ') bytes."
  log "rotate-kek: re-run with --apply to write. (Applying is irreversible without the old KEK.)"
  exit "$TOOL_RC"
fi

# ------------------------------------------------------------------ apply --
# 🔴 -v ON_ERROR_STOP=1 IS LOad-BEARING FOR §4.7, NOT TIDINESS, AND THAT IS WHY IT
# LIVES HERE RATHER THAN IN AN OPERATOR'S FINGERS. Without it, a guard that aborts
# leaves the transaction failed, the following `COPY … FROM STDIN` dies with
# "current transaction is aborted", and psql then sends the remaining COPY DATA
# LINES AS SQL STATEMENTS — putting the park's wrapped refs back into the server log
# as failed statements (measured: 40 distinct refs, 20 old and 20 new) and still
# exiting 0. The COPY design closes that leak only while this flag holds the
# transaction closed.
# 🔴 psql's EXIT CODE IS MAPPED, NOT PASSED THROUGH.
#
# ⚠️ THIS COMMENT USED TO SAY "this was the last invocation without a guard" AND IT
# WAS WRONG: the RLS-bypass probe fifty-nine lines ABOVE was also unguarded and
# leaked the same way, which an auditor measured while this sentence claimed the
# problem was solved. Both are mapped now; the claim is about this call only.
#
# It leaked: a psql that exited 3 made THIS SCRIPT exit 3, and
# 3 is published — here and in deploy/README.md — as "applied, but the park is NOT
# fully rotated; the old KEK cannot be destroyed". Measured with a stub psql on
# PATH: stub exit 3 -> script 3, stub exit 2 -> script 2, neither with a word of
# explanation, and in both cases NOTHING had been applied. The pipe masking was
# removed one round earlier and this was the same defect wearing the other shoe.
#
# Anything psql reports here means the transaction did not commit, so it maps to 1
# (refused/failed, nothing applied) — never to 3.
set +e
psql -X "$OWNER_DSN" -v ON_ERROR_STOP=1 -f "$WORK/rotate.sql"
PSQL_RC=$?
set -e
if [[ "$PSQL_RC" -ne 0 ]]; then
  die "applying the rotation FAILED (psql exit $PSQL_RC); the transaction rolled back and the park is
  unchanged. Reported as exit 1: nothing was applied. Do NOT read this as exit 3 -- 3 means the rotation
  DID write and is incomplete."
fi

log "rotate-kek: applied."
if [[ "$TOOL_RC" == "3" ]]; then
  log "rotate-kek: 🔴 the park is NOT fully rotated — the OLD KEK CANNOT be destroyed yet."
fi
exit "$TOOL_RC"
