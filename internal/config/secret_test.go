package config_test

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/mail"
)

var (
	keyType        = reflect.TypeFor[config.Key]()
	dsnType        = reflect.TypeFor[config.DSN]()
	credentialType = reflect.TypeFor[mail.Credential]()
)

// redactingTypes are the three types whose values no printer can read.
func redacting(t reflect.Type) bool { return t == keyType || t == dsnType || t == credentialType }

// secretName is the ad half of the gate: a field whose name says it holds a secret must be of a
// redacting type, whatever its Go type is. It matches on purpose more than today's fields need
// (salt, cipher, pepper, url) because the failure mode of a miss is a credential in the log.
var secretName = regexp.MustCompile(`(?i)key|kek|secret|passw|pwd|token|hmac|credential|dsn|database|cipher|salt|pepper|seed|username|url`)

// notASecretByName are the fields secretName matches and that hold no secret, each with the reason.
var notASecretByName = map[string]string{
	// Load refuses a user part in it (T5), so what is left is a public origin.
	"BaseURL": "the public origin",
}

// plainStrings is the closed list of string fields that hold no secret. A new string field is red
// until somebody decides which side it is on, because a secret with an innocent name (`Relay`) is
// the case a name pattern cannot catch.
var plainStrings = map[string]bool{
	"Env": true, "Addr": true, "BaseURL": true, "OperatorHost": true, "APIHost": true,
	"ResetDelivery": true, "InviteDelivery": true, "LogLevel": true, "LogFormat": true,
	"Mail.Host": true,
	// A sender address and a reply-to: public by design, on every mail the product sends.
	"Mail.From": true, "Mail.ReplyTo": true,
}

// harmlessTypes is the closed list of the other types Config may hold. A map, a pointer to a
// string, an interface or a new struct is red until it is classified here or wrapped.
var harmlessTypes = map[reflect.Type]bool{
	reflect.TypeFor[bool]():           true,
	reflect.TypeFor[int]():            true,
	reflect.TypeFor[float64]():        true,
	reflect.TypeFor[time.Duration]():  true,
	reflect.TypeFor[*time.Time]():     true,
	reflect.TypeFor[[]netip.Prefix](): true,
	// A certificate pool holds public keys of trust anchors.
	reflect.TypeFor[*x509.CertPool](): true,
}

// walkConfig calls visit for every leaf of Config, descending into the structs it holds by value
// (mail.Config). The redacting types are leaves though they are structs.
func walkConfig(t reflect.Type, prefix string, visit func(path string, f reflect.StructField)) {
	for i := range t.NumField() {
		f := t.Field(i)
		path := prefix + f.Name
		if f.Type.Kind() == reflect.Struct && !redacting(f.Type) {
			walkConfig(f.Type, path+".", visit)
			continue
		}
		visit(path, f)
	}
}

// configFieldProblem is the gate's rule for one field, apart from the walk so its own table can
// drive it: "" is a pass.
func configFieldProblem(path string, typ reflect.Type) string {
	name := path[strings.LastIndexByte(path, '.')+1:]
	if redacting(typ) {
		return ""
	}
	if _, exempt := notASecretByName[path]; !exempt && secretName.MatchString(name) {
		return "its name says it holds a secret and its type " + typ.String() + " prints the value"
	}
	if typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8 {
		return "a byte slice in Config is a key in the clear"
	}
	if typ.Kind() == reflect.String {
		if !plainStrings[path] {
			return "a new string field is unclassified: wrap it (config.Key, config.DSN) or add it to plainStrings with the reason"
		}
		return ""
	}
	if !harmlessTypes[typ] {
		return "type " + typ.String() + " is unclassified: wrap it or add it to harmlessTypes with the reason"
	}
	return ""
}

