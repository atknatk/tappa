package operator

import (
	"net/http"

	"github.com/a-h/templ"

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
