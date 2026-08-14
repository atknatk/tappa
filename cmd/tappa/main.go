// Command tappa is the Tappa server: a single binary serving the employee tap
// page, the admin dashboard and the JSON API.
//
// This file does wiring only — no business rules. See CLAUDE.md §3.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/review"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(cfg.LogLevel),
	})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The pool is built with a bounded startup context: an unreachable database
	// must fail the boot, not hang it.
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelDial()
	data, err := db.New(dialCtx, cfg)
	if err != nil {
		return err
	}
	defer data.Close()

	sessions, err := session.New(data, cfg)
	if err != nil {
		return err
	}
	invites, err := invite.New(data, cfg)
	if err != nil {
		return err
	}
	trail, err := audit.New(data)
	if err != nil {
		return err
	}
	activation, err := handler.NewActivation(invites, sessions, trail, cfg, slog.Default())
	if err != nil {
		return err
	}

	// The tap screen (M5-04). It is given the NON-ADVANCING half of internal/sun
	// on purpose: sun.Verifier offers both entry points, but handler.Tap's
	// consumer-side interface names only PreviewWithoutReplayProtection, so the
	// page cannot spend a chip's counter even by accident. The atomic advance
	// belongs to POST /api/checkin (M5-05).
	directory, err := tenant.NewDirectory(data)
	if err != nil {
		return err
	}
	// The tap ORCHESTRATOR (M5-05): the one thing on this path that writes an
	// attendance record. It gets the raw *db.DB rather than the verifier, because
	// the atomic counter advance and the record insert are its own work and
	// nothing above it may do either.
	checkins, err := checkin.New(data, trail, cfg, slog.Default())
	if err != nil {
		return err
	}
	tap, err := handler.NewTap(sun.NewVerifier(data, cfg.TagKEK), directory, sessions, checkins, trail, cfg, slog.Default())
	if err != nil {
		return err
	}

	// Panel authentication (M6-01 phase B). It is a SEPARATE manager from
	// `sessions` on purpose — separate table, separate resolver, separate cookie
	// and a separately derived HMAC key — so an employee cookie can never reach
	// the panel and an admin cookie can never reach the tap surface.
	//
	// It also carries the middleware M6-02 mounts the dashboard behind
	// (AdminAuth.Protect), so the dashboard does not have to know how an admin is
	// resolved.
	admins, err := adminauth.New(data, cfg)
	if err != nil {
		return err
	}
	// The panel's READ side (M6-03). It takes the same *db.DB every other service
	// does, so its queries run inside WithTenant and are subject to RLS as well as
	// to their own explicit tenant predicate.
	records, err := ledger.NewReader(data, slog.Default())
	if err != nil {
		return err
	}
	// The panel's WRITE side (M6-04): the FLAGGED approval queue. It takes the SAME
	// audit recorder every other service does, because the review row and its audit
	// row have to share one transaction — internal/domain/review says why that is
	// the opposite of what most callers of internal/audit want.
	reviewer, err := review.NewReviewer(data, trail, slog.Default())
	if err != nil {
		return err
	}
	// The employees section's WRITE side (M6-05 phase B): deactivate, and move
	// somebody between venues or departments. It takes the SAME audit recorder for
	// the same reason the reviewer does — the employee change and its trail row have
	// to share one transaction, which is the opposite of what most callers of
	// internal/audit want (internal/domain/tenant/staff.go says why).
	staff, err := tenant.NewStaff(data, trail, slog.Default())
	if err != nil {
		return err
	}
	// The locations section's venue and department side — its reads and its writes
	// (M6-06 phase A): venue
	// configuration and department management. It takes the SAME audit recorder as
	// the two above, and here the reason is sharper than for either of them —
	// `locations` has no updated_at, so the trail row written inside the write's own
	// transaction is the ONLY place a change to the evidence a tap is judged on
	// leaves a mark (internal/domain/tenant/venue.go).
	venues, err := tenant.NewVenues(data, trail, slog.Default())
	if err != nil {
		return err
	}
	// The wall plaques (M6-06 phase B): the list a manager reads, mounting a plaque
	// Tappa loaded, replacing the one already on a wall, and taking one back off a
	// wall into stock. Same audit recorder
	// again, and here the reason is the sharpest of the four — retiring a plaque
	// takes away the thing every tap at that entrance is authenticated by (§5 row 1),
	// and `tags` has no updated_at either.
	//
	// 🔴 IT CANNOT CREATE A PLAQUE AND THERE IS NOTHING TO WIRE FOR THAT. Creating one
	// means holding its AES key (user decision, 2026-08-08: Tappa encodes the plaque
	// and loads the row; the panel only binds it), so db/queries/tags.sql ships no
	// INSERT and this type exposes no such method.
	plaques, err := tenant.NewPlaques(data, trail, slog.Default())
	if err != nil {
		return err
	}
	// The manual record writer (M6-08) — the SECOND writer of `transactions` in this
	// process, and the first that is not a tap. It exists because Q18 decided the
	// system produces no checkout of its own: an hour the software invented is an hour
	// that stops being evidence, so somebody has to type it.
	//
	// Same audit recorder again, and here the reason is the strongest of the five: the
	// record and its trail row are ONE act, `transactions` takes no UPDATE and no
	// DELETE, and a record that appeared with no trail would be an hour on a payslip
	// that nobody can be shown to have entered.
	entries, err := manual.NewRecorder(data, trail, slog.Default())
	if err != nil {
		return err
	}
	// records is passed TWICE, as the day reader and as the queue reader. Two
	// parameters rather than one because internal/handler declares two narrow
	// interfaces over it (§7, the consumer owns the interface); one implementation
	// satisfies both.
	//
	// 🔴 `invites` IS PASSED TO THE PANEL AS WELL AS TO THE ACTIVATION FLOW, AND IT IS
	// THE SAME MANAGER ON PURPOSE. It is what mints the code, and it hands the link to
	// a Channel rather than returning it — so the panel's access to a raw credential is
	// the sink internal/invite built for exactly this screen, not a second minting
	// path. Until this line the seam had no PRODUCTION caller — the type existed and so
	// did its audit action, and nothing in cmd/ reached them.
	//
	// ⚠️ "NO CALLER AT ALL" WOULD BE FALSE and was written that way for one round:
	// `git grep NewManagerVisibleChannel` finds internal/handler/e2e_db_test.go, where
	// M5-02 drove the channel end to end. The distinction matters — the seam was
	// TESTED and unmounted, which is the M5-04 shape (a capability delivered, approved
	// and dead in the wired product) rather than an unproven one.
	// 🔴 THE RULEBOOK IS BUILT FROM THE DECIDING SERVICE'S OWN WINDOWS, NOT FROM cfg.
	// checkins.Params() returns the bounded guardrail windows checkin.New resolved, so
	// the Policies screen prints the numbers the tap engine actually compares against.
	// Reading cfg a second time here would be a second construction of that wiring --
	// hand-off N3 was exactly that defect one layer down, where TAPPA_DEBOUNCE_SECONDS
	// was range-checked at startup and then never reached the guardrail, so a
	// deployment that configured 120 s ran 60 s and nothing said so. A screen that
	// re-derived it could disagree with the engine in the same silent way, and it is
	// the screen a customer would believe.
	// THE GPS RING COMES FROM THE SAME PLACE FOR THE SAME REASON. It is what
	// tap.Decide compares against through geo.WithinRadius, so the screen shows the
	// deciding service's own value rather than re-reading the environment.
	rules, err := tenant.NewRulebook(data, checkins.Params(), checkins.GPSRadiusM(), slog.Default())
	if err != nil {
		return err
	}
	// 🔴 THE WRITER'S AUTHORITY IS THE DECIDING SERVICE ITSELF (M6-09 phase B). The
	// panel asks the policy engine before it changes a rulebook, and it asks through
	// checkin.Service.StoredSet — the NON-materialising read — so an admin who is
	// about to be refused leaves no rows behind in an append-only table. Passing
	// `checkins` here rather than building a second assembler is what keeps the gate
	// and the tap path deciding on the same rulebook.
	ruleWriter, err := tenant.NewRuleWriter(data, trail, checkins, checkins.Params(), slog.Default())
	if err != nil {
		return err
	}
	// 🔴 THE BILLING REGISTER (M6-12 phase B). It takes the same trail every other
	// writer takes, and for a reason that is stronger here than anywhere else:
	// billing.NewBook REFUSES a nil audit sink, because Close writes the frozen row and
	// the `billing.period_closed` row in ONE transaction — if the trail cannot be
	// written the freeze rolls back with it. Everywhere else a lost audit row would
	// hide a FAILURE; here it would hide a permanent, irreversible SUCCESS, and an
	// invoice nobody can attach to a person is worse than no invoice.
	//
	// NO PRICE AND NO MONTH BOUNDARY IS PASSED IN. Both live in migration 0016's
	// functions, called by db/queries/billing.sql, so there is ONE definition shared by
	// the live preview and the statement that freezes — a Go copy here would be a
	// second one that drifts.
	books, err := billing.NewBook(data, trail, slog.Default())
	if err != nil {
		return err
	}
	// 🔴 TAPPA'S OWN LEGAL TEXTS (M7-06). It is the eighth writer and the only one
	// whose rows belong to NO tenant: legal_documents has no tenant_id at all
	// (migration 00020), so nothing this store does can name a business. It takes the
	// same trail as the others because §4.3 applies here too — a publication changes
	// what every visitor to /legal reads — and legal.NewStore REFUSES a nil sink for
	// the same reason billing.NewBook does: the row is written with RecordTx, inside
	// the transaction that publishes, so a trail that cannot be written rolls the
	// publication back with it.
	texts, err := legal.NewStore(data, trail)
	if err != nil {
		return err
	}
	// 🔴 THE SNAPSHOT IS FILLED HERE AND A FAILURE IS NOT FATAL, WHICH IS A DECISION.
	// The public pages serve from memory precisely so /legal/* never touches the pool
	// (internal/domain/legal says why), and this is the read that fills it. If the
	// database is unreachable at boot the four pages fall back to exactly what they
	// printed before M7-06 — "this text has not been published yet" — which
	// UNDER-claims rather than over-claims, and is the direction this product is
	// required to be wrong in. Refusing to start would take the whole marketing site
	// down over a document nobody had published yet.
	//
	// IT NAMES NO TENANT, because there is none to name: legal.readContext runs the
	// read under a uuid that matches no tenant, so every RLS-scoped table is empty for
	// its duration and the only thing reachable is the one table whose policy is
	// `USING (true)`.
	if err := texts.Refresh(ctx); err != nil {
		slog.Default().Error("the published legal texts could not be read at start-up; /legal will show its placeholders until the next publication", "err", err)
	}
	panelAuth, err := handler.NewAdminAuth(admins, trail, records, records, reviewer, staff, invites, venues, plaques, entries, rules, ruleWriter, books, texts, cfg, slog.Default())
	if err != nil {
		return err
	}

	// The PUBLIC surface (M7-01): the landing page at / and the four legal documents
	// under /legal. Until this line the root of the site answered 404 — the router
	// registered /healthz and /static/* at the top level and nothing else.
	//
	// 🔴 IT TAKES NO POOL, NO SESSION MANAGER AND NO AUDIT SINK, AND THAT IS THE
	// SECURITY DECISION RATHER THAN A CONVENIENCE. Every other feature above is handed
	// all three; this one is handed none, so no handler on it can reach the database,
	// read a cookie or write a row. internal/handler.Marketing records why that shape
	// is what replaces the panel's protections on a surface that has no identity to
	// protect.
	//
	// ⚠️ M7-06 GAVE IT ONE READER AND THE SENTENCE ABOVE STILL HOLDS. `texts` is the
	// legal-document SNAPSHOT, whose only method takes no context and returns no
	// error — it cannot query, so the pool stays out of reach of the one surface that
	// is deliberately unmetered.
	//
	// IT IS MOUNTED LAST for readability only — chi matches on the pattern, not on
	// registration order, and these paths overlap with nothing above.
	marketing := handler.NewMarketing(texts, slog.Default())

	// 🔴 THE SIGN-UP WIZARD (M7-02) — THE FIRST PATH IN THIS PROCESS THAT CREATES A
	// TENANT. Until this line, `INSERT INTO tenants` existed only in the seed fixture
	// and in test helpers, which is the fact migration 00011 built a risk boundary on
	// and said in writing would expire here.
	//
	// IT TAKES THE SAME audit RECORDER EVERY OTHER WRITER TAKES, and the reason is
	// the strongest of the eight: a registration is tenant + venues + first operator
	// in ONE transaction, and internal/domain/signup uses RecordTx so the trail row
	// shares that transaction. A registration that half-happened would leave a
	// `tenants` row nobody can sign into, cannot be deleted (§4.6) and holds the VAT
	// number against the customer's own second attempt.
	provisioner, err := signup.NewProvisioner(data, trail, slog.Default())
	if err != nil {
		return err
	}
	// THE VIES CHECKER IS BUILT HERE AND IS THE ONLY OUTBOUND HTTP CLIENT IN THIS
	// PROCESS. Q09 (2026-08-13) made the check BEST EFFORT: a timeout or an outage
	// records "not verified" and never stops a registration, so this dependency
	// cannot take the sign-up flow down with it. It is net/http and nothing else
	// (§1: no new dependency), it is pointed at a constant URL, and the endpoint that
	// calls it carries its own outbound budget (internal/handler/signupratelimit.go).
	//
	// ⚠️ IT IS PASSED EVEN IN DEVELOPMENT, deliberately: a checker that only exists
	// in production is a code path that is first exercised on a customer. What makes
	// that safe is the shape rather than the environment — the wizard's tests drive a
	// local server through the same seam, and a machine with no route to the
	// Commission simply records "not verified", which is a state the product handles.
	signupFlow, err := handler.NewSignup(provisioner, signup.NewChecker(), cfg, slog.Default())
	if err != nil {
		return err
	}

	// PANEL PASSWORD RECOVERY (M7-04 phase B) — the flow that lets an operator who
	// cannot sign in get back in without another owner doing it for them.
	//
	// 🔴 THE DELIVERY CHANNEL IS nil, AND THAT IS THE SHIPPED STATE RATHER THAN A
	// MISSING WIRE. Q02 — which mail provider, in which region, under which processing
	// agreement — is unanswered, so this process has no mail transport; and unlike
	// invitations there is no interim channel available, because the interim channel
	// would be "show the link to whoever typed the address into a public form", which
	// ADR 0015 identifies as the one thing standing between minting and account
	// takeover. config.ResetDelivery carries the argument in full.
	//
	// WHAT nil DOES: the request form says, before anything is typed, that this
	// deployment cannot send a link — and the POST answers without resolving the
	// address, minting a row or retiring anybody's pending link. The alternative
	// (mint and fail to send) would manufacture the harm ADR 0015 accepts, for no
	// benefit at all.
	//
	// THE VALUE IS SWITCHED ON A CONFIG STRING rather than hardcoded so that the day a
	// transport exists it is one case in this switch and nothing else moves. An
	// unknown value never reaches here: config.Load refuses to boot on it.
	var resetChannel handler.ResetChannel
	switch cfg.ResetDelivery {
	case config.ResetDeliveryNone:
		resetChannel = nil
	default:
		// Unreachable: config.Load validates the value. It is written anyway because
		// an unreachable branch that fails CLOSED is what stops the next person's new
		// case from silently defaulting to "no delivery, but the screen says a link is
		// on its way".
		return fmt.Errorf("main: TAPPA_RESET_DELIVERY=%q passed config validation but nothing implements it", cfg.ResetDelivery)
	}
	resets, err := adminauth.NewResets(data, cfg)
	if err != nil {
		return err
	}
	resetFlow, err := handler.NewAdminReset(resets, resetChannel, trail, cfg, slog.Default())
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpx.NewRouter(cfg, activation, tap, panelAuth, marketing, signupFlow, resetFlow),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		// Drain in-flight taps before exiting: a dropped request is a lost
		// attendance record, and records are never lost (CLAUDE.md §4).
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func parseLevel(s string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return slog.LevelInfo
	}
	return l
}
