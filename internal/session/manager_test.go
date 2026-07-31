package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// manager_test.go -- the parts of the lifecycle that need no database: the
// SINGLE-QUERY guarantee of Verify, the mapping of every failure onto the right
// sentinel, and constructor validation. The real-Postgres round trip, the RLS
// isolation proof and the "no raw value in the row" proof are in db_test.go.

// testConfig builds a Config with a fake key. Never reads the environment.
func testConfig(env, baseURL string) *config.Config {
	return &config.Config{Env: env, BaseURL: baseURL, SessionHMACKey: fakeHMACKey}
}

// countingDB is a fake Database that records how each method was called. It
// exists to PROVE the card's "verification is a single query" criterion: a fake
// can count calls, a live database cannot be asked afterwards how many it saw.
type countingDB struct {
	resolveCalls  int
	withTenant    int
	lastHash      string
	resolveResult db.ResolvedSession
	resolveErr    error
}

func (c *countingDB) WithTenant(_ context.Context, _ uuid.UUID, _ db.TxFunc) error {
	c.withTenant++
	return errors.New("countingDB: WithTenant must not run on the verification path")
}

func (c *countingDB) GetEmployeeBySessionHash(_ context.Context, tokenHash string) (db.ResolvedSession, error) {
	c.resolveCalls++
	c.lastHash = tokenHash
	return c.resolveResult, c.resolveErr
}

