package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/mail"
)

// newEmailFlow wires the REAL recovery flow to the REAL e-mail channel and the REAL
// SMTP transport, speaking to an in-test relay: the whole path an "email" deployment
// takes, minus the relay. Every line goes to logged as text and, when asJSON is not
// nil, also to asJSON as one JSON object per line (so a test can read its KEYS).
func newEmailFlow(t *testing.T, relay *fakeRelay, resets panelResets, trail *fakeTrail, logged, asJSON *bytes.Buffer, timeout time.Duration) (*AdminReset, string, string) {
	t.Helper()
	user, pass := relayCredentials(t)
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	var handler slog.Handler = slog.NewTextHandler(logged, opts)
	if asJSON != nil {
		handler = teeHandler{handler, slog.NewJSONHandler(asJSON, opts)}
	}
	log := slog.New(handler)
	ch, err := NewEmailResetChannel(relay.breaker(t, user, pass, timeout), adminTestConfig().BaseURL, log)
	if err != nil {
		t.Fatalf("NewEmailResetChannel: %v", err)
	}
	h, err := NewAdminReset(resets, ch, trail, adminTestConfig(), log)
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	stopWorkerAtCleanup(t, h)
	return h, user, pass
}

// teeHandler writes every record to both handlers.
type teeHandler struct{ a, b slog.Handler }

func (h teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.a.Enabled(ctx, l) || h.b.Enabled(ctx, l)
}

func (h teeHandler) Handle(ctx context.Context, r slog.Record) error {
	return errors.Join(h.a.Handle(ctx, r.Clone()), h.b.Handle(ctx, r.Clone()))
}

func (h teeHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return teeHandler{h.a.WithAttrs(as), h.b.WithAttrs(as)}
}

func (h teeHandler) WithGroup(name string) slog.Handler {
	return teeHandler{h.a.WithGroup(name), h.b.WithGroup(name)}
}

// resetLogKeys is ADR 0022 §10's closed set for a delivery's log line, on the reset
// path, plus slog's own three: the client address, the administrator's and the reset
// row's ids, the error's Go type, a SendError's class and reply code, an accepted
// send's message id, and the outbox's state on a refused hand-over.
var resetLogKeys = map[string]bool{
	"time": true, "level": true, "msg": true,
	"ip": true, "admin_user_id": true, "reset_id": true, "err_type": true,
	"class": true, "smtp_code": true, "message_id": true, "queue": true,
}

// offClosedSet returns every key in the JSON lines that is outside resetLogKeys and
// the extra keys a test admits for a line it knows (the M7-04 budget line's "scope").
func offClosedSet(t *testing.T, jsonLines string, extra ...string) []string {
	t.Helper()
	admitted := map[string]bool{}
	for _, k := range extra {
		admitted[k] = true
	}
	var off []string
	for _, line := range strings.Split(strings.TrimSpace(jsonLines), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("a log line is not JSON: %v", err)
		}
		for k := range rec {
			if !resetLogKeys[k] && !admitted[k] {
				off = append(off, k)
			}
		}
	}
	return off
}

// resetLinkBase is the link base the handler builds for adminTestConfig.
func resetLinkBase() string {
	return strings.TrimRight(adminTestConfig().BaseURL, "/") + adminResetNewPath
}

