package handler

// passwordnotice_test.go — the "your password was changed" notice (M10 EM-9). The
// claim these tests measure is in passwordnotice.go, in three parts.
//
// 🔴 NO TEST HERE PRINTS AN ADDRESS, A LINK, A TOKEN OR A RENDERED BODY in a failure
// message: they name the case, a count or a class.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/mail"
)

// fakeNotices is a passwordNotifier that keeps every change it was told about. It is
// what every AdminAuth built by a test is given unless the test is about the notice.
type fakeNotices struct {
	mu      sync.Mutex
	changes []passwordChange
}

func (f *fakeNotices) passwordChanged(_ context.Context, c passwordChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, c)
}

func (f *fakeNotices) all() []passwordChange {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]passwordChange(nil), f.changes...)
}

// panicTrail is an audit recorder whose every write panics — the shape of a fault
// INSIDE the audit writer. It counts the writes it was asked for.
type panicTrail struct {
	mu    sync.Mutex
	calls int
}

func (p *panicTrail) Record(context.Context, audit.Event) (uuid.UUID, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	panic("a recorder fault")
}

// noticeLogKeys is ADR 0022 §10's closed set as the notice uses it, plus slog's own
// three: the administrator's and the tenant's ids, the error's Go type, a SendError's
// class and reply code, and an accepted send's message id.
var noticeLogKeys = map[string]bool{
	"time": true, "level": true, "msg": true,
	"admin_user_id": true, "tenant_id": true, "err_type": true,
	"class": true, "smtp_code": true, "message_id": true,
}

// noticeKeysOffTheSet returns every key of the JSON lines outside noticeLogKeys.
func noticeKeysOffTheSet(t *testing.T, jsonLines string) []string {
	t.Helper()
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
			if !noticeLogKeys[k] {
				off = append(off, k)
			}
		}
	}
	return off
}

// noticeRows returns the notice rows of a trail, in order.
func noticeRows(events []audit.Event) []audit.Event {
	var out []audit.Event
	for _, e := range events {
		if e.Action == ActionAdminPasswordNoticeSent || e.Action == ActionAdminPasswordNoticeUndelivered {
			out = append(out, e)
		}
	}
	return out
}

