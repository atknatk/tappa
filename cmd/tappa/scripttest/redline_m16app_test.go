package scripttest

import (
	"strings"
	"testing"
)

// TestRedline_M16_AppSurfaceLogCalls pins R7's app-surface arm (M16-07): a Go log or fmt call in
// a /app/v1 handler file (internal/handler/app*.go) that names the device's signature, the request
// nonce, the activation challenge, the public key or the consent binding FAILS -- the five roots
// R7's Go pattern does not carry (a bare signature root was measured as 35 lines of noise
// tree-wide, API-3). The controls: the same file with a value-free log line, and the documented
// boundary -- the same call in a handler file NOT named app*.go is outside this arm. Fixtures are
// masked with "~~" (m16Unmask) for redline_api3_test.go's rule.
func TestRedline_M16_AppSurfaceLogCalls(t *testing.T) {
	t.Parallel()
	const h = "internal/handler/"
	red := map[string]string{
		h + "app_nonce.go":     "package handler\nfunc f() { a.log.InfoContext(ctx, \"issued\", \"non~~ce\", n) }\n",
		h + "app_sig.go":       "package handler\nfunc f() { slog.Warn(\"bad\", \"si~~g\", s) }\n",
		h + "app_signature.go": "package handler\nfunc f() error { return fmt.Errorf(\"refused %s\", req.Signa~~ture) }\n",
		h + "app_challenge.go": "package handler\nfunc f() { a.log.ErrorContext(ctx, \"x\", \"chal~~lenge\", ch.Challenge.Wire()) }\n",
		h + "app_key.go":       "package handler\nfunc f() { a.log.Info(\"bind\", \"public~~Key\", in.PublicKey) }\n",
		h + "app_binding.go":   "package handler\nfunc f() {\n\ta.log.WarnContext(ctx, \"moved\",\n\t\t\"bind~~ing\", b)\n}\n",
	}
	green := map[string]string{
		h + "app_ok.go":   "package handler\nfunc f() { a.log.InfoContext(ctx, \"app: refused a signed request\", \"reason\", ae.Reason()) }\n",
		h + "tap_misc.go": "package handler\nfunc f() { a.log.InfoContext(ctx, \"x\", \"non~~ce\", n) }\n",
	}
	m16RedGreen(t, "R7", red, green)

	// The arm's title names it, so a FAIL from R7's other arms is not mistaken for this one.
	out, _ := redlineRun(t, m16Tree(map[string]string{h + "app_nonce.go": red[h+"app_nonce.go"]}))
	if !strings.Contains(out, "uygulama yuzeyinin log cagrisinda") {
		t.Errorf("the app-surface arm did not report under its own title:\n%s", out)
	}
}
