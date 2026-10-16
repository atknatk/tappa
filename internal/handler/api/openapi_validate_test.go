package api

// openapi_validate_test.go -- openapi.yaml as a CONTRACT, not as text (M11 API-12; ADR 0028 §8,
// "API-12 uygulama notu"):
//
//	TestOpenAPI_TheDocumentIsOpenAPI31        the document is OpenAPI 3.1 in its structure: the
//	                                          root, every operation, parameter and response; every
//	                                          $ref resolves; every schema uses only keywords the
//	                                          checker below enforces, and every pattern compiles;
//	                                          every example validates against its own schema
//	TestOpenAPI_EveryRouteAndErrorDocumented  every route answers, under probes that reach each of
//	                                          its outcomes through the shipped surface, exactly the
//	                                          statuses its operation lists and exactly the codes of
//	                                          its x-error-codes, and every answer validates against
//	                                          the documented schema; every error example is what the
//	                                          handler writes; ErrorDetails' keys and closed values
//	                                          are the package's own, both ways; x-limits is the code's
//	TestOpenAPI_TheWebhooksAreTheSenders      the webhooks section is the sender's: its headers, its
//	                                          retry schedule, its tries, its timeout, its body

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/egress"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/webhook"
	"github.com/atknatk/tappa/internal/webhook/delivery"
	"github.com/atknatk/tappa/internal/webhook/endpoints"
)

// openAPIDoc parses the embedded document with the subset reader.
func openAPIDoc(t *testing.T) *ymap {
	t.Helper()
	v, err := parseYAMLSubset(string(OpenAPI()))
	if err != nil {
		t.Fatalf("openapi.yaml is outside the YAML subset: %v", err)
	}
	m, ok := v.(*ymap)
	if !ok {
		t.Fatal("openapi.yaml is not a mapping")
	}
	return m
}