// TestConfig_EverySecretFieldIsARedactingType is the gate for the field nobody has written yet: it
// derives the fields from the struct, so a new secret of a printing type is red here before it
// can reach a log (backlog T80, CLAUDE.md §4.7).
//
// CONTROL: the floor below says the walk still sees the secrets it was built for, and the table
// beneath it says the rule reds each shape.
//
// MUTANTS (measured, 2026-11-06, each reverted): a `RelayToken string`, a `BackupKEK []byte` and an
// innocently named `Relay string` added to Config, and a `ProxyPassword string` added to mail.Config:
// each red here.
func TestConfig_EverySecretFieldIsARedactingType(t *testing.T) {
	var keys, dsns, credentials []string
	walkConfig(reflect.TypeFor[config.Config](), "", func(path string, f reflect.StructField) {
		switch f.Type {
		case keyType:
			keys = append(keys, path)
		case dsnType:
			dsns = append(dsns, path)
		case credentialType:
			credentials = append(credentials, path)
		}
		if p := configFieldProblem(path, f.Type); p != "" {
			t.Errorf("Config.%s: %s (backlog T80, CLAUDE.md §4.7)", path, p)
		}
	})
	if len(keys) != 8 || len(dsns) != 2 || len(credentials) != 2 {
		t.Errorf("the walk found keys %v, DSNs %v, credentials %v; it has gone blind or a secret moved "+
			"(today: eight keys, two DSNs, the two SMTP credentials)", keys, dsns, credentials)
	}

	for _, tc := range []struct {
		path string
		typ  reflect.Type
		bad  bool
	}{
		{"RelayToken", reflect.TypeFor[string](), true},
		{"BackupURL", reflect.TypeFor[string](), true},
		{"BackupKEK", reflect.TypeFor[[]byte](), true},
		{"Blob", reflect.TypeFor[[]byte](), true},
		{"Relay", reflect.TypeFor[string](), true},
		{"Mail.ProxyPassword", reflect.TypeFor[string](), true},
		{"Pointer", reflect.TypeFor[*string](), true},
		{"Bag", reflect.TypeFor[map[string]string](), true},
		{"Any", reflect.TypeFor[any](), true},
		{"RelayToken", keyType, false},
		{"BackupURL", dsnType, false},
		{"Mail.Password", credentialType, false},
		{"BaseURL", reflect.TypeFor[string](), false},
		{"Env", reflect.TypeFor[string](), false},
		{"Debounce", reflect.TypeFor[time.Duration](), false},
	} {
		if got := configFieldProblem(tc.path, tc.typ) != ""; got != tc.bad {
			t.Errorf("configFieldProblem(%s, %s) refuses = %v, want %v", tc.path, tc.typ, got, tc.bad)
		}
	}
}

// canaryConfig is a Config whose every secret field holds its own recognisable value, filled by
// reflection so a new secret field is covered on the day it passes the gate above.
func canaryConfig(t *testing.T) (config.Config, []string) {
	t.Helper()
	var c config.Config
	var canaries []string
	var fill func(v reflect.Value)
	n := 0
	fill = func(v reflect.Value) {
		for i := range v.NumField() {
			f := v.Field(i)
			n++
			canary := fmt.Sprintf("CANARY-%02d-fixture-value-%02d!", n, n)
			switch f.Type() {
			case keyType:
				f.Set(reflect.ValueOf(config.NewKey([]byte(canary))))
			case dsnType:
				f.Set(reflect.ValueOf(config.NewDSN("postgres://tappa_app:" + canary + "@db.invalid/tappa?password=" + canary)))
			case credentialType:
				f.Set(reflect.ValueOf(mail.NewCredential(canary)))
			default:
				if f.Kind() == reflect.Struct {
					fill(f)
				}
				continue
			}
			canaries = append(canaries, canary)
		}
	}
	fill(reflect.ValueOf(&c).Elem())
	if len(canaries) != 12 {
		t.Fatalf("%d secret fields were filled, want 12", len(canaries))
	}
	return c, canaries
}

// spellings are the forms a value takes on the way out of a printer: as text, as Go prints a byte
// slice, in hex either case, in base64.
func spellings(canary string) []string {
	decimal := fmt.Sprint([]byte(canary))
	return []string{
		canary,
		decimal[1 : len(decimal)-1],
		strings.Join(strings.Fields(fmt.Sprintf("%#v", []byte(canary))), ""),
		hex.EncodeToString([]byte(canary)),
		strings.ToUpper(hex.EncodeToString([]byte(canary))),
		base64.StdEncoding.EncodeToString([]byte(canary)),
	}
}

func leaked(rendered string, canaries []string) string {
	flat := strings.ReplaceAll(rendered, " ", "")
	for _, c := range canaries {
		for _, s := range spellings(c) {
			if strings.Contains(rendered, s) || strings.Contains(flat, strings.ReplaceAll(s, " ", "")) {
				return s
			}
		}
	}
	return ""
}

func slogRender(jsonForm bool, v any) string {
	var b bytes.Buffer
	var h slog.Handler = slog.NewTextHandler(&b, nil)
	if jsonForm {
		h = slog.NewJSONHandler(&b, nil)
	}
	slog.New(h).Info("m", slog.Any("v", v))
	return b.String()
}

