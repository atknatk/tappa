package config_test

// operator_test.go -- the platform operator surface's four variables (M10 OP-7):
// all four or none, the two keys' size and separation, and the host's one spelling.
// The leak half (no error repeats a value) is at the end of this file.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// randKey is a fresh, valid 32-byte key, base64 as the loader wants it. Random so no
// two keys of a test collide by accident (setRequired's session and tag keys are
// both all-zero, and otherKey is all-seven).
func randKey(t *testing.T) (b64 string, raw []byte) {
	t.Helper()
	raw = make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw), raw
}

// randText is a random lower-case hex string: a sentinel no source file contains.
func randText(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// operatorDSN is a DSN built at run time around a sentinel password, so no file in
// the repository carries a credential-shaped URL (scripts/secretscan.sh's url-cred).
func operatorDSN(password string) string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword("tappa_operator", password), Host: "127.0.0.1:1", Path: "/tappa"}
	return u.String() + "?sslmode=disable"
}

// setOperator sets all four operator variables to valid, mutually distinct values
// and returns them.
func setOperator(t *testing.T) (dsn, kek, hmacKey, host string) {
	t.Helper()
	dsn = operatorDSN(randText(t, 16))
	kek, _ = randKey(t)
	hmacKey, _ = randKey(t)
	host = "ops.taptime.mt"
	t.Setenv("TAPPA_OPERATOR_DATABASE_URL", dsn)
	t.Setenv("TAPPA_OPERATOR_TOTP_KEK", kek)
	t.Setenv("TAPPA_OPERATOR_TOKEN_HMAC_KEY", hmacKey)
	t.Setenv("TAPPA_OPERATOR_HOST", host)
	return dsn, kek, hmacKey, host
}

// TestOperatorSurfaceVariables_AreTheFourTheCardNames pins the set's MEMBERS, since
// every other test here and cmd/tappa's packaging test iterate over it: a list that
// lost a member would leave that variable unchecked everywhere at once.
func TestOperatorSurfaceVariables_AreTheFourTheCardNames(t *testing.T) {
	got := config.OperatorSurfaceVariables()
	want := []string{"TAPPA_OPERATOR_DATABASE_URL", "TAPPA_OPERATOR_TOTP_KEK", "TAPPA_OPERATOR_TOKEN_HMAC_KEY", "TAPPA_OPERATOR_HOST"}
	if !slices.Equal(got, want) {
		t.Fatalf("OperatorSurfaceVariables() = %v, want %v (m10-platform.md, OP-7: four variables)", got, want)
	}
	// A fresh slice per call: a caller that edits its copy cannot shrink the set.
	got[0] = "changed"
	if config.OperatorSurfaceVariables()[0] != want[0] {
		t.Fatal("OperatorSurfaceVariables returns a shared slice; a caller can change the set every check reads")
	}
}

// TestLoad_OperatorSurfaceIsAllOrNothing is the OP-7 acceptance "kısmi config =
// açılış reddi": every one of the fourteen proper, non-empty subsets of the four
// variables refuses to load and names what is set and what is missing; none loads
// with the surface off; all four load with the surface on.
func TestLoad_OperatorSurfaceIsAllOrNothing(t *testing.T) {
	names := config.OperatorSurfaceVariables()

	t.Run("none: loads, surface off", func(t *testing.T) {
		setRequired(t)
		c, err := config.Load()
		if err != nil {
			t.Fatalf("no operator variable must load: %v", err)
		}
		if c.OperatorSurfaceConfigured() || c.OperatorDatabaseURL != "" || c.OperatorTOTPKEK != nil ||
			c.OperatorTokenHMACKey != nil || c.OperatorHost != "" {
			t.Fatal("with no operator variable set the Config carries operator values; the surface must be off")
		}
	})

	t.Run("all four: loads, surface on", func(t *testing.T) {
		setRequired(t)
		dsn, _, _, host := setOperator(t)
		c, err := config.Load()
		if err != nil {
			t.Fatalf("four valid operator variables must load: %v", err)
		}
		if !c.OperatorSurfaceConfigured() || c.OperatorDatabaseURL != dsn || len(c.OperatorTOTPKEK) != 32 ||
			len(c.OperatorTokenHMACKey) != 32 || c.OperatorHost != host {
			t.Fatal("four valid operator variables did not all reach the Config")
		}
	})

	subsets := 0
	for mask := 1; mask < 1<<len(names)-1; mask++ {
		var set, missing []string
		for i, n := range names {
			if mask&(1<<i) != 0 {
				set = append(set, n)
			} else {
				missing = append(missing, n)
			}
		}
		subsets++
		t.Run("partial/"+strings.Join(set, "+"), func(t *testing.T) {
			setRequired(t)
			setOperator(t)
			for _, n := range missing {
				t.Setenv(n, "")
			}
			_, err := config.Load()
			if err == nil {
				t.Fatalf("set %v without %v loaded; a partial operator configuration must refuse to start", set, missing)
			}
			msg := err.Error()
			for _, n := range missing {
				if !strings.Contains(msg, n) {
					t.Errorf("the refusal does not name the missing %s: %q", n, msg)
				}
			}
			if !strings.Contains(msg, "only some are set") {
				t.Errorf("the refusal is not the partial-set refusal: %q", msg)
			}
		})
	}
	if subsets != 14 {
		t.Fatalf("walked %d partial subsets of four variables, want 14", subsets)
	}
}