// at walks a path of keys (and list indexes) from v; ok=false when any step is missing.
func yat(v any, path ...string) (any, bool) {
	for _, p := range path {
		switch x := v.(type) {
		case *ymap:
			var ok bool
			if v, ok = x.get(p); !ok {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

func mustMap(t *testing.T, v any, where string) *ymap {
	t.Helper()
	m, ok := v.(*ymap)
	if !ok {
		t.Fatalf("%s is not a mapping (%T)", where, v)
	}
	return m
}

// resolveRef follows a local $ref ("#/a/b", JSON pointer escapes) inside doc.
func resolveRef(doc *ymap, ref string) (any, error) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, fmt.Errorf("a $ref that is not local: %q", ref)
	}
	var steps []string
	for _, s := range strings.Split(ref[2:], "/") {
		steps = append(steps, strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~"))
	}
	v, ok := yat(doc, steps...)
	if !ok {
		return nil, fmt.Errorf("the $ref %q resolves to nothing", ref)
	}
	return v, nil
}

// deref follows $ref chains.
func deref(doc *ymap, v any) (any, error) {
	for range 16 {
		m, ok := v.(*ymap)
		if !ok {
			return v, nil
		}
		r, isRef := m.get("$ref")
		if !isRef {
			return v, nil
		}
		s, _ := r.(string)
		var err error
		if v, err = resolveRef(doc, s); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("a $ref chain longer than 16")
}

// schemaKeywords are the JSON Schema keywords the validator below ENFORCES (or that are pure
// annotation). A schema using any other is a red test: a constraint the checker would ignore is a
// constraint nobody checks.
var schemaKeywords = map[string]bool{
	"type": true, "enum": true, "properties": true, "required": true, "additionalProperties": true, "items": true,
	"pattern": true, "minLength": true, "maxLength": true, "minimum": true, "maximum": true, "format": true, "$ref": true,
	"description": true, "default": true, "title": true, "example": true,
}

// schemaChecker validates values against the document's schemas.
type schemaChecker struct {
	doc *ymap
}

// check validates val against schema; the errors name where.
func (c schemaChecker) check(schema, val any, where string) []string {
	s, err := deref(c.doc, schema)
	if err != nil {
		return []string{where + ": " + err.Error()}
	}
	m, ok := s.(*ymap)
	if !ok {
		return []string{fmt.Sprintf("%s: a schema that is not a mapping (%T)", where, s)}
	}
	var errs []string
	fail := func(f string, a ...any) { errs = append(errs, where+": "+fmt.Sprintf(f, a...)) }
	if t, has := m.get("type"); has {
		var types []string
		switch x := t.(type) {
		case string:
			types = []string{x}
		case []any:
			for _, e := range x {
				types = append(types, fmt.Sprint(e))
			}
		}
		okType := false
		for _, ty := range types {
			okType = okType || hasJSONType(val, ty)
		}
		if !okType {
			fail("%v is not of type %v", val, types)
			return errs
		}
	}
	if e, has := m.get("enum"); has {
		list, _ := e.([]any)
		found := false
		for _, x := range list {
			found = found || canonical(x) == canonical(val)
		}
		if !found {
			fail("%v is not one of %v", val, list)
		}
	}
	if str, isStr := val.(string); isStr {
		if p, has := m.get("pattern"); has {
			re, err := regexp.Compile(p.(string))
			if err != nil {
				fail("the pattern does not compile: %v", err)
			} else if !re.MatchString(str) {
				fail("%q does not match %s", str, p)
			}
		}
		if n, has := m.get("minLength"); has && float64(len([]rune(str))) < n.(float64) {
			fail("%q is shorter than %v", str, n)
		}
		if n, has := m.get("maxLength"); has && float64(len([]rune(str))) > n.(float64) {
			fail("%q is longer than %v", str, n)
		}
		if f, has := m.get("format"); has {
			switch f {
			case "date-time":
				if _, err := time.Parse(time.RFC3339Nano, str); err != nil {
					fail("%q is not an RFC 3339 date-time", str)
				}
			case "uuid":
				if _, err := uuid.Parse(str); err != nil || len(str) != 36 {
					fail("%q is not a uuid", str)
				}
			default:
				fail("a format the checker does not know: %v", f)
			}
		}
	}
	if num, isNum := val.(float64); isNum {
		if n, has := m.get("minimum"); has && num < n.(float64) {
			fail("%v is below %v", num, n)
		}
		if n, has := m.get("maximum"); has && num > n.(float64) {
			fail("%v is above %v", num, n)
		}
	}
	if obj, isObj := val.(*ymap); isObj {
		props, _ := m.get("properties")
		pm, _ := props.(*ymap)
		if req, has := m.get("required"); has {
			for _, k := range req.([]any) {
				if _, present := obj.get(k.(string)); !present {
					fail("the required %q is missing", k)
				}
			}
		}
		for _, k := range obj.keys {
			var sub any
			if pm != nil {
				sub, _ = pm.get(k)
			}
			if sub == nil {
				ap, has := m.get("additionalProperties")
				switch {
				case has && ap == false:
					fail("%q is not a property of the schema", k)
					continue
				case has && ap != true:
					sub = ap
				default:
					continue
				}
			}
			errs = append(errs, c.check(sub, obj.m[k], where+"."+k)...)
		}
	}
	if list, isList := val.([]any); isList {
		if items, has := m.get("items"); has {
			for i, e := range list {
				errs = append(errs, c.check(items, e, fmt.Sprintf("%s[%d]", where, i))...)
			}
		}
	}
	return errs
}

func hasJSONType(v any, ty string) bool {
	switch ty {
	case "null":
		return v == nil
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == math.Trunc(f)
	case "object":
		_, ok := v.(*ymap)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	}
	return false
}

// httpMethods are the operation keys of a path item.
var httpMethods = []string{"get", "put", "post", "delete", "patch", "head", "options", "trace"}

// operation is one operation of the document.
type operation struct {
	where  string // "paths /v1/x get" or "webhooks punch post"
	path   string
	method string
	op     *ymap
}

func operationsOf(t *testing.T, doc *ymap) []operation {
	t.Helper()
	var out []operation
	for _, section := range []string{"paths", "webhooks"} {
		sec, _ := doc.get(section)
		sm := mustMap(t, sec, section)
		for _, p := range sm.keys {
			item := mustMap(t, sm.m[p], section+" "+p)
			for _, k := range item.keys {
				if slices.Contains(httpMethods, k) {
					out = append(out, operation{where: section + " " + p + " " + k, path: p, method: k, op: mustMap(t, item.m[k], p+" "+k)})
				} else if !slices.Contains([]string{"summary", "description", "parameters"}, k) {
					t.Errorf("%s %s: a path item key OpenAPI 3.1 does not define: %q", section, p, k)
				}
			}
		}
	}
	return out
}

// walkValues calls f on every value of the document with its path.
func walkValues(v any, path string, f func(path string, v any)) {
	f(path, v)
	switch x := v.(type) {
	case *ymap:
		for _, k := range x.keys {
			walkValues(x.m[k], path+"/"+k, f)
		}
	case []any:
		for i, e := range x {
			walkValues(e, fmt.Sprintf("%s/%d", path, i), f)
		}
	}
}

// TestOpenAPI_TheDocumentIsOpenAPI31 (M11 API-12): the document read as OpenAPI 3.1, with no
// library -- the YAML subset above and the checks below.
//
// MUTANTS (measured, API-12 report): a $ref to a missing schema; a response without a description;
// a path parameter the path does not name; an example field the schema does not have; a schema
// keyword the checker does not enforce (oneOf) -- each red.
func TestOpenAPI_TheDocumentIsOpenAPI31(t *testing.T) {
	doc := openAPIDoc(t)
	for _, k := range doc.keys {
		if !slices.Contains([]string{"openapi", "info", "jsonSchemaDialect", "servers", "paths", "webhooks", "components", "security", "tags", "externalDocs"}, k) {
			t.Errorf("a root key OpenAPI 3.1 does not define: %q", k)
		}
	}
	if v, _ := doc.get("openapi"); v != "3.1.0" {
		t.Fatalf("openapi is %v, want 3.1.0", v)
	}
	for _, k := range []string{"title", "version"} {
		if v, ok := yat(doc, "info", k); !ok || v == "" {
			t.Errorf("info.%s is missing", k)
		}
	}
	if v, ok := yat(doc, "servers", "0", "url"); !ok || !strings.HasPrefix(fmt.Sprint(v), "https://") {
		t.Errorf("servers[0].url: %v", v)
	}
	schemes := mustMap(t, func() any { v, _ := yat(doc, "components", "securitySchemes"); return v }(), "securitySchemes")
	if v, _ := yat(schemes, "bearer", "type"); v != "http" {
		t.Errorf("the bearer scheme's type is %v", v)
	}
	if v, _ := yat(schemes, "bearer", "scheme"); v != "bearer" {
		t.Errorf("the bearer scheme is %v", v)
	}

	// Every $ref resolves; a $ref's siblings are summary and description only (3.1's Reference
	// Object; a schema's $ref has none here).
	refs := 0
	walkValues(doc, "#", func(path string, v any) {
		m, ok := v.(*ymap)
		if !ok {
			return
		}
		r, has := m.get("$ref")
		if !has {
			return
		}
		refs++
		if _, err := resolveRef(doc, fmt.Sprint(r)); err != nil {
			t.Errorf("%s: %v", path, err)
		}
		for _, k := range m.keys {
			if k != "$ref" && k != "summary" && k != "description" {
				t.Errorf("%s: a $ref with the sibling %q", path, k)
			}
		}
	})
	if refs < 50 {
		t.Fatalf("anti-vacuity: %d $refs", refs)
	}

	// Every schema: known keywords, a compiling pattern, required names that are properties.
	check := schemaChecker{doc: doc}
	schemas := 0
	var visitSchema func(where string, s any)
	visitSchema = func(where string, s any) {
		m, ok := s.(*ymap)
		if !ok {
			t.Errorf("%s: a schema that is not a mapping", where)
			return
		}
		schemas++
		for _, k := range m.keys {
			if !schemaKeywords[k] {
				t.Errorf("%s: the schema keyword %q is not one the checker enforces", where, k)
			}
		}
		if p, has := m.get("pattern"); has {
			if _, err := regexp.Compile(fmt.Sprint(p)); err != nil {
				t.Errorf("%s: pattern %v: %v", where, p, err)
			}
		}
		props, _ := m.get("properties")
		pm, _ := props.(*ymap)
		if req, has := m.get("required"); has {
			for _, k := range req.([]any) {
				if pm == nil {
					t.Errorf("%s: required without properties", where)
					break
				}
				if _, ok := pm.get(fmt.Sprint(k)); !ok {
					t.Errorf("%s: required %q is not a property", where, k)
				}
			}
		}
		if pm != nil {
			for _, k := range pm.keys {
				visitSchema(where+".properties."+k, pm.m[k])
			}
		}
		for _, k := range []string{"items", "additionalProperties"} {
			if sub, has := m.get(k); has {
				if _, isBool := sub.(bool); !isBool {
					visitSchema(where+"."+k, sub)
				}
			}
		}
		if ex, has := m.get("example"); has {
			for _, e := range check.check(m, ex, where+" example") {
				t.Error(e)
			}
		}
	}
	walkValues(doc, "#", func(path string, v any) {
		m, ok := v.(*ymap)
		if !ok {
			return
		}
		if s, has := m.get("schema"); has {
			visitSchema(path+"/schema", s)
		}
	})
	compSchemas := mustMap(t, func() any { v, _ := yat(doc, "components", "schemas"); return v }(), "components.schemas")
	for _, k := range compSchemas.keys {
		visitSchema("components.schemas."+k, compSchemas.m[k])
	}
	if schemas < 60 {
		t.Fatalf("anti-vacuity: %d schemas", schemas)
	}

	// Every operation: a unique operationId, a summary, responses keyed by status, each response
	// with a description; path parameters exactly the path's placeholders, each required.
	ids := map[string]bool{}
	examples := 0
	ops := operationsOf(t, doc)
	for _, o := range ops {
		id, _ := o.op.get("operationId")
		if id == nil || ids[fmt.Sprint(id)] {
			t.Errorf("%s: a missing or repeated operationId %v", o.where, id)
		}
		ids[fmt.Sprint(id)] = true
		if s, _ := o.op.get("summary"); s == nil {
			t.Errorf("%s: no summary", o.where)
		}
		for _, k := range o.op.keys {
			if !strings.HasPrefix(k, "x-") && !slices.Contains([]string{"operationId", "summary", "description", "parameters", "requestBody", "responses", "security", "tags", "deprecated"}, k) {
				t.Errorf("%s: an operation key OpenAPI 3.1 does not define: %q", o.where, k)
			}
		}
		resp := mustMap(t, func() any { v, _ := o.op.get("responses"); return v }(), o.where+" responses")
		if len(resp.keys) == 0 {
			t.Errorf("%s: no response", o.where)
		}
		for _, status := range resp.keys {
			if !regexp.MustCompile(`^([1-5][0-9][0-9]|[1-5]XX|default)$`).MatchString(status) {
				t.Errorf("%s: response key %q", o.where, status)
			}
			r, err := deref(doc, resp.m[status])
			if err != nil {
				t.Errorf("%s %s: %v", o.where, status, err)
				continue
			}
			rm := mustMap(t, r, o.where+" "+status)
			if d, _ := rm.get("description"); d == nil || d == "" {
				t.Errorf("%s %s: a response without a description", o.where, status)
			}
			examples += checkContentExamples(t, check, o.where+" "+status, rm)
		}
		var declared []string
		if ps, has := o.op.get("parameters"); has {
			for i, p := range ps.([]any) {
				pv, err := deref(doc, p)
				if err != nil {
					t.Errorf("%s parameter %d: %v", o.where, i, err)
					continue
				}
				pm := mustMap(t, pv, o.where+" parameter")
				in, _ := pm.get("in")
				name, _ := pm.get("name")
				if !slices.Contains([]string{"query", "header", "path", "cookie"}, fmt.Sprint(in)) || name == nil {
					t.Errorf("%s: parameter %d: in %v, name %v", o.where, i, in, name)
				}
				if _, has := pm.get("schema"); !has {
					t.Errorf("%s: parameter %v has no schema", o.where, name)
				}
				if in == "path" {
					declared = append(declared, fmt.Sprint(name))
					if req, _ := pm.get("required"); req != true {
						t.Errorf("%s: path parameter %v is not required", o.where, name)
					}
				}
				if ex, has := pm.get("example"); has {
					s, _ := pm.get("schema")
					for _, e := range check.check(s, ex, o.where+" parameter "+fmt.Sprint(name)+" example") {
						t.Error(e)
					}
					examples++
				}
			}
		}
		var placeholders []string
		for _, m := range regexp.MustCompile(`\{([^}]+)\}`).FindAllStringSubmatch(o.path, -1) {
			placeholders = append(placeholders, m[1])
		}
		slices.Sort(declared)
		slices.Sort(placeholders)
		if !slices.Equal(declared, placeholders) {
			t.Errorf("%s: path parameters %v, the path names %v", o.where, declared, placeholders)
		}
		if rb, has := o.op.get("requestBody"); has {
			examples += checkContentExamples(t, check, o.where+" requestBody", mustMap(t, rb, o.where+" requestBody"))
		}
	}
	if len(ops) < 12 || examples < 25 {
		t.Fatalf("anti-vacuity: %d operations, %d examples", len(ops), examples)
	}
}

// checkContentExamples validates the examples of a content map (a response or a request body)
// against their media type's schema and returns how many it read.
func checkContentExamples(t *testing.T, check schemaChecker, where string, holder *ymap) int {
	t.Helper()
	c, has := holder.get("content")
	if !has {
		return 0
	}
	n := 0
	cm := mustMap(t, c, where+" content")
	for _, mt := range cm.keys {
		media := mustMap(t, cm.m[mt], where+" "+mt)
		schema, hasSchema := media.get("schema")
		if !hasSchema {
			t.Errorf("%s %s: no schema", where, mt)
			continue
		}
		if ex, has := media.get("examples"); has {
			em := mustMap(t, ex, where+" examples")
			for _, name := range em.keys {
				val, ok := yat(em.m[name], "value")
				if !ok {
					t.Errorf("%s example %s: no value", where, name)
					continue
				}
				for _, e := range check.check(schema, val, where+" example "+name) {
					t.Error(e)
				}
				n++
			}
		}
	}
	return n
}

// probeLocations, probeEmployees, probePunches and probeDirectory are domain fakes that answer what
// a probe asks for -- a success, or one domain refusal.
type probeLocations struct {
	created  bool
	err      error
	venueErr error
}

func (p probeLocations) PutVenueTx(context.Context, pgx.Tx, tenant.VenuePut) (tenant.VenuePutResult, error) {
	if p.err != nil {
		return tenant.VenuePutResult{}, p.err
	}
	return tenant.VenuePutResult{Venue: probeVenue(), Created: p.created, Changed: !p.created}, nil
}

func (p probeLocations) APIVenue(context.Context, uuid.UUID, uuid.UUID) (tenant.APIVenue, error) {
	if p.venueErr != nil {
		return tenant.APIVenue{}, p.venueErr
	}
	return probeVenue(), nil
}

func probeVenue() tenant.APIVenue {
	return tenant.APIVenue{ID: uuid.New(), ExternalRef: "KF-VLT", Name: "Valletta", Active: true, Timezone: "Europe/Malta", CreatedAt: time.Now()}
}

type probeEmployees struct {
	created bool
	err     error
	byIDErr error
	// admit, when set, is the class the fake's PUT asks the checkpoint for (the domain's).
	admit tenant.EmployeeWrite
}

func probeEmployee() tenant.APIEmployee {
	ref, first, last := "E-1042", "Jane", "Example"
	now := time.Now()
	return tenant.APIEmployee{ID: uuid.New(), ExternalRef: &ref, FirstName: &first, LastName: &last, FullName: "Jane Example",
		Status: "invited", HasEmail: true, CreatedAt: now, UpdatedAt: now, Invitation: tenant.InvitationQueued, InvitationAt: &now}
}

func (p probeEmployees) result() (tenant.EmployeePutResult, error) {
	if p.err != nil {
		return tenant.EmployeePutResult{}, p.err
	}
	return tenant.EmployeePutResult{Employee: probeEmployee(), Created: p.created}, nil
}

func (p probeEmployees) PutEmployeeTx(ctx context.Context, _ pgx.Tx, _ tenant.EmployeePut, admit tenant.Admit) (tenant.EmployeePutResult, error) {
	class := p.admit
	if class == 0 {
		class = tenant.EmployeeChanges
		if p.created {
			class = tenant.EmployeeOpens
		}
	}
	if err := admit(ctx, class); err != nil {
		return tenant.EmployeePutResult{}, err
	}
	return p.result()
}

func (p probeEmployees) DeleteEmployeeTx(context.Context, pgx.Tx, tenant.EmployeeDelete) (tenant.EmployeePutResult, error) {
	return p.result()
}

func (p probeEmployees) ResendActivationTx(context.Context, pgx.Tx, tenant.EmployeeResend) (tenant.EmployeePutResult, error) {
	return p.result()
}

func (p probeEmployees) APIEmployeeByRef(context.Context, uuid.UUID, string) (tenant.APIEmployee, error) {
	if p.err != nil {
		return tenant.APIEmployee{}, p.err
	}
	return probeEmployee(), nil
}

func (p probeEmployees) APIEmployeeByID(context.Context, uuid.UUID, uuid.UUID) (tenant.APIEmployee, error) {
	if p.byIDErr != nil {
		return tenant.APIEmployee{}, p.byIDErr
	}
	return probeEmployee(), nil
}

func (p probeEmployees) ListAPIEmployees(context.Context, uuid.UUID, *uuid.UUID, int) ([]tenant.APIEmployee, error) {
	return []tenant.APIEmployee{probeEmployee()}, nil
}

func (probeEmployees) RecordDeactivationBudgetExceeded(context.Context, uuid.UUID, uuid.UUID, string, int) error {
	return nil
}

type probePunches struct{}

func (probePunches) PunchPage(context.Context, uuid.UUID, tenant.FeedRole, int64, int) (tenant.PunchPage, error) {
	return tenant.PunchPage{Next: 7}, nil
}

func (probePunches) HistoryPage(context.Context, uuid.UUID, tenant.FeedRole, tenant.HistoryQuery, int) (tenant.HistoryPage, error) {
	return tenant.HistoryPage{Next: tenant.HistoryPosition{S0: 7, Time: time.Now(), Seq: 7}}, nil
}

type probeDirectory struct{}

func (probeDirectory) DirectoryPage(context.Context, uuid.UUID, tenant.DirectoryQuery, int) (tenant.DirectoryPage, error) {
	return tenant.DirectoryPage{AsOf: time.Now()}, nil
}

// probe is one request against the shipped surface with fakes set up for one outcome.
type probe struct {
	name   string
	method string
	path   string
	body   string
	auth   string // "" none, "key" a valid key, "closed" a key of a business whose API is closed
	hdr    []string
	cfg    func(*Config)
	// twice sends the request twice and answers the second (a bucket of one).
	twice bool
	// failTouch makes last_used_at's write fail (an outage on our side).
	failTouch bool
	want      int
	code      Code // "" for a success
}

func (p probe) run(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	tenantID, keyID := uuid.New(), uuid.New()
	answer := known(tenantID, keyID)
	if p.auth == "closed" {
		answer = func(string) (db.ResolvedAPIKey, error) {
			return db.ResolvedAPIKey{TenantID: tenantID, KeyID: keyID}, nil
		}
	}
	r := newRig(t, answer, func(c *Config) {
		c.Writes = &fakeWrites{}
		c.Locations, c.Employees, c.Punches, c.Directory = probeLocations{created: true}, probeEmployees{created: true}, probePunches{}, probeDirectory{}
		if p.cfg != nil {
			p.cfg(c)
		}
	})
	r.touches.fail.Store(p.failTouch)
	auth := ""
	if p.auth != "" {
		auth = "Bearer " + liveKey(t)
	}
	send := func() *httptest.ResponseRecorder {
		return r.do(p.method, p.path, auth, "", strings.NewReader(p.body), p.hdr...)
	}
	if p.twice {
		send()
	}
	return send()
}

// operationOf finds the documented operation a request lands on: its path template's segments
// match the request's ({x} matches one segment).
func operationOf(ops []operation, method, path string) (operation, bool) {
	segs := strings.Split(strings.SplitN(path, "?", 2)[0], "/")
	for _, o := range ops {
		if !strings.HasPrefix(o.where, "paths ") || o.method != strings.ToLower(method) {
			continue
		}
		tpl := strings.Split(o.path, "/")
		if len(tpl) != len(segs) {
			continue
		}
		match := true
		for i := range tpl {
			match = match && (tpl[i] == segs[i] || (strings.HasPrefix(tpl[i], "{") && segs[i] != ""))
		}
		if match {
			return o, true
		}
	}
	return operation{}, false
}

// The probes: per route, every outcome the surface can answer -- the chain's (no key, a closed API,
// a bucket, an outage), the body bound's (a compressed body, a body over the bound), and the
// route's own (each domain refusal, the checkpoint, the runner's Idempotency answers, a replay whose
// record is gone).
func surfaceProbes() []probe {
	big := strings.Repeat("a", MaxBody+1)
	gz := []string{"Content-Encoding", "gzip"}
	limitOne := func(c *Config) { c.AddressLimit = 1 }
	suspended := func(c *Config) { c.Suspension = suspendedSwitch{on: true} }
	idem := []string{"Idempotency-Key", "order-1"}
	mismatch := func(c *Config) { c.Writes = &fakeWrites{err: apikey.ErrIdempotencyMismatch} }
	replay := func(c *Config) { c.Writes = &fakeWrites{replay: true} }
	emp := func(e probeEmployees) func(*Config) { return func(c *Config) { c.Employees = e } }
	loc := func(l probeLocations) func(*Config) { return func(c *Config) { c.Locations = l } }
	locBody := `{"name":"Valletta","active":true}`
	empBody := `{"firstName":"Jane","lastName":"Example","email":"jane.example@example.com","active":true}`
	var out []probe
	// The three documents.
	for _, doc := range []string{"/v1/openapi.yaml", "/v1/guide.md", "/v1/guide.tr.md"} {
		out = append(out,
			probe{name: "the document", method: "GET", path: doc, want: 200},
			probe{name: "a compressed body", method: "GET", path: doc, hdr: gz, want: 400, code: CodeInvalidRequest},
			probe{name: "a body over the bound", method: "GET", path: doc, body: big, want: 400, code: CodeBodyTooLarge},
			probe{name: "the address bucket", method: "GET", path: doc, cfg: limitOne, twice: true, want: 429, code: CodeRateLimited},
			probe{name: "a query parameter", method: "GET", path: doc + "?lang=tr", want: 400, code: CodeInvalidRequest},
		)
	}
	for _, doc := range []string{"/v1/guide.md", "/v1/guide.tr.md"} {
		out = append(out, probe{name: "a copy that is current", method: "GET", path: doc, hdr: []string{"If-None-Match", "*"}, want: 304})
	}
	// The chain and the body bound, on every authenticated route.
	type route struct{ method, path, body string }
	routes := []route{{"PUT", "/v1/locations/KF-VLT", locBody}, {"GET", "/v1/employees", ""}, {"GET", "/v1/employees/E-1042", ""},
		{"PUT", "/v1/employees/E-1042", empBody}, {"DELETE", "/v1/employees/E-1042", ""}, {"POST", "/v1/employees/E-1042/resend-activation", ""},
		{"GET", "/v1/punches?scope=employer", ""}, {"GET", "/v1/directory", ""}}
	for _, rt := range routes {
		out = append(out,
			probe{name: "no key", method: rt.method, path: rt.path, body: rt.body, want: 401, code: CodeUnauthorized},
			probe{name: "a closed API", method: rt.method, path: rt.path, body: rt.body, auth: "closed", want: 403, code: CodeAPIDisabled},
			probe{name: "the address bucket", method: rt.method, path: rt.path, body: rt.body, auth: "key", cfg: limitOne, twice: true, want: 429, code: CodeRateLimited},
			probe{name: "an outage", method: rt.method, path: rt.path, body: rt.body, auth: "key", failTouch: true, want: 503, code: CodeUnavailable},
			probe{name: "a compressed body", method: rt.method, path: rt.path, body: rt.body, auth: "key", hdr: gz, want: 400, code: CodeInvalidRequest},
			probe{name: "a body over the bound", method: rt.method, path: rt.path, body: big, auth: "key", want: 400, code: CodeBodyTooLarge},
		)
	}
	k := "key"
	out = append(out,
		// PUT /v1/locations/{ref}
		probe{name: "a new location", method: "PUT", path: "/v1/locations/KF-VLT", body: locBody, auth: k, want: 201},
		probe{name: "a known location", method: "PUT", path: "/v1/locations/KF-VLT", body: locBody, auth: k, cfg: loc(probeLocations{}), want: 200},
		probe{name: "malformed JSON", method: "PUT", path: "/v1/locations/KF-VLT", body: `{"name":`, auth: k, want: 400, code: CodeInvalidRequest},
		probe{name: "an unknown field", method: "PUT", path: "/v1/locations/KF-VLT", body: `{"name":"V","active":true,"lat":1}`, auth: k, want: 422, code: CodeValidationFailed},
		probe{name: "another time zone", method: "PUT", path: "/v1/locations/KF-VLT", body: locBody, auth: k, cfg: loc(probeLocations{err: &tenant.TimezoneMismatch{BusinessZone: "Europe/Malta"}}), want: 422, code: CodeTimezoneMismatch},
		probe{name: "suspended", method: "PUT", path: "/v1/locations/KF-VLT", body: locBody, auth: k, cfg: suspended, want: 403, code: CodeTenantSuspended},
		probe{name: "a key reused", method: "PUT", path: "/v1/locations/KF-VLT", body: locBody, auth: k, hdr: idem, cfg: mismatch, want: 422, code: CodeIdempotencyKeyReused},
		probe{name: "a replay whose location is gone", method: "PUT", path: "/v1/locations/KF-VLT", body: locBody, auth: k, hdr: idem, cfg: func(c *Config) { replay(c); c.Locations = probeLocations{venueErr: tenant.ErrUnknownVenue} }, want: 404, code: CodeNotFound},
		// GET /v1/employees
		probe{name: "a page", method: "GET", path: "/v1/employees", auth: k, want: 200},
		probe{name: "an unknown parameter", method: "GET", path: "/v1/employees?page=2", auth: k, want: 400, code: CodeInvalidRequest},
		probe{name: "a query string that does not parse", method: "GET", path: "/v1/employees?limit=1;x=1", auth: k, want: 400, code: CodeInvalidRequest},
		// GET /v1/employees/{ref}
		probe{name: "one employee", method: "GET", path: "/v1/employees/E-1042", auth: k, want: 200},
		probe{name: "a query parameter", method: "GET", path: "/v1/employees/E-1042?x=1", auth: k, want: 400, code: CodeInvalidRequest},
		probe{name: "a reference never held", method: "GET", path: "/v1/employees/E-1042", auth: k, cfg: emp(probeEmployees{err: tenant.ErrUnknownEmployee}), want: 404, code: CodeNotFound},
		probe{name: "a reference outside the pattern", method: "GET", path: "/v1/employees/E%2F1042", auth: k, want: 422, code: CodeValidationFailed},
		// PUT /v1/employees/{ref}
		probe{name: "a new employee", method: "PUT", path: "/v1/employees/E-1042", body: empBody, auth: k, want: 201},
		probe{name: "a known employee", method: "PUT", path: "/v1/employees/E-1042", body: empBody, auth: k, cfg: emp(probeEmployees{}), want: 200},
		probe{name: "deactivating a reference never held", method: "PUT", path: "/v1/employees/E-1042", body: empBody, auth: k, cfg: emp(probeEmployees{err: tenant.ErrUnknownEmployee}), want: 404, code: CodeNotFound},
		probe{name: "an address in use", method: "PUT", path: "/v1/employees/E-1042", body: empBody, auth: k, cfg: emp(probeEmployees{err: tenant.ErrEmailTaken}), want: 409, code: CodeEmailUnavailable},
		probe{name: "email missing", method: "PUT", path: "/v1/employees/E-1042", body: `{"firstName":"J","lastName":"E","active":true}`, auth: k, want: 422, code: CodeValidationFailed},
		probe{name: "the deactivation budget", method: "PUT", path: "/v1/employees/E-1042", body: empBody, auth: k, cfg: emp(probeEmployees{err: &tenant.DeactivationBudgetExceeded{Budget: 20, RetryAfter: time.Minute}}), want: 429, code: CodeDeactivationBudgetExceeded},
		probe{name: "suspended, a change", method: "PUT", path: "/v1/employees/E-1042", body: empBody, auth: k, cfg: func(c *Config) { suspended(c); c.Employees = probeEmployees{} }, want: 403, code: CodeTenantSuspended},
		probe{name: "a key reused", method: "PUT", path: "/v1/employees/E-1042", body: empBody, auth: k, hdr: idem, cfg: mismatch, want: 422, code: CodeIdempotencyKeyReused},
		probe{name: "malformed JSON", method: "PUT", path: "/v1/employees/E-1042", body: `[`, auth: k, want: 400, code: CodeInvalidRequest},
		// DELETE /v1/employees/{ref}
		probe{name: "a deactivation", method: "DELETE", path: "/v1/employees/E-1042", auth: k, want: 200},
		probe{name: "a reference never held", method: "DELETE", path: "/v1/employees/E-1042", auth: k, cfg: emp(probeEmployees{err: tenant.ErrUnknownEmployee}), want: 404, code: CodeNotFound},
		probe{name: "suspended", method: "DELETE", path: "/v1/employees/E-1042", auth: k, cfg: suspended, want: 403, code: CodeTenantSuspended},
		probe{name: "a reference outside the pattern", method: "DELETE", path: "/v1/employees/E%2F1042", auth: k, want: 422, code: CodeValidationFailed},
		probe{name: "the deactivation budget", method: "DELETE", path: "/v1/employees/E-1042", auth: k, cfg: emp(probeEmployees{err: &tenant.DeactivationBudgetExceeded{Budget: 20, RetryAfter: time.Minute}}), want: 429, code: CodeDeactivationBudgetExceeded},
		probe{name: "a key reused", method: "DELETE", path: "/v1/employees/E-1042", auth: k, hdr: idem, cfg: mismatch, want: 422, code: CodeIdempotencyKeyReused},
		// POST /v1/employees/{ref}/resend-activation
		probe{name: "a resend", method: "POST", path: "/v1/employees/E-1042/resend-activation", auth: k, want: 202},
		probe{name: "a reference never held", method: "POST", path: "/v1/employees/E-1042/resend-activation", auth: k, cfg: emp(probeEmployees{err: tenant.ErrUnknownEmployee}), want: 404, code: CodeNotFound},
		probe{name: "activated", method: "POST", path: "/v1/employees/E-1042/resend-activation", auth: k, cfg: emp(probeEmployees{err: tenant.ErrAlreadyActivated}), want: 409, code: CodeAlreadyActivated},
		probe{name: "no address", method: "POST", path: "/v1/employees/E-1042/resend-activation", auth: k, cfg: emp(probeEmployees{err: tenant.ErrNoAddressToInvite}), want: 422, code: CodeValidationFailed},
		probe{name: "suspended is open", method: "POST", path: "/v1/employees/E-1042/resend-activation", auth: k, cfg: suspended, want: 202},
		probe{name: "a key reused", method: "POST", path: "/v1/employees/E-1042/resend-activation", auth: k, hdr: idem, cfg: mismatch, want: 422, code: CodeIdempotencyKeyReused},
		probe{name: "a replay whose employee is gone", method: "POST", path: "/v1/employees/E-1042/resend-activation", auth: k, hdr: idem, cfg: func(c *Config) { replay(c); c.Employees = probeEmployees{byIDErr: tenant.ErrUnknownEmployee} }, want: 404, code: CodeNotFound},
		// GET /v1/punches, GET /v1/directory
		probe{name: "the feed", method: "GET", path: "/v1/punches?scope=location", auth: k, want: 200},
		probe{name: "a history page", method: "GET", path: "/v1/punches?scope=employer&from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z", auth: k, want: 200},
		probe{name: "no scope", method: "GET", path: "/v1/punches", auth: k, want: 400, code: CodeInvalidRequest},
		probe{name: "a query string that does not parse", method: "GET", path: "/v1/punches?scope=employer&%zz", auth: k, want: 400, code: CodeInvalidRequest},
		probe{name: "the directory", method: "GET", path: "/v1/directory", auth: k, want: 200},
		probe{name: "an unknown parameter", method: "GET", path: "/v1/directory?since=x", auth: k, want: 400, code: CodeInvalidRequest},
		probe{name: "a query string that does not parse", method: "GET", path: "/v1/directory?li%mit=1", auth: k, want: 400, code: CodeInvalidRequest},
	)
	return out
}

// TestOpenAPI_EveryRouteAndErrorDocumented (M11 API-12; ADR 0028 "Test edilecekler", API-12): the
// document is the surface's, both ways --
//
//	statuses and codes  per route, the probes reach every outcome of the shipped surface; the
//	                    statuses they answer are EXACTLY the operation's responses and the codes
//	                    EXACTLY its x-error-codes (a status or a code answered but undocumented, or
//	                    documented but unreachable, is red); every answer validates against the
//	                    documented schema of its status
//	error examples      every example of a shared error response is the body writeProblem writes for
//	                    the named refusal
//	error details       ErrorDetails' properties are the keys the package's refusals carry, and its
//	                    closed values (field, reason, parameter, parameters, header) are the
//	                    package's constants -- read from the source, both ways
//	limits              info.x-limits is the code's constants (the guide's table is pinned to it)
//
// MUTANTS (measured, API-12 report): a code dropped from an operation's x-error-codes; a 404 added
// to the feed; a reason added to the source without the document; an error example's message
// edited; x-limits' requestsPerKeyPerMinute off by one -- each red.
func TestOpenAPI_EveryRouteAndErrorDocumented(t *testing.T) {
	doc := openAPIDoc(t)
	check := schemaChecker{doc: doc}
	ops := operationsOf(t, doc)

	type seen struct{ statuses, codes map[string]bool }
	observed := map[string]*seen{}
	for _, p := range surfaceProbes() {
		o, ok := operationOf(ops, p.method, p.path)
		if !ok {
			t.Fatalf("%s %s: no documented operation", p.method, p.path)
		}
		w := p.run(t)
		if w.Code != p.want {
			t.Errorf("%s (%s %s): %d %s, want %d", p.name, p.method, p.path, w.Code, w.Body.String(), p.want)
			continue
		}
		s := observed[o.where]
		if s == nil {
			s = &seen{statuses: map[string]bool{}, codes: map[string]bool{}}
			observed[o.where] = s
		}
		s.statuses[strconv.Itoa(w.Code)] = true
		if p.code != "" {
			if got := problemOf(t, w).Code; got != p.code {
				t.Errorf("%s (%s %s): code %q, want %q", p.name, p.method, p.path, got, p.code)
			}
			s.codes[string(p.code)] = true
		}
		// The answer validates against the documented response of its status.
		resp, _ := yat(o.op, "responses", strconv.Itoa(w.Code))
		r, err := deref(doc, resp)
		if err != nil {
			t.Errorf("%s: %v", o.where, err)
			continue
		}
		schema, hasBody := yat(r, "content", "application/json", "schema")
		if !hasBody {
			continue
		}
		val, err := jsonValue(w.Body.Bytes())
		if err != nil {
			t.Errorf("%s (%s): the answer is not JSON: %q", p.name, o.where, w.Body.String())
			continue
		}
		for _, e := range check.check(schema, val, o.where+" "+p.name) {
			t.Error(e)
		}
	}
	for _, o := range ops {
		if !strings.HasPrefix(o.where, "paths ") {
			continue
		}
		s := observed[o.where]
		if s == nil {
			t.Errorf("%s: no probe reached it", o.where)
			continue
		}
		var documented []string
		resp, _ := o.op.get("responses")
		documented = append(documented, resp.(*ymap).keys...)
		var got []string
		for st := range s.statuses {
			got = append(got, st)
		}
		slices.Sort(documented)
		slices.Sort(got)
		if !slices.Equal(documented, got) {
			t.Errorf("%s: documented statuses %v, answered %v", o.where, documented, got)
		}
		var codes, answered []string
		xc, _ := o.op.get("x-error-codes")
		for _, c := range xc.([]any) {
			codes = append(codes, fmt.Sprint(c))
		}
		for c := range s.codes {
			answered = append(answered, c)
		}
		slices.Sort(codes)
		slices.Sort(answered)
		if !slices.Equal(codes, answered) {
			t.Errorf("%s: x-error-codes %v, answered %v", o.where, codes, answered)
		}
		for _, c := range codes {
			if !slices.Contains(documented, strconv.Itoa(StatusOf(Code(c)))) {
				t.Errorf("%s: %s answers %d, which the operation does not list", o.where, c, StatusOf(Code(c)))
			}
		}
	}

	// The error examples are writeProblem's bodies.
	wantExamples := map[string]*Problem{
		"BadRequest/limit": problemLimit, "BadRequest/body_too_large": problemBodyTooLarge,
		"Unauthorized/unauthorized": problemUnauthorized,
		"Forbidden/api_disabled":    problemAPIDisabled, "Forbidden/tenant_suspended": problemTenantSuspended,
		"NotFound/not_found":         problemEmployeeNotFound,
		"Conflict/email_unavailable": problemEmailUnavailable, "Conflict/already_activated": problemAlreadyActivated,
		"Unprocessable/unknown_field": problemUnknownField, "Unprocessable/timezone_mismatch": timezoneMismatch("Europe/Malta"),
		"Unprocessable/idempotency_key_reused": problemIdempotencyReused,
		"TooManyRequests/rate_limited":         rateLimited(addressPeriod), "TooManyRequests/deactivation_budget_exceeded": budgetExceeded(1500 * time.Second),
		"Unavailable/unavailable": problemUnavailable,
	}
	responses := mustMap(t, func() any { v, _ := yat(doc, "components", "responses"); return v }(), "components.responses")
	found := map[string]bool{}
	for _, rn := range responses.keys {
		ex, ok := yat(responses.m[rn], "content", "application/json", "examples")
		if !ok {
			continue
		}
		em := ex.(*ymap)
		for _, name := range em.keys {
			key := rn + "/" + name
			found[key] = true
			p, ok := wantExamples[key]
			if !ok {
				t.Errorf("the error example %s names no refusal of this test", key)
				continue
			}
			rec := httptest.NewRecorder()
			writeProblem(rec, p)
			want, _ := jsonValue(rec.Body.Bytes())
			got, _ := yat(em.m[name], "value")
			if canonical(want) != canonical(got) {
				t.Errorf("the error example %s is not the handler's body:\n got %s\nwant %s", key, canonical(got), canonical(want))
			}
		}
	}
	for k := range wantExamples {
		if !found[k] {
			t.Errorf("the error example %s is missing from the document", k)
		}
	}

	// ErrorDetails is the source's, both ways.
	keys, closed := problemDetailsInSource(t)
	props := mustMap(t, func() any { v, _ := yat(doc, "components", "schemas", "ErrorDetails", "properties"); return v }(), "ErrorDetails.properties")
	documentedKeys := append([]string(nil), props.keys...)
	slices.Sort(documentedKeys)
	if !slices.Equal(documentedKeys, keys) {
		t.Errorf("ErrorDetails' properties %v, the refusals' detail keys %v", documentedKeys, keys)
	}
	for key, values := range closed {
		enum, ok := yat(props, key, "enum")
		if !ok {
			enum, ok = yat(props, key, "items", "enum")
		}
		if !ok {
			t.Errorf("ErrorDetails.%s has no enum; the source's values are %v", key, values)
			continue
		}
		var doc []string
		for _, e := range enum.([]any) {
			doc = append(doc, fmt.Sprint(e))
		}
		slices.Sort(doc)
		if !slices.Equal(doc, values) {
			t.Errorf("ErrorDetails.%s: the document's values %v, the source's %v", key, doc, values)
		}
	}
	for _, k := range []string{"field", "reason", "parameter", "parameters", "header"} {
		if len(closed[k]) == 0 {
			t.Errorf("anti-vacuity: the source scan found no value of %q", k)
		}
	}

	// info.x-limits is the code's.
	limits := mustMap(t, func() any { v, _ := yat(doc, "info", "x-limits"); return v }(), "info.x-limits")
	idemHours := idempotencyHoursInSQL(t)
	pins := map[string]float64{
		"requestsPerAddressPerMinute": perMinute(addressLimit, addressPeriod),
		"requestsPerKeyPerMinute":     perMinute(keyLimit, keyPeriod),
		"deactivationsPerKeyPerHour":  float64(tenant.DefaultDeactivationBudget) * float64(time.Hour) / float64(tenant.DeactivationBudgetWindow),
		"maxBodyBytes":                MaxBody,
		"defaultPageSize":             DefaultLimit,
		"maxPageSize":                 MaxLimit,
		"idempotencyHours":            idemHours,
		"requestTimeoutSeconds":       httpx.RequestTimeout.Seconds(),
		"feedWindowPositions":         tenant.PunchFeedWindow,
		"historyMaxHours":             tenant.MaxHistoryRange.Hours(),
		"directoryOverlapMinutes":     tenant.DirectoryOverlap.Minutes(),
		"webhookTimeoutSeconds":       egress.TotalTimeout.Seconds(),
		"webhookAttempts":             delivery.MaxAttempts,
		"webhookTestMessagesPerHour":  float64(endpoints.BucketLimit) * float64(time.Hour) / float64(endpoints.BucketWindow),
		"invitesPerBusinessPerHour":   invite.TenantHourLimit,
		"invitesPerBusinessPerDay":    invite.TenantDayLimit,
		"invitesPerEmployeePerHour":   invite.EmployeeHourLimit,
		"invitationQueueHours":        queueConstant(t, `queueLifetime\s*=\s*([0-9]+) \* time\.Hour`),
		"invitationSendAttempts":      queueConstant(t, `queueMaxAttempts\s*=\s*([0-9]+)`),
	}
	for _, k := range limits.keys {
		want, ok := pins[k]
		if !ok {
			t.Errorf("x-limits.%s is pinned to nothing", k)
			continue
		}
		if limits.m[k] != want {
			t.Errorf("x-limits.%s is %v, the code's %v", k, limits.m[k], want)
		}
	}
	for k := range pins {
		if _, ok := limits.get(k); !ok {
			t.Errorf("x-limits has no %s", k)
		}
	}
	if v, _ := yat(doc, "components", "parameters", "Limit", "schema", "maximum"); v != float64(MaxLimit) {
		t.Errorf("the Limit parameter's maximum %v", v)
	}
	if v, _ := yat(doc, "components", "parameters", "Limit", "schema", "default"); v != float64(DefaultLimit) {
		t.Errorf("the Limit parameter's default %v", v)
	}
}

// queueConstant reads one of internal/invite's unexported queue constants off its source (the
// package keeps them unexported; the document states them).
func queueConstant(t *testing.T, pattern string) float64 {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "invite", "queue.go"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(pattern).FindAllSubmatch(b, -1)
	if len(m) != 1 {
		t.Fatalf("internal/invite/queue.go: %d matches of %s, want 1", len(m), pattern)
	}
	v, _ := strconv.ParseFloat(string(m[0][1]), 64)
	return v
}

func perMinute(n int, per time.Duration) float64 {
	return float64(n) * float64(time.Minute) / float64(per)
}

// idempotencyHoursInSQL reads the ledger's window off its queries: every interval of
// db/queries/apiidempotency.sql, which must agree.
func idempotencyHoursInSQL(t *testing.T) float64 {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "queries", "apiidempotency.sql"))
	if err != nil {
		t.Fatal(err)
	}
	ms := regexp.MustCompile(`interval '([0-9]+) hours'`).FindAllStringSubmatch(string(b), -1)
	if len(ms) == 0 {
		t.Fatal("anti-vacuity: no interval in apiidempotency.sql")
	}
	for _, m := range ms[1:] {
		if m[1] != ms[0][1] {
			t.Fatalf("apiidempotency.sql's intervals disagree: %v", ms)
		}
	}
	h, _ := strconv.ParseFloat(ms[0][1], 64)
	return h
}

// apiSource parses the package's non-test Go files and collects its string constants.
func apiSource(t *testing.T) (files []*ast.File, consts map[string]string) {
	t.Helper()
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	consts = map[string]string{}
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, af)
		for _, d := range af.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, s := range g.Specs {
				vs := s.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if i < len(vs.Values) {
						if bl, ok := vs.Values[i].(*ast.BasicLit); ok && bl.Kind == token.STRING {
							consts[n.Name], _ = strconv.Unquote(bl.Value)
						}
					}
				}
			}
		}
	}
	if len(files) < 10 || len(consts) < 30 {
		t.Fatalf("anti-vacuity: %d files, %d constants", len(files), len(consts))
	}
	return files, consts
}

