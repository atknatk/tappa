package appdevice_test

// leak_external_test.go -- §7's never-logged values (device signature, request nonce) from
// OUTSIDE the package, where a handler sits. internal/session/leak_external_test.go is the
// precedent and the reason: redacting methods alone did not stop fmt from printing a value held
// in a caller's UNEXPORTED field (an audit measured it); the pointer field does. Both shapes are
// rendered here, with a positive control so an empty rendering cannot pass.

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/appdevice"
)

// A FAKE, searchable nonce and signature: well-shaped, never issued, never a real signature.
// The body is a marker too -- the request must not retain it.
const (
	fakeNonce = "FAKEfakeFAKEfakeFAKEfakeFAKEfakeFAKEfake120"
	fakeBody  = `{"marker":"BODYMARKERbodymarker"}`
)

var fakeSigBytes = bytes.Repeat([]byte{0xAB, 0xCD, 0xEF}, 24) // 72 bytes, within the DER bound

type handlerState struct {
	employee string
	req      appdevice.SignedRequest
	nonce    appdevice.Nonce
}

type exportedState struct {
	Employee string
	Req      appdevice.SignedRequest
	Nonce    appdevice.Nonce
}

func externalRequest(t *testing.T) (appdevice.SignedRequest, appdevice.Nonce, string) {
	t.Helper()
	sig := base64.RawURLEncoding.EncodeToString(fakeSigBytes)
	r := httptest.NewRequest(http.MethodPost, "/app/v1/tap", strings.NewReader(fakeBody))
	r.Header.Set(appdevice.HeaderDeviceID, uuid.New().String())
	r.Header.Set(appdevice.HeaderNonce, fakeNonce)
	r.Header.Set(appdevice.HeaderSignature, sig)
	req, err := appdevice.FromHTTP(r, []byte(fakeBody))
	if err != nil {
		t.Fatalf("FromHTTP: %v", err)
	}
	n, err := appdevice.ParseNonce(fakeNonce)
	if err != nil {
		t.Fatalf("ParseNonce: %v", err)
	}
	return req, n, sig
}

// TestSignedRequest_NoLeakOnAnyRenderingPath: fmt (every verb, exported and unexported field,
// %+v and %#v), log/slog (text and JSON, attribute and nested), encoding/json and error wrapping
// -- none carries the nonce, the signature (base64url or hex) or the body.
func TestSignedRequest_NoLeakOnAnyRenderingPath(t *testing.T) {
	req, n, sig := externalRequest(t)
	if n.Wire() != fakeNonce {
		t.Fatal("positive control: Wire() is the nonce's one egress and must return it")
	}
	rawNonce, err := base64.RawURLEncoding.DecodeString(fakeNonce)
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{
		"nonce":         fakeNonce,
		"signature":     sig,
		"signature hex": hex.EncodeToString(fakeSigBytes),
		"body":          "BODYMARKERbodymarker",
		// The RAW bytes too, as reflection would print them: fmt renders a byte array or slice
		// reached by reflection as hex (%x) or as a decimal list (%v). Without these two a
		// value-field Nonce leaked through %+v and this test stayed green (measured).
		"nonce bytes hex":     hex.EncodeToString(rawNonce),
		"nonce bytes decimal": strings.Trim(fmt.Sprint(rawNonce), "[]"),
		"signature decimal":   strings.Trim(fmt.Sprint(fakeSigBytes), "[]"),
		"nonce text decimal":  strings.Trim(fmt.Sprint([]byte(fakeNonce)), "[]"),
		"nonce text hex":      hex.EncodeToString([]byte(fakeNonce)),
	}
	un := handlerState{employee: "maria", req: req, nonce: n}
	ex := exportedState{Employee: "maria", Req: req, Nonce: n}

	var out []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
		out = append(out, fmt.Sprintf(verb, req), fmt.Sprintf(verb, n), fmt.Sprintf(verb, un), fmt.Sprintf(verb, ex),
			fmt.Sprintf(verb, &un), fmt.Sprintf(verb, []appdevice.SignedRequest{req}))
	}
	var text, js bytes.Buffer
	for _, h := range []slog.Handler{slog.NewTextHandler(&text, nil), slog.NewJSONHandler(&js, nil)} {
		l := slog.New(h)
		l.Info("app request", "req", req, "nonce", n, "state", un, "exported", ex)
		l.Info("grouped", slog.Group("g", slog.Any("req", req), slog.Any("nonce", n)))
	}
	out = append(out, text.String(), js.String())
	if b, err := json.Marshal(ex); err != nil {
		t.Fatalf("json.Marshal: %v", err)
	} else {
		out = append(out, string(b))
	}
	out = append(out, fmt.Errorf("handler: %v / %+v", req, un).Error())

	for _, s := range out {
		if s == "" {
			t.Fatal("an empty rendering would pass vacuously")
		}
		for what, v := range secrets {
			if strings.Contains(s, v) {
				t.Fatalf("a rendering carries the %s", what)
			}
		}
	}
	if !strings.Contains(text.String(), "maria") || !strings.Contains(js.String(), "maria") {
		t.Fatal("positive control: the logged struct's other field must still render")
	}
}

// TestRefusals_CarryNoRequestMaterial: no refusal text names a part of the request -- the
// shape errors included (they are decided from the request, the closest an error gets to it).
func TestRefusals_CarryNoRequestMaterial(t *testing.T) {
	_, _, sig := externalRequest(t)
	var errs []error
	_, err := appdevice.NewSignedRequest("POST", "/app/v1/tap?q="+fakeNonce, []byte(fakeBody), uuid.New().String(), fakeNonce, sig)
	errs = append(errs, err)
	_, err = appdevice.NewSignedRequest("POST", "/app/v1/tap", []byte(fakeBody), "not-a-device", fakeNonce, sig)
	errs = append(errs, err)
	_, err = appdevice.NewSignedRequest("POST", "/app/v1/tap", []byte(fakeBody), uuid.New().String(), fakeNonce+"x", sig)
	errs = append(errs, err)
	_, err = appdevice.NewSignedRequest("POST", "/app/v1/tap", []byte(fakeBody), uuid.New().String(), fakeNonce, sig+"!")
	errs = append(errs, err)
	_, err = appdevice.ParsePublicKey(fakeSigBytes[:65])
	errs = append(errs, err)
	errs = append(errs, appdevice.ErrUnknownDevice, appdevice.ErrRevokedDevice, appdevice.ErrBadSignature, appdevice.ErrNonceRejected)
	for i, e := range errs {
		if e == nil {
			t.Fatalf("case %d did not refuse", i)
		}
		s := e.Error() + fmt.Sprintf("%+v", e)
		for _, v := range []string{fakeNonce, sig, "BODYMARKER", hex.EncodeToString(fakeSigBytes[:8])} {
			if strings.Contains(s, v) {
				t.Fatalf("case %d: the refusal carries request material", i)
			}
		}
		var ae *appdevice.AuthError
		if i < 4 && !errors.As(e, &ae) {
			t.Fatalf("case %d: a shape refusal is not an AuthError", i)
		}
	}
}
