package mail_test

// leak_external_test.go — the §4.7 leak proofs from OUTSIDE the package, where a
// real caller stands (the invite.Code precedent: internal/invite/
// leak_external_test.go). Two values must never be rendered: the SMTP password,
// and whatever the relay writes in a refusal (SES's sandbox refusal names the
// recipient address; a refusal can quote the link). Every negative assertion has
// a positive control showing the value really was there to leak.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"net/textproto"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/mail"
)

// renderErr is every way a caller plausibly turns a send error into text.
func renderErr(err error) map[string]string {
	var slogText, slogJSON bytes.Buffer
	slog.New(slog.NewTextHandler(&slogText, nil)).Error("send failed", "err", err, "employee_id", "e-1")
	slog.New(slog.NewJSONHandler(&slogJSON, nil)).Error("send failed", "err", err, "employee_id", "e-1")
	j, jerr := json.Marshal(map[string]any{"err": err, "employee_id": "e-1"})
	if jerr != nil {
		j = []byte(jerr.Error())
	}
	out := map[string]string{
		"Error()":         err.Error(),
		"%v":              fmt.Sprintf("%v", err),
		"%+v":             fmt.Sprintf("%+v", err),
		"%#v":             fmt.Sprintf("%#v", err),
		"%s":              fmt.Sprintf("%s", err),
		"%q":              fmt.Sprintf("%q", err),
		"wrapped %w":      fmt.Errorf("panel: could not issue the invitation: %w", err).Error(),
		"slog text":       slogText.String(),
		"slog json":       slogJSON.String(),
		"encoding/json":   string(j),
		"struct with err": fmt.Sprintf("%+v", struct{ Err error }{err}),
	}
	var se *mail.SendError
	if errors.As(err, &se) {
		out["%#v of the value"] = fmt.Sprintf("%#v", *se)
		out["%+v of the value"] = fmt.Sprintf("%+v", *se)
	}
	return out
}

// TestSendError_CarriesNoServerText: a relay refusal that names the recipient
// address and carries a link with its query value, at each step that can refuse,
// renders in none of renderErr's forms with the address, its local part, the
// link value (or its first 8 characters) or the relay's prose; and the
// *textproto.Error is unreachable through errors.Unwrap/As.
func TestSendError_CarriesNoServerText(t *testing.T) {
	victim := "victim.person@example.test"
	mark := rand.Text() // stands for the link's query value; drawn at run time
	link := "https://taptime.test/reset?t=" + mark
	prose := "identities failed the check in region EU-CENTRAL-1"
	refusal := " <" + victim + "> " + prose + " " + link
	tests := []struct {
		name  string
		cfg   fakeConfig
		class mail.Class
		code  int
	}{
		{"RCPT 550", fakeConfig{rcptReply: "550 5.1.1" + refusal}, mail.ClassRejected, 550},
		{"RCPT 550 multi-line", fakeConfig{rcptReply: "550-5.1.1" + refusal + "\n550 5.1.1 " + victim}, mail.ClassRejected, 550},
		{"end of data 554 (SES sandbox shape)", fakeConfig{endReply: "554 Message rejected: Email address is not verified. The following" + refusal}, mail.ClassRejected, 554},
		{"MAIL 553", fakeConfig{mailReply: "553 5.7.1" + refusal}, mail.ClassRejected, 553},
		{"AUTH 535", fakeConfig{authReply: "535 5.7.8" + refusal}, mail.ClassAuth, 535},
		{"greeting 554", fakeConfig{greeting: "554 5.7.1" + refusal}, mail.ClassRejected, 554},
		{"RCPT 451 twice", fakeConfig{rcptReply: "451 4.7.1" + refusal}, mail.ClassThrottled, 451},
	}
	forbidden := []string{victim, "victim.person", mark, mark[:8], prose, "EU-CENTRAL-1", "taptime.test/reset"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake(t, always(tt.cfg))
			s, _ := newSender(t, f, nil)
			m := validMessage()
			m.To = victim
			_, _, err := sendWithin(context.Background(), t, 10*time.Second, s, m)
			sendError(t, err, tt.class, tt.code)

			// Positive control 1: the relay really wrote the address and the link.
			sess := f.session(0)
			wrote := strings.Join(sess.replies, "\n")
			if !strings.Contains(wrote, victim) || !strings.Contains(wrote, mark) {
				t.Fatal("control: the fake did not send the refusal text, so the test proves nothing")
			}
			for name, got := range renderErr(err) {
				if got == "" {
					t.Errorf("%s rendered nothing: the assertions below prove nothing", name)
				}
				for _, v := range forbidden {
					if strings.Contains(got, v) {
						t.Errorf("%s LEAKED %q: %s", name, v, got)
					}
				}
			}
			if errors.Unwrap(err) != nil {
				t.Error("SendError unwraps to something")
			}
			var te *textproto.Error
			if errors.As(err, &te) {
				t.Error("errors.As reaches a *textproto.Error through the SendError")
			}
		})
	}

	// Positive control 2: the same refusal through bare net/smtp yields an error
	// whose text carries both — the fixture can leak, and SendError is what stops it.
	f := newFake(t, always(fakeConfig{rcptReply: "550 5.1.1" + refusal}))
	c, err := smtp.Dial(net.JoinHostPort("127.0.0.1", strconv.Itoa(f.port())))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.StartTLS(&tls.Config{ServerName: "127.0.0.1", RootCAs: f.pool, MinVersion: tls.VersionTLS12}); err != nil {
		t.Fatal(err)
	}
	if err := c.Mail("no-reply@taptime.test"); err != nil {
		t.Fatal(err)
	}
	raw := c.Rcpt(victim)
	if raw == nil || !strings.Contains(raw.Error(), victim) || !strings.Contains(raw.Error(), mark) {
		t.Fatalf("control: net/smtp's own error does not carry the text (%v)", raw)
	}
}

