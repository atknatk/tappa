#!/usr/bin/env bash
# Tappa -- the pre-push gate on a built server image (M8-02).
#
# Usage: scripts/verify-image.sh <image-ref> <expected-full-sha>
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
set -euo pipefail

image=${1:?usage: verify-image.sh <image-ref> <expected-full-sha> | verify-image.sh --migrate <image-ref>}

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

want=${2:?usage: verify-image.sh <image-ref> <expected-full-sha>}

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

non_root "$image"

if (( fail )); then
  echo "verify-image: FAILED" >&2
  exit 1
fi
echo "verify-image: $image is traceable to $want, carries tzdata, has no migration tool and runs non-root"