// mountReset puts h's routes on a fresh router.
func mountReset(h *AdminReset) http.Handler {
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

// TestEmailResetChannel_SendsEachLinkToTheRowsAddressAndNamesNobody is M7-04's
// criterion 3 against an SMTP relay (EM-5): the link goes ONLY to the address on the
// administrator's own row (the form typed another spelling), each account behind one
// address gets its own message with its own link, the link arrives intact in both
// parts, and nothing else about the request travels — no name, no reset id (Ref goes
// nowhere, ADR 0022 §2), and the subject is the fixed one.
func TestEmailResetChannel_SendsEachLinkToTheRowsAddressAndNamesNobody(t *testing.T) {
	t.Parallel()
	typed, onTheRow := "OWNER@Several.Example.Test", "owner@several.example.test"
	grants := grantsFor(onTheRow, 3)
	relay := newFakeRelay(t, relayScript{})
	trail := &fakeTrail{}
	var logged bytes.Buffer
	h, _, _ := newEmailFlow(t, relay, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{typed: grants}}, trail, &logged, nil, 5*time.Second)
	router := mountReset(h)

	body := requestReset(t, newBrowser(t, router), typed)
	drain(t, h)

	sessions := relay.completed()
	if len(sessions) != len(grants) {
		t.Fatalf("the relay received %d conversation(s), want %d (one per account)", len(sessions), len(grants))
	}
	want := map[string]bool{}
	for _, g := range grants {
		want[g.Issued.Link(resetLinkBase())] = true
	}
	for i, s := range sessions {
		if len(s.rcptTo) != 1 || !strings.EqualFold(s.rcptTo[0], "RCPT TO:<"+onTheRow+">") || !strings.Contains(s.rcptTo[0], onTheRow) {
			t.Errorf("message %d went to %q, want exactly the row's address %q", i, s.rcptTo, onTheRow)
		}
		if !s.authed {
			t.Errorf("message %d: the relay saw no AUTH after TLS", i)
		}
		m := parseRelayed(t, s.message)
		if got := m.header.Get("Subject"); got != "Reset your Taptime password" {
			t.Errorf("message %d: Subject %q, want the fixed subject", i, got)
		}
		if got := m.header.Get("To"); got != onTheRow {
			t.Errorf("message %d: To %q, want %q", i, got, onTheRow)
		}
		found := ""
		for link := range want {
			if strings.Contains(m.text, link) && strings.Contains(m.html, link) {
				found = link
			}
		}
		if found == "" {
			t.Errorf("message %d carries none of the minted links in both parts", i)
		}
		delete(want, found)
		for _, g := range grants {
			if strings.Contains(s.message, g.Issued.Reset.ID.String()) {
				t.Errorf("message %d carries a reset id; the Ref is the caller's log label and travels nowhere", i)
			}
		}
		for _, name := range []string{"Several", "several.example.test"} {
			if strings.Contains(m.text, name) {
				t.Errorf("message %d names %q in its text; the reset e-mail names nobody", i, name)
			}
		}
	}
	if len(want) != 0 {
		t.Errorf("%d link(s) were never sent", len(want))
	}
	// The response carries none of them.
	for _, g := range grants {
		if strings.Contains(body, g.Issued.Link(resetLinkBase())) {
			t.Error("the response carries a recovery link")
		}
	}
	if n := trail.count(ActionAdminResetRequested); n != len(grants) {
		t.Errorf("%d requested row(s), want %d", n, len(grants))
	}
	if n := strings.Count(logged.String(), "message_id="+relayMessageID); n != len(grants) {
		t.Errorf("%d accepted line(s) carry the relay's message id, want %d:\n%s", n, len(grants), logged.String())
	}
}

