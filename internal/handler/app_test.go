package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/httpx"
)

// TestAppErrors_TheVocabularyIsClosedAndEchoesNothing: every /app/v1 error code is one of a fixed,
// distinct set of lower-case words with ONE status, and its body is the constant {"error":"<code>"}
// -- built from the code alone, so no request value can reach it.
func TestAppErrors_TheVocabularyIsClosedAndEchoesNothing(t *testing.T) {
	word := regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)
	seen := map[appError]bool{}
	for _, c := range appErrors {
		if !word.MatchString(string(c)) {
			t.Errorf("code %q is not a lower-case word", c)
		}
		if seen[c] {
			t.Errorf("code %q is listed twice", c)
		}
		seen[c] = true
		w := httptest.NewRecorder()
		writeAppError(w, c)
		if got, want := w.Body.String(), `{"error":"`+string(c)+`"}`+"\n"; got != want {
			t.Errorf("body for %q = %q, want %q", c, got, want)
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("headers for %q: %v", c, w.Header())
		}
	}
	if len(seen) != 16 || len(appErrorStatus) != len(seen) {
		t.Errorf("%d codes, %d statuses; the vocabulary changed -- update this count and the card's contract", len(seen), len(appErrorStatus))
	}
}

// TestAppContract_ErrorStatuses pins the wire contract's code -> status table (card M16-07, "Tel
// sözleşmesi"): the apps branch on these pairs.
func TestAppContract_ErrorStatuses(t *testing.T) {
	want := map[string]int{
		"bad_request": 400, "rate_limited": 429, "server_error": 500, "invalid_invite": 400,
		"consent_required": 400, "invalid_key": 400, "new_key_required": 409, "activation_not_ready": 409,
		"activation_needs_touch": 400, "activation_tap_failed": 400, "stale_context": 400,
		"unknown_plaque": 404, "foreign_tenant": 403, "try_again": 409, "not_found": 404,
		"method_not_allowed": 405,
	}
	if len(want) != len(appErrorStatus) {
		t.Fatalf("%d statuses in the code, %d in the contract", len(appErrorStatus), len(want))
	}
	for code, status := range want {
		w := httptest.NewRecorder()
		writeAppError(w, appError(code))
		if w.Code != status {
			t.Errorf("%s = %d, the contract says %d", code, w.Code, status)
		}
	}
}

// TestAppContract_ResponseShapes pins every /app/v1 response body's field names and kinds (the
// apps decode exactly these). A renamed json tag is red here before it reaches a phone.
func TestAppContract_ResponseShapes(t *testing.T) {
	logo := &appLogo{SHA256: "ab", Width: 1, Height: 2}
	brand := appBrand{Name: "N", Logo: logo, Accent: "1F5C41"}
	at := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		v    any
		want string
	}{
		"activate/lookup": {appLookupResponse{EmployeeName: "E", BusinessName: "B", LocationName: "L", WiFiSSID: "W", SecondDevice: true, Brand: brand},
			`{"employeeName":"E","businessName":"B","locationName":"L","wifiSsid":"W","secondDevice":true,"brand":{"name":"N","logo":{"sha256":"ab","width":1,"height":2},"accent":"1F5C41"}}`},
		"activate/bind":     {appBindResponse{ActivationID: "I", Challenge: "C"}, `{"activationId":"I","challenge":"C"}`},
		"activate/complete": {appCompleteResponse{DeviceID: "D", EmployeeName: "E"}, `{"deviceId":"D","employeeName":"E"}`},
		"nonce":             {appNonceResponse{Nonce: "N", ExpiresAt: at}, `{"nonce":"N","expiresAt":"2026-11-14T12:00:00Z"}`},
		"tap/start": {appTapStartResponse{EmployeeName: "E", LocationName: "L", Brand: appBrand{}, Direction: "in", QR: false, Context: "X"},
			`{"employeeName":"E","locationName":"L","brand":{},"direction":"in","qr":false,"context":"X"}`},
		"tap": {appTapResponse{Outcome: "recorded", Verdict: "ok", Direction: "out", Venue: "V", At: "09:00", BusinessType: "bar"},
			`{"outcome":"recorded","verdict":"ok","direction":"out","venue":"V","at":"09:00","businessType":"bar"}`},
		"me": {appMeResponse{EmployeeName: "E", Brand: appBrand{}, Direction: ""}, `{"employeeName":"E","brand":{},"direction":""}`},
	} {
		got, err := json.Marshal(tc.v)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Errorf("%s:\n got  %s\n want %s", name, got, tc.want)
		}
	}
}

