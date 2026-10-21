# LP-7 — share cards (OG images), titles, descriptions + the public states boards (builder B)

> **Copied into the repo 2026-10-21 and refreshed for LP-7 the same day** from the approved builder
> inventory (independent audit; LP-6: two rounds + the sec-S36 wording fix; LP-7: three rounds) as the
> claim-level source for [README.md](README.md) Parts II–III and the M14 cards. `file:line` refers to HEAD `6afba9f` (re-find by symbol).
> `lpgen/…`, `qa/…` and `bo/project/` are the generator and QA paths outside the repo; the boards themselves
> are in `docs/design/panel-redesign/boards/`.
> Claim ids carry their page as a prefix (`land-` landing, `prod-` /product, `price-` /pricing, `sec-`
> /security, `rest-` /for/restaurants, `fab-` /for/production, `api-` /docs/api), added on copy so ids are
> unambiguous. An id keeps the prefix of the page it was first drawn on: the LP-7 move put the `land-T`,
> `land-S` and `land-I` rows on `/product`, and they keep their `land-` ids (README §21).
> Test names that do not exist yet are written as descriptions ("new test: …"), never as identifiers
> (`TestEveryNamedTestExists`); the builder names them.

Generator: `lpgen/gen_c.py`. It imports `chrome.py` read-only. Boards in `bo/project/`, canvas entries in
`lpgen/canvas-C.json`:

| Board | w | h | title |
|---|---:|---:|---|
| ShareCards.dc.html | 2640 | 4953 | L08 · Share cards |
| States-Public.dc.html | 1440 | 3814 | L09 · States |
| M-States-Public.dc.html | 390 | 13103 | Mobile · States |

QA (`qa/run.py`): 0 findings on all three. Only palette tokens are used (scanned: no hex outside ink, porcelain,
paper, tappa-green, green-lite, saffron, saffron-lite, tomato, line, plus the `.lp` mint for the ink cards).

## Share cards: one per public page

Every card is 1200×630 and follows one layout: ring + "taptime" + PUNCHLESS lockup, an eyebrow, the page headline, a
"tap in · tap out · that's it" foot, and one motif on the right. There are no photos, no emoji and no prices: the
price is derived (LandingView) and a static image would go stale.

| Route | `<title>` | meta description | og:image | Card headline · motif | Source of the strings |
|---|---|---|---|---|---|
| `/` | Taptime — Punchless time & attendance | Stick one smart plaque on the wall. Your team taps their own phone on it and they're clocked in — verified, timestamped, on the right site. | `og/landing.png` | "Punchless. Every shift starts with a tap." · plaque + hanging APPROVED docket | title landing.templ:90 (exists); `LandingHeroLede` |
| `/product` | Product tour — Taptime | Your people touch a plaque with their own phone. Managers read the record, settle the doubtful ones and export the week. | `og/product.png` | "From one tap to the week's hours." · ticket rail with three dockets | builder A's /product H1 + lede (A's NEW CLAIMs; nothing new here) |
| `/pricing` | Pricing — Taptime | No hardware bill, no installation fee, no maintenance contract. Plaques included. | `og/pricing.png` | "Pay per person. Nothing per device — there are none." · receipt docket (People per month ✓ / Devices none / Plaques included ✓), no figure | `LandingPricingHeading` + `LandingPricingLede` |
| `/security` | Security & privacy — Taptime | A tap isn't trusted — it's verified. Each clock-in is scored against four independent signals before it lands in your records. | `og/security.png` | "Four proofs on every single tap." · chain Moment → Person → Place → Backup place (ink card) | `LandingSecurityLede`; the four Proof names |
| `/for/restaurants` | For restaurants & bars — Taptime | Late-night bars welcome: overnight shifts pair check-outs to the last open check-in, not the calendar date. | `og/restaurants.png` | "Restaurant & retail chains" · Rusty Bar 18:00–02:00 timeline, "8h 06m · one shift" | `LandingAudiences[0]` |
| `/for/production` | For production & facilities — Taptime | One plaque at the gate; departments resolve automatically from each employee's profile — bakers against their 04:00 shift, meat production against 05:00. | `og/production.png` | "Production & facilities" · five department shift bars (seed) | `LandingAudiences[1]` |
| `/signup` | Start free — Taptime | Start yourself today — first 3 months free. | `og/signup.png` | "Go punchless." · the three signup steps (green card) | title signup.templ:238 (exists); CTA line (the 3 is `FreeMonths`); step headings signup.templ:238/396/462 |
| `/legal/privacy`, `/terms`, `/imprint`, `/cookies` | `Page.Title` + " — Taptime" (exists, legal.templ:49) | each page's own `LegalPage.Lede` | `og/legal.png` (shared) | "Privacy · Terms · Company details · Cookies" · document sheet | `LegalPages` |
| `/docs/api`, `/docs/api/tr` | Taptime integration API — integration guide (exists, apiguideview.go:68) | Read every punch, write people and locations, and receive webhooks: the Taptime integration API, explained. | `og/api.png` (shared by both languages) | "Taptime integration API" · `/v1` docket: GET /v1/punches, PUT /v1/locations, POST your webhook, X-Taptime-Signature | guide H1; description **NEW CLAIM api-N1** |

