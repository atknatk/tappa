package sun

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Tests for the SUN Verify entry point (M2-07). They prove the fixed flow
// (resolve -> status -> QR -> unwrap/verifyMAC -> advance), the load-bearing
// verify-BEFORE-advance order, the whole tappa-sun case table, that Verify
// returns a FACT and never a verdict, and §4.7 (no secret in any error).
//
// SCOPE OF ASSURANCE — the SAME ceiling as verify_mac_test.go and the JSON's
// _warning. Every "valid" CMAC used below (frozen in sun_vectors.json or recomputed
// via referenceMAC) is SELF-CONSISTENT: minted by Tappa's own sv2+cmac+truncate
// chain with FAKE keys, NOT captured from a real NTAG 424 DNA chip. These vectors
// prove the wiring, the compare/truncation, the replay ordering and the reject
// paths, and pin them against regressions. They do NOT externally validate the
// absolute byte/endian order real silicon emits, nor the full chain against a real
// chip. A real-chip known-answer vector is REQUIRED before the M8 pilot (M8-05
// encode runbook). A green run here is NOT proof of real-chip correctness.

// ---------------------------------------------------------------------------
// Vector fixture loading.
// ---------------------------------------------------------------------------

// vectorsPath is the fixture relative to this package's test working directory
// (internal/sun -> repo-root/test/fixtures).
const vectorsPath = "../../test/fixtures/sun_vectors.json"

type vectorsFile struct {
	KEK          string            `json:"kek"`
	TagKeyA      string            `json:"tag_key_A"`
	TagKeyBWrong string            `json:"tag_key_B_wrong"`
	Vectors      []vector          `json:"vectors"`
	Malformed    []malformedVector `json:"malformed"`
}

type vector struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	UID          string `json:"uid"`
	TagKey       string `json:"tag_key"`
	LastCtr      int32  `json:"last_ctr"`
	CtrHex       string `json:"ctr_hex"`
	CMAC         string `json:"cmac"`
	CMACVerifies bool   `json:"cmac_verifies"`
	WantSUNValid bool   `json:"want_sun_valid"`
	WantGap      int32  `json:"want_gap"`
}

type malformedVector struct {
	Name  string `json:"name"`
	Query string `json:"query"`
}

func loadVectors(t *testing.T) vectorsFile {
	t.Helper()
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatalf("read %s: %v", vectorsPath, err)
	}
	var vf vectorsFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatalf("parse %s: %v", vectorsPath, err)
	}
	if len(vf.Vectors) == 0 || len(vf.Malformed) == 0 {
		t.Fatalf("vectors file looks empty: %d vectors, %d malformed", len(vf.Vectors), len(vf.Malformed))
	}
	return vf
}

// ---------------------------------------------------------------------------
// Non-DB: the JSON's frozen CMACs are exactly what the pipeline produces.
// This freezes the known-answer values against a regression WITHOUT a database:
// if SV2 layout, the empty MAC input, session-key derivation or truncation ever
// drift, a frozen cmac stops matching cmac_verifies and this turns red (a
// self-consistent recompute-and-compare would silently move with the bug).
// ---------------------------------------------------------------------------

