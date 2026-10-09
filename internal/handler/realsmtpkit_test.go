package handler

// realsmtpkit_test.go — M10 EM-5B's measuring kit (ADR 0022 "EM-5A notu" → EM-5B; the
// m10 card's "EM-5B — kalan ve neden kaldığı"). Two tools live here, and two
// tag-gated entry points in realsmtp_test.go hand them the environment:
//
//   - THE SENDER (em5bSend) sends the product's four e-mails — the recovery link, the
//     "your password was changed" notice, and the invitation for a VIES-verified and
//     for an unverified business — to one address, then one recovery link through a
//     transport with a WRONG password (the relay's refusal, measured as a class and a
//     code). It prints, per message, only its kind, sent or refused, the relay's
//     message id and the class and reply code.
//   - THE CHECKER (em5bCheck) reads the source of ONE received e-mail (the receiver's
//     Authentication-Results included) and holds it to M7-04's and EM-5B's criteria:
//     a table of criterion, expected, found and PASS / FAIL / INFO / N/A, then a
//     verdict line (FAIL over INCOMPLETE over PASS) and its diagnoses.
//
// 🔴 WHY THE KIT IS NOT BEHIND THE BUILD TAG. A file behind a tag is compiled only by
// whoever passes the tag — CI's go vet, staticcheck and go test never see it — so a
// renamed constructor would rot it silently until the one day it is needed. Everything
// here is compiled on every run and driven against the in-test relay
// (fakesmtp_test.go, loopback, no network) by the TestEM5BKit_ tests below; the tagged
// file holds only os.Getenv and the two calls.
//
// 🔴 WHY THROUGH THE PRODUCT'S CHANNELS, NOT mail.Send ALONE. EM-5B measures what an
// "email" deployment SENDS, and that is decided above the transport: the recovery link
// goes through NewEmailResetChannel's DeliverReset (refusable by the breaker) with the
// lifetime it states computed from the link's expiry, the notice through its
// DeliverPasswordNotice (SendExempt), the invitation through the panel route's sink
// over NewEmailInvitations — counted under the business by the ONE RecipientCap around
// the ONE Breaker around mail.New: the shape cmd/tappa's run() builds. So To, the Ref
// that travels nowhere, the subject, the stated lifetime and both ceilings are the
// product's. NOT COVERED: the outbox worker and the request paths that call these
// channels (they need a database; their tests measure them against the in-test relay),
// and the panel's own sentences.
//
// FIXTURES, NOT CREDENTIALS. The link values are plain words in the renderer's
// alphabet and activate nothing; the base is the production TAPPA_BASE_URL, so the
// received links are exactly the shape a real e-mail carries. Two name pairs: the
// VERIFIED pair the invitation must show (the positive control of the name gate), and
// a CANARY pair handed ONLY to the unverified render — it must appear in no received
// e-mail (ADR 0022 counted limit 22, the user's decision of 2026-10-09).
//
// WHAT IS NEVER PRINTED, BY EITHER TOOL: the recipient address, the SMTP username and
// password (nor the probe's), and any body. The checker's "found" column carries only
// counts, our own constants, hosts, ids and dates; TestEM5BKit_TheCheckerPassesWhatTheProductSends
// and TestEM5BKit_TheCheckerFailsEachBrokenCriterion hold every table they build free
// of the recipient and the credentials.
//
// THE CLAIM, IN THREE PARTS. Threat model: these pins hold against ACCIDENTAL drift in
// this kit and in the channels and renderers it calls; a source crafted to get past the
// checker is out of scope — it reads what a real receiver wrote.
//
// PART I — MEASURED HERE (in-test relay, loopback):
//   - the four fixtures go out through the production chain, each to the given address
//     with the relay's message id; the wrong-password chain ends as class auth, code
//     535; the channels' lines hold only ADR 0022 §10's keys and none of the address,
//     a link, a fixture name or a credential (nor an 8-character run of one) —
//     TestEM5BKit_SendsEveryMessageThroughTheProductionChain (the scan's own control
//     is in the same test);
//   - every criterion PASSes (or is INFO) on the four messages exactly as the product
//     composed them, under a receiver header in Outlook's shape and in Gmail's
//     (header.i only), and with SES's Message-ID in place of ours —
//     TestEM5BKit_TheCheckerPassesWhatTheProductSends;
//   - each criterion FAILs on a source broken in its own way, and ONLY the criteria
//     that source breaks fail; SES's own DKIM signature is recorded (INFO), not judged;
//     and the two shapes the real run met (EM-5B round 2, a mail.tm inbox): a source
//     with NO Authentication-Results gives N/A — never PASS, never FAIL — on spf, dkim
//     taptime.mt and dmarc, an INCOMPLETE verdict and a red entry point, and a source
//     whose export moved the CRLF after the first boundary before it fails the rows
//     that read the parts and prints the MIME diagnosis (no repair: the checker never
//     edits what it judges); a canary name ADDED to an invitation with its other
//     words intact (business or person, unverified or verified) fails names on its
//     own; a source checked without the sender's 250 id gives N/A on that row and an
//     INCOMPLETE, red verdict (round 4) — TestEM5BKit_TheCheckerFailsEachBrokenCriterion;
//   - a missing setting fails instead of skipping, and no problem quotes a value —
//     TestEM5BKit_ReadsItsSettingsFailClosed.
//
// PART II — NOT MEASURED HERE, BY CONSTRUCTION: the real relay (SES) — that is the
// tagged entry points' run, which the orchestrator makes; what a receiver writes into
// Authentication-Results beyond the two shapes above; e-mail clients' rendering.
//
// PART III — Any form not listed above is the subject of code review — no
// completeness claim.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/textproto"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/mail"
)

// The deployment the measurement is about (ADR 0022 §1: K5, K6, K7; deploy/k8s/05-config.yaml).
const (
	em5bBaseURL        = "https://taptime.mt"
	em5bFrom           = "Taptime <no-reply@taptime.mt>"
	em5bFromDomain     = "taptime.mt"
	em5bMailFromDomain = "mail.taptime.mt"
	em5bSESDomain      = "amazonses.com"
	em5bRegion         = "eu-central-1"
	em5bDefaultPort    = 587
	em5bImplicitTLS    = 465
)

// The fixtures. Each link value is unique to its message, so a received source shows
// which send it came from; none is 43 characters long, the shape of a real code or
// recovery link value, which the checker's last criterion looks for.
const (
	em5bResetValue      = "fixture-reset-link-em-five-b"
	em5bVerifiedValue   = "fixture-invite-verified-em-five-b"
	em5bUnverifiedValue = "fixture-invite-unverified-em-five-b"
	em5bVerifiedTenant  = "Fixture Verified Kitchen Ltd"
	em5bVerifiedPerson  = "Fixture Verified Person"
	em5bCanaryTenant    = "Canary Unverified Bistro"
	em5bCanaryPerson    = "Canary Unverified Person"
	// em5bInviteLifetime is internal/invite's default lifetime (7 days), the span
	// the invitation e-mail states.
	em5bInviteLifetime = 7 * 24 * time.Hour
	// em5bProbeKind is the wrong-password send's label in the sender's output.
	em5bProbeKind = "auth-probe"
)

// The settings the two entry points read. The first six are the deployment's own
// names (ADR 0022 §5); the last four are this measurement's.
const (
	em5bEnvHost    = "TAPPA_SMTP_HOST"
	em5bEnvPort    = "TAPPA_SMTP_PORT"
	em5bEnvUser    = "TAPPA_SMTP_USERNAME"
	em5bEnvPass    = "TAPPA_SMTP_PASSWORD"
	em5bEnvFrom    = "TAPPA_MAIL_FROM"
	em5bEnvReplyTo = "TAPPA_MAIL_REPLY_TO"
	em5bEnvTo      = "TAPPA_REALSMTP_TO"
	em5bEnvSource  = "TAPPA_REALSMTP_EML"
	em5bEnvKind    = "TAPPA_REALSMTP_KIND"
	em5bEnvSESID   = "TAPPA_REALSMTP_SES_ID"
)

// em5bKind names one of the four messages.
type em5bKind string

const (
	em5bReset            em5bKind = "reset"
	em5bNotice           em5bKind = "notice"
	em5bInviteVerified   em5bKind = "invite-verified"
	em5bInviteUnverified em5bKind = "invite-unverified"
)

var em5bKinds = []em5bKind{em5bReset, em5bNotice, em5bInviteVerified, em5bInviteUnverified}

// em5bWanted is what one kind's received e-mail must say.
type em5bWanted struct {
	subject string
	link    string // the e-mail's one absolute URL, exactly
	value   string // the link's parameter value; "" for the notice, whose link has none
	// shown must appear in BOTH parts: the verified names in their sentences, or the
	// neutral words standing in for them.
	shown []string
}

func em5bExpect(k em5bKind) (em5bWanted, bool) {
	switch k {
	case em5bReset:
		return em5bWanted{subject: "Reset your Taptime password",
			link: em5bBaseURL + adminResetNewPath + "?t=" + em5bResetValue, value: em5bResetValue}, true
	case em5bNotice:
		return em5bWanted{subject: "Your Taptime password was changed", link: em5bBaseURL + adminLoginPath}, true
	case em5bInviteVerified:
		return em5bWanted{subject: "Your Taptime invitation",
			link: em5bBaseURL + "/activate?code=" + em5bVerifiedValue, value: em5bVerifiedValue,
			shown: []string{"Hello " + em5bVerifiedPerson + ",", em5bVerifiedTenant + " has invited you to Taptime"}}, true
	case em5bInviteUnverified:
		return em5bWanted{subject: "Your Taptime invitation",
			link: em5bBaseURL + "/activate?code=" + em5bUnverifiedValue, value: em5bUnverifiedValue,
			shown: []string{"Hello,", "Your employer has invited you to Taptime"}}, true
	}
	return em5bWanted{}, false
}

// ---------------------------------------------------------------------------------
// The sender.
// ---------------------------------------------------------------------------------

// em5bSendSettings is what the sender needs. user and pass are kept beside cfg only so
// the log scan can look for them; they are never printed.
type em5bSendSettings struct {
	cfg        mail.Config
	to         string
	user, pass string
}

