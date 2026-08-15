#!/usr/bin/env bash
# Tappa -- post-rollout gates on a LIVE deployment (M8-02).
#
# Usage:
#   scripts/verify-deployment.sh cloudflare [host]        default: tappa.everva.com.tr
#   scripts/verify-deployment.sh db-role    [namespace]   default: tappa
#   scripts/verify-deployment.sh all        [host] [ns]
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

check_cloudflare() {
  local host=${1:-tappa.everva.com.tr} headers
  # 🔴 A GATE RATHER THAN A LINE IN A RUNBOOK, BECAUSE THE DEFAULT IS THE WRONG ONE.
  # everva.com.tr is on Cloudflare with a PROXIED WILDCARD (measured 2026-08-15: an
  # invented subdomain resolves to the same two Cloudflare addresses), and a NEW A
  # record in Cloudflare defaults to PROXIED. So the single most likely operator
  # mistake produces a deployment that looks completely healthy and in which:
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
    echo "::warning::${host} does not resolve. If this is the first deploy that is expected: create an A record to 144.76.158.60 with the proxy OFF (grey cloud), then re-run. Nothing about Cloudflare could be checked."
    return 0
  fi
  if (( rc != 0 )); then
    echo "::error::${host} resolves but https could not be reached (curl exit ${rc}). This gate cannot tell a proxied host from a healthy one when it gets no answer, so it fails rather than passing blind. Output: ${headers}" >&2
    return 1
  fi
  if grep -qi '^cf-ray:' <<<"$headers"; then
    echo "::error::https://${host} is served THROUGH CLOUDFLARE (cf-ray present). Every client will resolve to a Cloudflare edge address: proof-of-place is dead, the panel login budget collapses to one key for all customers, and tap records will claim network evidence they do not have. Set the DNS record to 'DNS only' (grey cloud) — deploy/k8s/40-ingress.yaml carries the measurements." >&2
    grep -iE '^(server|cf-ray):' <<<"$headers" >&2 || true
    return 1
  fi
  echo "cloudflare: not proxied — the origin answers directly"
  grep -iE '^(HTTP|server|strict-transport-security):' <<<"$headers" || true
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
  # point in a deploy can only be the server: the migration Job has completed and its
  # pod is gone, and a `kubectl exec` psql arrives over the unix socket (client_addr
  # null), so this query does not accuse itself.
  #
  # ⚠️ IT IS A DEPLOY-TIME GATE, NOT A RUNTIME ONE. It cannot catch a value changed
  # after the deploy. The in-process check is a counted limit; see the M8-02 card.
  local sql_offenders="SELECT usename || ' from ' || host(client_addr) FROM pg_stat_activity WHERE datname = current_database() AND client_addr IS NOT NULL AND usename <> 'tappa_app';"
  local sql_summary="SELECT usename, count(*) FROM pg_stat_activity WHERE datname = current_database() AND client_addr IS NOT NULL GROUP BY 1;"

  # PGPASSWORD is read from the container's OWN environment, so the password is
  # never an argument here and never reaches a CI log (CLAUDE.md §4.7).
  psql_as_owner() {
    kubectl -n "$ns" exec -i statefulset/tappa-postgres -- \
      sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -U tappa_owner -d tappa -At'
  }

  offenders=$(printf '%s\n' "$sql_offenders" | psql_as_owner | sed '/^$/d')
  if [[ -n $offenders ]]; then
    echo "::error::a network connection to the tappa database is open as a role other than tappa_app. RLS is not enforced for the schema owner, so tenant isolation is void for whatever opened it:" >&2
    printf '%s\n' "$offenders" >&2
    return 1
  fi
  printf '%s\n' "$sql_summary" | psql_as_owner
  echo "db-role: every network connection to the database is tappa_app"
}

case $mode in
  cloudflare) check_cloudflare "${2:-}" ;;
  db-role)    check_db_role    "${2:-}" ;;
  all)        check_cloudflare "${2:-}"; check_db_role "${3:-}" ;;
  *) echo "unknown mode: $mode (expected cloudflare, db-role or all)" >&2; exit 2 ;;
esac
