#!/bin/sh
# Tappa — put the verified dump somewhere the node's disk failing cannot reach, and
# expire the old ones. Second half of deploy/k8s/50-backup.yaml's CronJob; the first
# half is scripts/pg-backup.sh, which has already refused to hand anything over if the
# dump was empty, short or blind to row-level security.
#
# 🔴 WHY THIS STEP EXISTS AT ALL, AND WHY A LOCAL COPY IS NOT A BACKUP HERE. This
# cluster has ONE node and ONE StorageClass. Measured 2026-08-16:
#   kubectl get nodes      -> k8s-1.fsn1.private, and nothing else
#   kubectl get sc         -> local-path (default), rancher.io/local-path, Delete
#   kubectl get csidrivers -> No resources found
#   kubectl get pv         -> 20 of 20 on local-path
# local-path is a directory on that one node's disk. A dump written to a PVC therefore
# dies in the same event as the database it is a copy of, and CLAUDE.md §4.3 makes
# those rows legal evidence that cannot be reconstructed. So this job STAGES to an
# emptyDir — which is discarded with the pod — and the only durable artifact is the
# one this script pushes off the node. If it cannot push, the job goes red and there
# is deliberately no consolation copy left behind to be mistaken for a backup later.
#
# 🔴 WHERE "OFF NODE" ACTUALLY IS, IS THE OPERATOR'S AND NOT THIS FILE'S. Nothing in
# this cluster offers off-node storage today (the measurements above), so the
# destination needs a credential that only the operator has. It arrives as ONE file,
# an rclone config, mounted from secret/tappa-backup-target — so this manifest names
# no provider, no bucket and no key, and switching provider is a Secret change rather
# than a code change. deploy/README.md carries the chosen target and the eliminated
# ones with the reason each was eliminated.
#
# 🔴 ENCRYPTION BELONGS TO THAT SAME CONFIG, AND THE KEY MUST NOT LIVE WHERE
# TAPPA_TAG_KEK LIVES. rclone's `crypt` remote wraps the destination transparently, so
# the passphrase sits in the same file as the destination credential and never in
# tappa-secrets. That separation is the whole point: the only scenario a backup exists
# for is "the cluster is gone", and a backup key stored beside the cluster's other
# secrets is a key that is gone in exactly that scenario. deploy/README.md, "Yedek ve
# geri yükleme", says where both copies belong and why they are different envelopes.
#
# POSIX sh (busybox in the rclone image). No pipelines whose exit status matters.
set -eu

STAGE="${BACKUP_STAGE_DIR:-/staging}"
RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"

fail() { echo "pg-backup-ship: FAILED: $*" >&2; exit 1; }
say()  { echo "pg-backup-ship: $*"; }

TMPD="${TMPDIR:-/tmp}/pg-backup-ship.$$"
mkdir -p "$TMPD" || { echo "pg-backup-ship: FAILED: cannot create $TMPD" >&2; exit 1; }
trap 'rm -rf "$TMPD"' EXIT INT TERM

# ---------------------------------------------------------------------------------
# 1. THE DESTINATION MUST BE CONFIGURED, AND "not configured" MUST BE LOUD.
#    An unconfigured backup that silently does nothing is the failure this whole task
#    is about. ⚠️ The honest form of that sentence is a command and a date, not a
#    claim about how long it has been true: measured 2026-08-16, `kubectl -n tappa get
#    cronjob` answered "No resources found in tappa namespace". Whether the cluster
#    has one TODAY is deploy/README.md's question to ask, not this comment's to
#    assert — a sentence about the world goes stale, a sentence about a measurement
#    does not.
# ---------------------------------------------------------------------------------
: "${RCLONE_CONFIG:?RCLONE_CONFIG is not set: the CronJob points it at the file mounted from secret/tappa-backup-target}"
[ -f "$RCLONE_CONFIG" ] \
  || fail "$RCLONE_CONFIG does not exist. Create secret/tappa-backup-target with a single key 'rclone.conf' — deploy/README.md, 'Operatörün yapması gerekenler', the backup step."
