package sun

import (
	"context"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/atknatk/tappa/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Tests for PreviewWithoutReplayProtection (M5-04). The ONE property that
// matters more than all the others: this path must never advance the chip's
// counter, because the tap page runs on it and an employee who opens the page
// without pressing the button must not burn a counter value.
//
// "Must never advance" is proven TWO ways, because one of them alone would be a
// claim rather than a measurement:
//
//   - WITH A COUNTING FAKE, at the level of the actual SQL: the fake's
//     WithTenant genuinely INVOKES the callback with a transaction stub, so if
//     the code under test called AdvanceCounter the advance query would be
//     issued and counted. (verify_test.go's fakeResolver deliberately does NOT
//     invoke the callback, so on its own it can only show that WithTenant was
//     not reached — a weaker statement.)
//   - AGAINST REAL POSTGRES: open the page N times and read tags.last_ctr back
//     unchanged, then call Verify and watch it move. The second half is the
//     positive control — without it, "the counter did not move" would also be
//     true of a test that measured nothing.

// ---------------------------------------------------------------------------
// A transaction stub that counts the statements issued through it.
// ---------------------------------------------------------------------------

// countingTx implements pgx.Tx by embedding the interface: every method it does
// not override is nil and would panic if called, which is exactly the behaviour
// wanted — an unexpected database call fails loudly instead of passing quietly.
// Only QueryRow is real, because that is the single method
// store.AdvanceTagCounter uses.
type countingTx struct {
	pgx.Tx
	queries *int32
}

func (c countingTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	atomic.AddInt32(c.queries, 1)
	return noRow{}
}

// noRow is a pgx.Row that reports "no rows", which AdvanceCounter maps to
// ErrReplay. The mapping is irrelevant here; what matters is that Scan is
// reachable at all, i.e. that a query was really issued.
type noRow struct{}

func (noRow) Scan(_ ...any) error { return pgx.ErrNoRows }

// spyResolver is a tagResolver whose WithTenant ACTUALLY RUNS the callback, so
// any counter advance inside it reaches countingTx and is counted.
type spyResolver struct {
	tag        db.ResolvedTag
	resolveErr error
	withTenant int32
	queries    int32
}

func (s *spyResolver) GetTagByUID(_ context.Context, _ string) (db.ResolvedTag, error) {
	if s.resolveErr != nil {
		return db.ResolvedTag{}, s.resolveErr
	}
	return s.tag, nil
}

func (s *spyResolver) WithTenant(ctx context.Context, _ uuid.UUID, fn db.TxFunc) error {
	atomic.AddInt32(&s.withTenant, 1)
	return fn(ctx, countingTx{queries: &s.queries})
}

// previewFixture builds a verifier over a spyResolver holding one tag whose
// wrapped key is genuinely tagKey under kek, plus a valid CMAC for that tag.
func previewFixture(t *testing.T, status string) (*Verifier, *spyResolver, Params) {
	t.Helper()
	kek := hexBytes(t, fakeKEKHex)
	uidBytes := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeTagKey)
	mac := referenceMAC(t, key, uidBytes, hexBytes(t, fakeCtrHex))
	s := &spyResolver{tag: fakeActiveTag(t, kek, uidBytes, key, status)}
	return NewVerifier(s, kek), s, mustParse(t, fakeUIDHex, fakeCtrHex, hex.EncodeToString(mac[:]))
}

// ---------------------------------------------------------------------------
// THE core property.
// ---------------------------------------------------------------------------

