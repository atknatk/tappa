package main

import (
	"bytes"
	"context"
	stddebug "debug/buildinfo"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/buildinfo"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/httpx"
)

// serving_test.go — "the deployment is one file", tested by taking the files away.

// TestPackaging_AssetsAndTemplatesComeFromTheBinary runs the router with the
// working directory moved somewhere that contains NO web/ tree at all.
//
// 🔴 THE POSITIVE CONTROL IS THE FIRST ASSERTION AND IT IS THE WHOLE TEST: it proves
// the directory really is empty of the assets, so serving them afterwards can only
// have come from the embed. Without it this passes on a machine sitting in the repo.
//
// It needs no database, so it runs everywhere. TestArtifact_ServesFromAnEmptyWorkingDirectory
// below is the same statement about the SHIPPED BINARY and needs one.
func TestPackaging_AssetsAndTemplatesComeFromTheBinary(t *testing.T) {
	t.Chdir(t.TempDir())

	// CONTROL: from here, the asset does not exist on disk.
	if _, err := os.ReadFile("web/static/js/tap.js"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the working directory still reaches the repository (%v); this test would prove nothing", err)
	}

	marketing := handler.NewMarketing(nil, slog.New(slog.DiscardHandler))
	srv := httptest.NewServer(httpx.NewRouter(&config.Config{}, marketing))
	t.Cleanup(srv.Close)

	get := func(path string) (int, string) {
		t.Helper()
		res, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}

	// A static asset, from web/embed.go's embed.FS.
	code, body := get("/static/js/tap.js")
	if code != http.StatusOK || !strings.Contains(body, "getCurrentPosition") {
		t.Errorf("GET /static/js/tap.js = %d (%d bytes) with no browser geolocation call; the asset did not come from the binary", code, len(body))
	}
	// A rendered page, from templ-compiled Go.
	code, body = get("/")
	if code != http.StatusOK || !strings.Contains(body, "<html") {
		t.Errorf("GET / = %d (%d bytes); templates are compiled into the binary and must render with no files present", code, len(body))
	}
}