// newNoticeFlow is the recovery flow with the given channel (nil: "none"), resets and
// trail, logging JSON into logged. The worker is drained at cleanup.
func newNoticeFlow(t *testing.T, resets panelResets, ch ResetChannel, trail auditRecorder, logged *bytes.Buffer) *AdminReset {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	if logged != nil {
		log = slog.New(slog.NewJSONHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	h, err := NewAdminReset(resets, ch, trail, adminTestConfig(), log)
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	stopWorkerAtCleanup(t, h)
	return h
}

// changeOnce calls passwordChanged and fails the test if a panic escapes it.
func changeOnce(t *testing.T, h *AdminReset, c passwordChange) {
	t.Helper()
	defer func() {
		if recover() != nil {
			t.Fatal("a panic escaped passwordChanged: a committed change would be answered with a 500")
		}
	}()
	h.passwordChanged(context.Background(), c)
}

// TestPasswordNotice_EveryChangeEndsInOneRowAndTheChangeStands — §4.6 for the notice:
// whatever happens to it, the change it is about gets exactly ONE notice row (sent, or
// undelivered with a fixed reason), nothing escapes to the caller, and the work stops
// where it should (the address is read only when a send is possible, the channel is
// asked only when there is an address).
func TestPasswordNotice_EveryChangeEndsInOneRowAndTheChangeStands(t *testing.T) {
	t.Parallel()
	tenantID, adminID := uuid.New(), uuid.New()
	const onTheRow = "Owner.Row@Notice.Example.Test"
	onRow := func() *fakeResets { return &fakeResets{recipients: map[uuid.UUID]string{adminID: onTheRow}} }
	rejected := &mail.SendError{Class: mail.ClassRejected, SMTPCode: 550}

	for _, tc := range []struct {
		name       string
		channel    func() ResetChannel // nil channel: TAPPA_RESET_DELIVERY=none
		resets     *fakeResets
		spendCap   bool
		action     string
		reason     string
		class      string
		code       int
		sends      int // notices the channel was handed; -1: not a recordingChannel
		reads      int // NoticeRecipient calls
		nilChannel bool
	}{
		{name: "sent", channel: func() ResetChannel { return &recordingChannel{} }, resets: onRow(),
			action: ActionAdminPasswordNoticeSent, sends: 1, reads: 1},
		{name: "the relay refuses it", channel: func() ResetChannel { return &recordingChannel{noticeErr: rejected} }, resets: onRow(),
			action: ActionAdminPasswordNoticeUndelivered, reason: noticeReasonSendFailed, class: string(mail.ClassRejected), code: 550, sends: 1, reads: 1},
		{name: "the channel fails with a plain error", channel: func() ResetChannel { return &recordingChannel{noticeErr: errors.New("boom")} }, resets: onRow(),
			action: ActionAdminPasswordNoticeUndelivered, reason: noticeReasonSendFailed, sends: 1, reads: 1},
		{name: "none: this deployment sends no e-mail", nilChannel: true, resets: onRow(),
			action: ActionAdminPasswordNoticeUndelivered, reason: noticeReasonOff, sends: 0, reads: 0},
		{name: "the row has no address", channel: func() ResetChannel { return &recordingChannel{} }, resets: &fakeResets{},
			action: ActionAdminPasswordNoticeUndelivered, reason: noticeReasonNoAddress, sends: 0, reads: 1},
		{name: "the address read fails", channel: func() ResetChannel { return &recordingChannel{} }, resets: &fakeResets{recipientErr: errors.New("db down")},
			action: ActionAdminPasswordNoticeUndelivered, reason: noticeReasonReadFailed, sends: 0, reads: 1},
		{name: "the account's cap is spent", channel: func() ResetChannel { return &recordingChannel{} }, resets: onRow(), spendCap: true,
			action: ActionAdminPasswordNoticeUndelivered, reason: noticeReasonLimited, sends: 0, reads: 0},
		{name: "the channel panics with the address in the value", channel: func() ResetChannel { return &panicChannel{panicOn: map[int]bool{1: true}} }, resets: onRow(),
			action: ActionAdminPasswordNoticeUndelivered, reason: noticeReasonFault, sends: -1, reads: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			trail := &fakeTrail{}
			var logged bytes.Buffer
			var ch ResetChannel
			if !tc.nilChannel {
				ch = tc.channel()
			}
			h := newNoticeFlow(t, tc.resets, ch, trail, &logged)
			if tc.spendCap {
				for i := 0; i < passwordNoticeLimit; i++ {
					h.noticeLimiter.Charge(adminID.String())
				}
			}
			changeOnce(t, h, passwordChange{tenantID: tenantID, adminUserID: adminID, via: noticeViaAccount})

			if trail.total() != 1 {
				t.Fatalf("%d row(s) for one change, want exactly 1", trail.total())
			}
			e := trail.eventsSnapshot()[0]
			if e.Action != tc.action {
				t.Fatalf("the row is %q, want %q", e.Action, tc.action)
			}
			if e.TenantID != tenantID || e.ActorID == nil || *e.ActorID != adminID || e.Target != adminID.String() {
				t.Error("the row is not about the changed account in its own tenant")
			}
			d, ok := e.Detail.(passwordNoticeDetail)
			if !ok {
				t.Fatalf("the detail is %T, want passwordNoticeDetail", e.Detail)
			}
			if d.Via != noticeViaAccount || d.Reason != tc.reason || d.Class != tc.class || d.SMTPCode != tc.code {
				t.Errorf("detail via=%q reason=%q class=%q code=%d; want via=%q reason=%q class=%q code=%d",
					d.Via, d.Reason, d.Class, d.SMTPCode, noticeViaAccount, tc.reason, tc.class, tc.code)
			}
			if want := map[string]string{ActionAdminPasswordNoticeSent: "sent", ActionAdminPasswordNoticeUndelivered: "undelivered"}[tc.action]; d.Outcome != want {
				t.Errorf("outcome %q, want %q", d.Outcome, want)
			}
			if rc, ok := ch.(*recordingChannel); ok && tc.sends >= 0 {
				got := rc.allNotices()
				if len(got) != tc.sends {
					t.Fatalf("the channel was handed %d notice(s), want %d", len(got), tc.sends)
				}
				for _, n := range got {
					if n.Recipient != onTheRow || n.TenantID != tenantID || n.AdminUserID != adminID {
						t.Error("the notice was not addressed to the row's address for the changed account")
					}
				}
			}
			tc.resets.mu.Lock()
			reads := tc.resets.recipientCalls
			tc.resets.mu.Unlock()
			if reads != tc.reads {
				t.Errorf("the address was read %d time(s), want %d", reads, tc.reads)
			}
			if strings.Contains(strings.ToLower(logged.String()), strings.ToLower(onTheRow)) {
				t.Error("the address reached the log")
			}
			if off := noticeKeysOffTheSet(t, logged.String()); len(off) > 0 {
				t.Errorf("log keys outside the closed set: %q", off)
			}
		})
	}

	// A FAULT INSIDE THE AUDIT WRITER: the outcome row's write had begun, so no second
	// (fault) row is attempted — one write, which panicked, and nothing escapes.
	t.Run("the recorder itself panics", func(t *testing.T) {
		t.Parallel()
		trail := &panicTrail{}
		h := newNoticeFlow(t, onRow(), &recordingChannel{}, trail, nil)
		changeOnce(t, h, passwordChange{tenantID: tenantID, adminUserID: adminID, via: noticeViaRecovery})
		trail.mu.Lock()
		calls := trail.calls
		trail.mu.Unlock()
		if calls != 1 {
			t.Errorf("the recorder was asked for %d write(s), want 1: a write that had begun must not be followed by a second row", calls)
		}
	})
}

// TestPasswordNotice_TheRowCarriesNoAddress — the notice row's detail, as it reaches
// audit_log (JSON), is a closed set of keys and holds no address and no '@' (ADR 0022
// §7's rule, B29's reason), over the outcomes that fill the most fields.
func TestPasswordNotice_TheRowCarriesNoAddress(t *testing.T) {
	t.Parallel()
	tenantID, adminID := uuid.New(), uuid.New()
	const onTheRow = "row-owner@notice.example.test"
	allowed := map[string]bool{"outcome": true, "via": true, "reason": true, "class": true, "smtp_code": true}
	for _, ch := range []ResetChannel{
		&recordingChannel{},
		&recordingChannel{noticeErr: &mail.SendError{Class: mail.ClassRejected, SMTPCode: 554}},
		nil,
	} {
		trail := &fakeTrail{}
		h := newNoticeFlow(t, &fakeResets{recipients: map[uuid.UUID]string{adminID: onTheRow}}, ch, trail, nil)
		changeOnce(t, h, passwordChange{tenantID: tenantID, adminUserID: adminID, via: noticeViaRecovery})
		rows := noticeRows(trail.eventsSnapshot())
		if len(rows) != 1 {
			t.Fatalf("%d notice row(s), want 1", len(rows))
		}
		raw, err := json.Marshal(rows[0].Detail)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var keys map[string]any
		if err := json.Unmarshal(raw, &keys); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for k := range keys {
			if !allowed[k] {
				t.Errorf("the notice row carries the key %q", k)
			}
		}
		if keys["via"] != noticeViaRecovery {
			t.Errorf("the row's via is %v, want %q", keys["via"], noticeViaRecovery)
		}
		if strings.Contains(string(raw), "@") || strings.Contains(strings.ToLower(string(raw)), "row-owner") {
			t.Error("the notice row carries the address")
		}
	}
}

// newNoticeAuth is an AdminAuth whose session resolves to a live admin and whose change
// notifier is n, behind the real router. The admins hook decides the change's outcome.
func newNoticeAuth(t *testing.T, admins *fakeAdmins, trail auditRecorder, n passwordNotifier) http.Handler {
	t.Helper()
	if admins.verify == nil {
		admins.verify = func() (adminauth.Resolved, error) {
			return adminauth.Resolved{
				SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
				Role: accountTestManagerRole, FullName: "Admin Person",
			}, nil
		}
	}
	h, err := NewAdminAuth(admins, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, n,
		adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

func signedInBrowser(t *testing.T, h http.Handler) *browser {
	t.Helper()
	b := newBrowser(t, h)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// TestAccountPasswordSave_NotifiesAfterACommittedChangeOnly — the Account section's
// half of EM-9: a COMMITTED change of one's own password tells the notifier, once,
// with the SESSION's tenant and administrator (§4.5); a change that did not happen
// tells it nothing; and a notice that fails or panics leaves the change's answer (303)
// and its admin.password_changed row exactly as they were — the notice row comes
// AFTER the change's row.
func TestAccountPasswordSave_NotifiesAfterACommittedChangeOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		change  func(_, _, _ uuid.UUID, _, _ string) (int, error)
		form    func() url.Values
		code    int
		notices int
	}{
		{"a committed change", func(_, _, _ uuid.UUID, _, _ string) (int, error) { return 1, nil },
			func() url.Values { return validChange("the-current-password", "a-brand-new-password") }, http.StatusSeeOther, 1},
		{"the current password is wrong", func(_, _, _ uuid.UUID, _, _ string) (int, error) { return 0, adminauth.ErrWrongCurrentPassword },
			func() url.Values { return validChange("not-the-current-one", "a-brand-new-password") }, http.StatusOK, 0},
		{"the new password is the current one", func(_, _, _ uuid.UUID, _, _ string) (int, error) { return 0, adminauth.ErrSameAsCurrentPassword },
			func() url.Values { return validChange("the-current-password", "the-current-password") }, http.StatusOK, 0},
		{"the change fails on our side", func(_, _, _ uuid.UUID, _, _ string) (int, error) { return 0, errors.New("db down") },
			func() url.Values { return validChange("the-current-password", "a-brand-new-password") }, http.StatusInternalServerError, 0},
		{"the new password is refused before the database", nil,
			func() url.Values { return validChange("the-current-password", "short") }, http.StatusOK, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := &fakeNotices{}
			admins := &fakeAdmins{changePassword: tc.change}
			b := signedInBrowser(t, newNoticeAuth(t, admins, &fakeTrail{}, n))
			rec := b.do(http.MethodPost, accountPasswordHref, tc.form())
			if rec.Code != tc.code {
				t.Fatalf("POST answered %d, want %d", rec.Code, tc.code)
			}
			got := n.all()
			if len(got) != tc.notices {
				t.Fatalf("the notifier was told %d time(s), want %d", len(got), tc.notices)
			}
			for _, c := range got {
				if c.tenantID != panelTestTenant || c.adminUserID != panelTestAdmin || c.via != noticeViaAccount {
					t.Errorf("the notice names tenant/admin/via %s/%s/%q, want the session's %s/%s and %q",
						c.tenantID, c.adminUserID, c.via, panelTestTenant, panelTestAdmin, noticeViaAccount)
				}
			}
		})
	}

	// THE REAL NOTIFIER, FAILING AND PANICKING: the answer and the change's row stand.
	for _, tc := range []struct {
		name   string
		ch     ResetChannel
		reason string
	}{
		{"the relay refuses the notice", &recordingChannel{noticeErr: &mail.SendError{Class: mail.ClassRejected, SMTPCode: 550}}, noticeReasonSendFailed},
		{"the channel panics", &panicChannel{panicOn: map[int]bool{1: true}}, noticeReasonFault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			trail := &fakeTrail{}
			flow := newNoticeFlow(t, &fakeResets{recipients: map[uuid.UUID]string{panelTestAdmin: "owner@notice.example.test"}}, tc.ch, trail, nil)
			admins := &fakeAdmins{changePassword: func(_, _, _ uuid.UUID, _, _ string) (int, error) { return 2, nil }}
			b := signedInBrowser(t, newNoticeAuth(t, admins, trail, flow))
			rec := b.do(http.MethodPost, accountPasswordHref, validChange("the-current-password", "a-brand-new-password"))
			if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "done=password") {
				t.Fatalf("a committed change whose notice failed answered %d to %q, want 303 and the done flash", rec.Code, rec.Header().Get("Location"))
			}
			events := trail.eventsSnapshot()
			if len(events) != 2 || events[0].Action != ActionAdminPasswordChanged || events[1].Action != ActionAdminPasswordNoticeUndelivered {
				var actions []string
				for _, e := range events {
					actions = append(actions, e.Action)
				}
				t.Fatalf("the trail holds %q, want the change's row and then one undelivered notice row", actions)
			}
			if d, _ := events[1].Detail.(passwordNoticeDetail); d.Reason != tc.reason {
				t.Errorf("the notice row's reason is %q, want %q", d.Reason, tc.reason)
			}
		})
	}
}

