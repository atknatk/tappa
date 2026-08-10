package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
)

// The WALL PLAQUES' WRITES — M6-06 PHASE B. MOUNT a plaque Tappa loaded, REPLACE the
// one already on a wall, and take one back OFF a wall into stock.
//
// 🔴 THERE IS NO CREATE AND NO DELETE. The panel cannot create a plaque
// (creating one means holding its AES key — user decision, 2026-08-08), cannot
// delete one (00004 revokes DELETE on `tags` from tappa_app), and cannot rewrite a
// uid or a key (00013 narrowed the UPDATE grant to five columns). What is reachable
// from HTTP is exactly what those grants permit.
//
// 🔴 EVERY ROUTE HERE MOUNTS ProtectWriting AND THE ORDER IS THE POINT.
// floodGate → sameOriginGate → requireAdmin → sessionGate: FOUR stages, with the
// Origin check BEFORE the resolver, so a cross-origin POST costs zero database
// work. M6-04 shipped that chain with the gate INSIDE the protected group and an
// audit measured what it bought: 300 cross-origin POSTs spent a signed-in
// manager's whole session budget and locked them out for ten minutes, charging 301
// unbudgeted session lookups on the way. A POST registered in mountSections would
// silently take the READ chain instead.
//
// 🔴 BOTH ACTS WRITE audit_log IN THE SAME TRANSACTION AS THE CHANGE
// (audit.RecordTx, inside internal/domain/tenant/plaque.go). `tags` has no
// updated_at, so without it there would be no record anywhere of who took a
// working plaque off a wall — and that plaque is the thing every tap at that
// entrance is authenticated by.
//
// 🔴 REPLACE IS CONFIRMED AND MOUNT IS NOT, AND THE ASYMMETRY IS A MEASUREMENT
// PLUS A DECISION RATHER THAN AN OVERSIGHT.
//
// What was measured first, live as tappa_app on 2026-08-09 inside a rolled-back
// transaction, because phase A's gate was argued from "a DELETE destroys the row
// forever and there is no archive":
//
//	DELETE FROM tags ...                            -> permission denied for table tags
//	retire, then status back to 'active' + stamps cleared  -> UPDATE 1, UPDATE 1
//	un-mount (status + location in one statement), re-mount -> UPDATE 1, UPDATE 1
//	afterwards: same uid, same wall, no stamp, key still 44 bytes
//
// So NOTHING IS DESTROYED and every column these acts write is schema-reversible.
// ⚠️ AND THAT IS NOT THE QUESTION PHASE A's GATE ANSWERED. It exists because a
// manager cannot UNDO the act from the panel, and that is true here: db/queries has
// no un-retire, and a mount of the retired uid answers `plaque-not-stock`
// (measured). Until somebody physically swaps the plaque, every tap at that entrance
// is refused (§5 row 1). "The row survives" and "the manager can put it back" are
// different sentences; the first was measured and the second is false, so the gate is
// required. Decision recorded 2026-08-09.
//
// ⚠️ THIS PARAGRAPH SAID "no un-retire AND NO UN-MOUNT (measured)" IN THE SAME CHANGE
// SET THAT ADDED unmountPlaque, ABOUT SIXTY LINES BELOW. The measurement was true when
// it was taken and the sentence was not re-read when the behaviour changed — the exact
// failure the round before had just been RED for, repeated inside the file that
// contained the fix. An un-mount IS shipped, and it is what makes the mount's
// ungated-ness honest.
//
// WHAT IS *NOT* APPLIED, and why, so the next reader does not read it as an
// inconsistency: phase A also made removal `owner`-only. That gate was for PERMANENT
// DELETION of configuration. Swapping a failed plaque is operational maintenance at
// one door — the job of the manager who is standing in front of it at six in the
// morning — and reserving it for an owner would make the product unusable at exactly
// the moment it is needed. Removal stays owner-only; this is not removal.
//
// AND MOUNT IS NOT GATED, measured rather than assumed: it takes a plaque OUT of a
// box and puts it in service, so the worst outcome is a plaque on the wrong door
// rather than a door with no working plaque, and it destroys nothing at all.
// TestPlaqueWrites_MountNeedsNoConfirmationAndReplaceDoes pins BOTH halves, so the
// asymmetry cannot quietly become "nothing is gated".
//
// 🔴 A REFUSED ACT REDIRECTS WITH A WORD, IT DOES NOT RE-RENDER. That is the
// opposite of the venue form and the difference is the input: a venue form carries
// eight fields a manager typed and a redirect would throw them away, while these
// two carry a uid and a choice from a dropdown. There is nothing to lose, and a
// redirect is what keeps a refresh from re-posting.