// TestArtifact_ServesFromAnEmptyWorkingDirectory runs THE REAL BINARY.
//
// 🔴 IT IS THE ONLY TEST THAT PROVES /readyz IS MOUNTED IN THE SHIPPED PRODUCT.
// Everything in internal/handler drives a router the test itself assembled; this
// drives the wiring in main.go. That distinction is not academic here — M7-01
// shipped an approved, tested capability that no route reached, and this repository
// has since made "was it mounted?" a question tests answer.
//
// It also measures, on the artifact rather than on a unit:
//   - the start-up line names the commit, and names the SAME commit the binary's
//     build info carries (two independent readings);
//   - a modified tree is announced at WARN;
//   - the process starts and serves with no repository on disk;
//   - nothing it prints contains the key material it was configured with.
func TestArtifact_ServesFromAnEmptyWorkingDirectory(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the shipped-artifact test (real Postgres required)")
	}
	bin := theArtifact(t)

	// The binary's own account of itself, read independently of the process.
	info, err := stddebug.ReadFile(bin)
	if err != nil {
		t.Fatalf("reading build info: %v", err)
	}
	var wantRevision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			wantRevision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if wantRevision == "" {
		t.Fatal("the artifact carries no revision, so the log assertion below would be vacuous")
	}

	addr := freeAddr(t)
	// Obviously fake, distinct 32-byte keys (agent-brief madde 2). They must differ:
	// config.Load refuses equal session and invite keys.
	sessionKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("S"), 32))
	tagKEK := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("K"), 32))
	inviteKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("I"), 32))

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	// 🔴 AN EMPTY DIRECTORY: no web/, no templates, no .env. If the deployment needed
	// a file next to the binary, it would fail here.
	cmd.Dir = t.TempDir()
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"DATABASE_URL=" + dsn,
		"TAPPA_ADDR=" + addr,
		"TAPPA_ENV=dev",
		"TAPPA_BASE_URL=http://" + addr,
		"TAPPA_SESSION_HMAC_KEY=" + sessionKey,
		"TAPPA_TAG_KEK=" + tagKEK,
		"TAPPA_INVITE_HMAC_KEY=" + inviteKey,
		"TAPPA_RETENTION_YEARS=2",
	}
	var out lockedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	// Its own process group, so the cleanup below cannot leave an orphan holding the
	// port (agent-brief madde 5).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the artifact: %v", err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _, _ = cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(25 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	t.Cleanup(stop)

	client := &http.Client{Timeout: 5 * time.Second}
	base := "http://" + addr
	deadline := time.Now().Add(30 * time.Second)
	var live bool
	for time.Now().Before(deadline) {
		res, err := client.Get(base + "/healthz")
		if err == nil {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			live = res.StatusCode == http.StatusOK
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !live {
		t.Fatalf("the artifact never answered /healthz on %s.\nIts output was:\n%s", addr, out.String())
	}

	// The four surfaces a deployment depends on, through the REAL wiring.
	for _, tc := range []struct {
		path     string
		wantCode int
		wantBody string
	}{
		{"/healthz", http.StatusOK, "ok"},
		{"/readyz", http.StatusOK, "ready"},
		{"/static/js/tap.js", http.StatusOK, "getCurrentPosition"},
		{"/", http.StatusOK, "<html"},
	} {
		res, err := client.Get(base + tc.path)
		if err != nil {
			t.Errorf("GET %s: %v", tc.path, err)
			continue
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != tc.wantCode {
			t.Errorf("GET %s = %d, want %d", tc.path, res.StatusCode, tc.wantCode)
		}
		if !strings.Contains(string(body), tc.wantBody) {
			t.Errorf("GET %s did not contain %q (%d bytes)", tc.path, tc.wantBody, len(body))
		}
	}

	// HEAD, from the clients that watch these two URLs.
	for _, path := range []string{"/healthz", "/readyz"} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodHead, base+path, nil)
		if err != nil {
			t.Fatalf("building HEAD %s: %v", path, err)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Errorf("HEAD %s: %v", path, err)
			continue
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("HEAD %s = %d, want 200 — this is what an uptime monitor sends", path, res.StatusCode)
		}
	}

	stop()
	logged := out.String()
	t.Logf("the artifact's start-up output:\n%s", logged)

	// THE COMMIT IS SURFACED, and it is the same one the binary carries.
	if !strings.Contains(logged, "revision="+wantRevision) {
		t.Errorf("the start-up output does not name the artifact's own revision (%s):\n%s", wantRevision, logged)
	}
	if modified == "true" && !regexp.MustCompile(`level=WARN[^\n]*MODIFIED`).MatchString(logged) {
		t.Errorf("this artifact was built from a modified tree and did not say so at WARN:\n%s", logged)
	}
	if modified == "false" && !regexp.MustCompile(`level=INFO[^\n]*msg=build`).MatchString(logged) {
		t.Errorf("this artifact was built from a clean tree and did not log its build at INFO:\n%s", logged)
	}
	// ⚠️ AND IT SAYS NOTHING ELSE. A start-up line is the easiest place in a product
	// to print a configuration dump; §4.7 forbids exactly that.
	for name, secret := range map[string]string{
		"the session key": sessionKey,
		"the tag KEK":     tagKEK,
		"the invite key":  inviteKey,
		"the DSN":         dsn,
	} {
		if strings.Contains(logged, secret) {
			t.Errorf("the process printed %s", name)
		}
	}
}

