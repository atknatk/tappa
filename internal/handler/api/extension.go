package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/apikey"
)

// EXTENSIONS (M11 API-11, ADR 0028 §11). The sandbox build adds one endpoint to /v1 -- the tap
// simulation, POST /v1/sandbox/taps -- and its code is NOT in this package: it lives in
// internal/sandbox, which only cmd/tappa's `sandbox`-tagged file imports, so the production binary
// does not contain it (cmd/tappa's TestPackaging_ProdBuildHasNoSandboxPackage). What this package
// offers is the place: an Extension mounts into the same two groups the shipped endpoints use, so
// an extension's routes pass the same host gate, headers, body bound and guard, the same buckets,
// the same Bearer chain (K-22's 403 included) and the same write-query rule, and its writes run
// through the same write path -- Idempotency-Key, the runner's transaction, the suspension
// checkpoint and the error contract. Nothing here knows what a simulation is.
//
// The production surface has no extension: cmd/tappa's untagged build passes none, and the OpenAPI
// test (TestOpenAPI_PathsAreTheRoutersAndTheErrorIsSection8) reads that surface.

// Extension is a group of routes another package adds to /v1.
type Extension interface {
	// MountPublic registers routes beside openapi.yaml: after the host gate and the address bucket,
	// with no key.
	MountPublic(r chi.Router)
	// MountAuthenticated registers routes after the whole Bearer chain and the write-query rule.
	// write is the surface's own write path (Surface.write).
	MountAuthenticated(r chi.Router, write WriteFunc)
}

// WriteFunc is the surface's write path as an extension calls it: it runs work under the
// Idempotency-Key, the runner and the checkpoint, and answers the request itself when ok is false.
type WriteFunc func(w http.ResponseWriter, r *http.Request, body []byte, work Work) (out apikey.Outcome, replayed, ok bool)

// WriteProblem answers with p (the error contract; ADR 0028 §8).
func WriteProblem(w http.ResponseWriter, p *Problem) { writeProblem(w, p) }

// ReadBody reads the whole (bounded) body: over MaxBody is the body_too_large problem.
func ReadBody(r *http.Request) ([]byte, *Problem) { return readBody(r) }

// Unavailable is the 503 problem every outage answers with -- a copy, so a caller cannot change the
// surface's own.
func Unavailable() *Problem {
	p := *problemUnavailable
	return &p
}

// NoQuery is the 400 problem of a request that carries a query string its endpoint does not take
// (details.allowed empty) -- a copy.
func NoQuery() *Problem {
	p := *problemNoQuery
	return &p
}

// NotFound is the 404 problem with details -- server constants only, never input (errors.go).
func NotFound(message string, details map[string]any) *Problem {
	return &Problem{Code: CodeNotFound, Message: message, Details: details}
}

// InvalidField is a 422 validation_failed naming the field and the reason.
func InvalidField(field, reason, message string) *Problem {
	return invalidField(field, reason, message)
}

// The validation reasons an extension may answer with (the closed set's, locations.go).
const (
	ReasonRequired = reasonRequired
	ReasonType     = reasonType
	ReasonInvalid  = reasonInvalid
)

// ParseObject reads a request body that must be exactly one JSON object whose keys are all in
// allowed, by the rules every /v1 body follows (locations.go, employees.go): UTF-8, one object and
// nothing after it, no key twice, no key outside allowed (matched exactly, never case-folded). A
// refusal names the allowed set, never what the request sent.
func ParseObject(body []byte, allowed []string) (map[string]json.RawMessage, *Problem) {
	if !utf8.Valid(body) {
		return nil, problemNotUTF8
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	raw, duplicate, ok := readObject(dec)
	if !ok {
		return nil, problemLocationBody
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, problemLocationBody
	}
	if duplicate {
		return nil, problemDuplicateField
	}
	for k := range raw {
		if !slices.Contains(allowed, k) {
			return nil, &Problem{Code: CodeValidationFailed, Message: "The body carries a field this endpoint does not accept.",
				Details: map[string]any{"reason": reasonUnknownField, "allowed": slices.Clone(allowed)}}
		}
	}
	return raw, nil
}

// StringField reads raw[key] as a JSON string: absent is (_, false, nil); null or another type is
// the field's type problem; an unpaired surrogate escape is the UTF-8 problem.
func StringField(raw map[string]json.RawMessage, key string) (string, bool, *Problem) {
	v, ok := raw[key]
	if !ok {
		return "", false, nil
	}
	var s string
	if bytes.Equal(bytes.TrimSpace(v), []byte("null")) || json.Unmarshal(v, &s) != nil {
		return "", true, invalidField(key, reasonType, key+" must be a string.")
	}
	if brokenEscape(v, s) {
		return "", true, problemNotUTF8
	}
	return s, true, nil
}