// freshLinkValue is a recovery-link value of the shape adminauth mints (32 random bytes,
// unpadded base64url), drawn at run time so no such literal sits in this file.
func freshLinkValue(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("crypto/rand: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// TestAdminReset_ACompletedRecoveryNotifiesAndStillSignsIn — the recovery's half of
// EM-9: a spent link sends the notice for the link's administrator, AFTER the
// completed row, and the person still lands on the sign-in form; a failing notice
// changes neither; a refused link sends nothing, and neither does a spend that failed
// on our side (Submit's default branch: 500, nothing changed, nothing announced).
func TestAdminReset_ACompletedRecoveryNotifiesAndStillSignsIn(t *testing.T) {
	t.Parallel()
	linkValue := freshLinkValue(t)
	for _, tc := range []struct {
		name      string
		noticeErr error
		consume   error
		action    string // the notice row; "" means no notice row
		sent      int
	}{
		{"the notice is sent", nil, nil, ActionAdminPasswordNoticeSent, 1},
		{"the notice fails", &mail.SendError{Class: mail.ClassThrottled, SMTPCode: 451}, nil, ActionAdminPasswordNoticeUndelivered, 1},
		{"the link is refused", nil, adminauth.ErrResetUnusable, "", 0},
		{"spending the link fails on our side", nil, errors.New("db down"), "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tenantID, adminID := uuid.New(), uuid.New()
			resets := &fakeResets{
				consumed:   adminauth.ConsumedReset{ResetID: uuid.New(), TenantID: tenantID, AdminUserID: adminID, RevokedSessions: 1},
				consumeErr: tc.consume,
				recipients: map[uuid.UUID]string{adminID: "recovered@notice.example.test"},
			}
			if errors.Is(tc.consume, adminauth.ErrResetUnusable) {
				resets.consumeResolved.TenantID = tenantID
				resets.consumeResolved.AdminUserID = adminID
				resets.consumeResolved.ID = uuid.New()
			}
			ch := &recordingChannel{noticeErr: tc.noticeErr}
			trail := &fakeTrail{}
			router, _ := newResetRouter(t, resets, ch, trail)
			rec := submitLink(t, newBrowser(t, router), linkValue, "a-good-enough-password")
			switch {
			case tc.consume == nil:
				if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), adminLoginPath) {
					t.Fatalf("the recovery answered %d to %q, want 303 to the sign-in form", rec.Code, rec.Header().Get("Location"))
				}
			case !errors.Is(tc.consume, adminauth.ErrResetUnusable):
				if rec.Code != http.StatusInternalServerError {
					t.Fatalf("a spend that failed on our side answered %d, want 500 (the case this row is about)", rec.Code)
				}
			}
			got := ch.allNotices()
			if len(got) != tc.sent {
				t.Fatalf("the channel was handed %d notice(s), want %d", len(got), tc.sent)
			}
			for _, n := range got {
				if n.Recipient != "recovered@notice.example.test" || n.AdminUserID != adminID || n.TenantID != tenantID {
					t.Error("the notice is not for the link's administrator at the row's address")
				}
			}
			events := trail.eventsSnapshot()
			rows := noticeRows(events)
			if tc.action == "" {
				if len(rows) != 0 {
					t.Fatalf("%d notice row(s) for a link that changed nothing, want 0", len(rows))
				}
				return
			}
			if len(rows) != 1 || rows[0].Action != tc.action {
				t.Fatalf("%d notice row(s), want one %q", len(rows), tc.action)
			}
			if d, _ := rows[0].Detail.(passwordNoticeDetail); d.Via != noticeViaRecovery {
				t.Errorf("the notice row's via is %q, want %q", d.Via, noticeViaRecovery)
			}
			completedAt, noticeAt := -1, -1
			for i, e := range events {
				switch e.Action {
				case ActionAdminResetCompleted:
					completedAt = i
				case tc.action:
					noticeAt = i
				}
			}
			if completedAt < 0 || noticeAt < completedAt {
				t.Errorf("the completed row is at %d and the notice row at %d; the notice comes after the change's row", completedAt, noticeAt)
			}
		})
	}
}