[ -s "$RCLONE_CONFIG" ] \
  || fail "$RCLONE_CONFIG is empty, so no remote name can resolve and nothing can be shipped. (⚠️ The reason written here for one round was WRONG and is corrected rather than deleted: it claimed rclone would treat an unknown 'name:path' as a LOCAL PATH and 'succeed' by writing inside the pod. Measured with rclone v1.71.2: an undefined remote gives 'CRITICAL ... didn't find section in config file', a non-zero exit, and NO directory is created. The guard is still right — it names the cause instead of leaving rclone to — but it is not standing between the job and a silent local write.)"

: "${BACKUP_REMOTE:?BACKUP_REMOTE is not set: it is the rclone destination, e.g. tappa-backup:tappa-prod/db}"

# 🔴 A BARE REMOTE IS REFUSED, BECAUSE STEP 4 DELETES BY AGE. `rclone delete
# remote: --min-age 30d` on a bucket root removes every old object in that bucket,
# including things this product never wrote. Requiring a path segment makes the
# blast radius of the prune a directory this job owns.
case "$BACKUP_REMOTE" in
  *:) fail "BACKUP_REMOTE='$BACKUP_REMOTE' has no path. This job PRUNES BY AGE under that prefix, so a bare remote would expire objects it does not own. Use something like 'tappa-backup:bucket/tappa-prod'." ;;
  *:*/*|*:*) : ;;
  *)  fail "BACKUP_REMOTE='$BACKUP_REMOTE' is not an rclone remote (expected 'name:path')." ;;
esac
case "$BACKUP_REMOTE" in
  */) fail "BACKUP_REMOTE='$BACKUP_REMOTE' ends in a slash; drop it so the destination path below is unambiguous." ;;
esac

# 🔴 ONE MASKED SPELLING, USED IN EVERY MESSAGE — SUCCESS AND FAILURE ALIKE.
# The first version masked only the happy path, and the audit measured the gap: the
# "cannot list or create <remote>" branch and the prune branch both printed the bucket
# and path verbatim, and those are the branches an operator actually reads — the
# comment above them calls a dead credential "the most likely long-run failure".
# Anyone with `pods/log` in this namespace can read them, and a bucket name is a
# pointer at every employee's attendance history. §4.7 does not list it, so this is
# not a red-line breach; it is this task's own stated standard applied consistently.
REMOTE_SAFE="${BACKUP_REMOTE%%:*}:<path redacted>"

case "$RETENTION_DAYS" in
  ''|*[!0-9]*) fail "BACKUP_RETENTION_DAYS='$RETENTION_DAYS' is not a whole number of days." ;;
esac
[ "$RETENTION_DAYS" -ge 7 ] \
  || fail "BACKUP_RETENTION_DAYS='$RETENTION_DAYS' is below the floor of 7. A retention shorter than a week cannot survive a fault discovered after a weekend, and this deployment has no replica to fall back on."

# ---------------------------------------------------------------------------------
# 2. THERE MUST BE SOMETHING TO SHIP. If the dump container was skipped, replaced or
#    silently produced nothing, this is where it shows — rather than in a green job
#    that shipped an empty directory.
# ---------------------------------------------------------------------------------
[ -d "$STAGE" ] || fail "staging directory $STAGE does not exist"
gz="$(find "$STAGE" -maxdepth 1 -type f -name 'tappa-*.sql.gz' | head -n 1)"
[ -n "$gz" ] \
  || fail "no tappa-*.sql.gz in $STAGE: the dump container produced nothing. Read its log (kubectl -n tappa logs job/<job> -c dump-and-verify)."
manifest="${gz%.sql.gz}.manifest"
[ -f "$manifest" ] \
  || fail "$(basename "$gz") has no manifest beside it; scripts/pg-backup.sh writes both or neither, so this staging volume is not what it should be."
