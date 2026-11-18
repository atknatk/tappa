//go:build sandbox

package oauth

import "testing"

// TestOAuthRedirect_SandboxAcceptsLoopback is the sandbox build's half of §10.4: the loopback form --
// port required, matched as registered -- is registered and redirected to, and the https rule is
// unchanged.
func TestOAuthRedirect_SandboxAcceptsLoopback(t *testing.T) {
	if Env != "test" || !LoopbackRedirects || Prefix(KindAccess) != "tt_at_test_" {
		t.Fatalf("the sandbox build: Env %q, LoopbackRedirects %v", Env, LoopbackRedirects)
	}
	for _, u := range []string{"http://localhost:3000/api/integrations/taptime/callback", "http://127.0.0.1:65535/cb"} {
		if err := RegistrationURIError(u); err != nil {
			t.Errorf("%s: %v", u, err)
		}
		if !AuthorizeURIAllowed(u) {
			t.Errorf("%s: V2 refused it", u)
		}
	}
	for _, u := range []string{"http://localhost/cb", "http://localhost:0/cb", "http://localhost:70000/cb", "http://localhost:3000/cb?x=1", "http://example.com:3000/cb"} {
		if RegistrationURIError(u) == nil {
			t.Errorf("%s was accepted", u)
		}
	}
}