// TestSendError_HasOnlyAClassAndACode pins the type that makes the claim above
// structural: exactly the fields Class (mail.Class) and SMTPCode (int), and a
// method set of exactly {Error} — no Unwrap, Is, As, Format or anything that
// could hand back a reply. Adding a text field or an Unwrap turns it red.
func TestSendError_HasOnlyAClassAndACode(t *testing.T) {
	typ := reflect.TypeOf(mail.SendError{})
	var fields []string
	for i := range typ.NumField() {
		fields = append(fields, typ.Field(i).Name+" "+typ.Field(i).Type.String())
	}
	if got := strings.Join(fields, ", "); got != "Class mail.Class, SMTPCode int" {
		t.Fatalf("SendError fields = %s", got)
	}
	if typ.Field(0).Type.Kind() != reflect.String {
		t.Fatal("Class is no longer a string kind")
	}
	for _, tt := range []struct {
		typ  reflect.Type
		want string
	}{
		{reflect.TypeOf(&mail.SendError{}), "Error"},
		{reflect.TypeOf(mail.SendError{}), ""},
	} {
		var methods []string
		for i := range tt.typ.NumMethod() {
			methods = append(methods, tt.typ.Method(i).Name)
		}
		sort.Strings(methods)
		if got := strings.Join(methods, " "); got != tt.want {
			t.Fatalf("method set of %v = %q, want %q", tt.typ, got, tt.want)
		}
	}
}

// credHolder is the shape that broke session.Token (M5-01): a caller-local struct
// with UNEXPORTED fields carrying the secret, printed with %+v when something
// goes wrong. credHolderExported is the shape that passed all along.
type credHolder struct {
	name string
	p    mail.Credential
	cfg  mail.Config
	smtp *mail.SMTP
}

type credHolderExported struct {
	Name string
	P    mail.Credential
	Cfg  mail.Config
}

