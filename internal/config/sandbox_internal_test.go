package config

import (
	"strings"
	"testing"
)

// TestEnvForBuild_TheSandboxValueBelongsToTheSandboxBuild (M11 API-11, ADR 0028 §11): the rule's
// table, in both builds -- the build is a parameter here, so the untagged run measures the sandbox
// build's half too. The Load-level twin is TestLoad_TheBuildDecidesTheSandboxValue.
//
// MUTANTS (measured): the sandbox branch's `raw != EnvSandbox` relaxed to `raw != "" && raw !=
// EnvSandbox` (the dev default comes back) -> the "" row red; the untagged branch deleted -> the
// sandbox row red; envDefault returning EnvDev for the sandbox build -> the default row red.
func TestEnvForBuild_TheSandboxValueBelongsToTheSandboxBuild(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		sandbox bool
		refused bool
	}{
		{"", false, false}, {EnvDev, false, false}, {EnvStaging, false, false}, {EnvProd, false, false},
		{EnvSandbox, false, true},
		{EnvSandbox, true, false},
		{"", true, true}, {EnvDev, true, true}, {EnvStaging, true, true}, {EnvProd, true, true},
		{"Sandbox", true, true}, {" sandbox", true, true},
	} {
		err := envForBuild(tc.raw, tc.sandbox)
		if (err != nil) != tc.refused {
			t.Errorf("envForBuild(%q, sandbox build %v) = %v, refused want %v", tc.raw, tc.sandbox, err, tc.refused)
		}
		if err != nil && !strings.Contains(err.Error(), "TAPPA_ENV") {
			t.Errorf("envForBuild(%q, %v): the refusal does not name the variable: %v", tc.raw, tc.sandbox, err)
		}
	}
	if got := envDefault("", false); got != EnvDev {
		t.Errorf("an untagged build's unset TAPPA_ENV is %q, want the dev default", got)
	}
	if got := envDefault("", true); got != "" {
		t.Errorf("the sandbox build's unset TAPPA_ENV is %q; it has no default (envForBuild refuses it)", got)
	}
	if got := envDefault(EnvProd, true); got != EnvProd {
		t.Errorf("a set TAPPA_ENV is the variable, got %q", got)
	}
}

// TestSandboxSettings_TheSandboxSendsNoEmail (ADR 0028 §11: "gerçek e-posta yok; davet panel
// modunda"): with TAPPA_ENV=sandbox both flows must be off -- the reset flow none, the invitation
// panel -- and every other environment is untouched by the rule.
//
// MUTANTS (measured): either check deleted -> its row red; the Env guard deleted -> the prod row red.
func TestSandboxSettings_TheSandboxSendsNoEmail(t *testing.T) {
	for _, tc := range []struct {
		env, reset, invite string
		errs               int
	}{
		{EnvSandbox, ResetDeliveryNone, InviteDeliveryPanel, 0},
		{EnvSandbox, ResetDeliveryEmail, InviteDeliveryPanel, 1},
		{EnvSandbox, ResetDeliveryNone, InviteDeliveryEmail, 1},
		{EnvSandbox, ResetDeliveryEmail, InviteDeliveryEmail, 2},
		{EnvProd, ResetDeliveryEmail, InviteDeliveryEmail, 0},
		{EnvDev, ResetDeliveryEmail, InviteDeliveryEmail, 0},
	} {
		errs := sandboxSettings(&Config{Env: tc.env, ResetDelivery: tc.reset, InviteDelivery: tc.invite})
		if len(errs) != tc.errs {
			t.Errorf("%s reset=%s invite=%s: %d refusals %v, want %d", tc.env, tc.reset, tc.invite, len(errs), errs, tc.errs)
		}
	}
}

// TestIsHardened_IsProductionOrTheSandbox (K-28): the predicate the nine gates ask.
func TestIsHardened_IsProductionOrTheSandbox(t *testing.T) {
	for env, want := range map[string]bool{EnvProd: true, EnvSandbox: true, EnvDev: false, EnvStaging: false, "": false, "production": false} {
		if got := (&Config{Env: env}).IsHardened(); got != want {
			t.Errorf("Env %q: IsHardened() = %v, want %v", env, got, want)
		}
	}
}