stamp="$(basename "$gz" .sql.gz)"
stamp="${stamp#tappa-}"
dest="$BACKUP_REMOTE/$stamp"

# ---------------------------------------------------------------------------------
# 2b. 🔴 THE DESTINATION MUST ACTUALLY BE ENCRYPTED, AND THAT IS CHECKED RATHER THAN
#     INTENDED. Until this existed, the word "crypt" appeared in this file ONLY in a
#     comment: nothing ever asked the remote what it was. Point BACKUP_REMOTE at a
#     plain S3 bucket and every tap's full GPS, every wrapped aes_key_ref and every
#     session token hash leaves the node in clear text — and the job stays GREEN.
#     deploy/README.md limit 19 lists four properties of the destination that cannot
#     be measured from this repository (reachable · in the EU · durable · who can
#     read it). "Encrypted" is not like those four: it IS measurable, from inside the
#     job, at the moment it runs. This task's own rule for escrow — proven, not
#     declared — applies here for the same reason.
#
#     `rclone config show` and NOT `config dump`, measured with a marker passphrase:
#       config show <name> -> "password = *** ENCRYPTED ***", 0 occurrences of the
#                             obscured value and 0 of the plaintext marker
#       config dump        -> the obscured value, twice
#     Only the `type` line is ever read out of the section; the rest is never printed.
#
#     An `alias` is followed rather than rejected, because `alias -> crypt` is a
#     legitimate way to name a subdirectory. The hop limit makes a config that points
#     at itself terminate instead of spinning.
# ---------------------------------------------------------------------------------
# 🔴 STDERR IS KEPT, NOT DISCARDED, AND THE EXIT STATUS IS CHECKED. The first version
# was `rclone config show "$1" 2>/dev/null` assigned under `set -eu` with no `||`:
# measured, an rclone.conf with ONE unparsable line (the most likely hand-edited-Secret
# mistake) made rclone exit 1 printing "CRITICAL: Failed to load config file ... could
# not parse line", 2>/dev/null threw that away, and the assignment killed the script
# with ZERO output. A missing rclone binary did the same at exit 127. The path I had
# tested — an undefined remote — exits 0, which is exactly why it looked fine.
remote_section() {
  if ! rclone config show "$1" > "$TMPD/section" 2> "$TMPD/section.err"; then
    fail "rclone could not read $RCLONE_CONFIG. It said: $(tr '\n' ' ' < "$TMPD/section.err" | cut -c1-300)"
  fi
  cat "$TMPD/section"
}
probe_remote="${BACKUP_REMOTE%%:*}"
hop=0
while : ; do
  hop=$((hop + 1))
  [ "$hop" -le 5 ] || fail "the remote '${BACKUP_REMOTE%%:*}' is a chain of aliases more than 5 deep, or it points at itself. Refusing rather than looping."
  section="$(remote_section "$probe_remote")"
  rtype="$(printf '%s\n' "$section" | awk -F'= *' '$1 ~ /^type[[:space:]]*$/ { print $2; exit }')"
  case "$rtype" in
    crypt)
      crypt_remote="$probe_remote"
      crypt_under="$(printf '%s\n' "$section" | awk -F'= *' '$1 ~ /^remote[[:space:]]*$/ { print $2; exit }')"
      [ -n "$crypt_under" ] || fail "remote '$probe_remote' is type crypt but names no underlying remote; fix secret/tappa-backup-target."
      break ;;
    alias)
      next="$(printf '%s\n' "$section" | awk -F'= *' '$1 ~ /^remote[[:space:]]*$/ { print $2; exit }')"
      next="${next%%:*}"
      [ -n "$next" ] || fail "remote '$probe_remote' is an alias with no target; fix secret/tappa-backup-target."
      probe_remote="$next" ;;
    "")
      fail "remote '$probe_remote' is not defined in $RCLONE_CONFIG (rclone reports no type for it). Check the name before the colon in BACKUP_REMOTE." ;;
    *)
      fail "remote '$probe_remote' has type '$rtype', which is NOT encrypted. This backup carries every employee's attendance history, every tap's full GPS coordinates, the wrapped AES key of every wall plaque (tags.aes_key_ref) and every session token hash. Wrap the destination in an rclone 'crypt' remote — deploy/README.md, operator step 9(b). Refusing to ship." ;;
  esac