// TestCredential_IsRedactedInEveryRendering: the SMTP password and username are
// drawn at run time, and in each rendering listed below neither value nor its
// first 8 characters appears: String, GoString, %v %+v %#v %s %q %x %d, a
// pointer, a slice, a map, a Config (%+v, %#v), the built *SMTP and SMTP (%+v,
// %#v — there the unexported fields print as a pointer ADDRESS, {v:0xc...}, which
// is the indirection working, not the placeholder), a caller's struct with
// unexported and with exported fields, slog's text and JSON handlers and
// encoding/json. The controls show each rendering is non-empty and still renders
// the non-secret neighbour, and that slog resolves a Credential to the
// placeholder string.
func TestCredential_IsRedactedInEveryRendering(t *testing.T) {
	pw := rand.Text()
	user := strings.ToLower(rand.Text())
	p := mail.NewCredential(pw)
	cfg := mail.Config{Host: "smtp.example.test", Port: 587, Username: mail.NewCredential(user), Password: p,
		From: "Taptime <no-reply@taptime.test>"}
	s, err := mail.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := credHolder{name: "maria", p: p, cfg: cfg, smtp: s}
	he := credHolderExported{Name: "maria", P: p, Cfg: cfg}

	var slogText, slogJSON bytes.Buffer
	for _, l := range []*slog.Logger{
		slog.New(slog.NewTextHandler(&slogText, nil)),
		slog.New(slog.NewJSONHandler(&slogJSON, nil)),
	} {
		l.Info("smtp", "p", p, "cfg", cfg, "smtp", s, "holder", h, "exported", he, "name", "maria")
	}
	j, err := json.Marshal(map[string]any{"p": p, "cfg": cfg, "exported": he, "name": "maria"})
	if err != nil {
		t.Fatal(err)
	}
	inSlice := []mail.Credential{p}
	inMap := map[string]mail.Credential{"k": p}
	renderings := map[string]string{
		"String()":                p.String(),
		"GoString()":              p.GoString(),
		"%v":                      fmt.Sprintf("%v", p),
		"%+v":                     fmt.Sprintf("%+v", p),
		"%#v":                     fmt.Sprintf("%#v", p),
		"%s":                      fmt.Sprintf("%s", p),
		"%q":                      fmt.Sprintf("%q", p),
		"%x":                      fmt.Sprintf("%x", p),
		"%d":                      fmt.Sprintf("%d", p),
		"pointer":                 fmt.Sprintf("%v", &p),
		"slice":                   fmt.Sprintf("%v", inSlice),
		"map value":               fmt.Sprintf("%v", inMap),
		"Config %+v":              fmt.Sprintf("%+v", cfg),
		"Config %#v":              fmt.Sprintf("%#v", cfg),
		"*SMTP %+v":               fmt.Sprintf("%+v", s),
		"SMTP %+v":                fmt.Sprintf("%+v", *s),
		"SMTP %#v":                fmt.Sprintf("%#v", *s),
		"%+v on unexported field": fmt.Sprintf("%+v", h),
		"%#v on unexported field": fmt.Sprintf("%#v", h),
		"%+v on exported field":   fmt.Sprintf("%+v", he),
		"slog text":               slogText.String(),
		"slog json":               slogJSON.String(),
		"encoding/json":           string(j),
	}
	for name, got := range renderings {
		if got == "" {
			t.Errorf("%s rendered nothing", name)
		}
		for _, v := range []string{pw, pw[:8], user, user[:8]} {
			if strings.Contains(got, v) {
				t.Errorf("%s LEAKED a credential (or its first 8 characters): %s", name, got)
			}
		}
	}
	for _, name := range []string{"%+v on unexported field", "%+v on exported field", "slog text", "slog json", "encoding/json"} {
		if !strings.Contains(renderings[name], "maria") {
			t.Errorf("%s lost the non-secret neighbour: the test is not rendering what it thinks", name)
		}
	}
	if !strings.Contains(fmt.Sprintf("%v", struct{ V string }{pw}), pw) {
		t.Fatal("control: a plain string field does not render the value, so the assertions above are meaningless")
	}
	// slog's own handlers would still redact without LogValue (text goes through
	// Format, JSON through MarshalText), so that method is pinned on its contract:
	// a handler that resolves the value — and may then encode it by reflection,
	// as third-party handlers do — receives the placeholder STRING, not the struct.
	if v := slog.AnyValue(p).Resolve(); v.Kind() != slog.KindString || v.String() != p.String() {
		t.Fatalf("slog resolves the credential to kind %v, not the placeholder string", v.Kind())
	}
}

