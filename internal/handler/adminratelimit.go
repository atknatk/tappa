package handler

import "time"

// The PANEL AUTH budgets — migration 00011's PHASE B OBLIGATION 3, first half.
//
// THEY ARE NOT THE ACTIVATION BUDGETS AND NOT THE TAP BUDGETS, and the reason is
// what each request COSTS. An activation request is a database round trip; a tap
// is a round trip plus a CMAC; an unauthenticated POST /admin/login is up to
// adminauth's candidate cap MULTIPLIED BY a cost-12 bcrypt, measured at ~380 ms
// each on this machine. That is the most expensive anonymous request this product
// serves, by two orders of magnitude, and it is the number these budgets are sized
// against rather than a feeling about how often people log in.
//
// THREE BUDGETS, AND THE SPLIT IS THE DESIGN — the shape ratelimit.go arrived at
// after an audit measured what a single per-IP bucket bought (60 anonymous
// requests and the next GENUINE activation got a 429):
//
//	flood    per address, charged on EVERY panel-auth request, HIGH ceiling. The
//	         DoS shield. It is the only budget that can refuse a request carrying
//	         correct credentials, and reaching it takes deliberate automation.
//	attempt  per address, charged ONLY BY FAILED logins, LOW ceiling. This is the
//	         one that bounds CPU: it caps how many times per window an address may
//	         make the server run the bcrypt loop and come away with nothing.
//	account  per ADMIN USER id, charged by failures that resolved to a real admin.
//	         It bounds audit_log — nothing else. It NEVER refuses a request.
//
// 🔴 WHY THE ACCOUNT BUDGET DOES NOT GATE, WHICH IS THE OPPOSITE OF WHAT
// ratelimit.go's invite budget DOES. An invitation is a credential: refusing
// further attempts against a burnt one costs its owner nothing they cannot fix by
// asking for a new link. An ADMIN ACCOUNT is not replaceable that way, and a
// per-account gate is a classic account-lockout: anyone who knows an owner's email
// address could spend that account's budget from anywhere and keep the owner out
// of their own panel, indefinitely, at no cost. Two readings, both measured
// against the same arithmetic:
//
//	GATE ON THE ACCOUNT       an attacker needs 11 requests per 10 minutes, from
//	                          any address, to keep one named owner locked out. The
//	                          attacker needs only the email address, which is
//	                          semi-public (it is on the invoice).
//	GATE ON THE ADDRESS ONLY  an attacker with N addresses gets N x 10 attempts per
//	                          window; a botnet defeats the CPU bound in proportion
//	                          to its size. Nobody is locked out of anything.
//
// THE ADDRESS IS CHOSEN. The lockout is a certain harm to a real customer from a
// trivially cheap attack; the address bound is a partial defence against an
// expensive one, and the thing it protects (CPU) degrades gracefully where a
// locked-out owner does not. THE RESIDUAL IS REAL AND IS NOT CLAIMED AWAY: a
// distributed attacker is bounded by nothing here except the candidate cap, and
// the honest mitigation for that is operational (a proxy-level limit, M8) rather
// than in-process. It is written down instead of being papered over with a lockout
// that would hurt the customer first.
//
// LIMITS INHERITED FROM THE PRIMITIVE (httpx.Limiter, stated where it lives):
// in-process, cleared by a restart, and a FIXED window — so a burst spanning a
// boundary can reach 2x the limit. For the attempt budget that means a worst case
// of 20 bcrypt loops rather than 10, i.e. ~61 s of CPU rather than ~30 s; the
// conclusion below is unchanged by the factor of two and is stated with it.
const (
	// adminFloodLimit / adminFloodPeriod: the per-address shield over the whole
	// panel surface.
	//
	// 🔴 IT WAS 200 AND THAT NUMBER WAS DERIVED FOR A SURFACE THAT NO LONGER
	// EXISTS. The old derivation counted LOGINS only ("200 carries 50 complete
	// logins; an office of ten arriving at 09:00 spends 40") because only the four
	// unauthenticated auth routes were metered. Round 12 put the shield in front of
	// the AUTHENTICATED routes too — correctly, they were paying unbudgeted
	// resolver reads — and that silently made the same 200 carry every panel page
	// load of every admin behind the address. An audit measured the consequence:
	// ONE operator's 100 ordinary page loads consumed HALF the window.
	//
	// RE-DERIVED TWICE. The first re-derivation guessed the dashboard's shape; the
	// second MEASURED it, once M6-02 had shipped one:
	//
	//	10 admins behind one NAT (the office case this file already names)
	//	  x  1 login each                          =    40-60 requests
	//	  +  ~20 page views each per 10 minutes
	//	     x 1 request per view   <- MEASURED, M6-02
	//	                                            =  200 requests
	//	                                              ---------------
	//	                                              ~260 per window
	//
	// THE FACTOR USED TO BE "~10 requests per view (HTMX fragments, M6-02's whole
	// design)" AND THAT PREMISE WAS FALSE. M6-02 shipped the dashboard with no HTMX
	// at all: the five sections are plain <a href> links, so a section is one
	// document and the browser makes one request for it. Measured over real HTTP
	// against real Postgres with a real session — 305 sequential GET /admin gave 300
	// x 200 and a 429 at exactly #301, which is one charged request per view and not
	// ten. The stylesheet is NOT in that number: /static/* is mounted outside
	// Protect(), and it still answered 200 with the session budget fully spent.
	//
	// ⚠️ THE MULTIPLIER COMES BACK WITH M6-03, which is the task that brings HTMX
	// and the first real fragment (docs/plan/m6-dashboard.md, M6-03's inherited
	// block). THE DEBT IS NOT CANCELLED, IT CHANGED HANDS: whoever adds fragments
	// counts them again, against the threshold recorded at adminSessionLimit.
	//
	// 3000 sits above that with room, and it is deliberately the SAME number as
	// httpx.tapAddressLimit: the tap surface solved this exact problem with a WIDE
	// address shield plus a per-session budget, and copying two thirds of that
	// pattern is what produced the regression above.
	//
	// ⚠️ WIDENING THIS DOES NOT WIDEN THE bcrypt BOUND, which is the number that
	// actually matters for CPU. That is adminAttemptLimit (10 FAILED logins per
	// window), and it is untouched. What this ceiling bounds is DATABASE work.
	//
	// 🔴 THE COST WAS MEASURED ON THE WRONG ARM AND WAS 2-4x TOO LOW. This comment
	// said "3000 x ~1.5 ms = ~4.5 s per window, about 0.75% of one core". The 1.5 ms
	// is the INVENTED-TOKEN arm: a resolver read that finds nothing and stops. The
	// ceiling was widened for AUTHENTICATED page loads, and every one of those pays
	// a resolver read PLUS a WithTenant transaction PLUS a TouchAdminSession UPDATE.
	// Measured against real Postgres, median of 200 per arm:
	//
	//	                          idle      4 busy cores   independent audit
	//	no cookie (no DB at all)  0.083 ms      0.172 ms          0.142 ms
	//	invented token (read)     0.650 ms      0.875 ms          1.206 ms
	//	LIVE session (read+UPDATE) 3.040 ms     4.161 ms          5.717 ms
	//
	// So the honest arithmetic, on the arm the ceiling exists for:
	//
	//	3000 x 3.0-5.7 ms = 9-17 s of database time per window per address
	//	                  = 1.5-2.9% of one core
	//
	// ⚠️ AND THE REFUSED REQUESTS ARE IN THAT NUMBER. sessionGate runs AFTER the
	// resolver (see adminSessionLimit), so a request refused by the session budget
	// has ALREADY paid the read and the UPDATE. Measured: 3000 requests on one live
	// session produced 3000 Verify calls and 2700 x 429 — the 429s cost the same as
	// the 300 that were served.
	//
	// 3000 STILL STANDS, and the re-derivation is why rather than inertia: the
	// legitimate requirement below is ~2000, so the ceiling cannot go under it, and
	// 2.9% of one core per source address is a cost this deployment can pay. What
	// changed is the sentence, not the number.
	//
	// ✅ THE RE-DERIVATION M6-02 OWED IS PAID, AND THE NUMBER SURVIVED IT.
	//
	// ⚠️ BOTH HEADROOM FIGURES ARE OVER THE SAME DENOMINATOR, WRITTEN OUT, because
	// the first version of this paragraph used two different ones and produced a
	// third wrong number: it divided by the page requests ALONE for the new figure
	// (3000/200 = 15x) and by page requests PLUS logins for the old one
	// (3000/2060 = 1.46x), so the two were not comparable and the improvement read
	// 30% larger than it is.
	//
	//	OLD PREMISE  200 views x 10 fragments = 2000, + 40-60 logins = ~2060
	//	             3000 / 2060 = 1.46x
	//	MEASURED     200 views x  1 request   =  200, + 40-60 logins =  ~260
	//	             3000 /  260 = 11.5x      <- the comparable figure
	//
	// It is NOT lowered: the ceiling exists for an anonymous flood rather than for
	// the office, and the cost arithmetic above (9-17 s of database time per window
	// per address) is what sizes it. What was wrong was the premise, and the premise
	// is now a measurement.
	adminFloodLimit  = 3000
	adminFloodPeriod = 10 * time.Minute

	// adminSessionLimit / adminSessionPeriod: the THIRD stage, keyed on the panel
	// SESSION id rather than the address.
	//
	// 🔴 ITS ABSENCE WAS THE OTHER HALF OF THE REGRESSION. tap.go mounts
	// ByAddress -> Identify -> BySession, and internal/httpx/ratelimit.go explains
	// why the third stage exists: without it a resolved caller is starved by the
	// ANONYMOUS budget, so legitimate work and an anonymous flood share one bucket
	// — the exact shape ratelimit.go records having REMOVED from the activation
	// flow. The panel had stages one and two only.
	//
	// 🔴 WHAT IT DOES AND DOES NOT BOUND — THE EARLIER SENTENCE OVERCLAIMED. It said
	// this "bounds what a SINGLE session can do (a stolen cookie being replayed)".
	// Measured, that is false: the gate runs AFTER requireAdmin, so a refused
	// request has already paid the resolver read and the TouchAdminSession UPDATE.
	// A stolen cookie replayed 3000 times produced 3000 Verify calls and only 300
	// served responses — the DATABASE cost of that cookie is bounded by the ADDRESS
	// ceiling (3000), not by this one. A refused request even leaves a trace:
	// last_used_at changes on a request that answers 429.
	//
	// MOVING IT EARLIER IS NOT AVAILABLE, and internal/adminauth/manager.go already
	// says why: a per-session budget cannot be keyed before the session is resolved,
	// and resolving is the cost. tap.go has no such problem because httpx.Identify
	// deliberately does NOT write; the panel's authority for "is this admin still
	// active" is the UPDATE itself.
	//
	// SO THE HONEST DIVISION OF LABOUR IS:
	//	floodGate   (3000 / address)  bounds the resolver read + the UPDATE
	//	sessionGate ( 300 / session)  bounds the work that happens AFTER
	//	                              authentication — which today is a placeholder
	//	                              page, and which M6-02 fills with the dashboard.
	//
	// That is a defensible design; the indefensible thing was the sentence. The key
	// is the session's own UUID, so this budget cannot be spent by anybody else.
	//
	// THE KEY IS THE SESSION UUID, NEVER THE TOKEN OR ITS HASH. ratelimit.go states
	// the rule for the invite budget and it holds here: an in-memory map key ends
	// up in heap dumps, and a token hash is a bearer credential (section 4.7).
	//
	// 🔴 300 WAS COPIED FROM httpx.tapSessionLimit, NOT DERIVED, and it was written
	// down as a limit rather than quietly fixed. ✅ M6-02 DERIVED IT, AND THE NUMBER
	// SURVIVED — but the headroom this comment claimed was wrong in the SAFE
	// direction, which is worth being exact about because the old figure was the
	// reason this was called the tighter of the two ceilings.
	//
	//	CLAIMED  ~200 requests per admin per window (20 views x 10 fragments),
	//	         so 300 leaves 1.5x.
	//	MEASURED  1 request per view (the sections are plain links, no HTMX), so an
	//	         admin's 20 views cost 20 requests and 300 leaves 15x. A session's
	//	         budget is 300 SECTION VIEWS per 10 minutes.
	//
	// And tap's 300 was derived for a different shape entirely
	// (httpx/ratelimit.go: "an employee taps a few times a day … a refresh every
	// 5 s = 120 requests, 2.5x headroom") — that part of the objection stands.
	//
	// ⚠️ THE THRESHOLD, so the next person does not have to re-measure to know
	// whether they are near it: at 20 views per window, 300 becomes binding at
	// ≥15 requests per view.
	//
	// ✅ M6-03 BROUGHT HTMX AND COUNTED THEM. THE MULTIPLIER DID NOT COME BACK.
	//
	// Measured the same way M6-02 measured it — real server, real Postgres, real
	// session, sequential GET /admin until refusal:
	//
	//	300 served, first 429 at request #301
	//	=> CHARGED REQUESTS PER SECTION VIEW = 1.000, unchanged
	//
	// 🔴 THE DENOMINATOR MATTERS HERE AND IS WRITTEN OUT, because this is exactly
	// where the old premise went wrong. "Requests per VIEW" is per DOCUMENT LOAD,
	// and it is still one: opening the section costs one request, and changing a
	// filter costs one more because the filter bar is a plain GET form rather than
	// a fragment swap. What M6-03 adds is a SECOND, different denominator:
	//
	//	per SECTION VIEW  (one document)             1 charged request
	//	per FILTER CHANGE (one document)             1 charged request
	//	per DAY-WALK      (document + every "show     ceil(N / PageSize) charged,
	//	                   more" to the end of it)    where N is the filtered day
	//
	// The fragment IS charged: with the session budget spent, GET /admin/dockets
	// answers 429 (measured). The static assets are NOT — /static/* is registered
	// on the root router BEFORE any feature mounts (internal/httpx/router.go), so
	// htmx.min.js and app.css never enter Protect(). ⚠️ That is a STRUCTURAL fact
	// plus M6-02's measurement, NOT something re-measured here: the panel test
	// harness mounts AdminAuth on a bare router, so /static 404s in it, and a 404
	// means "not mounted in that harness" rather than "not charged".
	//
	// WHAT ceil(N / PageSize) IS IN PRACTICE, against the real distribution in the
	// development database (40 850 tenant-days):
	//
	//	median day        1 record    ->  1 request
	//	95th percentile  20 records   ->  1 request
	//	99th percentile  30 records   ->  2 requests
	//	only 3 tenant-days exceed 300 records, and all three are days this
	//	repository's own test suite wrote into the dev database, not businesses.
	//
	// So at the 99th percentile of real data a complete day-walk costs TWO charged
	// requests, and 300 remains ~150x the load rather than 15x. The ≥15 threshold
	// is reached only by a single unfiltered view holding ≥350 records — THIRTEEN
	// deliberate presses of "show more" — which is what the six filters exist to
	// make unnecessary. (ceil(350/25) = 14 REQUESTS: one document plus thirteen
	// fragments. The press count is one less than the request count, and an earlier
	// version of this line printed the request count under the press label.)
	//
	// ⚠️ WHAT DID CHANGE, AND IT IS NOT NOTHING: before M6-03 a session had no way
	// to spend more than one request per view at all, so the ceiling was
	// unreachable by any sequence of user actions. It is now reachable, by paging.
	// The number is not lowered and not raised here (this task does not own that
	// decision); it is counted, and the shape of the new cost is written down.
	// The lever, if it is ever wanted, is ledger.PageSize rather than this constant:
	// doubling it halves every figure in the table above.
	//
	// ✅ M6-05 PHASE A ADDED A SECTION AND COUNTED IT. THE PER-VIEW FIGURE DID NOT
	// MOVE; A THIRD DENOMINATOR APPEARED.
	//
	// The employees section loads NO SCRIPT, so it has no fragment route and paging
	// is a whole-document link. Per VIEW and per FILTER CHANGE it is therefore the
	// same single charged request as every other section — the multiplier M6-02
	// feared still has not come back. What is new is the shape of a WALK:
	//
	//	per SECTION VIEW   (one document)                   1 charged request
	//	per FILTER CHANGE  (one document)                   1 charged request
	//	per ROSTER-WALK    (document + every "Next page")   ceil(E / RosterPageSize)
	//	                                                    where E is the tenant's
	//	                                                    FILTERED employee count
	//
	// 🔴 AND THIS DENOMINATOR IS THE ONE THAT CROSSED THE CEILING, because E is the
	// PAYROLL rather than a day's traffic — the one quantity in this product that
	// grows without limit. It is also the reason ledger.RosterPageSize is 50 rather
	// than PageSize's 25 (user decision, 2026-08-07).
	//
	// 🔴 THIS IS THE ONE PLACE THE ROSTER-WALK FIGURES LIVE. Three files carried three
	// different values for the same quantity within a day (349 / 353 / 356), because
	// it drifts — see the warning below. Everything else points here.
	//
	// MEASURED OVER THE DEVELOPMENT DATABASE. Command, so the next person re-runs it
	// rather than trusting it:
	//
	//	WITH t AS (SELECT tenant_id, count(*) n FROM employees GROUP BY 1),
	//	     w AS (SELECT n, ceil(n/25.0)::int p25, ceil(n/50.0)::int p50 FROM t)
	//	SELECT (SELECT count(*) FROM tenants), count(*), max(n), max(p25), max(p50),
	//	       percentile_disc(0.5)  WITHIN GROUP (ORDER BY p50),
	//	       percentile_disc(0.95) WITHIN GROUP (ORDER BY p50),
	//	       percentile_disc(0.99) WITHIN GROUP (ORDER BY p50),
	//	       count(*) FILTER (WHERE p50 >= 15),
	//	       count(*) FILTER (WHERE p50 > 300),
	//	       count(*) FILTER (WHERE p25 > 300) FROM w;
	//
	//	                                        at 25 (was)   at 50 (SHIPPED)
	//	median tenant                              1 req          1 req
	//	95th percentile                            1              1
	//	99th percentile                            1              1
	//	tenants at or over the ≥15 threshold        1              1
	//	tenants OVER the 300 budget                 1              0   <- the decision
	//
	// 🔴 THE LARGEST ROSTER'S HEADCOUNT IS NOT A ROW IN THAT TABLE, ON PURPOSE. It is
	// the one quantity here that is a clock rather than a measurement (see the drift
	// warning below), and every attempt to keep it fresh in prose failed within the
	// round that made it. What the last two rows say is the part that DECIDED the
	// page size and the part that does not move: this database contains exactly one
	// tenant whose roster needs more than 300 page-turns at 25 and none that does at
	// 50 — which is another way of saying its largest roster sits between
	// PageSize x adminSessionLimit and RosterPageSize x adminSessionLimit. Run the
	// query above if you want today's digits.
	//
	// ⚠️ THE DENOMINATOR IS NOT "EVERY TENANT" AND THIS LINE USED TO SAY IT WAS. The
	// query GROUPs BY tenant_id over `employees`, so the population is tenants that
	// have AT LEAST ONE employee — roughly two thirds of the rows in `tenants`, and
	// BOTH of those counts drift for the same reason the roster does, so neither is
	// written here. The percentiles are therefore CONDITIONAL on having staff, which
	// is the right population for a question about walking a roster and the wrong
	// label for it. A tenant with nobody on the books needs zero page-turns and would
	// only have made the median look better.
	//
	// 🔴 THE FIRST VERSION OF THIS CORRECTION QUOTED BOTH FIGURES, in the file whose
	// own heading says this is the one place roster-walk numbers live and whose next
	// paragraph warns they drift. They were stale by the following audit. The rule
	// this file now follows applies to every population count on the page, not only
	// to the headcount: run the query.
	//
	// Either way the figure is dominated by this repository's own test residue, so
	// the TAIL is the finding and the shape is not. At 25 the largest tenant's
	// manager was refused at request #301, roughly six sevenths of the way down their
	// own roster, with the remainder unreachable by browsing. At 50 nobody in this
	// database is out of reach.
	//
	// ⚠️ THE LARGEST ROSTER IS NOT A STABLE NUMBER AND MUST NOT BE TREATED AS ONE.
	// The simulated-day fixture hires into that tenant on every `make test` run
	// (seedflow_db_test.go's insertEmployee, against fixtures.TenantKF) and employees
	// has no DELETE grant, so it climbs by roughly ten a run — it moved SIX times
	// while M6-05 was being reviewed, twice inside one audit. This is the M6-04
	// lesson about anchoring a number to a magnitude the suite itself pollutes, and
	// it cost three consecutive blocking findings before the response stopped being
	// "refresh the number" and became "stop writing it":
	// TestComments_DoNotQuoteTheDriftingRosterSize now fails on the shapes it was
	// written in. Nothing in the product is derived from it — rosterDesignCeiling is
	// a round promise chosen ABOVE the drift, precisely so the invariant does not
	// move when the fixture does.
	//
	// THE INVARIANT IS ASSERTED RATHER THAN REMEMBERED. RosterPageSize x
	// adminSessionLimit is the largest roster one session can walk end to end —
	// 15 000 today — and TestRosterPageSize_KeepsAWholeRosterInsideTheSessionBudget
	// requires it to reach rosterDesignCeiling (10 000). It is MEASURED going red at
	// 25; before it existed, moving the constant back broke no test at all.
	//
	// The walk is still not the intended path: the section ships a NAME BOX beside
	// the pager, which turns "find Maria" into one request whatever the payroll size.
	// What the page size buys is that browsing to the END stays possible, which is
	// §4.6's shape — a person nobody can reach is a person nobody can audit.
	//
	// ✅ M6-05 PHASE B ADDED THREE WRITE ROUTES AND COUNTED THEM. THE PER-VIEW FIGURE
	// STILL DID NOT MOVE, AND A FOURTH DENOMINATOR APPEARED: PER ACTION.
	//
	// Measured the same way — real router, real budgets, one session, the flow
	// repeated until the first 429, then adminSessionLimit / repetitions:
	//
	//	section view                                 300 flows  ->  1.00 charged
	//	open one person's action card                300 flows  ->  1.00 charged
	//	invite / re-invite (POST renders the link)   300 flows  ->  1.00 charged
	//	move (POST + follow the 303)                 150 flows  ->  2.00 charged
	//	deactivate (confirm link + POST + follow)    100 flows  ->  3.00 charged
	//
	// 🔴 WHY THE INVITE IS ONE AND THE OTHERS ARE NOT. It is the panel's only write
	// that RENDERS its response instead of redirecting, because the activation link
	// must not travel in a Location header (§4.7, internal/handler/employeeactions.go).
	// So the cheapest route is cheap for a security reason rather than a performance
	// one, and the deactivation is the dearest because it asks first — a confirmation
	// step this product cannot skip, since nothing can undo it (docs/adr/0010).
	//
	// AGAINST THE ≥15 THRESHOLD recorded above: the worst flow is 3, which is a fifth
	// of it. A manager who deactivated one person every ten minutes would use 3 of
	// 300. The budget is still spent by BROWSING (the roster-walk row above), not by
	// acting: even 100 consecutive deactivations fit inside one window, and a business
	// that deactivates a hundred people in ten minutes has a different problem.
	//
	// ⚠️ WHAT THIS DOES NOT COUNT: the WORK behind each request. A deactivation is a
	// read plus an UPDATE plus an audit INSERT in one transaction; a move is the same
	// shape; an invitation is an INSERT plus an audit INSERT in two. They are all
	// single-row writes on indexed keys, which is why they are not in the millisecond
	// table below — but "3 charged requests" is a statement about the BUDGET and not
	// about the database, and the two axes are kept apart deliberately here.
	//
	// 🔴 THE SECOND AXIS: WHAT ONE REQUEST COSTS, WHICH THIS FILE NEVER COUNTED.
	// Everything above counts REQUESTS. An audit named the gap: a ceiling on how
	// MANY requests a session may make says nothing about how BIG each one is, and
	// the panel had a control whose size grew with the customer. Measured on a real
	// tenant, same day, before and after (user decision, 2026-08-06):
	//
	//	                              BEFORE      AFTER
	//	full page                    867 233 B   32 066 B   -96.3% (27.0x)
	//	  of which, all <select>     836 771 B    1 452 B
	//	  of which, the employee one 835 319 B        0 B   (it is an <input> now)
	//	  docket cards on the page          25         25   (payload unchanged)
	//
	// ⚠️ THE DENOMINATORS ARE DIFFERENT AND ARE LABELLED. "Page bytes" is the whole
	// document; "filter bar bytes" is the <select> elements inside it. Confusing the
	// two is how the first version of this paragraph would have claimed a 96%
	// saving on something that was never 96% of anything.
	//
	// The roster was the only unbounded one: venues and departments are bounded by
	// how many places a business has, a payroll is not. At 111.5 B per <option>
	// the old shape crossed 100 KB at ~645 employees and 250 KB at ~2 022, on a page
	// that is Cache-Control: no-store and therefore re-sent on EVERY view and EVERY
	// filter change. The new shape is flat in the size of the business.
	//
	// 🔴 THE OTHER HALF OF THE SECOND AXIS: DATABASE TIME. The paragraph above
	// answers "how big is a request" in BYTES and stops there, which an audit named
	// as leaving the axis this block itself opened half-answered. The ceilings above
	// are sized on 3.0-5.7 ms per request -- and that figure is the RESOLVER READ
	// PLUS TouchAdminSession, measured before this panel had a query behind it.
	// M6-03 put one there and did not add it to the arithmetic.
	//
	// MEASURED (EXPLAIN ANALYZE; tenant with 7 769 employees, warm cache, 3 repeats,
	// after ANALYZE -- the development database had never been analysed, which is
	// what made an earlier round's timings unreproducible):
	//
	//	                          ordinary day (1 674)   largest day (13 458)
	//	no name filter               0.6 - 0.8 ms           0.6 - 0.9 ms
	//	name filter, many matches    9.7 - 17.2 ms         20.3 - 37.5 ms
	//	name filter, few matches     8.8 - 14.8 ms          9.3 - 11.8 ms
	//	name filter, no matches      8.5 - 14.6 ms         12.7 - 20.2 ms
	//
	// ⚠️ THE DENOMINATOR: those are per PAGE REQUEST -- one document or one paging
	// fragment -- not per session and not per view-plus-paging. An independent audit
	// measured up to 108 ms on the same shape, so treat the upper end as variable
	// with the pattern and the day rather than as a ceiling.
	//
	// SO THE HONEST PER-REQUEST COST, adding the auth cost this file already knew:
	//
	//	unfiltered page   0.6-0.9 ms  +  3.0-5.7 ms  =   ~4 - 7 ms
	//	filtered page     8.5-37.5 ms +  3.0-5.7 ms  =  ~12 - 43 ms
	//
	// AND WHAT THAT DOES TO adminSessionLimit: 300 filtered requests is roughly
	// 3.6-12.9 s of database time per window per SESSION, against the ~1 s the old
	// 3.0-5.7 ms figure implied. The address ceiling is 3000, so an AUTHENTICATED
	// caller holding several sessions can multiply it, on the connection pool the
	// tap surface shares.
	//
	// IT IS NOT LOWERED HERE, and the reasons are the ones this file already gives
	// for not lowering anything on measurement alone: reaching it needs a valid
	// panel session (not an anonymous flood), the oversized roster driving the slow
	// arm is this repository's test residue rather than a customer, and the
	// unfiltered page -- what a manager actually opens -- costs 0.6-0.9 ms. What was
	// missing was the NUMBER, and the number is now here with its denominator.
	//
	// ✅ M6-05 ADDED A THIRD ROW TO THIS AXIS, AND IT IS THE MOST EXPENSIVE ONE.
	// The paragraphs above are the TRANSACTIONS section. The roster is a different
	// query with a different scaling law, and an audit caught this block still
	// describing only the first. Measured through tappa_app inside a tenant-scoped
	// transaction, rows actually returned (not count(*)), warm, 6 repeats:
	//
	//	roster page (LIMIT RosterPageSize+1)   19 - 68 ms
	//	  of which the anchor lookup            0.44 - 0.51 ms   (paged requests only)
	//
	// 🔴 IT SCALES WITH THE ROSTER, NOT WITH THE PAGE, and that is the finding rather
	// than the milliseconds. EXPLAIN shows the LATERAL costing the planner its top-N
	// heapsort: the whole tenant's employee set is sorted with a quicksort on every
	// request, so asking for 50 rows out of a large payroll costs what sorting that
	// payroll costs. db/queries/employees.sql carries the plan detail.
	//
	// WHY IT IS STILL ACCEPTED: 300 x ~35 ms is roughly 10.5 s of database time per
	// window, which sits INSIDE the 3.6-12.9 s band this file already accepted for
	// the filtered transactions page -- and the session budget is keyed on the
	// session UUID, so a third party cannot spend it. The anchor lookup adds about
	// 2% to a paged request and nothing at all to a section view or a filter change,
	// which is the price of taking an unbounded name out of the URL (§4.6 and the
	// Referrer-Policy note in adminlogin.go).
	//
	// ⚠️ THE FIRST OF THOSE TWO FIGURES WAS WRITTEN BEFORE IT WAS MEASURED, in the
	// SQL comment, as "0.6 - 1.9 ms". The measurement was 0.44-0.51, and the
	// difference came from timing a lookup whose id was found by a subquery -- work
	// the product does not do. Recorded because it is the same class as the roster
	// count above: a number that arrived before its measurement.
	//
	// ⚠️ sessionGate runs BEFORE the handler, so a refused request does NOT run this
	// query; the 429s cost the resolver read, not the page.
	//
	// ✅ M6-04 ROUND 6 ADDED ONE MORE READ AND IT IS COUNTED HERE RATHER THAN LEFT AS
	// "one indexed lookup". The decision confirmation is verified against the database
	// instead of believed from the query string (internal/handler's
	// confirmedDecision), so the GET that follows a decision — the second of the two
	// requests a decision costs — now also runs GetTransactionReview.
	//
	// MEASURED (EXPLAIN ANALYZE, seed tenant, warm, 3 repeats):
	//
	//	0.367 / 0.081 / 0.095 ms   an audit's reading
	//	0.427 / 0.190 / 0.106 ms   reproduced here (the id resolved by a subselect,
	//	                           which is why the numbers sit slightly higher)
	//
	// Both runs chose the same plan: Index Scan using transaction_reviews_tenant_idx.
	//
	// Against the ~4-27 ms a panel request already costs that is roughly 0.4% at the
	// low end and invisible at the high end, and it is paid ONLY on a GET carrying
	// ?done — i.e. once per decision, never on an ordinary section view. The ceilings
	// above are unchanged and the arithmetic for "per DECISION = 2 served requests"
	// is unchanged; what changed is that the second of those two requests is no longer
	// free of database work, and now says so.
	//
	// ⚠️ THE ABSOLUTE NUMBERS OVERSTATE A REAL CUSTOMER, and the evidence for that
	// sentence had to be re-measured because the first version of it was WRONG.
	//
	// 🔴 IT SAID "another tenant in the same database has 76 618" AND NO TENANT HAS
	// ANYTHING LIKE THAT. The figure came from a query grouped by tenant NAME, and
	// "Kebab Factory Ltd" is a name shared by 44 752 different tenants this
	// repository's own tests created -- so the count summed a roster across all of
	// them and was then written down as one business's payroll. Same label mistake
	// as everywhere else in this session: measured over one denominator, reported
	// under another.
	//
	// MEASURED CORRECTLY (GROUP BY tenant_id):
	//
	//	largest single tenant      7 669 employees   (the seed tenant)
	//	next largest                  31 employees
	//	all tenants together     111 167 employees across 64 838 tenants
	//
	// The caveat survives and is STRONGER: the page figures above were taken on the
	// seed tenant, whose 7 669 employees are almost entirely test residue, and the
	// second-largest business in this database has 31 people. The SHAPE is the
	// finding, not the magnitude.
	//
	// CONSEQUENCE, STATED PRECISELY: a LEGITIMATE admin having a heavy day meets 429
	// on their own panel at request 301. It is not a lockout — signing out still
	// works (TestAdminLogout_CannotBeBlockedByAThirdParty, measured with both
	// buckets burnt) — but the panel is dead for the rest of that window.
	//
	// ✅ PAID BY M6-02 (see the arithmetic above). The consequence sentence directly
	// above still holds — a session that spends 300 requests meets 429 on its own
	// panel — but reaching it now takes 300 section views in ten minutes rather than
	// the 30 the old estimate implied.
	//
	// ✅ M6-04 RE-COUNTED IT AND THE MULTIPLIER STILL DID NOT COME BACK, but it added
	// the panel's FIRST denominator that is not one request:
	//
	//	per SECTION VIEW  (one document)               1 served request
	//	per FILTER CHANGE (one document)               1 served request
	//	per DAY-WALK      (document + "show more"s)    ceil(N / PageSize)
	//	per DECISION      (POST + the 303's GET)       2 served requests   <- NEW
	//
	// 🔴 THE DECISION FIGURE IS MEASURED, NOT ARGUED:
	// TestReviewBudget_ADecisionCostsTwoChargedRequests drives the real router with
	// the real budgets until it is refused, and prints
	//
	//	300 served + 1 refused; 150 complete decisions = 2.000 SERVED per decision
	//
	// ⚠️ ITS FIRST VERSION PRINTED 2.007, which was 301/150 — total requests over
	// completed decisions, i.e. the refused one counted in the numerator and not in
	// the denominator. The figure is taken over SERVED requests now and the two
	// counters are separate in the test.
	//
	// SO THE ≥15-PER-VIEW THRESHOLD ABOVE IS STILL NOT REACHED, and the honest way to
	// state the new cost is: clearing a queue of Q records costs 1 + 2Q served
	// requests, so 300 buys ~149 decisions in a ten-minute window. A manager working
	// through a genuinely large backlog is the first user of this panel who can meet
	// the ceiling by doing ORDINARY WORK rather than by paging. If that is ever
	// reported the levers are this constant, or making the redirect unnecessary — the
	// 303 is deliberate (a rendered POST re-submits on refresh) and is not traded for
	// a request here. Counted, not lowered and not raised.
	//
	// 🔴 THE OTHER AXIS — DATABASE TIME — GREW FOR EVERY PANEL REQUEST, INCLUDING THE
	// FOUR SECTIONS THAT PREVIOUSLY MADE NO QUERY AT ALL. M6-04 puts the approval
	// queue's count in the NAVIGATION, so it is read on whichever section is opened.
	// Measured (EXPLAIN ANALYZE, seed tenant of 21 419 records with 4 634 flagged,
	// warm cache, after ANALYZE, 3 repeats):
	//
	//	queue NOT empty (the cap fills early)    0.7 /  1.0 /  7.1 ms
	//	queue EMPTY     (the cap never fills)   17.8 / 20.3 ms
	//	an exact count(*) instead of a cap      59.4 / 63.6 / 66.4 ms   (rejected)
	//
	// ⚠️ THE EMPTY CASE IS THE DEARER ONE AND IT IS ALSO THE STEADY STATE of a panel
	// somebody keeps clear, so the figure to plan with is the second row. On top of
	// this file's existing per-request cost:
	//
	//	unfiltered page   0.6-0.9 + 3.0-5.7 + 0.7-20.3 ms  =  ~4 - 27 ms
	//
	// — up to roughly four times what a panel request cost before this task, in the
	// steady state. 300 of them is ~1.3-8.1 s of database time per window per
	// session, inside the range this file already accepts for filtered pages
	// (3.6-12.9 s), and reaching it still needs a valid panel session rather than an
	// anonymous flood. It is written down rather than optimised because the obvious
	// lever — showing the badge only on the review section — trades away the reason
	// the badge exists, which is a manager seeing the backlog WITHOUT going to look
	// for it. That is a product decision rather than this task's.
	adminSessionLimit  = 300
	adminSessionPeriod = 10 * time.Minute

	// adminLogoutLimit / adminLogoutPeriod: sign-out's OWN, deliberately very wide
	// address ceiling. See the sign-out group in adminlogin.go for why it is
	// separate from adminFloodLimit and what invariant it trades.
	//
	// 🔴 ITS COST WAS NEVER COMPUTED — THE 12TH LIMIT. The flood ceiling above shows
	// its arithmetic; this one showed none, and it is the WIDEST ceiling in the
	// product. Derived from this file's own measurements of the arm sign-out
	// actually drives (an invented token: shape gate passes, resolver reads, no row):
	//
	//	30000 x 0.65-1.21 ms = 19.5-36.3 s of database time per window per address
	//	                     = 3.3-6.0% of one core
	//
	// That is roughly TWICE what the flood ceiling buys (9-17 s, 1.5-2.9%), on the
	// same connection pool the tap surface depends on — so the product's most
	// expensive per-address allowance is the one route nobody costed.
	//
	// IT IS NOT LOWERED HERE, and the reason is the trade it was chosen for: the
	// invariant is that a third party must not be able to refuse somebody's
	// sign-out, and every request taken off this ceiling moves that threshold closer
	// to reach. 6% of one core per source address is payable on a single VPS; a
	// customer who cannot end a session is not. What was missing was the NUMBER, not
	// the decision — and the number belongs to whoever revisits either.
	// ⚠️ M6-02 INHERITED THIS WITH THE OTHER TWO AND DID NOT SPEND IT. Sign-out is
	// one POST at the end of a session, so the dashboard's shape does not touch this
	// ceiling the way it touches the other two — the cost above is unchanged and the
	// number is still nobody's measurement but this comment's arithmetic.
	adminLogoutLimit  = 10 * adminFloodLimit
	adminLogoutPeriod = 10 * time.Minute

	// adminAttemptLimit / adminAttemptPeriod: 10 FAILED logins in 10 minutes from
	// one address. This is the CPU bound, and the arithmetic is the argument:
	//
	//	10 failures x 8 candidates (adminauth's cap) x ~380 ms = ~30 s of CPU
	//	per 10-minute window per address = ~5% of one core, sustained.
	//	Worst case across a fixed-window boundary: 20 failures, ~61 s, ~10%.
	//
	// 🔴 THE "ORDINARY FIGURE" SENTENCE IS RETIRED (M7-02 round 4). It read: "one
	// candidate per email today, so ten failures cost ~3.8 s". adminauth now pads every
	// login to MaxCandidates comparisons (padComparisons) to close a timing channel
	// that answered "is this address registered" for the price of one sign-up, so the
	// ORDINARY figure IS the arithmetic above — the discount an unregistered address
	// used to get is exactly what was leaking.
	//
	// THE CEILING IS UNCHANGED AND WAS ALREADY WRITTEN FOR THIS. That is what made the
	// trade affordable: this budget was sized on "10 x 8 candidates", not on "10 x 1".
	//
	// ONLY FAILURES ARE CHARGED, and that is what keeps a shared office address
	// safe: ten people signing in correctly spend nothing here, so the budget
	// cannot refuse a correct password on account of somebody else's typing. Ten
	// WRONG passwords from one address in ten minutes is already well past what a
	// person does — three is the usual human maximum before they use the reset
	// link (which M7-04 will provide; today they ask another owner).
	adminAttemptLimit  = 10
	adminAttemptPeriod = 10 * time.Minute

	// adminLoginWorkLimit / adminLoginWorkPeriod: every login that REACHES the password
	// loop, whatever its outcome.
	//
	// 🔴🔴 IT EXISTS BECAUSE THE BUDGET ABOVE MEASURES ONLY HALF THE WORK, AND M7-02
	// BOUGHT A SECURITY DECISION WITH THE OTHER HALF. adminAttemptLimit is charged in
	// failLogin and nowhere else — completeLogin charges nothing — so a SUCCESSFUL
	// sign-in was bounded only by adminFloodLimit (3000 per 10 minutes). Measured by a
	// security audit on the shipped stack:
	//
	//	18 consecutive SUCCESSFUL sign-ins -> 18 x 303, median 2.39 s, zero 429
	//	13 consecutive FAILED sign-ins     -> 10 x 401, then 429 from the 11th
	//
	// That asymmetry was harmless while a valid credential implied a legitimate user.
	// 🔴 M7-02 ENDED THAT: the sign-up wizard is public, so anybody can mint themselves
	// a valid credential in a couple of minutes. "Successful login" stopped being a
	// proxy for "real operator" and became just another way to buy CPU.
	//
	// AND THE PADDING MULTIPLIED IT BY EIGHT. adminauth.padComparisons makes every
	// login cost MaxCandidates comparisons, which is what closed the timing channel —
	// and M7-02 justified that trade with "the attempt budget was already sized for
	// eight comparisons per request". THAT SENTENCE WAS TRUE OF THE FAILURE PATH AND
	// FALSE OF THIS ONE, which nothing was measuring. The arithmetic, at the repo's own
	// ~380 ms per cost-12 comparison:
	//
	//	failure path  10 x 8 x 0.38 s =    30.4 CPU-s per window = ~0.05 cores (as
	//	                                   documented; unchanged)
	//	success path  3000 x 8 x 0.38 s = 9120   CPU-s per 600 s = ~15 CORES, from ONE
	//	                                   address — against ~1.9 cores before padding
	//
	// 🔴 AND IT REACHES §4.6. The tap surface shares this process and this connection
	// pool (httpx.TapLimiter says so from its side); CPU that a panel flood has taken
	// is CPU a tap cannot have, and a tap that cannot be served is an attendance record
	// that is not written — the one thing §4.6 forbids losing.
	//
	// THE NUMBER IS DERIVED FROM OPERATOR BEHAVIOUR, the way every other budget in this
	// file is, and NOT from the CPU ceiling — the ceiling is then reported rather than
	// chosen:
	//
	//	10 admins behind one NAT, one sign-in each per window   10   (this file's own
	//	                                                             office model)
	//	x2 for a second device (phone and laptop)               20
	//	a large office of 30 admins on two devices              60
	//	x2 headroom over that                                  120   <- the limit
	//
	// RESULTING CEILING: 120 x 8 x 0.38 s = ~365 CPU-s per 600 s window per address,
	// i.e. ~0.61 of one core. That is BELOW the ~1.9 cores the success path could reach
	// before padding existed, so this closes a hole that predates M7-02 as well as the
	// one M7-02 widened.
	//
	// ⚠️ IT CAN REFUSE A CORRECT PASSWORD, WHICH adminAttemptLimit DELIBERATELY CANNOT.
	// That is the same trade adminFloodLimit already makes and is keyed the same way —
	// on the ADDRESS, never on the account — so it is not an account lockout: nobody
	// can spend a named operator's budget from somewhere else. Reaching it needs 120
	// sign-ins from one address in ten minutes, which is twice the largest office this
	// file models and is not a shape a person produces.
	//
	// WHY NOT SIMPLY CHARGE adminAttemptLimit ON SUCCESS: that budget is 10, sized for
	// wrong passwords, and ten is fewer than one busy office does legitimately. Reusing
	// it would lock real operators out of their own panel — the harm this file spends
	// its longest paragraph refusing.
	adminLoginWorkLimit  = 120
	adminLoginWorkPeriod = 10 * time.Minute

	// adminAccountLimit / adminAccountPeriod: 10 attributable failures in 10
	// minutes against ONE admin account, bounding audit_log ONLY.
	//
	// 🔴 THE M5-02 LESSON, APPLIED BEFORE IT COULD BE REPEATED: "a protection's COST
	// can be an attack on the thing it protects". There, two handler branches wrote
	// an audit row per request while charging no window, and 300 requests produced
	// 290 x 429 AND 300 permanent rows in a table that not even tappa_owner can
	// delete from. Here the same shape is available and is worse in one way: a
	// failed login against a KNOWN email writes a row into the tenant that owns
	// that email — and from M7-02 an attacker can put a victim's address into their
	// OWN tenant, so failures also write into the VICTIM's audit_log. Unbounded,
	// that is a write primitive into somebody else's append-only table.
	//
	// Past this limit the branch answers identically and writes ONE final
	// "rate_limited" row (FirstOverLimit), then nothing. The count keeps climbing,
	// so the window still expires normally.
	//
	// IT GATES NOTHING (see the account-vs-address reasoning above): a burnt audit
	// budget never refuses a login, so an attacker cannot use it to lock anyone out.
	//
	// 🔴 BUT IT IS A TRAIL-SILENCING PRIMITIVE, AND THAT WAS NOT WRITTEN DOWN. A
	// security audit named it: 11 requests burn one account's budget, and for the
	// rest of the window EVERY failed sign-in against that account goes unwritten —
	// the attacker's own, a third party's, and the one that matters most, "the
	// CORRECT password on a disabled account" (adminlogin.go). "No lockout" is true
	// and was the only half this comment used to state; the other half is "silence".
	//
	// WHY IT IS AN INTERRUPTION RATHER THAN A BLACKOUT, which is why it is a LIMIT
	// and not a hole: exactly one ActionAdminLoginLimited row is still written per
	// window per account, and it now carries SuppressedFrom — the ordinal of the
	// first suppressed attempt. So an investigator ALWAYS sees that the account was
	// attacked and when suppression began; what they cannot get from the database is
	// HOW MANY attempts followed.
	//
	// ⚠️ THE TOTAL IS NOT RECOVERABLE, and that is the honest residual. Carrying a
	// real count would need a row written when the WINDOW CLOSES, and httpx.Limiter
	// has no expiry hook — it evicts lazily, so there is no moment at which "the
	// window ended with N suppressed" can be observed. Adding one is infrastructure
	// (a timer or a sweep over the limiter's map), and it would put a write on a
	// path whose entire purpose is to stop writing. Counted, named, and handed on
	// rather than half-built.
	adminAccountLimit  = 10
	adminAccountPeriod = 10 * time.Minute
)

// ⚠️ THE VALUES ABOVE ARE PINNED BY TestPanelConstants_ShippedValuesArePinned
// (adminlogin_test.go). That is not ceremony: a family scan over every constant
// M6-01 phase B introduced changed each one ALONE and ran the suite, and four of
// the six budget numbers here turned out to be free to change — every test that
// mentioned them was written in terms of the constant, so the expectations moved
// with the value. The numbers are the product decision (adminratelimit.go's whole
// content is an argument about them), so they are pinned as literals in a test
// whose failure message carries the arithmetic to re-derive.
