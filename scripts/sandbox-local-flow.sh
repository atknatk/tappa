#!/usr/bin/env bash
# Tappa -- `make sandbox-local-flow`: the browser half of a LOCAL sandbox, driven with curl (M11 API-14;
# deploy/README.md "Sandbox (API-11)" §9).
#
# It does what a person does in the panel and on the operator surface -- no shortcut around either:
#   1. signs up a business through /signup (three steps and the on-screen confirmation link a sandbox
#      shows instead of sending an e-mail);
#   2. creates an operator account with cmd/opadmin, applies its SQL as the owner, and enrolls it on
#      /operator/enroll (password + TOTP, the code computed from the enrollment page's key);
#   3. as that operator, opens the business's integration API (company code SANDBOX_CODE);
#   4. signs in to the panel as the business's owner and issues a tt_test_ key on /admin/api-keys.
# NO SECRET ON A COMMAND LINE (ps shows every argv): a form value travels as `--data-urlencode
# name@file`, a header as `-H @file`, a URL that carries a token as `url = ...` in a `curl -K` file --
# each file written by the shell's printf builtin, mode 600, in .sandbox-local/<db>.secrets/ (700,
# git-ignored). It prints the business id; the key, the two passwords, the TOTP key, the enrollment
# link, the session cookies and the key's Authorization header file stay in that directory and never
# reach the terminal. Read the key yourself: `curl -H @.sandbox-local/<db>.secrets/api-auth.header …`.
#
# WHY CURL AND NOT A BROWSER, and what it costs: a sandbox is hardened (TAPPA_BASE_URL must be https,
# every cookie Secure, __Host- on the operator's), and a local server speaks plain HTTP. The forms
# accept a browser's own fetch metadata (Origin: null + Sec-Fetch-Site: same-origin -- the shape the
# pages' no-referrer policy produces), so this script sends that; curl keeps Secure cookies for
# localhost but drops them for ops.localhost, so the operator's session cookie is carried by hand.
# A browser treating *.localhost as a secure context may manage the same steps; this script is the
# path that was measured.
#
# opadmin prints its enrollment link on stderr only when stderr is a character device (a terminal, or
# /dev/null, which discards it -- cmd/opadmin's stderrIsTerminal), and offers no other way out for the
# link: the SQL holds the secret's hash, never the secret. So the link is read from a PSEUDO-TERMINAL
# this script opens with python3's standard pty module (os.openpty), the same on macOS and Linux --
# not with script(1), whose arguments differ between BSD and util-linux and which busybox lacks.
# deploy/README.md's operator runbook forbids a recording terminal in production (O9-2): this is a
# throwaway local sandbox, and the recording is the link's only copy, kept beside the other secrets.
set -euo pipefail
cd "${0%/*}/.."

die() {
	echo "sandbox-local-flow: $*" >&2
	exit 1
}
# EVERY PRECONDITION BEFORE ANYTHING IS WRITTEN: a run that dies half-way leaves a business behind and
# a secrets directory that refuses the next run.
DB=${SANDBOX_DB:-tappa_sbx_local}
ENV_FILE=.sandbox-local/$DB.env
[[ -f $ENV_FILE ]] || die "$ENV_FILE does not exist -- make sandbox-local SANDBOX_DB=$DB first"
NAME=${SANDBOX_BUSINESS:-Malta Calculator Test Ltd}
CODE=${SANDBOX_CODE:-MCT}
OWNER_EMAIL=${SANDBOX_OWNER_EMAIL:-owner@malta-calculator-test.example.com}
OPERATOR_EMAIL=${SANDBOX_OPERATOR_EMAIL:-operator@sandbox-local.example.com}
[[ $NAME == *[Tt]est* ]] || die "SANDBOX_BUSINESS must say it is a test business (the seed refuses another)"
for c in curl psql python3 openssl go grep sed head cut; do command -v "$c" >/dev/null || die "$c is required"; done
python3 -I -c 'import os; m, s = os.openpty(); os.close(m); os.close(s)' 2>/dev/null ||
	die "python3 cannot open a pseudo-terminal here (opadmin prints its enrollment link only to a terminal)"