// em5bSendEnv reads the sender's settings from getenv. EVERY MISSING SETTING IS A
// PROBLEM, NOT A SKIP: an empty run must not look like a green one. A problem names its
// variable and never its value.
func em5bSendEnv(getenv func(string) string) (em5bSendSettings, []string) {
	var problems []string
	need := func(name string) string {
		v := getenv(name)
		if strings.TrimSpace(v) == "" {
			problems = append(problems, name+" is not set")
		}
		return v
	}
	s := em5bSendSettings{}
	host := need(em5bEnvHost)
	s.user = need(em5bEnvUser)
	s.pass = need(em5bEnvPass)
	from := need(em5bEnvFrom)
	s.to = need(em5bEnvTo)
	if s.to != "" && !mail.ValidRecipient(s.to) {
		problems = append(problems, em5bEnvTo+" is not one bare address (the transport's recipient rule)")
	}
	port := em5bDefaultPort
	if p := strings.TrimSpace(getenv(em5bEnvPort)); p != "" {
		n, err := strconv.Atoi(p)
		switch {
		case err != nil || n < 1 || n > 65535:
			problems = append(problems, em5bEnvPort+" is not a port number")
		case n == em5bImplicitTLS:
			problems = append(problems, em5bEnvPort+" is 465, implicit TLS; the transport speaks STARTTLS (587)")
		default:
			port = n
		}
	}
	s.cfg = mail.Config{
		Host:     host,
		Port:     port,
		Username: mail.NewCredential(s.user),
		Password: mail.NewCredential(s.pass),
		From:     from,
		ReplyTo:  getenv(em5bEnvReplyTo),
	}
	return s, problems
}

// em5bProbeConfig is cfg with a password nobody holds: drawn here, used once, never
// printed. The relay refuses it at AUTH, so nothing is sent.
func em5bProbeConfig(t *testing.T, cfg mail.Config) (mail.Config, string) {
	t.Helper()
	b := make([]byte, 30)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	wrong := "em5b-wrong-" + base64.RawURLEncoding.EncodeToString(b)
	probe := cfg
	probe.Password = mail.NewCredential(wrong)
	return probe, wrong
}

// em5bOutcome is one send's printable result: never the address, the message or a
// credential.
type em5bOutcome struct {
	kind      string
	sent      bool
	messageID string
	class     string
	code      int
	errType   string
}

func em5bOutcomeOf(kind string, err error) em5bOutcome {
	o := em5bOutcome{kind: kind, sent: err == nil}
	if err == nil {
		return o
	}
	if class, code, ok := sendErrorClass(err); ok {
		o.class, o.code = class, code
		return o
	}
	// Not a *mail.SendError: a render refusal (a sentinel that quotes no value) or a
	// programmer error. Its Go type, like the outbox's err_type.
	o.errType = fmt.Sprintf("%T", err)
	return o
}

// line is the outcome as the sender prints it.
func (o em5bOutcome) line() string {
	switch {
	case o.sent && o.messageID == "":
		return fmt.Sprintf("%-18s sent     message_id=(empty)", o.kind)
	case o.sent:
		return fmt.Sprintf("%-18s sent     message_id=%s", o.kind, o.messageID)
	case o.class != "":
		return fmt.Sprintf("%-18s refused  class=%s smtp_code=%d", o.kind, o.class, o.code)
	}
	return fmt.Sprintf("%-18s failed   err_type=%s", o.kind, o.errType)
}

// em5bRun is one measurement: the outcomes in send order and every line the channels
// and the ceilings wrote, as JSON objects.
type em5bRun struct {
	outcomes []em5bOutcome
	log      string
}

// em5bChain builds what cmd/tappa's run() builds for two "email" flows: one transport,
// one breaker around it, the reset channel on the breaker, and the invitation route
// on the per-mailbox cap around the same breaker.
func em5bChain(cfg mail.Config, log *slog.Logger) (ResetChannel, *EmailInvitations, error) {
	transport, err := mail.New(cfg) // its errors name a field, never a value
	if err != nil {
		return nil, nil, fmt.Errorf("em5b: the transport: %w", err)
	}
	breaker, err := mail.NewBreaker(transport, mail.BreakerConfig{Log: log})
	if err != nil {
		return nil, nil, fmt.Errorf("em5b: the breaker: %w", err)
	}
	resets, err := NewEmailResetChannel(breaker, em5bBaseURL, log)
	if err != nil {
		return nil, nil, fmt.Errorf("em5b: the reset channel: %w", err)
	}
	mailboxes, err := mail.NewRecipientCap(breaker)
	if err != nil {
		return nil, nil, fmt.Errorf("em5b: the per-mailbox cap: %w", err)
	}
	invitations, err := NewEmailInvitations(mailboxes, em5bBaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("em5b: the invitation route: %w", err)
	}
	return resets, invitations, nil
}

// em5bSend sends the four fixtures to `to` through the chain built on cfg, pausing
// between sends (SES allows 14 a second; this is politeness, not a limit), then one
// recovery link through a chain built on probe. Each send has its own deadline. The
// error is a construction failure; a send's failure is its outcome.
func em5bSend(ctx context.Context, cfg, probe mail.Config, to string, pause time.Duration) (em5bRun, error) {
	var logged bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	resets, invitations, err := em5bChain(cfg, log)
	if err != nil {
		return em5bRun{}, err
	}
	probeResets, _, err := em5bChain(probe, log)
	if err != nil {
		return em5bRun{}, err
	}
	resetDelivery := func() ResetDelivery {
		w, _ := em5bExpect(em5bReset)
		return ResetDelivery{Recipient: to, Link: w.link, ExpiresAt: time.Now().Add(adminauth.ResetTTL), ResetID: uuid.New()}
	}
	invitation := func(ctx context.Context, k em5bKind, person, tenant string, verified bool) em5bOutcome {
		w, _ := em5bExpect(k)
		now := time.Now()
		sink := &emailLinkSink{mail: invitations, log: log}
		id, err := sink.SendInvitation(ctx, invite.Delivery{
			Invite: invite.Invite{
				ID: uuid.New(), TenantID: uuid.New(), EmployeeID: uuid.New(),
				CreatedAt: now, ExpiresAt: now.Add(em5bInviteLifetime),
			},
			ActivationURL: w.link,
			Recipient:     invite.Recipient{Address: to, EmployeeName: person, TenantName: tenant, TenantVerified: verified},
		})
		o := em5bOutcomeOf(string(k), err)
		o.messageID = id
		return o
	}
	steps := []func(context.Context) em5bOutcome{
		func(ctx context.Context) em5bOutcome {
			d := resetDelivery()
			o := em5bOutcomeOf(string(em5bReset), resets.DeliverReset(ctx, d))
			if o.sent {
				o.messageID = em5bLoggedID(logged.String(), "reset_id", d.ResetID.String())
			}
			return o
		},
		func(ctx context.Context) em5bOutcome {
			n := PasswordNotice{Recipient: to, TenantID: uuid.New(), AdminUserID: uuid.New()}
			o := em5bOutcomeOf(string(em5bNotice), resets.DeliverPasswordNotice(ctx, n))
			if o.sent {
				o.messageID = em5bLoggedID(logged.String(), "admin_user_id", n.AdminUserID.String())
			}
			return o
		},
		func(ctx context.Context) em5bOutcome {
			return invitation(ctx, em5bInviteVerified, em5bVerifiedPerson, em5bVerifiedTenant, true)
		},
		func(ctx context.Context) em5bOutcome {
			return invitation(ctx, em5bInviteUnverified, em5bCanaryPerson, em5bCanaryTenant, false)
		},
		func(ctx context.Context) em5bOutcome {
			d := resetDelivery()
			o := em5bOutcomeOf(em5bProbeKind, probeResets.DeliverReset(ctx, d))
			if o.sent {
				o.messageID = em5bLoggedID(logged.String(), "reset_id", d.ResetID.String())
			}
			return o
		},
	}
	var run em5bRun
	for i, step := range steps {
		if i > 0 && pause > 0 {
			select {
			case <-ctx.Done():
				return run, ctx.Err()
			case <-time.After(pause):
			}
		}
		sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		run.outcomes = append(run.outcomes, step(sctx))
		cancel()
	}
	run.log = logged.String()
	return run, nil
}

// em5bLoggedID is the message_id of the line whose key holds value ("" when there is
// none): the reset channel reports an accepted send only in its log line.
func em5bLoggedID(jsonLines, key, value string) string {
	for _, line := range strings.Split(jsonLines, "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if v, _ := rec[key].(string); v == value {
			id, _ := rec["message_id"].(string)
			return id
		}
	}
	return ""
}

// em5bLogKeys is ADR 0022 §10's closed set over every line this run can write: the
// three slog keys, the delivery lines' ids, class, code and message id, the outbox's
// keys, and the two ceilings' counters.
var em5bLogKeys = map[string]bool{
	"time": true, "level": true, "msg": true,
	"tenant_id": true, "employee_id": true, "admin_user_id": true, "invite_id": true, "reset_id": true,
	"message_id": true, "class": true, "smtp_code": true, "ip": true, "err_type": true, "err": true,
	"queue": true, "limit": true, "window": true, "refused": true, "hour_limit": true, "day_limit": true,
}

// em5bLogProblems scans the run's lines: every key inside em5bLogKeys, and none of the
// recipient (any case), the four link values, the links in base64, the fixture names
// or a credential — whole or as any 8-character run. It names what it found, never the
// value.
func em5bLogProblems(jsonLines, to string, credentials ...string) []string {
	var problems []string
	off := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(jsonLines), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			problems = append(problems, "a log line is not JSON")
			continue
		}
		for k := range rec {
			if !em5bLogKeys[k] {
				off[k] = true
			}
		}
	}
	for k := range off {
		problems = append(problems, "log key outside ADR 0022 §10's set: "+k)
	}
	lower := strings.ToLower(jsonLines)
	if to != "" && strings.Contains(lower, strings.ToLower(to)) {
		problems = append(problems, "the log holds the recipient address")
	}
	for _, k := range em5bKinds {
		w, _ := em5bExpect(k)
		if w.value != "" && strings.Contains(jsonLines, w.value) {
			problems = append(problems, "the log holds the "+string(k)+" link value")
		}
		if strings.Contains(jsonLines, base64.StdEncoding.EncodeToString([]byte(w.link))) {
			problems = append(problems, "the log holds the "+string(k)+" link in base64")
		}
	}
	for _, n := range []string{em5bVerifiedTenant, em5bVerifiedPerson, em5bCanaryTenant, em5bCanaryPerson} {
		if strings.Contains(lower, strings.ToLower(n)) {
			problems = append(problems, "the log holds a fixture name")
		}
	}
	for _, c := range credentials {
		if em5bHolds([]string{jsonLines}, c) {
			problems = append(problems, "the log holds an SMTP credential or a run of one")
		}
	}
	sort.Strings(problems)
	return problems
}