// TestLoad_OperatorKeysAreThirtyTwoBytes: both keys go through key32, so a wrong
// encoding or size is a startup failure naming the variable and the size -- the same
// rule TAPPA_TAG_KEK has (ADR 0020: a 32-byte AES-256 KEK and a 32-byte HMAC key).
func TestLoad_OperatorKeysAreThirtyTwoBytes(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	long := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 33))
	for _, name := range []string{"TAPPA_OPERATOR_TOTP_KEK", "TAPPA_OPERATOR_TOKEN_HMAC_KEY"} {
		for _, tc := range []struct{ label, value, want string }{
			{"16 bytes", short, "want 32 bytes, got 16"},
			{"33 bytes", long, "want 32 bytes, got 33"},
			{"not base64", "!!!!", "not valid base64"},
		} {
			t.Run(name+"/"+tc.label, func(t *testing.T) {
				setRequired(t)
				setOperator(t)
				t.Setenv(name, tc.value)
				_, err := config.Load()
				if err == nil {
					t.Fatalf("%s with %s loaded", name, tc.label)
				}
				if !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("the refusal does not say %q about %s: %q", tc.want, name, err.Error())
				}
			})
		}
	}
}

// TestLoad_OperatorKeysDifferFromEveryOtherKey is the OP-7 acceptance "TOTP KEK
// diğer anahtarlarla aynıysa başlangıç reddi", widened as the card's OP-4 block does
// to the token HMAC key as well: each operator key equal to each other key -- the
// other operator key included -- is refused, naming both; all distinct loads.
//
// The OTHER keys are listed here by variable; that the list of keys the rule compares
// covers every key FIELD of Config is TestNamedKeys_ListEveryKeyFieldOfTheConfig's job.
func TestLoad_OperatorKeysDifferFromEveryOtherKey(t *testing.T) {
	setDistinct := func(t *testing.T) {
		t.Helper()
		setRequired(t)
		for _, n := range []string{"TAPPA_SESSION_HMAC_KEY", "TAPPA_TAG_KEK", "TAPPA_TAG_KEK_PREVIOUS", "TAPPA_INVITE_HMAC_KEY"} {
			k, _ := randKey(t)
			t.Setenv(n, k)
		}
		setOperator(t)
	}

	t.Run("control: all distinct loads", func(t *testing.T) {
		setDistinct(t)
		if _, err := config.Load(); err != nil {
			t.Fatalf("six distinct keys must load: %v", err)
		}
	})

	operatorKeys := []string{"TAPPA_OPERATOR_TOTP_KEK", "TAPPA_OPERATOR_TOKEN_HMAC_KEY"}
	others := []string{"TAPPA_SESSION_HMAC_KEY", "TAPPA_TAG_KEK", "TAPPA_TAG_KEK_PREVIOUS", "TAPPA_INVITE_HMAC_KEY"}
	for _, op := range operatorKeys {
		for _, other := range append(slices.Clone(others), operatorKeys...) {
			if other == op {
				continue
			}
			t.Run(op+"="+other, func(t *testing.T) {
				setDistinct(t)
				same, _ := randKey(t)
				t.Setenv(op, same)
				t.Setenv(other, same)
				_, err := config.Load()
				if err == nil {
					t.Fatalf("%s equal to %s loaded", op, other)
				}
				msg := err.Error()
				if !strings.Contains(msg, op) || !strings.Contains(msg, other) || !strings.Contains(msg, "must differ") {
					t.Errorf("the refusal does not name the identical pair %s / %s: %q", op, other, msg)
				}
			})
		}
	}

	// The OTHER keys' own pairs are not this rule's: the tag KEK equal to the invite
	// key is not refused by it (widening that would be a new refusal for production
	// configurations nobody has measured) -- the boundary is the rule's text.
	t.Run("boundary: a pair without an operator key is not this rule's", func(t *testing.T) {
		setDistinct(t)
		same, _ := randKey(t)
		t.Setenv("TAPPA_TAG_KEK", same)
		t.Setenv("TAPPA_INVITE_HMAC_KEY", same)
		if _, err := config.Load(); err != nil {
			t.Fatalf("a non-operator pair was refused by a rule about operator keys: %v", err)
		}
	})
}