// hostileHost rewrites every request's Host and forwarding headers to another place
// before it reaches h — the shape of a request a proxy or a client can send.
func hostileHost(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "evil.example"
		r.Header.Set("X-Forwarded-Host", "evil.example")
		r.Header.Set("X-Forwarded-Proto", "http")
		r.Header.Set("Forwarded", "host=evil.example;proto=http")
		h.ServeHTTP(w, r)
	})
}

// absoluteURLs finds every absolute or protocol-relative URL start and the run after it.
var absoluteURLs = regexp.MustCompile(`(?i)(?:\b[a-z][a-z0-9+.-]*:)?//[^\s"'<>]*`)

// TestPasswordNotice_GoesToTheRowsAddressWithTheConfiguredSignInLinkOnly — both paths,
// end to end through the REAL e-mail channel and SMTP transport to an in-test relay,
// with every request carrying a hostile Host, X-Forwarded-Host and Forwarded: each
// notice goes to the address ON THE ROW (a spelling no request carried), carries in
// each part exactly ONE URL — TAPPA_BASE_URL + "/admin/login" — and no reset token, no
// recovery or activation path, no '?' and nothing of the hostile host.
func TestPasswordNotice_GoesToTheRowsAddressWithTheConfiguredSignInLinkOnly(t *testing.T) {
	t.Parallel()
	linkValue := freshLinkValue(t)
	recoveredID := uuid.New()
	recovered := "Recovered.Owner@Row.Example.Test"
	accountOwner := "Account.Owner@Row.Example.Test"
	resets := &fakeResets{
		consumed: adminauth.ConsumedReset{ResetID: uuid.New(), TenantID: uuid.New(), AdminUserID: recoveredID},
		recipients: map[uuid.UUID]string{
			recoveredID:    recovered,
			panelTestAdmin: accountOwner,
		},
	}
	relay := newFakeRelay(t, relayScript{})
	trail := &fakeTrail{}
	var logged bytes.Buffer
	flow, _, _ := newEmailFlow(t, relay, resets, trail, &logged, nil, 5*time.Second)

	// The recovery path, through the reset routes.
	if rec := submitLink(t, newBrowser(t, hostileHost(mountReset(flow))), linkValue, "a-good-enough-password"); rec.Code != http.StatusSeeOther {
		t.Fatalf("the recovery answered %d, want 303", rec.Code)
	}
	// The Account path, through the panel's write chain.
	admins := &fakeAdmins{changePassword: func(_, _, _ uuid.UUID, _, _ string) (int, error) { return 0, nil }}
	b := signedInBrowser(t, hostileHost(newNoticeAuth(t, admins, trail, flow)))
	if rec := b.do(http.MethodPost, accountPasswordHref, validChange("the-current-password", "a-brand-new-password")); rec.Code != http.StatusSeeOther {
		t.Fatalf("the account change answered %d, want 303", rec.Code)
	}

	sessions := relay.completed()
	if len(sessions) != 2 {
		t.Fatalf("the relay received %d message(s), want 2 (one per path)", len(sessions))
	}
	signIn := testBaseURL + "/admin/login"
	wantTo := map[string]bool{recovered: true, accountOwner: true}
	for i, s := range sessions {
		if len(s.rcptTo) != 1 {
			t.Fatalf("message %d had %d recipient(s), want 1", i, len(s.rcptTo))
		}
		to := strings.TrimSuffix(strings.TrimPrefix(s.rcptTo[0], "RCPT TO:<"), ">")
		if !wantTo[to] {
			t.Errorf("message %d went to an address that is not on either row (or not in the row's spelling)", i)
		}
		delete(wantTo, to)
		m := parseRelayed(t, s.message)
		if got := m.header.Get("Subject"); got != "Your Taptime password was changed" {
			t.Errorf("message %d: Subject %q, want the fixed one", i, got)
		}
		for part, body := range map[string]string{"text": m.text, "html": m.html} {
			urls := absoluteURLs.FindAllString(body, -1)
			if len(urls) != 1 || strings.TrimRight(urls[0], ".") != signIn {
				t.Errorf("message %d, %s part: %d URL(s), want exactly the configured sign-in page", i, part, len(urls))
			}
			for _, bad := range []string{"evil.example", linkValue, "?t=", "/admin/reset", "/activate", "code="} {
				if strings.Contains(body, bad) {
					t.Errorf("message %d, %s part carries %q", i, part, bad)
				}
			}
			if strings.Contains(strings.ToLower(body), "row.example.test") {
				t.Errorf("message %d, %s part carries the recipient's address in its words", i, part)
			}
		}
	}
	if len(wantTo) != 0 {
		t.Errorf("%d row address(es) got no notice", len(wantTo))
	}
	if n := trail.count(ActionAdminPasswordNoticeSent); n != 2 {
		t.Errorf("%d sent notice row(s), want 2", n)
	}
}

