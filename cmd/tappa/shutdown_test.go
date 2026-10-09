package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler"
)

// shutdownResets resolves every address to the same grants; Consume is not reached.
type shutdownResets struct{ grants []adminauth.ResetGrant }

func (f *shutdownResets) IssueForEmail(context.Context, string) ([]adminauth.ResetGrant, []adminauth.Reset, error) {
	return f.grants, nil, nil
}

// Withdraw: the undelivered grants' links are retired (M10 EM-7C); not what this test
// counts.
func (f *shutdownResets) Withdraw(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (f *shutdownResets) Consume(context.Context, adminauth.ResetToken, string) (adminauth.ConsumedReset, db.ResolvedPasswordReset, error) {
	return adminauth.ConsumedReset{}, db.ResolvedPasswordReset{}, errors.New("not reached in this test")
}

// NoticeRecipient is not reached: no password changes in this test (M10 EM-9).
func (f *shutdownResets) NoticeRecipient(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return "", errors.New("not reached in this test")
}

// shutdownTrail keeps every audit event.
type shutdownTrail struct {
	mu     sync.Mutex
	events []audit.Event
}

func (f *shutdownTrail) Record(_ context.Context, e audit.Event) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return uuid.New(), nil
}

func (f *shutdownTrail) snapshot() []audit.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]audit.Event(nil), f.events...)
}

// stuckRelay is a channel whose send never finishes on its own: it returns only when
// its context ends — the shape of a relay that accepted the connection and stopped
// answering, against a transport that honours its context (internal/mail does).
type stuckRelay struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
}