// problemDetailsInSource reads the package's non-test Go files: the keys of every details map, and
// for the closed keys (field, reason, parameter, parameters, header) every value -- a string
// literal or a constant, also through invalidField(field, reason, …).
func problemDetailsInSource(t *testing.T) (keys []string, closed map[string][]string) {
	t.Helper()
	files, consts := apiSource(t)
	str := func(e ast.Expr) []string {
		switch x := e.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				s, _ := strconv.Unquote(x.Value)
				return []string{s}
			}
		case *ast.Ident:
			if v, ok := consts[x.Name]; ok {
				return []string{v}
			}
		case *ast.CompositeLit:
			var out []string
			for _, el := range x.Elts {
				if bl, ok := el.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					s, _ := strconv.Unquote(bl.Value)
					out = append(out, s)
				} else if id, ok := el.(*ast.Ident); ok {
					if v, ok := consts[id.Name]; ok {
						out = append(out, v)
					}
				}
			}
			return out
		}
		return nil
	}
	keySet := map[string]bool{}
	values := map[string]map[string]bool{}
	add := func(k string, vs []string) {
		if values[k] == nil {
			values[k] = map[string]bool{}
		}
		for _, v := range vs {
			values[k][v] = true
		}
	}
	isClosed := map[string]bool{"field": true, "reason": true, "parameter": true, "parameters": true, "header": true}
	for _, af := range files {
		ast.Inspect(af, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				mt, ok := x.Type.(*ast.MapType)
				if !ok {
					return true
				}
				if k, ok := mt.Key.(*ast.Ident); !ok || k.Name != "string" {
					return true
				}
				for _, el := range x.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					ks := str(kv.Key)
					if len(ks) != 1 {
						continue
					}
					keySet[ks[0]] = true
					if isClosed[ks[0]] {
						add(ks[0], str(kv.Value))
					}
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "invalidField" && len(x.Args) == 3 {
					keySet["field"], keySet["reason"] = true, true
					add("field", str(x.Args[0]))
					add("reason", str(x.Args[1]))
				}
			}
			return true
		})
	}
	// nameProblem passes its field through: its callers name it.
	for _, f := range []string{fieldFirstName, fieldLastName} {
		add("field", []string{f})
	}
	for k := range keySet {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	closed = map[string][]string{}
	for k, vs := range values {
		for v := range vs {
			closed[k] = append(closed[k], v)
		}
		slices.Sort(closed[k])
	}
	return keys, closed
}

