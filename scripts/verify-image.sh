#!/usr/bin/env bash
# Tappa -- the pre-push gate on a built server image (M8-02).
#
# Usage: scripts/verify-image.sh <image-ref> <expected-full-sha>              the production server
#        scripts/verify-image.sh --sandbox <image-ref> <expected-full-sha>    the sandbox server
#        scripts/verify-image.sh --migrate <image-ref>                        the migration image
#        scripts/verify-image.sh --binary-tags prod|sandbox <binary>          criterion 6 alone, on a
#                                                                             local binary (the test)
#        scripts/verify-image.sh --access <image-ref>...                      after the push: pullable
#                                                                             WITH this client's
#                                                                             credential, refused
#                                                                             WITHOUT one (OPS-2)
#
# 🔴 WHY THIS IS A FILE AND NOT EIGHT LINES INSIDE deploy.yml, which is where it
# started. The first version lived in the workflow and was WRONG in a way nothing
# could have caught before a real deploy: it parsed `go version -m` with
# `awk '$2=="vcs.revision"{print $3}'`, but that output is TAB separated with the
# value glued to the key --
#
#     <tab>build<tab>vcs.revision=c2d77d2b26b5333dcda528a6afbfa2ac4cd3bca5
#
# so $2 is `vcs.revision=<sha>` (never `vcs.revision`) and $3 does not exist. Both
# variables came out EMPTY, the comparison against the deploy sha was therefore
# always unequal, and the gate failed EVERY run -- before the push step, so the
# product could not be deployed at all. A security audit measured it.
#
# ⚠️ AND THE SECOND-ORDER HAZARD IS WORSE THAN THE BUG: the natural repair for a
# gate that is red on every run is to loosen it. A gate that only ever runs inside
# CI is a gate nobody can test; one that runs from a shell can be pointed at a good
# image and a bad one, which is what the report for this task does.
#
# It exits 0 only if ALL of the following hold, and each one is a criterion some
# other part of this repository depends on:
#
#   1. vcs.revision == the commit being deployed   (M8-01: the artifact names itself)
#   2. vcs.modified == false                       (M8-01 hand-off item 2: nothing
#                                                   refused an untraceable binary)
#   3. the IANA timezone database is compiled in   (M8-01: scratch has no zoneinfo,
#                                                   and every shift start resolves
#                                                   through time.LoadLocation)
#   4. goose is NOT in the image                   (M8-01: the application must not
#                                                   be able to migrate)
#   5. the image runs as a non-root uid            (deploy/k8s/20-app.yaml pins the
#                                                   same uid; this is the other half)
#   6. the build's tags are the image's            (M11 API-11, ADR 0028 §11: the
#                                                   production server carries NO `sandbox`
#                                                   tag -- the simulation is not in it -- and
#                                                   the sandbox server, --sandbox, carries it)
set -euo pipefail

image=${1:?usage: verify-image.sh [--sandbox] <image-ref> <expected-full-sha> | verify-image.sh --migrate <image-ref> | verify-image.sh --binary-tags prod|sandbox <binary> | verify-image.sh --access <image-ref>...}

fail=0
say()  { printf '  %-52s %s\n' "$1" "$2"; }
bad()  { fail=1; printf '::error::%s\n' "$1" >&2; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# non_root checks the image's configured user; shared by both modes.
non_root() {
  local user
  user=$(docker inspect "$1" --format '{{.Config.User}}')
  case $user in
    ''|0|0:*|root|root:*) bad "$1 would run as root (User=${user:-<unset>})" ;;
    *)                    say "image User" "$user" ;;
  esac
}