// TestLoad_OperatorHostIsOneSpelling: TAPPA_OPERATOR_HOST is a lower-case DNS name
// and nothing else, and never the base URL's host (ADR 0020 §4).
func TestLoad_OperatorHostIsOneSpelling(t *testing.T) {
	label63 := strings.Repeat("a", 63)
	// 4 labels of 63 + 3 dots = 255; trimmed to 253 / 254 by the last label.
	name253 := label63 + "." + label63 + "." + label63 + "." + strings.Repeat("a", 61)
	good := []string{"ops.taptime.mt", "localhost", "ops.localhost", "a", "x-1.y2", label63 + ".mt", name253, "10.0.0.1"}
	bad := []string{
		"OPS.taptime.mt", "Ops.Taptime.Mt", "ops.taptime.mt.", "ops.taptime.mt:443", "https://ops.taptime.mt",
		"ops.taptime.mt/", "ops.taptime.mt/operator", "user@ops.taptime.mt", " ops.taptime.mt", "ops.taptime.mt ",
		"-ops.taptime.mt", "ops-.taptime.mt", "ops..taptime.mt", ".ops.taptime.mt", "ops_x.taptime.mt",
		"öps.taptime.mt", "[::1]", "*.taptime.mt", label63 + "a.mt", name253 + "a", "ops.taptime.mt\n",
		operatorDSN("x"),
	}
	for _, h := range good {
		t.Run("good/"+h[:min(len(h), 20)], func(t *testing.T) {
			setRequired(t)
			setOperator(t)
			// The default base URL is http://localhost:8080, whose host is one of the
			// good names; the base-host rule is the next block's subject, not this one's.
			t.Setenv("TAPPA_BASE_URL", "https://example.test")
			t.Setenv("TAPPA_OPERATOR_HOST", h)
			c, err := config.Load()
			if err != nil {
				t.Fatalf("%d-byte host refused: %v", len(h), err)
			}
			if c.OperatorHost != h {
				t.Fatal("the loaded host is not the configured one")
			}
		})
	}
	for i, h := range bad {
		t.Run(fmt.Sprintf("bad/%d", i), func(t *testing.T) {
			setRequired(t)
			setOperator(t)
			t.Setenv("TAPPA_OPERATOR_HOST", h)
			if _, err := config.Load(); err == nil {
				t.Fatalf("bad host #%d (%q) loaded", i, h)
			} else if !strings.Contains(err.Error(), "TAPPA_OPERATOR_HOST") {
				t.Errorf("bad host #%d refused without naming the variable: %q", i, err.Error())
			}
		})
	}

	for _, tc := range []struct{ base, host string }{
		{"https://taptime.mt", "taptime.mt"},
		{"https://TapTime.MT", "taptime.mt"},
		{"http://localhost:8080", "localhost"},
	} {
		t.Run("base host/"+tc.base, func(t *testing.T) {
			setRequired(t)
			setOperator(t)
			t.Setenv("TAPPA_BASE_URL", tc.base)
			t.Setenv("TAPPA_OPERATOR_HOST", tc.host)
			_, err := config.Load()
			if err == nil {
				t.Fatalf("operator host %q equal to the base URL's host loaded", tc.host)
			}
			if !strings.Contains(err.Error(), "TAPPA_BASE_URL") {
				t.Errorf("the refusal does not say why: %q", err.Error())
			}
		})
	}
	// CONTROL: a sub-domain of the base host is a DIFFERENT host and loads.
	t.Run("base host/control", func(t *testing.T) {
		setRequired(t)
		setOperator(t)
		t.Setenv("TAPPA_BASE_URL", "https://taptime.mt")
		t.Setenv("TAPPA_OPERATOR_HOST", "ops.taptime.mt")
		if _, err := config.Load(); err != nil {
			t.Fatalf("ops.taptime.mt beside https://taptime.mt was refused: %v", err)
		}
	})
}