// TestPasswordNotice_LogsOnlyIdsClassAndCode — ADR 0022 §10 on the notice, against a
// relay that does what relays do: quotes the recipient, the link and the credentials
// in its refusals, throttles, refuses the credentials, offers no TLS, never greets.
// Every line's keys are in the closed set, and no line carries the address (any case),
// the SMTP credentials (or any 8-character run of either) or the relay's words; each
// change ends in one row with the class and code. CONTROL per row: the reply really
// carries what is scanned for.
func TestPasswordNotice_LogsOnlyIdsClassAndCode(t *testing.T) {
	t.Parallel()
	const who = "notice-leak-probe@relay.example.test"
	signIn := testBaseURL + "/admin/login"
	quoted := "5.1.1 <" + strings.ToUpper(who) + "> refused while sending " + signIn + " " +
		base64.StdEncoding.EncodeToString([]byte(who))
	for _, tc := range []struct {
		name    string
		script  relayScript
		timeout time.Duration
		action  string
		class   mail.Class
		code    int
		quotes  bool
	}{
		{"accepted", relayScript{}, 5 * time.Second, ActionAdminPasswordNoticeSent, "", 0, false},
		{"recipient refused, quoting it", relayScript{rcptReply: "550 " + quoted}, 5 * time.Second, ActionAdminPasswordNoticeUndelivered, mail.ClassRejected, 550, true},
		{"refused at the end of data", relayScript{endReply: "554 " + quoted}, 5 * time.Second, ActionAdminPasswordNoticeUndelivered, mail.ClassRejected, 554, true},
		{"throttled twice", relayScript{rcptReply: "451 4.3.0 " + quoted}, 5 * time.Second, ActionAdminPasswordNoticeUndelivered, mail.ClassThrottled, 451, true},
		{"credentials refused", relayScript{authReply: "535 5.7.8 " + quoted}, 5 * time.Second, ActionAdminPasswordNoticeUndelivered, mail.ClassAuth, 535, true},
		{"no STARTTLS", relayScript{noSTARTTLS: true}, 5 * time.Second, ActionAdminPasswordNoticeUndelivered, mail.ClassTLS, 0, false},
		{"never greets", relayScript{hang: "greeting"}, 500 * time.Millisecond, ActionAdminPasswordNoticeUndelivered, mail.ClassTimeout, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.quotes && !strings.Contains(strings.ToLower(tc.script.rcptReply+tc.script.endReply+tc.script.authReply), who) {
				t.Fatal("CONTROL FAILED: the relay's reply does not carry the address")
			}
			adminID, tenantID := uuid.New(), uuid.New()
			relay := newFakeRelay(t, tc.script)
			trail := &fakeTrail{}
			var logged, asJSON bytes.Buffer
			flow, user, pass := newEmailFlow(t, relay, &fakeResets{recipients: map[uuid.UUID]string{adminID: who}}, trail, &logged, &asJSON, tc.timeout)
			changeOnce(t, flow, passwordChange{tenantID: tenantID, adminUserID: adminID, via: noticeViaAccount})

			if asJSON.Len() == 0 {
				t.Fatal("CONTROL FAILED: nothing was logged, so the key check read nothing")
			}
			if off := noticeKeysOffTheSet(t, asJSON.String()); len(off) > 0 {
				t.Errorf("log keys outside the closed set: %q", off)
			}
			rows := noticeRows(trail.eventsSnapshot())
			if len(rows) != 1 || rows[0].Action != tc.action {
				t.Fatalf("%d notice row(s), want one %q", len(rows), tc.action)
			}
			d, _ := rows[0].Detail.(passwordNoticeDetail)
			if d.Class != string(tc.class) || d.SMTPCode != tc.code {
				t.Errorf("the row says class=%q code=%d, want %q %d", d.Class, d.SMTPCode, tc.class, tc.code)
			}
			got := logged.String()
			if tc.action == ActionAdminPasswordNoticeUndelivered {
				for _, want := range []string{"class=" + string(tc.class), "smtp_code=" + strconv.Itoa(tc.code), "err_type=*mail.SendError"} {
					if !strings.Contains(got, want) {
						t.Errorf("the failure line lacks %s", want)
					}
				}
			} else if !strings.Contains(got, "message_id="+relayMessageID) {
				t.Error("the accepted line does not carry the relay's message id")
			}
			secrets := map[string]string{"username": user, "password": pass, "relay text": "refused while sending",
				"base64 address": base64.StdEncoding.EncodeToString([]byte(who))}
			for _, cred := range []string{user, pass} {
				for i := 0; i+8 <= len(cred); i++ {
					secrets["credential window "+strconv.Itoa(i)] = cred[i : i+8]
				}
			}
			if strings.Contains(strings.ToLower(got), strings.ToLower(who)) {
				t.Error("the recipient reached the log")
			}
			for what, s := range secrets {
				if strings.Contains(got, s) {
					t.Errorf("the %s reached the log", what)
				}
			}
		})
	}
}