done

# ---------------------------------------------------------------------------------
# 2c. 🔴 PROVE THE ENCRYPTION, DO NOT READ ITS DECLARATION. `type = crypt` IS NOT THE
#     PROPERTY, AND THAT WAS MEASURED THE HARD WAY: an audit pointed the shipped image
#     at a remote that is genuinely `type = crypt` and added ONE option, and this
#     script said "destination is an encrypted (crypt) remote", uploaded, verified and
#     exited 0 — while the dump sat at rest as plain gzip and the manifest was readable
#     with `cat`. Two options do it, and neither changes the `type` line:
#
#       no_data_encryption = true   -> contents stored in the clear
#       filename_encryption = off   -> object names stored in the clear
#
#     Both are separate questions in `rclone config`'s wizard, so one wrong keystroke
#     during operator step 9(b) ships every tap's full GPS, every plaque's wrapped AES
#     key and every session token hash in the clear — silently, permanently, green.
#     My own nine degenerate paths all varied the `type` VALUE and never once varied
#     crypt's OWN options: a detector written to the shape of the previous defect.
#
#     SO THE CHECK NOW MEASURES THE ARTIFACT. A canary is written THROUGH the crypt
#     remote and read back FROM THE REMOTE UNDERNEATH IT, before any real data moves:
#       - stored bytes must begin with rclone's crypt magic, RCLONE\0\0
#       - the stored name must not contain the canary's plaintext marker
#     Measured across seven configurations — default ✓ · no_data_encryption=true
#     (bytes are plaintext) ✗ · filename_encryption=off (name is plaintext) ✗ · both ✗
#     · filename_encryption=obfuscate ✓ · no_data_encryption=TRUE ✗ · crypt with no
#     underlying remote ✗. The uppercase case is the point of doing it this way: the
#     property does not care how the option was spelled.
#
#     IT RUNS BEFORE THE UPLOAD ON PURPOSE. A check that ran afterwards would prove the
#     destination unencrypted only after writing plaintext employee records to it.
#
#     The canary is written at the CRYPT REMOTE'S OWN ROOT rather than under
#     BACKUP_REMOTE's path, because that keeps the plaintext path and the path
#     cryptdecode is asked about identical — no alias arithmetic, one code path. It is
#     deleted immediately, and its name is fixed so a failed cleanup overwrites rather
#     than accumulates.
# ---------------------------------------------------------------------------------
# 🔴 JOINING A REMOTE BASE TO A RELATIVE NAME IS NOT STRING CONCATENATION WITH A
# SLASH, AND GETTING THAT WRONG BROKE THE HEALTHY CASE. The first version wrote
# "$crypt_under/$canary_stored". When a crypt's `remote =` carries NO path after the
# colon — `remote = back:`, which is the form rclone's own wizard documents ("Normally
# should contain a ':' and not end with a '/'") and the natural spelling for an SFTP
# target — that produces `back:/<name>`, an ABSOLUTE path, while the canary was
# written at the remote's ROOT. Measured with the shipped image against a correctly
# encrypting store:
#     rclone cat back:<name>   -> R C L O N E \0 \0   rc=0     (the truth)
#     rclone cat back:/<name>  -> ERROR: error listing: directory not found, rc=3
# The job then failed EVERY NIGHT with "COULD NOT BE PROVEN" on a destination that was
# encrypting perfectly, and sent the operator hunting for a credential or network
# fault when the cause was one line of rclone.conf.
#     ⚠️ This is also the branch the previous round called "could not be induced with
# a local backing store". It could — with one config line. A claim that something is
# unreachable is itself a measurement, and that one was wrong.
rjoin() { # base, relative-name -> a path rclone can resolve
  _b="${1%/}"
  case "$_b" in
    *:) printf '%s%s' "$_b" "$2" ;;   # remote with no path: back:  + n -> back:n
    *)  printf '%s/%s' "$_b" "$2" ;;  # remote with a path, or a local path
  esac
}

