#!/usr/bin/env bash
# Tappa -- `make sandbox-local-oauth` and `make sandbox-local-app`: the OAuth "Connected apps" half of a
# LOCAL sandbox, driven with curl (issue #37, ADR 0047 §8.6, §10.4; deploy/README.md "Sandbox (API-11)"
# §10). It runs after `make sandbox-local-flow` (which signed up the business, enrolled the operator and
# opened the API) and reuses that flow's secrets directory.
#
# TWO MODES:
#   register   the operator signs in on /operator/login (password + TOTP) and registers ONE app on
#              /operator/apps -- SANDBOX_APP_NAME, SANDBOX_APP_REDIRECT (one URI), SANDBOX_APP_SCOPES.
#              The page shows the client_id and the secret ONCE; they are written to SANDBOX_APP_OUT
#              (mode 600, two lines: client_id=... and client_secret=...), which must not exist yet.
#              Nothing of either value reaches the terminal.
#   flow       (the default) register a test app with a LOOPBACK redirect (the sandbox build alone
#              accepts one -- ADR 0047 §10.4), then do what the app and the business's owner do:
#                1. the owner signs in to the panel;
#                2. GET /oauth/authorize (as a top-level navigation from the app's site) -> the consent
#                   page -> "Allow": the 303 to the redirect URI carries code, state and iss;
#                3. POST /oauth/token -- HTTP Basic, form body, PKCE S256 code_verifier;
#                4. GET /v1/business and GET /v1/timesheets?from=2026-11-02&to=2026-11-08 with the
#                   access token -- the seeded week's figures are checked (make sandbox-local-seed) --
#                   and POST /v1/sandbox/taps, which is no OAuth route (403 insufficient_scope);
#                5. refresh (a new pair), revoke the refresh token (RFC 7009), and then both access
#                   tokens are 401 and the refresh token invalid_grant.
#
# NO SECRET ON A COMMAND LINE (ps shows every argv) -- sandbox-local-flow.sh's rule: a form value is
# `--data-urlencode name@file`, a header `-H @file`, a URL that carries a value `url = ...` in a
# `curl -K` file; each written by python3 or the shell's printf builtin, mode 600, under
# .sandbox-local/<db>.secrets/oauth-<n>/ (git-ignored). The terminal sees statuses, prefixes, lengths
# and the API's non-secret answers (the business card, the timesheet figures). At the end (an EXIT trap,
# a failure included) the operator's session is signed out (POST /operator/logout) and the files that
# are still a live credential are deleted: the registration page, the panel's and the operator's
# session cookies, EVERY response header file curl wrote (-D: a Set-Cookie travels in them -- the
# operator's sign-in challenge and session, the panel's), and, in the flow mode, the test app's
# credentials and Basic header; the code and the tokens stay, spent and revoked. The register mode's
# credentials go to SANDBOX_APP_OUT alone, which must be outside this repository tree.
#
# THE ENV FILE IS READ, NOT SOURCED: TAPPA_ADDR, TAPPA_OPERATOR_HOST, TAPPA_API_HOST and TAPPA_BASE_URL,
# each refused unless it is this machine's (loopback, *.localhost).
#
# Why curl and not a browser, and what it costs: sandbox-local-flow.sh's header says it (a hardened
# build over plain HTTP; the panel forms accept Origin: null + Sec-Fetch-Site: same-origin; the
# operator's __Host- cookies are carried by hand).
set -euo pipefail
cd "${0%/*}/.."

die() {
	echo "sandbox-local-oauth: $*" >&2
	exit 1
}

MODE=${1:-flow}
case $MODE in flow | register) ;; *) die "the mode is flow or register, not $MODE" ;; esac
# EVERY PRECONDITION BEFORE ANYTHING IS SENT.
DB=${SANDBOX_DB:-tappa_sbx_local}
ENV_FILE=.sandbox-local/$DB.env
[[ -f $ENV_FILE ]] || die "$ENV_FILE does not exist -- make sandbox-local SANDBOX_DB=$DB first"
S=.sandbox-local/$DB.secrets
[[ -d $S ]] || die "$S does not exist -- make sandbox-local-flow SANDBOX_DB=$DB first"
for f in owner-password operator-password operator-totp-key; do
	[[ -s $S/$f ]] || die "$S/$f is missing -- this database's sandbox-local-flow did not finish"
