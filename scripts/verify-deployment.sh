#!/usr/bin/env bash
# Tappa -- post-rollout gates on a LIVE deployment (M8-02).
#
# Usage:
#   scripts/verify-deployment.sh cloudflare [host]        default: taptime.mt
#   scripts/verify-deployment.sh db-role    [namespace]   default: tappa
#   scripts/verify-deployment.sh all        [host] [ns]
#
# 🔴 THE `pull-credential` MODE WAS REMOVED (M8-02 FAZ D). Its whole subject was
# `secret/ghcr` -- it printed that Secret's managedFields timestamps beside the server
# pods' start times so an operator could tell whether a running pod would survive a
# restart under the kubelet's pull credential verification.
#
# WHY IT WENT, STATED AS A CONDITION RATHER THAN AS A FACT ABOUT TODAY -- because the
# unconditional version of this paragraph was WRONG when it was written, and the file
# it belongs to is the last place that should carry a stale claim:
#
#   ONCE a cluster pulls both images anonymously (a public repository, no
#   imagePullSecrets), no credential is recorded, so nothing can expire or be
#   overwritten and this mode would print "UNREADABLE -- no evidence printed" on every
#   healthy cluster forever -- a diagnostic alarming about a condition that cannot
#   arise, which is the shape this card keeps paying for.
#
#   WHILE a cluster still pulls with a credential, the condition IS live, and what this
#   mode used to print has to be read by hand. deploy/README.md carries the two
#   commands under "Olay müdahalesi" (secret/ghcr managedFields times vs server pod
#   start times) and states the rule: a pod that started BEFORE the newest write cannot
#   restart. The cure is a fresh, successful deploy.
#
# 🔴 WHICH OF THE TWO A GIVEN CLUSTER IS IN IS NOT ANSWERED HERE. The single record,
# with the command that measures it, is deploy/README.md, "Elle deploy / rollback".
#
# 🔴 WHY THIS IS A FILE. Twice in this task a gate written directly into
# .github/workflows/deploy.yml was wrong in a way that only running it could reveal:
# first an awk field-split that made the image gate fail on every run, then a shell
# heredoc at column 0 inside a YAML block scalar that made the whole workflow
# unparseable (`could not find expected ':' while scanning a simple key`). Both were
# caught by pointing a parser or a shell at them -- which is precisely what a gate
# embedded in YAML cannot have done to it. Same reasoning as scripts/verify-image.sh.
set -euo pipefail

mode=${1:?usage: verify-deployment.sh <cloudflare|db-role|all> [host] [namespace]}

# CF_OUTCOME records WHICH of the three things happened, because the return code
# cannot: this gate returns 0 both when the host is clean AND when the name does not
# resolve (a legitimate first-deploy state that must not stop a deploy), and returns 1
# both for a proxied host AND for an unreachable one. Those return codes are the
# `cloudflare` mode's contract and .github/workflows/deploy.yml depends on them, so
# they are unchanged; `all` reads this variable instead of guessing from the code.
CF_OUTCOME=unchecked
check_cloudflare() {
  local host=${1:-taptime.mt} headers
  # 🔴 A GATE RATHER THAN A LINE IN A RUNBOOK, BECAUSE THE DEFAULT IS THE WRONG ONE.
  # A NEW A record in Cloudflare defaults to PROXIED, and toggling a zone's proxy back
  # on is a one-click regression. taptime.mt's records were created DNS-only (grey
  # cloud) at cutover, but "created correct once" is not "stays correct", which is why
  # this is an exit code and not a note. The single most likely operator mistake — a
  # proxied primary — produces a deployment that looks completely healthy and in which:
  #   - §5's proof of place can never be true for anybody -- ingress-nginx runs with
  #     an EMPTY ConfigMap, so use-forwarded-headers is false and nginx REPLACES
  #     X-Forwarded-For with $remote_addr, which would be a Cloudflare edge address;
  #   - the panel's per-address login budget (120/10 min, backlog T30) collapses onto
  #     a handful of edge addresses shared by every customer;
  #   - a venue configured with a Cloudflare range would stamp "network proof of
  #     place" onto every tap on earth, into an immutable row (the mirror of T40).
  # None of that raises an alert anywhere, which is what makes it worth an exit code.
  # 🔴 A FAILED curl IS NOT ONE OUTCOME, AND TREATING IT AS ONE MADE THIS GATE
  # FAIL-OPEN (audit B5). The earlier version warned and returned 0 on ANY curl
  # error, while its message named a single cause ("the DNS record does not exist
  # yet"). A 20-second timeout on a busy runner, a transient TLS error or a
  # connection refused would all have been waved through as "first deploy" — a gate
  # that passes when it cannot see is not a gate.
  #
  # curl's exit code separates them: 6 is "could not resolve host", which is the one
  # state a first deploy is legitimately in. Everything else means the name DOES
  # resolve and we still could not get an answer, and that has to be loud, because an
  # unanswered check cannot distinguish a proxied host from a healthy one.
  local rc=0
  headers=$(curl -sSI --connect-timeout 10 --max-time 20 "https://${host}/healthz" 2>&1) || rc=$?
  if (( rc == 6 )); then
    CF_OUTCOME=unchecked
    echo "::warning::${host} does not resolve. If this is the first deploy that is expected: create an A record to 144.76.158.60 with the proxy OFF (grey cloud), then re-run. Nothing about Cloudflare could be checked."
    return 0
  fi
  if (( rc != 0 )); then
    CF_OUTCOME=unchecked
    echo "::error::${host} resolves but https could not be reached (curl exit ${rc}). This gate cannot tell a proxied host from a healthy one when it gets no answer, so it fails rather than passing blind. Output: ${headers}" >&2
    return 1
  fi
  if grep -qi '^cf-ray:' <<<"$headers"; then
    CF_OUTCOME=violation
    echo "::error::https://${host} is served THROUGH CLOUDFLARE (cf-ray present). Every client will resolve to a Cloudflare edge address: proof-of-place is dead, the panel login budget collapses to one key for all customers, and tap records will claim network evidence they do not have. Set the DNS record to 'DNS only' (grey cloud) — deploy/k8s/40-ingress.yaml carries the measurements." >&2
    grep -iE '^(server|cf-ray):' <<<"$headers" >&2 || true
    return 1
  fi
  CF_OUTCOME=pass
  echo "cloudflare: not proxied — the origin answers directly"
  # 'HTTP/' and not 'HTTP:' -- the status line is "HTTP/2 200", with no colon after
  # HTTP, so the previous pattern ^(HTTP|...): could never match it and the one line
  # that says WHICH response was inspected was silently missing from every run.
  # Measured against the live host: only strict-transport-security printed.
  grep -iE '^(HTTP/|server:|strict-transport-security:)' <<<"$headers" || true
}