New titles: the four new pages ("Product tour", "Pricing", "Security & privacy", "For restaurants & bars", "For
production & facilities", each + " — Taptime"). Each is the page's own nav or footer label, not a claim.

### NEW CLAIMS
- **api-N1** (/docs/api description): "Read every punch, write people and locations, and receive webhooks: the Taptime
  integration API, explained."
  - Facts: GET /v1/punches (internal/handler/api/punches.go:19, history.go:21); PUT /v1/locations and the staff
    write (CLAUDE.md §3 `tenant.PutVenueTx`, `staffapi.go`); webhooks (internal/webhook/delivery, signing.go:35
    `X-Taptime-Signature`).
  - Proposed Fact: `FactAPIGuideCoversPunchesWritesWebhooks`.
  - The API is "enabled by Taptime" (FAQ wording); a 160-character description has no room to repeat that, and the
    guide says it.
- No other new sentence. Every other description and headline is an existing value.
- The motif labels are drawing furniture with invented sample names and times: People / Devices / Plaques, Moment /
  Person / Place / Backup place, department names from the seed.

### Port notes
1. **Static files.** Export each card as a 1200×630 PNG into `web/static/og/<name>.png`: landing, product, pricing,
   security, restaurants, production, signup, legal, api. They are embedded like the rest of `web/static`. Add a size
   and sha256 note like the fonts README.
2. **Head tags** in `documentHead` (an optional OG block, given only by the marketing shells):
   - `og:type=website`, `og:title` = `<title>`, `og:description` = the description.
   - `og:url` = the canonical absolute URL.
   - `og:image` = an **absolute** URL built from `TAPPA_BASE_URL` (config.go:29) + `/static/og/<name>.png`, with
     `og:image:width=1200` and `og:image:height=630`.
   - `og:image:alt` = the card headline.
   - `twitter:card=summary_large_image`.
   - `<meta name="description">` = the same description.
3. **CSP is unaffected.** Crawlers fetch og:image; the page never renders it, so no `img-src` is added.
   `TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage` stays green.
4. **Derived numbers stay derived.** The signup description's "3" comes from `FreeMonths` at render time (as the CTA
   line does). The images carry no price and no free-month count.
5. **Pins.**
   - Each description is either an existing landingview.go value (reuse the variable; do not copy the string) or api-N1
     with its Fact.
   - Add a test that every public route renders exactly one `og:image` whose file exists in the embedded FS, and a
     `twitter:card`.
   - Signup stays `RobotsPrivate` (noindex). OG tags do not change that; links shared in chat still unfurl.
   - The landing's unbranded golden (`panelbrand_golden_test.go`) changes when the head gains these tags. Update it in
     the same change.
