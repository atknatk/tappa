// Package operatorauth_test is an EXTERNAL test package on purpose (the OP-6
// acceptance "sızıntı testleri harici pakette"): M5-01 produced two REDs from a "no
// caller can print this" claim tested from INSIDE the package, where the test never
// wrote the struct shapes a real caller writes. Everything below is what
// internal/handler (OP-8) or cmd/opadmin (OP-9) can actually do with these types.
package operatorauth_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	gotoken "go/token"
	"go/types"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
)

// The fakes are obviously fake, searchable stand-ins, each shaped like the value it
// imitates. They are produced by nothing and protect nothing.
const (
	fakeSession   = "FAKEopFAKEopFAKEopFAKEopFAKEopFAKEopFAKEo12" // 43 characters, a session token's shape
	fakeChallenge = "FAKEchallengeFAKEchallengeFAKE.FAKEmacFAKEmac"
	fakeBlob      = "FAKEpendingblobFAKEpendingblobFAKEpendingblob"
	fakeDigest    = "$2a$12$FAKEdigestFAKEdigestFAKEdigestFAKEdigestFAKEdigestFAKE"
)

// fakeEnvelope is a searchable 48-byte "envelope" -- the operator envelope's size.
var fakeEnvelope = []byte("FAKEenvelopeFAKEenvelopeFAKEenvelopeFAKEenvelope")

// specimens builds one POPULATED value of each type of the CLOSED TYPE SET that carries
// a secret, through the doors an external caller has. The set is not chosen here:
// TestExportedTypes_EveryOneIsASpecimenOrANamedException derives it from this package's
// exported API with exact types (apiTypes) and requires each type to be a specimen or a
// named exception (exceptions); TestSpecimens_SearchEveryRedactedValueTheyHold requires
// each specimen to SEARCH every redacted value it holds. A zero value would make every
// "does not leak" assertion trivially true (adminauth's leak test learned that).
type specimen struct {
	name  string
	value any
	// secrets are the raw values the specimen holds; each is searched in every form a
	// leak of it takes (secretForms).
	secrets [][]byte
	// extra are further searched texts: a value derived from a secret (a hash).
	extra []string
	// placeholder is what a REDACTING type prints instead of itself and what each of
	// its five methods returns; "" for a container whose redacting fields speak for it.
	placeholder string
	// unsearched names, by path, a redacted value the specimen HOLDS but its secrets do
	// not include, with the reason (TestSpecimens_SearchEveryRedactedValueTheyHold).
	unsearched map[string]string
}

// byteForms are the verb-independent forms a leaked secret takes: raw, hex (both
// cases), fmt's decimal list (%v, and every badVerb re-print), fmt's Go-syntax list
// (%#v), %q's escaped text, a JSON string's inside -- encoding/json's (HTML escaped)
// and slog's JSON handler's (not: the bytes control measured the difference on a
// random secret with '<', '>' or '&' in it) -- and encoding/json's base64. The decimal
// list is the 5th audit's measurement: a key printed that way carries none of the raw
// or hex forms.
func byteForms(b []byte) []string {
	dec := fmt.Sprint(b)
	gos := fmt.Sprintf("%#v", b)
	q := strconv.Quote(string(b))
	j, _ := json.Marshal(string(b))
	var raw bytes.Buffer
	enc := json.NewEncoder(&raw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(string(b)); err != nil {
		panic(err) // a string always encodes
	}
	jr := strings.TrimSpace(raw.String())
	return []string{string(b), hex.EncodeToString(b), strings.ToUpper(hex.EncodeToString(b)), dec[1 : len(dec)-1],
		gos[strings.Index(gos, "{")+1 : len(gos)-1], q[1 : len(q)-1], string(j[1 : len(j)-1]), jr[1 : len(jr)-1],
		base64.StdEncoding.EncodeToString(b)}
}

// verbForms are the forms a leaked secret takes under ONE verb: the verb applied to the
// bytes (a slice's brackets dropped) and to the text. %c of a []byte, for one, is
// "[F A K E …]", which carries none of byteForms (the 7th round's bytes control
// measured it).
func verbForms(verb string, b []byte) []string {
	bs := fmt.Sprintf(verb, b)
	if strings.HasPrefix(bs, "[") && strings.HasSuffix(bs, "]") {
		bs = bs[1 : len(bs)-1]
	}
	return []string{bs, fmt.Sprintf(verb, string(b))}
}

// secretForms is everything the search looks for on a path printed with verb ("" for
// a path without one).
func (s specimen) secretForms(verb string) []string {
	out := append([]string(nil), s.extra...)
	for _, b := range s.secrets {
		out = append(out, byteForms(b)...)
		if verb != "" {
			out = append(out, verbForms(verb, b)...)
		}
	}
	return out
}

// specimenStore is a Store no specimen ever calls: New only keeps it.
type specimenStore struct{ operatorauth.Store }

// The fake keys of the Authenticator, Config and Key specimens: obviously fake, 32 bytes.
var (
	specimenKEK  = bytes.Repeat([]byte("FAKEkekFAKEkekFA"), 2)
	specimenHMAC = bytes.Repeat([]byte("FAKEhmacFAKEhmac"), 2)
)

// specimenAddr is a client address the Authenticator specimen's flood budget has seen:
// its budget maps are FILLED, and the address is one of the specimen's secrets (8th
// round, "8b": with the budgets held by value, fmt's badVerb printed their keys).
const specimenAddr = "192.0.2.123"

// specimenAuthenticator is New's product, built once: New pays one cost-12 bcrypt.
var specimenAuthenticator = sync.OnceValues(func() (*operatorauth.Authenticator, error) {
	a, err := operatorauth.New(specimenStore{}, specimenConfig())
	if err != nil {
		return nil, err
	}
	a.AllowRequest(specimenAddr)
	return a, nil
})

func specimenConfig() operatorauth.Config {
	return operatorauth.Config{TOTPKEK: operatorauth.NewKey(specimenKEK), TokenHMACKey: operatorauth.NewKey(specimenHMAC), Log: slog.Default()}
}

func specimens(t *testing.T) []specimen {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/operator", nil)
	req.AddCookie(&http.Cookie{Name: operatorauth.SessionCookieName, Value: fakeSession})
	req.AddCookie(&http.Cookie{Name: operatorauth.ChallengeCookieName, Value: fakeChallenge})
	tok, err := operatorauth.ReadSessionCookie(req)
	if err != nil {
		t.Fatalf("ReadSessionCookie: %v", err)
	}
	ch, err := operatorauth.ReadChallengeCookie(req)
	if err != nil {
		t.Fatalf("ReadChallengeCookie: %v", err)
	}
	sec, err := operatorauth.NewSecret()
	if err != nil {
		t.Fatalf("NewSecret: %v", err)
	}
	secRaw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(sec.Base32())
	if err != nil {
		t.Fatalf("decode the secret's display form: %v", err)
	}
	enr, err := operatorauth.NewEnrollmentToken()
	if err != nil {
		t.Fatalf("NewEnrollmentToken: %v", err)
	}
	auth, err := specimenAuthenticator()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	blob := operatorauth.PendingBlobFromForm(fakeBlob)
	ck := hmac.New(sha256.New, specimenHMAC)
	_, _ = ck.Write([]byte(challengeKeyLabel)) // hash writes never fail (documented)
	challengeKey := ck.Sum(nil)
	pk := hmac.New(sha256.New, specimenKEK)
	_, _ = pk.Write([]byte(pendingKeyLabel))
	pendingKey := pk.Sum(nil)
	acct := db.OperatorAccount{ID: uuid.New(), Digest: db.NewPasswordHash(fakeDigest), Sealed: db.NewSealedSecret(fakeEnvelope)}
	b := func(s string) []byte { return []byte(s) }
	return []specimen{
		{name: "SessionToken", value: tok, secrets: [][]byte{b(fakeSession)},
			placeholder: "operatorauth.SessionToken(redacted)"},
		{name: "Challenge", value: ch, secrets: [][]byte{b(fakeChallenge)},
			placeholder: "operatorauth.Challenge(redacted)"},
		{name: "Secret", value: sec, secrets: [][]byte{secRaw, b(sec.Base32())},
			placeholder: "operatorauth.Secret(redacted)"},
		{name: "EnrollmentToken", value: enr, secrets: [][]byte{b(enr.RevealForLink())}, extra: []string{enr.Hash()},
			placeholder: "operatorauth.EnrollmentToken(redacted)"},
		{name: "PendingBlob", value: blob, secrets: [][]byte{b(fakeBlob)},
			placeholder: "operatorauth.PendingBlob(redacted)"},
		{name: "Issued", value: operatorauth.Issued{Token: tok, AdminID: uuid.New()}, secrets: [][]byte{b(fakeSession)}},
		{name: "Pending", value: operatorauth.Pending{Secret: sec, Blob: blob}, secrets: [][]byte{secRaw, b(sec.Base32()), b(fakeBlob)}},
		{name: "Key", value: operatorauth.NewKey(specimenKEK), secrets: [][]byte{specimenKEK},
			placeholder: "operatorauth.Key(redacted)"},
		{name: "Authenticator", value: auth, secrets: [][]byte{specimenKEK, specimenHMAC, challengeKey, pendingKey, b(specimenAddr)},
			placeholder: "operatorauth.Authenticator(redacted)",
			unsearched:  map[string]string{"Authenticator.keys.dummyDigest": "a cost-12 bcrypt digest of 32 random bytes New draws and discards: unknowable from outside, and the digest of nothing anyone knows"}},
		{name: "Config", value: specimenConfig(), secrets: [][]byte{specimenKEK, specimenHMAC},
			placeholder: "operatorauth.Config(redacted)"},
		{name: "db.OperatorAccount", value: acct, secrets: [][]byte{b(fakeDigest), fakeEnvelope}},
		{name: "db.SealedSecret", value: db.NewSealedSecret(fakeEnvelope), secrets: [][]byte{fakeEnvelope},
			placeholder: "db.SealedSecret(redacted)"},
		{name: "db.PasswordHash", value: db.NewPasswordHash(fakeDigest), secrets: [][]byte{b(fakeDigest)},
			placeholder: "db.PasswordHash(redacted)"},
	}
}

// heldUnexported is the shape that breaks redacting METHODS: a caller-local struct with
// an UNEXPORTED field. fmt cannot hand such a field to an interface and falls through
// to reflection -- so a redacting type's own field layout is what keeps its bytes out.
type heldUnexported struct {
	who string
	v   any
}

type heldExported struct {
	Who string
	V   any
}

// printVerbs are the fmt verbs of the matrix: every verb fmt defines. %p and %w reach
// no method (%p prints a pointer or falls to badVerb; %w of a non-error is a badVerb,
// which sets fmt's erroring flag and prints by reflection) -- the 6th audit measured a
// Config's keys leaking through both.
var printVerbs = []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%b", "%o", "%O", "%c", "%U",
	"%e", "%E", "%f", "%F", "%g", "%G", "%t", "%p", "%w"}

// printHolders are the shapes a value is printed inside (S1-S6). methods is whether fmt
// can reach the value's own methods through that shape.
var printHolders = []struct {
	name    string
	methods bool
	wrap    func(v any) any
}{
	{"S1 the value itself", true, func(v any) any { return v }},
	{"S2 an exported any field", true, func(v any) any { return heldExported{Who: "op", V: v} }},
	{"S3 an unexported any field", false, func(v any) any { return heldUnexported{who: "op", v: v} }},
	{"S4 the pointee, in an unexported any field", false, func(v any) any {
		return heldUnexported{who: "op", v: reflect.Indirect(reflect.ValueOf(v)).Interface()}
	}},
	{"S5 a slice element", true, func(v any) any { return []any{v, v} }},
	{"S6 a map value", true, func(v any) any { return map[string]any{"k": v} }},
}

// printed is one rendering of a value: the path's name, its verb ("" for slog and
// encoding/json), whether fmt could reach the value's methods, whether fmt printed
// only an ADDRESS (%p of a pointer, a slice or a map -- nothing of the value, so the
// positive control cannot show anything there; the search still runs: m10-platform.md,
// OP-6 md. 18, "Tek liste (11. tur)", S13), and the text.
type printed struct {
	path        string
	verb        string
	methods     bool
	addressOnly bool
	text        string
}

// render prints v on THE MATRIX -- every printVerbs verb × every printHolders shape,
// through fmt.Sprintf and through fmt.Errorf -- and on the fixed paths F1-F5: slog's
// text handler (the value, and inside S2), slog's JSON handler (inside S2), and
// json.Marshal (the value, and inside S2). These and only these.
func render(t *testing.T, v any) []printed {
	t.Helper()
	var out []printed
	for _, h := range printHolders {
		w := h.wrap(v)
		k := reflect.ValueOf(w).Kind()
		for _, verb := range printVerbs {
			m := h.methods && verb != "%p" && verb != "%w"
			addr := verb == "%p" && (k == reflect.Pointer || k == reflect.Slice || k == reflect.Map)
			out = append(out,
				printed{"Sprintf " + verb + " · " + h.name, verb, m, addr, fmt.Sprintf(verb, w)},
				printed{"Errorf " + verb + " · " + h.name, verb, m, addr, fmt.Errorf(verb, w).Error()})
		}
	}
	var text, textHeld, js bytes.Buffer
	slog.New(slog.NewTextHandler(&text, nil)).Info("issued", "value", v)
	slog.New(slog.NewTextHandler(&textHeld, nil)).Info("issued", "state", heldExported{Who: "op", V: v})
	slog.New(slog.NewJSONHandler(&js, nil)).Info("issued", "state", heldExported{Who: "op", V: v})
	direct, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	held, err := json.Marshal(heldExported{Who: "op", V: v})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return append(out,
		printed{"F1 slog text, the value", "", true, false, text.String()},
		printed{"F2 slog text, inside S2", "", true, false, textHeld.String()},
		printed{"F3 slog JSON, inside S2", "", true, false, js.String()},
		printed{"F4 json.Marshal, the value", "", true, false, string(direct)},
		printed{"F5 json.Marshal, inside S2", "", true, false, string(held)})
}

// TestLeak_NoSecretOnAnyPrintingPath: on THE MATRIX and the fixed paths (render), no
// specimen's secrets appear in any of their forms (byteForms, and verbForms for the
// path's verb). A REDACTING specimen must also print its placeholder on every path
// where fmt reaches its methods, and each of its five methods must return it: without
// that, deleting a method whose job another method or a pointer happened to cover
// stayed green (6th round: %d reaches only Format; a slog handler falls back to
// MarshalText when LogValue is gone).
func TestLeak_NoSecretOnAnyPrintingPath(t *testing.T) {
	paths := 0
	for _, s := range specimens(t) {
		if s.placeholder != "" {
			checkRedactingMethods(t, s)
		}
		for _, p := range render(t, s.value) {
			paths++
			for _, m := range s.secretForms(p.verb) {
				if len(m) >= 6 && strings.Contains(p.text, m) {
					t.Errorf("%s leaks on %s (%d characters of a secret's form visible)", s.name, p.path, len(m))
					break
				}
			}
			if s.placeholder != "" && p.methods && !strings.Contains(p.text, s.placeholder) {
				t.Errorf("%s does not print its placeholder on %s", s.name, p.path)
			}
			if p.text == "" {
				t.Errorf("%s rendered nothing on %s; the assertion above would be vacuous", s.name, p.path)
			}
		}
	}
	t.Logf("%d renderings searched", paths)
}

// checkRedactingMethods pins the five methods of a redacting specimen: each returns the
// placeholder, and slog RESOLVES the value to that string (LogValue), not to the value.
func checkRedactingMethods(t *testing.T, s specimen) {
	t.Helper()
	if _, ok := s.value.(fmt.Formatter); !ok {
		t.Errorf("%s is not a fmt.Formatter", s.name)
	} else if got := fmt.Sprintf("%d", s.value); got != s.placeholder {
		t.Errorf("%s: Format wrote %d characters that are not its placeholder", s.name, len(got))
	}
	if v, ok := s.value.(fmt.Stringer); !ok || v.String() != s.placeholder {
		t.Errorf("%s: String is missing or is not its placeholder", s.name)
	}
	if v, ok := s.value.(fmt.GoStringer); !ok || v.GoString() != s.placeholder {
		t.Errorf("%s: GoString is missing or is not its placeholder", s.name)
	}
	if v, ok := s.value.(encoding.TextMarshaler); !ok {
		t.Errorf("%s is not an encoding.TextMarshaler", s.name)
	} else if b, err := v.MarshalText(); err != nil || string(b) != s.placeholder {
		t.Errorf("%s: MarshalText is not its placeholder (err %v)", s.name, err)
	}
	if r := slog.AnyValue(s.value).Resolve(); r.Kind() != slog.KindString || r.String() != s.placeholder {
		t.Errorf("%s: slog resolves it to a %s, not to its placeholder (LogValue)", s.name, r.Kind())
	}
}

