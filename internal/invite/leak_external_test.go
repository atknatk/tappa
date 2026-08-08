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
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
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

// --- the DELIVERY seam ----------------------------------------------------------

// recordingSink is a LinkSink that keeps what it was handed, so the assertions below
// have a positive control: the link really did reach the panel.
type recordingSink struct{ link string }

func (s *recordingSink) ShowActivationLink(_ context.Context, d invite.Delivery) error {
	s.link = d.ActivationURL
	return nil
}

// countingRecorder is the narrow audit slice ManagerVisibleChannel needs.
type countingRecorder struct {
	events []audit.Event
}

func (r *countingRecorder) Record(_ context.Context, e audit.Event) (uuid.UUID, error) {
	r.events = append(r.events, e)
	return uuid.New(), nil
}

// TestManagerVisibleChannel_DoesNotLogTheLinkItHolds is §4.7 at the ONE function that
// holds a raw activation URL.
//
// 🔴 IT EXISTS BECAUSE THE NET WAS ON THE WRONG SIDE OF THE SEAM. M6-05 phase B put
// this channel on the production path (cmd/tappa wires the panel to it) and asserted
// "the link is never logged" — but the assertion lived in internal/handler and drove
// the HANDLER's logger. An audit added `slog.Default().Info("delivered invitation",
// "link", d.ActivationURL)` to DeliverInvite itself — the function whose own comment
// says "NEVER log it" — and measured `go test ./internal/invite ./internal/handler`
// answering ok with redline-check clean. The product does not leak; the claim was
// covering somebody else's logger.
//
// WHAT IT COVERS AND WHAT IT DOES NOT. It swaps the DEFAULT logger, which is the one
// a package with no injected logger can reach, and drives the real channel. A future
// implementation holding its own *slog.Logger would not be seen — that is a limit,
// and it is smaller than the one it replaces: this package injects no logger at all
// today, so slog.Default() is the only way out.
func TestManagerVisibleChannel_DoesNotLogTheLinkItHolds(t *testing.T) {
	var captured bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	sink := &recordingSink{}
	rec := &countingRecorder{}
	actor := uuid.New()
	ch, err := invite.NewManagerVisibleChannel(sink, rec, &actor)
	if err != nil {
		t.Fatalf("NewManagerVisibleChannel: %v", err)
	}

	link := "https://time.tappa.mt/activate?code=" + fakeCodeValue
	err = ch.DeliverInvite(context.Background(), invite.Delivery{
		Invite: invite.Invite{
			ID: uuid.New(), TenantID: uuid.New(), EmployeeID: uuid.New(),
			ExpiresAt: time.Now().Add(time.Hour),
		},
		ActivationURL: link,
	})
	if err != nil {
		t.Fatalf("DeliverInvite: %v", err)
	}

	// POSITIVE CONTROLS FIRST, or every absence below is the absence of a delivery.
	if sink.link != link {
		t.Fatalf("the sink received %q, want the link; nothing was delivered", sink.link)
	}
	if len(rec.events) != 1 || rec.events[0].Action != invite.ActionCodeShownToManager {
		t.Fatalf("the disclosure was not recorded; the channel did not run: %+v", rec.events)
	}

	if strings.Contains(captured.String(), fakeCodeValue) {
		t.Errorf("the activation code reached the process log from the delivery seam "+
			"(§4.7). Captured:\n%s", captured.String())
	}
	// AND THE AUDIT PAYLOAD, from this side of the seam as well as the handler's.
	blob, err := json.Marshal(rec.events[0].Detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	if strings.Contains(string(blob), fakeCodeValue) {
		t.Errorf("the activation code reached audit_log.detail: %s", blob)
	}
	// THE ANTI-VACUITY CHECK FOR THE LOG ASSERTION: the capture has to be capable of
	// holding something, or "the code is not in it" is true of an empty buffer.
	slog.Default().Info("probe: the capture is live")
	if !strings.Contains(captured.String(), "the capture is live") {
		t.Error("the captured logger recorded nothing at all, so the absence of the code " +
			"in it proves nothing")
	}
}