curl -K /dev/null --version >/dev/null 2>&1 || die "this curl reads no -K config file"

# shellcheck disable=SC1090
set -a
. "./$ENV_FILE"
set +a
PORT=${TAPPA_ADDR##*:}
PANEL=http://localhost:$PORT
OPS=http://$TAPPA_OPERATOR_HOST:$PORT
S=.sandbox-local/$DB.secrets
[[ -e $S ]] && die "$S exists: this flow already ran for $DB (start a new database with make sandbox-local)"
curl -fsS -o /dev/null "$PANEL/signup" || die "no sandbox answers on $PANEL -- make sandbox-local-run SANDBOX_DB=$DB in another terminal"
umask 077
mkdir -p "$S"
OWNER_PW=$S/owner-password
OP_PW=$S/operator-password
ENROLL_TOKEN=$S/enrollment-token

FETCH=(-H 'Origin: null' -H 'Sec-Fetch-Site: same-origin')
JAR=$S/panel.cookies
# field FILE NAME: a hidden input's value, written to $S/v.NAME (no newline) -- the file a form sends
# as `NAME@$S/v.NAME`.
field() {
	local v
	v=$(grep -o "name=\"$2\" value=\"[^\"]*\"" "$1" | head -1 | sed 's/.*value="//;s/"$//;s/&amp;/\&/g')
	[[ -n $v ]] || die "$1 has no $2 field"
	printf '%s' "$v" >"$S/v.$2"
}
# urlcfg PATH-AND-QUERY: a curl config file naming the panel URL, for a URL that carries a token.
urlcfg() {
	printf 'url = "%s"\n' "$PANEL/${1#/}" >"$S/url.cfg"
	echo "$S/url.cfg"
}
# post URL OUT [--data-urlencode ...]: one form POST with the panel's cookie jar; prints the status.
# URL "-" takes the URL from $S/url.cfg.
post() {
	local url=$1 out=$2
	shift 2
	if [[ $url == - ]]; then
		curl -sS -K "$S/url.cfg" -c "$JAR" -b "$JAR" "${FETCH[@]}" -o "$out" -D "$out.h" -w '%{http_code}' -X POST "$@"
	else
		curl -sS -c "$JAR" -b "$JAR" "${FETCH[@]}" -o "$out" -D "$out.h" -w '%{http_code}' -X POST "$url" "$@"
	fi
}
want() { [[ $1 == "$2" ]] || die "$3 answered $1, want $2 (the page is in $S)"; }

echo "1/4 signing up \"$NAME\""
openssl rand -base64 18 | tr -d '\n' >"$OWNER_PW"
curl -sS -c "$JAR" -b "$JAR" -o "$S/s1.html" "$PANEL/signup"
field "$S/s1.html" csrf
want "$(post "$PANEL/signup" "$S/s2.html" --data-urlencode csrf@"$S/v.csrf" --data-urlencode "name=$NAME" \
	--data-urlencode "vat_number=${SANDBOX_VAT:-MT12345678}" \
	--data-urlencode business_type=restaurant --data-urlencode structure=single)" 303 "the business step"
curl -sS -c "$JAR" -b "$JAR" -o "$S/s3.html" "$PANEL/signup/places"
field "$S/s3.html" csrf
want "$(post "$PANEL/signup/places" "$S/s4.html" --data-urlencode csrf@"$S/v.csrf" --data-urlencode "venue=Front door")" 303 "the places step"
curl -sS -c "$JAR" -b "$JAR" -o "$S/s5.html" "$PANEL/signup/account"
field "$S/s5.html" csrf
# The wizard refuses a registration completed faster than a person can type (8 s from its first
# page -- "signup refused: the form was completed faster than a person can type"): wait like one.
sleep 9
want "$(post "$PANEL/signup/account" "$S/s6.html" --data-urlencode csrf@"$S/v.csrf" --data-urlencode internal_ref= \
	--data-urlencode "full_name=Test Owner" --data-urlencode "email=$OWNER_EMAIL" \
	--data-urlencode password@"$OWNER_PW")" 303 "the account step"
curl -sS -c "$JAR" -b "$JAR" -o "$S/s7.html" "$PANEL/signup/sent"
LINK=$(grep -o 'href="https://[^"]*/admin/signup/confirm?t=[^"]*"' "$S/s7.html" | head -1 | sed 's/^href="//;s/"$//;s/&amp;/\&/g')
[[ -n $LINK ]] || die "the sent page shows no confirmation link"
urlcfg "${LINK#https://*/}" >/dev/null
unset LINK
curl -sS -K "$S/url.cfg" -c "$JAR" -b "$JAR" -o /dev/null
curl -sS -c "$JAR" -b "$JAR" -o "$S/s8.html" "$PANEL/admin/signup/confirm"
field "$S/s8.html" csrf
want "$(post "$PANEL/admin/signup/confirm" "$S/s9.html" --data-urlencode csrf@"$S/v.csrf" \
	--data-urlencode password@"$OWNER_PW")" 200 "the confirmation"
TENANT=$(psql -X "$SANDBOX_OWNER_DSN" -At -v ON_ERROR_STOP=1 -c "SELECT t.id FROM tenants t JOIN admin_users a ON a.tenant_id = t.id
  WHERE a.email = '$OWNER_EMAIL' AND a.role = 'owner' ORDER BY t.created_at DESC LIMIT 1")
[[ -n $TENANT ]] || die "the registration left no business"

echo "2/4 enrolling an operator"
go build -o "$S/opadmin" ./cmd/opadmin
# opadmin's stdout (the SQL) to a file, its stderr to a pseudo-terminal read into the recording.
python3 -I - "$S/opadmin.sql" "$S/opadmin.record" "$S/opadmin" create --email "$OPERATOR_EMAIL" --name "Local Operator" \
	--host "$TAPPA_OPERATOR_HOST" <<'PY' || die "opadmin create failed (its report is in $S/opadmin.record)"
import os, subprocess, sys
sql, record, argv = sys.argv[1], sys.argv[2], sys.argv[3:]
master, slave = os.openpty()
with open(sql, "wb") as out:
    child = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=out, stderr=slave)
os.close(slave)
data = b""
while True:
    try:
        chunk = os.read(master, 4096)
    except OSError:  # Linux: EIO once the child's side is closed
        break
    if not chunk:  # macOS: end of file
        break
    data += chunk
os.close(master)
rc = child.wait()
fd = os.open(record, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
os.write(fd, data)
os.close(fd)
sys.exit(rc)
PY
psql -X "$SANDBOX_OWNER_DSN" -q -v ON_ERROR_STOP=1 -f "$S/opadmin.sql" >/dev/null 2>&1 || die "applying opadmin's SQL failed"
ACCOUNT=$(grep -o 'enroll?id=[0-9a-f-]*' "$S/opadmin.record" | head -1 | cut -d= -f2)
[[ -n $ACCOUNT ]] || die "opadmin's report names no account"
curl -sS -o "$S/e1.html" "$OPS/operator/enroll?id=$ACCOUNT"
grep -o 'otpauth://[^"<]*' "$S/e1.html" | head -1 | grep -o 'secret=[A-Z2-7=]*' | cut -d= -f2 >"$S/operator-totp-key"
[[ -s $S/operator-totp-key ]] || die "the enrollment page shows no TOTP key"
openssl rand -base64 21 | tr -d '\n' >"$OP_PW"
grep -o 'enroll?id=[^#]*#[A-Za-z0-9_-]*' "$S/opadmin.record" | head -1 | cut -d'#' -f2 | tr -d '\r\n' >"$ENROLL_TOKEN"
field "$S/e1.html" id
field "$S/e1.html" blob
# The TOTP code of now, into $S/v.code (CLAUDE.md §7: a TOTP code is never on a command line or in a log).
python3 -I - "$S/operator-totp-key" "$S/v.code" <<'PY'
import base64, hashlib, hmac, os, struct, sys, time
k = open(sys.argv[1]).read().strip()
key = base64.b32decode(k + "=" * ((8 - len(k) % 8) % 8))
h = hmac.new(key, struct.pack(">Q", int(time.time()) // 30), hashlib.sha1).digest()
o = h[-1] & 15
fd = os.open(sys.argv[2], os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
os.write(fd, b"%06d" % ((struct.unpack(">I", h[o:o + 4])[0] & 0x7FFFFFFF) % 1000000))
os.close(fd)
PY
code=$(curl -sS "${FETCH[@]}" -o "$S/e2.html" -D "$S/e2.h" -w '%{http_code}' -X POST "$OPS/operator/enroll" \
	--data-urlencode id@"$S/v.id" --data-urlencode blob@"$S/v.blob" \
	--data-urlencode token@"$ENROLL_TOKEN" \
	--data-urlencode password@"$OP_PW" --data-urlencode password_again@"$OP_PW" \
	--data-urlencode code@"$S/v.code")
want "$code" 303 "the operator enrollment"
COOKIE=$(grep -i '^set-cookie: __Host-taptime_op=' "$S/e2.h" | head -1 | sed -E 's/^[Ss]et-[Cc]ookie: ([^;]*);.*/\1/' | tr -d '\r')
[[ -n $COOKIE ]] || die "the enrollment set no operator session"
printf 'Cookie: %s\n' "$COOKIE" >"$S/operator.header"
unset COOKIE

echo "3/4 opening the business's API (company code $CODE)"
code=$(curl -sS -H @"$S/operator.header" "${FETCH[@]}" -o "$S/o1.html" -D "$S/o1.h" -w '%{http_code}' \
	-X POST "$OPS/operator/tenants/$TENANT/integration" --data-urlencode "api_code=$CODE")
want "$code" 303 "opening the API"
grep -qi '^location: .*done=api-opened' "$S/o1.h" || die "the operator surface did not open the API (see $S/o1.h)"

echo "4/4 issuing a tt_test_ key as the owner"
rm -f "$JAR"
curl -sS -c "$JAR" -b "$JAR" -o "$S/p1.html" "$PANEL/admin/login"
field "$S/p1.html" csrf
want "$(post "$PANEL/admin/login" "$S/p2.html" --data-urlencode csrf@"$S/v.csrf" \
	--data-urlencode "email=$OWNER_EMAIL" \
	--data-urlencode password@"$OWNER_PW")" 303 "the owner's sign-in"
curl -sS -c "$JAR" -b "$JAR" -o "$S/k1.html" "$PANEL/admin/api-keys"
ACTION=$(grep -o 'action="/admin/api-keys?tb=[^"]*"' "$S/k1.html" | head -1 | sed 's/^action="//;s/"$//;s/&amp;/\&/g')
[[ -n $ACTION ]] || die "the API keys section shows no form (is the API open?)"
urlcfg "$ACTION" >/dev/null
unset ACTION
want "$(post - "$S/k2.html" --data-urlencode "name=Malta Calculator (local)")" 200 "issuing the key"
KEY=$(grep -o 'tt_test_[A-Za-z0-9_-]\{43\}' "$S/k2.html" | head -1)
[[ -n $KEY ]] || die "the panel showed no key"
printf '%s' "$KEY" >"$S/api-key"
printf 'Authorization: Bearer %s\n' "$KEY" >"$S/api-auth.header"
unset KEY
rm -f "$S/k2.html" # the page that showed the key once

cat <<DONE
sandbox-local-flow: done.
  business  $TENANT  ("$NAME", API open, company code $CODE)
  key       $S/api-key   (tt_test_, shown nowhere else; the panel lists it as "Malta Calculator (local)")
  header    $S/api-auth.header   (curl -H @$S/api-auth.header …)
  Next:     make sandbox-local-seed SANDBOX_DB=$DB TAPPA_TIMESHEET_SEED_TENANT=$TENANT
DONE