# 🔴 UNIQUE PER RUN, NOT A FIXED NAME. concurrencyPolicy: Forbid only governs the Jobs
# the CronJob controller creates; deploy/README.md creates Jobs DIRECTLY in two places
# (operator step 9(c)'s tappa-backup-first and A YOLU step 2's tappa-backup-preresto),
# and those can overlap a scheduled run. With one fixed name each run would delete the
# other's canary and the loser would refuse to ship. The suffix removes the race
# outright rather than documenting it.
CANARY_NAME=".tappa-encryption-canary-PLAINTEXTMARKER-$(date -u +%Y%m%dT%H%M%SZ)-$$"
# 🔴 A FAILED CLEANUP IS REPORTED, NOT SWALLOWED. The canary is written at the crypt
# remote's ROOT, which is deliberately OUTSIDE the prefix this job owns — and step 4's
# `rclone delete --min-age` only sweeps under BACKUP_REMOTE. So a delete that fails
# leaves a file nothing will ever collect. It cannot pile up unboundedly (one per run,
# and the name says what it is), but silence here would mean nobody ever learns.
# ⚠️ A failing delete could NOT be induced against a local backing store, so this
# branch is guarded and unmeasured — said plainly rather than implied.
canary_cleanup() {
  if ! rclone deletefile "$crypt_remote:$CANARY_NAME" >/dev/null 2>"$TMPD/canary.rm.err"; then
    echo "pg-backup-ship: WARNING: could not remove the encryption canary '$CANARY_NAME' from remote '$crypt_remote'. It sits at that remote's ROOT, which is outside the prefix this job prunes, so nothing will collect it — delete it by hand. rclone said: $(tr '\n' ' ' < "$TMPD/canary.rm.err" | cut -c1-200)" >&2
  fi
}

printf 'PLAINTEXTMARKER this file proves nothing except whether the destination encrypts\n' > "$TMPD/canary"

rclone copyto "$TMPD/canary" "$crypt_remote:$CANARY_NAME" \
  || fail "could not write the encryption canary to $REMOTE_SAFE. Nothing has been uploaded."

# The same treatment the config read got: keep rclone's own sentence and check the
# exit status, instead of discarding both and inferring from an empty string.
if rclone cryptdecode --reverse "$crypt_remote:" "$CANARY_NAME" > "$TMPD/decode" 2> "$TMPD/decode.err"; then
  canary_stored="$(awk '{ print $NF }' "$TMPD/decode")"
else
  canary_stored=""
fi
if [ -z "$canary_stored" ]; then
  canary_cleanup
  fail "rclone could not tell what name it stores the canary under, so the destination's encryption COULD NOT BE PROVEN. Nothing has been uploaded. This is NOT the same as 'it is unencrypted'. rclone said: $(tr '\n' ' ' < "$TMPD/decode.err" | cut -c1-200)"
fi

if rclone cat --count 8 "$(rjoin "$crypt_under" "$canary_stored")" > "$TMPD/canary.raw" 2>"$TMPD/canary.err"; then
  canary_read=yes
else
  canary_read=no
fi
canary_cleanup

if [ "$canary_read" != yes ]; then
  fail "the remote underneath the crypt layer could not be read, so encryption COULD NOT BE PROVEN and nothing has been uploaded. This is NOT a claim that the destination is unencrypted — it is a refusal to guess. rclone said: $(tr '\n' ' ' < "$TMPD/canary.err" | cut -c1-200)"
fi