// TestPasswordNotice_AnAddressTheRelayCannotTakeIsNeverDialled — an address stored on a
// row that the SENDING rule refuses (the stored rule is weaker — ADR 0022 B14): a
// non-ASCII local part, and a CR LF that would forge a header. Each ends undelivered
// with class invalid_address, the relay is never dialled, and the address is not
// logged.
func TestPasswordNotice_AnAddressTheRelayCannotTakeIsNeverDialled(t *testing.T) {
	t.Parallel()
	for name, stored := range map[string]string{
		"non-ASCII": "ćali@relay.example.test",
		"CR LF":     "ali@relay.example.test\r\nBcc: x@evil.example",
		"two":       "ali@relay.example.test, x@evil.example",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			adminID := uuid.New()
			relay := newFakeRelay(t, relayScript{})
			trail := &fakeTrail{}
			var logged bytes.Buffer
			flow, _, _ := newEmailFlow(t, relay, &fakeResets{recipients: map[uuid.UUID]string{adminID: stored}}, trail, &logged, nil, time.Second)
			changeOnce(t, flow, passwordChange{tenantID: uuid.New(), adminUserID: adminID, via: noticeViaAccount})
			if n := len(relay.completed()); n != 0 {
				t.Errorf("the relay was dialled %d time(s) for an address the sending rule refuses", n)
			}
			rows := noticeRows(trail.eventsSnapshot())
			if len(rows) != 1 || rows[0].Action != ActionAdminPasswordNoticeUndelivered {
				t.Fatalf("%d notice row(s), want one undelivered", len(rows))
			}
			if d, _ := rows[0].Detail.(passwordNoticeDetail); d.Class != string(mail.ClassInvalidAddress) {
				t.Errorf("class %q, want %s", d.Class, mail.ClassInvalidAddress)
			}
			if got := logged.String(); strings.Contains(got, "relay.example.test") || strings.Contains(got, "evil.example") {
				t.Error("the stored address reached the log")
			}
		})
	}
}

// TestPasswordNotice_AFullResetOutboxDoesNotStopIt — the decision passwordnotice.go
// argues: the notice does not go through the reset outbox, so anonymous traffic that
// fills that outbox cannot keep it from the account holder. CONTROL: the outbox really
// is full — one more reset grant is recorded undelivered with the full-queue reason.
func TestPasswordNotice_AFullResetOutboxDoesNotStopIt(t *testing.T) {
	t.Parallel()
	gate := newGateChannel()
	adminID := uuid.New()
	first, fill, late := grantsFor("first@x.test", 1), grantsFor("fill@x.test", resetOutboxSize), grantsFor("late@x.test", 1)
	resets := &fakeResets{
		grantsFor:  map[string][]adminauth.ResetGrant{"first@x.test": first, "fill@x.test": fill, "late@x.test": late},
		recipients: map[uuid.UUID]string{adminID: "owner@notice.example.test"},
	}
	trail := &fakeTrail{}
	router, h := newResetRouter(t, resets, gate, trail)
	t.Cleanup(func() { close(gate.release) })
	requestReset(t, newBrowser(t, router), "first@x.test")
	gate.waitStarted(t) // the worker is inside a send; the outbox is empty
	requestReset(t, newBrowser(t, router), "fill@x.test")
	requestReset(t, newBrowser(t, router), "late@x.test")
	full := 0
	for _, e := range trail.eventsSnapshot() {
		if d, ok := e.Detail.(adminResetDetail); ok && d.Reason == resetReasonOutboxFull {
			full++
		}
	}
	if full != 1 {
		t.Fatalf("CONTROL FAILED: %d grant(s) recorded as refused by a full outbox, want 1", full)
	}

	changeOnce(t, h, passwordChange{tenantID: uuid.New(), adminUserID: adminID, via: noticeViaAccount})
	gate.mu.Lock()
	notices := gate.notices
	gate.mu.Unlock()
	if notices != 1 {
		t.Fatalf("the channel received %d notice(s) while the outbox was full, want 1", notices)
	}
	if n := trail.count(ActionAdminPasswordNoticeSent); n != 1 {
		t.Errorf("%d sent notice row(s), want 1", n)
	}
}

// TestPasswordNotice_ThePerAccountCapIsExactUnderConcurrentChanges — many changes of
// ONE account at once (every call built before a start line, 200 runs): exactly
// passwordNoticeLimit notices are SENT, every other change has its undelivered row
// with the cap's reason, the rate-limited line is written exactly once, and another
// account is untouched.
func TestPasswordNotice_ThePerAccountCapIsExactUnderConcurrentChanges(t *testing.T) {
	t.Parallel()
	const callers, runs = 40, 200
	for run := 0; run < runs; run++ {
		adminID, other := uuid.New(), uuid.New()
		ch := &recordingChannel{}
		trail := &fakeTrail{}
		var logged bytes.Buffer
		h := newNoticeFlow(t, &fakeResets{recipients: map[uuid.UUID]string{adminID: "a@x.test", other: "b@x.test"}}, ch, trail, &logged)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				h.passwordChanged(context.Background(), passwordChange{tenantID: uuid.Nil, adminUserID: adminID, via: noticeViaAccount})
			}()
		}
		close(start)
		wg.Wait()
		if n := len(ch.allNotices()); n != passwordNoticeLimit {
			t.Fatalf("run %d: %d notice(s) sent for one account, want exactly %d", run, n, passwordNoticeLimit)
		}
		limited := 0
		for _, e := range noticeRows(trail.eventsSnapshot()) {
			if d, _ := e.Detail.(passwordNoticeDetail); d.Reason == noticeReasonLimited {
				limited++
			}
		}
		if trail.total() != callers || limited != callers-passwordNoticeLimit {
			t.Fatalf("run %d: %d row(s), %d of them capped; want %d and %d", run, trail.total(), limited, callers, callers-passwordNoticeLimit)
		}
		if n := strings.Count(logged.String(), `"msg":"panel credential notice rate limited"`); n != 1 {
			t.Fatalf("run %d: %d rate-limited line(s), want exactly 1", run, n)
		}
		h.passwordChanged(context.Background(), passwordChange{tenantID: uuid.Nil, adminUserID: other, via: noticeViaAccount})
		if n := len(ch.allNotices()); n != passwordNoticeLimit+1 {
			t.Fatalf("run %d: another account's notice was not sent", run)
		}
	}
}

