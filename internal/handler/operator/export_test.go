package operator

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// Doors into this package for its external tests. This file is a _test.go file, so the
// go tool compiles it into this package's test build and not into the product.

// RenderForTest is the surface's render, so a test can drive a component that refuses
// to render (operatorpages.TenantScreen without a name) through the real path.
func RenderForTest(s *Surface, w http.ResponseWriter, r *http.Request, c templ.Component) {
	s.render(w, r, http.StatusOK, c)
}

// FormValueForTest wraps v as the handlers wrap a form field.
func FormValueForTest(v string) formValue { return formValue{v: &v} }

// ProblemPagesForTest is problemPages() (render.go).
func ProblemPagesForTest() []operatorpages.ProblemView { return problemPages() }

// VisibleTextForTest is legal.go's visibleText.
func VisibleTextForTest(body string) bool { return visibleText(body) }

// MaxLegalBodyForTest is legal.go's maxLegalBody; LegalVersionsLimitForTest its page;
// LegalWriteTimeoutForTest the bound of a publication and its refresh.
const (
	MaxLegalBodyForTest       = maxLegalBody
	LegalVersionsLimitForTest = legalVersionsLimit
	LegalWriteTimeoutForTest  = legalWriteTimeout
)

// TenantPageSizeForTest is tenants.go's tenantPageSize; MaxTenantPageForTest its
// maxTenantPage.
const (
	TenantPageSizeForTest = tenantPageSize
	MaxTenantPageForTest  = maxTenantPage
)

// PlaqueWordForTest is one entry of plaques.go's dictionary, exported field by field.
type PlaqueWordForTest struct {
	Label, Sentence string
	Tone            operatorpages.PlaqueTone
}

// PlaqueWordsForTest is plaques.go's plaqueWords, copied.
func PlaqueWordsForTest() map[db.PlaqueShape]PlaqueWordForTest {
	out := map[db.PlaqueShape]PlaqueWordForTest{}
	for k, w := range plaqueWords {
		out[k] = PlaqueWordForTest{w.label, w.sentence, w.tone}
	}
	return out
}

// PlaqueWordForShapeForTest is plaques.go's plaqueWordFor.
func PlaqueWordForShapeForTest(shape db.PlaqueShape) PlaqueWordForTest {
	w := plaqueWordFor(shape)
	return PlaqueWordForTest{w.label, w.sentence, w.tone}
}

// AuditPageSizeForTest is audit.go's auditPageSize.
const AuditPageSizeForTest = auditPageSize

// AuditKindWordsForTest is audit.go's auditKindWords, copied.
func AuditKindWordsForTest() map[db.OperatorAuditKind]string {
	out := map[db.OperatorAuditKind]string{}
	for k, w := range auditKindWords {
		out[k] = w
	}
	return out
}

// AuditScopeWordsForTest and AuditSearchWordsForTest are audit.go's auditScopeWords and
// auditSearchWords, copied.
func AuditScopeWordsForTest() map[string]string  { return copyWords(auditScopeWords) }
func AuditSearchWordsForTest() map[string]string { return copyWords(auditSearchWords) }

func copyWords(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, w := range m {
		out[k] = w
	}
	return out
}

// AuditKindWordForTest is audit.go's auditKindWord.
func AuditKindWordForTest(raw string) operatorpages.AuditWord { return auditKindWord(raw) }

// MaxBillingPageForTest is billing.go's maxBillingPage.
const MaxBillingPageForTest = maxBillingPage

// MoneyTextForTest is billing.go's moneyText.
func MoneyTextForTest(n pgtype.Numeric, currency string) (string, error) {
	return moneyText(n, currency)
}