// em5bHolds reports whether any of hay holds secret whole or, for a secret of at
// least eight characters, any eight-character run of it (internal/mail's echo window).
func em5bHolds(hay []string, secret string) bool {
	if secret == "" {
		return false
	}
	for _, h := range hay {
		if strings.Contains(h, secret) {
			return true
		}
		for i := 0; i+8 <= len(secret); i++ {
			if strings.Contains(h, secret[i:i+8]) {
				return true
			}
		}
	}
	return false
}

// em5bSendProblems is the sender's verdict: the four fixtures sent, each with a
// message id that passed internal/mail's shape and echo rules (an empty id is either a
// 250 without one or an id the rules dropped — ADR 0022 §2, counted limit 21); the
// probe refused at AUTH; the log clean.
func em5bSendProblems(run em5bRun, s em5bSendSettings, probePass string) []string {
	var problems []string
	got := map[string]em5bOutcome{}
	for _, o := range run.outcomes {
		got[o.kind] = o
	}
	compact := func(o em5bOutcome) string { return strings.Join(strings.Fields(o.line())[1:], " ") }
	for _, k := range em5bKinds {
		o, ok := got[string(k)]
		switch {
		case !ok:
			problems = append(problems, string(k)+": not attempted")
		case !o.sent:
			problems = append(problems, string(k)+": not sent ("+compact(o)+")")
		case o.messageID == "":
			problems = append(problems, string(k)+": sent, but no message id survived the 250's shape and echo rules")
		}
	}
	switch o, ok := got[em5bProbeKind]; {
	case !ok:
		problems = append(problems, em5bProbeKind+": not attempted")
	case o.sent || o.class != string(mail.ClassAuth):
		problems = append(problems, em5bProbeKind+": want refused with class auth, got "+compact(o))
	}
	return append(problems, em5bLogProblems(run.log, s.to, s.user, s.pass, probePass)...)
}

// ---------------------------------------------------------------------------------
// The checker.
// ---------------------------------------------------------------------------------

// em5bInput is what the checker compares one received source against.
type em5bInput struct {
	kind em5bKind
	// to is TAPPA_REALSMTP_TO; replyTo is TAPPA_MAIL_REPLY_TO ("" = no Reply-To).
	to, replyTo string
	// user and pass are scanned for in the source and never printed.
	user, pass string
	// sesID is the message id the sender printed for this message. Not required to
	// start, but without it the "250 message id" row is N/A and the run INCOMPLETE.
	sesID string
}

// em5bCheckEnv reads the checker's settings. Every required one missing is a problem
// that names its variable, never its value.
func em5bCheckEnv(getenv func(string) string) (string, em5bInput, []string) {
	var problems []string
	need := func(name string) string {
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			problems = append(problems, name+" is not set")
		}
		return v
	}
	path := need(em5bEnvSource)
	in := em5bInput{kind: em5bKind(need(em5bEnvKind)), to: need(em5bEnvTo)}
	if _, ok := em5bExpect(in.kind); in.kind != "" && !ok {
		problems = append(problems, em5bEnvKind+" is none of reset, notice, invite-verified, invite-unverified")
	}
	in.user, in.pass = need(em5bEnvUser), need(em5bEnvPass)
	in.replyTo = strings.TrimSpace(getenv(em5bEnvReplyTo))
	in.sesID = strings.TrimSpace(getenv(em5bEnvSESID))
	return path, in, problems
}

// em5bRow is one line of the checker's table. note, when set, is printed under the
// table: a diagnosis the row's verdict alone does not carry.
type em5bRow struct {
	criterion, expected, found, result string
	note                               string
}

// The four results. N/A IS NOT A PASS: it is a criterion this source cannot answer
// (a receiver that wrote no Authentication-Results), and the table says so under it;
// the entry point keeps the test red while any row is N/A, so an unverified reading
// never reads green.
const (
	em5bPass = "PASS"
	em5bFail = "FAIL"
	em5bInfo = "INFO"
	em5bNA   = "N/A"
)

func em5bVerdict(ok bool) string {
	if ok {
		return em5bPass
	}
	return em5bFail
}

// em5bPart is one MIME part, decoded.
type em5bPart struct {
	mediaType, charset, encoding, body string
	decoded                            bool
}

// em5bSource is one received e-mail, read.
type em5bSource struct {
	raw        string
	head       netmail.Header
	topType    string
	parts      []em5bPart
	text, html string // the first text/plain and text/html parts, decoded
	// glued is true when a line starts with the boundary and goes straight on into
	// text — a header stuck to the boundary line. A composer does not write that; an
	// export that moved the line break does (measured 2026-10-09: mail.tm's /sources
	// moves the CRLF after the first boundary to before it). A parser then skips that
	// part as preamble, so the rows that read the parts may be reading the export's
	// damage, not the message as sent.
	glued bool
}