// renderings is every path from a value to text the T80 measurement covered; the caller hands the
// value in the shapes fmt treats differently.
func renderings(v any) map[string]string {
	out := map[string]string{}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%p", "%t", "%e", "%U", "%c", "%w"} {
		out["Sprintf "+verb] = fmt.Sprintf(verb, v)
	}
	// A variable, because the point is a non-error under %w and vet refuses the constant spelling.
	wrap := "wrapped: %w"
	out["Errorf %w"] = fmt.Errorf(wrap, v).Error()
	out["Sprint"], out["Sprintln"] = fmt.Sprint(v), fmt.Sprintln(v)
	if j, err := json.Marshal(v); err == nil {
		out["json"] = string(j)
	} else {
		out["json error"] = err.Error()
	}
	out["slog text"], out["slog json"] = slogRender(false, v), slogRender(true, v)
	return out
}

// TestConfig_NoRenderingCarriesASecret drives the redaction end to end, through every shape the
// measurement behind secret.go found leaking: a Config in an exported and an UNEXPORTED field, a
// pointer in each, inside a slice and a map, %p, %w, and each secret field read out on its own.
//
// CONTROL: TestLeakDetector_SeesARawKey says the detector reds a value that is printed.
//
// MUTANTS (measured, 2026-11-06, each reverted): Key and DSN over a plain string instead of a
// *string (the value prints through an unexported field) -- red; see also the exact-text mutants on
// TestSecretTypes_PrintTheirConstantOnEveryPath.
func TestConfig_NoRenderingCarriesASecret(t *testing.T) {
	c, canaries := canaryConfig(t)
	type holder struct {
		Exported   config.Config
		unexported config.Config
		ptr        *config.Config
	}
	shapes := map[string]any{
		"value":          c,
		"pointer":        &c,
		"holder":         holder{Exported: c, unexported: c, ptr: &c},
		"holder pointer": &holder{Exported: c, unexported: c, ptr: &c},
		"slice":          []config.Config{c},
		"pointer slice":  []*config.Config{&c},
		"map":            map[string]config.Config{"c": c},
	}
	for name, v := range shapes {
		for how, text := range renderings(v) {
			if s := leaked(text, canaries); s != "" {
				t.Errorf("%s under %s carries a secret (%d bytes of it matched)", name, how, len(s))
			}
		}
	}

	// Each field on its own: the path no method on Config could see.
	rv := reflect.ValueOf(c)
	var each func(rv reflect.Value, prefix string)
	each = func(rv reflect.Value, prefix string) {
		for i := range rv.NumField() {
			f, sf := rv.Field(i), rv.Type().Field(i)
			if redacting(sf.Type) {
				for how, text := range renderings(f.Interface()) {
					if s := leaked(text, canaries); s != "" {
						t.Errorf("Config.%s%s under %s carries a secret (%d bytes of it matched)", prefix, sf.Name, how, len(s))
					}
				}
				continue
			}
			if sf.Type.Kind() == reflect.Struct {
				each(f, prefix+sf.Name+".")
			}
		}
	}
	each(rv, "")
}

// TestLeakDetector_SeesARawKey is the positive control for the table above: a detector that saw
// nothing would make it green for the wrong reason.
func TestLeakDetector_SeesARawKey(t *testing.T) {
	const canary = "CANARY-00-fixture-value-00!"
	raw := []byte(canary)
	for _, verb := range []string{"%v", "%s", "%x", "%X", "%q", "%#v"} {
		if leaked(fmt.Sprintf(verb, raw), []string{canary}) == "" {
			t.Errorf("the detector missed a raw key under %s", verb)
		}
	}
	if leaked(base64.StdEncoding.EncodeToString(raw), []string{canary}) == "" {
		t.Error("the detector missed a base64 key")
	}
	if leaked(slogRender(true, raw), []string{canary}) == "" {
		t.Error("the detector missed a raw key through slog's JSON handler")
	}
}

