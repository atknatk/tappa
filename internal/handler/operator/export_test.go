package operator

import (
	"net/http"

	"github.com/a-h/templ"

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