# ⚠️ `filename_encryption = obfuscate` PASSES THIS CHECK, AND THAT IS A COUNTED
# CHOICE RATHER THAN AN OVERSIGHT. rclone's own documentation calls obfuscate a
# reversible scrambling, not encryption: someone who can read the store can undo it.
# It passes because the question asked here is "is the plaintext marker visible", and
# under obfuscate it is not. The residual exposure is names only — backup timestamps
# and this deployment's schedule — while CONTENT is still verified encrypted by the
# byte check below, which is where the attendance records, GPS and wrapped AES keys
# live. Accepted; deploy/README.md limit 19 carries it.
case "$canary_stored" in
  *PLAINTEXTMARKER*)
    fail "the destination stores OBJECT NAMES IN THE CLEAR: it wrote the canary under a name that still contains its plaintext marker. That is 'filename_encryption = off' on remote '$crypt_remote'. Backup file names carry timestamps and this deployment's schedule; more importantly it means the crypt layer is not configured the way operator step 9(b) describes. Nothing has been uploaded." ;;
esac

if ! head -c 8 "$TMPD/canary.raw" | od -An -c | tr -s ' ' | grep -q 'R C L O N E'; then
  fail "🔴 THE DESTINATION IS NOT ENCRYPTING CONTENT. The canary was written through crypt remote '$crypt_remote' and came back from the store underneath it WITHOUT rclone's RCLONE\0\0 magic, i.e. in the clear — this is 'no_data_encryption = true'. Nothing has been uploaded, and that is deliberate: this dump carries every employee's attendance history, every tap's full GPS coordinates, the wrapped AES key of every wall plaque and every session token hash. Fix the remote (deploy/README.md, operator step 9(b)) and re-run."
fi

say "destination proven encrypted: canary stored under an opaque name, contents carry rclone's crypt magic"

# ---------------------------------------------------------------------------------
# 3. REACH THE DESTINATION BEFORE TRUSTING IT. An expired or revoked credential is
#    the most likely long-run failure of any backup job, and it must read as
#    "destination unreachable", not as a partial upload.
# ---------------------------------------------------------------------------------
# ⚠️ THE REMOTE'S NAME IS PRINTED, ITS PATH IS NOT. BACKUP_REMOTE holds a bucket and
# a path; anyone with `pods/log` in this namespace can read this job's output, and a
# bucket name is a pointer at every employee's attendance history. The alias before
# the colon is enough to tell two destinations apart during an incident.
say "destination $REMOTE_SAFE (config $RCLONE_CONFIG, retention ${RETENTION_DAYS}d)"
rclone lsd "$BACKUP_REMOTE" >/dev/null 2>&1 \
  || rclone mkdir "$BACKUP_REMOTE" \
  || fail "cannot list or create $REMOTE_SAFE. The credential in secret/tappa-backup-target is wrong, expired, or the endpoint is unreachable from this pod."

say "uploading $(basename "$gz") and $(basename "$manifest") to $REMOTE_SAFE/$stamp/"
rclone copyto "$gz"       "$dest/$(basename "$gz")"       --no-traverse \
  || fail "upload of the dump failed; nothing durable was written for $stamp"
rclone copyto "$manifest" "$dest/$(basename "$manifest")" --no-traverse \
  || fail "upload of the manifest failed. The dump may be there without it; a dump whose backup instant and row counts are unknown must not be counted as a backup — re-run this job."