// isLeaf reports whether rt is a REDACTING LEAF: a redacting type with one field, which
// rule 3 keeps behind a pointer badVerb does not open (a *string, in every leaf today).
func isLeaf(rt reflect.Type) bool {
	return rt.Kind() == reflect.Struct && formats(rt) && rt.NumField() == 1
}

// redactedValues collects, by path, the value of every redacting leaf a value holds,
// walking this module's structs and anonymous ones through values, pointers, interfaces,
// slices, arrays, and maps' keys and values. Reflection reads an unexported field's
// string without exporting it. A place a leaf can sit that holds NO value is listed as
// "": a leaf whose pointer is nil, whose string is empty or whose field is not a
// *string; a nil pointer, an empty slice or an empty map whose type can hold a leaf
// (holdsLeaf -- `*struct{ K Key }`, `**Key`, `*[1]Key`, `[]Key{}`). A specimen that
// leaves such a place unset searches nothing a production value would put there (11th
// round, own escape attempt: `Extra Key` added to Config and left unset was green; 12th
// round, the 11th auditor: a nil pointer to a leaf-holding type was not seen). A map's
// entries are named by position, never by key: a key can be a searched value, and the
// path is printed. The second result is every struct type the walk entered. NOT
// WALKED, BY NAME (m10-platform.md, OP-6 md. 18, "Tek liste (11. tur)", S7): the inside
// of a struct of a package outside this module (sync.Map, atomic.Pointer), and a nil
// interface (its type, so what it could hold, is unknown).
func redactedValues(v reflect.Value, path string) (map[string]string, map[reflect.Type]bool) {
	out := map[string]string{}
	types := map[reflect.Type]bool{}
	type at struct {
		p uintptr
		t reflect.Type
	}
	seen := map[at]bool{}
	var walk func(v reflect.Value, path string)
	walk = func(v reflect.Value, path string) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				if holdsLeaf(v.Type().Elem()) {
					out[path] = ""
				}
				return
			}
			k := at{v.Pointer(), v.Type()}
			if seen[k] {
				return
			}
			seen[k] = true
			walk(v.Elem(), path)
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem(), path)
			}
		case reflect.Slice, reflect.Array:
			if v.Len() == 0 && holdsLeaf(v.Type().Elem()) {
				out[path] = ""
			}
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i), fmt.Sprintf("%s[%d]", path, i))
			}
		case reflect.Map:
			if v.Len() == 0 && (holdsLeaf(v.Type().Key()) || holdsLeaf(v.Type().Elem())) {
				out[path] = ""
			}
			i := 0
			for it := v.MapRange(); it.Next(); i++ {
				walk(it.Key(), fmt.Sprintf("%s{key %d}", path, i))
				walk(it.Value(), fmt.Sprintf("%s{value %d}", path, i))
			}
		case reflect.Struct:
			rt := v.Type()
			types[rt] = true
			if isLeaf(rt) {
				out[path] = ""
				if f := v.Field(0); f.Kind() == reflect.Pointer && !f.IsNil() && f.Elem().Kind() == reflect.String {
					out[path] = f.Elem().String()
				}
				return
			}
			if rt.Name() != "" && !strings.HasPrefix(rt.PkgPath(), modulePrefix) {
				return
			}
			for i := 0; i < rt.NumField(); i++ {
				walk(v.Field(i), path+"."+rt.Field(i).Name)
			}
		}
	}
	walk(v, path)
	return out, types
}

// holdsLeaf reports whether a value of type rt can hold a redacting leaf, as far as
// redactedValues reads: through pointers, slices, arrays, maps' keys and values, and the
// fields of this module's structs and of anonymous ones (an interface's run-time content
// is m10-platform.md, OP-6 md. 18, "Tek liste (11. tur)", S1).
func holdsLeaf(rt reflect.Type) bool {
	seen := map[reflect.Type]bool{}
	var in func(t reflect.Type) bool
	in = func(t reflect.Type) bool {
		if seen[t] {
			return false
		}
		seen[t] = true
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			return in(t.Elem())
		case reflect.Map:
			return in(t.Key()) || in(t.Elem())
		case reflect.Struct:
			if isLeaf(t) {
				return true
			}
			if t.Name() != "" && !strings.HasPrefix(t.PkgPath(), modulePrefix) {
				return false
			}
			for i := 0; i < t.NumField(); i++ {
				if in(t.Field(i).Type) {
					return true
				}
			}
		}
		return false
	}
	return in(rt)
}

// TestSpecimens_SearchEveryRedactedValueTheyHold makes a specimen's SEARCHED secrets
// mechanical rather than a list someone wrote: every redacted value the specimen holds
// (redactedValues) is among its secrets, or is named in its `unsearched` with a reason --
// and every `unsearched` entry is a value it holds. (11th round, the 10th auditor: the
// Authenticator specimen searched the KEK, the HMAC key and an address, and not the
// derived challenge key it also holds -- the key that signs a post-password challenge.)
// An EXCEPTION whose type can hold a leaf is held by a specimen, whose search then
// covers it (11th round, own escape attempt: `k Key` added to Identity, an exception,
// was searched by nothing). An `unsearched` entry's reason is the reviewer's claim: the
// test pins the entries' paths (below), not their text (m10-platform.md, OP-6 card
// correction, md. 18, "Tek liste (11. tur)", S11).
func TestSpecimens_SearchEveryRedactedValueTheyHold(t *testing.T) {
	leaves := 0
	reached := map[reflect.Type]bool{}
	var unsearched []string
	for _, s := range specimens(t) {
		for path := range s.unsearched {
			unsearched = append(unsearched, path)
		}
		held, types := redactedValues(reflect.ValueOf(s.value), s.name)
		for rt := range types {
			reached[rt] = true
		}
		for path, val := range held {
			leaves++
			found := false
			for _, sec := range s.secrets {
				found = found || (val != "" && string(sec) == val)
			}
			switch {
			case found || s.unsearched[path] != "":
			case val == "":
				t.Errorf("%s holds no value at %s, where a redacting leaf can sit: the specimen leaves unsearched what production would put there; fill it, or name it in unsearched", s.name, path)
			default:
				t.Errorf("%s holds a redacted value at %s that its secrets do not include: the search would not see it leak", s.name, path)
			}
		}
		for path := range s.unsearched {
			if _, ok := held[path]; !ok {
				t.Errorf("%s names %s as unsearched, which it does not hold", s.name, path)
			}
		}
	}
	if leaves == 0 {
		t.Fatal("PREMISE: no specimen holds a redacted value; the walk has gone blind")
	}
	// Every specimen's `unsearched` entries, pinned by path: an entry added -- a value
	// moved out of the search with any reason -- is red until a reviewer changes this
	// list too (12th round, the 11th auditor: the challenge key taken out of the
	// Authenticator specimen's secrets and named unsearched, with a wrong reason, was
	// green).
	sort.Strings(unsearched)
	if want := []string{"Authenticator.keys.dummyDigest"}; strings.Join(unsearched, ",") != strings.Join(want, ",") {
		t.Errorf("the specimens leave %q unsearched; the pinned list is %q", unsearched, want)
	}
	for _, e := range exceptions(t) {
		rt := e.typ
		for rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		if holdsLeaf(rt) && !reached[rt] {
			t.Errorf("the exception %s can hold a redacting leaf that no specimen holds: its values are never searched; make it a specimen, or hold it in one", typeName(rt))
		}
	}
}

// bareText and bareBytes are the POSITIVE CONTROL: each specimen's first secret with no
// protection -- as text and as bytes, in an exported field so encoding/json sees it
// (adminauth's leak test explains why the control needs that) -- must be FOUND on
// every path of the matrix by the same forms, or the test above proves nothing there.
type (
	bareText  struct{ V string }
	bareBytes struct{ V []byte }
)

func TestLeak_TheBareValueIsThePositiveControl(t *testing.T) {
	for _, s := range specimens(t) {
		for _, bare := range []any{bareText{V: string(s.secrets[0])}, bareBytes{V: s.secrets[0]}} {
			for _, p := range render(t, bare) {
				if p.addressOnly {
					continue
				}
				found := false
				for _, m := range s.secretForms(p.verb) {
					if len(m) >= 6 && strings.Contains(p.text, m) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s: the UNPROTECTED %T did not show on %s, so the redaction test says nothing there", s.name, bare, p.path)
				}
			}
		}
	}
}

// TestSessionToken_PlaceholderIsNotAnotherCredentialsPlaceholder: each type of the
// closed type set that formats itself (fmt.Formatter on the value or the pointer) names
// ITSELF in its placeholder, shares it with no other, and borrows neither the panel
// token's nor the employee token's -- those derived by rendering THOSE types rather
// than restating their strings, so a change on either side is seen (ADR 0020 §2;
// adminauth token.go: two suites that share a placeholder vouch for each other).
func TestSessionToken_PlaceholderIsNotAnotherCredentialsPlaceholder(t *testing.T) {
	// Hoisted out of the fmt calls: redline R7 flags the type's NAME inside a fmt call
	// (adminauth's own leak test does the same).
	panelZero, employeeZero := adminauth.Token{}, session.Token{}
	panel := fmt.Sprintf("%v", panelZero)
	employee := fmt.Sprintf("%v", employeeZero)
	seen := map[string]string{}
	for _, ct := range closedTypeSet(t) {
		if !formats(ct.typ) {
			continue
		}
		zero := reflect.New(ct.typ).Interface() // a pointer: reaches value AND pointer receivers
		got := fmt.Sprintf("%v", zero)
		if !strings.Contains(got, ct.name) {
			t.Errorf("%s renders %q, which does not name the type", ct.name, got)
		}
		if got == panel || got == employee || strings.Contains(got, panel) || strings.Contains(got, employee) {
			t.Errorf("%s renders %q: it borrows the panel's %q or the employee's %q placeholder", ct.name, got, panel, employee)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%s and %s share the placeholder %q", ct.name, prev, got)
		}
		seen[got] = ct.name
		for _, via := range []string{fmt.Sprintf("%#v", zero), fmt.Sprintf("%x", zero), fmt.Sprintf("%s", zero)} {
			if !strings.Contains(via, ct.name) && !strings.Contains(via, hex.EncodeToString([]byte(got))) {
				t.Errorf("%s: a zero value renders %q on another verb", ct.name, via)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("PREMISE: no type of the closed set formats itself; the read has gone blind")
	}
	if !strings.Contains(panel, "adminauth") || !strings.Contains(employee, "session") {
		t.Fatalf("PREMISE: the other packages' placeholders are %q and %q", panel, employee)
	}
}

// formats reports whether rt (or *rt) is a fmt.Formatter: a REDACTING type.
func formats(rt reflect.Type) bool {
	f := reflect.TypeOf((*fmt.Formatter)(nil)).Elem()
	return rt.Kind() != reflect.Interface && (rt.Implements(f) || reflect.PointerTo(rt).Implements(f))
}

// ------------------------------------------------------- the closed type set --

const (
	operatorauthPkg = "github.com/atknatk/tappa/internal/operatorauth"
	dbPkg           = "github.com/atknatk/tappa/internal/db"
)

// typeName is a type's name in the closed set (moduleTypeName); an anonymous type is
// spelled out.
func typeName(rt reflect.Type) string {
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Name() == "" {
		return rt.String()
	}
	return moduleTypeName(rt.PkgPath(), rt.Name())
}

// modulePrefix is this module's path: every package under it is RECORDED by the closed
// type set (11th round -- the 10th auditor: `func E() sun.EV2Auth`, with its session
// keys as plain []byte, was green while only this package and internal/db were).
const modulePrefix = "github.com/atknatk/tappa/"

// moduleTypeName is how the closed set names a named type: bare for this package's,
// "db." for internal/db's, the FULL package path for any other package's
// ("github.com/atknatk/tappa/internal/sun.Result", "net/http.Cookie") -- a
// module-relative path could equal a standard library one ("internal/poll").
func moduleTypeName(pkgPath, name string) string {
	switch pkgPath {
	case operatorauthPkg:
		return name
	case dbPkg:
		return "db." + name
	}
	return pkgPath + "." + name
}

// typeID spells a type with every named type's FULL PACKAGE PATH -- unique in a build,
// so two packages' types that share a name, and even a package name, are told apart --
// and every part of an unnamed type a change of which would change the type (a channel's
// direction, a struct field's tag, a function's variadic parameter). An alias is the
// type it stands for: reflection never sees aliases. allowedFields pins each field to it.
func typeID(rt reflect.Type) string {
	if rt.Name() != "" {
		if rt.PkgPath() == "" {
			return rt.Name()
		}
		return rt.PkgPath() + "." + rt.Name()
	}
	list := func(n int, at func(int) reflect.Type) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = typeID(at(i))
		}
		return strings.Join(parts, ", ")
	}
	switch rt.Kind() {
	case reflect.Pointer:
		return "*" + typeID(rt.Elem())
	case reflect.Slice:
		return "[]" + typeID(rt.Elem())
	case reflect.Array:
		return fmt.Sprintf("[%d]%s", rt.Len(), typeID(rt.Elem()))
	case reflect.Map:
		return "map[" + typeID(rt.Key()) + "]" + typeID(rt.Elem())
	case reflect.Chan:
		return rt.ChanDir().String() + " " + typeID(rt.Elem())
	case reflect.Func:
		return fmt.Sprintf("func(%s) (%s) variadic=%t", list(rt.NumIn(), rt.In), list(rt.NumOut(), rt.Out), rt.IsVariadic())
	case reflect.Interface:
		var b strings.Builder
		b.WriteString("interface{")
		for i := 0; i < rt.NumMethod(); i++ {
			m := rt.Method(i)
			fmt.Fprintf(&b, "%s %s %s; ", m.PkgPath, m.Name, typeID(m.Type))
		}
		b.WriteString("}")
		return b.String()
	case reflect.Struct:
		var b strings.Builder
		b.WriteString("struct{")
		for i := 0; i < rt.NumField(); i++ {
			fld := rt.Field(i)
			fmt.Fprintf(&b, "%s %s %s %q embedded=%t; ", fld.PkgPath, fld.Name, typeID(fld.Type), fld.Tag, fld.Anonymous)
		}
		b.WriteString("}")
		return b.String()
	}
	return rt.String()
}

// notSpecimens are the closed set's types that hold no secret value to print, each with
// its reason. EVERY `why` in this file -- notSpecimens', exceptions', allowedFields' and
// a specimen's unsearched -- is the REVIEWER'S claim: the tests pin each entry's name,
// type and membership of its set, never its text, so a mutant that falsifies a reason
// without changing a name, a type or a set is outside the contract by definition, and
// one that changes any of the three is red (m10-platform.md, OP-6 card correction,
// md. 18, "Tek liste (11. tur)", S11). The reasons are still written from what was
// measured.
var notSpecimens = []struct {
	typ reflect.Type
	why string
}{
	{reflect.TypeOf(operatorauth.Identity{}), "two uuids, the session's and the operator's: an operator is named by id (ADR 0020 §5)"},
	{reflect.TypeOf((*operatorauth.Store)(nil)).Elem(), "an interface: it holds no value of its own; its implementation is OP-7's pool"},
	{reflect.TypeOf(db.OperatorSession{}), "two uuids, op_touch_session's answer -- never the hash (ADR 0021 §1)"},
	{reflect.TypeOf(db.OperatorAuthEvent("")), "a pre-session row kind: one of db's OperatorAuthEvent constants"},
}

type closedType struct {
	name string
	typ  reflect.Type // dereferenced
}

// closedTypeSet is the specimens' types and the exceptions' types, in that order.
func closedTypeSet(t *testing.T) []closedType {
	t.Helper()
	var out []closedType
	deref := func(rt reflect.Type) reflect.Type {
		for rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		return rt
	}
	for _, s := range specimens(t) {
		out = append(out, closedType{s.name, deref(reflect.TypeOf(s.value))})
	}
	for _, e := range exceptions(t) {
		out = append(out, closedType{typeName(e.typ), deref(e.typ)})
	}
	return out
}