func newManager(t *testing.T, data Database) *Manager {
	t.Helper()
	m, err := New(data, testConfig("dev", "http://localhost:8080"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// TestNew_RejectsBadWiring: a short or absent HMAC key must fail loudly at
// construction. A zero key still produces plausible hex, so a silent default
// would stay invisible until sessions turned out to be forgeable.
func TestNew_RejectsBadWiring(t *testing.T) {
	if _, err := New(nil, testConfig("dev", "")); err == nil {
		t.Error("New accepted a nil database")
	}
	if _, err := New(&countingDB{}, nil); err == nil {
		t.Error("New accepted a nil config")
	}
	for _, n := range []int{0, 1, 16, 31, 33, 64} {
		cfg := testConfig("dev", "")
		cfg.SessionHMACKey = make([]byte, n)
		if _, err := New(&countingDB{}, cfg); err == nil {
			t.Errorf("New accepted a %d-byte key", n)
		}
	}
}

// TestNew_CopiesKey: mutating the caller's slice after construction must not
// change how tokens hash.
func TestNew_CopiesKey(t *testing.T) {
	cfg := testConfig("dev", "")
	cfg.SessionHMACKey = append([]byte(nil), fakeHMACKey...)
	m, err := New(&countingDB{}, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tk, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	before, err := tk.hash(m.hmacKey)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	for i := range cfg.SessionHMACKey {
		cfg.SessionHMACKey[i] ^= 0xFF
	}
	after, err := tk.hash(m.hmacKey)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if before != after {
		t.Fatal("the manager aliased the caller's key slice")
	}
}

// TestVerify_IsASingleQuery is the card's "doğrulama tek sorgu" criterion,
// measured rather than asserted: exactly one resolver call, zero tenant-scoped
// transactions, for the live, the revoked and the unknown outcome alike.
func TestVerify_IsASingleQuery(t *testing.T) {
	tk, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	revoked := time.Now().UTC()

	tests := []struct {
		name    string
		result  db.ResolvedSession
		err     error
		wantErr error
	}{
		{
			name:   "live",
			result: db.ResolvedSession{ID: uuid.New(), TenantID: uuid.New(), EmployeeID: uuid.New()},
		},
		{
			name:    "revoked",
			result:  db.ResolvedSession{ID: uuid.New(), TenantID: uuid.New(), EmployeeID: uuid.New(), RevokedAt: &revoked},
			wantErr: ErrRevoked,
		},
		{
			name:    "unknown",
			err:     pgx.ErrNoRows,
			wantErr: ErrNoSession,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &countingDB{resolveResult: tc.result, resolveErr: tc.err}
			m := newManager(t, fake)

			got, err := m.Verify(context.Background(), tk)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("Verify error = %v, want %v", err, tc.wantErr)
			}
			if fake.resolveCalls != 1 {
				t.Fatalf("resolver called %d times, want exactly 1", fake.resolveCalls)
			}
			if fake.withTenant != 0 {
				t.Fatalf("verification opened %d tenant transactions, want 0", fake.withTenant)
			}
			// The value handed to the database is the HMAC, never the cookie.
			if fake.lastHash == tk.reveal() {
				t.Fatal("the raw cookie value was sent to the database")
			}
			if strings.Contains(fake.lastHash, tk.reveal()) {
				t.Fatal("the value sent to the database contains the raw cookie value")
			}
			want, err := tk.hash(fakeHMACKey)
			if err != nil {
				t.Fatalf("hash: %v", err)
			}
			if fake.lastHash != want {
				t.Fatal("the database was queried with something other than the keyed hash")
			}
			// §4.6 / §5 row 4: a REVOKED session must still hand back its facts
			// so the caller can write a record instead of losing the attempt.
			if tc.wantErr == ErrRevoked {
				if got.EmployeeID != tc.result.EmployeeID || got.TenantID != tc.result.TenantID || got.ID != tc.result.ID {
					t.Fatalf("revoked session returned %+v, want the resolved facts", got)
				}
				if got.RevokedAt == nil {
					t.Fatal("revoked session returned a nil RevokedAt")
				}
			}
			if tc.wantErr == ErrNoSession && got != (Resolved{}) {
				t.Fatalf("unknown session returned %+v, want the zero value", got)
			}
		})
	}
}

// TestVerify_MalformedSkipsTheDatabase: a junk cookie is rejected by the shape
// gate, so it never becomes a query at all.
func TestVerify_MalformedSkipsTheDatabase(t *testing.T) {
	fake := &countingDB{}
	m := newManager(t, fake)

	for _, val := range []string{"", "short", strings.Repeat("A", 1<<16), strings.Repeat("*", tokenLen)} {
		got, err := m.Verify(context.Background(), wrap(val))
		if !errors.Is(err, ErrNoSession) {
			t.Fatalf("Verify(malformed) error = %v, want ErrNoSession", err)
		}
		if got != (Resolved{}) {
			t.Fatalf("Verify(malformed) returned %+v, want the zero value", got)
		}
	}
	if fake.resolveCalls != 0 {
		t.Fatalf("malformed values produced %d queries, want 0", fake.resolveCalls)
	}
}

// TestVerify_DatabaseFailureIsNotNoSession: an outage must not be reported as
// "no session", which would silently redirect every tap to activation and write
// no records at all (§4.6).
func TestVerify_DatabaseFailureIsNotNoSession(t *testing.T) {
	boom := errors.New("connection refused")
	m := newManager(t, &countingDB{resolveErr: boom})
	tk, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	_, err = m.Verify(context.Background(), tk)
	if err == nil {
		t.Fatal("Verify hid a database failure")
	}
	if errors.Is(err, ErrNoSession) || errors.Is(err, ErrRevoked) {
		t.Fatalf("database failure was classified as %v", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("Verify did not wrap the underlying error: %v", err)
	}
	if strings.Contains(err.Error(), tk.reveal()) {
		t.Fatalf("the error leaks the raw value: %q", err)
	}
}

// TestManager_RejectsNilIdentifiers: every write path refuses the nil UUID
// rather than scoping a query to a non-existent tenant (the WithTenant rule,
// one layer up).
func TestManager_RejectsNilIdentifiers(t *testing.T) {
	fake := &countingDB{}
	m := newManager(t, fake)
	ctx := context.Background()
	id := uuid.New()

	if _, err := m.Issue(ctx, IssueParams{EmployeeID: id}); err == nil {
		t.Error("Issue accepted a nil tenant")
	}
	if _, err := m.Issue(ctx, IssueParams{TenantID: id}); err == nil {
		t.Error("Issue accepted a nil employee")
	}
	if err := m.Touch(ctx, Resolved{ID: id}); err == nil {
		t.Error("Touch accepted a nil tenant")
	}
	if err := m.Touch(ctx, Resolved{TenantID: id}); err == nil {
		t.Error("Touch accepted a nil session id")
	}
	if err := m.Revoke(ctx, uuid.Nil, id); err == nil {
		t.Error("Revoke accepted a nil tenant")
	}
	if err := m.Revoke(ctx, id, uuid.Nil); err == nil {
		t.Error("Revoke accepted a nil session id")
	}
	if _, err := m.RevokeAllForEmployee(ctx, uuid.Nil, id); err == nil {
		t.Error("RevokeAllForEmployee accepted a nil tenant")
	}
	if _, err := m.ListForEmployee(ctx, id, uuid.Nil); err == nil {
		t.Error("ListForEmployee accepted a nil employee")
	}
	if fake.withTenant != 0 {
		t.Fatalf("a rejected call still opened %d transactions", fake.withTenant)
	}
}

// TestSessionLive is the tiny predicate the callers branch on.
func TestSessionLive(t *testing.T) {
	now := time.Now()
	if !(Session{}).Live() {
		t.Error("a session with no revoked_at is not Live()")
	}
	if (Session{RevokedAt: &now}).Live() {
		t.Error("a revoked session reports Live()")
	}
}

// TestDeviceLabel bounds sessions.device_info. The column is plain `text`, so
// nothing below this function stops a full User-Agent (or a megabyte of it) from
// being written once per activation. Rejecting rather than truncating is a §7
// call: a silent truncation is silent acceptance wearing a different hat.
func TestDeviceLabel(t *testing.T) {
	// Real, current user agents. These are what a caller would wrongly pass, and
	// they MUST be rejected -- if one of them squeaked under the limit the bound
	// would be decorative.
	const iosSafariUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
	const desktopChromeUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	t.Run("accepts real coarse labels unchanged", func(t *testing.T) {
		for _, in := range []string{
			"iPhone Safari",
			"Android Chrome",
			"Pixel 8 / Firefox 130",
			"iPad Safari (iPadOS 17)",
			"Samsung Internet",
			"Wéb Tarayıcı — Türkçe etiket", // non-ASCII must survive
		} {
			got, err := deviceLabel(in)
			if err != nil {
				t.Errorf("deviceLabel(%q) = error %v, want it accepted", in, err)
			}
			if got != in {
				t.Errorf("deviceLabel(%q) rewrote the label to %q", in, got)
			}
		}
	})

	t.Run("absent stays absent", func(t *testing.T) {
		// Only SPACE-category input is "absent". A tab-or-newline-only string is
		// not blank-but-harmless, it is malformed, and it is rejected below.
		for _, in := range []string{"", "   ", "  "} {
			got, err := deviceLabel(in)
			if err != nil {
				t.Errorf("deviceLabel(%q) = error %v, want the empty label accepted", in, err)
			}
			if got != "" {
				t.Errorf("deviceLabel(%q) = %q, want \"\" so the column stores NULL", in, got)
			}
		}
	})

	t.Run("trims spaces but never hides a control character", func(t *testing.T) {
		got, err := deviceLabel("   iPhone Safari  ")
		if err != nil {
			t.Fatalf("deviceLabel: %v", err)
		}
		if got != "iPhone Safari" {
			t.Fatalf("deviceLabel trimmed to %q, want %q", got, "iPhone Safari")
		}

		// EDGE control characters used to be swallowed by TrimSpace before the
		// scan ran, so the caller never learned its label was malformed. They are
		// now rejected exactly like interior ones: position must not matter.
		for name, in := range map[string]string{
			"leading NEL U+0085":  "\u0085iPhone Safari",
			"trailing NEL U+0085": "iPhone Safari\u0085",
			"leading VT U+000B":   "\u000biPhone Safari",
			"leading LF":          "\niPhone Safari",
			"trailing tab":        "iPhone Safari\t",
			"trailing CR":         "iPhone Safari\r",
			"controls only":       "\t\n ",
		} {
			if got, err := deviceLabel(in); err == nil {
				t.Errorf("deviceLabel accepted %s and silently stored %q", name, got)
			}
		}
	})

	t.Run("rejects full user agents", func(t *testing.T) {
		for name, ua := range map[string]string{
			"ios safari":     iosSafariUA,
			"desktop chrome": desktopChromeUA,
		} {
			if _, err := deviceLabel(ua); err == nil {
				t.Errorf("deviceLabel accepted a full %s user agent (%d runes)", name, len([]rune(ua)))
			}
		}
	})

	t.Run("rejects oversize", func(t *testing.T) {
		for name, in := range map[string]string{
			"one over the limit": strings.Repeat("a", deviceLabelMaxLen+1),
			"absurdly long":      strings.Repeat("a", 1<<20),
		} {
			if _, err := deviceLabel(in); err == nil {
				t.Errorf("deviceLabel accepted %s", name)
			}
		}
		// Exactly at the limit is fine: the boundary is inclusive.
		if _, err := deviceLabel(strings.Repeat("a", deviceLabelMaxLen)); err != nil {
			t.Errorf("deviceLabel rejected a label exactly at the limit: %v", err)
		}
	})

	t.Run("bounds the raw input in bytes before scanning", func(t *testing.T) {
		// Padding past the byte gate is refused rather than trimmed, so every
		// step below the gate runs on a bounded string.
		if _, err := deviceLabel(strings.Repeat(" ", deviceLabelMaxBytes+1)); err == nil {
			t.Error("deviceLabel accepted padding past the byte gate")
		}
		// The byte gate must never reject content the rune limit allows: a
		// full-length multi-byte label still passes.
		multi := strings.Repeat("é", deviceLabelMaxLen) // 2 bytes per rune
		if _, err := deviceLabel(multi); err != nil {
			t.Errorf("byte gate rejected a %d-rune label the rune limit allows: %v", deviceLabelMaxLen, err)
		}
		if deviceLabelMaxBytes < 4*deviceLabelMaxLen {
			t.Errorf("byte gate (%d) is below 4x the rune limit (%d): it could reject valid content",
				deviceLabelMaxBytes, 4*deviceLabelMaxLen)
		}
	})

	// One case per rune class the doc comment claims to reject. The earlier
	// version of this test only covered C0 and DEL while the comment said
	// "control characters", which let U+0085 (a genuine Unicode Cc) through — the
	// exact drift these cases now pin.
	t.Run("rejects controls and format characters", func(t *testing.T) {
		for name, in := range map[string]string{
			"C0 newline":              "iPhone\nSafari",
			"C0 tab":                  "iPhone\tSafari",
			"C0 carriage return":      "iPhone\rSafari",
			"C0 null":                 "iPhone\x00Safari",
			"DEL":                     "iPhone\u007fSafari",
			"C1 NEL U+0085":           "iPhone\u0085Safari",
			"C1 U+009F":               "iPhone\u009fSafari",
			"Cf zero width U+200B":    "iPhone\u200bSafari",
			"Cf ZWJ U+200D":           "iPhone\u200dSafari",
			"Cf RTL override U+202E":  "iPhone\u202eSafari",
			"Cf BiDi isolate U+2066":  "iPhone\u2066Safari",
			"Cf soft hyphen U+00AD":   "iPhone\u00adSafari",
			"Cf Arabic letter U+061C": "iPhone\u061cSafari",
			"Zl line sep U+2028":      "iPhone\u2028Safari",
			"Zp para sep U+2029":      "iPhone\u2029Safari",
		} {
			if _, err := deviceLabel(in); err == nil {
				t.Errorf("deviceLabel accepted %s", name)
			}
		}
	})

	// The utf8.ValidString branch had NO case at all before this: an audit had to
	// probe it by hand to show it worked. §8 wants a case per branch.
	t.Run("rejects invalid UTF-8", func(t *testing.T) {
		for name, in := range map[string]string{
			"lone continuation byte": "iPhone \x80 Safari",
			"truncated sequence":     "iPhone \xe2\x82 Safari",
			"invalid pair":           "iPhone \xff\xfe Safari",
			"bare 0xff":              "\xff",
		} {
			got, err := deviceLabel(in)
			if err == nil {
				t.Errorf("deviceLabel accepted %s (returned %q)", name, got)
				continue
			}
			if !strings.Contains(err.Error(), "UTF-8") {
				t.Errorf("%s rejected with the wrong error: %v", name, err)
			}
		}
	})

	// Explicitly NOT this function's job — pinned so nobody "fixes" it here and
	// quietly changes what callers may assume downstream.
	t.Run("passes through what other layers must handle", func(t *testing.T) {
		// CSV formula injection is neutralised by the CSV writer (M6-07),
		// because whether a leading '=' is dangerous depends on the sink.
		const formula = "=cmd|'/c calc'!A1"
		got, err := deviceLabel(formula)
		if err != nil {
			t.Fatalf("deviceLabel rejected %q: this function does not own CSV escaping: %v", formula, err)
		}
		if got != formula {
			t.Fatalf("deviceLabel rewrote %q to %q: content must not be altered", formula, got)
		}
		// No Unicode normalisation: the decomposed form survives as sent.
		const decomposed = "Caf\u0065\u0301 Browser" // e + COMBINING ACUTE, not the precomposed U+00E9
		got, err = deviceLabel(decomposed)
		if err != nil {
			t.Fatalf("deviceLabel rejected a decomposed label: %v", err)
		}
		if got != decomposed {
			t.Fatal("deviceLabel normalised the label; it promises not to rewrite content")
		}
	})

	t.Run("Issue refuses rather than truncating", func(t *testing.T) {
		fake := &countingDB{}
		m := newManager(t, fake)
		_, err := m.Issue(context.Background(), IssueParams{
			TenantID: uuid.New(), EmployeeID: uuid.New(), DeviceInfo: iosSafariUA,
		})
		if err == nil {
			t.Fatal("Issue accepted a full user agent as a device label")
		}
		// The rejection happens BEFORE any database work, so nothing is written.
		if fake.withTenant != 0 {
			t.Fatalf("Issue opened %d transactions before rejecting the label, want 0", fake.withTenant)
		}
	})
}