// TestArtifact_SaysWhatItIsEVENWhenTheBootFails.
//
// 🔴 THE ORDER OF TWO LINES IN main.go CARRIES THIS, AND NOTHING HELD IT: an audit
// moved logBuild below db.New and every test stayed green. The comment there says
// the identity is printed first "because the moment somebody most needs to know
// which commit is running is the moment the boot FAILS" — so this drives exactly
// that moment. The database is a closed port, and the whole output of the process
// is two lines: the identity, and the failure. Move the identity below the dial and
// the only line left names no build at all.
//
// It needs no database, so it runs everywhere the artifact can be built.
func TestArtifact_SaysWhatItIsEVENWhenTheBootFails(t *testing.T) {
	bin := theArtifact(t)
	info, err := stddebug.ReadFile(bin)
	if err != nil {
		t.Fatalf("reading build info: %v", err)
	}
	var wantRevision string
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			wantRevision = s.Value
		}
	}
	if wantRevision == "" {
		t.Fatal("the artifact carries no revision, so this assertion would be vacuous")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		// Port 1 on the loopback: refused immediately, so the boot fails in
		// milliseconds rather than on db.New's ten second dial budget.
		"DATABASE_URL=postgres://tappa_app:pw@127.0.0.1:1/tappa?sslmode=disable",
		"TAPPA_ADDR=127.0.0.1:0",
		"TAPPA_ENV=dev",
		"TAPPA_SESSION_HMAC_KEY=" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("S"), 32)),
		"TAPPA_TAG_KEK=" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("K"), 32)),
		"TAPPA_INVITE_HMAC_KEY=" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("I"), 32)),
		"TAPPA_RETENTION_YEARS=2",
	}
	out, err := cmd.CombinedOutput()
	t.Logf("a boot that could not reach its database said:\n%s", out)

	if err == nil {
		t.Fatal("the process exited 0 with an unreachable database; this test is not measuring a failed boot")
	}
	if !strings.Contains(string(out), "revision="+wantRevision) {
		t.Errorf("a FAILED boot printed no build identity (looking for revision=%s).\n"+
			"The identity must be logged BEFORE the database dial: a process that cannot start is exactly "+
			"the one nobody can identify from a running port.\nIts whole output was:\n%s", wantRevision, out)
	}
	// And the failure itself is still reported — the identity line must not have
	// replaced the diagnosis.
	if !strings.Contains(string(out), "fatal") {
		t.Errorf("the boot failure was not reported at all:\n%s", out)
	}
}

// freeAddr reserves a loopback port and releases it.
//
// ⚠️ NOT :8080. On this machine an unrelated container holds it, and a test that
// assumed a port would fail for a reason that has nothing to do with the product.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("releasing the port: %v", err)
	}
	return addr
}

// lockedBuffer is a bytes.Buffer safe for the reader and the child's writer.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// TestLogBuild_SaysWhichCommitAndHowTrustworthy — the unit behind the artifact
// assertion above.
func TestLogBuild_SaysWhichCommitAndHowTrustworthy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		build     buildinfo.Build
		wantLevel string
	}{
		{
			name:      "a clean tree is ordinary news",
			build:     buildinfo.Build{Version: "v1", Revision: "abc123", Time: "t", Go: "go1.26.5"},
			wantLevel: "level=INFO",
		},
		{
			// A binary built from a dirty tree cannot be traced to any commit, and a
			// production process saying so must be filterable.
			name:      "a modified tree is a warning",
			build:     buildinfo.Build{Version: "v1", Revision: "abc123", Time: "t", Go: "go1.26.5", Modified: true},
			wantLevel: "level=WARN",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			logBuild(slog.New(slog.NewTextHandler(&buf, nil)), tc.build)
			got := buf.String()
			if !strings.Contains(got, tc.wantLevel) {
				t.Errorf("logged %q, want %s", got, tc.wantLevel)
			}
			if !strings.Contains(got, "revision=abc123") {
				t.Errorf("the line does not carry the revision: %q", got)
			}
			if !strings.Contains(got, fmt.Sprintf("modified=%v", tc.build.Modified)) {
				t.Errorf("the line does not carry modified=%v: %q", tc.build.Modified, got)
			}
		})
	}
}