# 6. THE BUILD'S TAGS (M11 API-11). `go version -m` prints the build settings the binary was compiled
# with, `-tags=` among them when any tag was given (comma separated). The judgment reads the WORD
# `sandbox` in that list, not the line: a production binary with any tag list that names sandbox is
# refused, and a sandbox binary without it is refused. $1 is the stamps, $2 prod|sandbox.
judge_tags() {
  local tags has=0 t
  tags=$(sed -n 's/^[[:space:]]*build[[:space:]]*-tags=//p' <<<"$1")
  local IFS=,
  for t in $tags; do
    [[ $t == sandbox ]] && has=1
  done
  case $2 in
    prod)
      if (( has )); then
        bad "the production server was built with the sandbox tag (-tags=$tags): it would contain the tap simulation (ADR 0028 §11)"
      else
        say "build tags" "${tags:-<none>} (no sandbox)"
      fi ;;
    sandbox)
      if (( has )); then
        say "build tags" "$tags (the sandbox build)"
      else
        bad "the sandbox server was built WITHOUT the sandbox tag (-tags=${tags:-<none>}): it would refuse TAPPA_ENV=sandbox and has no simulation"
      fi ;;
    *) bad "judge_tags: unknown build kind $2" ;;
  esac
}

if [[ $image == --binary-tags ]]; then
  kind=${2:?usage: verify-image.sh --binary-tags prod|sandbox <binary>}
  bin=${3:?usage: verify-image.sh --binary-tags prod|sandbox <binary>}
  judge_tags "$(go version -m "$bin")" "$kind"
  if (( fail )); then echo "verify-image: FAILED" >&2; exit 1; fi
  echo "verify-image: $bin carries the $kind build's tags"
  exit 0
fi