// TestPreview_NeverIssuesTheAdvanceQuery covers every shape of tap the page can
// be opened with — including the one that WOULD advance under Verify (a valid
// CMAC on an active tag). Zero WithTenant calls, zero statements.
func TestPreview_NeverIssuesTheAdvanceQuery(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uidBytes := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeTagKey)
	goodMAC := hex.EncodeToString(func() []byte { m := referenceMAC(t, key, uidBytes, hexBytes(t, fakeCtrHex)); return m[:] }())

	tests := []struct {
		name          string
		status        string
		ctrHex, cmac  string
		wantCMACValid bool
	}{
		{"valid cmac on an active tag", "active", fakeCtrHex, goodMAC, true},
		{"bad cmac", "active", fakeCtrHex, "0000000000000000", false},
		{"retired tag", "retired", fakeCtrHex, goodMAC, false},
		{"lost tag", "lost", fakeCtrHex, goodMAC, false},
		{"qr url with no sun", "active", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &spyResolver{tag: fakeActiveTag(t, kek, uidBytes, key, tc.status)}
			pv, err := NewVerifier(s, kek).PreviewWithoutReplayProtection(
				context.Background(), mustParse(t, fakeUIDHex, tc.ctrHex, tc.cmac))
			if err != nil {
				t.Fatalf("preview err = %v, want nil", err)
			}
			if pv.CMACValid != tc.wantCMACValid {
				t.Fatalf("CMACValid = %v, want %v", pv.CMACValid, tc.wantCMACValid)
			}
			if got := atomic.LoadInt32(&s.withTenant); got != 0 {
				t.Fatalf("WithTenant called %d times, want 0 — the page must not open a tenant transaction", got)
			}
			if got := atomic.LoadInt32(&s.queries); got != 0 {
				t.Fatalf("%d statement(s) issued, want 0 — the counter must not be advanced while the page opens (§4.4, M5-04)", got)
			}
		})
	}
}

// TestVerify_StillIssuesTheAdvanceQuery is the POSITIVE CONTROL for the test
// above and the REGRESSION GUARD for the inspect() refactor: the same fake, the
// same tap, through Verify — exactly one statement, which is the advance.
// Without this, "zero statements" would also be true if the fake were simply
// never wired up.
func TestVerify_StillIssuesTheAdvanceQuery(t *testing.T) {
	ver, s, p := previewFixture(t, "active")

	if _, err := ver.Verify(context.Background(), p); err != nil {
		t.Fatalf("Verify err = %v, want nil (the stub reports no rows -> ErrReplay -> reject, not an error)", err)
	}
	if got := atomic.LoadInt32(&s.withTenant); got != 1 {
		t.Fatalf("WithTenant called %d times, want 1 — Verify must still advance the counter", got)
	}
	if got := atomic.LoadInt32(&s.queries); got != 1 {
		t.Fatalf("%d statement(s) issued, want 1 — Verify must still issue the atomic advance (§4.4)", got)
	}
}

// ---------------------------------------------------------------------------
// The type itself is part of the defence.
// ---------------------------------------------------------------------------

// TestPreview_CarriesNothingAReplayCheckCouldBeBuiltFrom pins measure 3 from
// preview.go, in the wider form an audit forced.
//
// The first version of this test only looked for a SUNValid field, and that was
// not enough: Preview carried the whole db.ResolvedTag, so last_ctr was right
// there and `pv.CMACValid && p.Ctr > pv.Tag.LastCtr` was a sentence a caller
// could write — a read-then-compare replay check in Go, which is exactly the
// TOCTOU §4.4 forbids. The field a caller must not have is not only the
// CONCLUSION (SUNValid) but the INGREDIENT (a counter). It also handed an HTTP
// handler aes_key_ref, the KEK-wrapped per-tag key, for no reason at all (§4.7).
//
// So this asserts the whole surface: exactly four fields, each one named, and
// nothing that is a counter, a byte blob or an embedded row.
func TestPreview_CarriesNothingAReplayCheckCouldBeBuiltFrom(t *testing.T) {
	pt := reflect.TypeOf(Preview{})

	allowed := map[string]reflect.Kind{
		"CMACValid": reflect.Bool,
		"TenantID":  reflect.Array, // uuid.UUID
		"TagStatus": reflect.String,
		"Location":  reflect.Array, // uuid.UUID
	}
	if pt.NumField() != len(allowed) {
		t.Fatalf("Preview has %d fields, want exactly %d — a new field on this type "+
			"is how a replay check gets its ingredients back", pt.NumField(), len(allowed))
	}
	uuidType := reflect.TypeOf(uuid.UUID{})
	for i := 0; i < pt.NumField(); i++ {
		f := pt.Field(i)
		want, ok := allowed[f.Name]
		if !ok {
			t.Fatalf("Preview has an unexpected field %q (%s): every field here is a "+
				"deliberate decision, so an addition must be argued for, not inherited", f.Name, f.Type)
		}
		if f.Type.Kind() != want {
			t.Fatalf("Preview.%s is %s, want %s", f.Name, f.Type.Kind(), want)
		}
		// The two named counter-ish spellings, and any raw byte carrier.
		low := strings.ToLower(f.Name)
		if strings.Contains(low, "ctr") || strings.Contains(low, "counter") {
			t.Fatalf("Preview.%s looks like a counter: this path never checks one, "+
				"and handing a caller last_ctr invites the Go-side comparison §4.4 forbids", f.Name)
		}
		if strings.EqualFold(f.Name, "SUNValid") {
			t.Fatalf("Preview has a field %q: this path does NOT establish SUN validity", f.Name)
		}
		if f.Type != uuidType && (f.Type.Kind() == reflect.Slice || f.Type.Kind() == reflect.Array) {
			t.Fatalf("Preview.%s is a raw byte field: aes_key_ref used to reach the "+
				"HTTP layer through exactly such a field (§4.7)", f.Name)
		}
		if f.Type == reflect.TypeOf(db.ResolvedTag{}) {
			t.Fatalf("Preview.%s embeds the whole tag row again", f.Name)
		}
	}

	// And the two results are not interchangeable, so code written for Verify
	// cannot be pointed at the preview without being rewritten.
	if reflect.TypeOf(Preview{}).AssignableTo(reflect.TypeOf(Result{})) {
		t.Fatal("Preview is assignable to Result: the type difference is measure 2 in preview.go")
	}
}

