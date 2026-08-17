package sun

import (
	"context"
	"errors"
	"fmt"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Verify is the SINGLE entry point that assembles the M2 pieces (Parse, resolve,
// Unwrap, verifyMAC, AdvanceCounter) into one ordered flow (m2-sun.md M2-07,
// skill tappa-sun). It answers ONE question — "is this tap's SUN genuine, and by
// how much did the counter jump?" — and NOTHING more.
//
// VERIFY DOES NOT DECIDE. It returns a Result (a fact: SUNValid + tag + location
// + ctr gap), never a verdict. ok / flag / reject and the trust score are the job
// of internal/domain/tap via the policy engine (CLAUDE.md §5); pulling any of
// that in here would fold the whole decision table into the crypto layer. The
// division mirrors §5's decision table: Verify supplies the raw truth for line 1
// (tag status) and line 2 (SUN valid = CMAC good AND counter advanced); the
// decision engine applies the rules.
//
// ORDER IS FIXED and load-bearing (m2-sun.md M2-06, skill tappa-sun):
//
//	2. resolve tag by uid (context-less)  — unknown uid -> ErrUnknownTag (reject)
//	3. status active?                      — retired/lost -> SUNValid=false, STOP
//	4. HasSUN()?                           — QR (no SUN)  -> SUNValid=false, STOP
//	5. Unwrap key -> verifyMAC -> Zero     — bad CMAC     -> SUNValid=false, STOP
//	6. AdvanceCounter (verify FIRST)       — replay/race  -> SUNValid=false
//	7. return Result{SUNValid, Tag, Location, CtrGap}
//
// The counter is advanced ONLY after the CMAC has verified (step 6 strictly after
// step 5). The reverse would let an attacker push last_ctr forward with
// invalid-CMAC requests and get legitimate taps rejected — a DoS (§4.4). Steps 3
// and 4 short-circuit BEFORE any key is unwrapped, so a dead tag or a QR fallback
// never touches cryptography and never advances the counter.
//
// SECRETS (§4.7). This file logs NOTHING — like the rest of internal/sun it is
// side-effect-light so no key, session key, SV2 or CMAC byte can reach a log
// line. It surfaces only value-free typed errors: the wrapped Unwrap/verifyMAC
// errors are already proven secret-free (keys_test.go, verify_mac_test.go), and
// this package's own errors (ErrUnknownTag) name no value. The HANDLER logs the
// (secret-free) error with slog and shows the user the single generic string
// UserParseError ("Couldn't verify that tap — try again.", params.go) for EVERY
// non-nil error — the error types exist for internal distinction, the user
// message stays generic (§4.7). A leak test in verify_test.go fails red if any
// Verify error ever formats a key or a CMAC.

// tagStatusActive is the only tag status that proceeds to SUN verification
// (tags.status CHECK is active|retired|lost, ADR 0003 madde 5 / M1-05). retired
// and lost are §5 line-1 rejects handled before any crypto runs.
const tagStatusActive = "active"

// ErrUnknownTag reports that the tap's uid resolved to NO tag row (the resolver
// returned pgx.ErrNoRows). It is a distinct sentinel (errors.Is-matchable) so the
// caller can tell "unknown plaque" apart from an infrastructure failure. Unlike
// the other reject outcomes it is an ERROR, not a Result with SUNValid=false,
// because an unknown uid yields NO tenant and NO location: there is nothing to
// build a tenant-scoped Result around and nothing to record against (an unknown
// plaque cannot write an RLS-scoped row — §4.5). This does NOT violate §4.6's
// "records are never lost": that rule protects taps by a KNOWN employee on a
// KNOWN tag whose LOCATION evidence is weak (write + flag); a uid that matches no
// tag carries no tenant context at all, so the tap is rejected outright — exactly
// as the tap decision table treats it (reject). It is a FACT ("this uid is not
// ours"), not a verdict, in the same spirit as advance.go's ErrReplay.
var ErrUnknownTag = errors.New("sun: tap rejected: unknown tag uid")

// Result is what Verify learned about a tap's SUN — a fact, never a verdict
// (CLAUDE.md §5; the fields are exactly the m2-sun.md M2-07 contract).
type Result struct {
	// SUNValid is the proof-of-moment truth: true ONLY when the chip's CMAC
	// verified AND the monotonic counter advanced (steps 5 and 6 both passed).
	// It is false for a QR fallback (no SUN present), a retired/lost tag (short-
	// circuited before crypto), a bad CMAC, and a replay/backward/raced counter —
	// every one of §5 line 2's "SUN invalid" cases collapse here. It is NEVER a
	// verdict: the decision engine maps SUNValid plus context to ok/flag/reject.
	SUNValid bool

	// Tag is the resolved tag row (uid, tenant, location, status, wrapped key).
	// It carries the tenant the decision layer scopes everything else to, and the
	// status the decision engine reads for §5 line 1 (retired/lost -> reject). It
	// is zero only alongside a non-nil error (unknown uid / infra failure).
	Tag db.ResolvedTag

	// Location is the tapped plaque's location id (== Tag.LocationID), surfaced as
	// a first-class field because it is what the decision/geo layer reads most: it
	// is the location whose static IP / GPS / shift the tap is matched against
	// (§5 — the TAPPED location, not the employee's profile location). Callers
	// read it directly rather than reaching through Tag.
	//
	// 🔴 nil MEANS THE PLAQUE IS NOT ON A WALL, and it is a pointer for exactly the
	// reason db.ResolvedTag.LocationID is (M6-06 phase B): migration 00013 made the
	// column nullable for the inventory model, and pgx scans SQL NULL into a
	// uuid.UUID as uuid.Nil WITHOUT AN ERROR — so a flat field made a plaque sitting
	// in a box indistinguishable from one mounted at "location zero". A tap on such
	// a plaque is refused by §5 line 1 long before this value is used for anything,
	// which is why the change altered no behaviour; the type is what stops the next
	// caller from having to know that.
	Location *uuid.UUID

	// CtrGap is the number of chip reads skipped since we last saw this tag
	// (ctr - old_last_ctr - 1), returned as DATA for the base:ctr-gap-review
	// policy (M3-06, Q21) — Verify applies NO threshold. It is meaningful only
	// when SUNValid is true; it is 0 for every reject and every non-NFC path.
	CtrGap int32
}

// tagResolver is the one-consumer slice of internal/db that Verify needs,
// declared HERE at the consumer (CLAUDE.md §7) rather than depending on the whole
// *db.DB. *db.DB satisfies it directly, and a test double implementing just these
// two methods lets Verify's sequencing and error mapping be proven WITHOUT a
// database (the atomic advance itself still needs real Postgres — verify_test.go).
//
//   - GetTagByUID runs CONTEXT-LESS on the pool: the tap carries only a uid, and
//     the tenant is the RESULT of this lookup, not an input (ADR 0002 madde 7).
//   - WithTenant then runs the counter advance INSIDE the resolved tenant's
//     context, so the atomic UPDATE executes under RLS (§4.5).
type tagResolver interface {
	GetTagByUID(ctx context.Context, uid string) (db.ResolvedTag, error)
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Verifier holds Verify's injected dependencies (CLAUDE.md §7: no package-level
// singletons, no init magic). kek is the process KEK (Config.TagKEK) used to
// unwrap per-tag keys for the duration of a single verification; it is the only
// long-lived secret this package references and it is never copied elsewhere.
type Verifier struct {
	tags tagResolver
	// keks is ordered: keks[0] is the primary (Config.TagKEK) and is tried first.
	// It has more than one entry only during a KEK rotation; see UnwrapAny.
	keks [][]byte
}

// NewVerifier wires a Verifier. tags is normally *db.DB; kek is Config.TagKEK
// (32 bytes, validated by internal/config). The KEK is held by reference, exactly
// as keys.go's contract expects — the package caches no unwrapped tag key.
//
// previous is the OPTIONAL rotation fallback (Config.TagKEKPrevious). It is
// variadic so that the twenty-eight existing call sites keep compiling unchanged
// — churning them would have been twenty-eight chances to pass the wrong slice in
// a change whose whole point is that the read path must not break.
//
// EMPTY ENTRIES ARE DROPPED, and that is safe rather than lenient: an unset
// previous KEK is nil, which is the ordinary steady state, so main can pass
// cfg.TagKEKPrevious straight through without a branch. A previous KEK that is
// SET but malformed never arrives here at all — internal/config refuses to start.
func NewVerifier(tags tagResolver, kek []byte, previous ...[]byte) *Verifier {
	keks := [][]byte{kek}
	for _, p := range previous {
		if len(p) > 0 {
			keks = append(keks, p)
		}
	}
	return &Verifier{tags: tags, keks: keks}
}

// Verify runs the fixed SUN flow for an already-parsed tap (p comes from Parse at
// the handler boundary, so the domain layer sees only valid data — §7). It
// returns the Result fact and, for the two outcomes that carry no tenant/location
// to record against, an error:
//
//   - ErrUnknownTag        — uid matched no tag (reject; nothing to record).
//   - any other non-nil err — an infrastructure or programming failure to surface
//     (DB down, corrupt aes_key_ref, wrong KEK, invalid key length). NOT a normal
//     reject: a healthy tag with correct config always unwraps and MACs.
//
// Every other outcome — retired/lost tag, QR fallback, bad CMAC, replayed/backward
// counter — is a normal SUN failure returned as (Result{SUNValid:false, ...}, nil):
// the tag resolved, so the decision layer has the tenant/location/status it needs
// to record the attempt and decide (§5). Note the QR-vs-bad-CMAC distinction the
// decision engine needs (a QR tap may still be ok via IP; a bad-CMAC NFC tap is a
// hard reject) is carried by p.Channel, which the decision layer already holds —
// so it is deliberately NOT duplicated into Result.
func (v *Verifier) Verify(ctx context.Context, p Params) (Result, error) {
	// Steps 2-5 (resolve -> status -> QR -> unwrap/verifyMAC) are SHARED with
	// PreviewWithoutReplayProtection and live in inspect (preview.go). The split
	// is exactly at the point where the two callers diverge: Verify goes on to
	// step 6 and advances the counter, the preview stops here. Sharing the code
	// rather than copying it is what keeps a second, drifting SUN flow from
	// existing — the failure mode where a "lighter" path quietly skips a check.
	tag, valid, err := v.inspect(ctx, p)
	if err != nil {
		if errors.Is(err, ErrUnknownTag) {
			// Unknown uid is a reject with no tenant to record. Returned bare, so
			// callers can errors.Is it (advance.go's ErrReplay follows the same rule).
			return Result{}, ErrUnknownTag
		}
		return Result{}, fmt.Errorf("sun: verify: %w", err)
	}

	// From here we have a tenant + location, so every non-error outcome carries
	// them for the decision layer.
	res := Result{Tag: tag, Location: tag.LocationID}

	if !valid {
		// A retired/lost tag (§5 line 1), a QR URL with no SUN, or a CMAC that did
		// not verify. Return NOW, before AdvanceCounter: the counter is advanced
		// only after a verified MAC (§4.4 DoS guard), and a dead tag or a QR
		// fallback must never move it either. WithTenant is not reached on any of
		// these paths — proven by verify_test.go's order tests.
		return res, nil
	}

	// Step 6: MAC verified -> advance the monotonic counter ATOMICALLY, inside the
	// tag's tenant context so the single UPDATE runs under RLS. The strict
	// less-than guard lives in the SQL (store.AdvanceTagCounter); a replay,
	// backward counter or lost race maps to ErrReplay -> reject (SUNValid=false).
	var gap int32
	err = v.tags.WithTenant(ctx, tag.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		gap, e = AdvanceCounter(ctx, store.New(tx), tag.TenantID, tag.UID, p.Ctr)
		return e
	})
	if err != nil {
		if errors.Is(err, ErrReplay) {
			// Replay / backward / lost concurrent race: a normal SUN failure, not
			// an outage. §5 line 2 ("counter did not advance") -- reject, but recorded.
			return res, nil
		}
		return Result{}, fmt.Errorf("sun: verify: advance: %w", err)
	}

	// Step 7: CMAC genuine AND counter advanced -> the SUN is valid.
	res.SUNValid = true
	res.CtrGap = gap
	return res, nil
}

// verifySUN unwraps the tag's per-tag AES key, checks the chip's truncated CMAC
// against it, and wipes the key before returning (§4.7 — the plaintext key lives
// only for this call). It reports (true, nil) for a genuine MAC, (false, nil) for
// a cryptographic mismatch (a normal reject), and a non-nil error only for a real
// failure: a corrupt/moved aes_key_ref, the wrong KEK (both surface as a GCM auth
// error from Unwrap — never a garbage key) or an invalid key length (a §7
// programming error from verifyMAC). Those are surfaced, not swallowed. It never
// logs and its errors carry no key or CMAC bytes (Unwrap/verifyMAC guarantee it).
//
// The AAD passed to Unwrap is p.UIDBytes — the same raw uid the tag was resolved
// by — so for any tag that resolved, the AAD matches and Unwrap can fail ONLY on
// our own corrupt storage or a wrong KEK, i.e. a server problem, never the
// tapper's fault (ADR 0003 madde 4, the portability guard).
func (v *Verifier) verifySUN(tag db.ResolvedTag, p Params) (bool, error) {
	// UnwrapAny, not Unwrap: during a KEK rotation the park is mixed and a row may
	// still be sealed under the previous KEK. With one KEK configured this is
	// Unwrap with one extra slice bounds check.
	key, err := UnwrapAny(v.keks, p.UIDBytes, tag.AESKeyRef)
	if err != nil {
		return false, fmt.Errorf("unwrap tag key: %w", err)
	}
	// Wipe the plaintext key as soon as verification returns — no cached key
	// (§4.7, keys.go Zero). defer runs after verifyMAC has read it.
	defer Zero(key)

	ok, err := verifyMAC(key, p.UIDBytes, p.CtrBytes[:], p.CMAC)
	if err != nil {
		return false, fmt.Errorf("verify mac: %w", err)
	}
	return ok, nil
}
