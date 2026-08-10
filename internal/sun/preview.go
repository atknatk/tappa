package sun

import (
	"context"
	"errors"
	"fmt"

	"github.com/atknatk/tappa/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A SECOND ENTRY POINT THAT DELIBERATELY DOES NOT PROTECT AGAINST REPLAY.
//
// This file exists for ONE caller — GET /t, the tap page (M5-04) — and for one
// reason: the page must be able to say "this is a real plaque and this URL was
// really signed by its chip" WITHOUT spending the chip's counter. Verify cannot
// do that, because step 6 of its fixed flow is AdvanceCounter (verify.go), and
// an employee who opens the page and then walks away would burn a counter value
// that a later, genuine tap needs. The M5-04 card states the requirement
// plainly: "the counter is NOT advanced while the page opens".
//
// 🔴 EVERYTHING BELOW IS THE DANGEROUS PART OF THAT TRADE, so read it before
// calling anything here.
//
// PreviewWithoutReplayProtection ANSWERS A NARROWER QUESTION THAN Verify:
//
//	Verify                          "is this tap's SUN genuine RIGHT NOW?"
//	                                = CMAC verified AND the counter advanced.
//	PreviewWithoutReplayProtection  "did this UID resolve, is the tag alive, and
//	                                does the CMAC check out against the tag's key?"
//	                                — and NOTHING about the counter.
//
// A REPLAYED URL PASSES THIS FUNCTION. That is not a defect, it is the whole
// shape of it: an attacker who kept yesterday's tap URL still has a genuine
// CMAC for a genuine (uid, ctr) pair, and the ONLY thing that has ever
// distinguished that from a fresh touch is the strict less-than guard on
// last_ctr inside the single atomic UPDATE (§4.4, store.AdvanceTagCounter).
// This function does not run it, does not read it, and does not compare the
// counter in Go — a read-then-compare here would be the TOCTOU hole §4.4 names,
// dressed as a helpful hint.
//
// 🔴 THEREFORE: POST /api/checkin (M5-05) MUST CALL Verify — or, at minimum,
// AdvanceCounter — ON EVERY TAP IT RECORDS. There is no arrangement in which
// this function's answer substitutes for that. A caller that treats a
// successful preview as proof of a fresh physical touch has re-opened replay,
// and no test in this package can catch that from here, because the defect
// would live at the call site.
//
// WHAT MAKES THAT HARD TO DO BY ACCIDENT — measures, not hopes:
//
//  1. The NAME says it. There is no Check, Validate or IsValid in this package;
//     a reader who greps for "the SUN check" finds Verify.
//  2. The RETURN TYPE IS DIFFERENT. Preview is not Result and is not
//     assignable to it, so code written against Verify does not compile against
//     this function — the substitution has to be typed out on purpose.
//  3. Preview HAS NO SUNValid FIELD, and — the part an audit had to correct —
//     IT ALSO CARRIES NO COUNTER. An earlier version returned the whole
//     db.ResolvedTag, which handed the caller last_ctr, so
//     `pv.CMACValid && p.Ctr > pv.Tag.LastCtr` was a sentence somebody could
//     write: read the counter, compare it in Go, conclude "SUN valid". That is
//     the TOCTOU §4.4 names by name, and this type was quietly supplying its
//     ingredients while the comment claimed CMACValid was the only field on
//     offer. The struct now carries the three facts the page actually needs
//     and nothing a replay check could be assembled from. (Reflection tests
//     fail red on a SUNValid field, on a counter field, and on a byte field.)
//  4. It is proven, in tests, that this path reaches NEITHER WithTenant NOR
//     AdvanceCounter — with a counting fake, and against real Postgres by
//     opening the page N times and reading tags.last_ctr back unchanged.
//
// SECRETS (§4.7): as with the rest of this package, nothing here logs and no
// error carries a key or a CMAC — the shared inspect step is the same code
// Verify uses, and verify_test.go's leak test covers both. The RESULT is now
// secret-free too: db.ResolvedTag carries aes_key_ref (the KEK-wrapped per-tag
// key), and returning it put that key in an HTTP handler's hands for no reason.
// Verify still returns it, because the decision layer is the tag's owner; the
// page is not.

// Preview is what the tap PAGE learned about a tap URL, without spending the
// chip's counter. It is a fact about cryptography and tag state — never a
// verdict, and never proof that the tap is fresh.
type Preview struct {
	// CMACValid reports ONLY that the chip's truncated CMAC verified against
	// this tag's key. It is deliberately NOT called SUNValid, because it does
	// not carry that meaning: a replayed URL from last week has a perfectly
	// valid CMAC. SUN validity additionally requires the counter to have
	// advanced, which only Verify establishes.
	//
	// It is false — with a nil error — for every case the flow short-circuits
	// on, and only ONE of those three is about a signature:
	//
	//	a plaque whose status is not `active` — retired, lost, or loaded but not
	//	  yet mounted (§5 line 1, before any cryptography runs). NOTHING was
	//	  checked here; the plaque is out of service.
	//	a QR URL with no SUN parameters at all. There is nothing to check.
	//	a CMAC that genuinely did not verify.
	//
	// 🔴 SO A CALLER MUST NOT TURN THIS FIELD INTO A SENTENCE ABOUT A SIGNATURE.
	// TagStatus below is what separates the first case from the third, and the
	// decision engine uses it: sys:tag-not-active is guardrail #2 and
	// sys:sun-invalid is #3, so a plaque in a box is refused for being in a box.
	CMACValid bool

	// TenantID is the tenant the TAG resolved to.
	//
	// 🔴 THE TAG HALF OF THE N5 HAND-OFF (state.md): the tap page must carry it
	// forward so M5-05 can put both tenants into tap.Input and finally make
	// sys:tenant-mismatch bite. The page CARRIES it and decides nothing with
	// it — comparing the two tenants is a decision and belongs to the policy
	// engine.
	TenantID uuid.UUID

	// TagStatus is tags.status (active | retired | lost | unassigned), passed on so
	// the decision engine can apply §5 line 1 to a RECORDED attempt rather than the
	// page silently dropping one (§4.6).
	//
	// ⚠️ THE FOURTH VALUE ARRIVED IN MIGRATION 00013 and this comment named three
	// until phase B. That is not a typo worth ignoring: `unassigned` is the value a
	// deny-list in the policy engine failed to see, and the reason it is carried
	// here as a STRING rather than as a bool is precisely so a new value travels
	// without anybody editing this file.
	//
	// It is the status as a plain string rather than the row it came from,
	// deliberately: see measure 3 above for what the row was also carrying.
	TagStatus string

	// Location is the tapped plaque's location, surfaced as a first-class field
	// exactly as Result.Location is: it is the venue
	// whose name the page greets the employee with, and the venue the decision
	// engine matches IP/GPS/shift against — the TAPPED location, not the
	// employee's profile location (§5).
	//
	// 🔴 nil MEANS THE PLAQUE IS NOT ON A WALL — see db.ResolvedTag.LocationID for
	// the driver measurement that made this a pointer in M6-06 phase B. The page
	// renders WITHOUT a venue name in that case, which is the same treatment a
	// plaque belonging to another tenant already gets, and the POST then records the
	// refusal (§4.6) instead of the page dropping it.
	Location *uuid.UUID
}

// PreviewWithoutReplayProtection resolves the tag and checks the chip's CMAC
// WITHOUT advancing the monotonic counter. Read the file header before calling
// it; the short version is that it CANNOT tell a fresh touch from a replay, and
// the caller that records a tap must still run the atomic advance.
//
// Outcomes mirror Verify's, minus everything about the counter:
//
//   - ErrUnknownTag         — the uid matched no tag. No tenant, no location,
//     nothing to render and nothing to record against.
//   - any other non-nil err — infrastructure or configuration (database down,
//     corrupt aes_key_ref, wrong KEK). NOT a normal reject.
//   - (Preview{CMACValid:false}, nil) — retired/lost tag, QR URL, or a CMAC
//     that did not verify. All three are normal outcomes that still carry a
//     tenant and a location, so the page can render and the eventual POST can
//     produce a RECORDED decision (§4.6) instead of a silent drop.
func (v *Verifier) PreviewWithoutReplayProtection(ctx context.Context, p Params) (Preview, error) {
	tag, cmacOK, err := v.inspect(ctx, p)
	if err != nil {
		if errors.Is(err, ErrUnknownTag) {
			return Preview{}, ErrUnknownTag
		}
		return Preview{}, fmt.Errorf("sun: preview: %w", err)
	}
	// Copied FIELD BY FIELD rather than by embedding the row: what this type
	// does not carry is the point of it (measure 3).
	return Preview{
		CMACValid: cmacOK,
		TenantID:  tag.TenantID,
		TagStatus: tag.Status,
		Location:  tag.LocationID,
	}, nil
}

// inspect is steps 2-5 of the fixed SUN flow (verify.go): resolve the uid,
// short-circuit on a dead tag, short-circuit on a QR URL with no SUN, then
// unwrap the tag key and check the CMAC. It stops one step BEFORE the atomic
// counter advance, which is what makes it shareable between Verify (which goes
// on to advance) and the preview (which must not).
//
// IT IS UNEXPORTED, AND THAT IS LOAD-BEARING. Exporting a "everything except
// the replay guard" helper would hand every future caller the footgun this
// package spends two files trying to keep pointed at the floor; the only way
// out of this package is Verify (which advances) or
// PreviewWithoutReplayProtection (whose name and type say what it is not).
//
// Errors are returned UNWRAPPED so each caller can apply its own prefix and
// Verify's error strings stay exactly what they were before this refactor.
func (v *Verifier) inspect(ctx context.Context, p Params) (db.ResolvedTag, bool, error) {
	// Step 2: resolve the plaque uid -> tag, context-less (the tenant is
	// produced here, not supplied; ADR 0002 item 7).
	tag, err := v.tags.GetTagByUID(ctx, p.UID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ResolvedTag{}, false, ErrUnknownTag
		}
		return db.ResolvedTag{}, false, fmt.Errorf("resolve tag: %w", err)
	}

	// Step 3: ANY status other than `active` is a §5 line-1 reject. Short-circuit
	// BEFORE unwrapping any key — a plaque that is not in service must not touch
	// cryptography.
	//
	// 🔴 IT IS AN ALLOW-LIST AND THE VALUE SET HAS ALREADY GROWN ONCE. Migration
	// 00013 added a FOURTH status, `unassigned` — a plaque Tappa encoded and loaded
	// but nobody has mounted yet (the inventory model) — and a deny-list elsewhere
	// did not see it. Asking for `active` is what makes the next value refused on the
	// day it is added rather than on the day somebody remembers.
	//
	// 🔴 WHAT cmacOK=false MEANS ON THIS BRANCH, because the field's NAME does not
	// say it and a migration comment once got this wrong in print. It means the CMAC
	// WAS NEVER ASKED, not that it failed: the key is never unwrapped and no
	// comparison runs. The distinction is not academic, because it decides which
	// sentence the employee is shown:
	//
	//	the DECISION layer reaches sys:tag-not-active FIRST (guardrail #2, ahead of
	//	sys:sun-invalid at #3), because internal/domain/checkin puts tags.status into
	//	the policy context from the row it re-read — so the recorded reason is about
	//	the plaque's STATE, which is the true one.
	//	sys:sun-invalid, whose reason is about a SIGNATURE, is reached only by a tap
	//	whose plaque IS active and whose CMAC genuinely did not verify.
	//
	// ⚠️ THAT ORDER IS THE ONLY THING KEEPING THE TWO APART, and it holds because
	// this function's caller carries the status forward (Preview.TagStatus) rather
	// than collapsing everything into one boolean. A caller that reported only
	// CMACValid would make a plaque sitting in a box indistinguishable from a forged
	// signature — which is exactly what an earlier reading of this branch described,
	// and it was true while sys:tag-not-active was still a deny-list that `unassigned`
	// escaped.
	if tag.Status != tagStatusActive {
		return tag, false, nil
	}

	// Step 4: a QR URL carries no proof of moment (no ctr, no cmac). Nothing to
	// verify; base:qr-requires-ip takes over downstream (§5).
	if !p.HasSUN() {
		return tag, false, nil
	}

	// Step 5: unwrap this tag's key, verify the chip's CMAC, wipe the key.
	ok, err := v.verifySUN(tag, p)
	if err != nil {
		return tag, false, err
	}
	return tag, ok, nil
}
