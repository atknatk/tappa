package sandbox

import (
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/handler/api"
)

// TestSandboxOpenAPI_IsTheSimulationsRoutesAndTheSharedErrorSet: the sandbox's document, both ways --
// its paths are exactly the routes the extension mounts (method and path), the Error schema's code
// enum is the integration API's closed set, every x-error-codes entry is in it, ErrorDetails' field
// and reason enums are the simulation's own values, and the bearer scheme names the sandbox prefix.
//
// MUTANTS (measured): a route added to the extension without its path, a code dropped from the enum,
// a reason constant renamed, the scheme's prefix edited -- each red.
func TestSandboxOpenAPI_IsTheSimulationsRoutesAndTheSharedErrorSet(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			XErrorCodes []string `json:"x-error-codes"`
		} `json:"paths"`
		Components struct {
			SecuritySchemes map[string]struct{ Description string } `json:"securitySchemes"`
			Schemas         map[string]struct {
				Properties map[string]struct {
					Enum []any `json:"enum"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(OpenAPI(), &doc); err != nil {
		t.Fatalf("the document is not JSON: %v", err)
	}
	var documented []string
	for path, ops := range doc.Paths {
		for method := range ops {
			documented = append(documented, strings.ToUpper(method)+" "+path)
		}
	}
	r := chi.NewRouter()
	r.Route(api.Prefix, func(r chi.Router) {
		s := &Simulation{}
		s.MountPublic(r)
		s.MountAuthenticated(r, func(http.ResponseWriter, *http.Request, []byte, api.Work) (apikey.Outcome, bool, bool) {
			return apikey.Outcome{}, false, false
		})
	})
	var routed []string
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routed = append(routed, method+" "+route)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(documented)
	sort.Strings(routed)
	if !slices.Equal(documented, routed) || len(routed) != 2 {
		t.Errorf("the document's operations %v, the extension's routes %v", documented, routed)
	}
	var codes []string
	for _, c := range api.Codes() {
		codes = append(codes, string(c))
	}
	if got := strs(doc.Components.Schemas["Error"].Properties["code"].Enum); !slices.Equal(got, codes) {
		t.Errorf("the Error code enum %v, the integration API's set %v", got, codes)
	}
	for path, ops := range doc.Paths {
		for method, op := range ops {
			for _, c := range op.XErrorCodes {
				if !slices.Contains(codes, c) {
					t.Errorf("%s %s lists %q, which is not a code", method, path, c)
				}
			}
		}
	}
	details := doc.Components.Schemas["ErrorDetails"].Properties
	if got := strs(details["field"].Enum); !slices.Equal(got, tapFields) {
		t.Errorf("ErrorDetails.field %v, the body's fields %v", got, tapFields)
	}
	reasons := []string{api.ReasonRequired, "type", api.ReasonInvalid, "unknown_field", "duplicate_field", "malformed_json", "invalid_utf8",
		reasonNoEmployee, reasonNoLocation, reasonNoPlaque}
	if got := strs(details["reason"].Enum); !slices.Equal(got, reasons) {
		t.Errorf("ErrorDetails.reason %v, the simulation's %v", got, reasons)
	}
	if !strings.Contains(doc.Components.SecuritySchemes["bearer"].Description, "`"+apikey.SandboxPrefix+"`") {
		t.Errorf("the bearer scheme does not name %s", apikey.SandboxPrefix)
	}
	for _, p := range []*api.Problem{problemNoEmployee, problemNoLocation, problemNoPlaque, problemAmbiguousLocation, problemChannel} {
		if r, ok := p.Details["reason"].(string); !ok || !slices.Contains(reasons, r) {
			t.Errorf("%s answers reason %v, which the document does not list", p.Code, p.Details["reason"])
		}
	}
}

func strs(v []any) []string {
	var out []string
	for _, x := range v {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