done
OWNER_EMAIL=${SANDBOX_OWNER_EMAIL:-owner@malta-calculator-test.example.com}
OPERATOR_EMAIL=${SANDBOX_OPERATOR_EMAIL:-operator@sandbox-local.example.com}
CALLBACK_PORT=${SANDBOX_CALLBACK_PORT:-18791}
[[ $CALLBACK_PORT =~ ^[1-9][0-9]{0,4}$ ]] || die "SANDBOX_CALLBACK_PORT must be a port number"
if [[ $MODE == register ]]; then
	APP_NAME=${SANDBOX_APP_NAME:?SANDBOX_APP_NAME (the name of the app) is required}
	APP_REDIRECT=${SANDBOX_APP_REDIRECT:?SANDBOX_APP_REDIRECT (its one redirect URI) is required}
	APP_SCOPES=${SANDBOX_APP_SCOPES:-timesheets:read employees:read employees:write}
	APP_OUT=${SANDBOX_APP_OUT:?SANDBOX_APP_OUT (the file the client_id and the secret are written to) is required}
	[[ -e $APP_OUT ]] && die "$APP_OUT exists: nothing is overwritten (pick another file)"
	[[ -d $(dirname "$APP_OUT") ]] || die "the directory of $APP_OUT does not exist"
	# The credentials never land in the repository tree (it is PUBLIC; .gitignore is no guard for a
	# file somebody adds by hand): the output's directory, symlinks resolved, is outside this tree.
	out_dir=$(cd "$(dirname "$APP_OUT")" && pwd -P)
	root=$(pwd -P)
	[[ $out_dir/ == "$root"/* ]] && die "$APP_OUT is inside the repository tree ($root): write the credentials outside it"
else
	APP_NAME="Taptime OAuth local test $(date -u +%Y%m%dT%H%M%SZ)"
	APP_REDIRECT=http://127.0.0.1:$CALLBACK_PORT/callback
	APP_SCOPES="timesheets:read employees:read employees:write"
fi
for c in curl python3; do command -v "$c" >/dev/null || die "$c is required"; done
curl -K /dev/null --version >/dev/null 2>&1 || die "this curl reads no -K config file"

# The env file is READ, not sourced: four values of it, none a secret, and nothing of it is exported
# (sourcing would run the file and hand every key it holds to every command below).
envval() {
	local line
	line=$(grep -m 1 "^$1=" "$ENV_FILE" || true)
	printf '%s' "${line#*=}"
}
TAPPA_ADDR=$(envval TAPPA_ADDR)
TAPPA_OPERATOR_HOST=$(envval TAPPA_OPERATOR_HOST)
TAPPA_API_HOST=$(envval TAPPA_API_HOST)
TAPPA_BASE_URL=$(envval TAPPA_BASE_URL)
# A local sandbox lives on this machine alone: the server on a loopback address, the operator's and
# the API's hosts loopback names (RFC 6761 *.localhost) -- the operator's session and the tokens are
# sent to them.
[[ $TAPPA_ADDR =~ ^(127\.0\.0\.1|localhost):[0-9]{1,5}$ ]] || die "TAPPA_ADDR in $ENV_FILE is not a loopback address and port"
loopname() { [[ $1 =~ ^([a-z0-9-]+\.)*localhost$ || $1 == 127.0.0.1 ]]; }
loopname "$TAPPA_OPERATOR_HOST" || die "TAPPA_OPERATOR_HOST in $ENV_FILE is not a loopback name (*.localhost)"
loopname "$TAPPA_API_HOST" || die "TAPPA_API_HOST in $ENV_FILE is not a loopback name (*.localhost)"
[[ $TAPPA_BASE_URL =~ ^https?://localhost(:[0-9]{1,5})?$ ]] || die "TAPPA_BASE_URL in $ENV_FILE is not this machine's"
PORT=${TAPPA_ADDR##*:}
PANEL=http://localhost:$PORT
OPS=http://$TAPPA_OPERATOR_HOST:$PORT
API=http://$TAPPA_API_HOST:$PORT
curl -fsS -o /dev/null "$PANEL/signup" || die "no sandbox answers on $PANEL -- make sandbox-local-run SANDBOX_DB=$DB in another terminal"
umask 077
n=1
while [[ -e $S/oauth-$n ]]; do n=$((n + 1)); done
O=$S/oauth-$n
mkdir "$O"
# Whatever happens, at the end the operator's session is signed out and the files that are a live
# credential go: the page that showed the secret once, the panel's and the operator's session cookies,
# every response header file a -D below writes (Set-Cookie travels in them -- TestSandboxLocalOAuth_
# NoSecretOnACommandLine reads each -D path off this script and wants it here), and -- in the flow
# mode, whose app is a throwaway -- the app's credentials and their Basic header (the register mode's
# credentials are its output). The code and the tokens stay: spent and revoked by then (or dead in an
# hour).
cleanup() {
	if [[ -s $O/operator.header ]]; then
		curl -sS -o /dev/null -H @"$O/operator.header" -H 'Origin: null' -H 'Sec-Fetch-Site: same-origin' \
			-X POST "$OPS/operator/logout" || true
	fi
	rm -f "$O/app.html" "$O/op-challenge.header" "$O/operator.header" "$O/panel.cookies" \
		"$O/op1.h" "$O/op2.h" "$O/app.h" "$O/allow.h" "$O/token-1.h" "$O/after-revoke-1.h" "$O/after-revoke-2.h"
	if [[ $MODE == flow ]]; then rm -f "$O/client.credentials" "$O/basic.header"; fi
}
trap cleanup EXIT

FETCH=(-H 'Origin: null' -H 'Sec-Fetch-Site: same-origin')
want() { [[ $1 == "$2" ]] || die "$3 answered $1, want $2 (the exchange is in $O)"; }
# py: python3 in isolated mode, its script on stdin, its arguments FILE PATHS (never a value).
py() { python3 -I - "$@"; }

# ---------------------------------------------------------------- the operator signs in --
echo "operator: signing in"
# The TOTP code of the next unused time step, into $O/v.code (a code is refused twice -- the flow's
# enrollment and an earlier run used theirs; the step is remembered in $S/operator-totp-step).
py "$S/operator-totp-key" "$O/v.code" "$S/operator-totp-step" <<'PY'
import base64, hashlib, hmac, os, struct, sys, time
k = open(sys.argv[1]).read().strip()
key = base64.b32decode(k + "=" * ((8 - len(k) % 8) % 8))
last = 0
try:
    last = int(open(sys.argv[3]).read().strip() or 0)
except FileNotFoundError:
    pass
while int(time.time()) // 30 <= last:
    time.sleep(1)
step = int(time.time()) // 30
h = hmac.new(key, struct.pack(">Q", step), hashlib.sha1).digest()
o = h[-1] & 15
for path, data in ((sys.argv[2], b"%06d" % ((struct.unpack(">I", h[o:o + 4])[0] & 0x7FFFFFFF) % 1000000)), (sys.argv[3], str(step).encode())):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.write(fd, data)
    os.close(fd)
PY
printf '%s' "$OPERATOR_EMAIL" >"$O/v.email"
code=$(curl -sS "${FETCH[@]}" -o "$O/op1.html" -D "$O/op1.h" -w '%{http_code}' -X POST "$OPS/operator/login" \
	--data-urlencode email@"$O/v.email" --data-urlencode password@"$S/operator-password")
want "$code" 303 "the operator's password step"
# cookie NAME HEADERS OUT: a Set-Cookie value of the headers file, as a "Cookie: ..." header file.
cookie() {
	py "$2" "$3" "$1" <<'PY' || die "no $1 cookie was set"
import os, sys
headers, out, name = sys.argv[1], sys.argv[2], sys.argv[3]
for line in open(headers, encoding="latin-1"):
    k, _, v = line.partition(":")
    if k.strip().lower() == "set-cookie" and v.strip().startswith(name + "="):
        fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        os.write(fd, ("Cookie: " + v.strip().split(";", 1)[0] + "\n").encode())
        os.close(fd)
        sys.exit(0)
sys.exit(1)
PY
}
cookie __Host-taptime_op_login "$O/op1.h" "$O/op-challenge.header"
code=$(curl -sS -H @"$O/op-challenge.header" "${FETCH[@]}" -o "$O/op2.html" -D "$O/op2.h" -w '%{http_code}' \
	-X POST "$OPS/operator/login/totp" --data-urlencode code@"$O/v.code")
want "$code" 303 "the operator's TOTP step"
cookie __Host-taptime_op "$O/op2.h" "$O/operator.header"

# ---------------------------------------------------------------- the operator registers the app --
echo "operator: registering \"$APP_NAME\" ($APP_REDIRECT; $APP_SCOPES)"
scopes=()
for s in $APP_SCOPES; do scopes+=(--data-urlencode "scope=$s"); done
code=$(curl -sS -H @"$O/operator.header" "${FETCH[@]}" -o "$O/app.html" -D "$O/app.h" -w '%{http_code}' \
	-X POST "$OPS/operator/apps" --data-urlencode "name=$APP_NAME" --data-urlencode "redirect_uris=$APP_REDIRECT" "${scopes[@]}")
want "$code" 200 "the registration"
CREDS=$O/client.credentials
[[ $MODE == register ]] && CREDS=$APP_OUT
# The page shows the client_id and then the secret, each in a <p class="op-key">, once: both into the
# credentials file, then the page is deleted (sandbox-local-flow.sh's rule for the page that showed
# the key).
py "$O/app.html" "$CREDS" <<'PY' || die "the registration page shows no client_id and secret"
import html, os, re, sys
page = open(sys.argv[1], encoding="utf-8").read()
vals = [html.unescape(v).strip() for v in re.findall(r'<p class="op-key">([^<]*)</p>', page)]
if len(vals) != 2 or not re.fullmatch(r"app_[0-9a-f-]{36}", vals[0]) or not re.fullmatch(r"tt_cs_(live|test)_[A-Za-z0-9_-]{43}", vals[1]):
    sys.exit(1)
fd = os.open(sys.argv[2], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
os.write(fd, ("client_id=%s\nclient_secret=%s\n" % (vals[0], vals[1])).encode())
os.close(fd)
PY
rm -f "$O/app.html"
if [[ $MODE == register ]]; then
	echo "sandbox-local-oauth: registered. client_id and client_secret are in $APP_OUT (mode 600; printed nowhere)."
	exit 0
fi

# ---------------------------------------------------------------- 1. the owner signs in --
echo "1/5 the owner signs in to the panel"
JAR=$O/panel.cookies
field() {
	py "$1" "$O/v.$2" "$2" <<'PY' || die "$1 has no $2 field"
import html, os, re, sys
m = re.search(r'name="%s" value="([^"]*)"' % re.escape(sys.argv[3]), open(sys.argv[1], encoding="utf-8").read())
if not m:
    sys.exit(1)
fd = os.open(sys.argv[2], os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
os.write(fd, html.unescape(m.group(1)).encode())
os.close(fd)
PY
}
curl -sS -c "$JAR" -b "$JAR" -o "$O/p1.html" "$PANEL/admin/login"
field "$O/p1.html" csrf
printf '%s' "$OWNER_EMAIL" >"$O/v.owner"
code=$(curl -sS -c "$JAR" -b "$JAR" "${FETCH[@]}" -o "$O/p2.html" -w '%{http_code}' -X POST "$PANEL/admin/login" \
	--data-urlencode csrf@"$O/v.csrf" --data-urlencode email@"$O/v.owner" --data-urlencode password@"$S/owner-password")
want "$code" 303 "the owner's sign-in"

# ---------------------------------------------------------------- 2. authorize and consent --
echo "2/5 GET /oauth/authorize -> the consent page -> Allow"
# The PKCE pair, the state and the authorization URL (a curl -K file: the URL carries the client_id,
# the state and the challenge -- none a secret, kept off argv all the same).
py "$CREDS" "$O" "$PANEL" "$APP_REDIRECT" "$APP_SCOPES" <<'PY'
import base64, hashlib, os, secrets, sys, urllib.parse
creds, out, panel, redirect, scopes = sys.argv[1:6]
cid = dict(l.split("=", 1) for l in open(creds).read().split())["client_id"]
def put(name, data):
    fd = os.open(os.path.join(out, name), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.write(fd, data.encode())
    os.close(fd)
verifier = secrets.token_urlsafe(48)[:64]
challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()
state = secrets.token_urlsafe(16)
q = urllib.parse.urlencode({"response_type": "code", "client_id": cid, "redirect_uri": redirect, "scope": scopes,
                            "state": state, "code_challenge": challenge, "code_challenge_method": "S256"})
put("v.verifier", verifier)
put("v.state", state)
put("authorize.cfg", 'url = "%s/oauth/authorize?%s"\n' % (panel, q))
PY
# A top-level navigation from the app's site: cross-site (ADR 0047 §4.2's named exception -- the GET
# that writes the pending request cookie). -L follows the front door's 303 to /admin/oauth/authorize.
code=$(curl -sS -L -K "$O/authorize.cfg" -c "$JAR" -b "$JAR" -H 'Sec-Fetch-Site: cross-site' -H 'Sec-Fetch-Mode: navigate' \
	-H 'Sec-Fetch-Dest: document' -o "$O/consent.html" -w '%{http_code}')
want "$code" 200 "the consent page"
py "$O/consent.html" "$O/allow.cfg" "$O/v.rd" "$PANEL" <<'PY' || die "the consent page has no Allow form (is the owner signed in, the business not suspended?)"
import html, os, re, sys
page = open(sys.argv[1], encoding="utf-8").read()
m = re.search(r'<form method="post" action="([^"]*)"[^>]*>\s*<input type="hidden" name="decision" value="allow"\s*/?>\s*<input type="hidden" name="rd" value="([^"]*)"', page)
if not m:
    sys.exit(1)
