package handler

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// TestAppErrors_TheVocabularyIsClosedAndEchoesNothing: every /app/v1 error code is one of a fixed,
// distinct set of lower-case words, and its body is the constant {"error":"<code>"} -- built from
// the code alone, so no request value can reach it.
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
		writeAppError(w, http.StatusBadRequest, c)
		if got, want := w.Body.String(), `{"error":"`+string(c)+`"}`+"\n"; got != want {
			t.Errorf("body for %q = %q, want %q", c, got, want)
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("headers for %q: %v", c, w.Header())
		}
	}
	if len(seen) != 14 {
		t.Errorf("%d codes; the vocabulary changed -- update this count with the card", len(seen))
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