// newEmailNoticeFlow is the recovery flow over the REAL e-mail channel and transport to
// relay, with any audit recorder (newEmailFlow takes only a *fakeTrail).
func newEmailNoticeFlow(t *testing.T, relay *fakeRelay, resets panelResets, trail auditRecorder, timeout time.Duration) *AdminReset {
	t.Helper()
	user, pass := relayCredentials(t)
	ch, err := NewEmailResetChannel(relay.breaker(t, user, pass, timeout), adminTestConfig().BaseURL, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewEmailResetChannel: %v", err)
	}
	return newNoticeFlow(t, resets, ch, trail, nil)
}

// TestPasswordNotice_AVisitorWhoLeavesStillGetsTheNotice — the request's context is
// already cancelled when the notice starts (the browser closed after the change
// committed): the notice is still sent through the real transport and its row is still
// written, on a trail that refuses an ended context as Postgres does.
func TestPasswordNotice_AVisitorWhoLeavesStillGetsTheNotice(t *testing.T) {
	t.Parallel()
	adminID := uuid.New()
	relay := newFakeRelay(t, relayScript{})
	trail := &ctxTrail{}
	flow := newEmailNoticeFlow(t, relay, &fakeResets{recipients: map[uuid.UUID]string{adminID: "left@relay.example.test"}}, trail, 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	flow.passwordChanged(ctx, passwordChange{tenantID: uuid.New(), adminUserID: adminID, via: noticeViaAccount})
	if n := len(relay.completed()); n != 1 {
		t.Errorf("the relay received %d message(s) after the visitor left, want 1", n)
	}
	if n := trail.count(ActionAdminPasswordNoticeSent); n != 1 {
		t.Errorf("%d sent notice row(s), want 1", n)
	}
}

// TestPasswordNotice_AHungRelayHoldsTheAnswerOnlyForTheSendGrace — a relay that accepts
// the connection and never answers, behind a transport whose own timeout is far longer:
// the notice gives up at ITS grace, records undelivered (timeout) on the row's OWN
// context — the trail refuses an ended context, as Postgres does, so a row written on
// the spent send context would be lost — and returns. The grace is lowered for the test
// (the field exists for that); the shipped value is held by
// TestPasswordNotice_TheShippedClocksAndCapAreWired and cmd/tappa's budget test.
func TestPasswordNotice_AHungRelayHoldsTheAnswerOnlyForTheSendGrace(t *testing.T) {
	t.Parallel()
	const grace = 300 * time.Millisecond
	adminID := uuid.New()
	relay := newFakeRelay(t, relayScript{hang: "greeting"})
	trail := &ctxTrail{}
	flow := newEmailNoticeFlow(t, relay, &fakeResets{recipients: map[uuid.UUID]string{adminID: "hung@relay.example.test"}}, trail, 30*time.Second)
	flow.noticeSendGrace = grace

	done := make(chan time.Duration, 1)
	go func() {
		began := time.Now()
		flow.passwordChanged(context.Background(), passwordChange{tenantID: uuid.New(), adminUserID: adminID, via: noticeViaAccount})
		done <- time.Since(began)
	}()
	select {
	case took := <-done:
		t.Logf("the notice returned after %v (grace %v, transport timeout 30s)", took, grace)
		if took < grace {
			t.Errorf("returned after %v, before its own grace of %v: the relay was not what stopped it", took, grace)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the notice held the change's answer for more than 5 s against a grace of 300 ms")
	}
	rows := noticeRows(trail.eventsSnapshot())
	if len(rows) != 1 || rows[0].Action != ActionAdminPasswordNoticeUndelivered {
		t.Fatalf("%d notice row(s), want one undelivered", len(rows))
	}
	if d, _ := rows[0].Detail.(passwordNoticeDetail); d.Class != string(mail.ClassTimeout) {
		t.Errorf("class %q, want %s", d.Class, mail.ClassTimeout)
	}
}

// stuckRecipients is fakeResets whose address read never answers on its own: it
// returns only when its context ends (as a pgx query does) or when the test releases it.
type stuckRecipients struct {
	*fakeResets
	release chan struct{}
}

func (s *stuckRecipients) NoticeRecipient(ctx context.Context, _, _ uuid.UUID) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-s.release:
		return "", errors.New("released by the test")
	}
}

// stuckTrail is an audit recorder whose write never answers on its own: it returns only
// when its context ends (as a database round trip does) or when the test releases it.
type stuckTrail struct {
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (s *stuckTrail) Record(ctx context.Context, _ audit.Event) (uuid.UUID, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return uuid.Nil, ctx.Err()
	case <-s.release:
		return uuid.Nil, errors.New("released by the test")
	}
}

// timeNotice runs passwordChanged and returns how long it took, failing the test if it
// has not returned within limit.
func timeNotice(t *testing.T, h *AdminReset, limit time.Duration) time.Duration {
	t.Helper()
	done := make(chan time.Duration, 1)
	go func() {
		began := time.Now()
		h.passwordChanged(context.Background(), passwordChange{tenantID: uuid.New(), adminUserID: uuid.New(), via: noticeViaAccount})
		done <- time.Since(began)
	}()
	select {
	case took := <-done:
		return took
	case <-time.After(limit):
		t.Fatalf("the notice held the change's answer for more than %v: a step has no bound of its own", limit)
		return 0
	}
}