# 🔴 --access: THE REGISTRY IS PRIVATE, MEASURED IN BOTH DIRECTIONS (OPS-2). It replaced the
# deploy's inline "both images must be pullable with NO credential" gate, whose subject was a
# PUBLIC Docker Hub repository; the images now live in a private Harbor project, so the
# question has two halves and each is red on its own. For every ref:
#
#   WITH the credential this docker client holds (the deploy's `docker login`, the ci robot)
#     the manifest must come back -- `"schemaVersion"` in the output, not merely exit 0: a
#     stub that exits 0 and prints nothing was once waved through by the old gate's first
#     draft (a healthy answer and an empty one are not the same thing). Its output is NOT
#     printed: it ran with a credential, and an error path is where a client would say more
#     than it should; the exit code is.
#   WITHOUT one -- asked with curl, by the Registry v2 token flow, NOT with docker (OPS-2 fix
#     round 1). The first version pointed DOCKER_CONFIG at an empty directory and read docker's
#     words, and it was red on EVERY run: docker 29 reports Harbor's anonymous 401 as
#     "no such manifest: <ref>" -- no "unauthorized" anywhere in what it prints (measured
#     2026-10-19 with docker 29.8.2 against a local registry that answers exactly as Harbor
#     does: /v2/ 401 with a Bearer challenge, an anonymous token, the manifest 401
#     UNAUTHORIZED). A client's paraphrase is not the registry's answer, so the answer is now
#     read off the wire:
#       1. GET https://HOST/v2/ with no credential. 401 with a `Bearer realm=...,service=...`
#          challenge -> step 2. 200 (a registry that asks for nothing) -> step 3 without a
#          token. Anything else is red.
#       2. GET the realm, ?service=<the challenge's>&scope=repository:REPO:pull, with no
#          credential: the anonymous token, which is what a public repository would be read
#          with. It must be 200 and carry a non-empty "token" (or "access_token"); anything
#          else is red -- in particular NOT "refused", because a manifest asked for with no
#          token is a 401 even on a PUBLIC repository, so an empty token would make every
#          registry look private.
#       3. GET https://HOST/v2/REPO/manifests/TAG with that token.
#            401 and the error code UNAUTHORIZED   -> REFUSED: the expected answer (Harbor's)
#            200                                   -> RED: the repository is pullable by anyone
#            any other code, a transport failure,
#            no answer, a 401 without UNAUTHORIZED -> RED: it cannot show that the registry
#                                                     refuses (5xx, DNS, a timeout, empty)
#     curl runs with -q first, so no ~/.curlrc can add a credential, and with --proto =https,
#     so nothing it is redirected or challenged to is plain HTTP. No credential exists in this
#     half to print, so the registry's HTTP code and error code ARE printed (a word of
#     [A-Z_]); the bodies are not.
#
# Exit 0 only if every ref passes both halves; 1 otherwise. Every response is written under
# the script's own work dir and removed on exit.
# cmd/tappa/deployregistry_test.go runs this against a stub docker and the REAL curl, pointed
# (by --connect-to) at a local server that answers as Harbor does.
anon_get() { # url out [curl args...] -> prints the HTTP code ("000" when none); exit = curl's
  local url=$1 out=$2
  shift 2
  curl -q -sS --proto =https --max-time 20 --connect-timeout 10 -o "$out" -D "$out.h" -w '%{http_code}' "$@" "$url" 2>"$out.err"
}
# challenge_param NAME HEADERFILE -> the value of NAME="..." in the Bearer challenge, or nothing.
challenge_param() {
  tr -d '\r' <"$2" | sed -n 's/^[Ww][Ww][Ww]-[Aa][Uu][Tt][Hh][Ee][Nn][Tt][Ii][Cc][Aa][Tt][Ee]:[[:space:]]*[Bb]earer[[:space:]]//p' |
    tr ',' '\n' | sed -n "s/^[[:space:]]*$1=\"\\([^\"]*\\)\"[[:space:]]*\$/\\1/p" | sed -n '1p'
}
# registry_code FILE -> the first error code in a Registry v2 error body, or "none".
registry_code() {
  local c
  c=$(grep -o '"code"[[:space:]]*:[[:space:]]*"[A-Z_]*"' "$1" 2>/dev/null | sed -n '1s/.*"\([A-Z_]*\)"$/\1/p') || c=
  printf '%s' "${c:-none}"
}
# anonymous REF -> 0 refused (printed), 1 red (said why)
anonymous() {
  local ref=$1 host rest repo tag last w code rc realm service token
  host=${ref%%/*} rest=${ref#*/}
  if [[ $ref != */* || $host != *.* ]]; then
    bad "$ref: not a registry reference (no registry host), so the anonymous half cannot ask it"
    return 1
  fi
  if [[ $rest == *@* ]]; then
    repo=${rest%%@*} tag=${rest#*@}
  else
    last=${rest##*/}
    [[ $last == *:* ]] && repo=${rest%:*} tag=${last##*:}
  fi
  if [[ -z ${repo:-} || -z ${tag:-} ]]; then
    bad "$ref: names no tag or digest, so the anonymous half has no manifest to ask for"
    return 1
  fi
  w="$work/anon-$((++anon_n))"
  rc=0
  code=$(anon_get "https://$host/v2/" "$w.v2") || rc=$?
  case $code in
    401)
      realm=$(challenge_param realm "$w.v2.h")
      service=$(challenge_param service "$w.v2.h")
      if [[ -z $realm ]]; then
        bad "$ref: the registry answered /v2/ with 401 but no Bearer challenge (no realm), so no anonymous token can be asked for and this run cannot show that it refuses anonymous pulls"
        return 1
      fi
      rc=0
      code=$(anon_get "$realm" "$w.token" --get --data-urlencode "service=$service" --data-urlencode "scope=repository:$repo:pull") || rc=$?
      token=$(sed -n 's/.*"token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$w.token" 2>/dev/null | sed -n '1p')
      [[ -n $token ]] || token=$(sed -n 's/.*"access_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$w.token" 2>/dev/null | sed -n '1p')
      if [[ $code != 200 || -z $token ]]; then
        bad "$ref: the registry's token service did not hand out an anonymous token (HTTP $code, curl exit $rc), so this run cannot show that the registry refuses anonymous pulls -- a manifest asked for without a token is refused even by a public repository"
        return 1
      fi
      rc=0
      code=$(anon_get "https://$host/v2/$repo/manifests/$tag" "$w.manifest" -H "Authorization: Bearer $token" \
        -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json') || rc=$?
      ;;
    200)
      rc=0
      code=$(anon_get "https://$host/v2/$repo/manifests/$tag" "$w.manifest" \
        -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json') || rc=$?
      ;;
    *)
      bad "$ref: the registry's /v2/ answered HTTP $code (curl exit $rc), neither a 401 challenge nor 200, so this run cannot show that it refuses anonymous pulls (unreachable? a 5xx?)"
      sed -n '1p' "$w.v2.err" 2>/dev/null | cut -c1-200 >&2
      return 1
      ;;
  esac
  case $code in
    200)
      bad "$ref IS pullable WITHOUT a credential (HTTP 200 for its manifest with an anonymous token): the registry project is public. It must be private (deploy/README.md, operator step 4); nothing has been applied to the cluster."
      return 1
      ;;
    401)
      if [[ $(registry_code "$w.manifest") == UNAUTHORIZED ]]; then
        say "pull WITHOUT a credential" "refused: $ref (HTTP 401 UNAUTHORIZED)"
        return 0
      fi
      ;;
  esac
  bad "$ref: the anonymous manifest request got HTTP $code (registry code $(registry_code "$w.manifest"), curl exit $rc), NOT the 401 UNAUTHORIZED a private repository answers, so this run cannot show that the registry refuses anonymous pulls (unreachable? a timeout? an empty answer?)"
  sed -n '1p' "$w.manifest.err" 2>/dev/null | cut -c1-200 >&2
  return 1
}
if [[ $image == --access ]]; then
  shift
  (( $# > 0 )) || { echo "usage: verify-image.sh --access <image-ref>..." >&2; exit 2; }
  anon_n=0
  for ref in "$@"; do
    rc=0
    out=$(docker manifest inspect "$ref" 2>&1) || rc=$?
    if (( rc == 0 )) && [[ $out == *'"schemaVersion"'* ]]; then
      say "pull WITH the registry credential" "ok: $ref"
    else
      bad "$ref is NOT pullable with the credential this job logged in with (docker manifest inspect exit $rc; its output is not shown, it ran with the credential). Check the ci robot's permissions on the project and that the push above went to this name."
      continue
    fi
    anonymous "$ref" || true
  done
  if (( fail )); then echo "verify-image: FAILED" >&2; exit 1; fi
  echo "verify-image: $# image(s) pullable with the registry credential and refused without one"
  exit 0
fi

kind=prod
if [[ $image == --sandbox ]]; then
  kind=sandbox
  shift
  image=${1:?usage: verify-image.sh --sandbox <image-ref> <expected-full-sha>}
fi

# 🔴 --migrate GATES THE OTHER ARTIFACT, WHICH WAS SHIPPING UNCHECKED (audit B7).
# Only the server image was ever verified, and the server is the one that CANNOT
# touch the schema. The migration image is the one that runs DDL as tappa_owner —
# initdb's bootstrap SUPERUSER, the role that bypasses RLS unconditionally — so an
# unverified one is strictly the more dangerous of the two.
#
# It cannot be checked the same way: goose is built from its OWN module, so the
# binary's buildinfo names goose's revision and not this repository's. What ties it
# to this commit is its PAYLOAD — the migrations it carries must be byte-identical to
# db/migrations in the tree being deployed. That is the property that actually
# matters: a migrate image built from a different commit's migrations would apply
# statements nobody reviewed.
if [[ $image == --migrate ]]; then
  image=${2:?usage: verify-image.sh --migrate <image-ref>}
  repo_root=$(cd "$(dirname "$0")/.." && pwd)

  cid=$(docker create "$image")
  docker cp "$cid":/migrations "$work/migrations" >/dev/null
  docker rm -f "$cid" >/dev/null

  digest_of() { # sha256 over "name  sha256" of every .sql, sorted — order-independent
    (cd "$1" && find . -name '*.sql' -type f | sort | while read -r f; do
        printf '%s  %s\n' "${f#./}" "$(sha256sum "$f" 2>/dev/null | cut -d' ' -f1 || shasum -a 256 "$f" | cut -d' ' -f1)"
      done) | sha256sum 2>/dev/null | cut -d' ' -f1 || \
    (cd "$1" && find . -name '*.sql' -type f | sort | while read -r f; do
        printf '%s  %s\n' "${f#./}" "$(shasum -a 256 "$f" | cut -d' ' -f1)"
      done) | shasum -a 256 | cut -d' ' -f1
  }
  in_image=$(digest_of "$work/migrations")
  in_tree=$(digest_of "$repo_root/db/migrations")
  n_image=$(find "$work/migrations" -name '*.sql' -type f | wc -l | tr -d ' ')
  n_tree=$(find "$repo_root/db/migrations" -name '*.sql' -type f | wc -l | tr -d ' ')

  say "migrations in image" "$n_image"
  say "migrations in tree" "$n_tree"
  if [[ $in_image == "$in_tree" && -n $in_image ]]; then
    say "migration set digest" "${in_image:0:16}… (matches the tree)"
  else
    bad "the migrations inside $image are NOT the ones in db/migrations (image ${in_image:0:16}…, tree ${in_tree:0:16}…): this image would apply DDL that does not belong to the commit being deployed"
  fi

  non_root "$image"

  if (( fail )); then echo "verify-image: FAILED" >&2; exit 1; fi
  echo "verify-image: $image carries exactly this tree's $n_tree migrations and runs non-root"
  exit 0
fi

want=${2:?usage: verify-image.sh [--sandbox] <image-ref> <expected-full-sha>}

cid=$(docker create "$image")
docker cp "$cid":/tappa "$work/tappa" >/dev/null
docker rm -f "$cid" >/dev/null

stamps=$(go version -m "$work/tappa")

# 🔴 THE PARSE. sed strips the whole `<tab>build<tab>vcs.revision=` prefix and
# prints only the value, so there is no field-splitting to get wrong. An ABSENT
# key yields an empty string, which is checked explicitly below rather than being
# allowed to compare unequal and produce a confusing message.
field() { sed -n "s/^[[:space:]]*build[[:space:]]*$1=//p" <<<"$stamps"; }

rev=$(field 'vcs\.revision')
mod=$(field 'vcs\.modified')

if [[ -z $rev ]]; then
  bad "the image carries no vcs.revision at all: .git was missing from the build context, so this binary cannot be traced to a commit"
else
  say "vcs.revision" "$rev"
  [[ $rev == "$want" ]] || bad "the image was built from $rev but this deploy is $want"
fi

if [[ -z $mod ]]; then
  bad "the image carries no vcs.modified stamp, so nothing can tell whether it is traceable"
else
  say "vcs.modified" "$mod"
  [[ $mod == false ]] || bad "this binary reports vcs.modified=$mod, so its revision does not identify it (a tracked path excluded by .dockerignore looks like a deletion inside the builder, and so does an uncommitted edit)"
fi

# Africa/Abidjan is the first entry of the zip time/tzdata embeds; the same marker
# cmd/tappa/packaging_test.go uses, and its discriminating power is proved there by
# two control binaries differing only in the blank import.
if grep -a -q 'Africa/Abidjan' "$work/tappa"; then
  say "IANA timezone database" "present"
else
  bad "the shipped binary does not carry the IANA timezone database; on a scratch image every shift start and lateness verdict would silently resolve in UTC"
fi

if grep -a -q 'pressly/goose' "$work/tappa"; then
  bad "the server image contains goose: the application must not be able to run a migration"
else
  say "goose" "absent"
fi

judge_tags "$stamps" "$kind"

non_root "$image"

if (( fail )); then
  echo "verify-image: FAILED" >&2
  exit 1
fi
echo "verify-image: $image ($kind) is traceable to $want, carries tzdata, has no migration tool, carries the $kind build's tags and runs non-root"