// TestSecretTypes_PrintTheirConstantOnEveryPath holds the redacted form to one fixed text, the zero
// values included: a DSN's form carries no host and no user, a key's form no length.
//
// MUTANTS (measured, 2026-11-06, each reverted): Key.Format, DSN.Format, Key.MarshalText,
// DSN.MarshalText and Key.LogValue each removed in turn -- red.
func TestSecretTypes_PrintTheirConstantOnEveryPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    any
		want string
	}{
		{"a key", config.NewKey([]byte("CANARY-key-CANARY-key-CANARY-key")), "config.Key(redacted)"},
		{"the zero key", config.Key{}, "config.Key(redacted)"},
		{"a DSN", config.NewDSN("postgres://u:" + "CANARY" + "@h/d"), "config.DSN(redacted)"},
		{"the zero DSN", config.DSN{}, "config.DSN(redacted)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
				if got := fmt.Sprintf(verb, tc.v); got != tc.want {
					t.Errorf("Sprintf(%s) = %q, want %q", verb, got, tc.want)
				}
			}
			if got := tc.v.(fmt.Stringer).String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
			if got := tc.v.(fmt.GoStringer).GoString(); got != tc.want {
				t.Errorf("GoString() = %q, want %q", got, tc.want)
			}
			if got := tc.v.(slog.LogValuer).LogValue().String(); got != tc.want {
				t.Errorf("LogValue() = %q, want %q", got, tc.want)
			}
			j, err := json.Marshal(tc.v)
			if err != nil {
				t.Fatal(err)
			}
			if string(j) != `"`+tc.want+`"` {
				t.Errorf("json.Marshal = %s, want the constant", j)
			}
		})
	}
}

// TestSecretTypes_RevealIsTheOneWayOut: what the using paths rely on. The key comes back byte for
// byte as a fresh copy, an empty one reads as unset (an optional key's "unset" is its zero value),
// and writing into a revealed slice or into NewKey's argument reaches nothing.
func TestSecretTypes_RevealIsTheOneWayOut(t *testing.T) {
	in := bytes.Repeat([]byte{0xA5, 0x00, 0xFF}, 11)[:32]
	want := bytes.Clone(in)
	k := config.NewKey(in)
	if !bytes.Equal(k.Reveal(), want) || k.Len() != 32 {
		t.Fatalf("a key does not come back byte for byte (non-UTF-8 bytes included): len %d", k.Len())
	}
	in[0] ^= 0xFF
	if !bytes.Equal(k.Reveal(), want) {
		t.Error("NewKey aliased the caller's slice")
	}
	revealed := k.Reveal()
	revealed[0] ^= 0xFF
	if !bytes.Equal(k.Reveal(), want) {
		t.Error("Reveal handed out the key's own storage")
	}
	for name, zero := range map[string]config.Key{"zero value": {}, "nil": config.NewKey(nil), "empty": config.NewKey([]byte{})} {
		if zero.Len() != 0 || zero.Reveal() != nil {
			t.Errorf("%s: Len = %d, Reveal = %v, want an unset key", name, zero.Len(), zero.Reveal())
		}
	}
	const dsn = "postgres://tappa_app:" + "fixture-value" + "@db.invalid/tappa"
	if got := config.NewDSN(dsn).Reveal(); got != dsn {
		t.Errorf("a DSN does not come back as it went in")
	}
	if got := (config.DSN{}).Reveal(); got != "" || config.NewDSN("").Reveal() != "" {
		t.Errorf("the zero DSN reveals %q", got)
	}
}

// TestLoad_SecretsArriveAndDoNotPrint is the path from the environment: Load hands each secret to
// its reader intact, and the same Config prints none of them.
func TestLoad_SecretsArriveAndDoNotPrint(t *testing.T) {
	setRequired(t)
	const dsnSecret = "not-a-real-password-canary"
	dsn := "postgres://tappa_app:" + dsnSecret + "@db.invalid/tappa"
	t.Setenv("DATABASE_URL", dsn)
	kek := bytes.Repeat([]byte("K"), 32)
	mac := bytes.Repeat([]byte("M"), 32)
	t.Setenv("TAPPA_TAG_KEK", base64.StdEncoding.EncodeToString(kek))
	t.Setenv("TAPPA_SESSION_HMAC_KEY", base64.StdEncoding.EncodeToString(mac))
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseURL.Reveal() != dsn {
		t.Error("the DSN did not reach its reader")
	}
	if !bytes.Equal(c.TagKEK.Reveal(), kek) || !bytes.Equal(c.SessionHMACKey.Reveal(), mac) {
		t.Error("a key did not reach its reader")
	}
	secrets := []string{dsnSecret, string(kek), string(mac)}
	for how, text := range renderings(c) {
		if s := leaked(text, secrets); s != "" {
			t.Errorf("a loaded Config under %s carries a secret (%d bytes of it matched)", how, len(s))
		}
	}
}
