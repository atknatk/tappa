package operator

import (
	"encoding"
	"fmt"
	"log/slog"
	"net/http"
)

// formValue holds a credential or a piece of personal data lifted off an operator form --
// the password, the email address, the TOTP code, the enrollment link's token -- until the
// call that consumes it (OP-6's redacting-type proposal, m10-platform.md OP-4 block, OP-8
// (a)). operatorauth takes these as plain strings (Password, TOTP, CompleteEnrollment),
// and a plain string printed by mistake -- a log attribute, a %v in an error -- prints
// itself. A formValue prints its placeholder under the fmt verbs, slog formats and
// encoding.TextMarshaler that TestFormValue_PrintsNoValue names, and holds the value
// behind a *string (the repository's redacting types: internal/operatorauth/key.go).
// reveal() returns the plain string; a deliberate extraction is not prevented.
// TestFormValues_TheListedSitesAloneRevealOrReadTheForm catches the list in its header.
type formValue struct{ v *string }

const formValueRedacted = "operator.formValue(redacted)"

var (
	_ fmt.Formatter          = formValue{}
	_ fmt.Stringer           = formValue{}
	_ fmt.GoStringer         = formValue{}
	_ slog.LogValuer         = formValue{}
	_ encoding.TextMarshaler = formValue{}
)

// postValue lifts one field of r.PostForm (the body). The form must already be parsed
// (readForm).
func postValue(r *http.Request, name string) formValue {
	v := r.PostForm.Get(name)
	return formValue{v: &v}
}

// reveal is the plain value (formValue's doc comment).
func (f formValue) reveal() string {
	if f.v == nil {
		return ""
	}
	return *f.v
}

func (formValue) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(formValueRedacted)) }
func (formValue) String() string               { return formValueRedacted }
func (formValue) GoString() string             { return formValueRedacted }
func (formValue) LogValue() slog.Value         { return slog.StringValue(formValueRedacted) }
func (formValue) MarshalText() ([]byte, error) { return []byte(formValueRedacted), nil }
