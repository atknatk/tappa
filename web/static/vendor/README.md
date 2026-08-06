# web/static/vendor — third-party code, served from our own origin

Everything in this directory is **vendored** — written by somebody else — and is
served from **our own origin** and embedded in the
Go binary (`web/embed.go`, `//go:embed all:static`). **There is no CDN reference
anywhere in this product**, and that is a red line rather than a preference: a
runtime request to a third party sends the current page's URL, and on the
activation page that URL carries the invite code.

The same discipline the brand faces came in under (M5-04, `web/static/fonts/`):
downloaded **once, at build time**, committed, with the source URL, the size and a
sha256 recorded here.

## Files

> 🔴 **OUR OWN SCRIPTS ARE NOT HERE.** `web/static/js/` holds what we wrote
> (`tap.js`); this directory holds what we did not. The split is **load-bearing
> rather than tidy**: `tailwind.config.js` scans `web/static/js/**/*.js` as RAW
> TEXT, and dropping a minified library in there mined three rules nothing renders
> out of htmx's own strings (`.ease-in`, `.resize`, `.transition`). An earlier
> `!*.min.js` exclusion was defeated twice — by a subdirectory, and by a vendored
> file not named `.min`. So the DIRECTORY is the rule, and this one is named in no
> content glob. Guarded by `TestTailwind_ScansNoMinifiedSource`.
>
> Both directories are embedded and served (`web/embed.go` embeds `all:static`);
> this decides only what Tailwind READS.

| File | What it is | Version | Licence |
|---|---|---|---|
| `htmx.min.js` | vendored — HTMX, for the panel's pagination (M6-03) | **2.0.10** | 0BSD |

### htmx.min.js

| | |
|---|---|
| **Version** | `2.0.10` |
| **Source** | `https://unpkg.com/htmx.org@2.0.10/dist/htmx.min.js` |
| **Size** | 51 238 bytes |
| **sha256** | `71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de` |
| **Licence** | 0BSD (`https://github.com/bigskysoftware/htmx/blob/master/LICENSE`) |
| **Served at** | `/static/vendor/htmx.min.js` |
| **Loaded by** | `web/templates/pages/transactions.templ` — the transactions section, and no other page |

Verify:

```sh
shasum -a 256 web/static/vendor/htmx.min.js
# 71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de
```

#### Why it is here at all

CLAUDE.md §1 puts HTMX in the fixed stack, so this needed no new-dependency
approval — but M6-02 measured that the repo contained **no HTMX code at all**
(every match was prose) and deliberately did **not** vendor it, because the
skeleton had no fragment to swap: the tabs are plain `<a href>` links. M6-03 is
the task that brings the first real fragment (paging the docket list), so M6-03 is
the task that vendors it and that justifies the policy change.

#### What it cost the Content-Security-Policy

`internal/handler/adminlogin.go` now derives **two** policies from one base string.
The panel's default is unchanged; the transactions section adds exactly two
directives:

```
script-src 'self'    the file above, from our own origin. No CDN entry.
connect-src 'self'   htmx pages with XMLHttpRequest, and connect-src FALLS BACK
                     to default-src, which is 'none'. Without it the script loads
                     and every request it makes is blocked by the browser.
```

**No `'unsafe-inline'` and no `'unsafe-eval'`.** `hx-get` / `hx-target` /
`hx-swap` are attributes htmx's own code reads with `getAttribute`; the browser
never evaluates them, so they are not inline script.

#### The two dynamic-code paths in htmx, and why they are unreachable here

The minified file contains exactly one `new Function` and one `eval(`. Both are
gated behind syntaxes this product does not use:

| Path | Reached by | Used here? |
|---|---|---|
| `new Function("event", …)` | an `hx-on:*` / `hx-on…` attribute | **no** |
| `eval(…)` | the `js:` prefix on `hx-vals` / `hx-headers`, and bracketed `[…]` event filters | **no** |

This abstinence is **asserted, not assumed** —
`TestPanelMarkup_UsesNoHtmxSyntaxThatNeedsUnsafeEval` in
`internal/handler/transactions_test.go` scans the rendered panel for all three and
fails on any of them, so a future edit cannot quietly create the need for a wider
policy.

#### Upgrading

1. Download the new `dist/htmx.min.js` into this directory.
2. Update the version, size and sha256 in the table above.
3. Re-check the dynamic-code table: `grep -o "new Function\|eval(" htmx.min.js`.
4. Run `make test` — the CSP and htmx-syntax nets are in
   `internal/handler/transactions_test.go`.
