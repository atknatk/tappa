package adminauth

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
)

// signupclaimskew_db_test.go -- the registration draft's lifetime against 00078's ceiling when the two
// clocks disagree (M14-17b's finding, fixed 2026-10-27). Open dates the expiry by the application's clock;
// identity_open_signup_claim refuses one later than the DATABASE's now() + 24 hours.

// TestSignupClaimTTL_LeavesTheSkewMarginUnderThe00078Ceiling pins the arithmetic and the ceiling's source:
// TTL + margin is the ceiling, the margin is not zero, and the ceiling is the one 00078's function writes.
func TestSignupClaimTTL_LeavesTheSkewMarginUnderThe00078Ceiling(t *testing.T) {
	if SignupClaimTTL+signupClaimSkewMargin != signupClaimCeiling {
		t.Fatalf("TTL %v + margin %v != ceiling %v", SignupClaimTTL, signupClaimSkewMargin, signupClaimCeiling)
	}
	if signupClaimSkewMargin < time.Minute {
		t.Fatalf("the skew margin is %v; a database clock behind the application's by more refuses every registration", signupClaimSkewMargin)
	}
	src, err := os.ReadFile("../../db/migrations/00078_verify_signups_by_e_mail.sql")
	if err != nil {
		t.Fatal(err)
	}
	if signupClaimCeiling != 24*time.Hour || !strings.Contains(string(src), "AND p_expires_at <= pg_catalog.now() + interval '24 hours'") {
		t.Fatal("the ceiling here is not identity_open_signup_claim's (00078)")
	}
}

// TestSignupClaims_OpenSurvivesADatabaseClockBehindTheApplications, on Postgres through the real
// identity_open_signup_claim: the application's clock (SignupClaims.now, the injected seam) set AHEAD of
// the database's by 2 s -- the database's clock behind -- and Open still writes the draft. CONTROL: ahead
// by more than the margin, the function refuses (the ceiling is real and this test reaches it).
//
// MUTATION (measured): signupClaimSkewMargin = 0 -> the 2 s case is refused, red.
func TestSignupClaims_OpenSurvivesADatabaseClockBehindTheApplications(t *testing.T) {
	d := testDB(t)
	owner := ownerConn(t)
	s, err := NewSignupClaims(d, &config.Config{SessionHMACKey: []byte(strings.Repeat("s", 32))})
	if err != nil {
		t.Fatal(err)
	}
	s.digest = func(string) (string, error) { return "$2a$04$" + strings.Repeat("k", 53), nil }
	draft := []byte(`{"full_name": "Skew Owner", "business_name": "Skew", "vat_number": "MT00000000", "business_type": "restaurant", ` +
		`"structure": "single", "vat_check": "unknown", "venues": ["Front door"]}`)
	open := func(ahead time.Duration) error {
		t.Helper()
		s.now = func() time.Time { return time.Now().Add(ahead) }
		email := "skew-" + uuid.NewString() + "@signup.example"
		t.Cleanup(func() {
			if _, err := owner.Exec(context.Background(), `DELETE FROM admin_signup_claims WHERE email = $1`, email); err != nil {
				t.Errorf("remove the draft: %v", err)
			}
		})
		_, err := s.Open(context.Background(), email, "pw", draft)
		return err
	}
	if err := open(2 * time.Second); err != nil {
		t.Fatalf("the database's clock 2 s behind refused the registration: %v", err)
	}
	if err := open(signupClaimSkewMargin + time.Minute); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("CONTROL: a clock ahead by more than the margin answered %v, want the function's refusal", err)
	}
}