// TestEmailResetChannel_LogsOnlyIdsClassAndCode is ADR 0022 §10 on the reset path,
// against a relay that does what relays do: names the recipient and quotes what it was
// sending in its refusals, echoes a value it was sent into its message id, stalls.
//
// THE ONLY THINGS A LINE MAY CARRY are the reset row's id, the administrator's id,
// the client address, err_type, the class and reply code of a *mail.SendError and an
// accepted send's message id. So each row asserts the outcome row AND scans the whole
// log for: the recipient (case-insensitive), the link, the token, the link in base64,
// the SMTP username and password (and every 8-character run of each), and the relay's
// own words.
//
// POSITIVE CONTROL, per row: the relay's reply really carries the secrets it is
// scanned for — a scan of a log that could not contain them proves nothing.
func TestEmailResetChannel_LogsOnlyIdsClassAndCode(t *testing.T) {
	t.Parallel()
	const who = "leak-probe@relay.example.test"
	g := grantFor(who)
	link := g.Issued.Link(resetLinkBase())
	token := strings.TrimPrefix(link, resetLinkBase()+"?t=")
	quoted := "5.1.1 <" + strings.ToUpper(who) + "> refused while sending " + link + " " +
		base64.StdEncoding.EncodeToString([]byte(link))

	for _, tc := range []struct {
		name    string
		script  relayScript
		timeout time.Duration
		action  string
		class   mail.Class
		code    int
		quotes  bool // the relay's reply text carries the secrets
		msgID   string
	}{
		{"accepted, clean id", relayScript{}, 5 * time.Second, ActionAdminResetRequested, "", 0, false, relayMessageID},
		{"accepted, the id echoes the token", relayScript{endReply: "250 Ok " + token + "-000000"}, 5 * time.Second,
			ActionAdminResetRequested, "", 0, true, ""},
		{"recipient refused, quoting address and link", relayScript{rcptReply: "550 " + quoted}, 5 * time.Second,
			ActionAdminResetUndelivered, mail.ClassRejected, 550, true, ""},
		{"message refused at the end of data", relayScript{endReply: "554 " + quoted}, 5 * time.Second,
			ActionAdminResetUndelivered, mail.ClassRejected, 554, true, ""},
		{"throttled twice", relayScript{rcptReply: "451 4.3.0 " + quoted}, 5 * time.Second,
			ActionAdminResetUndelivered, mail.ClassThrottled, 451, true, ""},
		{"credentials refused", relayScript{authReply: "535 5.7.8 " + quoted}, 5 * time.Second,
			ActionAdminResetUndelivered, mail.ClassAuth, 535, true, ""},
		{"no STARTTLS", relayScript{noSTARTTLS: true}, 5 * time.Second,
			ActionAdminResetUndelivered, mail.ClassTLS, 0, false, ""},
		{"accepts the connection and never greets", relayScript{hang: "greeting"}, 500 * time.Millisecond,
			ActionAdminResetUndelivered, mail.ClassTimeout, 0, false, ""},
		{"stalls inside the TLS handshake", relayScript{hang: "handshake"}, 500 * time.Millisecond,
			ActionAdminResetUndelivered, mail.ClassTimeout, 0, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.quotes {
				reply := tc.script.rcptReply + tc.script.endReply + tc.script.authReply
				if !strings.Contains(strings.ToLower(reply), strings.ToLower(who)) && !strings.Contains(reply, token) {
					t.Fatal("CONTROL FAILED: the relay's reply carries neither the address nor the token")
				}
			}
			relay := newFakeRelay(t, tc.script)
			trail := &fakeTrail{}
			var logged, asJSON bytes.Buffer
			h, user, pass := newEmailFlow(t, relay, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: {g}}},
				trail, &logged, &asJSON, tc.timeout)
			requestReset(t, newBrowser(t, mountReset(h)), who)
			drain(t, h)

			// THE KEYS: every line's keys are inside the closed set — a key that is not
			// (an "err", a "to") is the leak this rule exists to stop, whatever its
			// value happens to be in this run.
			if off := offClosedSet(t, asJSON.String()); len(off) > 0 {
				t.Errorf("log keys outside ADR 0022 §10's closed set: %q\n%s", off, asJSON.String())
			}
			if asJSON.Len() == 0 {
				t.Fatal("CONTROL FAILED: no line was logged, so the key check read nothing")
			}

			if n := trail.count(tc.action); n != 1 || trail.total() != 1 {
				t.Errorf("%d %q row(s) of %d in all, want exactly 1", n, tc.action, trail.total())
			}
			got := logged.String()
			if tc.action == ActionAdminResetUndelivered {
				for _, want := range []string{"class=" + string(tc.class), "smtp_code=" + strconv.Itoa(tc.code),
					"err_type=*mail.SendError", "reset_id=" + g.Issued.Reset.ID.String()} {
					if !strings.Contains(got, want) {
						t.Errorf("the failure line lacks %s:\n%s", want, got)
					}
				}
			} else {
				// slog's text handler writes an empty value as "".
				want := "message_id=" + tc.msgID
				if tc.msgID == "" {
					want = `message_id=""`
				}
				if !strings.Contains(got, want) {
					t.Errorf("the accepted line does not carry %s:\n%s", want, got)
				}
			}
			secrets := map[string]string{
				"token": token, "link": link, "base64 link": base64.StdEncoding.EncodeToString([]byte(link)),
				"username": user, "password": pass, "relay text": "refused while sending",
			}
			for _, cred := range []string{user, pass} {
				for i := 0; i+8 <= len(cred); i++ {
					secrets["credential window "+strconv.Itoa(i)] = cred[i : i+8]
				}
			}
			if strings.Contains(strings.ToLower(got), strings.ToLower(who)) {
				t.Errorf("the recipient reached the log:\n%s", got)
			}
			for what, secret := range secrets {
				if strings.Contains(got, secret) {
					t.Errorf("the %s reached the log:\n%s", what, got)
				}
			}
		})
	}
}