# The artifact is verified where it now lives, not where it came from. --one-way
# because the destination legitimately holds every previous night as well.
#
# 🔴 SCOPED TO TONIGHT'S STAMP, AND THAT IS A FIX FOR A MEASURED FAILURE. The first
# version compared the WHOLE staging directory against one stamp's destination
# directory. In the CronJob the staging volume is a fresh emptyDir holding exactly one
# backup, so it could not misfire there — but a staging directory carrying two backups
# (which is what happens the moment anyone runs this by hand against a reused
# directory, and is exactly how it was found) makes the older stamp's two files report
# as "file not in Encrypted drive ... 2 differences found", and the job fails having
# uploaded a perfectly good backup. --include ties the comparison to the artifact this
# run actually shipped.
#
# ⚠️ AND A COUNTED LIMIT ON THE MASKING ABOVE: REMOTE_SAFE covers THIS SCRIPT'S
# messages, not rclone's. rclone names the remote in its own diagnostics — and the
# scope written here for one round ("on a failing run") was TOO NARROW: measured on a
# perfectly healthy run, exit 0, two lines carry it —
# "NOTICE: Encrypted drive 'enc:db/<stamp>': 0 differences found" and "... 2 matching
# files". So the bucket and path reach the job log EVERY NIGHT, not only when
# something breaks. The direction was right, the count was not. It is not
# piped through a masking filter on purpose: `set -eu` cannot see a failure on the
# left of a pipe, and swapping a §4.7 papercut for a fail-open exit status is the
# trade this repository has already paid for twice. Stated rather than half-closed.
#
# ⚠️ ON A crypt REMOTE THIS PRINTS A LINE THAT SAYS "ERROR" AND IS NOT ONE — measured
# with rclone v1.71.2 against a crypt-over-local remote: "ERROR : No common hash found
# - not using a hash for checks", followed by "0 differences found / 2 matching files"
# and exit 0. It is rclone saying the two sides share no hash algorithm, which is a
# property of encrypted remotes, and --ignore-checksum does NOT suppress it (measured
# both ways). It is written down here because an operator grepping a nightly log for
# "ERROR" would otherwise chase it. The exit status is what decides.
rclone check "$STAGE" "$dest" --one-way --size-only --include "tappa-$stamp.*" \
  || fail "the uploaded copy does not match the staged one. Treat $stamp as absent."

say "verified on the destination: $REMOTE_SAFE/$stamp"

# ---------------------------------------------------------------------------------
# 4. EXPIRE. Runs after the upload, so a prune failure never costs tonight's backup —
#    but it is still fatal, because a destination that cannot be pruned eventually
#    cannot be written to either, and that failure would arrive as a missing backup.
#
#    🔴 THIS IS A COUNTED LIMIT, NOT A SETTLED ANSWER. A backup keeps a deleted
#    record alive, so backup retention is bound to the same unanswered legal question
#    as TAPPA_RETENTION_YEARS (deploy/README.md limit 2, backlog B3). Thirty days is
#    chosen to be long enough to outlive a fault found after a weekend and short
#    enough that it cannot quietly become a second, unmanaged archive of employees'
#    attendance. It is not a legal opinion.
#
#    Degenerate paths measured rather than assumed: an empty prefix and a prefix
#    where nothing is old enough both make `rclone delete` exit 0 and print nothing.
# ---------------------------------------------------------------------------------
rclone delete "$BACKUP_REMOTE" --min-age "${RETENTION_DAYS}d" \
  || fail "pruning objects older than ${RETENTION_DAYS}d under $REMOTE_SAFE failed; tonight's backup IS uploaded, but the destination will fill."
rclone rmdirs "$BACKUP_REMOTE" --leave-root >/dev/null 2>&1 || true

# 🔴 A FAILED LISTING MUST NOT READ AS "0 RETAINED". The first version swallowed
# rclone's exit status with 2>/dev/null inside a command substitution, so a remote
# that had just accepted an upload but could no longer be listed printed
# "done: 0 backup(s) retained" and exited 0 — the most alarming possible sentence,
# produced by the least alarming possible cause, on a job that had in fact succeeded.
if rclone lsf "$BACKUP_REMOTE" --dirs-only > "$STAGE/.kept" 2>"$STAGE/.kept.err"; then
  kept="$(awk 'NF{n++} END{print n+0}' "$STAGE/.kept")"
  say "done: $kept backup(s) retained at the destination"
else
  say "done: tonight's backup IS uploaded and verified, but the destination could not be listed afterwards, so the retained count is UNKNOWN (not zero). rclone said: $(tr '\n' ' ' < "$STAGE/.kept.err" | cut -c1-200)"
fi