6. **The API guide's head** is `layout.MarketingTranslated`. Mind the byte-for-byte note on that shell (M12-46).

## States boards (L09 / Mobile · States)

The boards show every interactive public control in default / hover / focus-visible / pressed / disabled (where it
applies) / current or open:
- primary, ghost, header and CTA-band buttons;
- header nav links (current = aria-current), Sign in, footer links and the next-step link (`.more`);
- the FAQ `<details>` closed and open;
- the phone Menu summary closed and open, its links (current page) and a full open-Menu drawing on the phone board;
- the sticky phone "Start free" bar (`.mbar`, as A specified: not on Signup);
- the demo dialog's close button.

Decisions:
1. **One focus ring:** 2 px solid, 2 px offset. It is ink on porcelain and paper (14.32:1 and 16.17:1) and paper on
   green and ink (7.73:1 and 16.17:1). The offset keeps it on the surface, so the 3:1 always holds against what
   surrounds it (WCAG 2.2 focus appearance).
   - **This replaces the landing's `.lp :focus-visible { outline: 3px solid var(--saffron) }`** (input.css:1799):
     saffron measures 2.32:1 on porcelain and 2.62:1 on paper, both below 3:1.
   - In the port: `:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px }`, with `--ring` = ink, and
     paper inside green or ink bands.
2. **Pressed (`:active`)** is ink with a 2 px green inner edge (paper on the green band), moved down 1 px. It differs
   from hover (ink) without adding a colour.
3. **Disabled** exists only on form submit buttons (Signup). Ghost buttons and links have no disabled state, and their
   cells say n/a.
4. **Visited** links keep their default colour: navigation, not a reading list. This is stated on the board for Sign
   in, the footer and next-step links.
5. **Menu link current state:** `.mlink[aria-current]` gets green text (7.73:1 on paper) and a 3 px green inset bar.
   chrome.py has no current style for `.mlink` today. This is a proposal for builder A's chrome.
6. Phone board: hover is omitted, because touch has no hover. Focus-visible stays for keyboards and switch access.

## Also in this round (LP-7, gen_b.py)
- /security sec-S35 now carries only "Taptime's production server runs in Falkenstein, Germany." under "Hosted in
  Germany". The backup and EU-bucket half is gone. The inventory row in LP6-inventory-B.md is updated: user approved
  2026-10-21, with the port note to change the Privacy page's pending hosting fact in the same commit.
- The nine B boards were regenerated on A's current chrome (bands, sticky bar). QA: 0 findings. Heights after the latest run (it happened after `.b-done`) are in `lpgen/heights_b.json` and `lpgen/canvas-B.json`, e.g. Security 8275, M-Security 13700. `lpgen/.b-done` is created.

## Audit fix round (LP-7 audit, CHANGES REQUIRED)
- **States: rows added.** The landing's whole-card link (`.fitcard`); the /product "On this page" index (a docket entry
  on desktop, a chip on tablet and phone); the lockup home link; the `.flink` legal links; Legal's document switcher;
  and Signup's text input, business-type chip, place-count card, password field and submit "Create my account". All
  use the 2px / 2px-offset ring. The Menu link's current style is green text with an inset 3px green bar, as in A's
  chrome.
- **Real sample strings.** The next-step row reads "Pricing in detail". The FAQ row uses the real question, "What if
  someone doesn't have their phone that day?", wrapped.
- **Share cards.**
  - The production card names the seed departments with their prefix ("KM Pastry & Bakery" …).
  - Every motif text is now at least 18px, so it survives a ~500px unfurl, or is purely graphic: the legal sheet; the
    plaque's TAPTIME label was dropped.
  - The rail now has two dockets, so the stamps fit at 18px.
- **sec-S35 section heading** is now "Where Taptime runs.", and inventory B's row is updated.
- **QA, 0 findings** on: the nine B boards; Security, T-Security and M-Security with the Menu closed and open (the open
  state rendered from a temporary copy with menu=open, deleted afterwards; only the phone board has a Menu);
  ShareCards; and both States boards.