// ---------------------------------------------------------------------------
// Error mapping mirrors Verify's, so a caller cannot be surprised.
// ---------------------------------------------------------------------------

func TestPreview_UnknownUIDReturnsErrUnknownTag(t *testing.T) {
	s := &spyResolver{resolveErr: pgx.ErrNoRows}
	_, err := NewVerifier(s, hexBytes(t, fakeKEKHex)).PreviewWithoutReplayProtection(
		context.Background(), mustParse(t, fakeUIDHex, fakeCtrHex, "83c0104d8ac39077"))
	if !errors.Is(err, ErrUnknownTag) {
		t.Fatalf("err = %v, want ErrUnknownTag", err)
	}
	if atomic.LoadInt32(&s.withTenant) != 0 {
		t.Fatal("an unknown tag reached a tenant transaction")
	}
}

func TestPreview_InfraErrorIsNotAnUnknownTag(t *testing.T) {
	boom := errors.New("connection reset by peer")
	_, err := NewVerifier(&spyResolver{resolveErr: boom}, hexBytes(t, fakeKEKHex)).
		PreviewWithoutReplayProtection(context.Background(), mustParse(t, fakeUIDHex, fakeCtrHex, "83c0104d8ac39077"))
	if errors.Is(err, ErrUnknownTag) {
		t.Fatal("an outage was reported as ErrUnknownTag (a page that says 'unknown plaque' during an outage sends the employee to their manager for nothing)")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the underlying failure", err)
	}
}

// TestPreview_CarriesTenantAndLocation pins the N5 hand-off input (state.md):
// the page must be able to carry the TAG's tenant forward so M5-05 can feed
// sys:tenant-mismatch. Carrying is all that is claimed — no comparison happens
// here or on the page.
func TestPreview_CarriesTenantAndLocation(t *testing.T) {
	ver, s, p := previewFixture(t, "active")

	pv, err := ver.PreviewWithoutReplayProtection(context.Background(), p)
	if err != nil {
		t.Fatalf("preview err = %v", err)
	}
	if pv.TenantID != s.tag.TenantID || pv.TenantID == uuid.Nil {
		t.Fatalf("Preview.TenantID = %s, want %s", pv.TenantID, s.tag.TenantID)
	}
	if pv.Location != s.tag.LocationID {
		t.Fatalf("Preview.Location = %s, want %s", pv.Location, s.tag.LocationID)
	}
	if pv.TagStatus != "active" {
		t.Fatalf("Preview.TagStatus = %q, want it carried for §5 line 1", pv.TagStatus)
	}
}