func em5bParse(raw []byte) (em5bSource, error) {
	m, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return em5bSource{}, err
	}
	s := em5bSource{raw: string(raw), head: m.Header}
	mt, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	s.topType = mt
	if err != nil || !strings.HasPrefix(mt, "multipart/") || params["boundary"] == "" {
		body, _ := io.ReadAll(m.Body)
		s.parts = []em5bPart{em5bDecode(m.Header.Get("Content-Type"), m.Header.Get("Content-Transfer-Encoding"), body)}
	} else {
		s.glued = regexp.MustCompile(`(?m)^--` + regexp.QuoteMeta(params["boundary"]) + `[^\s-]`).MatchString(s.raw)
		mr := multipart.NewReader(m.Body, params["boundary"])
		for {
			p, err := mr.NextRawPart()
			if err != nil {
				break // io.EOF, or a body that stops parsing: the MIME row reports the parts it got
			}
			body, _ := io.ReadAll(p)
			s.parts = append(s.parts, em5bDecode(p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"), body))
		}
	}
	for _, p := range s.parts {
		switch {
		case p.mediaType == "text/plain" && s.text == "":
			s.text = p.body
		case p.mediaType == "text/html" && s.html == "":
			s.html = p.body
		}
	}
	return s, nil
}

func em5bDecode(contentType, encoding string, body []byte) em5bPart {
	p := em5bPart{encoding: strings.ToLower(strings.TrimSpace(encoding))}
	if mt, params, err := mime.ParseMediaType(contentType); err == nil {
		p.mediaType, p.charset = mt, strings.ToLower(params["charset"])
	}
	var out []byte
	var err error
	switch p.encoding {
	case "quoted-printable":
		out, err = io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
	case "base64":
		out, err = base64.StdEncoding.DecodeString(strings.Join(strings.Fields(string(body)), ""))
	case "", "7bit", "8bit", "binary":
		out = body
	default:
		err = fmt.Errorf("em5b: unknown transfer encoding")
	}
	p.decoded = err == nil
	p.body = string(out)
	return p
}

// em5bCheck holds one received source to M7-04's and EM-5B's criteria.
func em5bCheck(raw []byte, in em5bInput) []em5bRow {
	want, ok := em5bExpect(in.kind)
	if !ok {
		return []em5bRow{{"kind", "reset | notice | invite-verified | invite-unverified", "unknown", em5bFail, ""}}
	}
	s, err := em5bParse(raw)
	if err != nil {
		return []em5bRow{{"source", "an RFC 5322 message", "does not parse", em5bFail, ""}}
	}
	rows := em5bAuthRows(s)
	rows = append(rows,
		em5bRegionRow(s), em5bTLSRow(s),
		em5bFromRow(s), em5bReplyToRow(s, in.replyTo), em5bToRow(s, in.to), em5bSubjectRow(s, want), em5bDateRow(s),
		em5bMessageIDRow(s), em5bSESIDRow(s, in.sesID),
		em5bMIMERow(s),
		em5bLinkRow("link (text part)", s.text, want.link, false),
		em5bLinkRow("link (html part)", s.html, want.link, true),
		em5bTrackingRow(s), em5bNamesRow(s, in.kind, want), em5bCredentialRow(s, in.user, in.pass), em5bValueRow(s, want),
	)
	return rows
}

// em5bAuth is one result of an Authentication-Results header (RFC 8601).
type em5bAuth struct {
	method, result string
	props          map[string]string
}

var em5bAroundEquals = regexp.MustCompile(`\s*=\s*`)

// em5bAuthResults reads one Authentication-Results value: comments dropped, results
// split at ';' outside quotes, each "method=result" followed by ptype.property=value.
func em5bAuthResults(v string) (string, []em5bAuth) {
	segs := em5bSplit(em5bStripComments(v))
	if len(segs) == 0 {
		return "", nil
	}
	serv := ""
	if f := strings.Fields(segs[0]); len(f) > 0 {
		serv = f[0]
	}
	var out []em5bAuth
	for _, seg := range segs[1:] {
		toks := em5bTokens(em5bAroundEquals.ReplaceAllString(seg, "="))
		if len(toks) == 0 {
			continue
		}
		mr := strings.SplitN(toks[0], "=", 2)
		if len(mr) != 2 {
			continue
		}
		a := em5bAuth{method: strings.ToLower(mr[0]), result: strings.ToLower(mr[1]), props: map[string]string{}}
		if i := strings.IndexByte(a.method, '/'); i >= 0 {
			a.method = a.method[:i]
		}
		for _, tk := range toks[1:] {
			if kv := strings.SplitN(tk, "=", 2); len(kv) == 2 {
				a.props[strings.ToLower(kv[0])] = strings.Trim(kv[1], `"`)
			}
		}
		out = append(out, a)
	}
	return serv, out
}

// em5bStripComments replaces every (comment), nested or not, with a space, leaving
// quoted strings alone.
func em5bStripComments(v string) string {
	var b strings.Builder
	depth, quoted := 0, false
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '\\' && i+1 < len(v):
			if depth == 0 {
				b.WriteByte(c)
				b.WriteByte(v[i+1])
			}
			i++
		case quoted:
			b.WriteByte(c)
			quoted = c != '"'
		case c == '"' && depth == 0:
			quoted = true
			b.WriteByte(c)
		case c == '(':
			depth++
		case c == ')' && depth > 0:
			depth--
			if depth == 0 {
				b.WriteByte(' ')
			}
		case depth == 0:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// em5bSplit splits at ';' outside quotes.
func em5bSplit(v string) []string {
	var out []string
	quoted, start := false, 0
	for i := 0; i < len(v); i++ {
		switch {
		case v[i] == '"':
			quoted = !quoted
		case v[i] == ';' && !quoted:
			out = append(out, v[start:i])
			start = i + 1
		}
	}
	return append(out, v[start:])
}

// em5bTokens splits at blanks outside quotes.
func em5bTokens(v string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '"':
			quoted = !quoted
			cur.WriteByte(c)
		case !quoted && (c == ' ' || c == '\t' || c == '\r' || c == '\n'):
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// em5bDomainOf is what follows the last '@', or the whole value, in lower case.
func em5bDomainOf(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if i := strings.LastIndexByte(v, '@'); i >= 0 {
		return v[i+1:]
	}
	return v
}

// em5bAuthRows reads the TOPMOST Authentication-Results header: receiving servers
// prepend theirs, so the top one is the final receiver's verdict.
func em5bAuthRows(s em5bSource) []em5bRow {
	headers := s.head["Authentication-Results"]
	serv, results := "", []em5bAuth(nil)
	if len(headers) > 0 {
		serv, results = em5bAuthResults(headers[0])
	}
	info := em5bRow{"receiver verdict", "the topmost Authentication-Results", "none", em5bInfo, ""}
	if len(headers) > 0 {
		info.found = strconv.Itoa(len(headers)) + " header(s); topmost written by " + serv
	}
	first := func(method string) (em5bAuth, bool) {
		for _, a := range results {
			if a.method == method {
				return a, true
			}
		}
		return em5bAuth{}, false
	}

	spf := em5bRow{"spf", "pass, smtp.mailfrom domain " + em5bMailFromDomain, "no spf result", em5bFail, ""}
	if a, ok := first("spf"); ok {
		domain := em5bDomainOf(a.props["smtp.mailfrom"])
		spf.found = a.result + ", smtp.mailfrom domain " + domain
		spf.result = em5bVerdict(a.result == "pass" && domain == em5bMailFromDomain)
	}

	var dkims []string
	signed := map[string]bool{}
	for _, a := range results {
		if a.method != "dkim" {
			continue
		}
		domain := strings.ToLower(a.props["header.d"])
		if domain == "" {
			domain = em5bDomainOf(a.props["header.i"])
		}
		dkims = append(dkims, a.result+" d="+domain)
		if a.result == "pass" {
			signed[domain] = true
		}
	}
	found := "no dkim result"
	if len(dkims) > 0 {
		found = strings.Join(dkims, ", ")
	}
	dkimOurs := em5bRow{criterion: "dkim " + em5bFromDomain, expected: "pass, d=" + em5bFromDomain, found: found,
		result: em5bVerdict(signed[em5bFromDomain])}
	// SES's own signature is RECORDED, NOT JUDGED (orchestrator's decision, EM-5B round
	// 2): DMARC aligns on the taptime.mt signature, and the amazonses.com one is
	// SES's documented habit, not a property this deployment relies on.
	dkimSES := em5bRow{criterion: "dkim " + em5bSESDomain, expected: "recorded: SES's own signature (DMARC rests on d=" + em5bFromDomain + ")",
		found: found, result: em5bInfo}

	dmarc := em5bRow{"dmarc", "pass, header.from=" + em5bFromDomain, "no dmarc result", em5bFail, ""}
	if a, ok := first("dmarc"); ok {
		from := strings.ToLower(a.props["header.from"])
		dmarc.found = a.result + ", header.from=" + from
		dmarc.result = em5bVerdict(a.result == "pass" && (from == "" || from == em5bFromDomain))
	}

	// NO VERDICT AT ALL IS NOT A FAILED VERDICT — AND NEVER A PASS (EM-5B round 2,
	// measured: mail.tm writes no Authentication-Results, while the same four messages
	// verified independently — DKIM both signatures, SPF mail.taptime.mt, DMARC
	// aligned). The three judged rows say N/A, the table says why under it, and the
	// entry point stays red until they are verified another way.
	if len(headers) == 0 {
		for _, r := range []*em5bRow{&spf, &dkimOurs, &dmarc} {
			r.found, r.result = "no receiver verdict in this source", em5bNA
		}
		dkimSES.found = "no receiver verdict in this source"
		info.note = "this receiver wrote no Authentication-Results: spf, dkim " + em5bFromDomain +
			" and dmarc are N/A, not passed — verify them independently (deploy/README.md, EM-5B)"
	}
	return []em5bRow{info, spf, dkimOurs, dkimSES, dmarc}
}

var (
	em5bSESHostRe   = regexp.MustCompile(`([a-z]{2}-[a-z]+-[0-9]+)\.amazonses\.com`)
	em5bRegionRe    = regexp.MustCompile(`\b([a-z]{2}-[a-z]+-[0-9]+)\b`)
	em5bReceivedSES = regexp.MustCompile(`(?i)^from\s+(\S*amazonses\.com)\b`)
)

// em5bRegionRow is M7-04's "the provider is in the EU": the SES region every SES
// trace in the headers names (the sending host in Received, SES's Message-ID and
// Return-Path, the Feedback-ID SES adds).
func em5bRegionRow(s em5bSource) em5bRow {
	regions := map[string][]string{}
	note := func(where, v string) {
		for _, m := range em5bSESHostRe.FindAllStringSubmatch(strings.ToLower(v), -1) {
			regions[m[1]] = append(regions[m[1]], where)
		}
	}
	for _, r := range s.head["Received"] {
		if m := em5bReceivedSES.FindStringSubmatch(r); m != nil {
			note("received", m[1])
		}
	}
	note("message-id", s.head.Get("Message-Id"))
	note("return-path", s.head.Get("Return-Path"))
	if fb := s.head.Get("Feedback-Id"); strings.Contains(strings.ToLower(fb), "amazonses") {
		for _, m := range em5bRegionRe.FindAllStringSubmatch(fb, -1) {
			regions[m[1]] = append(regions[m[1]], "feedback-id")
		}
	}
	row := em5bRow{"region", em5bRegion + " in every SES trace", "no SES trace in the headers", em5bFail, ""}
	if len(regions) == 0 {
		return row
	}
	var names []string
	for r, where := range regions {
		names = append(names, r+" ("+strings.Join(em5bUnique(where), ", ")+")")
	}
	sort.Strings(names)
	row.found = strings.Join(names, "; ")
	_, ours := regions[em5bRegion]
	row.result = em5bVerdict(len(regions) == 1 && ours)
	return row
}

func em5bUnique(v []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range v {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// em5bTLSRow reads the hop from SES to the receiver (the configuration set's TLS
// Require, ADR 0022 §11): the receiver's Received line for a host under amazonses.com
// must name ESMTPS or a TLS version.
func em5bTLSRow(s em5bSource) em5bRow {
	row := em5bRow{"ses to receiver tls", "ESMTPS or TLS on the hop from *." + em5bSESDomain, "no Received line from an " + em5bSESDomain + " host", em5bFail, ""}
	for _, r := range s.head["Received"] {
		m := em5bReceivedSES.FindStringSubmatch(r)
		if m == nil {
			continue
		}
		tls := strings.Contains(r, "ESMTPS") || strings.Contains(r, "TLS")
		row.found = "from " + m[1] + ": "
		if tls {
			row.found += "TLS"
		} else {
			row.found += "no ESMTPS, no TLS"
		}
		row.result = em5bVerdict(tls)
		return row
	}
	return row
}

func em5bFromRow(s em5bSource) em5bRow {
	row := em5bRow{"from", em5bFrom, "", em5bFail, ""}
	want, _ := netmail.ParseAddress(em5bFrom)
	froms := s.head["From"]
	if len(froms) != 1 {
		row.found = strconv.Itoa(len(froms)) + " From header(s)"
		return row
	}
	list, err := netmail.ParseAddressList(froms[0])
	if err != nil || len(list) != 1 {
		row.found = "not one address"
		return row
	}
	row.found = list[0].Name + " <" + list[0].Address + ">"
	row.result = em5bVerdict(list[0].Name == want.Name && list[0].Address == want.Address)
	return row
}

// em5bReplyToRow: absent while TAPPA_MAIL_REPLY_TO is empty (the shipped ConfigMap),
// else that one address. The value is not printed.
func em5bReplyToRow(s em5bSource, want string) em5bRow {
	got := s.head["Reply-To"]
	if want == "" {
		row := em5bRow{"reply-to", "absent (" + em5bEnvReplyTo + " empty)", "absent", em5bPass, ""}
		if len(got) > 0 {
			row.found, row.result = "present", em5bFail
		}
		return row
	}
	row := em5bRow{"reply-to", "= " + em5bEnvReplyTo, "absent", em5bFail, ""}
	if len(got) == 1 {
		a, err := netmail.ParseAddress(got[0])
		row.found = "differs"
		if err == nil && a.Address == want {
			row.found, row.result = "= "+em5bEnvReplyTo, em5bPass
		}
	} else if len(got) > 1 {
		row.found = strconv.Itoa(len(got)) + " headers"
	}
	return row
}

// em5bToRow is B3: the message went to the one address it was sent to. The address
// is never printed.
func em5bToRow(s em5bSource, want string) em5bRow {
	row := em5bRow{"to", "one address, = " + em5bEnvTo, "", em5bFail, ""}
	if want == "" {
		row.found = "not checked: " + em5bEnvTo + " unset"
		return row
	}
	tos := s.head["To"]
	if len(tos) != 1 {
		row.found = strconv.Itoa(len(tos)) + " To header(s)"
		return row
	}
	list, err := netmail.ParseAddressList(tos[0])
	switch {
	case err != nil:
		row.found = "does not parse"
	case len(list) != 1:
		row.found = strconv.Itoa(len(list)) + " addresses"
	case list[0].Address != want:
		row.found = "one address, differs"
	default:
		row.found, row.result = "one address, = "+em5bEnvTo, em5bPass
	}
	return row
}

// em5bSubjectRow: the fixed ASCII subject, as written — not re-encoded on the way.
func em5bSubjectRow(s em5bSource, want em5bWanted) em5bRow {
	row := em5bRow{"subject", want.subject + " (fixed ASCII)", "", em5bFail, ""}
	got := s.head["Subject"]
	if len(got) != 1 {
		row.found = strconv.Itoa(len(got)) + " Subject header(s)"
		return row
	}
	v := got[0]
	if len(v) <= 120 && em5bPrintable(v) {
		row.found = v
	} else {
		row.found = "not printable ASCII (" + strconv.Itoa(len(v)) + " bytes)"
	}
	row.result = em5bVerdict(v == want.subject)
	return row
}

func em5bPrintable(v string) bool {
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] > 0x7e {
			return false
		}
	}
	return true
}

func em5bDateRow(s em5bSource) em5bRow {
	row := em5bRow{"date", "one Date header, RFC 5322", "", em5bFail, ""}
	got := s.head["Date"]
	if len(got) != 1 {
		row.found = strconv.Itoa(len(got)) + " Date header(s)"
		return row
	}
	if _, err := netmail.ParseDate(got[0]); err != nil {
		row.found = "does not parse"
		return row
	}
	row.found, row.result = got[0], em5bPass
	return row
}

var em5bMessageIDRe = regexp.MustCompile(`^<([^<>@\s]+)@([^<>@\s]+)>$`)

// em5bMessageIDRow says what became of OUR Message-ID (the composer writes
// <random@taptime.mt>, ADR 0022 §2): kept, or replaced by the relay's. Either is an
// answer; EM-5B's question is which.
func em5bMessageIDRow(s em5bSource) em5bRow {
	row := em5bRow{"message-id", "ours <random@" + em5bFromDomain + "> or SES's (recorded)", "", em5bInfo, ""}
	got := s.head["Message-Id"]
	if len(got) != 1 {
		row.found = strconv.Itoa(len(got)) + " Message-ID header(s)"
		return row
	}
	v := strings.TrimSpace(got[0])
	m := em5bMessageIDRe.FindStringSubmatch(v)
	switch {
	case m == nil || len(v) > 200 || !em5bPrintable(v):
		row.found = "not <local@domain>"
	case strings.ToLower(m[2]) == em5bFromDomain:
		row.found = v + " (ours: kept)"
	case strings.HasSuffix(strings.ToLower(m[2]), "."+em5bSESDomain) || strings.ToLower(m[2]) == em5bSESDomain:
		row.found = v + " (SES's: ours replaced)"
	default:
		row.found = v + " (another's)"
	}
	return row
}

var em5bIDShape = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// em5bSESIDRow ties the id the sender read from the 250 to this source: the headers
// that carry it (SES writes its id into its Message-ID and into the bounce address).
func em5bSESIDRow(s em5bSource, id string) em5bRow {
	row := em5bRow{"250 message id", "the sender's message_id appears in the headers", "", em5bFail, ""}
	// NO ID IS N/A, NOT INFO (EM-5B round 4): this row is the one tie between the source
	// and THIS run's send. Without it the other rows judge only the fixtures, and an
	// older source carrying the same fixtures would read PASS.
	if id == "" {
		row.found, row.result = "not given ("+em5bEnvSESID+" unset)", em5bNA
		row.note = "without the message_id the sender printed for this message, nothing ties this source to " +
			"this run — an older source with the same fixtures would pass; set " + em5bEnvSESID
		return row
	}
	if !em5bIDShape.MatchString(id) {
		row.found = em5bEnvSESID + " is not an id"
		return row
	}
	var where []string
	for name, values := range s.head {
		for _, v := range values {
			if strings.Contains(v, id) {
				where = append(where, strings.ToLower(name))
				break
			}
		}
	}
	sort.Strings(where)
	if len(where) == 0 {
		row.found = "in no header"
		return row
	}
	row.found, row.result = "in "+strings.Join(where, ", "), em5bPass
	return row
}

// em5bMIMERow: multipart/alternative of exactly text/plain then text/html, both UTF-8
// (ADR 0022 §2, §8).
func em5bMIMERow(s em5bSource) em5bRow {
	row := em5bRow{"mime", "multipart/alternative: text/plain, text/html; utf-8", "", em5bFail, ""}
	var parts []string
	ok := s.topType == "multipart/alternative" && len(s.parts) == 2
	for i, p := range s.parts {
		parts = append(parts, p.mediaType+" "+p.charset+" "+p.encoding)
		want := "text/plain"
		if i == 1 {
			want = "text/html"
		}
		ok = ok && p.mediaType == want && p.charset == "utf-8" && p.decoded && utf8.ValidString(p.body) && p.body != ""
	}
	row.found = s.topType + ": " + strings.Join(parts, ", ")
	row.result = em5bVerdict(ok)
	// A DIAGNOSIS, NOT A REPAIR: the row keeps its verdict on what it read. Re-inserting
	// the line break here would make the checker pass a source it had to edit first.
	if s.glued {
		row.found += " (a boundary line has a header glued to it)"
		row.note = "the source's MIME boundaries look damaged by its export (measured 2026-10-09: mail.tm's " +
			"/sources moves the CRLF after the first boundary to before it), so the rows that read the parts " +
			"(mime, the text part's link, names, link value) may be reading the export, not the message — check " +
			"the DKIM body hash (bh=) on the raw source (deploy/README.md, EM-5B)"
	}
	return row
}

var em5bURLRe = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"'()]+`)

// em5bURLs is every absolute URL in a decoded part (an HTML part's entities resolved).
func em5bURLs(body string, isHTML bool) []string {
	urls := em5bURLRe.FindAllString(body, -1)
	if isHTML {
		for i, u := range urls {
			urls[i] = html.UnescapeString(u)
		}
	}
	return urls
}

// em5bLinkRow: the part carries ONE absolute URL and it is the link as sent — no
// redirect, no click tracking (ADR 0022 §8, §11). A URL that differs is printed as its
// host only.
func em5bLinkRow(name, body, link string, isHTML bool) em5bRow {
	row := em5bRow{name, "one URL, the link unchanged", "", em5bFail, ""}
	urls := em5bURLs(body, isHTML)
	if len(urls) == 1 && urls[0] == link {
		row.found, row.result = "1 URL, unchanged", em5bPass
		return row
	}
	var hosts []string
	for _, u := range urls {
		host := u
		if i := strings.Index(host, "://"); i >= 0 {
			host = host[i+3:]
		}
		if i := strings.IndexAny(host, "/?#"); i >= 0 {
			host = host[:i]
		}
		hosts = append(hosts, host)
	}
	row.found = strconv.Itoa(len(urls)) + " URL(s)"
	if len(hosts) > 0 {
		row.found += ": " + strings.Join(hosts, ", ")
	}
	return row
}

var (
	em5bImgRe = regexp.MustCompile(`(?i)<img\b`)
	em5bSrcRe = regexp.MustCompile(`(?i)\ssrc\s*=`)
)

// em5bTrackingRow: open and click tracking off (ADR 0022 §11) — no awstrack anywhere
// (raw or decoded: a quoted-printable soft break can split the word in the raw form),
// no image and no src attribute in the HTML (§8's pixel ban).
func em5bTrackingRow(s em5bSource) em5bRow {
	aws := strings.Count(strings.ToLower(s.raw), "awstrack") + strings.Count(strings.ToLower(s.text+s.html), "awstrack")
	img := len(em5bImgRe.FindAllString(s.html, -1))
	src := len(em5bSrcRe.FindAllString(s.html, -1))
	return em5bRow{"tracking", "awstrack 0, <img 0, src= 0",
		"awstrack " + strconv.Itoa(aws) + ", <img " + strconv.Itoa(img) + ", src= " + strconv.Itoa(src),
		em5bVerdict(aws+img+src == 0), ""}
}

// em5bNamesRow is ADR 0022 counted limit 22: the unverified invitation names nobody
// (and its canary names appear in no message); the verified one shows both names (the
// gate's positive control); the reset and the notice name nobody.
func em5bNamesRow(s em5bSource, k em5bKind, want em5bWanted) em5bRow {
	expected := map[em5bKind]string{
		em5bInviteVerified:   "both names shown, in both parts",
		em5bInviteUnverified: "no name; the neutral words in both parts",
		em5bReset:            "no name",
		em5bNotice:           "no name",
	}[k]
	forbidden := []string{em5bCanaryTenant, em5bCanaryPerson}
	if k != em5bInviteVerified {
		forbidden = append(forbidden, em5bVerifiedTenant, em5bVerifiedPerson)
	}
	text, page := s.text, html.UnescapeString(s.html)
	hay := strings.ToLower(text + "\n" + page + "\n" + s.head.Get("Subject"))
	shown := 0
	for _, n := range forbidden {
		if strings.Contains(hay, strings.ToLower(n)) {
			shown++
		}
	}
	var missing []string
	for _, phrase := range want.shown {
		if !strings.Contains(text, phrase) || !strings.Contains(page, phrase) {
			missing = append(missing, `"`+phrase+`"`)
		}
	}
	found := "no forbidden name"
	if shown > 0 {
		found = strconv.Itoa(shown) + " forbidden name(s) shown"
	}
	if len(want.shown) > 0 {
		if len(missing) == 0 {
			found += "; expected words in both parts"
		} else {
			found += "; missing " + strings.Join(missing, ", ")
		}
	}
	return em5bRow{"names", expected, found, em5bVerdict(shown == 0 && len(missing) == 0), ""}
}

// em5bCredentialRow: the SMTP username and password are nowhere in the source, whole
// or as any 8-character run. Never printed, whichever way it ends.
func em5bCredentialRow(s em5bSource, user, pass string) em5bRow {
	row := em5bRow{"smtp credentials", "absent, whole and every 8-character run", "", em5bFail, ""}
	if user == "" || pass == "" {
		row.found = "not checked: " + em5bEnvUser + " or " + em5bEnvPass + " unset"
		return row
	}
	hay := []string{s.raw, s.text, s.html, html.UnescapeString(s.html)}
	if em5bHolds(hay, user) || em5bHolds(hay, pass) {
		row.found = "PRESENT (not printed)"
		return row
	}
	row.found, row.result = "absent", em5bPass
	return row
}

var (
	em5bValueRe = regexp.MustCompile(`[?&](?:t|code)=([^&\s"'<>#]*)`)
	em5bRunRe   = regexp.MustCompile(`[A-Za-z0-9_-]{43,}`)
)

// em5bValueRow: every link value in the parts is this message's fixture, and nothing
// in them has the shape of a real one (43 characters of base64url, the shape both
// producers mint). It guards the measurement itself: a real e-mail's source — a live
// link — handed to the checker by mistake fails here.
func em5bValueRow(s em5bSource, want em5bWanted) em5bRow {
	page := html.UnescapeString(s.html)
	var values []string
	for _, part := range []string{s.text, page} {
		for _, m := range em5bValueRe.FindAllStringSubmatch(part, -1) {
			values = append(values, m[1])
		}
	}
	foreign := 0
	for _, v := range values {
		if v != want.value {
			foreign++
		}
	}
	runs := len(em5bRunRe.FindAllString(s.text, -1)) + len(em5bRunRe.FindAllString(page, -1))
	expected := "no link value; no 43-character base64url run"
	ok := len(values) == 0
	if want.value != "" {
		expected = "the fixture's value in both parts; no 43-character base64url run"
		ok = len(values) == 2 && foreign == 0
	}
	found := strconv.Itoa(len(values)) + " link value(s), " + strconv.Itoa(foreign) + " not the fixture's; " +
		strconv.Itoa(runs) + " long base64url run(s)"
	return em5bRow{"link value", expected, found, em5bVerdict(ok && runs == 0), ""}
}

// em5bTable renders the rows as the checker prints them.
func em5bTable(rows []em5bRow) string {
	head := em5bRow{"criterion", "expected", "found", "result", ""}
	w := [3]int{}
	for _, r := range append([]em5bRow{head}, rows...) {
		for i, c := range []string{r.criterion, r.expected, r.found} {
			if n := utf8.RuneCountInString(c); n > w[i] {
				w[i] = n
			}
		}
	}
	var b strings.Builder
	for _, r := range append([]em5bRow{head}, rows...) {
		fmt.Fprintf(&b, "%-*s | %-*s | %-*s | %s\n", w[0], r.criterion, w[1], r.expected, w[2], r.found, r.result)
	}
	for _, line := range em5bFooter(rows) {
		b.WriteString(line + "\n")
	}
	return b.String()
}

// em5bFooter is what the table says under itself: one verdict — FAIL over INCOMPLETE
// over PASS, so N/A never reads as a pass — the N/A criteria by name, and every note.
func em5bFooter(rows []em5bRow) []string {
	failed, unverified := em5bFailed(rows), em5bUnverified(rows)
	var out []string
	switch {
	case len(failed) > 0:
		out = append(out, "verdict: FAIL, "+strconv.Itoa(len(failed))+" criteria: "+strings.Join(failed, ", "))
	case len(unverified) > 0:
		out = append(out, "verdict: INCOMPLETE, nothing failed but not every criterion was verified")
	default:
		out = append(out, "verdict: PASS, every judged criterion passed (INFO rows are recorded, not judged)")
	}
	if len(unverified) > 0 {
		out = append(out, "not verified by this source (N/A), NOT passed: "+strings.Join(unverified, ", "))
	}
	for _, r := range rows {
		if r.note != "" {
			out = append(out, "note ("+r.criterion+"): "+r.note)
		}
	}
	return out
}

// em5bCheckVerdict is the checker entry point's decision: "" only when every judged
// criterion passed. A FAIL is reported as one; N/A with nothing failed is INCOMPLETE —
// still red, because "the receiver wrote no verdict" must never end as a green run.
func em5bCheckVerdict(rows []em5bRow) string {
	failed, unverified := em5bFailed(rows), em5bUnverified(rows)
	na := ""
	if len(unverified) > 0 {
		na = "not verified by this source (N/A): " + strings.Join(unverified, ", ")
	}
	switch {
	case len(failed) > 0 && na != "":
		return strconv.Itoa(len(failed)) + " criteria FAIL: " + strings.Join(failed, ", ") + "; and " + na
	case len(failed) > 0:
		return strconv.Itoa(len(failed)) + " criteria FAIL: " + strings.Join(failed, ", ")
	case na != "":
		return "INCOMPLETE, nothing failed but " + na + " — red so an unverified reading never reads green; " +
			"verify them independently and record that on the card (deploy/README.md, EM-5B)"
	}
	return ""
}

// em5bFailed names the criteria that failed.
func em5bFailed(rows []em5bRow) []string {
	return em5bWith(rows, em5bFail)
}

// em5bUnverified names the criteria this source could not answer (N/A).
func em5bUnverified(rows []em5bRow) []string {
	return em5bWith(rows, em5bNA)
}

func em5bWith(rows []em5bRow, result string) []string {
	var out []string
	for _, r := range rows {
		if r.result == result {
			out = append(out, r.criterion)
		}
	}
	return out
}

// ---------------------------------------------------------------------------------
// The kit's own tests (in-test relay, loopback, no network).
// ---------------------------------------------------------------------------------

// em5bFakeRun is one sender run against two in-test relays — one that accepts, and
// one that refuses AUTH as SES does a wrong password — and what the first received.
type em5bFakeRun struct {
	run       em5bRun
	settings  em5bSendSettings
	probePass string
	sources   map[em5bKind]string // each message exactly as the relay received it
}

func em5bFakeSend(t *testing.T) em5bFakeRun {
	t.Helper()
	relay := newFakeRelay(t, relayScript{})
	refusing := newFakeRelay(t, relayScript{authReply: "535 5.7.8 Authentication Credentials Invalid"})
	user, pass := relayCredentials(t)
	s := em5bSendSettings{to: "em5b.recipient@example.test", user: user, pass: pass}
	s.cfg = mail.Config{
		Host:       "127.0.0.1",
		Port:       relay.ln.Addr().(*net.TCPAddr).Port,
		Username:   mail.NewCredential(user),
		Password:   mail.NewCredential(pass),
		From:       em5bFrom,
		RootCAs:    relay.pool,
		Timeout:    5 * time.Second,
		RetryDelay: 20 * time.Millisecond,
	}
	probe, probePass := em5bProbeConfig(t, s.cfg)
	probe.Port, probe.RootCAs = refusing.ln.Addr().(*net.TCPAddr).Port, refusing.pool
	run, err := em5bSend(context.Background(), s.cfg, probe, s.to, 0)
	if err != nil {
		t.Fatalf("em5bSend: %v", err)
	}
	out := em5bFakeRun{run: run, settings: s, probePass: probePass, sources: map[em5bKind]string{}}
	for _, session := range relay.completed() {
		m := parseRelayed(t, session.message)
		for _, k := range em5bKinds {
			if w, _ := em5bExpect(k); m.header.Get("Subject") == w.subject && strings.Contains(m.text, w.link) {
				out.sources[k] = session.message
			}
		}
		if len(session.rcptTo) != 1 || session.rcptTo[0] != "RCPT TO:<"+s.to+">" {
			t.Errorf("a message went to %q, want exactly the given address", session.rcptTo)
		}
	}
	return out
}

// em5bReceiverOutlook is a receiver's header block in the shape with header.d (as
// Outlook and most servers write it), over an SES hop from eu-central-1.
func em5bReceiverOutlook(to string) string {
	return "Authentication-Results: mx.receiver.test;\r\n" +
		"\tdkim=pass header.d=taptime.mt header.i=@taptime.mt header.s=fixture header.b=Fixture1;\r\n" +
		"\tdkim=pass header.d=amazonses.com header.i=@amazonses.com header.s=fixture header.b=Fixture2;\r\n" +
		"\tspf=pass (receiver.test: domain of " + em5bBounce + " designates 192.0.2.10 as permitted sender)\r\n" +
		"\t smtp.mailfrom=" + em5bBounce + ";\r\n" +
		"\tdmarc=pass (p=NONE sp=NONE dis=NONE) header.from=taptime.mt\r\n" +
		"Received: from e0-0.smtp-out.eu-central-1.amazonses.com (e0-0.smtp-out.eu-central-1.amazonses.com. [192.0.2.10])\r\n" +
		"\tby mx.receiver.test with ESMTPS id fixture\r\n" +
		"\tfor <" + to + ">; Fri, 09 Oct 2026 10:00:00 +0000\r\n" +
		"Return-Path: <" + em5bBounce + ">\r\n" +
		"Feedback-ID: ::1.eu-central-1.fixture=:AmazonSES\r\n"
}

// em5bFixtureSESID stands for the id SES's 250 carries; em5bBounce is an envelope
// sender under the custom MAIL FROM domain with that id as its local part — the shape
// SES's bounce address takes (an assumption of this fixture, not a measurement; the
// real run's "250 message id" row found the id in the headers).
const (
	em5bFixtureSESID = "0107019fixture00-a1b2c3d4-0000-4000-8000-0123456789ab-000000"
	em5bBounce       = em5bFixtureSESID + "@" + em5bMailFromDomain
)

// em5bGmail is the same verdict in Gmail's shape: no header.d, the domain only in
// header.i — the shape the orchestrator's received sources carry.
func em5bGmail(src string) string {
	return strings.ReplaceAll(src, "header.d=taptime.mt header.i=@taptime.mt", "header.i=@taptime.mt")
}

// em5bReassemble rebuilds src with new decoded parts (quoted-printable, as the product
// writes them): the way to break a body without hand-editing quoted-printable.
func em5bReassemble(t *testing.T, src, text, page string) string {
	t.Helper()
	end := strings.Index(src, "\r\n\r\n")
	var keep []string
	for _, line := range strings.Split(src[:end], "\r\n") {
		if !strings.HasPrefix(line, "Content-Type:") {
			keep = append(keep, line)
		}
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, p := range [2][2]string{{"text/plain; charset=utf-8", text}, {"text/html; charset=utf-8", page}} {
		w, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {p[0]}, "Content-Transfer-Encoding": {"quoted-printable"}})
		if err != nil {
			t.Fatal(err)
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(p[1])); err != nil {
			t.Fatal(err)
		}
		if err := qp.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	keep = append(keep, "Content-Type: multipart/alternative; boundary="+mw.Boundary())
	return strings.Join(keep, "\r\n") + "\r\n\r\n" + body.String()
}

// em5bTableIsSafe holds a printed table free of what the tools must never print.
func em5bTableIsSafe(t *testing.T, table string, secrets ...string) {
	t.Helper()
	for _, v := range secrets {
		if v != "" && strings.Contains(strings.ToLower(table), strings.ToLower(v)) {
			t.Errorf("the printed table holds a value it must never print:\n%s", table)
		}
	}
}

// TestEM5BKit_SendsEveryMessageThroughTheProductionChain is the sender against the
// in-test relay: the four fixtures through the production chain, each sent with the
// relay's id; the wrong-password chain refused as auth 535; the log within ADR 0022
// §10 and free of the address, the links, the names and the credentials; and the
// log scan's own control — it does catch each of those.
func TestEM5BKit_SendsEveryMessageThroughTheProductionChain(t *testing.T) {
	t.Parallel()
	f := em5bFakeSend(t)
	var lines []string
	for _, o := range f.run.outcomes {
		lines = append(lines, o.line())
	}
	printed := strings.Join(lines, "\n")
	t.Logf("the sender's output:\n%s", printed)
	em5bTableIsSafe(t, printed, f.settings.to, f.settings.user, f.settings.pass, f.probePass)
	if len(f.run.outcomes) != 5 {
		t.Fatalf("%d outcomes, want 5 (four fixtures and the probe)", len(f.run.outcomes))
	}
	for i, k := range em5bKinds {
		o := f.run.outcomes[i]
		if o.kind != string(k) || !o.sent || o.messageID != relayMessageID {
			t.Errorf("outcome %d = %s, want %s sent with the relay's id", i, o.line(), k)
		}
	}
	if o := f.run.outcomes[4]; o.kind != em5bProbeKind || o.sent || o.class != string(mail.ClassAuth) || o.code != 535 {
		t.Errorf("the probe = %s, want refused with class auth and code 535", o.line())
	}
	if len(f.sources) != len(em5bKinds) {
		t.Errorf("the relay received %d of the four fixtures", len(f.sources))
	}
	if p := em5bSendProblems(f.run, f.settings, f.probePass); len(p) != 0 {
		t.Errorf("the sender's verdict on a clean run: %q", p)
	}

	// CONTROL: the scan is not blind to what it exists to catch.
	w, _ := em5bExpect(em5bReset)
	for name, line := range map[string]string{
		"recipient, upper case": `{"msg":"x","message_id":"` + strings.ToUpper(f.settings.to) + `"}`,
		"link value":            `{"msg":"x","message_id":"` + w.value + `"}`,
		"link in base64":        `{"msg":"x","message_id":"` + base64.StdEncoding.EncodeToString([]byte(w.link)) + `"}`,
		"fixture name":          `{"msg":"x","message_id":"` + em5bCanaryPerson + `"}`,
		"8 characters of one":   `{"msg":"x","message_id":"q` + f.settings.pass[2:10] + `"}`,
		"a key outside the set": `{"msg":"x","subject":"y"}`,
	} {
		if p := em5bLogProblems(line, f.settings.to, f.settings.user, f.settings.pass); len(p) == 0 {
			t.Errorf("CONTROL FAILED: the log scan misses a line holding the %s", name)
		}
	}
}

// TestEM5BKit_TheCheckerPassesWhatTheProductSends is the checker's no-false-alarm side:
// the four messages exactly as the product composed them, under a receiver's verdict
// in the header.d shape and in Gmail's header.i shape, and once with SES's Message-ID
// in place of ours (the id the sender read then found in the headers) — no criterion
// fails, and no table holds the address or a credential.
func TestEM5BKit_TheCheckerPassesWhatTheProductSends(t *testing.T) {
	t.Parallel()
	f := em5bFakeSend(t)
	for _, k := range em5bKinds {
		src, ok := f.sources[k]
		if !ok {
			t.Fatalf("no %s message reached the relay", k)
		}
		// Every case names the 250 id: without it the run is INCOMPLETE (round 4).
		in := em5bInput{kind: k, to: f.settings.to, user: f.settings.user, pass: f.settings.pass, sesID: em5bFixtureSESID}
		withSES := regexp.MustCompile(`(?m)^Message-ID: <[^>]*>\r$`).
			ReplaceAllString(src, "Message-ID: <"+em5bFixtureSESID+"@"+em5bRegion+"."+em5bSESDomain+">\r")
		cases := []struct {
			name, source, msgID, idIn string
		}{
			{"header.d", em5bReceiverOutlook(f.settings.to) + src, "(ours: kept)", "return-path"},
			{"gmail header.i", em5bGmail(em5bReceiverOutlook(f.settings.to)) + src, "(ours: kept)", "return-path"},
			{"ses message-id", em5bReceiverOutlook(f.settings.to) + withSES, "(SES's: ours replaced)", "message-id"},
		}
		for _, c := range cases {
			rows := em5bCheck([]byte(c.source), in)
			table := em5bTable(rows)
			if failed := em5bFailed(rows); len(failed) != 0 {
				t.Errorf("%s, %s: %q FAIL on what the product sends:\n%s", k, c.name, failed, table)
			}
			if v := em5bCheckVerdict(rows); v != "" || !strings.Contains(table, "verdict: PASS") {
				t.Errorf("%s, %s: the entry point's verdict %q, want green with a PASS line:\n%s", k, c.name, v, table)
			}
			if c.name == "gmail header.i" {
				t.Logf("%s, %s:\n%s", k, c.name, table)
			}
			if len(rows) != 21 {
				t.Errorf("%s, %s: %d rows, want 21:\n%s", k, c.name, len(rows), table)
			}
			for _, r := range rows {
				if r.criterion == "message-id" && !strings.HasSuffix(r.found, c.msgID) {
					t.Errorf("%s, %s: message-id found %q, want it to end %q", k, c.name, r.found, c.msgID)
				}
				if r.criterion == "250 message id" && (r.result != em5bPass || !strings.Contains(r.found, c.idIn)) {
					t.Errorf("%s, %s: the 250 id row %q %s, want PASS with the id in %s", k, c.name, r.found, r.result, c.idIn)
				}
			}
			em5bTableIsSafe(t, table, f.settings.to, f.settings.user, f.settings.pass)
		}
	}
}

// TestEM5BKit_TheCheckerFailsEachBrokenCriterion is the checker's other side: a source
// broken in one way fails exactly the criteria that way breaks — so a FAIL names its
// cause and a criterion is not satisfied by accident. Two shapes measured on the real
// relay's run (EM-5B round 2, a mail.tm inbox) are here too: a source with NO
// Authentication-Results gives N/A (never PASS, never FAIL) on spf, dkim taptime.mt and
// dmarc, an INCOMPLETE verdict and a red entry point; a source whose export moved the
// CRLF after the first boundary before it fails the rows that read the parts and
// carries the MIME diagnosis. Every table stays free of the address and the
// credentials, the credential case included.
func TestEM5BKit_TheCheckerFailsEachBrokenCriterion(t *testing.T) {
	t.Parallel()
	f := em5bFakeSend(t)
	to, user, pass := f.settings.to, f.settings.user, f.settings.pass
	good := func(k em5bKind) string { return em5bReceiverOutlook(to) + f.sources[k] }
	unbroken := func(k em5bKind) func() string { return func() string { return good(k) } }
	parts := func(k em5bKind) (string, string) {
		m := parseRelayed(t, f.sources[k])
		return m.text, m.html
	}
	reset, _ := em5bExpect(em5bReset)
	tracked := "https://fixture.r." + em5bRegion + ".awstrack.me/L0/https:%2F%2Ftaptime.mt%2Fadmin%2Freset%2Fnew/1/" +
		strings.Repeat("Fx0", 15) + "=391"
	replace := func(k em5bKind, old, new string) func() string {
		return func() string {
			src := good(k)
			if !strings.Contains(src, old) {
				t.Fatalf("the %s source has no %q to break", k, old)
			}
			return strings.ReplaceAll(src, old, new)
		}
	}
	body := func(k em5bKind, edit func(text, page string) (string, string)) func() string {
		return func() string {
			text, page := parts(k)
			text, page = edit(text, page)
			return em5bReassemble(t, good(k), text, page)
		}
	}
	// withName adds name as a paragraph of its own to both parts and changes nothing else.
	withName := func(name string) func(text, page string) (string, string) {
		return func(text, page string) (string, string) {
			if !strings.Contains(page, "</body>") {
				t.Fatal("the html part has no </body> to add a paragraph before")
			}
			return text + "\n" + name + "\n", strings.Replace(page, "</body>", "<p>"+name+"</p></body>", 1)
		}
	}
	cases := []struct {
		name   string
		kind   em5bKind
		source func() string
		input  func(*em5bInput)
		fails  []string
	}{
		{"spf fails", em5bReset, replace(em5bReset, "spf=pass", "spf=fail"), nil, []string{"spf"}},
		{"bounce domain is SES's, not mail.taptime.mt", em5bReset,
			replace(em5bReset, "smtp.mailfrom="+em5bBounce, "smtp.mailfrom="+em5bFixtureSESID+"@eu-central-1.amazonses.com"), nil, []string{"spf"}},
		{"no taptime.mt signature", em5bNotice,
			replace(em5bNotice, "header.d=taptime.mt header.i=@taptime.mt", "header.d=other.test header.i=@other.test"), nil, []string{"dkim taptime.mt"}},
		// Recorded, not judged (EM-5B round 2): the row is INFO, so nothing fails.
		{"SES's signature fails", em5bNotice,
			replace(em5bNotice, "dkim=pass header.d=amazonses.com", "dkim=fail header.d=amazonses.com"), nil, nil},
		{"dmarc fails", em5bReset, replace(em5bReset, "dmarc=pass", "dmarc=fail"), nil, []string{"dmarc"}},
		{"another region", em5bReset, replace(em5bReset, em5bRegion, "us-east-1"), nil, []string{"region"}},
		{"plain hop from SES", em5bReset, replace(em5bReset, "with ESMTPS", "with ESMTP"), nil, []string{"ses to receiver tls"}},
		// net/mail writes the display name quoted: the composer's From line reads
		// "Taptime" <no-reply@taptime.mt>.
		{"another sender", em5bReset, replace(em5bReset, "\r\nFrom: \"Taptime\" <no-reply@taptime.mt>\r\n", "\r\nFrom: \"Taptime\" <no-reply@other.test>\r\n"), nil, []string{"from"}},
		{"a Reply-To nobody configured", em5bReset,
			replace(em5bReset, "\r\nFrom: \"Taptime\" <no-reply@taptime.mt>\r\n", "\r\nFrom: \"Taptime\" <no-reply@taptime.mt>\r\nReply-To: replies@other.test\r\n"), nil, []string{"reply-to"}},
		{"another recipient", em5bReset, unbroken(em5bReset), func(in *em5bInput) { in.to = "someone.else@example.test" }, []string{"to"}},
		{"subject re-encoded", em5bReset,
			replace(em5bReset, "Subject: Reset your Taptime password", "Subject: =?utf-8?q?Reset_your_Taptime_password?="), nil, []string{"subject"}},
		{"no Date", em5bReset, func() string {
			return regexp.MustCompile(`(?m)^Date: [^\r]*\r\n`).ReplaceAllString(good(em5bReset), "")
		}, nil, []string{"date"}},
		{"the 250 id is not in the source", em5bReset, unbroken(em5bReset),
			func(in *em5bInput) { in.sesID = "0107019another00-a1b2c3d4-0000-4000-8000-0123456789ab-000000" }, []string{"250 message id"}},
		{"not alternative", em5bReset, replace(em5bReset, "multipart/alternative", "multipart/mixed"), nil, []string{"mime"}},
		{"html not utf-8", em5bReset, replace(em5bReset, "text/html; charset=utf-8", "text/html; charset=iso-8859-1"), nil, []string{"mime"}},
		{"click tracking", em5bReset, body(em5bReset, func(text, page string) (string, string) {
			return strings.ReplaceAll(text, reset.link, tracked), strings.ReplaceAll(page, reset.link, tracked)
		}), nil, []string{"link (text part)", "link (html part)", "tracking", "link value"}},
		{"open pixel", em5bNotice, body(em5bNotice, func(text, page string) (string, string) {
			pixel := `<img alt="" src="https://fixture.r.` + em5bRegion + `.awstrack.me/I0/fixture" width="1" height="1">`
			return text, strings.Replace(page, "</body>", pixel+"</body>", 1)
		}), nil, []string{"link (html part)", "tracking"}},
		{"the unverified invitation names its business", em5bInviteUnverified, body(em5bInviteUnverified, func(text, page string) (string, string) {
			named := strings.NewReplacer("Hello,", "Hello "+em5bCanaryPerson+",", "Your employer", em5bCanaryTenant)
			return named.Replace(text), named.Replace(page)
		}), nil, []string{"names"}},
		{"the verified invitation names nobody", em5bInviteVerified, body(em5bInviteVerified, func(text, page string) (string, string) {
			neutral := strings.NewReplacer("Hello "+em5bVerifiedPerson+",", "Hello,", em5bVerifiedTenant, "Your employer")
			return neutral.Replace(text), neutral.Replace(page)
		}), nil, []string{"names"}},
		// THE CANARY ALONE (round 4): the neutral words stay where they are, so only the
		// forbidden-name check can fail these — the case above that replaces the neutral
		// words is also caught by the missing-words check and pins nothing on its own.
		{"a canary business name in the unverified invitation, neutral words kept", em5bInviteUnverified,
			body(em5bInviteUnverified, withName(em5bCanaryTenant)), nil, []string{"names"}},
		{"a canary person name in the unverified invitation, neutral words kept", em5bInviteUnverified,
			body(em5bInviteUnverified, withName(em5bCanaryPerson)), nil, []string{"names"}},
		{"a canary name in the verified invitation, both names kept", em5bInviteVerified,
			body(em5bInviteVerified, withName(em5bCanaryTenant)), nil, []string{"names"}},
		{"a verified name in the reset", em5bReset, body(em5bReset, func(text, page string) (string, string) {
			return em5bVerifiedTenant + "\n" + text, page
		}), nil, []string{"names"}},
		{"the SMTP password in the body", em5bReset, body(em5bReset, func(text, page string) (string, string) {
			return text + "\n" + pass + "\n", page
		}), nil, []string{"smtp credentials"}},
		{"a run of the SMTP username in the html", em5bReset, body(em5bReset, func(text, page string) (string, string) {
			return text, strings.Replace(page, "</body>", "<p>q"+user[3:11]+"</p></body>", 1)
		}), nil, []string{"smtp credentials"}},
		{"a value the shape of a real one", em5bReset, body(em5bReset, func(text, page string) (string, string) {
			return text + "\nref " + strings.Repeat("Ab3_", 11) + "\n", page
		}), nil, []string{"link value"}},
		{"credentials not given", em5bReset, unbroken(em5bReset), func(in *em5bInput) { in.pass = "" }, []string{"smtp credentials"}},
	}
	for _, c := range cases {
		in := em5bInput{kind: c.kind, to: to, user: user, pass: pass, sesID: em5bFixtureSESID}
		if c.input != nil {
			c.input(&in)
		}
		rows := em5bCheck([]byte(c.source()), in)
		table := em5bTable(rows)
		got := em5bFailed(rows)
		sort.Strings(got)
		want := append([]string(nil), c.fails...)
		sort.Strings(want)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: FAIL on %q, want exactly %q:\n%s", c.name, got, want, table)
		}
		if na := em5bUnverified(rows); len(na) != 0 {
			t.Errorf("%s: N/A on %q, want none — the source has a receiver verdict:\n%s", c.name, na, table)
		}
		em5bTableIsSafe(t, table, to, user, pass)
	}

	// THE TWO SHAPES THE REAL RUN MET (mail.tm, 2026-10-09).
	noVerdict := func(k em5bKind) func() string {
		return func() string {
			src := good(k)
			out := regexp.MustCompile(`(?s)Authentication-Results:.*?header\.from=taptime\.mt\r\n`).ReplaceAllString(src, "")
			if out == src || strings.Contains(out, "Authentication-Results") {
				t.Fatalf("the %s source's receiver verdict was not removed", k)
			}
			return out
		}
	}
	// mail.tm's /sources export: "--<b>\r\nContent-…" after the blank line becomes
	// "\r\n--<b>Content-…" — the CRLF moved to before the boundary line.
	glue := func(src func() string) func() string {
		return func() string {
			s := src()
			b := regexp.MustCompile(`boundary=([0-9a-f]+)`).FindStringSubmatch(s)
			if b == nil || !strings.Contains(s, "\r\n--"+b[1]+"\r\nContent-") {
				t.Fatal("no first boundary to move the line break of")
			}
			return strings.Replace(s, "\r\n--"+b[1]+"\r\nContent-", "\r\n\r\n--"+b[1]+"Content-", 1)
		}
	}
	measured := []struct {
		name      string
		kind      em5bKind
		source    func() string
		fails, na []string
		notes     []string // the criteria whose notes the table must print
		verdict   string   // what the entry point's (red) verdict must say
		input     func(*em5bInput)
	}{
		{"no Authentication-Results", em5bReset, noVerdict(em5bReset),
			nil, []string{"spf", "dkim taptime.mt", "dmarc"}, []string{"receiver verdict"}, "INCOMPLETE", nil},
		{"the export moved the first boundary's CRLF", em5bReset, glue(unbroken(em5bReset)),
			[]string{"mime", "link (text part)", "link value"}, nil, []string{"mime"}, "3 criteria FAIL", nil},
		{"both, as the mail.tm sources had them", em5bInviteUnverified, glue(noVerdict(em5bInviteUnverified)),
			[]string{"mime", "link (text part)", "names", "link value"}, []string{"spf", "dkim taptime.mt", "dmarc"},
			[]string{"receiver verdict", "mime"}, "not verified by this source (N/A)", nil},
		// Round 4: a source checked without the id the sender printed is tied to no run —
		// an older source with the same fixtures. N/A, INCOMPLETE, red; never PASS.
		{"no 250 id given", em5bReset, unbroken(em5bReset),
			nil, []string{"250 message id"}, []string{"250 message id"}, "INCOMPLETE",
			func(in *em5bInput) { in.sesID = "" }},
	}
	for _, c := range measured {
		in := em5bInput{kind: c.kind, to: to, user: user, pass: pass, sesID: em5bFixtureSESID}
		if c.input != nil {
			c.input(&in)
		}
		rows := em5bCheck([]byte(c.source()), in)
		table := em5bTable(rows)
		for _, x := range []struct {
			what      string
			got, want []string
		}{{"FAIL", em5bFailed(rows), c.fails}, {"N/A", em5bUnverified(rows), c.na}} {
			got, want := append([]string(nil), x.got...), append([]string(nil), x.want...)
			sort.Strings(got)
			sort.Strings(want)
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("%s: %s on %q, want exactly %q:\n%s", c.name, x.what, got, want, table)
			}
		}
		for _, n := range c.notes {
			if !strings.Contains(table, "note ("+n+"): ") {
				t.Errorf("%s: the table prints no note under %q:\n%s", c.name, n, table)
			}
		}
		if v := em5bCheckVerdict(rows); !strings.Contains(v, c.verdict) {
			t.Errorf("%s: the entry point's verdict %q, want it red and saying %q", c.name, v, c.verdict)
		}
		if strings.Contains(table, "verdict: PASS") {
			t.Errorf("%s: the table says PASS:\n%s", c.name, table)
		}
		em5bTableIsSafe(t, table, to, user, pass)
		t.Logf("%s:\n%s", c.name, table)
	}
}

// TestEM5BKit_ReadsItsSettingsFailClosed: an empty environment fails naming every
// required variable (a skip would read as a green run); a bad port, a bad recipient
// and an unknown kind are problems; no problem quotes a value.
func TestEM5BKit_ReadsItsSettingsFailClosed(t *testing.T) {
	t.Parallel()
	none := func(string) string { return "" }
	if _, p := em5bSendEnv(none); len(p) != 5 {
		t.Errorf("an empty environment gave the sender %d problem(s), want 5 (host, username, password, from, to): %q", len(p), p)
	}
	if _, _, p := em5bCheckEnv(none); len(p) != 5 {
		t.Errorf("an empty environment gave the checker %d problem(s), want 5 (source, kind, to, username, password): %q", len(p), p)
	}
	user, pass := relayCredentials(t)
	env := map[string]string{
		em5bEnvHost: "email-smtp.eu-central-1.amazonaws.com", em5bEnvUser: user, em5bEnvPass: pass,
		em5bEnvFrom: em5bFrom, em5bEnvTo: "em5b.recipient@example.test",
		em5bEnvSource: "/nonexistent/em5b.eml", em5bEnvKind: string(em5bNotice),
	}
	get := func(over map[string]string) func(string) string {
		return func(k string) string {
			if v, ok := over[k]; ok {
				return v
			}
			return env[k]
		}
	}
	s, p := em5bSendEnv(get(nil))
	if len(p) != 0 || s.cfg.Port != em5bDefaultPort || s.to != env[em5bEnvTo] || s.cfg.From != em5bFrom || s.cfg.ReplyTo != "" {
		t.Errorf("a complete environment: problems %q, port %d, from %q", p, s.cfg.Port, s.cfg.From)
	}
	if _, in, p := em5bCheckEnv(get(nil)); len(p) != 0 || in.kind != em5bNotice || in.sesID != "" {
		t.Errorf("a complete environment: checker problems %q, kind %q", p, in.kind)
	}
	for name, over := range map[string]map[string]string{
		"implicit TLS port": {em5bEnvPort: "465"},
		"not a port":        {em5bEnvPort: "smtp"},
		"not an address":    {em5bEnvTo: "Someone <em5b.recipient@example.test>"},
		"blank password":    {em5bEnvPass: "   "},
	} {
		_, p := em5bSendEnv(get(over))
		joined := strings.Join(p, " ")
		if len(p) != 1 {
			t.Errorf("%s: %d problem(s), want 1: %q", name, len(p), p)
		}
		// The port's refusal names 465 itself — a constant of the rule, not a value read.
		for _, v := range []string{user, pass, over[em5bEnvTo]} {
			if strings.TrimSpace(v) != "" && strings.Contains(joined, v) {
				t.Errorf("%s: a problem quotes a value: %q", name, p)
			}
		}
	}
	if _, _, p := em5bCheckEnv(get(map[string]string{em5bEnvKind: "welcome"})); len(p) != 1 || strings.Contains(p[0], "welcome") {
		t.Errorf("an unknown kind: %q, want one problem that does not quote it", p)
	}
}