type exception struct {
	typ reflect.Type
	why string
}

// exceptions is notSpecimens and this package's UNEXPORTED types the walk reaches
// through an exported type's unexported fields (10th round: the walk now goes through
// every field, because fmt prints unexported ones too). Their reflect types are read off
// the Authenticator's fields -- an external test cannot name them -- and a missing field
// is a red test, not a silent skip.
func exceptions(t *testing.T) []exception {
	t.Helper()
	field := func(rt reflect.Type, name string) reflect.Type {
		f, ok := rt.FieldByName(name)
		if !ok {
			t.Fatalf("%s has no field %s; the internal exceptions moved", rt, name)
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Map {
			ft = ft.Elem()
		}
		return ft
	}
	keys := field(reflect.TypeOf(operatorauth.Authenticator{}), "keys")
	limits := field(reflect.TypeOf(operatorauth.Authenticator{}), "limits")
	budget := field(limits, "flood")
	window := field(budget, "windows")
	out := []exception{}
	for _, e := range notSpecimens {
		out = append(out, exception{e.typ, e.why})
	}
	return append(out,
		exception{keys, "the Authenticator's key set, held only by the Authenticator: its fields are named with their types in allowedFields, and the Authenticator specimen searches every value they hold but the pinned unsearched dummy digest"},
		exception{limits, "the Authenticator's budgets, behind *limits (8b): fields named with their types in allowedFields"},
		exception{budget, "one budget: fields named with their types in allowedFields; its windows' keys pinned to client addresses, operator ids and the empty key by TestLeak_NoInputInAnyErrorOrLogLine"},
		exception{window, "one budget window: fields named with their types in allowedFields"})
}

// exactImports is an importer that reads every dependency's EXPORT DATA as the compiler
// wrote it -- `go list -export -deps` names the files, and the build cache already holds
// them, because building this test built them (with -race when the test is built with
// -race: raceBuild, so the same cache entries are hit). With it go/types resolves every
// type of every package exactly -- an alias, a generic instance and its type arguments,
// an exported variable's inferred type -- instead of guessing from syntax.
//
// WHY THIS AND NOT A NARROWER CHECK (OP-6 verification, 9th round, measured): the 7th
// and 8th rounds type-checked this package and internal/db alone, every other import
// an empty package; every type of another package was invalid there, and two audits in
// a row found a declaration shape the walk could not see (a type argument of a foreign
// generic; then a type alias of one, and an exported variable whose type is inferred).
// The orchestrator's rule: load exact types if that adds at most ~5 s under -race.
// Measured, this package's own test, apiTypes as a whole (`go list` + the check + the
// walk -- since the 10th round the WHOLE type graph, about two thousand types): warm
// cache 0.19-0.32 s (0.37-0.53 s under -race); a COLD cache (a fresh GOCACHE, the test
// binary built from nothing) 0.43 s (0.83 s under -race) -- the dependencies' export
// data is what building the test just produced.
//
// It needs the `go` command of the toolchain that built the test: when cmd/go switches
// toolchains it puts that one first on PATH and sets GOROOT for the test, and a version
// mismatch -- export data the importer cannot read -- is refused by name below rather
// than failing as a garbled type error.
func exactImports(t *testing.T, fset *gotoken.FileSet) types.Importer {
	t.Helper()
	if v, err := exec.Command("go", "env", "GOVERSION").Output(); err != nil {
		t.Fatalf("go env GOVERSION: %v", err)
	} else if got := strings.TrimSpace(string(v)); got != runtime.Version() {
		t.Fatalf("the go command is %s and this test was built by %s: its export data would not be this toolchain's", got, runtime.Version())
	}
	args := []string{"list", "-export", "-deps", "-json=ImportPath,Export"}
	if raceBuild {
		args = append(args, "-race")
	}
	out, err := exec.Command("go", append(args, operatorauthPkg)...).Output()
	if err != nil {
		// go list's stderr is package, module and toolchain diagnostics -- import paths,
		// build constraints, version errors. It does not echo the environment (this
		// test's .env values included). A module FETCH error names the proxy URL, which
		// cmd/go prints REDACTED (url.URL.Redacted: the password masked --
		// cmd/go/internal/web/api.go, read in the 11th round); what a credential put in
		// the URL's USERNAME part would show is the one thing not masked, by name
		// (m10-platform.md, OP-6 md. 18, "Tek liste (11. tur)", S12). (10th round: the
		// bare exit status said nothing about the cause.)
		var ee *exec.ExitError
		var stderr string
		if errors.As(err, &ee) {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		t.Fatalf("go list -export: %v: %s", err, stderr)
	}
	exports := map[string]string{}
	for dec := json.NewDecoder(bytes.NewReader(out)); ; {
		var p struct{ ImportPath, Export string }
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("go list output: %v", err)
		}
		exports[p.ImportPath] = p.Export
	}
	if exports[dbPkg] == "" {
		t.Fatalf("PREMISE: go list named no export data for %s; the load has gone blind", dbPkg)
	}
	return importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		f, ok := exports[path]
		if !ok || f == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(f)
	})
}

// apiTypes is the CLOSED TYPE SET, with EXACT types (exactImports): every named type of
// THIS MODULE (any package under modulePrefix) reachable from this package's EXPORTED
// declarations -- types, functions, variables and constants, each by its RESOLVED type.
// The walk goes over the WHOLE go/types type graph with an exhaustive switch -- every
// kind handled by name, an unknown kind a red test (fail-closed) -- whatever package a
// type belongs to; only the RECORD is limited to this module's named types (11th
// round: it was this package's and internal/db's alone, and `func E() sun.EV2Auth`
// -- session keys as plain []byte -- was green). Every
// struct field is walked, exported or not (fmt prints unexported fields too: a value's
// printability does not depend on exportedness); of a named type, its type arguments,
// type parameters' constraints, the signatures of its EXPORTED methods (a caller can
// call those) and its underlying type; of a signature, receiver, type parameters,
// parameters and results; of an interface, its explicit methods and embedded types --
// union terms included; of a type parameter, its constraint. The roots are this
// package's own declarations only: an exception or a specimen is never a root, so an
// entry cannot make itself reachable (the 6th audit's finding). local lists the types
// declared INSIDE a function -- behind an interface such a type escapes every walk, so
// it is refused; a TYPE PARAMETER is not such a type and is not listed.
//
// History, measured (10th round, the 9th auditor): the 9th round walked a FOREIGN named
// type's type arguments only, on the premise that no other package's type could hold
// one of these two packages' types otherwise -- false: internal/operatorauth imports
// internal/sun, whose Result carries a db.ResolvedTag (with its AES key reference as a
// plain []byte), and `func R() sun.Result` stayed green. The closure now rests on the
// walk's structure, not on a premise about the import graph.
func apiTypes(t *testing.T) (reach map[string]bool, local, vars []string) {
	t.Helper()
	fset := gotoken.NewFileSet()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var files []*ast.File
	for _, nm := range names {
		if strings.HasSuffix(nm, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, nm, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", nm, err)
		}
		files = append(files, f)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: exactImports(t, fset)}
	pkg, err := conf.Check(operatorauthPkg, fset, files, info)
	if err != nil {
		t.Fatalf("type-check with exact imports: %v", err)
	}
	reach = map[string]bool{}
	seen := map[types.Type]bool{}
	kinds := map[string]int{}
	var visit func(types.Type)
	tparams := func(l *types.TypeParamList) {
		for i := 0; i < l.Len(); i++ {
			visit(l.At(i))
		}
	}
	args := func(l *types.TypeList) {
		for i := 0; i < l.Len(); i++ {
			visit(l.At(i))
		}
	}
	visit = func(ty types.Type) {
		if ty == nil || seen[ty] {
			return
		}
		seen[ty] = true
		kinds[fmt.Sprintf("%T", ty)]++
		switch x := ty.(type) {
		case *types.Basic:
		case *types.Pointer:
			visit(x.Elem())
		case *types.Array:
			visit(x.Elem())
		case *types.Slice:
			visit(x.Elem())
		case *types.Map:
			visit(x.Key())
			visit(x.Elem())
		case *types.Chan:
			visit(x.Elem())
		case *types.Struct:
			for i := 0; i < x.NumFields(); i++ {
				visit(x.Field(i).Type())
			}
		case *types.Tuple:
			for i := 0; i < x.Len(); i++ {
				visit(x.At(i).Type())
			}
		case *types.Signature:
			if r := x.Recv(); r != nil {
				visit(r.Type())
			}
			tparams(x.RecvTypeParams())
			tparams(x.TypeParams())
			visit(x.Params())
			visit(x.Results())
		case *types.Named:
			args(x.TypeArgs())
			tparams(x.TypeParams())
			if o := x.Obj(); o.Pkg() != nil && strings.HasPrefix(o.Pkg().Path(), modulePrefix) {
				reach[moduleTypeName(o.Pkg().Path(), o.Name())] = true
			}
			for i := 0; i < x.NumMethods(); i++ {
				if m := x.Method(i); m.Exported() {
					visit(m.Type())
				}
			}
			visit(x.Underlying())
		case *types.Alias:
			args(x.TypeArgs())
			tparams(x.TypeParams())
			visit(types.Unalias(x))
		case *types.Interface:
			for i := 0; i < x.NumExplicitMethods(); i++ {
				visit(x.ExplicitMethod(i).Type())
			}
			for i := 0; i < x.NumEmbeddeds(); i++ {
				visit(x.EmbeddedType(i))
			}
		case *types.Union:
			for i := 0; i < x.Len(); i++ {
				visit(x.Term(i).Type())
			}
		case *types.TypeParam:
			visit(x.Constraint())
		default:
			t.Fatalf("unhandled go/types kind %T: the walk is exhaustive or it is red", ty)
		}
	}
	roots := 0
	errType := types.Universe.Lookup("error").Type()
	exportedVars := map[types.Object]bool{}
	for _, nm := range pkg.Scope().Names() {
		if o := pkg.Scope().Lookup(nm); o.Exported() {
			roots++
			visit(o.Type())
			if v, isVar := o.(*types.Var); isVar {
				exportedVars[v] = true
				if !types.Identical(v.Type(), errType) {
					vars = append(vars, fmt.Sprintf("%s %s", v.Name(), v.Type()))
				}
			}
		}
	}
	// An exported error variable is a SENTINEL: declared with errors.New of a string
	// literal and never written again -- no assignment, no address taken -- anywhere in
	// the package (12d, the closing auditor: `var ErrLastKey error`, set in New to
	// errors.New of the KEK's text, was green; its type was right, its content unread).
	sentinels := 0
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.GenDecl:
				if x.Tok != gotoken.VAR {
					return true
				}
				for _, sp := range x.Specs {
					vs := sp.(*ast.ValueSpec)
					for i, id := range vs.Names {
						if !exportedVars[info.Defs[id]] || pkg.Scope().Lookup(id.Name) != info.Defs[id] {
							continue
						}
						if i >= len(vs.Values) || !isErrorsNewOfLiteral(vs.Values[i]) {
							vars = append(vars, fmt.Sprintf("%s (%s): not declared as errors.New of a string literal", id.Name, fset.Position(id.Pos())))
							continue
						}
						sentinels++
					}
				}
			case *ast.AssignStmt:
				for _, l := range x.Lhs {
					if id, ok := l.(*ast.Ident); ok && exportedVars[info.Uses[id]] {
						vars = append(vars, fmt.Sprintf("%s (%s): an exported variable written after its declaration", id.Name, fset.Position(id.Pos())))
					}
				}
			case *ast.UnaryExpr:
				if id, ok := x.X.(*ast.Ident); ok && x.Op == gotoken.AND && exportedVars[info.Uses[id]] {
					vars = append(vars, fmt.Sprintf("%s (%s): an exported variable's address taken", id.Name, fset.Position(id.Pos())))
				}
			}
			return true
		})
	}
	t.Logf("%d exported sentinel error variables, each errors.New of a string literal", sentinels)
	for id, o := range info.Defs {
		tn, ok := o.(*types.TypeName)
		if !ok || tn.Parent() == pkg.Scope() {
			continue
		}
		if _, param := tn.Type().(*types.TypeParam); param {
			continue
		}
		local = append(local, fmt.Sprintf("%s (%s)", tn.Name(), fset.Position(id.Pos())))
	}
	if roots == 0 || len(reach) == 0 {
		t.Fatalf("PREMISE: %d exported declaration(s), %d reachable type(s); the read has gone blind", roots, len(reach))
	}
	t.Logf("type graph walked: %d types by kind %v", len(seen), kinds)
	return reach, local, vars
}