func TestVerify_VectorsSelfConsistent(t *testing.T) {
	vf := loadVectors(t)
	for _, vec := range vf.Vectors {
		t.Run(vec.Name, func(t *testing.T) {
			key := hexBytes(t, vec.TagKey) // every tag holds tag_key_A
			uid := hexBytes(t, vec.UID)
			ctr := hexBytes(t, vec.CtrHex)
			cmac := hexBytes(t, vec.CMAC)

			ok, err := verifyMAC(key, uid, ctr, cmac)
			if err != nil {
				t.Fatalf("verifyMAC error: %v", err)
			}
			if ok != vec.CMACVerifies {
				t.Fatalf("frozen cmac %s: verifyMAC = %v, want cmac_verifies=%v "+
					"(regenerate the vector only if the algorithm intentionally changed)",
					vec.CMAC, ok, vec.CMACVerifies)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Non-DB: malformed / wrong-length / bad-hex URLs reject at Parse with a typed
// *ParseError and never panic (tappa-sun case table row "eksik/bozuk hex").
// ---------------------------------------------------------------------------

func TestVerify_MalformedInputRejected(t *testing.T) {
	vf := loadVectors(t)
	for _, m := range vf.Malformed {
		t.Run(m.Name, func(t *testing.T) {
			q, err := url.ParseQuery(m.Query)
			if err != nil {
				t.Fatalf("ParseQuery(%q): %v", m.Query, err)
			}
			// Recover proves the "no panic on hostile input" guarantee.
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse panicked on %q: %v", m.Query, r)
				}
			}()
			p, perr := Parse(q)
			if perr == nil {
				t.Fatalf("malformed %q parsed OK (channel=%s), want *ParseError", m.Query, p.Channel)
			}
			var pe *ParseError
			if !errors.As(perr, &pe) {
				t.Fatalf("error type = %T, want *ParseError", perr)
			}
			if pe.UserMessage() != UserParseError {
				t.Fatalf("user message = %q, want generic %q", pe.UserMessage(), UserParseError)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Non-DB unit tests via a fake resolver: Verify's sequencing and error mapping
// are proven without Postgres. The atomic advance itself needs real Postgres and
// is proven further down; here the fake's WithTenant records whether it was
// reached and returns a canned outcome WITHOUT running fn -- which is exactly what
// lets the verify-BEFORE-advance ORDER be proven at unit level (a bad CMAC must
// never reach WithTenant).
// ---------------------------------------------------------------------------

type fakeResolver struct {
	tag        db.ResolvedTag
	resolveErr error
	wtCalls    int32
	wtErr      error
}

func (f *fakeResolver) GetTagByUID(_ context.Context, _ string) (db.ResolvedTag, error) {
	if f.resolveErr != nil {
		return db.ResolvedTag{}, f.resolveErr
	}
	return f.tag, nil
}

// WithTenant records that step 6 was reached and returns a canned outcome. It
// deliberately does NOT invoke fn: these unit tests assert reachability and
// error mapping, not the atomic UPDATE (which the DB tests exercise for real).
func (f *fakeResolver) WithTenant(_ context.Context, _ uuid.UUID, _ db.TxFunc) error {
	atomic.AddInt32(&f.wtCalls, 1)
	return f.wtErr
}

// fakeActiveTag builds an active ResolvedTag whose aes_key_ref genuinely wraps
// tagKey under kek, so verifySUN's Unwrap succeeds and the flow reaches verifyMAC.
func fakeActiveTag(t *testing.T, kek, uidBytes, tagKey []byte, status string) db.ResolvedTag {
	t.Helper()
	ref, err := Wrap(kek, uidBytes, tagKey)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	return db.ResolvedTag{
		UID:        strings.ToUpper(hex.EncodeToString(uidBytes)),
		TenantID:   uuid.New(),
		LocationID: uuid.New(),
		AESKeyRef:  ref,
		LastCtr:    0,
		Status:     status,
	}
}

func TestVerify_UnknownUIDReturnsErrUnknownTag(t *testing.T) {
	f := &fakeResolver{resolveErr: pgx.ErrNoRows}
	ver := NewVerifier(f, hexBytes(t, fakeKEKHex))

	_, err := ver.Verify(context.Background(), mustParse(t, fakeUIDHex, "000065", "83c0104d8ac39077"))
	if !errors.Is(err, ErrUnknownTag) {
		t.Fatalf("err = %v, want ErrUnknownTag", err)
	}
	if f.wtCalls != 0 {
		t.Fatal("advance reached for an unknown tag")
	}
}

func TestVerify_ResolveInfraErrorSurfaced(t *testing.T) {
	boom := errors.New("connection reset by peer")
	f := &fakeResolver{resolveErr: boom}
	ver := NewVerifier(f, hexBytes(t, fakeKEKHex))

	_, err := ver.Verify(context.Background(), mustParse(t, fakeUIDHex, "000065", "83c0104d8ac39077"))
	if errors.Is(err, ErrUnknownTag) {
		t.Fatal("an infra failure must not be reported as ErrUnknownTag (outage != unknown tag)")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap the underlying failure", err)
	}
}

// TestVerify_RetiredAndLostShortCircuit: §5 line 1. A dead tag rejects
// (SUNValid=false) BEFORE any crypto and WITHOUT reaching advance -- proven by
// wtCalls==0 even though the tag carries a real wrapped key.
func TestVerify_RetiredAndLostShortCircuit(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uidBytes := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeTagKey)
	for _, status := range []string{"retired", "lost"} {
		t.Run(status, func(t *testing.T) {
			f := &fakeResolver{tag: fakeActiveTag(t, kek, uidBytes, key, status)}
			ver := NewVerifier(f, kek)

			res, err := ver.Verify(context.Background(), mustParse(t, fakeUIDHex, "000065", "83c0104d8ac39077"))
			if err != nil {
				t.Fatalf("Verify err = %v, want nil (dead tag is a Result reject)", err)
			}
			if res.SUNValid {
				t.Fatalf("%s tag returned SUNValid=true, want false", status)
			}
			if res.Tag.Status != status {
				t.Fatalf("Result.Tag.Status = %q, want %q (carried for §5 line 1)", res.Tag.Status, status)
			}
			if f.wtCalls != 0 {
				t.Fatalf("%s tag reached advance (wtCalls=%d), want 0 -- dead tag must not touch the counter", status, f.wtCalls)
			}
		})
	}
}

// TestVerify_QRHasNoSUN: §5 QR channel. No ctr/cmac -> SUNValid=false, no crypto,
// no advance. It is a valid Result, not an error.
func TestVerify_QRHasNoSUN(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uidBytes := hexBytes(t, fakeUIDHex)
	f := &fakeResolver{tag: fakeActiveTag(t, kek, uidBytes, hexBytes(t, fakeTagKey), "active")}
	ver := NewVerifier(f, kek)

	p := mustParse(t, fakeUIDHex, "", "") // QR: tag only
	if p.HasSUN() {
		t.Fatal("QR params report HasSUN()=true")
	}
	res, err := ver.Verify(context.Background(), p)
	if err != nil {
		t.Fatalf("Verify err = %v, want nil", err)
	}
	if res.SUNValid {
		t.Fatal("QR tap returned SUNValid=true, want false")
	}
	if res.Location != f.tag.LocationID {
		t.Fatal("QR Result must still carry the tag's location for the decision layer")
	}
	if f.wtCalls != 0 {
		t.Fatal("QR tap reached advance, want 0")
	}
}

// TestVerify_BadCMACDoesNotAdvance is THE unit-level ORDER PROOF (§4.4): an
// invalid CMAC on an ACTIVE tag rejects (SUNValid=false) and NEVER reaches
// WithTenant/advance. Reversing the order would let invalid-CMAC requests push
// last_ctr forward and DoS legitimate taps.
func TestVerify_BadCMACDoesNotAdvance(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uidBytes := hexBytes(t, fakeUIDHex)
	f := &fakeResolver{tag: fakeActiveTag(t, kek, uidBytes, hexBytes(t, fakeTagKey), "active")}
	ver := NewVerifier(f, kek)

	// A CMAC that cannot verify under the tag's key (all-zero 8 bytes).
	res, err := ver.Verify(context.Background(), mustParse(t, fakeUIDHex, "000065", "0000000000000000"))
	if err != nil {
		t.Fatalf("Verify err = %v, want nil (bad CMAC is a Result reject)", err)
	}
	if res.SUNValid {
		t.Fatal("bad CMAC returned SUNValid=true, want false")
	}
	if f.wtCalls != 0 {
		t.Fatalf("bad CMAC reached advance (wtCalls=%d), want 0 -- verify MUST precede advance", f.wtCalls)
	}
}

// TestVerify_AdvanceOutcomeMapping: with a genuine CMAC the flow reaches step 6,
// and WithTenant's outcome maps correctly -- nil -> SUNValid=true; ErrReplay ->
// SUNValid=false (a recorded reject, not an error); any other error -> surfaced.
func TestVerify_AdvanceOutcomeMapping(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	uidBytes := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeTagKey)
	ctr := hexBytes(t, fakeCtrHex)
	mac := referenceMAC(t, key, uidBytes, ctr) // genuine CMAC
	goodP := mustParse(t, fakeUIDHex, fakeCtrHex, hex.EncodeToString(mac[:]))

	t.Run("nil_advance_is_sun_valid", func(t *testing.T) {
		f := &fakeResolver{tag: fakeActiveTag(t, kek, uidBytes, key, "active"), wtErr: nil}
		ver := NewVerifier(f, kek)
		res, err := ver.Verify(context.Background(), goodP)
		if err != nil {
			t.Fatalf("Verify err = %v, want nil", err)
		}
		if !res.SUNValid {
			t.Fatal("genuine CMAC + advanced counter, want SUNValid=true")
		}
		if f.wtCalls != 1 {
			t.Fatalf("wtCalls = %d, want exactly 1 (advance reached once)", f.wtCalls)
		}
	})

	t.Run("replay_maps_to_reject", func(t *testing.T) {
		f := &fakeResolver{tag: fakeActiveTag(t, kek, uidBytes, key, "active"), wtErr: ErrReplay}
		ver := NewVerifier(f, kek)
		res, err := ver.Verify(context.Background(), goodP)
		if err != nil {
			t.Fatalf("Verify err = %v, want nil (replay is a Result reject)", err)
		}
		if res.SUNValid {
			t.Fatal("replay returned SUNValid=true, want false")
		}
	})

	t.Run("infra_error_surfaced", func(t *testing.T) {
		boom := errors.New("deadlock detected")
		f := &fakeResolver{tag: fakeActiveTag(t, kek, uidBytes, key, "active"), wtErr: boom}
		ver := NewVerifier(f, kek)
		_, err := ver.Verify(context.Background(), goodP)
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want the wrapped infra failure", err)
		}
		if errors.Is(err, ErrReplay) {
			t.Fatal("an infra failure must not be reported as a replay reject")
		}
	})
}

// TestVerify_UnwrapFailureSurfaced: a wrong process KEK (or corrupt aes_key_ref)
// is a server-side problem, not a normal reject -- Verify surfaces it as an error
// (not SUNValid=false), so ops can see corrupt key storage.
func TestVerify_UnwrapFailureSurfaced(t *testing.T) {
	rightKEK := hexBytes(t, fakeKEKHex)
	wrongKEK := hexBytes(t, fakeKEK2Hex)
	uidBytes := hexBytes(t, fakeUIDHex)
	// Tag wrapped under the RIGHT kek, but the Verifier holds the WRONG one.
	f := &fakeResolver{tag: fakeActiveTag(t, rightKEK, uidBytes, hexBytes(t, fakeTagKey), "active")}
	ver := NewVerifier(f, wrongKEK)

	_, err := ver.Verify(context.Background(), mustParse(t, fakeUIDHex, "000065", "83c0104d8ac39077"))
	if err == nil {
		t.Fatal("wrong KEK must surface an error, not a silent reject")
	}
	if errors.Is(err, ErrReplay) || errors.Is(err, ErrUnknownTag) {
		t.Fatalf("unwrap failure mis-mapped to a reject sentinel: %v", err)
	}
	if f.wtCalls != 0 {
		t.Fatal("advance reached despite an unwrap failure")
	}
}

// TestVerify_NoSecretInErrors is the §4.7 acceptance + MUTATION test: no Verify
// error may contain the plaintext tag key, the KEK, or the chip CMAC (raw bytes
// or hex, either case). If anyone changes Verify to format a key/CMAC into an
// error (e.g. fmt.Errorf("...%x", key)), an assertion below turns red.
func TestVerify_NoSecretInErrors(t *testing.T) {
	rightKEK := hexBytes(t, fakeKEKHex)
	wrongKEK := hexBytes(t, fakeKEK2Hex)
	uidBytes := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeTagKey)
	cmac := hexBytes(t, "83c0104d8ac39077")

	var errs []error
	// (1) wrong KEK -> unwrap error path.
	f1 := &fakeResolver{tag: fakeActiveTag(t, rightKEK, uidBytes, key, "active")}
	if _, e := NewVerifier(f1, wrongKEK).Verify(context.Background(), mustParse(t, fakeUIDHex, "000065", "83c0104d8ac39077")); e != nil {
		errs = append(errs, e)
	}
	// (2) corrupt aes_key_ref -> unwrap error path.
	corrupt := fakeActiveTag(t, rightKEK, uidBytes, key, "active")
	corrupt.AESKeyRef[gcmNonceLen] ^= 0x01
	f2 := &fakeResolver{tag: corrupt}
	if _, e := NewVerifier(f2, rightKEK).Verify(context.Background(), mustParse(t, fakeUIDHex, "000065", "83c0104d8ac39077")); e != nil {
		errs = append(errs, e)
	}
	// (3) unknown tag + (4) infra error -> sentinel/ wrapped error paths.
	if _, e := NewVerifier(&fakeResolver{resolveErr: pgx.ErrNoRows}, rightKEK).Verify(context.Background(), mustParse(t, fakeUIDHex, "000065", "83c0104d8ac39077")); e != nil {
		errs = append(errs, e)
	}
	if _, e := NewVerifier(&fakeResolver{resolveErr: errors.New("boom")}, rightKEK).Verify(context.Background(), mustParse(t, fakeUIDHex, "000065", "83c0104d8ac39077")); e != nil {
		errs = append(errs, e)
	}

	if len(errs) < 3 {
		t.Fatalf("expected several error paths to exercise, got %d", len(errs))
	}
	secrets := map[string][]byte{"tag key": key, "KEK": rightKEK, "CMAC": cmac}
	for i, e := range errs {
		msg := e.Error()
		for name, secret := range secrets {
			if strings.Contains(msg, string(secret)) {
				t.Fatalf("error %d leaks raw %s bytes: %q", i, name, msg)
			}
			h := hex.EncodeToString(secret)
			if strings.Contains(msg, h) || strings.Contains(msg, strings.ToUpper(h)) {
				t.Fatalf("error %d leaks hex %s: %q", i, name, msg)
			}
		}
	}
}

// mustParse builds Params through the real Parse (the handler-boundary path), so
// UID canonicalisation and the ctr/cmac shaping match production. Empty ctr/cmac
// yields a QR Params.
func mustParse(t *testing.T, uid, ctrHex, cmacHex string) Params {
	t.Helper()
	q := url.Values{}
	q.Set("tag", uid)
	if ctrHex != "" {
		q.Set("ctr", ctrHex)
	}
	if cmacHex != "" {
		q.Set("cmac", cmacHex)
	}
	p, err := Parse(q)
	if err != nil {
		t.Fatalf("Parse(tag=%s ctr=%s cmac=%s): %v", uid, ctrHex, cmacHex, err)
	}
	return p
}

// ---------------------------------------------------------------------------
// Real-Postgres proof (CLAUDE.md §8, Q04): resolve + atomic advance cannot be
// proven against a fake DB. These connect as tappa_app via DATABASE_URL (RLS in
// force) using appDB/randUID/finalCtr from advance_test.go, and skip when it is
// unset. Each test provisions its own tenant/location/tag with a REAL KEK-wrapped
// key so the full pipeline runs. UIDs are random + UPPERCASE (matching Parse's
// canonical form and tags.uid) so runs never collide (tappa_app cannot DELETE
// tags -- redline R5).
// ---------------------------------------------------------------------------

// newVerifyTag commits a fresh tenant + location + one tag holding tagKey wrapped
// under kek, with the given starting last_ctr and status.
func newVerifyTag(t *testing.T, d *db.DB, kek, tagKey []byte, startCtr int32, status string) (tenantID, locationID uuid.UUID, uid string) {
	t.Helper()
	tenantID = uuid.New()
	locationID = uuid.New()
	uid = strings.ToUpper(randUID(t)) // canonical uppercase, matches Parse + tags.uid
	ref, err := Wrap(kek, hexBytes(t, uid), tagKey)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	err = d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'sun-verify-test', $2, 'bar', 'single')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, gps_lat, gps_lng)
			 VALUES ($1, $2, 'loc', 35.899, 14.514)`,
			locationID, tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			uid, tenantID, locationID, ref, startCtr, status)
		return e
	})
	if err != nil {
		t.Fatalf("newVerifyTag: %v", err)
	}
	return tenantID, locationID, uid
}

// TestVerify_VectorsAgainstPostgres runs the whole tappa-sun case table through
// the real Verify against Postgres. Each vector gets its own random-UID tag; the
// CMAC is recomputed for that UID via the same pipeline (self-consistent, per the
// file note). Accepts advance last_ctr and return the gap; every reject leaves
// last_ctr UNCHANGED -- the end-to-end proof that no reject (bad key, bit flip,
// replay, backward) ever advances the counter.
func TestVerify_VectorsAgainstPostgres(t *testing.T) {
	d := appDB(t)
	vf := loadVectors(t)
	kek := hexBytes(t, vf.KEK)
	keyA := hexBytes(t, vf.TagKeyA)
	keyB := hexBytes(t, vf.TagKeyBWrong)
	ver := NewVerifier(d, kek)
	ctx := context.Background()

	for _, vec := range vf.Vectors {
		t.Run(vec.Name, func(t *testing.T) {
			tenantID, locationID, uid := newVerifyTag(t, d, kek, hexBytes(t, vec.TagKey), vec.LastCtr, "active")
			uidBytes := hexBytes(t, uid)
			ctrBytes := hexBytes(t, vec.CtrHex)

			// Recompute the CMAC for this random UID matching the vector's kind.
			var mac [8]byte
			switch vec.Name {
			case "wrong_key":
				mac = referenceMAC(t, keyB, uidBytes, ctrBytes) // signed with the wrong key
			case "single_bit_flip":
				mac = referenceMAC(t, keyA, uidBytes, ctrBytes)
				mac[0] ^= 0x01
			default:
				mac = referenceMAC(t, keyA, uidBytes, ctrBytes)
			}
			p := mustParse(t, uid, vec.CtrHex, hex.EncodeToString(mac[:]))

			res, err := ver.Verify(ctx, p)
			if err != nil {
				t.Fatalf("%s: Verify err = %v, want nil", vec.Name, err)
			}
			if res.SUNValid != vec.WantSUNValid {
				t.Fatalf("%s: SUNValid = %v, want %v", vec.Name, res.SUNValid, vec.WantSUNValid)
			}
			if vec.WantSUNValid {
				if res.CtrGap != vec.WantGap {
					t.Fatalf("%s: gap = %d, want %d", vec.Name, res.CtrGap, vec.WantGap)
				}
				if res.Location != locationID {
					t.Fatalf("%s: Result.Location = %s, want %s", vec.Name, res.Location, locationID)
				}
				if res.Tag.TenantID != tenantID {
					t.Fatalf("%s: Result.Tag.TenantID = %s, want %s", vec.Name, res.Tag.TenantID, tenantID)
				}
				if got := finalCtr(t, d, uid); got != int32(p.Ctr) {
					t.Fatalf("%s: last_ctr = %d after accept, want %d (advanced)", vec.Name, got, p.Ctr)
				}
			} else {
				if got := finalCtr(t, d, uid); got != vec.LastCtr {
					t.Fatalf("%s: last_ctr = %d after reject, want %d (UNCHANGED -- no advance)", vec.Name, got, vec.LastCtr)
				}
			}
		})
	}
}

// TestVerify_RetiredAndLostAgainstPostgres proves the §5 line-1 short-circuit end
// to end: even a CRYPTOGRAPHICALLY VALID tap on a retired/lost tag is rejected and
// does NOT advance the counter (dead tag never reaches step 6).
func TestVerify_RetiredAndLostAgainstPostgres(t *testing.T) {
	d := appDB(t)
	kek := hexBytes(t, fakeKEKHex)
	keyA := hexBytes(t, fakeTagKey)
	ver := NewVerifier(d, kek)
	ctx := context.Background()

	for _, status := range []string{"retired", "lost"} {
		t.Run(status, func(t *testing.T) {
			_, _, uid := newVerifyTag(t, d, kek, keyA, 100, status)
			uidBytes := hexBytes(t, uid)
			ctrBytes := hexBytes(t, "000065") // 101 = last+1, a would-be valid advance
			mac := referenceMAC(t, keyA, uidBytes, ctrBytes)
			p := mustParse(t, uid, "000065", hex.EncodeToString(mac[:]))

			res, err := ver.Verify(ctx, p)
			if err != nil {
				t.Fatalf("%s: Verify err = %v, want nil", status, err)
			}
			if res.SUNValid {
				t.Fatalf("%s tag with a valid CMAC returned SUNValid=true, want false", status)
			}
			if got := finalCtr(t, d, uid); got != 100 {
				t.Fatalf("%s: last_ctr = %d, want 100 (dead tag must not advance even on a valid tap)", status, got)
			}
		})
	}
}

// TestVerify_QRAgainstPostgres: a QR tap on a real active tag -> SUNValid=false,
// counter unchanged, tenant/location still carried for the decision layer.
func TestVerify_QRAgainstPostgres(t *testing.T) {
	d := appDB(t)
	kek := hexBytes(t, fakeKEKHex)
	ver := NewVerifier(d, kek)

	tenantID, locationID, uid := newVerifyTag(t, d, kek, hexBytes(t, fakeTagKey), 42, "active")
	res, err := ver.Verify(context.Background(), mustParse(t, uid, "", "")) // QR
	if err != nil {
		t.Fatalf("Verify err = %v, want nil", err)
	}
	if res.SUNValid {
		t.Fatal("QR tap returned SUNValid=true, want false")
	}
	if res.Location != locationID || res.Tag.TenantID != tenantID {
		t.Fatal("QR Result must carry the resolved tag's tenant + location")
	}
	if got := finalCtr(t, d, uid); got != 42 {
		t.Fatalf("last_ctr = %d after a QR tap, want 42 (unchanged)", got)
	}
}

// TestVerify_UnknownUIDAgainstPostgres: a UID that matches no tag row -> the
// resolver returns pgx.ErrNoRows and Verify maps it to ErrUnknownTag.
func TestVerify_UnknownUIDAgainstPostgres(t *testing.T) {
	d := appDB(t)
	ver := NewVerifier(d, hexBytes(t, fakeKEKHex))

	unknown := strings.ToUpper(randUID(t)) // never inserted
	_, err := ver.Verify(context.Background(), mustParse(t, unknown, "000065", "83c0104d8ac39077"))
	if !errors.Is(err, ErrUnknownTag) {
		t.Fatalf("err = %v, want ErrUnknownTag for an unknown uid", err)
	}
}

// TestVerify_ConcurrentSameTapExactlyOneWinner is the §4.4 proof at the Verify
// level: N goroutines call Verify with the SAME genuine (uid, ctr, cmac). Exactly
// one gets SUNValid=true; the rest get SUNValid=false (a recorded replay reject,
// NOT an error). This proves Verify wires the atomic advance correctly under real
// contention -- no sync.Mutex, the DB row lock is the only synchronisation point.
func TestVerify_ConcurrentSameTapExactlyOneWinner(t *testing.T) {
	d := appDB(t)
	kek := hexBytes(t, fakeKEKHex)
	keyA := hexBytes(t, fakeTagKey)
	ver := NewVerifier(d, kek)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, _, uid := newVerifyTag(t, d, kek, keyA, 0, "active")
	uidBytes := hexBytes(t, uid)
	const ctrHex = "000007"
	mac := referenceMAC(t, keyA, uidBytes, hexBytes(t, ctrHex))
	p := mustParse(t, uid, ctrHex, hex.EncodeToString(mac[:]))

	var wins, rejects, failures int64
	var launched, done sync.WaitGroup
	launched.Add(raceGoroutines)
	done.Add(raceGoroutines)
	start := make(chan struct{})

	for i := 0; i < raceGoroutines; i++ {
		go func() {
			defer done.Done()
			launched.Done()
			<-start // release all at once for genuine row-lock contention

			res, err := ver.Verify(ctx, p)
			switch {
			case err != nil:
				atomic.AddInt64(&failures, 1)
				t.Errorf("unexpected error (want nil, reject is SUNValid=false): %v", err)
			case res.SUNValid:
				atomic.AddInt64(&wins, 1)
			default:
				atomic.AddInt64(&rejects, 1)
			}
		}()
	}

	launched.Wait()
	close(start)
	done.Wait()

	if wins != 1 {
		t.Fatalf("SUNValid winners = %d, want EXACTLY 1 (atomic replay guard)", wins)
	}
	if rejects != raceGoroutines-1 {
		t.Fatalf("rejects = %d, want %d (all losers SUNValid=false)", rejects, raceGoroutines-1)
	}
	if failures != 0 {
		t.Fatalf("%d goroutines errored; a lost race must be SUNValid=false, not an error", failures)
	}
	if got := finalCtr(t, d, uid); got != 7 {
		t.Fatalf("final last_ctr = %d, want 7 (the single winner's ctr)", got)
	}
}
