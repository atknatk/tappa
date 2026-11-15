//go:build !sandbox

package oauth

import "testing"

// TestBuild_ProductionIsLiveAndRefusesLoopback pins the untagged build's half of the split (ADR 0047
// §7.1, §10.4) WITHOUT a skip: the production binary's values are tt_<kind>_live_ and it never takes a
// loopback redirect URI. The loopback tests above skip in the sandbox build by reading
// LoopbackRedirects, so without this pin an untagged build that had turned loopback on would skip them
// and stay green.
//
// MUTANTS (measured, phase 4 make-check fix): internal/config's untagged credentialWord set to "test",
// and config.IsSandboxBuild set to true -- each red here; under the second,
// TestOAuthRedirect_ProductionV2RefusesLoopback skips and stays green (measured: the gap this closes).
func TestBuild_ProductionIsLiveAndRefusesLoopback(t *testing.T) {
	if Env != "live" || LoopbackRedirects {
		t.Fatalf("the untagged build: Env %q, LoopbackRedirects %v -- want live and false", Env, LoopbackRedirects)
	}
	if AuthorizeURIAllowed("http://127.0.0.1:3000/cb") || RegistrationURIError("http://localhost:3000/cb") != ErrRedirectLoopback {
		t.Fatal("the untagged build takes a loopback redirect URI")
	}
	if Prefix(KindAccess) != "tt_at_live_" {
		t.Fatalf("the access token prefix is %q", Prefix(KindAccess))
	}
}