// TestAppContract_RequestShapes pins every request body's field names: the documented body decodes
// into the expected values, and one misnamed field is refused (strict decoding -- the versioning
// rule: the server ships a new field before any app sends it).
func TestAppContract_RequestShapes(t *testing.T) {
	var lookup appLookupRequest
	var bind appBindRequest
	var complete appCompleteRequest
	var start appTapStartRequest
	var tapIn appTapRequest
	for name, tc := range map[string]struct {
		body, misnamed string
		into           any
		check          func() bool
	}{
		"activate/lookup": {`{"code":"c"}`, `{"inviteCode":"c"}`, &lookup, func() bool { return lookup.Code == "c" }},
		"activate/bind": {`{"code":"c","consent":true,"publicKey":"k"}`, `{"code":"c","consent":true,"public_key":"k"}`, &bind,
			func() bool { return bind.Code == "c" && bind.Consent && bind.PublicKey == "k" }},
		"activate/complete": {`{"code":"c","publicKey":"k","platform":"ios","sun":"tag=1"}`, `{"code":"c","publicKey":"k","platform":"ios","sun":"tag=1","label":"x"}`, &complete,
			func() bool {
				return complete.Code == "c" && complete.PublicKey == "k" && complete.Platform == "ios" && complete.SUN == "tag=1"
			}},
		"tap/start": {`{"sun":"tag=1"}`, `{"url":"tag=1"}`, &start, func() bool { return start.SUN == "tag=1" }},
		"tap": {`{"context":"x","want":"in","lat":1.5,"lng":2.5,"mockLocation":true,"occurredAt":"2026-11-14T12:00:00Z"}`, `{"context":"x","mock":true}`, &tapIn,
			func() bool {
				return tapIn.Context == "x" && tapIn.Want == "in" && *tapIn.Lat == 1.5 && *tapIn.Lng == 2.5 && *tapIn.MockLocation && tapIn.OccurredAt != ""
			}},
	} {
		if !decodeAppJSON([]byte(tc.body), tc.into) || !tc.check() {
			t.Errorf("%s: the documented body did not decode into its fields", name)
		}
		if decodeAppJSON([]byte(tc.misnamed), tc.into) {
			t.Errorf("%s: a misnamed field was accepted", name)
		}
	}
	if _, ok := appPlatforms["ios"]; !ok || len(appPlatforms) != 2 {
		t.Errorf("platforms = %v, the contract says ios | android", appPlatforms)
	}
	if _, ok := appPlatforms["android"]; !ok {
		t.Error("android is not a platform")
	}
}

// TestDecodeAppJSON_IsStrict: one object, no unknown field, nothing after it; empty is {}.
func TestDecodeAppJSON_IsStrict(t *testing.T) {
	var v struct {
		A string `json:"a"`
	}
	for body, want := range map[string]bool{
		``:                 true,
		`{}`:               true,
		`{"a":"x"}`:        true,
		`{"a":"x","b":1}`:  false,
		`{"a":"x"} {}`:     false,
		`{"a":"x"}garbage`: false,
		`[]`:               false,
	} {
		if got := decodeAppJSON([]byte(body), &v); got != want {
			t.Errorf("decodeAppJSON(%q) = %v, want %v", body, got, want)
		}
	}
}

// TestApp_UnknownPathsAndMethodsAnswerTheVocabulary: under /app/v1 an unknown path is 404
// not_found and a known path with the wrong method is 405 method_not_allowed, both JSON and
// no-store -- not the site's HTML page or chi's plain text (M16-07 review). Mounted on a bare
// router: the handlers are never reached, so no dependency is needed beyond the limiter.
func TestApp_UnknownPathsAndMethodsAnswerTheVocabulary(t *testing.T) {
	a := &App{}
	a.limiter = httpx.NewTapLimiter(httpx.TapLimitParams{Refused: writeAppTooMany})
	r := chi.NewRouter()
	a.Mount(r)
	srv := httptest.NewServer(r)
	defer srv.Close()
	for _, tc := range []struct {
		method, path string
		status       int
		body         string
	}{
		{http.MethodGet, appPrefix + "/nope", 404, `{"error":"not_found"}` + "\n"},
		{http.MethodPost, AppMePath, 405, `{"error":"method_not_allowed"}` + "\n"},
		{http.MethodGet, AppTapPath, 405, `{"error":"method_not_allowed"}` + "\n"},
		{http.MethodGet, AppMePath + "/", 404, `{"error":"not_found"}` + "\n"},
	} {
		req, err := http.NewRequest(tc.method, srv.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var buf [256]byte
		n, _ := res.Body.Read(buf[:])
		res.Body.Close()
		if res.StatusCode != tc.status || string(buf[:n]) != tc.body || res.Header.Get("Cache-Control") != "no-store" ||
			res.Header.Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("%s %s = %d %q %v, want %d %q JSON no-store", tc.method, tc.path, res.StatusCode, buf[:n], res.Header, tc.status, tc.body)
		}
		// The clock-skew contract (cards M16-12, M16-16): every answer carries the server's Date.
		if _, err := http.ParseTime(res.Header.Get("Date")); err != nil {
			t.Errorf("%s %s carries no parseable Date header: %q", tc.method, tc.path, res.Header.Get("Date"))
		}
	}
}
