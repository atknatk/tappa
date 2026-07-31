# Self-hosted brand typefaces

Space Grotesk (display) and IBM Plex Mono (every number) — skill `tappa-brand`.

**Why these files are in the repo.** CLAUDE.md §9 and the brand skill forbid a
runtime connection to Google Fonts or any other third party: a font request
carries the current page's URL to someone else, and the activation page's URL
carries an invite code. Self-hosting is also what makes the product work on a
venue's bad wifi and offline. Until M5-04 these files did not exist, `app.css`
had zero `@font-face` rules and every screen fell back to the system typeface —
the red line held (no external request) but the brand did not.

## Provenance

Fetched once, at build time, on 2026-07-31 and committed. Nothing at runtime
reaches these origins.

| File | Bytes | Source |
|---|---|---|
| `space-grotesk-v22-latin-wght.woff2` | 22320 | `https://fonts.gstatic.com/s/spacegrotesk/v22/V8mDoQDjQSkFtoMM3T6r8E7mPbF4C_k3HqU.woff2` |
| `space-grotesk-v22-latin-ext-wght.woff2` | 18924 | `https://fonts.gstatic.com/s/spacegrotesk/v22/V8mDoQDjQSkFtoMM3T6r8E7mPb94C_k3HqUtEw.woff2` |
| `ibm-plex-mono-v20-latin-400.woff2` | 10052 | `https://fonts.gstatic.com/s/ibmplexmono/v20/-F63fjptAgt5VM-kVkqdyU8n1i8q131nj-o.woff2` |
| `ibm-plex-mono-v20-latin-ext-400.woff2` | 8860 | `https://fonts.gstatic.com/s/ibmplexmono/v20/-F63fjptAgt5VM-kVkqdyU8n1iEq131nj-otFQ.woff2` |
| `ibm-plex-mono-v20-latin-700.woff2` | 10128 | `https://fonts.gstatic.com/s/ibmplexmono/v20/-F6qfjptAgt5VM-kVkqdyU8n3pQPwlBFgsAXHNk.woff2` |
| `ibm-plex-mono-v20-latin-ext-700.woff2` | 8748 | `https://fonts.gstatic.com/s/ibmplexmono/v20/-F6qfjptAgt5VM-kVkqdyU8n3pQPwl5FgsAXHNlYzg.woff2` |

The URLs came from the Google Fonts CSS API (`css2?family=…`), which is Google's
own build of the upstream OFL sources — the same origin the "self-host your web
fonts" practice pulls from. 92 KB in total, embedded in the binary
(`web/embed.go` embeds `all:static`) and served from `/static/fonts/`.

SHA-256, so a reviewer can confirm the bytes were not altered after download:

```
a0d054c4af557de20afd6ca59f47ab353bcaec49c63ff04b6c9d39d0f8910557  space-grotesk-v22-latin-wght.woff2
054c266fbb441ee059365dba0885d206f67ca05b375de869b88e02ebfccc9b9d  space-grotesk-v22-latin-ext-wght.woff2
c36f509c0a8f9f85f29cb44bc8701d8a9e0b14c499e77a884f789ead7093a7ac  ibm-plex-mono-v20-latin-400.woff2
f1050dc5317b43434c0aeda599d4624c774ffc162e87a8cf204b949b6a85816d  ibm-plex-mono-v20-latin-ext-400.woff2
9e1455e6e9c5866f607e464fffb7855486dcf575b7e69b83a3d234e587fce41b  ibm-plex-mono-v20-latin-700.woff2
099842414ddad0cb366cf984641b14e75c01a2c6e3f42fa13a6e89d1f4c3fc20  ibm-plex-mono-v20-latin-ext-700.woff2
7e6b2818edbd8f6a01ae80641cc8f16a51080d08fb4e532be3a0b6f74adb07da  OFL-IBMPlexMono.txt
564ce565c371c5e5bbf286006565a7c9aa55a9f56e7ca58d56e05d649dd61a72  OFL-SpaceGrotesk.txt
```

## Licence

Both families are **SIL Open Font License 1.1**, which permits redistribution
and bundling. The full texts are here, next to the fonts they cover, as the OFL
requires:

- `OFL-SpaceGrotesk.txt` — Copyright 2020 The Space Grotesk Project Authors
  (<https://github.com/floriankarsten/space-grotesk>)
- `OFL-IBMPlexMono.txt` — Copyright © 2017 IBM Corp. with Reserved Font Name
  "Plex"

Neither font has been renamed, subset by us or otherwise modified — they are the
files as served, so the OFL's Reserved Font Name clause is not engaged.

## Why two subsets per weight

The `latin` subset covers ASCII and Western European accents; `latin-ext` adds
the Maltese letters **ċ ġ ħ ż** (U+010B, U+0121, U+0127, U+017C). The UI text is
English, but employee and venue names are not — "Ċikku Żammit" must not render
in a fallback face on the one screen that greets him by name. Each face declares
its `unicode-range` in `web/static/css/input.css`, so a browser downloads
`latin-ext` only when a page actually contains one of those characters.

## Replacing or updating

Fetch the new file, put its URL, size and SHA-256 in the table above, and check
the licence text still matches. Do NOT point `@font-face` at a remote URL — that
is the red line these files exist to keep.