// TestLoad_OperatorVariablesOfBlanksAreRefused: a value of BLANKS is a value
// (loadOperatorSurface's reading of "set"), so in EVERY one of the four variables it is
// refused as that variable's bad value, not taken for a missing variable. The DSN rows
// are 2b: a DSN of whitespace used to load, and pgx read it as an empty connection string
// (the surface booted "unavailable"). (2d: moved out of TestLoad_OperatorHostIsOneSpelling,
// whose name said nothing of the other three.)
func TestLoad_OperatorVariablesOfBlanksAreRefused(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"TAPPA_OPERATOR_HOST", "   ", "lower-case DNS host name"},
		{"TAPPA_OPERATOR_DATABASE_URL", " ", "TAPPA_OPERATOR_DATABASE_URL: is set but holds only whitespace"},
		{"TAPPA_OPERATOR_DATABASE_URL", " \t\n ", "TAPPA_OPERATOR_DATABASE_URL: is set but holds only whitespace"},
		{"TAPPA_OPERATOR_TOTP_KEK", "   ", "TAPPA_OPERATOR_TOTP_KEK: not valid base64"},
		{"TAPPA_OPERATOR_TOKEN_HMAC_KEY", "   ", "TAPPA_OPERATOR_TOKEN_HMAC_KEY: not valid base64"},
	} {
		t.Run(tc.name+"/"+strconv.Quote(tc.value), func(t *testing.T) {
			setRequired(t)
			setOperator(t)
			t.Setenv(tc.name, tc.value)
			_, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "only some are set") {
				t.Fatalf("%s of blanks: %v, want the refusal %q", tc.name, err, tc.want)
			}
		})
	}
}

// TestLoad_OperatorRefusalsRepeatNoValue is the leak half (OP-7 brief: no new error
// text prints a DSN, a password or a key). Every refusal this file can produce from
// the operator variables is rendered as the process would render it -- err.Error(),
// %v / %+v / %q, and through slog's text and JSON handlers at DEBUG, the shapes
// cmd/tappa's main uses -- and searched for the DSN, its password, and each key in
// base64, hex and as raw bytes.
//
// POSITIVE CONTROL: the same search over a rendering that DOES carry each value
// finds it, so a search that had gone blind would fail here rather than pass.
func TestLoad_OperatorRefusalsRepeatNoValue(t *testing.T) {
	type leakCase struct {
		name  string
		apply func(t *testing.T, dsn, kek, hmacKey string)
	}
	cases := []leakCase{
		{"partial: the DSN alone", func(t *testing.T, dsn, kek, hmacKey string) {
			t.Setenv("TAPPA_OPERATOR_TOTP_KEK", "")
			t.Setenv("TAPPA_OPERATOR_TOKEN_HMAC_KEY", "")
			t.Setenv("TAPPA_OPERATOR_HOST", "")
		}},
		{"partial: the keys alone", func(t *testing.T, dsn, kek, hmacKey string) {
			t.Setenv("TAPPA_OPERATOR_DATABASE_URL", "")
			t.Setenv("TAPPA_OPERATOR_HOST", "")
		}},
		{"equal operator keys", func(t *testing.T, dsn, kek, hmacKey string) {
			t.Setenv("TAPPA_OPERATOR_TOKEN_HMAC_KEY", kek)
		}},
		{"operator key equal to the tag KEK", func(t *testing.T, dsn, kek, hmacKey string) {
			t.Setenv("TAPPA_TAG_KEK", kek)
		}},
		{"a key of the wrong size", func(t *testing.T, dsn, kek, hmacKey string) {
			t.Setenv("TAPPA_OPERATOR_TOTP_KEK", kek[:20])
		}},
		{"the DSN pasted into the host", func(t *testing.T, dsn, kek, hmacKey string) {
			t.Setenv("TAPPA_OPERATOR_HOST", dsn)
		}},
		{"a key pasted into the host", func(t *testing.T, dsn, kek, hmacKey string) {
			t.Setenv("TAPPA_OPERATOR_HOST", hmacKey)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			dsn, kek, hmacKey, _ := setOperator(t)
			u, _ := url.Parse(dsn)
			password, _ := u.User.Password()
			tc.apply(t, dsn, kek, hmacKey)
			_, err := config.Load()
			if err == nil {
				t.Fatal("the case did not produce a refusal; it cannot test what a refusal prints")
			}
			secrets := secretForms(t, dsn, password, kek, hmacKey)
			for i, text := range errorRenderings(err) {
				for label, s := range secrets {
					if strings.Contains(text, s) {
						t.Errorf("rendering #%d of the refusal carries %s", i, label)
					}
				}
			}
			// POSITIVE CONTROL, per case: rendering the values themselves through the
			// same paths finds every one of them.
			for label, s := range secrets {
				found := false
				for _, text := range errorRenderings(fmt.Errorf("control %s", s)) {
					found = found || strings.Contains(text, s)
				}
				if !found {
					t.Fatalf("CONTROL FAILED: the search does not find %s in a rendering that carries it", label)
				}
			}
		})
	}
}