// panelPlaques is the slice of internal/domain/tenant this section needs, declared
// HERE at the consumer (§7).
//
// IT IS A FOURTH FIELD ON THE SAME PACKAGE AS staff AND venues, WHICH BENDS THE
// RULE M6-04 SET ("a different package gets a different field") AND IS THEREFORE
// STATED RATHER THAN SMUGGLED — the same way panelVenues states it. That rule
// exists so "no store call in ledger is not a SELECT" stays a fact grep can check;
// it separates READS from WRITES, and these are writes. What decides it here is
// blast radius: one wide interface carrying thirteen methods would let a change to
// the plaque side break the venue side's wiring with no compile error at the call
// site.
type panelPlaques interface {
	Screen(ctx context.Context, tenantID uuid.UUID) (tenant.PlaqueScreen, error)
	// Plaque resolves ONE plaque by uid. It is the fallback behind the capped
	// Screen list: a row past plaquePageLimit is still the tenant's.
	Plaque(ctx context.Context, tenantID uuid.UUID, uid string) (tenant.Plaque, error)
	// History is WHO did WHAT to one plaque, from audit_log — the half the `tags`
	// row cannot carry, because it has no actor column.
	History(ctx context.Context, tenantID uuid.UUID, uid string) ([]tenant.PlaqueEvent, error)
	Mount(ctx context.Context, c tenant.MountCommand) (tenant.Plaque, error)
	// Unmount is the UNDO of a mount: off the wall, back into stock. It is what
	// makes "a mount needs no confirmation" a true sentence.
	Unmount(ctx context.Context, c tenant.UnmountCommand) (tenant.Plaque, error)
	Replace(ctx context.Context, c tenant.ReplaceCommand) (tenant.Replacement, error)
	// ConfirmPlaqueAct answers "did THIS admin do THIS to THIS plaque just now?"
	// from the append-only trail. It decides whether an acknowledgement is PRINTED
	// and authorises nothing.
	ConfirmPlaqueAct(ctx context.Context, tenantID, actorID uuid.UUID, action, target string) (tenant.RemovalReceipt, error)
}