// TestPreview_NoSecretInErrors: §4.7. The preview shares Verify's crypto step,
// so it inherits the same obligation — no key, KEK or CMAC byte in any error.
func TestPreview_NoSecretInErrors(t *testing.T) {
	rightKEK := hexBytes(t, fakeKEKHex)
	wrongKEK := hexBytes(t, "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100")
	uidBytes := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeTagKey)
	mac := referenceMAC(t, key, uidBytes, hexBytes(t, fakeCtrHex))
	p := mustParse(t, fakeUIDHex, fakeCtrHex, hex.EncodeToString(mac[:]))

	s := &spyResolver{tag: fakeActiveTag(t, rightKEK, uidBytes, key, "active")}
	_, err := NewVerifier(s, wrongKEK).PreviewWithoutReplayProtection(context.Background(), p)
	if err == nil {
		t.Fatal("a wrong KEK must surface as an error, not as a quiet reject")
	}
	msg := err.Error()
	for name, secret := range map[string][]byte{"tag key": key, "KEK": rightKEK, "CMAC": mac[:]} {
		if strings.Contains(msg, string(secret)) {
			t.Fatalf("error leaks raw %s bytes: %q", name, msg)
		}
		h := hex.EncodeToString(secret)
		if strings.Contains(msg, h) || strings.Contains(msg, strings.ToUpper(h)) {
			t.Fatalf("error leaks hex %s: %q", name, msg)
		}
	}
}

// ---------------------------------------------------------------------------
// Real Postgres: opening the page N times does not move tags.last_ctr, and
// Verify then does. Skips without DATABASE_URL (see appDB).
// ---------------------------------------------------------------------------

func TestPreview_AgainstPostgresDoesNotMoveTheCounter(t *testing.T) {
	d := appDB(t)
	kek := hexBytes(t, fakeKEKHex)
	keyA := hexBytes(t, fakeTagKey)
	ver := NewVerifier(d, kek)
	ctx := context.Background()

	const startCtr = 500
	_, locationID, uid := newVerifyTag(t, d, kek, keyA, startCtr, "active")
	ctrBytes := hexBytes(t, "0001f5") // 501 = start+1, a genuine next tap
	mac := referenceMAC(t, keyA, hexBytes(t, uid), ctrBytes)
	p := mustParse(t, uid, "0001f5", hex.EncodeToString(mac[:]))

	// Open the page twenty times, exactly as twenty curious employees would.
	const opens = 20
	for i := 0; i < opens; i++ {
		pv, err := ver.PreviewWithoutReplayProtection(ctx, p)
		if err != nil {
			t.Fatalf("open %d: preview err = %v", i, err)
		}
		if !pv.CMACValid {
			t.Fatalf("open %d: CMACValid = false for a genuine CMAC", i)
		}
		if pv.Location != locationID {
			t.Fatalf("open %d: Location = %s, want %s", i, pv.Location, locationID)
		}
	}
	if got := finalCtr(t, d, uid); got != startCtr {
		t.Fatalf("last_ctr = %d after %d page opens, want %d UNCHANGED — the page spent the chip's counter (§4.4, M5-04)",
			got, opens, startCtr)
	}

	// POSITIVE CONTROL: the same tap through Verify DOES advance. Without this,
	// the assertion above would also pass against a preview that did nothing at
	// all, or against a tag nobody could advance.
	res, err := ver.Verify(ctx, p)
	if err != nil {
		t.Fatalf("Verify err = %v", err)
	}
	if !res.SUNValid {
		t.Fatal("Verify returned SUNValid=false for a tap the preview accepted and the counter allows")
	}
	if got := finalCtr(t, d, uid); got != startCtr+1 {
		t.Fatalf("last_ctr = %d after Verify, want %d — the positive control did not move the counter, so the test above proves nothing",
			got, startCtr+1)
	}

	// And the preview STILL passes on a URL that is now a replay: it cannot tell
	// the difference, which is exactly what its name says and why POST must
	// advance rather than trust it.
	pv, err := ver.PreviewWithoutReplayProtection(ctx, p)
	if err != nil {
		t.Fatalf("preview after replay err = %v", err)
	}
	if !pv.CMACValid {
		t.Fatal("preview rejected a replayed URL: it has no counter check, so this would mean it grew one silently")
	}
	if got := finalCtr(t, d, uid); got != startCtr+1 {
		t.Fatalf("last_ctr = %d after a replayed preview, want %d unchanged", got, startCtr+1)
	}
}