// isErrorsNewOfLiteral reports whether e is errors.New("…") -- a call of the errors
// package's New on one string literal.
func isErrorsNewOfLiteral(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok || len(c.Args) != 1 {
		return false
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return false
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "errors" {
		return false
	}
	lit, ok := c.Args[0].(*ast.BasicLit)
	return ok && lit.Kind == gotoken.STRING
}

// TestExportedTypes_EveryOneIsASpecimenOrANamedException pins the CLOSED TYPE SET from
// both sides: every type apiTypes reaches with exact types is a specimen or a
// notSpecimens entry, never both; every entry is in that set; no type is declared inside
// a function; no exported package variable is anything but a SENTINEL error -- typed
// error, declared as errors.New of a string literal, never written again nor its address
// taken (apiTypes's third result). An unclassified type, a stale entry, a function-local
// type and an exported variable outside that shape are each red.
//
// COUNTED LIMITS -- the full list for the closed type set and the leak tests is ONE
// place, m10-platform.md, OP-6 card correction, md. 18, "Tek liste (11. tur)"; the two
// that are this test's own (S1 and S2 there): a value reached only at RUN TIME -- an `any` (or any
// interface) a function fills, or a value made by reflection -- is not in the set, the
// set is what the types say; of a named type's methods the walk follows the EXPORTED
// ones (an unexported method is uncallable from outside its package and holds no value
// to print). Every package's types are walked and every package of this module is
// recorded (moduleTypeName).
func TestExportedTypes_EveryOneIsASpecimenOrANamedException(t *testing.T) {
	reach, local, vars := apiTypes(t)
	for _, l := range local {
		t.Errorf("a function-local type %s: behind an interface it escapes the closed set; declare it at package level and classify it", l)
	}
	// An exported package VARIABLE is state any importer reads and prints as it likes --
	// no rule above sees what it holds (12b, the 12th auditor: `var LastKEK []byte`, filled
	// by New, was green; 12d: an error variable filled with the KEK's text). The package's
	// are its sentinel errors; any other is red until it is reasoned here.
	for _, v := range vars {
		t.Errorf("an exported package variable %s: only sentinel errors are exported variables here", v)
	}
	classified := map[string]string{}
	nSpecimens := len(specimens(t))
	for i, ct := range closedTypeSet(t) {
		as := "specimen"
		if i >= nSpecimens {
			as = "exception"
		}
		if typeName(ct.typ) != ct.name {
			t.Errorf("the %s %q holds a %s", as, ct.name, typeName(ct.typ))
		}
		if prev, dup := classified[ct.name]; dup {
			t.Errorf("%s is classified twice (%s and %s)", ct.name, prev, as)
		}
		classified[ct.name] = as
	}
	for _, e := range exceptions(t) {
		if strings.TrimSpace(e.why) == "" {
			t.Errorf("the exception %s gives no reason", typeName(e.typ))
		}
	}
	for name := range reach {
		if _, ok := classified[name]; !ok {
			t.Errorf("%s is reachable from this package's exported API and is neither a specimen nor a named exception", name)
		}
	}
	for name, as := range classified {
		if !reach[name] {
			t.Errorf("the %s %s is not reachable from this package's exported API", as, name)
		}
	}
}

// allowedField is a field the closed type set may hold: its TYPE, pinned (typeID -- a
// type changed under the same name is red until the entry is reviewed; 11th round, the
// 10th auditor: `challengeKey Key` turned into `challengeKey [][]byte` kept its name and
// its entry, and the reason "a Key" was a claim nothing checked), and the reason.
type allowedField struct {
	typ string
	why string
}

// oa and dbt are typeID's spelling of this package's and internal/db's named types.
const (
	oa  = operatorauthPkg + "."
	dbt = dbPkg + "."
)

// allowedFields names every field the field walk reaches (TestExportedTypes_CarryNoPlainStringField:
// the closed type set's fields, and the fields of every struct they hold -- through
// pointers, slices, arrays and maps, of any package, anonymous ones under the holding
// field's path) with its pinned TYPE and the reason it may hold what it holds -- EVERY
// field, a function's and an okFieldTypes value's included (12th round). A non-struct
// type of the set is named by itself, its type pinned as its kind ("kind:string").
var allowedFields = map[string]allowedField{
	"Authenticator.store":            {oa + "Store", "the Store: an interface, OP-7's operator pool behind it"},
	"Authenticator.keys":             {oa + "authKeys", "the key material, walked (authKeys)"},
	"Authenticator.limits":           {"*" + oa + "limits", "the budgets, walked (limits)"},
	"authKeys.kek":                   {oa + "Key", "the TOTP KEK (ADR 0020 §1), the process's copy, a Key"},
	"authKeys.tokenKey":              {oa + "Key", "the token HMAC key (ADR 0020 §2), the same custody, a Key"},
	"authKeys.pendingKey":            {oa + "Key", "the enrollment page's sealing key, derived from the KEK (enrollment.go), a Key"},
	"authKeys.challengeKey":          {oa + "Key", "the challenge MAC key derived from it (challenge.go), a Key"},
	"authKeys.dummyDigest":           {oa + "Key", "the dummy bcrypt digest an unknown address is compared against (password.go), a Key"},
	"limits.flood":                   {"*" + oa + "budget", "a budget, walked"},
	"limits.work":                    {"*" + oa + "budget", "a budget, walked"},
	"limits.account":                 {"*" + oa + "budget", "a budget, walked"},
	"limits.auditCap":                {"*" + oa + "budget", "a budget, walked"},
	"limits.enrollAddr":              {"*" + oa + "budget", "a budget, walked (12c: the per-address enrollment share)"},
	"limits.enroll":                  {"*" + oa + "budget", "a budget, walked"},
	"limits.firstFactor":             {"*" + oa + "budget", "a budget, walked (OP-14 C: the per-operator cap on 'password_ok' rows)"},
	"budget.windows":                 {"map[string]*" + oa + "budgetWindow", "a budget's counters, keyed by a rate key -- a client address, an operator id, or the empty key of a process-wide budget (flow.go)"},
	"budget.limit":                   {"int", "a budget's number"},
	"budgetWindow.count":             {"int", "a budget window's count"},
	"Config.TOTPKEK":                 {oa + "Key", "the key OP-7 hands New, a Key"},
	"Config.TokenHMACKey":            {oa + "Key", "the key OP-7 hands New, a Key"},
	"Key.v":                          {"*string", "the redacting type's value, behind a *string (key.go)"},
	"SessionToken.v":                 {"*string", "the redacting type's value, behind a *string (token.go)"},
	"Challenge.v":                    {"*string", "the redacting type's value, behind a *string (challenge.go)"},
	"EnrollmentToken.v":              {"*string", "the redacting type's value, behind a *string (enrollment.go)"},
	"PendingBlob.v":                  {"*string", "the redacting type's value, behind a *string (enrollment.go)"},
	"Secret.b":                       {"*string", "the redacting type's value, behind a *string (totp.go)"},
	"Issued.Token":                   {oa + "SessionToken", "the session token, a SessionToken"},
	"Pending.Secret":                 {oa + "Secret", "the TOTP secret, a Secret"},
	"Pending.Blob":                   {oa + "PendingBlob", "the pending blob, a PendingBlob"},
	"db.OperatorAccount.Digest":      {dbt + "PasswordHash", "the password digest, a db.PasswordHash"},
	"db.OperatorAccount.Sealed":      {dbt + "SealedSecret", "the TOTP envelope, a db.SealedSecret"},
	"db.PasswordHash.v":              {"*string", "the redacting type's value, behind a *string (internal/db)"},
	"db.SealedSecret.v":              {"*string", "the redacting type's value, behind a *string (internal/db)"},
	"db.OperatorAuthEvent":           {"kind:string", "a pre-session row kind: one of db's OperatorAuthEvent constants"},
	"Authenticator.now":              {"func() (time.Time) variadic=false", "the clock (Config.Now, or time.Now): a function"},
	"Authenticator.log":              {"*log/slog.Logger", "the logger (Config.Log): a *slog.Logger, in okFieldTypes, its handler not walked"},
	"Authenticator.compareFn":        {"func([]uint8, []uint8) (error) variadic=false", "bcrypt.CompareHashAndPassword (password.go): a function"},
	"Authenticator.digestFn":         {"func(string) (string, error) variadic=false", "hashPassword (flow.go): a function"},
	"budget.mu":                      {"sync.Mutex", "the budget's lock"},
	"budget.period":                  {"time.Duration", "a budget's window length"},
	"budget.now":                     {"func() (time.Time) variadic=false", "the clock the budget reads"},
	"budgetWindow.start":             {"time.Time", "a budget window's start"},
	"budgetWindow.warned":            {"bool", "whether the window's first crossing of the limit was reported (OP-14 C, take): a flag"},
	"budgetWindow.written":           {"int", "how many of the window's charges became rows (OP-14 C, 3rd round, wrote): a count"},
	"budgetWindow.refusedWarned":     {"bool", "whether the window's first fail-closed refusal was reported (OP-14 C, 3rd round): a flag"},
	"Config.Now":                     {"func() (time.Time) variadic=false", "the clock OP-7 hands New; nil means time.Now"},
	"Config.Log":                     {"*log/slog.Logger", "the logger OP-7 hands New"},
	"Identity.SessionID":             {"github.com/google/uuid.UUID", "the session's id (ADR 0020 §5: an operator is named by id)"},
	"Identity.AdminID":               {"github.com/google/uuid.UUID", "the operator's id"},
	"Issued.AdminID":                 {"github.com/google/uuid.UUID", "the operator's id"},
	"db.OperatorAccount.ID":          {"github.com/google/uuid.UUID", "the operator's id"},
	"db.OperatorAccount.LockedUntil": {"*time.Time", "the end of the TOTP lock, nil when unlocked"},
	"db.OperatorSession.SessionID":   {"github.com/google/uuid.UUID", "the session's id, op_touch_session's answer"},
	"db.OperatorSession.AdminID":     {"github.com/google/uuid.UUID", "the operator's id, op_touch_session's answer"},
}

// okFieldTypes are the value types rule 2 does not read and the walk does not enter (a
// uuid is an array of bytes and no secret; a time, a duration, a logger, a lock). A
// field of one is still NAMED in allowedFields, with its type: rule 1 has no exemption
// (12th round, the 11th auditor: two uuid fields holding the KEK's halves, added to
// authKeys without a test file touched, were green and printed on 36 of 269
// renderings).
var okFieldTypes = map[reflect.Type]bool{
	reflect.TypeOf(uuid.UUID{}):         true,
	reflect.TypeOf(time.Time{}):         true,
	reflect.TypeOf((*time.Time)(nil)):   true,
	reflect.TypeOf(time.Duration(0)):    true,
	reflect.TypeOf((*slog.Logger)(nil)): true,
	reflect.TypeOf(sync.Mutex{}):        true,
}

// isBytesOrText reports whether a field of type rt, exported or not, holds text or bytes
// a printing path can show: a string; a sequence of integers (bytes and runes, and any
// other width -- a []uint16 carries UTF-16 text and prints as the same decimals a []byte
// does); at any depth of slices, arrays, map values and map keys of a non-string kind,
// and of anonymous structs. POINTERS, as the printers treat them:
//
//   - fmt opens ONE pointer on a path -- a field's, an element's, a map key's or value's,
//     at any depth -- when it points to an array, a slice, a struct or a map and the verb
//     is one a pointer does not take (badVerb re-prints it with %v, under which every
//     further pointer is an address). So `*[]byte`, `[]*[]byte`, `map[string]*[]byte` and
//     `[]*[32]byte` are read, and a pointer behind that first one is not (12th round,
//     the 11th auditor: `[]*[]byte` was exempt as "an address on every verb", and a key
//     in it printed on 24 of 269 renderings);
//   - encoding/json -- json.Marshal and slog's JSON handler -- follows EVERY pointer of an
//     EXPORTED field, so behind an exported field no pointer is exempt (own escape
//     attempt: an exported `*string` printed its text through json.Marshal).
//
// NOT READ, BY NAME (m10-platform.md, OP-6 card correction, md. 18, "Tek liste (11. tur)",
// S1, S3 and S4 -- TestExportedTypes_ExemptFormsPrintNoKeyBytes renders S3's forms filled
// with a key): in an UNEXPORTED field, a pointer to anything but an array, a slice, a
// struct or a map (`*string`, `**[]byte`) and every pointer behind the first one on a
// path; a channel and a function (an address); an element of a type in okFieldTypes (a
// uuid is an array of bytes and no secret); a NAMED struct (heldStructs hands it to the
// walk, which reads its fields); a map key of kind string (budget.windows, whose keys
// TestLeak_NoInputInAnyErrorOrLogLine pins by content on the arms it drives); a
// single integer; a sequence of bools, floats or complex numbers; an interface's
// run-time content.
func isBytesOrText(rt reflect.Type, exported bool) bool {
	var read func(t reflect.Type, opened bool) bool
	read = func(t reflect.Type, opened bool) bool {
		if okFieldTypes[t] {
			return false
		}
		switch t.Kind() {
		case reflect.String:
			return true
		case reflect.Slice, reflect.Array:
			if e := t.Elem().Kind(); e >= reflect.Int && e <= reflect.Uintptr {
				return true
			}
			return read(t.Elem(), opened)
		case reflect.Map:
			return (t.Key().Kind() != reflect.String && read(t.Key(), opened)) || read(t.Elem(), opened)
		case reflect.Pointer:
			if exported {
				return read(t.Elem(), opened)
			}
			switch t.Elem().Kind() {
			case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
				return !opened && read(t.Elem(), true)
			}
			return false
		case reflect.Struct:
			if t.Name() != "" {
				return false
			}
			for i := 0; i < t.NumField(); i++ {
				if read(t.Field(i).Type, opened) {
					return true
				}
			}
		}
		return false
	}
	return read(rt, false)
}

// heldStructs is every struct type a field of type rt holds itself -- through pointers,
// slices, arrays, and maps' keys and values, not through another struct's fields (the
// walk visits those in turn) -- of any package, anonymous ones included, except the
// value types in okFieldTypes. (11th round, own escape attempts: a foreign struct with a
// plain text field -- `last http.Cookie`, `last []http.Cookie` -- and a module type
// classified as an exception -- internal/sun.EV2Auth with its plain session keys --
// were green while the walk entered only this package's and internal/db's structs.)
func heldStructs(rt reflect.Type) []reflect.Type {
	if okFieldTypes[rt] {
		return nil
	}
	switch rt.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return heldStructs(rt.Elem())
	case reflect.Map:
		return append(heldStructs(rt.Key()), heldStructs(rt.Elem())...)
	case reflect.Struct:
		return []reflect.Type{rt}
	}
	return nil
}

// TestExportedTypes_CarryNoPlainStringField is the STRUCTURAL half, over the closed type
// set and every struct those types hold -- through values, pointers, slices, arrays and
// maps, of any package (heldStructs), anonymous ones under the holding field's path.
// Three rules:
//
//  1. EVERY field walked -- a function's and an okFieldTypes value's included -- is named
//     in allowedFields WITH ITS TYPE, and every entry there is a field walked, so adding,
//     removing or retyping a field is red until a reviewer edits allowedFields (6th
//     round: `lastCode string` added to Authenticator was green before the walk; 7th
//     round: a struct-typed field was walked but not named, so `cfg Config` added to
//     Authenticator was green; 11th round: `challengeKey Key` turned into
//     `challengeKey [][]byte` kept its name and its entry; 12th round: uuid and function
//     fields were not named, and two uuids holding the KEK's halves were green);
//  2. no field walked holds text or bytes a printing path shows (isBytesOrText: fmt's
//     one pointer opening, encoding/json's every pointer of an exported field) -- a
//     secret sits in a redacting type;
//  3. a REDACTING type with one field (a leaf: SessionToken, Key, db.SealedSecret …)
//     holds it behind a pointer fmt's badVerb does not open -- a pointer to anything but
//     an array, a slice, a struct or a map. The 6th auditor measured the mechanism: for
//     a verb a pointer does not take, badVerb opens a pointer ONCE, and the *[]byte of
//     Secret and db.SealedSecret and the pointer to Authenticator's key struct printed
//     their bytes under %s %q %e %f %t %c %U; a *string printed an address.
//
// What rule 2 does not read is listed, by name, at isBytesOrText; the full list of
// counted limits is m10-platform.md, OP-6 card correction, md. 18, "Tek liste (11. tur)"
// -- S3 to S5 are rule 2's and the walk's.
func TestExportedTypes_CarryNoPlainStringField(t *testing.T) {
	// okFieldTypes, pinned as a literal list: a type added to it is a shape rule 2 stops
	// reading and the walk stops entering, and is red until a reviewer changes this list
	// too (12b, the 12th auditor: [][]byte added to okFieldTypes, with
	// `authKeys.extra [][]byte` named, was green).
	var ok []string
	for rt := range okFieldTypes {
		ok = append(ok, typeID(rt))
	}
	sort.Strings(ok)
	if want := []string{"*log/slog.Logger", "*time.Time", "github.com/google/uuid.UUID", "sync.Mutex", "time.Duration", "time.Time"}; strings.Join(ok, ",") != strings.Join(want, ",") {
		t.Errorf("okFieldTypes is %q; the pinned list is %q", ok, want)
	}
	walked := map[string]string{} // name -> typeID
	seen := map[reflect.Type]bool{}
	// An ANONYMOUS struct type held in a field is walked too, its fields named under the
	// field's own path ("Authenticator.r.s"): rules 1-3 apply to them as to a named
	// type's (10th round, the 9th auditor: `r struct{ s string }`, named in allowedFields,
	// was green).
	var visit func(rt reflect.Type, owner string)
	visit = func(rt reflect.Type, owner string) {
		for rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		anon := rt.Kind() == reflect.Struct && rt.Name() == ""
		if !anon && (seen[rt] || okFieldTypes[rt]) {
			return
		}
		seen[rt] = true
		if owner == "" {
			owner = typeName(rt)
		}
		switch rt.Kind() {
		case reflect.Interface:
			return
		case reflect.Struct:
		default:
			walked[typeName(rt)] = "kind:" + rt.Kind().String()
			return
		}
		leaf := formats(rt) && rt.NumField() == 1
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			name := owner + "." + f.Name
			walked[name] = typeID(f.Type)
			if isBytesOrText(f.Type, f.IsExported()) {
				t.Errorf("%s holds text or bytes in the open (%s); a secret sits in a redacting type", name, f.Type)
			}
			if leaf {
				if e := f.Type; e.Kind() != reflect.Pointer || e.Elem().Kind() == reflect.Array || e.Elem().Kind() == reflect.Slice ||
					e.Elem().Kind() == reflect.Struct || e.Elem().Kind() == reflect.Map {
					t.Errorf("%s is a redacting type's value held as %s: fmt's badVerb opens that on %%s %%q %%e %%f %%t %%c %%U; hold it behind a *string", name, f.Type)
				}
			}
			for _, st := range heldStructs(f.Type) {
				if st.Name() == "" {
					visit(st, name)
				} else {
					visit(st, "")
				}
			}
		}
	}
	for _, ct := range closedTypeSet(t) {
		visit(ct.typ, "")
	}
	for f, typ := range walked {
		a, ok := allowedFields[f]
		switch {
		case !ok:
			t.Errorf("%s is a field nothing authorised; a code, a passphrase or a raw token could travel in it", f)
		case a.typ != typ:
			t.Errorf("%s is now a %s; allowedFields pins %s -- a type changed under the same name: review the entry and its reason", f, typ, a.typ)
		}
	}
	for f := range allowedFields {
		if _, ok := walked[f]; !ok {
			t.Errorf("allowedFields names %s, which the closed type set no longer holds", f)
		}
	}
}

// field and exportedField hold a value of type T in a field of that shape -- the
// unexported field of a struct of this package, the exported one of a struct whose
// fields encoding/json reads.
type (
	field[T any]         struct{ f T }
	exportedField[T any] struct{ F T }
)

// fieldShape is a field type filled with a key, rendered on the matrix by
// TestExportedTypes_ExemptFormsPrintNoKeyBytes.
type fieldShape struct {
	name     string
	v        any  // a field[T] or an exportedField[T]
	readByR2 bool // what isBytesOrText must answer for the field
}

