#!/usr/bin/env bash
# Regenerates the reference matrices internal/qrcode's tests compare against, with the
# command-line tool of libqrencode (an independent implementation of ISO/IEC 18004). The
# tests do NOT run this script -- CI has no qrencode -- they read what it wrote, which is
# committed. Run it from anywhere; it writes into its own directory.
#
#   vectors   *.in (the input bytes) and <name>-<level>.txt (qrencode -8 -m 0 -t ASCII:
#             a module is "##" dark or "  " light, a row per line, no quiet zone)
#   sweep.txt every version 1-40 at every level: the input is as long as the version holds
#             at that level -- the length found by qrencode itself, --strict-version
#             accepting it and refusing one byte more -- and the line records the length,
#             the mask qrencode chose (read from the format information it drew) and the
#             sha256 of its ASCII matrix. The input's bytes are sweepInput's formula
#             (qrcode_test.go): byte i is ALPHA[(37*i + 11*version + 5*level) mod 64],
#             level 0-3 for L M Q H.
#
# The inputs are invented; the otpauth URI's key is the letters FAKE repeated, not a key.
set -euo pipefail
cd "$(dirname "$0")"
command -v qrencode >/dev/null || { echo "generate.sh: qrencode is not installed" >&2; exit 1; }
QRVER=$(qrencode --version 2>&1 | head -n 1)

printf '%s' 'taptime' >short.in
printf '%s' 'Taptime operator enrollment -- a version 7 vector: one hundred and twenty bytes of plain ASCII text, padded out.' >v7.in
printf '%s' 'otpauth://totp/Taptime%20operator:ops.taptime.mt?secret=FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE&issuer=Taptime%20operator&algorithm=SHA1&digits=6&period=30' >otpauth.in
for l in L M Q H; do qrencode -8 -l "$l" -m 0 -t ASCII -r short.in -o "short-$l.txt"; done
qrencode -8 -l M -m 0 -t ASCII -r v7.in -o v7-M.txt
qrencode -8 -l M -m 0 -t ASCII -r otpauth.in -o otpauth-M.txt

ALPHA='ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'
sweep_input() { # version levelindex length
  awk -v v="$1" -v l="$2" -v n="$3" -v a="$ALPHA" 'BEGIN {
    for (i = 0; i < n; i++) printf "%s", substr(a, (37*i + 11*v + 5*l) % 64 + 1, 1) }'
}
fits() { # version level levelindex length
  sweep_input "$1" "$3" "$4" >sweep.tmp
  qrencode -8 -l "$2" -v "$1" --strict-version -m 0 -t ASCII -r sweep.tmp -o /dev/null 2>/dev/null
}
# mask_of: the mask a matrix's format information names. Its 15 bits, bit 0 first, are at
# (x=8, y=0..5), (8,7), (8,8), (7,8), then bits 9-14 at (x=5..0, y=8); they are XORed with
# 101010000010010, and the mask is bits 10-12.
mask_of() {
  awk 'function d(x, y) { return substr(row[y], 2*x + 1, 1) == "#" }
       { row[NR - 1] = $0 }
       END { b10 = d(4, 8); b11 = d(3, 8); b12 = d(2, 8)
             print (1 - b12) * 4 + b11 * 2 + (1 - b10) }' "$1"
}

{
  echo "# $QRVER -8 -m 0 -t ASCII; written by generate.sh"
  echo "# version level bytes mask sha256(ascii matrix)"
  li=0
  for l in L M Q H; do
    for v in $(seq 1 40); do
      lo=1; hi=3000 # the largest length that fits: fits(lo) holds, fits(hi) does not
      while [ $((hi - lo)) -gt 1 ]; do
        mid=$(((lo + hi) / 2))
        if fits "$v" "$l" "$li" "$mid"; then lo=$mid; else hi=$mid; fi
      done
      sweep_input "$v" "$li" "$lo" >sweep.tmp
      qrencode -8 -l "$l" -v "$v" --strict-version -m 0 -t ASCII -r sweep.tmp -o sweep.matrix.tmp
      printf '%d %s %d %d %s\n' "$v" "$l" "$lo" "$(mask_of sweep.matrix.tmp)" "$(shasum -a 256 <sweep.matrix.tmp | cut -d' ' -f1)"
    done
    li=$((li + 1))
  done
} >sweep.txt
rm -f sweep.tmp sweep.matrix.tmp
echo "generate.sh: wrote the vectors and sweep.txt with $QRVER"