// TestEmailResetChannel_QueueAndBudgetLinesLogOnlyIds is ADR 0022 §10 on the three
// lines TestEmailResetChannel_LogsOnlyIdsClassAndCode never reaches (M10 EM-5A, 2nd
// round): the outbox refusing a grant because it is FULL, because it is CLOSING, and
// the account budget's "rate limited" line. Each runs the real flow, transport and
// in-test relay; every line's keys must be in the closed set (plus "scope", the M7-04
// budget line's own key), and no line may carry the recipient (any case), a link, a
// token, a link in base64, the SMTP credentials (or any 8-character run of either) or
// the relay's words. Control, per case: the line it exists for was written.
func TestEmailResetChannel_QueueAndBudgetLinesLogOnlyIds(t *testing.T) {
	t.Parallel()
	const who = "queue-probe@relay.example.test"
	quoted := "5.1.1 <" + strings.ToUpper(who) + "> refused while sending"
	scan := func(t *testing.T, asJSON, user, pass string, grants []adminauth.ResetGrant) {
		t.Helper()
		if off := offClosedSet(t, asJSON, "scope"); len(off) > 0 {
			t.Errorf("log keys outside the closed set: %q\n%s", off, asJSON)
		}
		if strings.Contains(strings.ToLower(asJSON), strings.ToLower(who)) {
			t.Errorf("the recipient reached the log:\n%s", asJSON)
		}
		secrets := map[string]string{"username": user, "password": pass, "relay text": "refused while sending"}
		for _, cred := range []string{user, pass} {
			for i := 0; i+8 <= len(cred); i++ {
				secrets["credential window "+strconv.Itoa(i)] = cred[i : i+8]
			}
		}
		for i, g := range grants {
			link := g.Issued.Link(resetLinkBase())
			secrets["link "+strconv.Itoa(i)] = link
			secrets["token "+strconv.Itoa(i)] = strings.TrimPrefix(link, resetLinkBase()+"?t=")
			secrets["base64 link "+strconv.Itoa(i)] = base64.StdEncoding.EncodeToString([]byte(link))
		}
		for what, secret := range secrets {
			if strings.Contains(asJSON, secret) {
				t.Errorf("the %s reached the log:\n%s", what, asJSON)
			}
		}
	}

	t.Run("outbox full", func(t *testing.T) {
		t.Parallel()
		first, fill, late := grantsFor(who, 1), grantsFor(who, resetOutboxSize), grantsFor(who, 1)
		relay := newFakeRelay(t, relayScript{hang: "greeting"})
		var logged, asJSON bytes.Buffer
		h, user, pass := newEmailFlow(t, relay, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{
			"first@x.test": first, "fill@x.test": fill, "late@x.test": late,
		}}, &fakeTrail{}, &logged, &asJSON, 5*time.Second)
		router := mountReset(h)
		requestReset(t, newBrowser(t, router), "first@x.test")
		relay.waitAccepted(t, 1) // the worker is inside the relay; the outbox is empty
		requestReset(t, newBrowser(t, router), "fill@x.test")
		requestReset(t, newBrowser(t, router), "late@x.test")
		ctx, cancel := context.WithTimeout(context.Background(), ResetDrainWriteReserve+300*time.Millisecond)
		defer cancel()
		if err := h.Drain(ctx); err != nil {
			t.Fatalf("drain: %v", err)
		}
		// The controls match the value's PREFIX, so a line that grew something after
		// "full" or "closing" is still found here and judged by the scan below.
		if !strings.Contains(asJSON.String(), `"queue":"full`) {
			t.Fatalf("CONTROL FAILED: no full-outbox line was written:\n%s", asJSON.String())
		}
		scan(t, asJSON.String(), user, pass, append(append(first, fill...), late...))
	})

	t.Run("outbox closing", func(t *testing.T) {
		t.Parallel()
		grants := grantsFor(who, 2)
		relay := newFakeRelay(t, relayScript{})
		var logged, asJSON bytes.Buffer
		h, user, pass := newEmailFlow(t, relay, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants}},
			&fakeTrail{}, &logged, &asJSON, 5*time.Second)
		drain(t, h)
		requestReset(t, newBrowser(t, mountReset(h)), who)
		if strings.Count(asJSON.String(), `"queue":"closing`) != 2 {
			t.Fatalf("CONTROL FAILED: want two closing-outbox lines:\n%s", asJSON.String())
		}
		scan(t, asJSON.String(), user, pass, grants)
	})

	t.Run("one account past its budget", func(t *testing.T) {
		t.Parallel()
		one := grantFor(who)
		var grants []adminauth.ResetGrant
		for i := 0; i < adminResetAccountLimit+2; i++ {
			g := one
			g.Issued.Reset.ID = uuid.New()
			grants = append(grants, g)
		}
		relay := newFakeRelay(t, relayScript{rcptReply: "550 " + quoted})
		var logged, asJSON bytes.Buffer
		h, user, pass := newEmailFlow(t, relay, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: grants}},
			&fakeTrail{}, &logged, &asJSON, 5*time.Second)
		requestReset(t, newBrowser(t, mountReset(h)), who)
		drain(t, h)
		if !strings.Contains(asJSON.String(), `"msg":"panel recovery rate limited"`) {
			t.Fatalf("CONTROL FAILED: the budget line was not written:\n%s", asJSON.String())
		}
		scan(t, asJSON.String(), user, pass, grants)
	})
}