// fieldShapes are rule 2's exemptions in an unexported field (S3: the shapes fmt prints
// as an address and encoding/json does not read) and, beside them, the shapes rule 2
// reads -- the ones fmt or encoding/json prints the key through.
func fieldShapes(k []byte) []fieldShape {
	key := func() []byte { return append([]byte(nil), k...) }
	text := string(k)
	p := func() *[]byte { b := key(); return &b }
	var arr [32]byte
	copy(arr[:], k)
	ch := make(chan []byte, 1)
	ch <- key()
	return []fieldShape{
		{"*string", field[*string]{&text}, false},
		{"**[]byte", field[**[]byte]{func() **[]byte { q := p(); return &q }()}, false},
		{"[]*string", field[[]*string]{[]*string{&text}}, false},
		{"map[string]*string", field[map[string]*string]{map[string]*string{"k": &text}}, false},
		{"map[*string]bool", field[map[*string]bool]{map[*string]bool{&text: true}}, false},
		{"[]**[]byte", field[[]**[]byte]{[]**[]byte{func() **[]byte { q := p(); return &q }()}}, false},
		{"*[]*[]byte (a pointer behind the first)", field[*[]*[]byte]{&[]*[]byte{p()}}, false},
		{"[]*[]*[]byte (a pointer behind the first)", field[[]*[]*[]byte]{[]*[]*[]byte{{p()}}}, false},
		{"chan []byte", field[chan []byte]{ch}, false},
		{"func() []byte", field[func() []byte]{key}, false},

		{"[]byte", field[[]byte]{key()}, true},
		{"*[]byte", field[*[]byte]{p()}, true},
		{"[]*[]byte", field[[]*[]byte]{[]*[]byte{p()}}, true},
		{"map[string]*[]byte", field[map[string]*[]byte]{map[string]*[]byte{"k": p()}}, true},
		{"[]*[32]byte", field[[]*[32]byte]{[]*[32]byte{&arr}}, true},
		{"map[[32]byte]bool", field[map[[32]byte]bool]{map[[32]byte]bool{arr: true}}, true},
		{"[]*struct{ b []byte }", field[[]*struct{ b []byte }]{[]*struct{ b []byte }{{key()}}}, true},
		{"exported *string", exportedField[*string]{&text}, true},
		{"exported **[]byte", exportedField[**[]byte]{func() **[]byte { q := p(); return &q }()}, true},
	}
}

// TestExportedTypes_ExemptFormsPrintNoKeyBytes measures rule 2's pointer model against
// the printers instead of stating it: each shape of fieldShapes, filled with the KEK,
// is rendered on the matrix (render: every verb × every holder × Sprintf/Errorf, slog,
// json.Marshal). A shape rule 2 does not read prints no form of the key on any path; a
// shape it reads prints it on at least one (or the matrix says nothing about the
// exemption beside it); and isBytesOrText answers each as the measurement does. (12th
// round, the 11th auditor: `[]*[]byte` was exempt on a claim -- "fmt prints a pointer in
// a container as an address on every verb" -- and printed a key on 24 of 269
// renderings.)
func TestExportedTypes_ExemptFormsPrintNoKeyBytes(t *testing.T) {
	key := specimen{name: "key", secrets: [][]byte{specimenKEK}}
	for _, sh := range fieldShapes(specimenKEK) {
		ft := reflect.TypeOf(sh.v).Field(0)
		if got := isBytesOrText(ft.Type, ft.IsExported()); got != sh.readByR2 {
			t.Errorf("isBytesOrText(%s) = %t, want %t", sh.name, got, sh.readByR2)
		}
		shown := 0
		for _, p := range render(t, sh.v) {
			for _, m := range key.secretForms(p.verb) {
				if len(m) >= 6 && strings.Contains(p.text, m) {
					shown++
					if !sh.readByR2 {
						t.Errorf("%s prints the key on %s", sh.name, p.path)
					}
					break
				}
			}
		}
		if sh.readByR2 && shown == 0 {
			t.Errorf("%s printed the key on no path: the matrix shows nothing beside the exemptions", sh.name)
		}
		t.Logf("%s: the key on %d paths", sh.name, shown)
	}
}

// leakStore plays the database's side of the numbered arms below, as the test
// configures it between calls: an account found or not, a lookup that fails, a write
// that fails, a session or an enrollment the database refuses or fails on. It decides
// nothing the database decides (those properties are flow_db_test.go's); it exists so
// that the error paths the contract below numbers are reached, and their text
// collected.
//
// It also RECORDS the credential-bearing arguments the flow hands the database (the
// address it looks up, the session hashes, the raw enrollment token, the new digest,
// the new envelope): those values never came from the test, and the search needs them.
// HARNESS INTEGRITY: each method counts its CALLS in one statement and records its
// argument vector in another; TestLeak_NoInputInAnyErrorOrLogLine requires, method by
// method, exactly one vector of the method's arity per call -- so a dropped or partial
// recording turns it red (4th audit, measured: with the recording of the enrollment's
// arguments deleted, the previous version stayed green).
type leakStore struct {
	acct        *db.OperatorAccount // nil: no such operator
	lookupErr   error
	recordErr   error
	openErr     error
	completeErr error
	touchErr    error
	closeErr    error
	log         *storeLog
}

// storeLog is what the fake database saw, kept across the resets between arms.
type storeLog struct {
	calls map[string]int
	got   map[string][][]string
}

// storeArity is how many credential-bearing arguments each recording method takes.
var storeArity = map[string]int{
	"OperatorByEmail": 1, "RecordOperatorAuthEvent": 1, "OpenOperatorSession": 1,
	"CompleteOperatorEnrollment": 4, "TouchOperatorSession": 1, "CloseOperatorSession": 1,
}

func (s *leakStore) see(method string, vals ...string) {
	s.log.got[method] = append(s.log.got[method], vals)
}

func (s *leakStore) lookup() (db.OperatorAccount, error) {
	if s.lookupErr != nil {
		return db.OperatorAccount{}, s.lookupErr
	}
	if s.acct == nil {
		return db.OperatorAccount{}, db.ErrNoOperator
	}
	return *s.acct, nil
}
func (s *leakStore) OperatorByEmail(_ context.Context, email string) (db.OperatorAccount, error) {
	s.log.calls["OperatorByEmail"]++
	s.see("OperatorByEmail", email)
	return s.lookup()
}
func (s *leakStore) OperatorByID(context.Context, uuid.UUID) (db.OperatorAccount, error) {
	return s.lookup()
}
func (s *leakStore) RecordOperatorAuthEvent(_ context.Context, _ db.OperatorAuthEvent, email string, _ uuid.UUID) error {
	s.log.calls["RecordOperatorAuthEvent"]++
	s.see("RecordOperatorAuthEvent", email)
	return s.recordErr
}
func (s *leakStore) OpenOperatorSession(_ context.Context, _ uuid.UUID, h string, _ int64) error {
	s.log.calls["OpenOperatorSession"]++
	s.see("OpenOperatorSession", h)
	return s.openErr
}
func (s *leakStore) CompleteOperatorEnrollment(_ context.Context, _ uuid.UUID, raw, digest string, sealed []byte, _ int64, h string) error {
	s.log.calls["CompleteOperatorEnrollment"]++
	s.see("CompleteOperatorEnrollment", raw, digest, string(sealed), h)
	return s.completeErr
}
func (s *leakStore) TouchOperatorSession(_ context.Context, h string) (db.OperatorSession, error) {
	s.log.calls["TouchOperatorSession"]++
	s.see("TouchOperatorSession", h)
	return db.OperatorSession{}, s.touchErr
}
func (s *leakStore) CloseOperatorSession(_ context.Context, h string) error {
	s.log.calls["CloseOperatorSession"]++
	s.see("CloseOperatorSession", h)
	return s.closeErr
}

// errFakeDB stands for a database failure that is not a refusal. It carries nothing a
// request supplied, so any input found in a returned error was put there by the flow.
var errFakeDB = errors.New("db: fake failure (SQLSTATE XX000)")