check_db_role() {
  local ns=${1:-tappa} offenders
  # 🔴 WHAT THIS COVERS THAT internal/config CANNOT. config.Load refuses only the
  # case where the application DSN and the migration DSN are EQUAL. If somebody types
  # the owner DSN into the application's variable *alone* -- a plausible slip in
  # Infisical, where the two values sit next to each other -- nothing in the product
  # objects, and RLS is void from that moment: tappa_owner is initdb's bootstrap
  # SUPERUSER and bypasses every policy unconditionally. Measured: `rolbypassrls`,
  # `current_user` and `session_user` appear ZERO times in production code, so the
  # process itself cannot notice.
  #
  # (The migration variable is not spelled out anywhere in this file on purpose:
  # redline R5 reads that name under scripts/ as "the application connects with the
  # migration role", which is the correct default. scripts/seed.sh avoids it for the
  # same reason and says so.)
  #
  # The database can. Every pooled connection appears in pg_stat_activity under the
  # role that opened it, and a NETWORK connection (client_addr not null) at this
  # point in a deploy is the server: the migration Job has completed and its pod is
  # gone, and a `kubectl exec` psql arrives over the unix socket (client_addr null),
  # so this query does not accuse itself.
  #
  # ⚠️ THAT SENTENCE USED TO SAY "can ONLY be the server", AND M8-02 FAZ C MADE IT
  # SLIGHTLY LESS TRUE (2026-08-16). Both pod specs now carry a wait-for-postgres
  # initContainer that opens a TCP connection to 5432 and sends a startup packet
  # naming tappa_owner, so a network connection under that role is no longer proof
  # of a misconfigured server. It does NOT misfire today and the reason is timing,
  # not luck: an initContainer must terminate before its pod's main container is
  # created, and this gate runs after `kubectl rollout status` has already reported
  # the new pod Ready — so every such connection is long closed, and pg_stat_activity
  # only lists LIVE backends. The narrower true statement is therefore: at this point
  # a live network connection under a non-tappa_app role should not exist. Anything
  # that makes this gate run EARLIER (or any workload that holds a tappa_owner
  # connection open) breaks that, and this note is where to start looking.
  #
  # ⚠️ IT IS A DEPLOY-TIME GATE, NOT A RUNTIME ONE. It cannot catch a value changed
  # after the deploy. The in-process check is a counted limit; see the M8-02 card.
  local sql_offenders="SELECT usename || ' from ' || host(client_addr) FROM pg_stat_activity WHERE datname = current_database() AND client_addr IS NOT NULL AND usename <> 'tappa_app';"
  local sql_summary="SELECT usename, count(*) FROM pg_stat_activity WHERE datname = current_database() AND client_addr IS NOT NULL GROUP BY 1;"

  # PGPASSWORD is read from the container's OWN environment, so the password is
  # never an argument here and never reaches a CI log (CLAUDE.md §4.7).
  #
  # 🔴 -v ON_ERROR_STOP=1 IS WHAT MAKES THIS A GATE AT ALL, AND ITS ABSENCE WAS THE
  # THIRD INSTANCE OF THE DEFECT THIS FILE'S OWN HEADER DESCRIBES. Measured against a
  # real PostgreSQL 17.10, the same query text piped to psql on stdin:
  #   without ON_ERROR_STOP: "ERROR: column ... does not exist"  -> EXIT 0
  #   with    ON_ERROR_STOP: same error                          -> EXIT 3
  # psql reports a failed statement on stderr and still exits 0 unless told not to,
  # so if pg_stat_activity's shape or this role's permission on it ever changed, the
  # gate would compare NOTHING and report "every network connection is tappa_app" --
  # green, on no evidence. Healthy query measured both ways: exit 0.
  psql_as_owner() {
    kubectl -n "$ns" exec -i statefulset/tappa-postgres -- \
      sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -v ON_ERROR_STOP=1 -U tappa_owner -d tappa -At'
  }

  # The failure is caught EXPLICITLY rather than left to `set -e`, so the operator is
  # told the gate could not run instead of reading a bare non-zero exit.
  if ! offenders=$(printf '%s\n' "$sql_offenders" | psql_as_owner | sed '/^$/d'); then
    echo "::error::db-role: the pg_stat_activity query did not run, so NOTHING was checked. This is not a pass." >&2
    return 2
  fi
  if [[ -n $offenders ]]; then
    echo "::error::a network connection to the tappa database is open as a role other than tappa_app. RLS is not enforced for the schema owner, so tenant isolation is void for whatever opened it:" >&2
    printf '%s\n' "$offenders" >&2
    return 1
  fi
  # The summary is informational, but it runs as the same role against the same
  # view, so if it dies the offender query's clean result is no longer trustworthy
  # either. Failing here is CORRECT; failing SILENTLY is not -- measured before this
  # line existed, a broken summary killed the script with a bare exit 3 and no
  # explanation, which reads like a crash rather than a refusal.
  if ! printf '%s\n' "$sql_summary" | psql_as_owner; then
    echo "::error::db-role: the offender query passed but the summary query did not run, so this result is NOT trustworthy." >&2
    return 2
  fi
  echo "db-role: every network connection to the database is tappa_app"
}