// TestEmailResetChannel_TimingIsFlatWithASlowRelay is M7-04's criterion 5 (timing
// half) against an SMTP relay that holds its reply to the end of data for two
// seconds: the response does not wait for it (EM-5's re-run of the M7-04 criteria).
// Not parallel: it measures the clock.
func TestEmailResetChannel_TimingIsFlatWithASlowRelay(t *testing.T) {
	const who = "slow@relay.example.test"
	relay := newFakeRelay(t, relayScript{endDelay: 2 * time.Second})
	trail := &fakeTrail{}
	var logged bytes.Buffer
	h, _, _ := newEmailFlow(t, relay, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: {grantFor(who)}}},
		trail, &logged, nil, 5*time.Second)
	router := mountReset(h)

	const samples = 5
	var hit, miss []time.Duration
	for i := 0; i < samples; i++ {
		hit = append(hit, timeOneRequest(t, router, who))
		miss = append(miss, timeOneRequest(t, router, "nobody@unregistered.example.test"))
	}
	sort.Slice(hit, func(i, j int) bool { return hit[i] < hit[j] })
	sort.Slice(miss, func(i, j int) bool { return miss[i] < miss[j] })
	ceiling := resetRequestFloor + 50*time.Millisecond
	t.Logf("SMTP relay +2s: median registered %v, unregistered %v; band [%v, %v]", hit[samples/2], miss[samples/2], resetRequestFloor, ceiling)
	for name, m := range map[string]time.Duration{"registered": hit[samples/2], "unregistered": miss[samples/2]} {
		if m < resetRequestFloor || m > ceiling {
			t.Errorf("the %s median is %v, outside [%v, %v]", name, m, resetRequestFloor, ceiling)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), ResetDrainWriteReserve+500*time.Millisecond)
	defer cancel()
	if err := h.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	// ANTI-VACUITY: the registered arm reached the relay.
	if n := len(relay.completed()); n == 0 {
		t.Fatal("the relay received no conversation; the registered arm never sent")
	}
	if n := trail.total(); n != samples {
		t.Errorf("%d row(s), want %d — one per registered request", n, samples)
	}
}

// TestEmailResetChannel_RefusesWhatItCannotBuild: the constructor refuses at boot a
// base URL the e-mail cannot carry (the email package's own rule: https, plain http
// only on loopback) and a missing sender, instead of booting a deployment that
// records every link undelivered.
func TestEmailResetChannel_RefusesWhatItCannotBuild(t *testing.T) {
	t.Parallel()
	relay := newFakeRelay(t, relayScript{})
	user, pass := relayCredentials(t)
	s := relay.breaker(t, user, pass, time.Second)
	for _, tc := range []struct {
		base string
		ok   bool
	}{
		{"https://panel.example.test", true},
		{"https://panel.example.test/", true},
		{"http://localhost:8080", true},
		{"http://127.0.0.1:8080", true},
		{"http://panel.example.test", false},
		{"https://panel.example.test?x=1", false},
		{"", false},
	} {
		_, err := NewEmailResetChannel(s, tc.base, nil)
		if (err == nil) != tc.ok {
			t.Errorf("base %q: err = %v, want ok=%v", tc.base, err, tc.ok)
		}
	}
	if _, err := NewEmailResetChannel(nil, "https://panel.example.test", nil); err == nil {
		t.Error("a nil sender was accepted")
	}
}