// TestOpenAPI_TheWebhooksAreTheSenders (M11 API-12; ADR 0028 §6): the webhooks section describes
// what the sender sends -- the signature header and its shape (internal/webhook), the user agent and
// content type (delivery.Header), the retry schedule and the tries (delivery.Backoff, MaxAttempts),
// the body (delivery.Body: the punch item plus event and deliveryId, the schema's properties exactly
// the Punch's and those two, each shared property the Punch's own).
func TestOpenAPI_TheWebhooksAreTheSenders(t *testing.T) {
	doc := openAPIDoc(t)
	param := func(name string) *ymap {
		v, _ := yat(doc, "components", "parameters", name)
		return mustMap(t, v, name)
	}
	if v, _ := param("Signature").get("name"); v != webhook.SignatureHeader {
		t.Errorf("the signature parameter is %v, the sender's header %s", v, webhook.SignatureHeader)
	}
	if v, _ := yat(param("Signature"), "schema", "pattern"); v != "^"+webhook.SignaturePrefix+"[0-9a-f]{64}$" {
		t.Errorf("the signature's pattern %v", v)
	}
	hdr := delivery.Header(webhook.SigningKey{}, nil)
	if v, _ := yat(param("WebhookUserAgent"), "schema", "enum", "0"); v != hdr.Get("User-Agent") {
		t.Errorf("the documented user agent %v, the sender's %q", v, hdr.Get("User-Agent"))
	}
	for _, name := range []string{"punch", "ping"} {
		if _, ok := yat(doc, "webhooks", name, "post", "requestBody", "content", hdr.Get("Content-Type")); !ok {
			t.Errorf("webhooks.%s has no %s body", name, hdr.Get("Content-Type"))
		}
	}
	sched, _ := yat(doc, "webhooks", "punch", "post", "x-retry-schedule-seconds")
	var got []float64
	for _, v := range sched.([]any) {
		got = append(got, v.(float64))
	}
	var want []float64
	for _, d := range delivery.Backoff {
		want = append(want, d.Seconds())
	}
	if !slices.Equal(got, want) || len(want)+1 != delivery.MaxAttempts {
		t.Errorf("the documented schedule %v, the sender's %v (tries %d)", got, want, delivery.MaxAttempts)
	}
	if v, _ := yat(doc, "components", "schemas", "PunchDelivery", "properties", "event", "enum", "0"); v != delivery.EventPunch {
		t.Errorf("the documented event %v, the sender's %q", v, delivery.EventPunch)
	}
	if v, _ := yat(doc, "components", "schemas", "PunchDelivery", "properties", "deliveryId", "pattern"); !strings.HasPrefix(fmt.Sprint(v), "^"+delivery.IDPrefix) {
		t.Errorf("the delivery id's pattern %v", v)
	}
	punchProps := mustMap(t, func() any { v, _ := yat(doc, "components", "schemas", "Punch", "properties"); return v }(), "Punch")
	dlvProps := mustMap(t, func() any { v, _ := yat(doc, "components", "schemas", "PunchDelivery", "properties"); return v }(), "PunchDelivery")
	wantKeys := append([]string{"event", "deliveryId"}, punchProps.keys...)
	if !slices.Equal(dlvProps.keys, wantKeys) {
		t.Errorf("PunchDelivery's properties %v, want %v", dlvProps.keys, wantKeys)
	}
	for _, k := range punchProps.keys {
		if canonical(punchProps.m[k]) != canonical(dlvProps.m[k]) {
			t.Errorf("PunchDelivery.%s is not Punch.%s", k, k)
		}
	}
	pReq, _ := yat(doc, "components", "schemas", "Punch", "required")
	dReq, _ := yat(doc, "components", "schemas", "PunchDelivery", "required")
	if canonical(append([]any{"event", "deliveryId"}, pReq.([]any)...)) != canonical(dReq) {
		t.Errorf("PunchDelivery's required %v, Punch's %v", dReq, pReq)
	}
	// The body's JSON names are the documented properties, in the wire struct's own order.
	b, err := json.Marshal(delivery.Body{})
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	var wire []string
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch v := tok.(type) {
		case json.Delim:
			if v == '{' {
				depth++
			} else if v == '}' {
				depth--
			}
		case string:
			if depth == 1 {
				wire = append(wire, v)
				var skip json.RawMessage
				if err := dec.Decode(&skip); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if !slices.Equal(wire, wantKeys) {
		t.Errorf("the sender's body keys %v, the documented %v", wire, wantKeys)
	}
}
