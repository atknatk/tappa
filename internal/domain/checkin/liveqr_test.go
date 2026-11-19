package checkin

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
)

// TestValidate_AVerifiedLiveQRCodeCarriesItsScreenAndItsExpiry is the M16-21 security review's R8:
// sys:live-qr-expired reads tap:tokenOverdueSeconds, which tap.Decide writes only for a verified
// code WITH an expiry, and a missing key never matches -- so a verified request without its window
// would decide an old photograph as a fresh scan. The request is refused instead (a caller's bug,
// ErrInvalidRequest), as is one naming no screen; an unverified one needs neither (it is the
// recorded reject). CONTROL: the well-formed request passes.
func TestValidate_AVerifiedLiveQRCodeCarriesItsScreenAndItsExpiry(t *testing.T) {
	base := Request{SessionTenantID: uuid.New(), EmployeeID: uuid.New(), Channel: tap.ChannelQRLive}
	good := base
	good.TokenVerified, good.DisplayID, good.TokenExpiresAt = true, uuid.New(), time.Now()
	if err := good.validate(); err != nil {
		t.Fatalf("CONTROL: a well-formed verified live QR request was refused: %v", err)
	}
	if err := base.validate(); err != nil {
		t.Fatalf("an unverified live QR request (the recorded reject) was refused: %v", err)
	}
	noExpiry := good
	noExpiry.TokenExpiresAt = time.Time{}
	noScreen := good
	noScreen.DisplayID = uuid.Nil
	plaque := good
	plaque.TagUID = "04AABBCCDDEEFF"
	for name, r := range map[string]Request{"verified with no expiry": noExpiry, "verified with no screen": noScreen, "naming a plaque": plaque} {
		if err := r.validate(); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: err = %v, want ErrInvalidRequest", name, err)
		}
	}
}

// TestInsertParams_ALiveQRRowNeverWritesATrueSUN is the M16-19 hand-off's SECOND guard, measured on
// its own (the third eye's DÜŞÜK-1): tap.Decide already puts tap:sunValid=false on a live QR context,
// and insertParams writes false on a live QR row WHATEVER the decision's context says -- here a
// decision carrying true. The holding gate's tolerant arm and the site's refusal keys assume a live
// QR row's sun_valid is not true. CONTROL: the same decision on an NFC row writes its true.
// MUTANT (measured): the block that pins false removed -- the live QR row writes true, red.
func TestInsertParams_ALiveQRRowNeverWritesATrueSUN(t *testing.T) {
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	in := tap.TypeIn
	dec := tap.Decision{Verdict: tap.VerdictReject, Trust: 20, PolicyContext: map[policy.ContextKey]any{policy.CtxTapSunValid: true}}
	live, err := (&Service{}).insertParams(Request{Channel: tap.ChannelQRLive, Client: tap.ClientBrowser}, db.ResolvedTag{}, tapFacts{}, dec, at)
	if err != nil {
		t.Fatal(err)
	}
	if live.SunValid == nil || *live.SunValid {
		t.Fatalf("a live QR row whose decision said sunValid=true writes sun_valid=%s, want false", boolWord(live.SunValid))
	}
	dec.Verdict, dec.Type = tap.VerdictOK, &in
	nfc, err := (&Service{}).insertParams(Request{Channel: tap.ChannelNFC, Client: tap.ClientBrowser}, db.ResolvedTag{UID: "04AABBCCDDEEFF"}, tapFacts{}, dec, at)
	if err != nil {
		t.Fatal(err)
	}
	if nfc.SunValid == nil || !*nfc.SunValid {
		t.Fatalf("CONTROL: an NFC row with a valid SUN writes sun_valid=%s, want true", boolWord(nfc.SunValid))
	}
}

func boolWord(b *bool) string {
	switch {
	case b == nil:
		return "NULL"
	case *b:
		return "true"
	}
	return "false"
}