func (c *stuckRelay) DeliverReset(ctx context.Context, _ handler.ResetDelivery) error {
	c.mu.Lock()
	c.calls++
	if c.calls == 1 {
		close(c.started)
	}
	c.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

// DeliverPasswordNotice is not reached: no password changes in this test (M10 EM-9).
func (c *stuckRelay) DeliverPasswordNotice(context.Context, handler.PasswordNotice) error {
	return errors.New("not reached in this test")
}

// RefusingResets: this relay has no breaker in front of it (M10 EM-7A).
func (c *stuckRelay) RefusingResets() bool { return false }

func (c *stuckRelay) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestShutdown_DrainsTheResetOutboxAlongsideTheHTTPServer is ADR 0022 §6.6(b)'s pin:
// the shutdown sequence, driven with a request IN FLIGHT and a send STUCK in the
// outbox, (1) starts Shutdown AT ONCE — the listener refuses new connections within
// listenerRefusalBound of the call — and (2) finishes in less than the two waits would
// take back to back.
//
// 🔴 TWO ASSERTS, BECAUSE THERE ARE TWO SEQUENTIAL ORDERS AND THE CLOCK SEES ONLY ONE
// (M10 EM-5A, 2nd audit round). With a request of T in flight:
//
//	concurrent            ≈ max(T, D)          listener closed at once
//	Shutdown, then drain  ≥ T + D              caught by the total-time assert
//	drain, then Shutdown  ≈ max(T, D) as well — the request in flight runs DURING the
//	                      drain — so the total-time assert passes it (measured: 5/5
//	                      green, 2.000–2.002 s); but the listener stays open for D.
//	                      Caught by the refusal assert.
//
// In production the third order is the costly one: the worst sequence is D + the HTTP
// grace + the encode store's close (3 + 20 + 5 = 28 s against a kill at 30), and for D
// the listener keeps accepting recovery requests whose grants the closed outbox can
// only record undelivered.
//
// WHY A REQUEST MUST BE IN FLIGHT (ADR 0022 B34): with nothing in flight Shutdown
// returns at once, so EVERY order costs about D and the total-time assert separates
// nothing.
//
// THE NUMBERS, so a reader can re-derive them: T = 2 s. D is the drain as it runs
// against a stuck send — sends stop at handler.ResetDrainGrace minus
// handler.ResetDrainWriteReserve (2 s today), and the unsent rows are then written at
// once (the trail here is in memory). Shutdown polls idle connections at up to 500 ms
// plus 10% jitter (net/http's shutdownPollIntervalMax, ADR 0022 B35), so the correct
// sequence takes up to about T + 550 ms ≈ 2.55 s; the threshold is T + D − 600 ms
// = 3.4 s; Shutdown-then-drain takes at least T + D ≈ 4 s. Shutdown closes its
// listeners as its first act, so a correct sequence refuses a new connection within
// milliseconds (measured: see the log line); drain-then-Shutdown refuses only after D.
// The bound, 300 ms, is far above the first and far below the second.
//
// AND THE OUTCOME IS COUNTED, not only timed: three grants were queued, the first one
// stuck in the relay; all three end with exactly one undelivered row, the relay saw
// exactly one call (the other two were never sent), and the request in flight was
// answered.
//
// WHAT IT DOES NOT CATCH (ADR 0022 §6.6, İddia F): a wait added in run() OUTSIDE this
// function; an extra wait shorter than the margin; a wait paid only in a state this
// test does not build.
func TestShutdown_DrainsTheResetOutboxAlongsideTheHTTPServer(t *testing.T) {
	const (
		inFlight = 2 * time.Second
		margin   = 600 * time.Millisecond
		// listenerRefusalBound: how soon after shutdown() is called the listener must
		// refuse a new connection. Measured on the correct sequence in the log line
		// above (a few milliseconds); drain-then-Shutdown needs the drain's 2 s.
		listenerRefusalBound = 300 * time.Millisecond
	)
	drainTime := handler.ResetDrainGrace - handler.ResetDrainWriteReserve
	threshold := inFlight + drainTime - margin

	grant := func() adminauth.ResetGrant {
		// A value drawn at run time, in the shape adminauth mints (32 bytes, unpadded
		// base64url): no credential-shaped literal sits in the repository.
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			t.Fatal(err)
		}
		return adminauth.ResetGrant{
			Recipient: "owner@shutdown.example.test",
			Issued: adminauth.IssuedReset{
				Reset: adminauth.Reset{
					ID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New(),
					CreatedAt: time.Now(), ExpiresAt: time.Now().Add(adminauth.ResetTTL),
				},
				Token: adminauth.ParseResetToken(base64.RawURLEncoding.EncodeToString(raw)),
			},
		}
	}
	grants := []adminauth.ResetGrant{grant(), grant(), grant()}
	relay := &stuckRelay{started: make(chan struct{})}
	trail := &shutdownTrail{}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	cfg := &config.Config{Env: config.EnvDev, BaseURL: base, SessionHMACKey: []byte("0123456789abcdef0123456789abcdef")}
	reset, err := handler.NewAdminReset(&shutdownResets{grants: grants}, relay, trail, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	reset.Mount(r)
	slowStarted := make(chan struct{})
	r.Get("/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(slowStarted)
		time.Sleep(inFlight)
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Handler: r, ReadHeaderTimeout: 5 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	// QUEUE THE THREE GRANTS through the real form, and wait for the worker to be
	// stuck on the first.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	page, err := client.Get(base + "/admin/reset")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(page.Body)
	_ = page.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	const marker = `name="csrf" value="`
	i := strings.Index(string(body), marker)
	if i < 0 {
		t.Fatalf("no csrf field on the request form:\n%s", body)
	}
	csrf, _, _ := strings.Cut(string(body)[i+len(marker):], `"`)
	req, err := http.NewRequest(http.MethodPost, base+"/admin/reset",
		strings.NewReader(url.Values{"csrf": {csrf}, "email": {"owner@shutdown.example.test"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", base)
	posted, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, posted.Body)
	_ = posted.Body.Close()
	if posted.StatusCode != http.StatusOK {
		t.Fatalf("POST /admin/reset answered %d, want 200", posted.StatusCode)
	}
	select {
	case <-relay.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never reached the relay; the drain below would have nothing to wait for")
	}

	// A REQUEST IN FLIGHT for T.
	slowStatus := make(chan int, 1)
	go func() {
		res, err := client.Get(base + "/slow")
		if err != nil {
			slowStatus <- 0
			return
		}
		_ = res.Body.Close()
		slowStatus <- res.StatusCode
	}()
	select {
	case <-slowStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the in-flight request never started")
	}

	addr := ln.Addr().String()
	start := time.Now()
	finished := make(chan error, 1)
	go func() { finished <- shutdown(srv, 10*time.Second, reset, handler.ResetDrainGrace) }()
	// (1) THE LISTENER: dial until a connection is refused. A dial that still gets
	// through is closed at once, so it holds nothing open for Shutdown to wait on.
	refusedAfter := time.Duration(-1)
	for time.Since(start) < 2*drainTime {
		c, derr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if derr != nil {
			refusedAfter = time.Since(start)
			break
		}
		_ = c.Close()
		time.Sleep(5 * time.Millisecond)
	}
	err = <-finished
	took := time.Since(start)
	if err != nil {
		t.Errorf("shutdown returned %v; both drains had room to finish inside their budgets", err)
	}
	t.Logf("listener refused a new connection after %v (bound %v); shutdown took %v with a %v request in flight "+
		"and a %v drain; threshold %v (Shutdown-then-drain would be >= %v, drain-then-Shutdown keeps the "+
		"listener open for ~%v)", refusedAfter, listenerRefusalBound, took, inFlight, drainTime, threshold,
		inFlight+drainTime, drainTime)
	if refusedAfter < 0 || refusedAfter > listenerRefusalBound {
		t.Errorf("the listener still accepted connections %v after shutdown() was called (refused after %v, bound %v): "+
			"Shutdown did not start at once — the outbox is drained BEFORE the HTTP server rather than beside "+
			"it, which keeps taking requests for the drain's whole budget and adds it to the kill budget",
			listenerRefusalBound, refusedAfter, listenerRefusalBound)
	}
	// (2) THE TOTAL.
	if took >= threshold {
		t.Errorf("the shutdown sequence took %v, at or above T + D - margin = %v: the reset outbox is drained "+
			"AFTER the HTTP server rather than beside it (or something else in the sequence waits). In "+
			"production that adds handler.ResetDrainGrace to a kill budget nothing else counts it in.",
			took, threshold)
	}
	if took < drainTime-200*time.Millisecond {
		t.Errorf("the shutdown sequence took %v, well under the drain's send phase (%v): the drain did not "+
			"wait for the stuck send, so this timing measured nothing", took, drainTime)
	}

	if got := <-slowStatus; got != http.StatusOK {
		t.Errorf("the request in flight ended with %d, want 200 — Shutdown is meant to wait for it", got)
	}
	if n := relay.count(); n != 1 {
		t.Errorf("the relay saw %d send(s), want 1: the stuck first one, and nothing after the drain stopped sending", n)
	}
	rows := map[uuid.UUID]int{}
	for _, e := range trail.snapshot() {
		if e.Action != handler.ActionAdminResetUndelivered {
			t.Errorf("a %q row; every grant here was left unsent", e.Action)
			continue
		}
		rows[uuid.MustParse(e.Target)]++
	}
	for _, g := range grants {
		if n := rows[g.Issued.Reset.AdminUserID]; n != 1 {
			t.Errorf("grant for admin %s ended with %d undelivered row(s), want exactly 1", g.Issued.Reset.AdminUserID, n)
		}
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("Serve returned %v, want http.ErrServerClosed", err)
	}
}

// TestArtifact_BootsAndStopsWithTheResetEmailChannel drives THE SHIPPED BINARY with
// TAPPA_RESET_DELIVERY=email (M10 EM-5): it boots past the delivery refusal, the
// recovery form is the deliverable one — so run()'s ResetDeliveryEmail case built a
// channel rather than leaving it nil — a request is answered with "Check your email",
// and on SIGTERM the process runs its shutdown sequence (the outbox drain included)
// and exits 0. Nothing it prints carries the SMTP credentials it was given.
//
// The relay address is a port nothing listens on and the address asked for is not
// registered, so no send is attempted: the real relay is EM-5B's.
func TestArtifact_BootsAndStopsWithTheResetEmailChannel(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the shipped-artifact test (real Postgres required)")
	}
	bin := theArtifact(t)
	addr := freeAddr(t)
	relayHost, relayPort, err := net.SplitHostPort(freeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	creds := config.SMTPCredentialVariables()
	// The sentinels TestArtifact_RefusesAnEmailDeliveryThisBuildLacks uses: no 4
	// consecutive characters of either form a word.
	const user, pass = "jkq4wvz8xqp2zkv6", "qzx7vjw9kpq3xzt8"
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"DATABASE_URL=" + dsn,
		"TAPPA_ADDR=" + addr,
		"TAPPA_ENV=dev",
		"TAPPA_BASE_URL=http://" + addr,
		"TAPPA_SESSION_HMAC_KEY=" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("S"), 32)),
		"TAPPA_TAG_KEK=" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("K"), 32)),
		"TAPPA_INVITE_HMAC_KEY=" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("I"), 32)),
		"TAPPA_RETENTION_YEARS=2",
		"TAPPA_RESET_DELIVERY=" + config.ResetDeliveryEmail,
		"TAPPA_INVITE_DELIVERY=" + config.InviteDeliveryPanel,
		"TAPPA_SMTP_HOST=" + relayHost,
		"TAPPA_SMTP_PORT=" + relayPort,
		"TAPPA_MAIL_FROM=Taptime <no-reply@taptime.mt>",
		creds[0] + "=" + user,
		creds[1] + "=" + pass,
	}
	var out lockedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the artifact: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	})

	base := "http://" + addr
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	var form string
	for time.Now().Before(deadline) {
		res, err := client.Get(base + "/admin/reset")
		if err == nil {
			b, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				form = string(b)
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if form == "" {
		t.Fatalf("the artifact never served the recovery form.\nIts output was:\n%s", out.String())
	}
	if strings.Contains(form, "cannot send email yet") {
		t.Errorf("with TAPPA_RESET_DELIVERY=email the recovery form says it cannot send: run() left the channel nil")
	}
	const marker = `name="csrf" value="`
	i := strings.Index(form, marker)
	if i < 0 {
		t.Fatalf("no csrf field on the recovery form")
	}
	csrf, _, _ := strings.Cut(form[i+len(marker):], `"`)
	req, err := http.NewRequest(http.MethodPost, base+"/admin/reset",
		strings.NewReader(url.Values{"csrf": {csrf}, "email": {"nobody-" + uuid.NewString() + "@artifact.example.test"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", base)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	answer, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(answer), "Check your email") {
		t.Errorf("POST /admin/reset answered %d without the deliverable page", res.StatusCode)
	}

	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Errorf("the process exited with %v after SIGTERM, want 0 — its shutdown sequence (HTTP and "+
				"the reset outbox) did not end cleanly:\n%s", err, out.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("the process did not exit within 30 s of SIGTERM:\n%s", out.String())
	}
	logged := out.String()
	if !strings.Contains(logged, "shutting down") || strings.Contains(logged, "fatal") {
		t.Errorf("the output does not show a clean shutdown:\n%s", logged)
	}
	for _, cred := range []string{user, pass} {
		for i := 0; i+4 <= len(cred); i++ {
			if strings.Contains(logged, cred[i:i+4]) {
				t.Errorf("the process printed 4 bytes of a credential it was given (offset %d)", i)
				break
			}
		}
	}
}