// mountPlaque binds a loaded plaque to a wall.
func (a *AdminAuth) mountPlaque(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.readPlaqueBody(w, r) {
		return
	}
	uid, ok := a.plaqueTarget(w, r, "uid")
	if !ok {
		return
	}
	rawVenue := strings.TrimSpace(r.PostFormValue("location_id"))
	venueID, err := uuid.Parse(rawVenue)
	if err != nil || venueID == uuid.Nil {
		a.log.Warn("panel plaque mount refused: the venue id could not be read")
		a.redirect(w, venueReturn("", "unknown-venue"))
		return
	}

	// 🔴 THE VENUE NAME IS READ BEFORE THE WRITE AND IS FOR THE TRAIL ONLY. The
	// acknowledgement sentence is built from the audit row rather than from the URL
	// (reading C'), and after this request the only place the name is guaranteed to
	// sit beside the act is that row. A failure to read it is NOT a failure of the
	// act: the mount still happens and the trail keeps an empty name, which the
	// heading falls back for.
	venueName := ""
	if ven, err := a.venues.Venue(r.Context(), id.TenantID(), venueID); err == nil {
		venueName = ven.Name
	} else if !errors.Is(err, tenant.ErrUnknownVenue) {
		a.log.Warn("panel plaque mount: could not read the venue name for the trail", "err", err)
	}

	mounted, err := a.plaques.Mount(r.Context(), tenant.MountCommand{
		TenantID: id.TenantID(),
		// 🔴 THE ACTOR COMES FROM THE SESSION. There is no field on this form for it
		// and there could not be: the id below was resolved from a signed cookie
		// against admin_users, which is the only thing audit_log.actor_id may hold.
		ActorID:    id.Admin.AdminUserID,
		UID:        uid,
		LocationID: venueID,
		VenueName:  venueName,
	})
	if word := plaqueRefusal(err); word != "" {
		a.log.Warn("panel plaque mount refused", "reason", word, "tag_uid", uid)
		a.redirect(w, venueReturn("", word))
		return
	}
	if err != nil {
		a.log.Error("panel: could not mount the plaque", "err", err, "tag_uid", uid)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	a.log.Info("panel plaque mounted", "tag_uid", mounted.UID, "location_id", venueID)
	a.redirect(w, plaqueReturn("plaque-mounted", mounted.UID))
}

// replacePlaque retires the plaque on a wall and binds its successor to the SAME
// wall, in one transaction.
//
// 🔴 THE WALL IS NOT AN INPUT. It is read from the retiring plaque inside the
// domain's transaction, because a replacement is by definition "the same door, a
// different plaque". Asking a manager to re-pick the venue would be a second chance
// to send the new plaque somewhere else — and it would cost seconds the M6-06
// card's own 30-second criterion does not have.
func (a *AdminAuth) replacePlaque(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.readPlaqueBody(w, r) {
		return
	}
	retiring, ok := a.plaqueTarget(w, r, "uid")
	if !ok {
		return
	}
	successor, ok := a.plaqueSuccessor(w, r)
	if !ok {
		return
	}
	// 🔴 THE CONFIRMATION GATE, AND IT IS PHASE A's PRIMITIVE RATHER THAN A SECOND
	// ONE. deactivateconfirm.go's value is HMAC-signed under a key derived from the
	// server's secret, bound to ONE ACTION, ONE SUBJECT and ONE PANEL SESSION, with a
	// TTL and a future-skew bound. Payload v3 widened the subject from a uuid to an
	// opaque string precisely so a 14-hex plaque uid fits it; half-copying the shape
	// into a new one is a mistake this repository has made four times.
	//
	// IT RUNS AFTER THE BOUNDARY CHECKS so a malformed uid is "unreadable" rather than
	// "not confirmed" — nothing was looked up, and telling a prober which gate they
	// missed is what the collapsed refusal exists to avoid.
	// ⚠️ THE CONFIRMATION IS BOUND TO THE PLAQUE COMING OFF, NOT TO THE ONE GOING ON,
	// AND THAT IS COUNTED RATHER THAN LEFT TO BE DISCOVERED. A value minted at the card
	// for "replace X" authorises replacing X with ANY spare of this tenant, not only
	// the one the dropdown was showing. It is consistent with what the mechanism claims
	// (deactivateconfirm.go: reaching the write implies the server RENDERED THE WARNING
	// for THIS subject, in THIS session, inside the window) and the warning is about X
	// — which spare goes up is the manager's own choice on the same screen. Nobody
	// outside the panel's signed-in operator can reach it, and the successor is still
	// validated, still same-tenant, still `unassigned`. Binding it would mean putting
	// the successor in the payload, i.e. a v4 and a second subject; it buys nothing
	// against the actor this gate actually faces.
	if why := a.confirmationRefusalFor(r, confirmActionReplacePlaque, retiring); why != "" {
		a.log.Warn("panel plaque replace refused: not confirmed", "reason", why)
		a.redirect(w, venueReturn("", why))
		return
	}

	// The venue name for the trail, resolved from the plaque coming off. Best
	// effort, for the reason mountPlaque gives.
	venueName := ""
	if old, err := a.plaques.Plaque(r.Context(), id.TenantID(), retiring); err == nil && old.LocationID != nil {
		if ven, err := a.venues.Venue(r.Context(), id.TenantID(), *old.LocationID); err == nil {
			venueName = ven.Name
		}
	}

	done, err := a.plaques.Replace(r.Context(), tenant.ReplaceCommand{
		TenantID:     id.TenantID(),
		ActorID:      id.Admin.AdminUserID,
		RetiringUID:  retiring,
		SuccessorUID: successor,
		VenueName:    venueName,
	})
	if word := plaqueRefusal(err); word != "" {
		a.log.Warn("panel plaque replace refused", "reason", word,
			"retiring_tag_uid", retiring, "successor_tag_uid", successor)
		a.redirect(w, venueReturn("", word))
		return
	}
	if err != nil {
		a.log.Error("panel: could not replace the plaque", "err", err,
			"retiring_tag_uid", retiring)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	// 🔴 CLEARED ON SUCCESS ONLY — deleteVenue's asymmetry, and for the same reason.
	// A replacement has a failure mode the deactivation gate does not: the spare can
	// be mounted by somebody else between the card rendering and the press landing.
	// That refusal is about the WORLD, not about the manager's intent, so making them
	// re-read the warning to pick a different spare would be a penalty for somebody
	// else's timing. The value dies with the act.
	a.short.clear(w, adminConfirmCookieName)
	a.log.Info("panel plaque replaced", "retired_tag_uid", done.Retired.UID,
		"mounted_tag_uid", done.Mounted.UID)
	// 🔴 THE REDIRECT NAMES THE RETIRED PLAQUE, NOT THE NEW ONE, AND THAT IS WHAT
	// KEEPS THE TWO ACKNOWLEDGEMENTS APART. Only a replacement writes
	// plaque.retired, so a word verified against it cannot be printed after a plain
	// mount — which writes plaque.mounted for the new uid exactly as a replacement
	// does.
	a.redirect(w, plaqueReturn("plaque-replaced", done.Retired.UID))
}

// unmountPlaque takes a plaque off its wall and puts it back in stock — the UNDO of
// a mount.
//
// 🔴 IT IS WHAT MAKES "a mount needs no confirmation" TRUE. Before it, a plaque put
// on the wrong door could not be moved, could not go back to stock, and with no
// spare in the box its card offered nothing at all — while every tap at that door
// was judged against the WRONG venue's IP and coordinate and landed in the approval
// queue (§5 row 7). Two shipped comments said the opposite, with a "measured" label
// on them; an audit drove the real behaviour.
//
// 🔴 AND IT CARRIES THE SAME CONFIRMATION A REPLACEMENT DOES, because the act is
// service-affecting even though it is reversible: the door has no working plaque
// until something is mounted there (§5 row 1). Same primitive, FIFTH action — not a
// second mechanism.
//
// ⚠️ WHAT IT DELIBERATELY IS NOT: a retirement. retired_at and replaced_by are
// untouched, so "when did this plaque leave the wall for good" still has exactly one
// answer, and no spare is spent to do it.
func (a *AdminAuth) unmountPlaque(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !a.readPlaqueBody(w, r) {
		return
	}
	uid, ok := a.plaqueTarget(w, r, "uid")
	if !ok {
		return
	}
	if why := a.confirmationRefusalFor(r, confirmActionUnmountPlaque, uid); why != "" {
		a.log.Warn("panel plaque unmount refused: not confirmed", "reason", why)
		a.redirect(w, venueReturn("", why))
		return
	}

	// The venue name for the trail, read BEFORE the write because afterwards the row
	// has none. Best effort, for the reason mountPlaque gives.
	venueName := ""
	if before, err := a.plaques.Plaque(r.Context(), id.TenantID(), uid); err == nil && before.LocationID != nil {
		if ven, err := a.venues.Venue(r.Context(), id.TenantID(), *before.LocationID); err == nil {
			venueName = ven.Name
		}
	}

	done, err := a.plaques.Unmount(r.Context(), tenant.UnmountCommand{
		TenantID: id.TenantID(), ActorID: id.Admin.AdminUserID,
		UID: uid, VenueName: venueName,
	})
	if word := plaqueRefusal(err); word != "" {
		a.log.Warn("panel plaque unmount refused", "reason", word, "tag_uid", uid)
		a.redirect(w, venueReturn("", word))
		return
	}
	if err != nil {
		a.log.Error("panel: could not unmount the plaque", "err", err, "tag_uid", uid)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}
	// Cleared on success only — deleteVenue carries the argument for the asymmetry.
	a.short.clear(w, adminConfirmCookieName)
	a.log.Info("panel plaque unmounted", "tag_uid", done.UID)
	a.redirect(w, plaqueReturn("plaque-unmounted", done.UID))
}

// readPlaqueBody bounds the body and parses it.
//
// 🔴 THE BODY IS BOUNDED BEFORE IT IS PARSED. MaxBytesReader makes ParseForm fail
// on an oversized body rather than buffering it, so the ceiling is enforced by the
// READ rather than by a length check afterwards (readVenueBody's shape).
//
// 🔴 THE PARSE ERROR IS CLASSIFIED, NOT PRINTED. net/url's failure QUOTES THE
// OFFENDING INPUT — an audit measured three bytes of a manager's note reaching the
// process log through exactly this branch in M6-04. Nothing a caller sent appears
// in the log line below.
func (a *AdminAuth) readPlaqueBody(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxPlaqueBody)
	if err := r.ParseForm(); err != nil {
		reason := "malformed form"
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			reason = "body over the limit"
		}
		a.log.Warn("panel plaque action refused: the form could not be read",
			"limit_bytes", maxPlaqueBody, "reason", reason)
		a.redirect(w, venueReturn("", "unreadable"))
		return false
	}
	return true
}

