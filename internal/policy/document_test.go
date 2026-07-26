package policy

import (
	"errors"
	"testing"
)

// validJSON is a well-formed baseline-style document reused across parse tests.
const validJSON = `{
  "version": "2026-07-24",
  "name": "GPS-only taps need review",
  "statements": [
    {
      "sid": "gps-only-review",
      "effect": "review",
      "action": ["tap:record"],
      "resource": ["location/*"],
      "condition": { "Bool": { "tap:ipMatch": false, "tap:gpsMatch": true } },
      "reason": "verified via GPS only — no network proof of place"
    }
  ]
}`

func TestParse_Valid(t *testing.T) {
	doc, err := Parse([]byte(validJSON), DefaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.Version != "2026-07-24" || doc.Name == "" || len(doc.Statements) != 1 {
		t.Fatalf("parsed document mismatch: %+v", doc)
	}
	st := doc.Statements[0]
	if st.Sid != "gps-only-review" || st.Effect != EffectReview {
		t.Fatalf("statement mismatch: %+v", st)
	}
	if len(st.Action) != 1 || st.Action[0] != ActionTapRecord {
		t.Fatalf("action mismatch: %+v", st.Action)
	}
	// The Bool condition must survive parsing with its two predicates.
	args, ok := st.Condition[OpBool]
	if !ok || len(args) != 2 {
		t.Fatalf("condition mismatch: %+v", st.Condition)
	}
}

func TestParseAndValidate_Valid(t *testing.T) {
	if _, err := ParseAndValidate([]byte(validJSON), LayerBaseline, DefaultLimits()); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
}

// TestParseAndValidate_NullIsEmpty proves `null` — syntactically valid JSON that
// Parse accepts as a zero Document — is caught by Validate, not silently passed.
func TestParseAndValidate_NullIsEmpty(t *testing.T) {
	if _, err := Parse([]byte(`null`), DefaultLimits()); err != nil {
		t.Fatalf("Parse should accept syntactically-valid null: %v", err)
	}
	if _, err := ParseAndValidate([]byte(`null`), LayerTenant, DefaultLimits()); err == nil {
		t.Fatal("ParseAndValidate accepted an empty (null) document")
	}
}

func TestParse_ByteLimit(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxDocumentBytes = len(validJSON) // exactly at the limit

	if _, err := Parse([]byte(validJSON), lim); err != nil {
		t.Fatalf("document exactly at the byte limit was rejected: %v", err)
	}

	// One byte over: pad with a trailing space (still valid JSON shape-wise, but
	// the size guard must fire before decoding).
	over := validJSON + " "
	lim.MaxDocumentBytes = len(over) - 1
	_, err := Parse([]byte(over), lim)
	if err == nil {
		t.Fatal("document one byte over the limit was accepted")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Field != fieldDocument {
		t.Fatalf("want document-size ValidationError, got %v", err)
	}
}

func TestParse_Malformed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"truncated", `{"version":"1","name":"x","statements":[`},
		{"not an object", `["a","b"]`},
		{"garbage", `{@!#`},
		{"unknown field", `{"version":"1","name":"x","statements":[],"bogus":true}`},
		{"typoed field silently drops effect", `{"version":"1","name":"x","statements":[{"sid":"s","efect":"allow"}]}`},
		{"trailing data", validJSON + ` {"more":1}`},
		{"wrong type for statements", `{"version":"1","name":"x","statements":"nope"}`},
		{"condition not object", `{"version":"1","name":"x","statements":[{"sid":"s","effect":"allow","action":["tap:record"],"resource":["*"],"condition":"nope"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.raw), DefaultLimits()); err == nil {
				t.Fatalf("malformed input %q was accepted", tc.raw)
			}
		})
	}
}

// FuzzParse asserts Parse never panics on arbitrary bytes and that anything it
// accepts is a well-formed Document (same robustness contract as sun.FuzzParse).
// Short round: go test -run=xxx -fuzz=FuzzParse -fuzztime=20s ./internal/policy/...
func FuzzParse(f *testing.F) {
	f.Add([]byte(validJSON))
	f.Add([]byte(``))
	f.Add([]byte(`null`))
	f.Add([]byte(`{`))
	f.Add([]byte(`{"version":"1","name":"n","statements":[]}`))
	f.Add([]byte(`{"statements":[{"sid":"s","effect":"deny","action":["x"],"resource":["*"],"condition":{"Bool":{"k":true}}}]}`))
	f.Add([]byte(`{"version":"1","name":"n","statements":[{"condition":{"IpInPrefix":{"k":["zz"]}}}]}`))
	f.Add([]byte(`[1,2,3]`))

	f.Fuzz(func(t *testing.T, raw []byte) {
		doc, err := Parse(raw, DefaultLimits())
		if err != nil {
			return // rejected inputs prove nothing beyond "did not panic"
		}
		// Accepted: Validate must also not panic on it, whatever the verdict.
		_ = Validate(doc, LayerTenant, DefaultLimits())
	})
}
