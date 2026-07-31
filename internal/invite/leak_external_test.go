package invite_test

// leak_external_test.go -- the §4.7 leak proof from OUTSIDE the package.
//
// WHY A SEPARATE TEST PACKAGE. internal/session shipped a version of this type
// whose redaction was believed to be structural and was NOT: fmt consults
// Formatter/Stringer only for a value it can hand to an interface, and a value
// read out of an UNEXPORTED struct field cannot be
// (reflect.Value.CanInterface() == false). fmt then falls through to plain
// reflection and prints the field. The in-package test missed it because it only
// wrapped the value in an EXPORTED field. That was an audit RED (M5-01 round 2).
//
// invite.Code copies session.Token's fix (a *string field), so it must copy the
// PROOF too — from the position a real caller occupies. The idiom that broke it is
// completely ordinary Go and this package's own handler writes it.
//
// Every assertion has a POSITIVE CONTROL: the rendering must still contain
// something (the employee name), so an empty string cannot pass vacuously
// (agent-brief: "her negatif teste pozitif kontrol eşlik etmeli").

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/invite"
)

// fakeCodeValue is an obviously fake, searchable stand-in for an activation code.
// It is NOT produced by the package and is not a real secret (agent-brief madde
// 2); it exists so the assertions can grep for something specific. Its length
// matches a real code so nothing takes a different path because of size.
const fakeCodeValue = "FAKEfakeFAKEfakeFAKEfakeFAKEfakeFAKEfake123"

// activationState is the shape that broke session.Token: a caller-local struct
// with UNEXPORTED fields carrying the secret. An activation handler naturally
// writes this, and naturally logs it with %+v when something goes wrong.
type activationState struct {
	employee string
	code     invite.Code
}

// exportedState is the shape the original in-package test used. Keeping both
// side by side is the point: the exported case passed all along and proved
// nothing about the unexported one.
type exportedState struct {
	Employee string
	Code     invite.Code
}

func TestCode_NoLeakThroughUnexportedField(t *testing.T) {
	c := invite.ParseCode(fakeCodeValue)
	unexported := activationState{employee: "maria", code: c}
	exported := exportedState{Employee: "maria", Code: c}

	var slogText, slogJSON bytes.Buffer
	textLog := slog.New(slog.NewTextHandler(&slogText, nil))
	jsonLog := slog.New(slog.NewJSONHandler(&slogJSON, nil))
	textLog.Info("activating", "state", unexported, "delivered", c)
	jsonLog.Info("activating", "state", unexported, "delivered", c)

	jsonBytes, err := json.Marshal(map[string]any{"employee": "maria", "delivered": c})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	wrapped := fmt.Errorf("activation failed for %s: %w", "maria", fmt.Errorf("code %v rejected", c))

	renderings := map[string]string{
		"%v on unexported field":  fmt.Sprintf("%v", unexported),
		"%+v on unexported field": fmt.Sprintf("%+v", unexported),
		"%#v on unexported field": fmt.Sprintf("%#v", unexported),
		"%v on exported field":    fmt.Sprintf("%v", exported),
		"%+v on exported field":   fmt.Sprintf("%+v", exported),
		"%#v on exported field":   fmt.Sprintf("%#v", exported),
		"slice":                   fmt.Sprintf("%v", []invite.Code{c}),
		"map value":               fmt.Sprintf("%v", map[string]invite.Code{"k": c}),
		"pointer":                 fmt.Sprintf("%v", &c),
		"slog text":               slogText.String(),
		"slog json":               slogJSON.String(),
		"encoding/json":           string(jsonBytes),
		"wrapped error":           wrapped.Error(),
	}

	for name, got := range renderings {
		if strings.Contains(got, fakeCodeValue) {
			t.Errorf("%s LEAKED the code", name)
		}
		// Positive control: prefixes of the value must not appear either, and the
		// rendering must not be empty.
		if strings.Contains(got, fakeCodeValue[:8]) {
			t.Errorf("%s leaked a prefix of the code", name)
		}
		if got == "" {
			t.Errorf("%s produced nothing: the assertion above proves nothing", name)
		}
	}

	// The controls that make the whole test meaningful: the renderings that were
	// supposed to carry the employee name really do, and the fake value really is
	// findable when it IS present.
	for _, name := range []string{"%+v on unexported field", "slog text", "encoding/json"} {
		if !strings.Contains(renderings[name], "maria") {
			t.Errorf("%s lost the non-secret field too; the test is not rendering what it thinks", name)
		}
	}
	if !strings.Contains(fmt.Sprintf("%v", struct{ V string }{fakeCodeValue}), fakeCodeValue) {
		t.Fatal("positive control failed: a plain string field does not render the value, so the negative assertions are meaningless")
	}
}

// TestCode_NoExportedAccessor is a compile-time-ish guard expressed as a runtime
// one: the ONLY way to get a string out of a Code from another package is a
// delivery Channel (channel.go). If someone adds a Reveal()/Value()/String()
// that returns the real value, this test's expectations break loudly.
//
// KNOWN LIMIT, stated rather than claimed away: deliberate reflection still
// reads the value, and this test does not pretend otherwise — it demonstrates
// it, so nobody mistakes "redacted" for "unreachable".
func TestCode_KnownLimitIsReflection(t *testing.T) {
	c := invite.ParseCode(fakeCodeValue)
	if got := c.String(); strings.Contains(got, fakeCodeValue) {
		t.Fatal("String() returns the value")
	}
	// Documented escape hatch: this is what a determined developer can still do.
	// It is here so the limit is measured, not assumed.
	v := reflectValue(c)
	if v != fakeCodeValue {
		t.Fatalf("reflection read %q; the documented limit in code.go is wrong and should be updated", v)
	}
}

// reflectValue performs the deliberate reflection read that code.go names as its
// accepted limit. Kept in one clearly-labelled helper so it is obvious this is a
// measurement of the limit and not an idiom to copy.
func reflectValue(c invite.Code) string {
	v := reflect.ValueOf(c).Field(0)
	if v.IsNil() {
		return ""
	}
	return v.Elem().String()
}