// TestPasswordNotice_EachStepHasItsOwnBound — the two steps around the send are bounded
// by the clocks the code says bound them, measured against steps that never answer:
//   - the ADDRESS READ is inside PasswordNoticeSendGrace (the send grace is lowered to
//     300 ms for the test; the read gives up there, the row says the address could not
//     be read, and nothing reaches the channel);
//   - the ROW has its own PasswordNoticeRecordGrace (the shipped 5 s, not lowered: the
//     write is abandoned at the grace, and only one write is attempted).
//
// Each fake answers its context like a database round trip; the test releases both at
// cleanup, so a mutation that removes a bound fails here at the watchdog instead of
// hanging the package.
func TestPasswordNotice_EachStepHasItsOwnBound(t *testing.T) {
	t.Parallel()
	t.Run("the address read is inside the send grace", func(t *testing.T) {
		t.Parallel()
		const grace = 300 * time.Millisecond
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		ch := &recordingChannel{}
		trail := &fakeTrail{}
		h := newNoticeFlow(t, &stuckRecipients{fakeResets: &fakeResets{}, release: release}, ch, trail, nil)
		h.noticeSendGrace = grace
		took := timeNotice(t, h, 5*time.Second)
		t.Logf("a stuck address read: the notice returned after %v (send grace %v)", took, grace)
		if took < grace {
			t.Errorf("returned after %v, before the grace of %v", took, grace)
		}
		rows := noticeRows(trail.eventsSnapshot())
		if len(rows) != 1 {
			t.Fatalf("%d notice row(s), want 1", len(rows))
		}
		if d, _ := rows[0].Detail.(passwordNoticeDetail); d.Reason != noticeReasonReadFailed {
			t.Errorf("the row's reason is %q, want the read-failed reason", d.Reason)
		}
		if n := len(ch.allNotices()); n != 0 {
			t.Errorf("the channel was handed %d notice(s) without an address", n)
		}
	})
	t.Run("the row has its own grace", func(t *testing.T) {
		t.Parallel()
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		trail := &stuckTrail{release: release}
		adminID := uuid.New()
		h := newNoticeFlow(t, &fakeResets{recipients: map[uuid.UUID]string{adminID: "a@x.test"}}, &recordingChannel{}, trail, nil)
		done := make(chan time.Duration, 1)
		go func() {
			began := time.Now()
			h.passwordChanged(context.Background(), passwordChange{tenantID: uuid.New(), adminUserID: adminID, via: noticeViaAccount})
			done <- time.Since(began)
		}()
		select {
		case took := <-done:
			t.Logf("a stuck row write: the notice returned after %v (record grace %v)", took, PasswordNoticeRecordGrace)
			if took < PasswordNoticeRecordGrace {
				t.Errorf("returned after %v, before the record grace of %v", took, PasswordNoticeRecordGrace)
			}
		case <-time.After(PasswordNoticeRecordGrace + 3*time.Second):
			t.Fatalf("the notice held the change's answer for more than %v against a record grace of %v",
				PasswordNoticeRecordGrace+3*time.Second, PasswordNoticeRecordGrace)
		}
		trail.mu.Lock()
		calls := trail.calls
		trail.mu.Unlock()
		if calls != 1 {
			t.Errorf("the recorder was asked for %d write(s), want 1", calls)
		}
	})
}

// TestPasswordNotice_TheShippedClocksAndCapAreWired — the cap and the two clocks are
// the numbers ADR 0022's "EM-9 note" argues (five sends an hour per account; ten
// seconds to send, five to record), held THREE ways, because the first version only
// compared the flow's limiter with the same constants and so agreed with any value
// (an audit moved the window to one second and the cap to thirty, and it stayed
// green):
//   - each constant equals its literal here;
//   - the ADR states the same numbers, read from the ADR and formatted from the
//     constants — moving a number means moving the sentence that argues it;
//   - NewAdminReset gives every flow those values (the two fields a test may lower).
//
// And the cap leaves room for the ordinary sequence: a recovery, then a change from the
// Account section, both announced.
func TestPasswordNotice_TheShippedClocksAndCapAreWired(t *testing.T) {
	t.Parallel()
	if passwordNoticeLimit != 5 || passwordNoticePeriod != time.Hour {
		t.Errorf("the cap is %d per %v; the argued value is 5 per hour (ADR 0022, EM-9 note, decision 3) — "+
			"change it there first", passwordNoticeLimit, passwordNoticePeriod)
	}
	if PasswordNoticeSendGrace != 10*time.Second || PasswordNoticeRecordGrace != 5*time.Second {
		t.Errorf("the clocks are %v to send and %v to record; the argued values are 10 s and 5 s "+
			"(ADR 0022, EM-9 note, decision 2) — change them there first", PasswordNoticeSendGrace, PasswordNoticeRecordGrace)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "adr", "0022-islemsel-eposta.md"))
	if err != nil {
		t.Fatalf("reading ADR 0022: %v", err)
	}
	for _, want := range []string{
		// Built with strconv, not fmt: redline R7 reads a fmt call naming these
		// constants as a call that may print a credential.
		"Hesap başına tavan: saatte " + strconv.Itoa(passwordNoticeLimit) + " gönderim",
		"`PasswordNoticeSendGrace` (" + strconv.Itoa(int(PasswordNoticeSendGrace/time.Second)) + " s,",
		"`PasswordNoticeRecordGrace` (" + strconv.Itoa(int(PasswordNoticeRecordGrace/time.Second)) + " s)",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("ADR 0022 does not say %q: the constant and the sentence that argues it have parted", want)
		}
	}
	h := newNoticeFlow(t, &fakeResets{}, &recordingChannel{}, &fakeTrail{}, nil)
	if h.noticeSendGrace != PasswordNoticeSendGrace {
		t.Errorf("the flow's send grace is %v, want PasswordNoticeSendGrace (%v)", h.noticeSendGrace, PasswordNoticeSendGrace)
	}
	if h.noticeLimiter.Limit() != passwordNoticeLimit || h.noticeLimiter.Period() != passwordNoticePeriod {
		t.Errorf("the flow's cap is %d per %v, want %d per %v", h.noticeLimiter.Limit(), h.noticeLimiter.Period(), passwordNoticeLimit, passwordNoticePeriod)
	}
	if passwordNoticeLimit < 2 {
		t.Errorf("a cap of %d would withhold the second of a recovery and a change made right after it", passwordNoticeLimit)
	}
}

// TestPasswordNotice_TheNotifierIsRequired: NewAdminAuth refuses a nil notifier, untyped
// and typed — a panel built without one would change passwords that nobody is told
// about and that leave no notice row (the M5-04 shape).
func TestPasswordNotice_TheNotifierIsRequired(t *testing.T) {
	t.Parallel()
	records := newFakeLedger()
	build := func(n passwordNotifier) error {
		_, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
			&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
			newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, n, adminTestConfig(), discardLogger())
		return err
	}
	for name, n := range map[string]passwordNotifier{"nil": nil, "typed nil": (*AdminReset)(nil)} {
		if build(n) == nil {
			t.Errorf("a %s notifier was accepted", name)
		}
	}
	if err := build(&fakeNotices{}); err != nil {
		t.Fatalf("PREMISE: the same call with a notifier fails: %v", err)
	}
}
