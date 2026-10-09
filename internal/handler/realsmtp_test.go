//go:build realsmtp

package handler

// realsmtp_test.go — M10 EM-5B's two entry points against the REAL relay (AWS SES).
//
// 🔴 BEHIND A BUILD TAG, ON PURPOSE: without -tags realsmtp this file does not even
// compile, so no `go test ./...`, no CI run and no `make check` can send an e-mail.
// With the tag, each test FAILS — never skips — when a setting it needs is missing, so
// an empty run cannot read as a green one. Everything they call lives in
// realsmtpkit_test.go, which is compiled and self-tested on every run (it says why).
//
// THE SENDER (orchestrator's run; deploy/README.md, "EM-5B ölçümü"):
//
//	go test -tags realsmtp -count=1 -v -run '^TestEM5B_SendToTheRealRelay$' ./internal/handler
//
// reads TAPPA_SMTP_HOST, TAPPA_SMTP_PORT (empty = 587), TAPPA_SMTP_USERNAME,
// TAPPA_SMTP_PASSWORD, TAPPA_MAIL_FROM, TAPPA_MAIL_REPLY_TO (optional) and
// TAPPA_REALSMTP_TO, sends the four fixtures to that one address through the
// production chain a second apart, then one recovery link with a wrong password, and
// prints one line per send: kind, sent or refused, message_id, class and smtp_code.
//
// THE CHECKER, once per received source:
//
//	go test -tags realsmtp -count=1 -v -run '^TestEM5B_CheckAReceivedSource$' ./internal/handler
//
// reads TAPPA_REALSMTP_EML (the .eml file), TAPPA_REALSMTP_KIND (reset | notice |
// invite-verified | invite-unverified), TAPPA_REALSMTP_TO, TAPPA_SMTP_USERNAME and
// TAPPA_SMTP_PASSWORD (scanned for, never printed), TAPPA_MAIL_REPLY_TO (optional) and
// TAPPA_REALSMTP_SES_ID (the message_id the sender printed for that message; without
// it the row that ties the source to this run is N/A and the run INCOMPLETE, red), and
// prints the criteria table.
//
// Neither prints the address, a credential or a body; neither writes a file.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestEM5B_SendToTheRealRelay is EM-5B's send. It talks to the relay the environment
// names — with the system's root certificates, as production does (no custom pool,
// ADR 0022 §2) — and its verdict is the kit's: four fixtures sent, each with a message
// id that survived internal/mail's shape and echo rules; the wrong password refused as
// class auth; the channels' log within ADR 0022 §10 and free of the address, the
// links, the names and the credentials.
func TestEM5B_SendToTheRealRelay(t *testing.T) {
	s, problems := em5bSendEnv(os.Getenv)
	if len(problems) > 0 {
		t.Fatalf("EM-5B send: %s", strings.Join(problems, "; "))
	}
	probe, probePass := em5bProbeConfig(t, s.cfg)
	run, err := em5bSend(context.Background(), s.cfg, probe, s.to, time.Second)
	if err != nil {
		// A construction failure: mail.New's errors name a field, the channels' wrap
		// the e-mail package's sentinels — none quotes a value.
		t.Fatalf("EM-5B send: %v", err)
	}
	for _, o := range run.outcomes {
		t.Log(o.line())
	}
	if p := em5bSendProblems(run, s, probePass); len(p) > 0 {
		t.Errorf("EM-5B send, %d problem(s):\n  %s", len(p), strings.Join(p, "\n  "))
	}
}

// TestEM5B_CheckAReceivedSource is EM-5B's reading of one received e-mail: the table,
// and a red result naming every criterion that failed — or, when nothing failed but a
// criterion is N/A (a receiver that wrote no verdict), naming those: an unverified
// reading never ends green (em5bCheckVerdict).
func TestEM5B_CheckAReceivedSource(t *testing.T) {
	path, in, problems := em5bCheckEnv(os.Getenv)
	if len(problems) > 0 {
		t.Fatalf("EM-5B check: %s", strings.Join(problems, "; "))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("EM-5B check: the file %s names cannot be read", em5bEnvSource)
	}
	rows := em5bCheck(raw, in)
	t.Logf("EM-5B check, %s:\n%s", in.kind, em5bTable(rows))
	if v := em5bCheckVerdict(rows); v != "" {
		t.Errorf("EM-5B check, %s: %s", in.kind, v)
	}
}
