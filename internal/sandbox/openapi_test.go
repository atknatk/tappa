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

// TestSandboxOpenAPI_AnswersAreOpenTheRequestIsClosed is v1's compatibility rule on the sandbox's own
// document (the user's decision, 2026-10-23; the shipped document's
// TestOpenAPI_EveryAnswerSchemaIsOpenEveryRequestClosed): every schema an answer uses says
// additionalProperties: true -- an answer may gain fields -- and the schema a requestBody names stays
// false (422 unknown_field).
func TestSandboxOpenAPI_AnswersAreOpenTheRequestIsClosed(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Type                 string `json:"type"`
				AdditionalProperties *bool  `json:"additionalProperties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(OpenAPI(), &doc); err != nil {
		t.Fatalf("the document is not JSON: %v", err)
	}
	requests := map[string]bool{}
	for _, ops := range doc.Paths {
		for _, op := range ops {
			for _, c := range op.RequestBody.Content {
				if name, ok := strings.CutPrefix(c.Schema.Ref, "#/components/schemas/"); ok {
					requests[name] = true
				}
			}
		}
	}
	if !requests["SimulatedTap"] || len(requests) != 1 {
		t.Fatalf("PREMISE: the request schemas are %v, want SimulatedTap alone", requests)
	}
	open := 0
	for name, s := range doc.Components.Schemas {
		if s.Type != "object" {
			continue
		}
		want := !requests[name]
		if s.AdditionalProperties == nil || *s.AdditionalProperties != want {
			t.Errorf("%s: additionalProperties %v, want %v", name, s.AdditionalProperties, want)
		}
		if want {
			open++
		}
	}
	if open < 4 {
		t.Errorf("anti-vacuity: %d open answer schemas", open)
	}
}

// TestSandboxOpenAPI_TheAnswerIsTheWireStructKeyForKey is the sandbox answer's leak pin now that its
// schema is open (2026-10-23): the simulation answers json.NewEncoder(w).Encode(tapBodyOf(...)), so
// the keys tapBody and refBody encode are exactly SimulatedTapResult's and Ref's documented
// properties, both ways -- a key added to the struct (or dropped from the document) is red.
func TestSandboxOpenAPI_TheAnswerIsTheWireStructKeyForKey(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(OpenAPI(), &doc); err != nil {
		t.Fatal(err)
	}
	keysOf := func(v any) []string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		var out []string
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	for _, c := range []struct {
		schema string
		wire   any
	}{{"SimulatedTapResult", tapBody{}}, {"Ref", refBody{}}} {
		var documented []string
		for k := range doc.Components.Schemas[c.schema].Properties {
			documented = append(documented, k)
		}
		sort.Strings(documented)
		if got := keysOf(c.wire); !slices.Equal(got, documented) || len(got) == 0 {
			t.Errorf("%s: the wire encodes %v, the document names %v", c.schema, got, documented)
		}
	}
}
