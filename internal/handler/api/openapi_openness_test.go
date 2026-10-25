package api

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestOpenAPI_EveryAnswerSchemaIsOpenEveryRequestClosed is v1's compatibility rule on the document
// (the user's decision, 2026-10-23; ADR 0028 §8's K note; the guide's §3): an answer or a webhook body
// may gain fields, so every object schema an answer or a webhook body uses says
// additionalProperties: true; a request body stays closed (422 unknown_field), so every object schema
// a path's requestBody reaches -- through $ref, transitively -- says additionalProperties: false.
// Nested object schemas (Employee's activationEmail) follow the schema they sit in. The split is read
// from the document itself, so a new schema is held to the rule by where it is used, not by a list.
//
// What keeps a leak visible now that the answers are open is not the schema but the tests that pin an
// answer's keys -- and only in the cases those tests read: the story's answers
// (TestOpenAPI_TheExamplesAreTheHandlersAnswers, every family), the handler tests that decode an
// answer with DisallowUnknownFields (locOf / empOf in TestLocationsPut_* and TestEmployeesPut_*),
// TestPunches_NothingButTheFeedsColumnsAnswers, TestWebhookPayload_CarriesNothingButTheFeedItem,
// TestDirectory_HoldingTenantsOnlyNoEmailWithActive, TestWebhookPing_TheBodyIsSyntheticAndSigned, and
// for Error's details TestOpenAPI_EveryRouteAndErrorDocumented (its keys are read from the package's
// source, both ways). A key added unconditionally was measured red in each family (M15-13 fix
// rounds). A key added only in a state no test answers -- an activated employee, an inactive
// location -- is caught by none of them, and was not before the schemas opened either (backlog T162).
func TestOpenAPI_EveryAnswerSchemaIsOpenEveryRequestClosed(t *testing.T) {
	doc := openAPIDoc(t)
	schemas := mustMap(t, func() any { v, _ := yat(doc, "components", "schemas"); return v }(), "components.schemas")

	requests := map[string]bool{}
	var reach func(v any)
	reach = func(v any) {
		walkValues(v, "", func(_ string, x any) {
			m, ok := x.(*ymap)
			if !ok {
				return
			}
			ref, ok := m.get("$ref")
			if !ok {
				return
			}
			name, found := strings.CutPrefix(fmt.Sprint(ref), "#/components/schemas/")
			if !found || requests[name] {
				return
			}
			requests[name] = true
			if s, ok := schemas.get(name); ok {
				reach(s)
			}
		})
	}
	paths := mustMap(t, func() any { v, _ := doc.get("paths"); return v }(), "paths")
	for _, p := range paths.keys {
		item := mustMap(t, paths.m[p], p)
		for _, method := range httpMethods {
			if op, ok := item.get(method); ok {
				if rb, ok := mustMap(t, op, p+" "+method).get("requestBody"); ok {
					reach(rb)
				}
			}
		}
	}

	var open, closed []string
	for _, name := range schemas.keys {
		want := !requests[name]
		walkValues(schemas.m[name], name, func(where string, x any) {
			m, ok := x.(*ymap)
			if !ok {
				return
			}
			if typ, _ := m.get("type"); typ != "object" {
				return
			}
			ap, has := m.get("additionalProperties")
			switch {
			case !has:
				t.Errorf("%s: an object schema without additionalProperties (want %v -- %s)", where, want, opennessRule(want))
			case ap != want:
				t.Errorf("%s: additionalProperties is %v, want %v -- %s", where, ap, want, opennessRule(want))
			case want:
				open = append(open, where)
			default:
				closed = append(closed, where)
			}
		})
	}
	slices.Sort(closed)
	if !slices.Equal(closed, []string{"EmployeePut", "LocationPut"}) {
		t.Errorf("the closed (request) schemas are %v, want EmployeePut and LocationPut", closed)
	}
	if len(open) < 14 {
		t.Errorf("anti-vacuity: %d open answer schemas (%v)", len(open), open)
	}
}

func opennessRule(open bool) string {
	if open {
		return "an answer or a webhook body may gain fields in v1"
	}
	return "a request body is closed (422 unknown_field)"
}