// otpAt is RFC 6238 (HMAC-SHA1, 6 digits, 30 s), written again here from the RFC so
// the external test can type a code the Authenticator accepts -- or one it refuses.
func otpAt(key []byte, at time.Time) string {
	var c [8]byte
	binary.BigEndian.PutUint64(c[:], uint64(at.Unix()/30))
	m := hmac.New(sha1.New, key)
	_, _ = m.Write(c[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	s := strconv.FormatUint(uint64(bin%1000000), 10)
	return strings.Repeat("0", 6-len(s)) + s
}

// otpNotAt is a six-digit code valid nowhere in the ±1-step window at at.
func otpNotAt(key []byte, at time.Time) string {
	valid := map[string]bool{otpAt(key, at.Add(-30*time.Second)): true, otpAt(key, at): true, otpAt(key, at.Add(30*time.Second)): true}
	for i := 100000; ; i++ {
		if s := strconv.Itoa(i); !valid[s] {
			return s
		}
	}
}

// cookieValue is the value an entry point's product carries in its Set-Cookie line --
// the one way an external caller sees a challenge or a session token in the clear.
func cookieValue(t *testing.T, write func(http.ResponseWriter) error) string {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := write(rec); err != nil {
		t.Fatalf("write the cookie: %v", err)
	}
	c := rec.Result().Cookies()
	if len(c) != 1 || c[0].Value == "" {
		t.Fatalf("expected one non-empty cookie, got %d", len(c))
	}
	return c[0].Value
}

// errNoSentinel marks the arms whose error has no exported sentinel: the stored
// envelope not opening (a server fault, deliberately not the operator's failure), a
// page asked for the nil id, and an empty cookie refused by its setter.
var errNoSentinel = errors.New("test: an error without an exported sentinel")

// ------------------------------------------------------------ the contract --

// The member GROUPS of the search set, numbered. Each is filled by the drive below
// from the source named in its comment.
const (
	gSessionToken = "G1 session token, raw"    // the session cookie the sign-in set; fakeSession; the malformed cookie; PLUS every raw preimage of a G2 hash (preimageFound)
	gSessionHash  = "G2 session token hash"    // HMAC(token key, G1 cookie), computed here; every hash the fake store received
	gTOTPCode     = "G3 TOTP code"             // the right code at the current step, for both secrets; the wrong and the malformed codes
	gTOTPSecret   = "G4 TOTP secret"           // the account's key and the enrollment page's secret, as bytes
	gEnrollToken  = "G5 enrollment token"      // the raw token, the malformed one, and the raw token the fake store received
	gEnrollHash   = "G6 enrollment token hash" // EnrollmentTokenHash of G5's test values
	gPassword     = "G7 password"              // the passphrase, the wrong, the weak and the over-long one (the enrollment's NEW password is the same passphrase)
	gDigest       = "G8 password digest"       // the account's digest; every new digest the fake store received
	gEnvelope     = "G9 TOTP envelope"         // both accounts' envelopes; every new envelope the fake store received
	gPendingBlob  = "G10 pending blob"         // the enrollment page's blob, fakeBlob, the over-long blob
	gKEK          = "G11 TOTP KEK"             // both KEKs, the 31-byte one New refuses, and the pending-blob key derived from each (12c)
	gHMACKey      = "G12 token HMAC key"       // the key, and the 33-byte one New refuses
	gChallengeKey = "G13 challenge MAC key"    // HMAC(token key, the derivation label), computed here (units_test pins the label)
	gChallenge    = "G14 login challenge"      // both challenges the password step minted; fakeChallenge
	gEmail        = "G15 operator address"     // the address typed; every address the fake store received
	gClientAddr   = "G16 client address"       // every rate key the drive used (not on a never-log list: the package's own claim, flow.go)
	gTOTPCodeNear = "G17 TOTP code, ±1 step"   // the step before and the step after, for both secrets (live credentials: ADR 0020 §5)
)

// neverLog is the CLOSED criterion the search set is measured against: each
// never-log item of CLAUDE.md §7, ADR 0020 §5 and ADR 0021 §3.5 that exists in this
// package, and the group(s) that stand for it. The test requires every item to have at
// least one member in EACH of its groups. Items that do not exist in this package are named in the header
// (CMAC, invite code, full GPS coordinate, read ticket).
var neverLog = []struct {
	item   string
	groups []string
}{
	{"N1 operator session token (CLAUDE.md §7; ADR 0020 §5)", []string{gSessionToken}},
	{"N2 its hash (ADR 0020 §5)", []string{gSessionHash}},
	{"N3 TOTP code, the current one and the ±1 window's (CLAUDE.md §7; ADR 0020 §5; ADR 0021 §3.5)", []string{gTOTPCode, gTOTPCodeNear}},
	{"N4 TOTP secret (CLAUDE.md §7; ADR 0020 §5)", []string{gTOTPSecret}},
	{"N5 enrollment token (CLAUDE.md §7; ADR 0020 §5; ADR 0021 §3.5)", []string{gEnrollToken}},
	{"N6 its hash (ADR 0020 §5; ADR 0021 §3.5)", []string{gEnrollHash}},
	{"N7 password and new password (ADR 0020 §5)", []string{gPassword}},
	{"N8 password digest (ADR 0020 §5)", []string{gDigest}},
	{"N9 TOTP envelope (ADR 0020 §5)", []string{gEnvelope}},
	{"N10 pending blob -- an envelope of the TOTP secret (ADR 0020 §5)", []string{gPendingBlob}},
	{"N11 AES key: the TOTP KEK (CLAUDE.md §7)", []string{gKEK}},
	{"N12 token HMAC key (ADR 0020 §2: its own secret variable)", []string{gHMACKey}},
	{"N13 challenge MAC key, derived from N12", []string{gChallengeKey}},
	{"N14 login challenge: a post-password bearer credential (challenge.go)", []string{gChallenge}},
	{"N15 operator address (ADR 0020 §5: an operator is named by id, not address)", []string{gEmail}},
}

// minNeedle is the shortest rendering searched for. The shortest MEMBERS are the
// six-digit codes, and they stay in the set as they are: the package's own texts carry
// no run of six digits (the only numbers in them are key sizes, the audit cap and its
// window), so a code found in a text was put there. A false positive would surface as a
// RED test naming the member's index, never as silence.
const minNeedle = 6

// theRenderings are the SEARCHED renderings, numbered. A value leaked in a form not on
// this list is not seen -- the header names the forms measured and not searched.
var theRenderings = []struct {
	name string
	fn   func(v []byte) string
}{
	{"R1 raw", func(v []byte) string { return string(v) }},
	{"R2 %q inside", func(v []byte) string { q := strconv.Quote(string(v)); return q[1 : len(q)-1] }},
	{"R3 JSON string inside", func(v []byte) string { j, _ := json.Marshal(string(v)); return string(j[1 : len(j)-1]) }},
	{"R4 %x", func(v []byte) string { return hex.EncodeToString(v) }},
	{"R5 %X", func(v []byte) string { return strings.ToUpper(hex.EncodeToString(v)) }},
	{"R6 % x", func(v []byte) string { return fmt.Sprintf("% x", v) }},
	{"R7 % X", func(v []byte) string { return fmt.Sprintf("% X", v) }},
	{"R8 base32, padded", func(v []byte) string { return base32.StdEncoding.EncodeToString(v) }},
	{"R9 base32, unpadded", func(v []byte) string { return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(v) }},
	{"R10 base64 std, padded", func(v []byte) string { return base64.StdEncoding.EncodeToString(v) }},
	{"R11 base64 std, raw", func(v []byte) string { return base64.RawStdEncoding.EncodeToString(v) }},
	{"R12 base64 url, padded", func(v []byte) string { return base64.URLEncoding.EncodeToString(v) }},
	{"R13 base64 url, raw", func(v []byte) string { return base64.RawURLEncoding.EncodeToString(v) }},
	{"R14 %v of []byte", func(v []byte) string { return fmt.Sprint(v) }},
	{"R15 %#v of []byte", func(v []byte) string { return fmt.Sprintf("%#v", v) }},
}

// searchSet is the members, each with its group, and each member's searched renderings.
type searchSet struct {
	members [][]byte
	group   []string
	has     map[string]bool
	needles map[string]int // rendering -> index of its member
}

func (s *searchSet) add(group string, vals ...[]byte) {
	for _, v := range vals {
		if len(v) == 0 || s.has[string(v)] {
			continue
		}
		s.has[string(v)] = true
		i := len(s.members)
		s.members = append(s.members, append([]byte(nil), v...))
		s.group = append(s.group, group)
		for _, r := range theRenderings {
			if n := r.fn(v); len(n) >= minNeedle {
				if _, dup := s.needles[n]; !dup {
					s.needles[n] = i
				}
			}
		}
	}
}

func (s *searchSet) addStrings(group string, vals ...string) {
	for _, v := range vals {
		s.add(group, []byte(v))
	}
}

// inGroup reports whether the set holds a member of group.
func (s *searchSet) inGroup(group string) bool {
	for _, g := range s.group {
		if g == group {
			return true
		}
	}
	return false
}

// hits is the members whose rendering occurs in text.
func (s *searchSet) hits(text string) map[int]bool {
	out := map[int]bool{}
	for n, i := range s.needles {
		if strings.Contains(text, n) {
			out[i] = true
		}
	}
	return out
}

// sessionTokenLen is a session token's length: 256 bits in unpadded base64url.
const sessionTokenLen = 43

var (
	b64urlChars   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	spacedHexRun  = regexp.MustCompile(`(?:[0-9a-fA-F]{2} )+[0-9a-fA-F]{2}`)
	decimalList   = regexp.MustCompile(`\[(\d{1,3}(?: \d{1,3})*)\]`)
	goByteLiteral = regexp.MustCompile(`\[\]byte\{((?:0x[0-9a-f]{2}(?:, )?)+)\}`)
)

// byteViews are the byte strings a text holds under the inverse of each searched
// rendering (R1-R15): the text itself (R1-R3: a session token is plain base64url, so
// quoting leaves it as it is), every hex run decoded (R4, R5), every spaced-hex run
// (R6, R7), every window a base32 or base64 decoder accepts (R8-R13), and every
// decimal list and []byte literal (R14, R15).
func byteViews(text string) [][]byte {
	views := [][]byte{[]byte(text)}
	for i := 0; i < len(text); i++ {
		for _, w := range []struct {
			n   int
			dec func(string) ([]byte, error)
		}{
			{2 * sessionTokenLen, hex.DecodeString},
			{72, base32.StdEncoding.DecodeString}, {69, base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString},
			{60, base64.StdEncoding.DecodeString}, {58, base64.RawStdEncoding.DecodeString},
			{60, base64.URLEncoding.DecodeString}, {58, base64.RawURLEncoding.DecodeString},
		} {
			if i+w.n <= len(text) {
				if b, err := w.dec(text[i : i+w.n]); err == nil {
					views = append(views, b)
				}
			}
		}
	}
	for _, m := range spacedHexRun.FindAllString(text, -1) {
		if b, err := hex.DecodeString(strings.ReplaceAll(m, " ", "")); err == nil {
			views = append(views, b)
		}
	}
	for _, m := range decimalList.FindAllStringSubmatch(text, -1) {
		var b []byte
		for _, f := range strings.Fields(m[1]) {
			n, err := strconv.Atoi(f)
			if err != nil || n > 255 {
				b = nil
				break
			}
			b = append(b, byte(n))
		}
		views = append(views, b)
	}
	for _, m := range goByteLiteral.FindAllStringSubmatch(text, -1) {
		var b []byte
		for _, f := range strings.Split(m[1], ", ") {
			if n, err := strconv.ParseUint(strings.TrimPrefix(f, "0x"), 16, 8); err == nil {
				b = append(b, byte(n))
			}
		}
		views = append(views, b)
	}
	return views
}

// preimageFound reports whether text holds, under a searched rendering, a session
// token whose HMAC under key is one of hashes -- i.e. a RAW session token the test
// never saw (the one a failing sign-in or enrollment minted), found through the hash
// the fake store received. This is how G1 covers the tokens the test cannot know.
func preimageFound(text string, key []byte, hashes map[string]bool) bool {
	for _, v := range byteViews(text) {
		for i := 0; i+sessionTokenLen <= len(v); i++ {
			c := v[i : i+sessionTokenLen]
			if !b64urlChars.Match(c) {
				continue
			}
			m := hmac.New(sha256.New, key)
			_, _ = m.Write(c)
			if hashes[hex.EncodeToString(m.Sum(nil))] {
				return true
			}
		}
	}
	return false
}

// errorRenderings are the forms a returned error takes in a caller's hands.
func errorRenderings(e error) []string {
	return []string{e.Error(), fmt.Sprintf("%v", e), fmt.Sprintf("%+v", e), fmt.Sprintf("%#v", e)}
}

// fanout sends every record to both of the product's handler kinds (cmd/tappa's
// logHandler: text by default, JSON by configuration), so the search sees each
// rendering.
type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}
func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range f {
		if err := h.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	return nil
}
func (f fanout) WithAttrs(as []slog.Attr) slog.Handler {
	g := make(fanout, len(f))
	for i, h := range f {
		g[i] = h.WithAttrs(as)
	}
	return g
}
func (f fanout) WithGroup(n string) slog.Handler {
	g := make(fanout, len(f))
	for i, h := range f {
		g[i] = h.WithGroup(n)
	}
	return g
}

// debugCapture is a logger that keeps every level down to Debug -- the level the
// product can be run at (TAPPA_LOG_LEVEL) -- in both handler kinds.
func debugCapture(w *bytes.Buffer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	return slog.New(fanout{slog.NewTextHandler(w, opts), slog.NewJSONHandler(w, opts)})
}

// TestLeak_NoInputInAnyErrorOrLogLine -- THE CONTRACT, AND ONLY THE CONTRACT:
//
// No member of the 17 groups G1-G17 (the constants above; their sources in their
// comments) occurs, in any of the 15 renderings R1-R15 (theRenderings), in the errors
// the 45 numbered arms below return -- Error(), %v, %+v, %#v -- or in the log captured
// at Debug level through the text and the JSON handler. G1 additionally covers every
// raw session token whose hash the fake store received, found under R1-R15 by its HMAC
// (preimageFound). The groups are measured against a CLOSED list, neverLog: each
// never-log item of CLAUDE.md §7, ADR 0020 §5 and ADR 0021 §3.5 that exists here must
// have a member in each group it names (N3 names two: the current code and the ±1
// codes); G16 (the client address) is deliberately bound to no item -- it is on no
// never-log list and is searched as the package's own claim. The harness's own
// recording is pinned method by method (storeArity) -- by COUNT and ARITY, not content:
// a vector recorded with the right number of wrong values is not seen. POSITIVE
// CONTROLS: each rendering R1-R15 is paired, by name, with its own builder (the fmt verb
// or the encoder's streaming writer), and for every member the rendering's needle must
// be searched and must occur in that builder's text; each member is also put into an
// error and into a Debug log line through the builders and must be found; and a
// synthetic raw token must be found through its hash. AFTER the arms and the success
// paths of Verify and Logout, checkBudgetKeys pins each budget map's keys by content
// (the client addresses the drive used, its operators' ids, ""); and
// checkFirstFactorLines pins the two lines an operator's first-factor trail logs to its
// id (OP-14 phase C: the cap crossed; 3rd round: a fail-closed refusal) -- below the caps
// and without a refusal the drive logs nothing.
//
// THIS test searches what the ENTRY POINTS return and log. What the package's TYPES
// print is TestLeak_NoSecretOnAnyPrintingPath's contract, numbered with render: every
// fmt verb (printVerbs) × the shapes S1-S6 (printHolders: the value, an exported and an
// unexported any field, the pointee in an unexported field, a slice element, a map
// value) × fmt.Sprintf and fmt.Errorf, plus F1-F5 (slog text and JSON, json.Marshal),
// over every specimen of the closed type set.
//
// NOT CLAIMED, BY NAME:
//   - split or partial values (a code printed as "123-456", a prefix of a token);
//   - renderings not on the list -- measured (OP-6 verification, 5th round, seven
//     sample values: a code, a 20-byte key, a random envelope, a token, an address, a
//     digest, a non-ASCII text) and NOT found for at least one of them: %+q of a value
//     with non-ASCII bytes, % #x, %b and %o per byte, base32 with the hex alphabet,
//     ascii85, MIME-wrapped base64, URL escaping of a value with reserved or non-ASCII
//     bytes, HTML escaping of a value with <>&'" in it, reversed bytes, a [N]uint8
//     array's %#v, a case-folded raw value. (Measured and found through R1-R15 although
//     not listed: %#q, %#x, %s and %d of a []byte, %v of a [N]byte, slog's text and JSON
//     rendering of a []byte, json.Marshal of a []byte.);
//   - never-log items that do not exist in this package: CMAC, invite code, full GPS
//     coordinate, read ticket (OP-10);
//   - the arms that cannot be made to fail from outside: a failing crypto/rand read (the
//     secret, the challenge's nonce, a session or enrollment token, the dummy digest's
//     seed), a failing bcrypt generation, a failing Seal, a failing token hash (its key
//     size is refused by New) -- each returns a fixed errors.New text or a wrapped
//     randomness error;
//   - the CONTENT of what the fake store recorded: the harness pin holds the number of
//     calls and each vector's arity, so a recording of the right shape with the wrong
//     values is not seen.
//
// (The list is m10-platform.md, OP-6 card correction, md. 18, "Tek liste (11. tur)", S8
// and S9; the closed type set's and the field rule's are S1 to S7.)
//
// THE ARMS, NUMBERED (45; flow.go's order, then the helpers; E10 added in 12c, P7 and A2
// in OP-14 phase C):
//
//	Password  P1 work budget refused · P2 lookup fails · P3 unknown address · P4 its row
//	          cannot be written · P5 wrong passphrase · P6 its row cannot be written · P7
//	          right passphrase, its 'password_ok' row cannot be written (no challenge)
//	TOTP      T1 challenge does not verify · T2 account budget refused · T3 account no
//	          longer active · T4 lookup fails · T5 envelope does not open · T6 wrong code
//	          · T7 wrong code, locked · T8 its row cannot be written · T9 right code the
//	          database refuses · T10 the same, locked · T11 its row cannot be written ·
//	          T12 opening the session fails
//	Verify    V1 malformed token · V2 dead session · V3 the check fails
//	Logout    L1 malformed token · L2 dead session · L3 the close fails
//	Enroll    E1 work budget refused · E2 password rule (short, over-long) · E3 malformed
//	          token, nil id · E4 foreign or over-long page · E5 wrong or malformed first
//	          code · E6 the refusal row cannot be written · E7 process budget refused · E8
//	          the database refuses the link · E9 completing fails · E10 the address's
//	          share of the enrollment budget refused
//	Begin     B1 the nil account id
//	Cookies   C1 an empty session token refused by its setter · C2 an empty challenge
//	          refused by its setter · K1/K2 session cookie absent / empty · K3/K4
//	          challenge cookie absent / empty
//	Audit cap A1 the one WARN line when the process-wide cap is crossed -- crossed with
//	          enrollment_failed rows, which carry no address (an address-bearing kind
//	          crossing it is TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter's
//	          arm; measured, 5th round: an address put into the WARN line stays green
//	          here and turns that test red)
//	          A2 the one WARN line when ONE operator's first-factor cap is crossed --
//	          right passphrases of one operator from fresh addresses, each served
//	          (OP-14 phase C; it names the operator's id, never an address)
//	New       N1 keys of the wrong size
//
// Why the claim is this narrow (OP-6 verification, 5th round -- the orchestrator's
// decision, agent-brief.md's M8-02 FAZ C lesson): four audit rounds in a row each found
// a value or a rendering the previous wording ("derived", "every credential", "none of
// them") did not cover. A black-box search can only speak for a numbered set; the
// wording now says the set, and what is outside it by name.
func TestLeak_NoInputInAnyErrorOrLogLine(t *testing.T) {
	var logs bytes.Buffer
	set := &searchSet{has: map[string]bool{}, needles: map[string]int{}}
	seenLog := &storeLog{calls: map[string]int{}, got: map[string][][]string{}}
	// usedAddrs and usedIDs are what the drive below hands the Authenticator as a client
	// address and what its operators' ids are: checkBudgetKeys's sets.
	usedAddrs, usedIDs := map[string]bool{}, map[string]bool{}
	addr := func(s string) string { set.addStrings(gClientAddr, s); usedAddrs[s] = true; return s }

	kek := bytes.Repeat([]byte("K"), 32)
	otherKEK := bytes.Repeat([]byte("J"), 32)
	hmacKey := bytes.Repeat([]byte("H"), 32)
	shortKEK, longHMAC := kek[:31], append(append([]byte(nil), hmacKey...), 'H')
	set.add(gKEK, kek, otherKEK, shortKEK)
	for _, k := range [][]byte{kek, otherKEK} {
		pm := hmac.New(sha256.New, k)
		_, _ = pm.Write([]byte(pendingKeyLabel))
		set.add(gKEK, pm.Sum(nil))
	}
	set.add(gHMACKey, hmacKey, longHMAC)
	ck := hmac.New(sha256.New, hmacKey)
	_, _ = ck.Write([]byte(challengeKeyLabel))
	set.add(gChallengeKey, ck.Sum(nil))
	now := time.Unix(1_800_000_015, 0)
	const (
		pass  = "FAKEpassphraseFAKEpassphrase"
		email = "fakeoperator@example.test"
		raw   = "FAKErawFAKErawFAKErawFAKErawFAKErawFAKEraw1" // 43 characters
		weak  = "FAKEweakFAKE"                                // 12 runes: below the rule
		junk  = "FAKEwrongShapeFAKE"                          // a session cookie value of the wrong shape
	)
	shortRaw, longPass, wrongPass := "short-"+raw[:10], strings.Repeat("P", 100), pass+"x"
	longBlob := strings.Repeat("A", 2000)
	set.addStrings(gPassword, pass, wrongPass, weak, longPass)
	set.addStrings(gEmail, email)
	set.addStrings(gEnrollToken, raw, shortRaw)
	set.addStrings(gEnrollHash, operatorauth.EnrollmentTokenHash(raw), operatorauth.EnrollmentTokenHash(shortRaw))
	set.addStrings(gPendingBlob, fakeBlob, longBlob)
	set.addStrings(gChallenge, fakeChallenge)
	set.addStrings(gSessionToken, fakeSession, junk)

	// The operators the store returns: a digest of pass (bcrypt's minimum cost -- this
	// test is about text, not time) and a real envelope of a known TOTP key. The second
	// one's envelope is sealed under ANOTHER KEK, so it never opens here (T5).
	key := bytes.Repeat([]byte{0x5a}, 20)
	digest, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	set.add(gTOTPSecret, key)
	set.add(gDigest, digest)
	account := func(k []byte) db.OperatorAccount {
		id := uuid.New()
		usedIDs[id.String()] = true
		sealed, err := sun.Seal(k, id[:], key)
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		set.add(gEnvelope, sealed)
		return db.OperatorAccount{ID: id, Digest: db.NewPasswordHash(string(digest)), Sealed: db.NewSealedSecret(sealed)}
	}
	acct := account(kek)
	foreign := account(otherKEK)
	locked := now.Add(10 * time.Minute)
	lockedAcct := acct
	lockedAcct.LockedUntil = &locked

	st := &leakStore{log: seenLog}
	a, err := operatorauth.New(st, operatorauth.Config{
		TOTPKEK: operatorauth.NewKey(kek), TokenHMACKey: operatorauth.NewKey(hmacKey), Now: func() time.Time { return now }, Log: debugCapture(&logs),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	near := func(k []byte) []string {
		return []string{otpAt(k, now.Add(-30*time.Second)), otpAt(k, now.Add(30*time.Second))}
	}
	good, wrong := otpAt(key, now), otpNotAt(key, now)
	set.addStrings(gTOTPCode, good, wrong)
	set.addStrings(gTOTPCodeNear, near(key)...)
	sentinels := []error{operatorauth.ErrRefused, operatorauth.ErrThrottled, operatorauth.ErrChallenge, operatorauth.ErrCodeRejected,
		operatorauth.ErrLocked, operatorauth.ErrNoSession, operatorauth.ErrEnrollment, operatorauth.ErrWeakPassword, errFakeDB}

	var errs []error
	// arm records one call: its error must be exactly the arm's outcome.
	arm := func(name string, err, want error) {
		t.Helper()
		switch {
		case want == errNoSentinel:
			for _, s := range sentinels {
				if err == nil || errors.Is(err, s) {
					t.Errorf("%s: %v, want an error without a sentinel -- the drive did not reach this arm", name, err)
					break
				}
			}
		case !errors.Is(err, want):
			t.Errorf("%s: %v, want %v -- the drive did not reach this arm", name, err, want)
		}
		errs = append(errs, err)
	}
	reset := func(s leakStore) { s.log = seenLog; *st = s }
	enroll := func(ad string, id uuid.UUID, rawTok string, blob operatorauth.PendingBlob, pw, code string) error {
		_, err := a.CompleteEnrollment(ctx, addr(ad), id, rawTok, blob, pw, code)
		return err
	}

	// ---- the password step
	reset(leakStore{lookupErr: errFakeDB})
	_, err = a.Password(ctx, addr("192.0.2.1"), email, pass)
	arm("P2 password: the lookup fails", err, errFakeDB)
	reset(leakStore{})
	_, err = a.Password(ctx, addr("192.0.2.1"), email, pass)
	arm("P3 password: unknown address", err, operatorauth.ErrRefused)
	reset(leakStore{recordErr: errFakeDB})
	_, err = a.Password(ctx, addr("192.0.2.1"), email, pass)
	arm("P4 password: the unknown_email row cannot be written", err, errFakeDB)
	reset(leakStore{acct: &acct})
	_, err = a.Password(ctx, addr("192.0.2.1"), email, wrongPass)
	arm("P5 password: wrong passphrase", err, operatorauth.ErrRefused)
	reset(leakStore{acct: &acct, recordErr: errFakeDB})
	_, err = a.Password(ctx, addr("192.0.2.1"), email, wrongPass)
	arm("P6 password: the login_failed row cannot be written", err, errFakeDB)
	reset(leakStore{acct: &acct, recordErr: errFakeDB})
	_, err = a.Password(ctx, addr("192.0.2.1"), email, pass)
	arm("P7 password: right passphrase, the password_ok row cannot be written", err, errFakeDB)
	reset(leakStore{acct: &acct})
	ch, err := a.Password(ctx, addr("192.0.2.1"), email, pass)
	if err != nil {
		t.Fatalf("password: the right passphrase: %v", err)
	}
	set.addStrings(gChallenge, cookieValue(t, func(w http.ResponseWriter) error { return operatorauth.SetChallengeCookie(w, ch) }))
	reset(leakStore{acct: &foreign})
	chForeign, err := a.Password(ctx, addr("192.0.2.1"), email, pass)
	if err != nil {
		t.Fatalf("password: the right passphrase, second operator: %v", err)
	}
	set.addStrings(gChallenge, cookieValue(t, func(w http.ResponseWriter) error { return operatorauth.SetChallengeCookie(w, chForeign) }))

	// ---- the TOTP step, with the challenges the password step minted
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: operatorauth.ChallengeCookieName, Value: fakeChallenge})
	req.AddCookie(&http.Cookie{Name: operatorauth.SessionCookieName, Value: fakeSession})
	fakeCh, _ := operatorauth.ReadChallengeCookie(req)
	_, err = a.TOTP(ctx, fakeCh, good)
	arm("T1 totp: a challenge that does not verify", err, operatorauth.ErrChallenge)
	reset(leakStore{})
	_, err = a.TOTP(ctx, chForeign, good)
	arm("T3 totp: the account is no longer active", err, operatorauth.ErrRefused)
	reset(leakStore{acct: &foreign})
	_, err = a.TOTP(ctx, chForeign, good)
	arm("T5 totp: the stored envelope does not open", err, errNoSentinel)
	reset(leakStore{lookupErr: errFakeDB})
	_, err = a.TOTP(ctx, ch, good)
	arm("T4 totp: the lookup fails", err, errFakeDB)
	reset(leakStore{acct: &acct})
	_, err = a.TOTP(ctx, ch, wrong)
	arm("T6 totp: wrong code", err, operatorauth.ErrCodeRejected)
	reset(leakStore{acct: &lockedAcct})
	_, err = a.TOTP(ctx, ch, wrong)
	arm("T7 totp: wrong code, account locked", err, operatorauth.ErrLocked)
	reset(leakStore{acct: &acct, recordErr: errFakeDB})
	_, err = a.TOTP(ctx, ch, wrong)
	arm("T8 totp: the failed-code row cannot be written", err, errFakeDB)
	reset(leakStore{acct: &acct, openErr: db.ErrOperatorRefused})
	_, err = a.TOTP(ctx, ch, good)
	arm("T9 totp: right code the database refuses", err, operatorauth.ErrCodeRejected)
	reset(leakStore{acct: &lockedAcct, openErr: db.ErrOperatorRefused})
	_, err = a.TOTP(ctx, ch, good)
	arm("T10 totp: right code the database refuses, account locked", err, operatorauth.ErrLocked)
	reset(leakStore{acct: &acct, openErr: db.ErrOperatorRefused, recordErr: errFakeDB})
	_, err = a.TOTP(ctx, ch, good)
	arm("T11 totp: the refused-code row cannot be written", err, errFakeDB)
	reset(leakStore{acct: &acct, openErr: errFakeDB})
	_, err = a.TOTP(ctx, ch, good)
	arm("T12 totp: opening the session fails", err, errFakeDB)
	reset(leakStore{acct: &acct})
	iss, err := a.TOTP(ctx, ch, good)
	if err != nil {
		t.Fatalf("totp: the right code: %v", err)
	}
	session := cookieValue(t, func(w http.ResponseWriter) error { return operatorauth.SetSessionCookie(w, iss.Token) })
	set.addStrings(gSessionToken, session)
	sessionHash := hmac.New(sha256.New, hmacKey)
	_, _ = sessionHash.Write([]byte(session))
	set.addStrings(gSessionHash, hex.EncodeToString(sessionHash.Sum(nil)))
	// T2: the same challenge until the account budget refuses (bounded).
	for i := 0; ; i++ {
		if i > 20 {
			t.Fatal("T2: the account budget never refused")
		}
		_, err = a.TOTP(ctx, ch, good)
		if errors.Is(err, operatorauth.ErrThrottled) {
			break
		}
		if err != nil {
			t.Fatalf("T2: before the budget refused: %v", err)
		}
	}
	arm("T2 totp: the account budget refuses", err, operatorauth.ErrThrottled)

	// ---- the session check and the logout
	tok, _ := operatorauth.ReadSessionCookie(req)
	bad := httptest.NewRequest(http.MethodGet, "/", nil)
	bad.AddCookie(&http.Cookie{Name: operatorauth.SessionCookieName, Value: junk})
	malformed, _ := operatorauth.ReadSessionCookie(bad)
	_, err = a.Verify(ctx, malformed)
	arm("V1 verify: a malformed token", err, operatorauth.ErrNoSession)
	reset(leakStore{touchErr: db.ErrOperatorRefused})
	_, err = a.Verify(ctx, tok)
	arm("V2 verify: a dead session", err, operatorauth.ErrNoSession)
	reset(leakStore{touchErr: errFakeDB})
	_, err = a.Verify(ctx, iss.Token)
	arm("V3 verify: the check fails", err, errFakeDB)
	arm("L1 logout: a malformed token", a.Logout(ctx, malformed), operatorauth.ErrNoSession)
	reset(leakStore{closeErr: db.ErrOperatorRefused})
	arm("L2 logout: a dead session", a.Logout(ctx, tok), operatorauth.ErrNoSession)
	reset(leakStore{closeErr: errFakeDB})
	arm("L3 logout: the close fails", a.Logout(ctx, iss.Token), errFakeDB)
	// The two success paths, no arm (no error): driven so that a budget a success path
	// charged would be read by checkBudgetKeys (12b, the 12th auditor: a raw token
	// charged on Verify's success path and a charge on Logout's were green).
	reset(leakStore{})
	if _, err := a.Verify(ctx, iss.Token); err != nil {
		t.Fatalf("verify: the success path: %v", err)
	}
	reset(leakStore{})
	if err := a.Logout(ctx, iss.Token); err != nil {
		t.Fatalf("logout: the success path: %v", err)
	}

	// ---- the helpers: the page for the nil id, the cookie setters and readers
	_, err = a.BeginEnrollment(uuid.Nil)
	arm("B1 begin: the nil account id", err, errNoSentinel)
	arm("C1 cookie: an empty session token", operatorauth.SetSessionCookie(httptest.NewRecorder(), operatorauth.SessionToken{}), errNoSentinel)
	arm("C2 cookie: an empty challenge", operatorauth.SetChallengeCookie(httptest.NewRecorder(), operatorauth.Challenge{}), errNoSentinel)
	none := httptest.NewRequest(http.MethodGet, "/", nil)
	empty := httptest.NewRequest(http.MethodGet, "/", nil)
	empty.Header.Set("Cookie", operatorauth.SessionCookieName+"=; "+operatorauth.ChallengeCookieName+"=")
	_, err = operatorauth.ReadSessionCookie(none)
	arm("K1 cookie: no session cookie", err, operatorauth.ErrNoSession)
	_, err = operatorauth.ReadSessionCookie(empty)
	arm("K2 cookie: an empty session cookie", err, operatorauth.ErrNoSession)
	_, err = operatorauth.ReadChallengeCookie(none)
	arm("K3 cookie: no challenge cookie", err, operatorauth.ErrChallenge)
	_, err = operatorauth.ReadChallengeCookie(empty)
	arm("K4 cookie: an empty challenge cookie", err, operatorauth.ErrChallenge)

	// ---- the enrollment's last step
	eid := uuid.New()
	usedIDs[eid.String()] = true
	pend, err := a.BeginEnrollment(eid)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(pend.Secret.Base32())
	if err != nil {
		t.Fatalf("decode the page's secret: %v", err)
	}
	set.add(gTOTPSecret, secret)
	set.addStrings(gPendingBlob, pend.Blob.RevealForForm())
	egood, ewrong := otpAt(secret, now), otpNotAt(secret, now)
	malformedCode := "not a code " + egood
	set.addStrings(gTOTPCode, egood, ewrong, malformedCode)
	set.addStrings(gTOTPCodeNear, near(secret)...)
	for _, c := range []struct {
		name            string
		store           leakStore
		id              uuid.UUID
		raw, pass, code string
		blob            operatorauth.PendingBlob
		want            error
	}{
		{"E2 enrollment: a weak passphrase", leakStore{}, eid, raw, weak, egood, pend.Blob, operatorauth.ErrWeakPassword},
		{"E2 enrollment: an over-long passphrase", leakStore{}, eid, raw, longPass, egood, pend.Blob, operatorauth.ErrWeakPassword},
		{"E3 enrollment: a malformed token", leakStore{}, eid, shortRaw, pass, egood, pend.Blob, operatorauth.ErrEnrollment},
		{"E3 enrollment: the nil id", leakStore{}, uuid.Nil, raw, pass, egood, pend.Blob, operatorauth.ErrEnrollment},
		{"E4 enrollment: a foreign page", leakStore{}, eid, raw, pass, egood, operatorauth.PendingBlobFromForm(fakeBlob), operatorauth.ErrEnrollment},
		{"E4 enrollment: an over-long page", leakStore{}, eid, raw, pass, egood, operatorauth.PendingBlobFromForm(longBlob), operatorauth.ErrEnrollment},
		{"E5 enrollment: a wrong first code", leakStore{}, eid, raw, pass, ewrong, pend.Blob, operatorauth.ErrCodeRejected},
		{"E5 enrollment: a malformed first code", leakStore{}, eid, raw, pass, malformedCode, pend.Blob, operatorauth.ErrCodeRejected},
		{"E6 enrollment: the refusal row cannot be written", leakStore{recordErr: errFakeDB}, eid, raw, pass, ewrong, pend.Blob, errFakeDB},
		{"E8 enrollment: the database refuses the link", leakStore{completeErr: db.ErrOperatorRefused}, eid, raw, pass, egood, pend.Blob, operatorauth.ErrEnrollment},
		{"E9 enrollment: completing fails", leakStore{completeErr: errFakeDB}, eid, raw, pass, egood, pend.Blob, errFakeDB},
	} {
		reset(c.store)
		arm(c.name, enroll(addr("192.0.2.1"), c.id, c.raw, c.blob, c.pass, c.code), c.want)
	}
	// E10: requests that pass every check Go can make, from ONE address, until that
	// address's share of the enrollment budget refuses (12c) -- E7 below then still
	// spends the process budget's remainder from other addresses.
	reset(leakStore{completeErr: db.ErrOperatorRefused})
	for i := 0; ; i++ {
		if i > 10 {
			t.Fatal("E10: the per-address enrollment share never refused")
		}
		err = enroll("198.51.100.200", eid, raw, pend.Blob, pass, egood)
		if errors.Is(err, operatorauth.ErrThrottled) {
			break
		}
		arm("E10 enrollment: under the address's share", err, operatorauth.ErrEnrollment)
	}
	arm("E10 enrollment: the address's share refuses", err, operatorauth.ErrThrottled)
	// E7: requests that pass every check Go can make, from fresh addresses (so the
	// per-address budgets are not what refuses), until the process-wide budget refuses.
	reset(leakStore{completeErr: db.ErrOperatorRefused})
	e7passed := 0
	for i := 0; ; i++ {
		if i > 20 {
			t.Fatal("E7: the process-wide enrollment budget never refused")
		}
		err = enroll("198.51.100."+strconv.Itoa(10+i), eid, raw, pend.Blob, pass, egood)
		if errors.Is(err, operatorauth.ErrThrottled) {
			break
		}
		arm("E7 enrollment: under the process budget", err, operatorauth.ErrEnrollment)
		e7passed++
	}
	if e7passed == 0 {
		t.Error("E7: no request from another address passed after E10's address was refused; the process budget's remainder was not its")
	}
	arm("E7 enrollment: the process budget refuses", err, operatorauth.ErrThrottled)
	// P1 and E1: one address spends its work budget on requests refused before any
	// bcrypt (the password rule), then both entry points are refused by it.
	reset(leakStore{acct: &acct})
	for i := 0; ; i++ {
		if i > 40 {
			t.Fatal("E1: the work budget never refused")
		}
		err = enroll(addr("192.0.2.2"), eid, raw, pend.Blob, weak, egood)
		if errors.Is(err, operatorauth.ErrThrottled) {
			break
		}
		arm("E2 enrollment: a weak passphrase, spending the work budget", err, operatorauth.ErrWeakPassword)
	}
	arm("E1 enrollment: the work budget refuses", err, operatorauth.ErrThrottled)
	arm("E1 enrollment: the work budget refuses a well-formed request", enroll(addr("192.0.2.2"), eid, raw, pend.Blob, pass, egood), operatorauth.ErrThrottled)
	_, err = a.Password(ctx, addr("192.0.2.2"), email, pass)
	arm("P1 password: the work budget refuses", err, operatorauth.ErrThrottled)

	// ---- a constructor refusing its keys
	for _, badCfg := range []operatorauth.Config{
		{TOTPKEK: operatorauth.NewKey(shortKEK), TokenHMACKey: operatorauth.NewKey(hmacKey), Log: slog.Default()},
		{TOTPKEK: operatorauth.NewKey(kek), TokenHMACKey: operatorauth.NewKey(longHMAC), Log: slog.Default()},
	} {
		_, err = operatorauth.New(st, badCfg)
		if err == nil {
			t.Error("N1: New accepted a key of the wrong size")
		}
		errs = append(errs, err)
	}
	// Below the caps the drive's paths log one kind of line only: P7's fail-closed refusal
	// (OP-14 C, 3rd round, F4), naming its operator once. Until OP-14 phase C the password
	// step's success logged one Info line with the operator's id (12c); the durable
	// 'password_ok' row replaced it.
	checkFirstFactorLines(t, logs.String(), nil, []string{acct.ID.String()})

	// ---- A2: the line logged when ONE operator's first-factor cap is crossed (OP-14
	// phase C). Right passphrases of one operator, from fresh addresses (the work
	// budget is per address), until the line appears; each request is served.
	reset(leakStore{acct: &acct})
	for i := 0; !strings.Contains(logs.String(), firstFactorCapMsg); i++ {
		if i > 20 {
			t.Fatal("A2: the first-factor cap's WARN line never appeared")
		}
		_, err = a.Password(ctx, addr("198.51.100."+strconv.Itoa(60+i)), email, pass)
		arm("A2 password: a right passphrase, filling the operator's first-factor cap", err, nil)
	}
	checkFirstFactorLines(t, logs.String(), []string{acct.ID.String()}, []string{acct.ID.String()})

	// ---- A1: the other line this package logs, when the process-wide audit cap is
	// crossed. Refusals that write a password-less row and pay no bcrypt (a malformed
	// token), from fresh addresses, until the line appears.
	reset(leakStore{})
	linesBefore := strings.Count(logs.String(), "\n")
	for i := 0; !strings.Contains(logs.String(), "operator pre-session audit rows suppressed"); i++ {
		if i > 100 {
			t.Fatal("A1: the audit cap's WARN line never appeared")
		}
		arm("A1 enrollment: a malformed token, filling the audit cap",
			enroll("203.0.113."+strconv.Itoa(10+i/15), eid, shortRaw, pend.Blob, pass, egood), operatorauth.ErrEnrollment)
	}
	if n := strings.Count(logs.String(), "\n") - linesBefore; n != 2 {
		t.Errorf("A1: %d log line(s) when the cap was crossed, want 2 (one per handler kind)", n)
	}

	// ---- HARNESS INTEGRITY: every recording method recorded one vector of its arity
	// per call, and the drive reached each of them.
	for m, n := range storeArity {
		if seenLog.calls[m] == 0 {
			t.Errorf("harness: %s was never called; the drive does not reach it", m)
		}
		if len(seenLog.got[m]) != seenLog.calls[m] {
			t.Errorf("harness: %s was called %d time(s) and recorded %d", m, seenLog.calls[m], len(seenLog.got[m]))
		}
		for _, v := range seenLog.got[m] {
			if len(v) != n {
				t.Errorf("harness: %s recorded %d value(s) in a call, want %d", m, len(v), n)
			}
		}
	}
	// What the fake database received joins the set, each value in its group; the
	// session hashes are also the keys of the raw-token (G1) search.
	hashes := map[string]bool{}
	hashArg := func(h string) { set.addStrings(gSessionHash, h); hashes[h] = true }
	for _, v := range seenLog.got["OperatorByEmail"] {
		set.addStrings(gEmail, v[0])
	}
	for _, v := range seenLog.got["RecordOperatorAuthEvent"] {
		set.addStrings(gEmail, v[0])
	}
	for _, m := range []string{"OpenOperatorSession", "TouchOperatorSession", "CloseOperatorSession"} {
		for _, v := range seenLog.got[m] {
			hashArg(v[0])
		}
	}
	for _, v := range seenLog.got["CompleteOperatorEnrollment"] {
		set.addStrings(gEnrollToken, v[0])
		set.addStrings(gDigest, v[1])
		set.add(gEnvelope, []byte(v[2]))
		hashArg(v[3])
	}

	// ---- THE CLOSED CRITERION: each never-log item has a member.
	if len(neverLog) != 15 {
		t.Errorf("the never-log table has %d item(s); it is a pinned literal of 15", len(neverLog))
	}
	for _, it := range neverLog {
		for _, g := range it.groups {
			if !set.inGroup(g) {
				t.Errorf("never-log item %q: group %q has no member", it.item, g)
			}
		}
	}

	// ---- the search
	for i, e := range errs {
		if e == nil {
			continue
		}
		for _, r := range errorRenderings(e) {
			for j := range set.hits(r) {
				t.Errorf("error %d carries member %d (%s) in a rendering (the text is not printed)", i, j, set.group[j])
			}
			if preimageFound(r, hmacKey, hashes) {
				t.Errorf("error %d carries a raw session token whose hash the store received (%s; the text is not printed)", i, gSessionToken)
			}
		}
	}
	for j := range set.hits(logs.String()) {
		t.Errorf("the log carries member %d (%s; the text is not printed)", j, set.group[j])
	}
	if preimageFound(logs.String(), hmacKey, hashes) {
		t.Errorf("the log carries a raw session token whose hash the store received (%s)", gSessionToken)
	}

	// ---- POSITIVE CONTROL: every member, put into an error and into a Debug log line
	// through the verbs and encodings of the renderings, is FOUND by the same search;
	// and a synthetic raw session token is found through its hash in each rendering.
	var probe bytes.Buffer
	plog := debugCapture(&probe)
	builders := func(v []byte) []string {
		s := string(v)
		return []string{fmt.Sprintf("<%s>", s), fmt.Sprintf("%q", s), fmt.Sprintf("%x", v), fmt.Sprintf("%X", v),
			fmt.Sprintf("% x", v), fmt.Sprintf("% X", v), fmt.Sprintf("%v", v), fmt.Sprintf("%#v", v),
			base32.StdEncoding.EncodeToString(v), base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(v),
			base64.StdEncoding.EncodeToString(v), base64.RawStdEncoding.EncodeToString(v),
			base64.URLEncoding.EncodeToString(v), base64.RawURLEncoding.EncodeToString(v)}
	}
	found := func(text string, j int) bool {
		for _, r := range errorRenderings(fmt.Errorf("wrapped: %w", errors.New("x "+text+" y"))) {
			if set.hits(r)[j] {
				return true
			}
		}
		return false
	}
	// Each rendering has ITS OWN builder, a literal pairing by name: for every member the
	// rendering's needle must be one the search holds and must occur in the text its
	// builder produced -- an independent construction of that form (the fmt verb, or the
	// encoder's streaming writer), not the rendering's own function. 5th audit, measured:
	// with R3 emptied the test stayed green, because no builder produced a JSON string and
	// every builder's text was found through some OTHER rendering.
	stream := func(mk func(io.Writer) io.WriteCloser, v []byte) string {
		var b bytes.Buffer
		w := mk(&b)
		if _, err := w.Write(v); err != nil {
			t.Fatalf("encoder write: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("encoder close: %v", err)
		}
		return b.String()
	}
	b32 := func(e *base32.Encoding) func(io.Writer) io.WriteCloser {
		return func(w io.Writer) io.WriteCloser { return base32.NewEncoder(e, w) }
	}
	b64 := func(e *base64.Encoding) func(io.Writer) io.WriteCloser {
		return func(w io.Writer) io.WriteCloser { return base64.NewEncoder(e, w) }
	}
	builderOf := []struct {
		rendering string
		build     func(v []byte) string
	}{
		{"R1 raw", func(v []byte) string { return fmt.Sprintf("<%s>", v) }},
		{"R2 %q inside", func(v []byte) string { return fmt.Sprintf("%q", string(v)) }},
		{"R3 JSON string inside", func(v []byte) string {
			var b bytes.Buffer
			if err := json.NewEncoder(&b).Encode(string(v)); err != nil {
				t.Fatalf("json encode: %v", err)
			}
			return b.String()
		}},
		{"R4 %x", func(v []byte) string { return fmt.Sprintf("%x", v) }},
		{"R5 %X", func(v []byte) string { return fmt.Sprintf("%X", v) }},
		{"R6 % x", func(v []byte) string { return fmt.Sprintf("% x", v) }},
		{"R7 % X", func(v []byte) string { return fmt.Sprintf("% X", v) }},
		{"R8 base32, padded", func(v []byte) string { return stream(b32(base32.StdEncoding), v) }},
		{"R9 base32, unpadded", func(v []byte) string { return stream(b32(base32.StdEncoding.WithPadding(base32.NoPadding)), v) }},
		{"R10 base64 std, padded", func(v []byte) string { return stream(b64(base64.StdEncoding), v) }},
		{"R11 base64 std, raw", func(v []byte) string { return stream(b64(base64.RawStdEncoding), v) }},
		{"R12 base64 url, padded", func(v []byte) string { return stream(b64(base64.URLEncoding), v) }},
		{"R13 base64 url, raw", func(v []byte) string { return stream(b64(base64.RawURLEncoding), v) }},
		{"R14 %v of []byte", func(v []byte) string { return fmt.Sprintf("%d", v) }},
		{"R15 %#v of []byte", func(v []byte) string { return fmt.Sprintf("%#v", v) }},
	}
	if len(builderOf) != len(theRenderings) {
		t.Fatalf("POSITIVE CONTROL: %d builder(s) for %d rendering(s)", len(builderOf), len(theRenderings))
	}
	for i, r := range theRenderings {
		if builderOf[i].rendering != r.name {
			t.Fatalf("POSITIVE CONTROL: builder %d is for %q, the rendering is %q", i, builderOf[i].rendering, r.name)
		}
		for j, v := range set.members {
			n := r.fn(v)
			if _, held := set.needles[n]; len(n) < minNeedle || !held {
				t.Errorf("POSITIVE CONTROL: %s gives member %d (%s) no searched needle", r.name, j, set.group[j])
				continue
			}
			if !strings.Contains(builderOf[i].build(v), n) {
				t.Errorf("POSITIVE CONTROL: %s's needle for member %d (%s) is not in its builder's text", r.name, j, set.group[j])
			}
		}
	}
	for j, v := range set.members {
		for k, text := range builders(v) {
			if !found(text, j) {
				t.Errorf("POSITIVE CONTROL: member %d (%s) in builder %d was not found in an error", j, set.group[j], k)
			}
		}
		probe.Reset()
		plog.Debug("probe", "s", string(v), "b", v)
		if !set.hits(probe.String())[j] {
			t.Errorf("POSITIVE CONTROL: member %d (%s) in a Debug log line was not found", j, set.group[j])
		}
	}
	synthetic := strings.Repeat("S", sessionTokenLen-6) + "yNth3t"
	sm := hmac.New(sha256.New, hmacKey)
	_, _ = sm.Write([]byte(synthetic))
	synHashes := map[string]bool{hex.EncodeToString(sm.Sum(nil)): true}
	for k, text := range builders([]byte(synthetic)) {
		if !preimageFound("x "+text+" y", hmacKey, synHashes) {
			t.Errorf("POSITIVE CONTROL: a raw session token in builder %d was not found through its hash", k)
		}
	}
	probe.Reset()
	plog.Debug("probe", "s", synthetic, "b", []byte(synthetic))
	if !preimageFound(probe.String(), hmacKey, synHashes) {
		t.Error("POSITIVE CONTROL: a raw session token in a Debug log line was not found through its hash")
	}

	a.AllowRequest(addr("192.0.2.9"))
	checkBudgetKeys(t, a, usedAddrs, usedIDs)
}

// firstFactorCapMsg is the line recordPasswordOK logs when one operator's 'password_ok'
// cap is crossed (flow.go, OP-14 phase C). Until then the password step's success logged
// an Info line per right password (OP-6 12c); the durable row replaced it.
const firstFactorCapMsg = "operator first-factor audit rows suppressed for this operator for the rest of the window"

// firstFactorRefusedMsg is the line recordPasswordOK logs, once per window per operator,
// when it refuses the password step fail-closed (OP-14 phase C, 3rd round, F4).
const firstFactorRefusedMsg = "operator first-factor audit row not written; this operator's sign-in step was refused"

// checkFirstFactorLines pins the two lines: every line logged is one of them, in the text
// handler's shape or the JSON handler's, with its time, its level (WARN), its message and
// FOUR attributes -- the kind, the operator's id, the cap and the window -- and the ids of
// each are exactly the ids given, once per handler. So a line deleted is red, a line per
// request is red, and so is any attribute added (an address, an email, the error's text).
// A line is never printed: one that is not one of these can carry anything.
func checkFirstFactorLines(t *testing.T, logText string, capIDs, refusedIDs []string) {
	t.Helper()
	text := func(msg string) *regexp.Regexp {
		return regexp.MustCompile(`^time=\S+ level=WARN msg="` + regexp.QuoteMeta(msg) +
			`" kind=password_ok operator_id=(\S+) cap=10 window=10m0s$`)
	}
	lines := map[string]*regexp.Regexp{firstFactorCapMsg: text(firstFactorCapMsg), firstFactorRefusedMsg: text(firstFactorRefusedMsg)}
	fromText, fromJSON := map[string][]string{}, map[string][]string{}
	for _, l := range strings.Split(logText, "\n") {
		switch {
		case l == "":
		case strings.HasPrefix(l, "{"):
			var m map[string]any
			if err := json.Unmarshal([]byte(l), &m); err != nil {
				t.Errorf("a JSON log line that does not parse (%d characters)", len(l))
				continue
			}
			id, _ := m["operator_id"].(string)
			msg, _ := m["msg"].(string)
			if _, known := lines[msg]; !known || len(m) != 7 || m["level"] != "WARN" || m["kind"] != "password_ok" ||
				m["cap"] != float64(10) || m["window"] != "10m0s" || id == "" {
				t.Errorf("a JSON log line that is not one of the first-factor lines (%d attributes)", len(m))
				continue
			}
			fromJSON[msg] = append(fromJSON[msg], id)
		default:
			matched := false
			for msg, re := range lines {
				if mm := re.FindStringSubmatch(l); mm != nil {
					fromText[msg] = append(fromText[msg], mm[1])
					matched = true
				}
			}
			if !matched {
				t.Errorf("a text log line that is not one of the first-factor lines (%d characters)", len(l))
			}
		}
	}
	for msg, ids := range map[string][]string{firstFactorCapMsg: capIDs, firstFactorRefusedMsg: refusedIDs} {
		want := append([]string(nil), ids...)
		sort.Strings(want)
		sort.Strings(fromText[msg])
		sort.Strings(fromJSON[msg])
		if strings.Join(fromText[msg], ",") != strings.Join(want, ",") || strings.Join(fromJSON[msg], ",") != strings.Join(want, ",") {
			t.Errorf("the line %q: %d text and %d JSON line(s), want %d of each, with those operators' ids", msg, len(fromText[msg]), len(fromJSON[msg]), len(want))
		}
	}
}

// checkBudgetKeys reads, after every arm above and the two success paths have run, the
// keys of each of the Authenticator's budget maps -- by reflection, read-only, as an
// external test can -- and pins them by CONTENT: flood's and work's are among the client
// addresses the drive handed the Authenticator, account's among its operators' ids,
// auditCap's and enroll's the empty key alone. (12th round: the keys were pinned by
// SHAPE -- parses as an address, as a uuid -- and the 12th auditor kept five charges
// green: a raw session token on Verify's success path, a charge on Logout's, "::1%" and
// an email (an IPv6 zone parses), 32 hex characters of a challenge and a TOTP code in a
// uuid's shape (both parse as one).) A key is never printed: it can be a credential.
func checkBudgetKeys(t *testing.T, a *operatorauth.Authenticator, addrs, ids map[string]bool) {
	t.Helper()
	empty := map[string]bool{"": true}
	keyedBy := []struct {
		budget string
		among  map[string]bool
		what   string
	}{
		{"flood", addrs, "a client address the drive used"}, {"work", addrs, "a client address the drive used"},
		{"account", ids, "an operator id of the drive"},
		{"auditCap", empty, "the empty key"}, {"enroll", empty, "the empty key"},
		{"enrollAddr", addrs, "a client address the drive used"},
		{"firstFactor", ids, "an operator id of the drive"},
	}
	limits := reflect.ValueOf(a).Elem().FieldByName("limits")
	if !limits.IsValid() || limits.IsNil() {
		t.Fatal("the Authenticator has no limits field to read; checkBudgetKeys moved")
	}
	keys := 0
	for _, kb := range keyedBy {
		b := limits.Elem().FieldByName(kb.budget)
		if !b.IsValid() || b.IsNil() {
			t.Fatalf("limits has no budget %s; checkBudgetKeys moved", kb.budget)
		}
		w := b.Elem().FieldByName("windows")
		if !w.IsValid() || w.Kind() != reflect.Map || w.Type().Key().Kind() != reflect.String {
			t.Fatalf("budget %s has no windows map keyed by a string; checkBudgetKeys moved", kb.budget)
		}
		if w.Len() == 0 {
			t.Errorf("PREMISE: the %s budget was never charged by the drive, so its keys say nothing", kb.budget)
		}
		for _, k := range w.MapKeys() {
			keys++
			if !kb.among[k.String()] {
				t.Errorf("the %s budget holds a key that is not %s (%d characters; not printed: a key can be a credential)", kb.budget, kb.what, len(k.String()))
			}
		}
	}
	t.Logf("%d budget keys read", keys)
}

// challengeKeyLabel is the challenge MAC key's derivation label, restated from
// challenge.go so the external test can compute the key (G13); units_test.go's
// TestChallenge_KeyIsDerivedNotTheSessionKey pins that the package derives with
// exactly this text.
const challengeKeyLabel = "taptime/operator/login-challenge/v1/key-derivation"

// pendingKeyLabel is the pending blob's key derivation label, restated from
// enrollment.go so the external test can compute the key (G11, and the Authenticator
// specimen's secrets); flow_db_test.go's TestPendingBlob_IsNotTheStoredEnvelope pins
// that the package derives with exactly this text.
const pendingKeyLabel = "taptime/operator/enrollment-pending/v1/key-derivation"