// TestEmailResetChannel_ALinkWithUnderAMinuteLeftIsNotSent: the e-mail states the
// time left when it is SENT, and a link with under a minute left is refused by the
// renderer — the grant ends undelivered and the relay is never dialled.
func TestEmailResetChannel_ALinkWithUnderAMinuteLeftIsNotSent(t *testing.T) {
	t.Parallel()
	const who = "late-link@relay.example.test"
	g := grantFor(who)
	g.Issued.Reset.ExpiresAt = time.Now().Add(30 * time.Second)
	relay := newFakeRelay(t, relayScript{})
	trail := &fakeTrail{}
	var logged bytes.Buffer
	h, _, _ := newEmailFlow(t, relay, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{who: {g}}}, trail, &logged, nil, time.Second)
	requestReset(t, newBrowser(t, mountReset(h)), who)
	drain(t, h)
	if n := trail.count(ActionAdminResetUndelivered); n != 1 {
		t.Errorf("%d undelivered row(s), want 1", n)
	}
	if n := len(relay.completed()); n != 0 {
		t.Errorf("the relay was dialled %d time(s) for a link the e-mail could not honestly describe", n)
	}
	if !strings.Contains(logged.String(), "err_type=*fmt.wrapError") {
		t.Errorf("the failure line does not name the render failure's type:\n%s", logged.String())
	}
}

// TestEmailResetChannel_AStoredAddressTheRelayCannotTakeIsNeverDialled: the address
// rule of the stored row is weaker than the sending rule (ADR 0022 B14, §4) — a
// non-ASCII address can sit on an administrator's row. Its grant ends undelivered
// with class invalid_address, the relay is never dialled, and the address is not in
// the log.
func TestEmailResetChannel_AStoredAddressTheRelayCannotTakeIsNeverDialled(t *testing.T) {
	t.Parallel()
	const stored = "ćali@relay.example.test"
	relay := newFakeRelay(t, relayScript{})
	trail := &fakeTrail{}
	var logged bytes.Buffer
	h, _, _ := newEmailFlow(t, relay, &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{stored: {grantFor(stored)}}},
		trail, &logged, nil, time.Second)
	requestReset(t, newBrowser(t, mountReset(h)), stored)
	drain(t, h)
	if n := trail.count(ActionAdminResetUndelivered); n != 1 {
		t.Errorf("%d undelivered row(s), want 1", n)
	}
	if n := len(relay.completed()); n != 0 {
		t.Errorf("the relay was dialled %d time(s) for an address the sending rule refuses", n)
	}
	got := logged.String()
	if !strings.Contains(got, "class="+string(mail.ClassInvalidAddress)) {
		t.Errorf("the failure line does not say %s:\n%s", mail.ClassInvalidAddress, got)
	}
	if strings.Contains(got, "relay.example.test") || strings.Contains(got, "ali@") {
		t.Errorf("the stored address reached the log:\n%s", got)
	}
}

// TestEmailResetChannel_StatesTheTimeLeftWhenSent pins the lifetime decision: the
// e-mail's phrase comes from ExpiresAt − now, so a link minted a while ago never
// promises the full ResetTTL.
func TestEmailResetChannel_StatesTheTimeLeftWhenSent(t *testing.T) {
	t.Parallel()
	captured := &capturingSender{}
	b, err := mail.NewBreaker(captured, mail.BreakerConfig{Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	ch := &emailResetChannel{sender: b, baseURL: adminTestConfig().BaseURL, log: slog.New(slog.DiscardHandler)}
	g := grantFor("left@relay.example.test")
	for _, tc := range []struct {
		left time.Duration
		want string
	}{
		{adminauth.ResetTTL, "59 minutes"}, // a moment has passed since "now + TTL"
		{40 * time.Minute, "39 minutes"},
		{90 * time.Second, "1 minute"},
	} {
		if err := ch.DeliverReset(context.Background(), ResetDelivery{
			Recipient: "left@relay.example.test", Link: g.Issued.Link(resetLinkBase()),
			ExpiresAt: time.Now().Add(tc.left), ResetID: g.Issued.Reset.ID,
		}); err != nil {
			t.Fatalf("%v left: %v", tc.left, err)
		}
		m := captured.last
		if !strings.Contains(m.Text, "stays valid for "+tc.want+".") {
			t.Errorf("%v left: the text does not say %q", tc.left, tc.want)
		}
	}
}

// capturingSender keeps the last Message instead of sending it.
type capturingSender struct{ last mail.Message }

func (c *capturingSender) Send(_ context.Context, m mail.Message) (mail.Receipt, error) {
	if m.To == "" {
		return mail.Receipt{}, errors.New("no recipient")
	}
	c.last = m
	return mail.Receipt{MessageID: "id"}, nil
}