// secretForms is what a leaked operator value looks like: the DSN, its password, and
// each key as the base64 the variable holds, as hex, and as the raw decoded bytes.
func secretForms(t *testing.T, dsn, password, kek, hmacKey string) map[string]string {
	t.Helper()
	out := map[string]string{"the DSN": dsn, "the DSN's password": password}
	for label, k := range map[string]string{"the TOTP KEK": kek, "the token HMAC key": hmacKey} {
		raw, err := base64.StdEncoding.DecodeString(k)
		if err != nil {
			t.Fatal(err)
		}
		out[label+" (base64)"] = k
		out[label+" (hex)"] = hex.EncodeToString(raw)
		out[label+" (raw)"] = string(raw)
		// A []byte under %v: the decimal list OP-6 measured leaking.
		out[label+" (decimal list)"] = fmt.Sprint(raw)
		// A 20-character prefix: the wrong-size case pastes a truncated key, and a
		// prefix is the part a log line would show.
		out[label+" (base64 prefix)"] = k[:20]
	}
	return out
}

// errorRenderings are the forms a startup refusal takes: the text, the fmt verbs a
// caller wraps it with, and slog's two handlers at DEBUG -- cmd/tappa's main logs
// the refusal as slog.Error("fatal", "err", err) through one of them.
func errorRenderings(err error) []string {
	out := []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%q", err), fmt.Sprintf("%s", err)}
	for _, mk := range []func(*bytes.Buffer) slog.Handler{
		func(b *bytes.Buffer) slog.Handler {
			return slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug})
		},
		func(b *bytes.Buffer) slog.Handler {
			return slog.NewJSONHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug})
		},
	} {
		var buf bytes.Buffer
		slog.New(mk(&buf)).Log(context.Background(), slog.LevelDebug, "fatal", "err", err)
		out = append(out, buf.String())
	}
	return out
}

// TestOperatorSurfaceConfigured_AnyOneFieldCounts pins the fail-closed reading of a
// Config built WITHOUT Load (a struct literal, wiring that skips Load): any one operator
// field set reports the surface configured, so cmd/tappa goes on to open it and the
// constructors meet the missing pieces -- instead of the surface staying off without a
// word. None set is off. (OP-7 2nd round, B5c: `||` -> `&&` stayed green before this.)
func TestOperatorSurfaceConfigured_AnyOneFieldCounts(t *testing.T) {
	if (&config.Config{}).OperatorSurfaceConfigured() {
		t.Fatal("an empty Config reports the operator surface configured")
	}
	for name, c := range map[string]*config.Config{
		"only the DSN":       {OperatorDatabaseURL: "postgres://x@127.0.0.1:1/x"},
		"only the TOTP KEK":  {OperatorTOTPKEK: make([]byte, 32)},
		"only the token key": {OperatorTokenHMACKey: make([]byte, 32)},
		"only the host":      {OperatorHost: "ops.taptime.mt"},
	} {
		if !c.OperatorSurfaceConfigured() {
			t.Errorf("%s: reported unconfigured; the surface would stay off without a word", name)
		}
	}
}