// plaqueTarget reads and VALIDATES one plaque uid out of the form (§7: the
// boundary validates, the domain sees data that is already good).
//
// 🔴 THIS IS THE HANDLER-BOUNDARY CHECK THE DATA LAYER LEFT OWED, and the reason
// is measured in db/queries/tags.sql: `'AABBCCDDEEFF01ZZZZZZ'::char(14)` TRUNCATES
// to 'AABBCCDDEEFF01' with NO ERROR, so an over-long successor uid silently becomes
// a DIFFERENT, possibly real plaque and the self-FK accepts it — the audit chain
// would then point at the wrong successor and nothing would say so.
// RetireTagForReplacement added a predicate on the UNCAST parameter for that; this
// is the §7 half, and it refuses the shape before a statement ever sees it.
//
// 🔴 THIS ONE READS THE SUBJECT OF AN ACT, WHICH IS A LOOKUP KEY. The row exists,
// `tags.uid` is char(14) and case-sensitive, and the table still holds 18 010
// lower-case rows that predate 00013 — so folding case here makes the panel unable
// to act on a row it lists, and makes ErrPlaqueFrozen unreachable (both measured).
// plaqueSuccessor below reads the other kind.
//
// ⚠️ THE TWO USED TO BE ONE FUNCTION TAKING THE VALIDATOR AS A PARAMETER, AND AN
// AUDIT MEASURED WHY THAT WAS NOT A MECHANISM: both validators returned a plain
// string, so swapping them at a call site compiled and left the whole suite green.
// They now return two TYPES, so the call sites cannot be exchanged at all — and the
// RUN-time guarantee is still tenant.Replace re-validating, which is named there.
//
// AN UNREADABLE ID IS "unreadable", NOT "unknown", because nothing was looked up —
// the distinction the section's vocabulary makes and deleteTarget follows.
func (a *AdminAuth) plaqueTarget(w http.ResponseWriter, r *http.Request, field string) (string, bool) {
	uid, err := tenant.PlaqueRef(r.PostFormValue(field))
	if err != nil {
		a.log.Warn("panel plaque action refused: the plaque id could not be read",
			"field", field)
		a.redirect(w, venueReturn("", "unreadable"))
		return "", false
	}
	return uid, true
}