// TestCredential_KnownLimitIsReflection measures the limit credential.go names:
// deliberate reflection reads the value. It is here so the limit is a
// measurement, not an assumption — and so a change that makes it "unreachable"
// is noticed and the comment updated.
func TestCredential_KnownLimitIsReflection(t *testing.T) {
	pw := rand.Text()
	v := reflect.ValueOf(mail.NewCredential(pw)).Field(0)
	if v.IsNil() || v.Elem().String() != pw {
		t.Fatal("reflection no longer reads the value; credential.go's stated limit is wrong")
	}
	if !reflect.ValueOf(mail.Credential{}).Field(0).IsNil() {
		t.Fatal("the zero Credential holds a value")
	}
}

// echoMessage is a Message whose To, link value and greeting can be searched for.
func echoMessage() (m mail.Message, value string) {
	value = (rand.Text() + rand.Text())[:43]
	m = validMessage()
	m.Text = "Hello Maria,\nhttps://taptime.test/activate?code=" + value + "\n"
	m.HTML = `<p>Hello Maria,</p><a href="https://taptime.test/activate?code=` + value + `">x</a>`
	return m, value
}

// TestMessage_PrintsAsAPlaceholder: a Message printed by fmt (every verb listed,
// String, GoString, a pointer, a slice, a caller's struct with an EXPORTED
// field), slog's text and JSON handlers and encoding/json carries neither the
// recipient nor the link value (nor its first 8 characters) nor the greeting;
// each rendering shows the placeholder instead, and slog resolves a Message to
// the placeholder string.
func TestMessage_PrintsAsAPlaceholder(t *testing.T) {
	m, value := echoMessage()
	var slogText, slogJSON bytes.Buffer
	slog.New(slog.NewTextHandler(&slogText, nil)).Info("send", "m", m)
	slog.New(slog.NewJSONHandler(&slogJSON, nil)).Info("send", "m", m)
	j, err := json.Marshal(map[string]any{"m": m})
	if err != nil {
		t.Fatal(err)
	}
	inSlice := []mail.Message{m}
	exported := struct{ M mail.Message }{m}
	renderings := map[string]string{
		"String()":           m.String(),
		"GoString()":         m.GoString(),
		"%v":                 fmt.Sprintf("%v", m),
		"%+v":                fmt.Sprintf("%+v", m),
		"%#v":                fmt.Sprintf("%#v", m),
		"%s":                 fmt.Sprintf("%s", m),
		"%q":                 fmt.Sprintf("%q", m),
		"%x":                 fmt.Sprintf("%x", m),
		"%d":                 fmt.Sprintf("%d", m),
		"pointer":            fmt.Sprintf("%+v", &m),
		"slice":              fmt.Sprintf("%v", inSlice),
		"exported field %+v": fmt.Sprintf("%+v", exported),
		"slog text":          slogText.String(),
		"slog json":          slogJSON.String(),
		"encoding/json":      string(j),
	}
	for name, got := range renderings {
		if !strings.Contains(got, "Message(redacted)") {
			t.Errorf("%s does not show the placeholder: %s", name, got)
		}
		for _, v := range []string{m.To, value, value[:8], "Hello Maria"} {
			if strings.Contains(got, v) {
				t.Errorf("%s LEAKED %q: %s", name, v, got)
			}
		}
	}
	// The LogValuer contract, pinned on its own (slog's text and JSON handlers
	// would still print the placeholder through Format and MarshalText): a
	// handler that resolves the value receives the placeholder STRING.
	if v := slog.AnyValue(m).Resolve(); v.Kind() != slog.KindString || v.String() != m.String() {
		t.Fatalf("slog resolves a Message to kind %v, not the placeholder string", v.Kind())
	}
}

// TestMessage_KnownLimitIsAnUnexportedField measures the limit mail.Message's
// doc comment names: read out of an UNEXPORTED field of the caller's struct, a
// Message cannot reach its own methods, and fmt prints its fields — the
// recipient and the link included. A Message is not to be logged; this test is
// here so nobody mistakes the redaction for a guarantee.
func TestMessage_KnownLimitIsAnUnexportedField(t *testing.T) {
	m, value := echoMessage()
	got := fmt.Sprintf("%+v", struct{ m mail.Message }{m})
	if !strings.Contains(got, m.To) || !strings.Contains(got, value) {
		t.Fatalf("an unexported Message field no longer prints its fields (%q); mail.Message's stated limit is wrong", got)
	}
}