case $mode in
  cloudflare)      check_cloudflare       "${2:-}" ;;
  db-role)         check_db_role          "${2:-}" ;;
  # 🔴 BOTH GATES RUN, ALWAYS, AND THE RESULTS ARE COLLECTED. The previous spelling
  # was `check_cloudflare ...; check_db_role ...` under `set -e`, which meant a
  # failing FIRST gate skipped the second entirely -- measured: with a proxied host
  # stubbed in, db-role never executed. The run was red, so nothing was reported as
  # passing that had not passed; but a mode named "all" that silently runs one of two
  # checks promises a completeness it does not deliver, which is this card's signature
  # defect. `|| rc=$?` keeps `set -e` from aborting between them.
  #
  # Exit code: 1 if any gate found a VIOLATION, else 2 if any gate could not CHECK,
  # else 0. A violation outranks an unreadable gate because it is the definite result.
  all)
    cf=0; db=0
    check_cloudflare "${2:-}" || cf=$?
    check_db_role    "${3:-}" || db=$?
    # 🔴 cloudflare's WORD comes from CF_OUTCOME, not from its exit code, and that is
    # the fix for a measured defect: with the name unresolvable this gate prints
    # "Nothing about Cloudflare could be checked" and returns 0, and `all` then
    # labelled it `pass` and exited 0 -- announcing a check it had just been told did
    # not happen. The mirror case: a curl timeout (rc=28) returned 1 and was labelled
    # `VIOLATION`, which it is not; it is another UNCHECKED. db-role already reports
    # three states through its exit code (0 pass / 1 offender found / 2 could not run).
    # A named helper, because an inline `case` inside "$( )" inside a double-quoted
    # string does not survive the surrounding `case` -- measured: the summary line
    # printed its own source text.
    _verdict() { case ${1:-} in 0) echo pass ;; 1) echo VIOLATION ;; *) echo UNCHECKED ;; esac; }
    cfw=$CF_OUTCOME; [[ $cfw == violation ]] && cfw=VIOLATION; [[ $cfw == unchecked ]] && cfw=UNCHECKED
    echo "all: cloudflare=$cfw db-role=$(_verdict "$db")"
    # Exit code matches the words above: a violation outranks an unreadable gate.
    if [[ $cfw == VIOLATION || $db -eq 1 ]]; then exit 1; fi
    if [[ $cfw == UNCHECKED || $db -ne 0 ]]; then exit 2; fi
    ;;
  *) echo "unknown mode: $mode (expected cloudflare, db-role or all)" >&2; exit 2 ;;
esac