for path, data in ((sys.argv[2], 'url = "%s%s"\n' % (sys.argv[4], html.unescape(m.group(1)))), (sys.argv[3], html.unescape(m.group(2)))):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.write(fd, data.encode())
    os.close(fd)
PY
code=$(curl -sS -K "$O/allow.cfg" -c "$JAR" -b "$JAR" "${FETCH[@]}" -o "$O/allow.html" -D "$O/allow.h" -w '%{http_code}' \
	-X POST --data-urlencode decision=allow --data-urlencode rd@"$O/v.rd")
want "$code" 303 "Allow"
# The 303's Location: the registered redirect URI with code, state (ours, unchanged) and iss (RFC 9207 --
# this deployment's TAPPA_BASE_URL). The code goes to a file.
py "$O/allow.h" "$APP_REDIRECT" "$O/v.state" "$TAPPA_BASE_URL" "$O/v.code" <<'PY' || die "the redirect after Allow is not the registered URI with a code, our state and iss (see $O/allow.h)"
import os, sys, urllib.parse
headers, redirect, state, issuer, out = sys.argv[1:6]
loc = next((l.split(":", 1)[1].strip() for l in open(headers, encoding="latin-1") if l.lower().startswith("location:")), "")
base, _, query = loc.partition("?")
q = urllib.parse.parse_qs(query, strict_parsing=True)
if base != redirect or q.get("state") != [open(state).read()] or q.get("iss") != [issuer] or len(q.get("code", [])) != 1:
    sys.exit(1)
fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
os.write(fd, q["code"][0].encode())
os.close(fd)
print("    redirect: the registered URI with code (%s…, %d characters), our state, iss=%s" % (q["code"][0][:11], len(q["code"][0]), issuer))
PY

# ---------------------------------------------------------------- 3. the code exchange --
echo "3/5 POST /oauth/token (authorization_code: HTTP Basic, form body, PKCE S256)"
py "$CREDS" "$O/basic.header" <<'PY'
import base64, os, sys
c = dict(l.split("=", 1) for l in open(sys.argv[1]).read().split())
fd = os.open(sys.argv[2], os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
os.write(fd, ("Authorization: Basic %s\n" % base64.b64encode(("%s:%s" % (c["client_id"], c["client_secret"])).encode()).decode()).encode())
os.close(fd)
PY
# tokens ANSWER N: the token answer's pair into access-N/refresh-N and bearer-N.header; prints the
# answer's non-secret fields.
tokens() {
	py "$1" "$O" "$2" <<'PY' || die "the token answer $1 is not RFC 6749's"
import json, os, sys
a, out, n = json.load(open(sys.argv[1])), sys.argv[2], sys.argv[3]
if sorted(a) != ["access_token", "expires_in", "refresh_token", "scope", "token_type"] or a["token_type"] != "Bearer":
    sys.exit(1)
def put(name, data):
    fd = os.open(os.path.join(out, name), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    os.write(fd, data.encode())
    os.close(fd)
put("access-" + n, a["access_token"])
put("refresh-" + n, a["refresh_token"])
put("bearer-%s.header" % n, "Authorization: Bearer %s\n" % a["access_token"])
print("    token_type=%s expires_in=%d scope=\"%s\" access_token=%s… (%d) refresh_token=%s… (%d)" % (
    a["token_type"], a["expires_in"], a["scope"], a["access_token"][:11], len(a["access_token"]), a["refresh_token"][:11], len(a["refresh_token"])))
PY
}
code=$(curl -sS -H @"$O/basic.header" -o "$O/token-1.json" -D "$O/token-1.h" -w '%{http_code}' -X POST "$PANEL/oauth/token" \
	--data-urlencode grant_type=authorization_code --data-urlencode code@"$O/v.code" \
	--data-urlencode code_verifier@"$O/v.verifier" --data-urlencode "redirect_uri=$APP_REDIRECT")
want "$code" 200 "the code exchange"
grep -qi '^cache-control: no-store' "$O/token-1.h" || die "the token answer is cacheable"
tokens "$O/token-1.json" 1

# ---------------------------------------------------------------- 4. /v1 with the token --
echo "4/5 GET /v1/business, GET /v1/timesheets?from=2026-11-02&to=2026-11-08 and POST /v1/sandbox/taps with the access token"
code=$(curl -sS -H @"$O/bearer-1.header" -o "$O/business.json" -w '%{http_code}' "$API/v1/business")
want "$code" 200 "GET /v1/business"
echo "    business: $(python3 -I -c 'import json,sys; print(json.dumps(json.load(open(sys.argv[1]))))' "$O/business.json")"
code=$(curl -sS -H @"$O/bearer-1.header" -o "$O/timesheets.json" -w '%{http_code}' "$API/v1/timesheets?from=2026-11-02&to=2026-11-08")
want "$code" 200 "GET /v1/timesheets"
# The seeded week (internal/domain/checkin/timesheetseed_db_test.go -- make sandbox-local-seed).
# SANDBOX_SKIP_SEED_CHECK=1 prints the figures without checking them (a business with no seed).
py "$O/timesheets.json" "${SANDBOX_SKIP_SEED_CHECK:-0}" <<'PY' || die "the timesheet is not the seeded week's (make sandbox-local-seed first, or SANDBOX_SKIP_SEED_CHECK=1)"
import json, sys
page = json.load(open(sys.argv[1]))
want = {"MCT-001": (2400, 0, 0, 0), "MCT-002": (2550, 0, 0, 0), "MCT-003": (2400, 0, 0, 0),
        "MCT-004": (2400, 0, 0, 1), "MCT-005": (1440, 0, 1, 0), "MCT-006": (1920, 480, 0, 0)}
got = {}
for p in page["data"]:
    ref = p["employee"]["externalRef"]
    got[ref] = (p["workedMinutes"], p["awaitingMinutes"], p["openEntries"], p["manualShifts"])
    print("    %-8s workedMinutes %5d  awaitingMinutes %4d  openEntries %d  manualShifts %d" % ((ref,) + got[ref]))
print("    period %s..%s %s, complete %s, hasMore %s" % (page["period"]["from"], page["period"]["to"], page["period"]["timeZone"], page["complete"], page["hasMore"]))
if sys.argv[2] != "1" and (got != want or page["hasMore"]):
    sys.exit(1)
PY

# The simulation is no OAuth route (ADR 0047 §8.2, §8.6): a token is 403 insufficient_scope there.
printf '%s' '{"employeeRef":"MCT-001","locationRef":"MCT-FRONT","channel":"nfc"}' >"$O/tap.json"
code=$(curl -sS -H @"$O/bearer-1.header" -H 'Content-Type: application/json' -o "$O/tap-answer.json" -w '%{http_code}' \
	-X POST "$API/v1/sandbox/taps" --data-binary @"$O/tap.json")
want "$code" 403 "POST /v1/sandbox/taps with an access token"
python3 -I -c 'import json,sys; sys.exit(json.load(open(sys.argv[1])).get("code") != "insufficient_scope")' "$O/tap-answer.json" ||
	die "POST /v1/sandbox/taps refused a token with another code than insufficient_scope"
echo "    POST /v1/sandbox/taps with the access token: 403 insufficient_scope"

# ---------------------------------------------------------------- 5. refresh, revoke --
echo "5/5 refresh, revoke, and the next requests"
code=$(curl -sS -H @"$O/basic.header" -o "$O/token-2.json" -w '%{http_code}' -X POST "$PANEL/oauth/token" \
	--data-urlencode grant_type=refresh_token --data-urlencode refresh_token@"$O/refresh-1")
want "$code" 200 "the refresh"
tokens "$O/token-2.json" 2
cmp -s "$O/access-1" "$O/access-2" && die "the refresh answered the same access token"
cmp -s "$O/refresh-1" "$O/refresh-2" && die "the refresh answered the same refresh token"
code=$(curl -sS -H @"$O/bearer-2.header" -o /dev/null -w '%{http_code}' "$API/v1/business")
want "$code" 200 "GET /v1/business with the refreshed token"
code=$(curl -sS -H @"$O/basic.header" -o "$O/revoke.out" -w '%{http_code}' -X POST "$PANEL/oauth/revoke" \
	--data-urlencode token@"$O/refresh-2")
want "$code" 200 "POST /oauth/revoke"
echo "    revoke (the refresh token): 200"
for n in 1 2; do
	code=$(curl -sS -H @"$O/bearer-$n.header" -o "$O/after-revoke-$n.json" -D "$O/after-revoke-$n.h" -w '%{http_code}' "$API/v1/business")
	want "$code" 401 "access token $n after the revoke"
	grep -qi '^www-authenticate: Bearer realm="taptime", error="invalid_token"' "$O/after-revoke-$n.h" || die "access token $n: no invalid_token challenge"
	echo "    access token $n after the revoke: 401 invalid_token"
done
code=$(curl -sS -H @"$O/basic.header" -o "$O/token-3.json" -w '%{http_code}' -X POST "$PANEL/oauth/token" \
	--data-urlencode grant_type=refresh_token --data-urlencode refresh_token@"$O/refresh-2")
want "$code" 400 "the revoked refresh token"
python3 -I -c 'import json,sys; sys.exit(json.load(open(sys.argv[1])).get("error") != "invalid_grant")' "$O/token-3.json" ||
	die "the revoked refresh token is not invalid_grant"
echo "    the revoked refresh token: 400 invalid_grant"

cat <<DONE
sandbox-local-oauth: done. The code, the spent and revoked tokens and every answer are in $O (mode 600;
  no value was printed). The test app's credentials and the session cookies were deleted; the
  connection is revoked; the app stays registered.
DONE
