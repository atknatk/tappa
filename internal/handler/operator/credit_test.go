package operator_test

import (
	"strings"
	"testing"

	"github.com/atknatk/tappa/web/templates/layout"
)

// TestOperatorScreens_CarryNoMarketingCredit: the operator company's credit (the
// footer's "Built by" link and the head's author tags, user request 2026-10-23) is a
// PUBLIC-surface thing; none of the operator console's renders carries it.
func TestOperatorScreens_CarryNoMarketingCredit(t *testing.T) {
	t.Parallel()
	all := screens(t)
	if len(all) == 0 {
		t.Fatal("screens() rendered nothing, so this proves nothing")
	}
	for name, html := range all {
		for _, never := range []string{layout.CreditURL, layout.CreditName, `rel="author"`, `name="author"`} {
			if strings.Contains(html, never) {
				t.Errorf("operator screen %q carries %q", name, never)
			}
		}
	}
}