// plaqueSuccessor reads the plaque that is going ON a wall — a value being WRITTEN
// into `replaced_by`, whose own SQL predicate accepts the canonical form only.
//
// IT RETURNS tenant.CanonicalUID, WHICH IS THE POINT: the command field that
// receives it takes no other type, so a lookup key cannot reach it even by a
// copy-paste that compiles.
func (a *AdminAuth) plaqueSuccessor(w http.ResponseWriter, r *http.Request) (tenant.CanonicalUID, bool) {
	uid, err := tenant.PlaqueUID(r.PostFormValue("successor"))
	if err != nil {
		a.log.Warn("panel plaque action refused: the successor id could not be read")
		a.redirect(w, venueReturn("", "unreadable"))
		return "", false
	}
	return uid, true
}

// plaqueRefusal maps a typed domain refusal onto the section's closed vocabulary,
// or "" when the error is not one (nil included).
//
// 🔴 EVERY ARM IS A DIFFERENT SENTENCE BECAUSE EACH IS A DIFFERENT THING TO DO
// ABOUT IT (§4.6). "Somebody mounted that spare while you were looking" sends a
// manager back to pick another; "that plaque is not on a wall any more" tells them
// the job is already done. Collapsing them into one "could not" would leave them
// guessing.
//
// 🔴 ErrPlaqueFrozen IS MAPPED RATHER THAN LEFT TO THE 500 BRANCH, and it is a
// measured development reality rather than a hypothetical: ListTagsForTenant
// returns the 18 010 rows whose lower-case uid migration 00013 froze, so a manager
// on a development database can open one and press a button the database will
// refuse with SQLSTATE 23514. Without this arm that is a 500 on a row they can see.
func plaqueRefusal(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, tenant.ErrUnknownPlaque):
		return "unknown-plaque"
	case errors.Is(err, tenant.ErrPlaqueUID):
		return "unreadable"
	case errors.Is(err, tenant.ErrPlaqueNotInStock):
		return "plaque-not-stock"
	case errors.Is(err, tenant.ErrPlaqueNotOnAWall):
		return "plaque-not-active"
	case errors.Is(err, tenant.ErrPlaqueNoWall):
		return "plaque-no-wall"
	case errors.Is(err, tenant.ErrSamePlaque):
		return "same-plaque"
	case errors.Is(err, tenant.ErrPlaqueFrozen):
		return "plaque-frozen"
	case errors.Is(err, tenant.ErrUnknownVenue):
		return "unknown-venue"
	default:
		return ""
	}
}
