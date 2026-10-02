package db

// operatorrefusal_test.go -- M10 OP-7, 2nd round: which failures to open the operator's
// pool are "unreachable" (the customer product boots, the surface is unavailable) and
// which are a REFUSAL (the boot stops); the pin against a differently cased DSN key; the
// read-back's one accepted text; the rest of ADR 0021 §1's role description; and the
// customer pool's switched-session and parse-error fixes (F1, F2).

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atknatk/tappa/internal/config"
)

// unreachableByContract is the coordinator's decision for the 2nd round, written out
// here as a LITERAL so the production table cannot drift from it in either direction:
// a two-character entry is a whole SQLSTATE class, five characters one code.
var unreachableByContract = []string{"08", "28", "3D000", "53300", "57P01", "57P02", "57P03"}

func contractSaysUnreachable(code string) bool {
	for _, u := range unreachableByContract {
		if code == u || (len(u) == 2 && strings.HasPrefix(code, u)) {
			return true
		}
	}
	return false
}

// sqlstateUniverse is a broad sample of SQLSTATEs: every class PostgreSQL defines,
// crossed with subcodes that occur in its tables, plus the five the 1st auditor drove
// live. It is a SAMPLE -- the space of five-character codes is not enumerable -- so the
// property it pins is "the production table and the literal above agree on every one of
// these", which catches an entry added to or dropped from either.
func sqlstateUniverse() []string {
	classes := []string{"00", "01", "02", "03", "08", "09", "0A", "0B", "0F", "0L", "0P", "0Z", "20", "21", "22",
		"23", "24", "25", "26", "27", "28", "2B", "2D", "2F", "34", "38", "39", "3B", "3D", "3F", "40", "42", "44",
		"53", "54", "55", "57", "58", "72", "F0", "HV", "P0", "XX"}
	subs := []string{"000", "001", "002", "003", "004", "006", "007", "300", "400", "P01", "P02", "P03", "P04", "P05"}
	var out []string
	for _, c := range classes {
		for _, s := range subs {
			out = append(out, c+s)
		}
	}
	return append(out, "42501", "22023", "42704", "28P01", "3D000", "53300", "57014")
}

// TestOperatorConnectErr_OnlyTheTableIsUnreachable pins B1's contract on the function
// the ping's error goes through: a SQLSTATE in the table is unreachability, one outside
// it is a refusal; network, name resolution, timeouts, a cancelled start-up and a
// closed connection are unreachability; an unknown error type and the pin's own refusal
// are not. And operatorStepErr -- every step after the ping -- is NEVER unreachability,
// whatever the code (B5d).
func TestOperatorConnectErr_OnlyTheTableIsUnreachable(t *testing.T) {
	// The production table and the literal agree, entry by entry.
	if len(unreachableSQLSTATEs) != len(unreachableByContract) {
		t.Fatalf("the production table has %d entries, the contract %d", len(unreachableSQLSTATEs), len(unreachableByContract))
	}
	for i, u := range unreachableSQLSTATEs {
		if u.code != unreachableByContract[i] || strings.TrimSpace(u.why) == "" {
			t.Errorf("table entry %d is %q (why %q), the contract says %q with a reason", i, u.code, u.why, unreachableByContract[i])
		}
	}
	checked := 0
	for _, code := range sqlstateUniverse() {
		pgErr := &pgconn.PgError{Code: code, Message: "FAKEmessage", Detail: "FAKEdetail"}
		for _, wrapped := range []error{pgErr, fmt.Errorf("connect: %w", pgErr)} {
			got := operatorConnectErr(wrapped)
			if errors.Is(got, ErrOperatorUnreachable) != contractSaysUnreachable(code) {
				t.Errorf("SQLSTATE %s at connect: unreachable=%v, the contract says %v", code,
					errors.Is(got, ErrOperatorUnreachable), contractSaysUnreachable(code))
			}
			if errors.Is(operatorStepErr("read its role", wrapped), ErrOperatorUnreachable) {
				t.Errorf("SQLSTATE %s after the ping is marked unreachable; a reached server's failure fails closed", code)
			}
			if strings.Contains(got.Error(), "FAKE") {
				t.Errorf("SQLSTATE %s: the error carries the server's message or detail: %q", code, got)
			}
			checked++
		}
	}
	if checked < 500 {
		t.Fatalf("only %d codes checked; the universe has gone blind", checked)
	}

	for _, tc := range []struct {
		name        string
		err         error
		unreachable bool
		sentinel    error
	}{
		{"a deadline", fmt.Errorf("dial: %w", context.DeadlineExceeded), true, context.DeadlineExceeded},
		{"a cancelled start-up", context.Canceled, true, context.Canceled},
		{"connection refused (errno)", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, true, nil},
		{"name resolution", &net.DNSError{Err: "no such host", Name: "x"}, true, nil},
		{"a network error with no errno", &net.OpError{Op: "read", Net: "tcp", Err: errors.New("x")}, true, nil},
		{"the server closed the connection", fmt.Errorf("read: %w", io.ErrUnexpectedEOF), true, nil},
		{"an unknown error type", errors.New("server refused TLS connection"), false, nil},
		{"the pin's own refusal", &logParameterNotPinnedError{Got: "-1"}, false, nil},
	} {
		got := operatorConnectErr(tc.err)
		if errors.Is(got, ErrOperatorUnreachable) != tc.unreachable {
			t.Errorf("%s at connect: unreachable=%v, want %v (%v)", tc.name, errors.Is(got, ErrOperatorUnreachable), tc.unreachable, got)
		}
		if tc.sentinel != nil && !errors.Is(got, tc.sentinel) {
			t.Errorf("%s: the context sentinel is lost: %v", tc.name, got)
		}
		if errors.Is(operatorStepErr("read its role", tc.err), ErrOperatorUnreachable) {
			t.Errorf("%s after the ping is marked unreachable", tc.name)
		}
	}
}

// errRow is a pgx.Row whose Scan fails with err.
type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

type errQuerier struct{ err error }

func (q errQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return errRow(q) }

// TestReadOperatorRole_AFailureIsNeverUnreachability drives the role read -- the step
// after the ping -- with codes the TABLE calls unreachable (a lost connection, a
// shutdown, an authentication failure): through this step they are refusals, because
// the server was reached. B5d's mutant (marking this step unreachable) is red here.
func TestReadOperatorRole_AFailureIsNeverUnreachability(t *testing.T) {
	for _, code := range []string{"08006", "57P01", "28P01", "53300"} {
		_, err := readOperatorRoleOn(context.Background(), errQuerier{&pgconn.PgError{Code: code}})
		if err == nil {
			t.Fatalf("%s: the role read did not fail", code)
		}
		if errors.Is(err, ErrOperatorUnreachable) {
			t.Errorf("SQLSTATE %s during the role read is marked unreachable; the boot would go on", code)
		}
	}
}

// TestOperatorDB_AServerThatRefusesStopsTheBoot is B1's five live cases, driven against
// the real server through the EXPORTED constructor: each one is a server that WAS
// reached and refused what the DSN asked for, so it must be a refusal (not
// ErrOperatorUnreachable) naming its SQLSTATE. Sentinels sit in the parts of the DSN the
// server echoes back in its message (a role name, a setting's name); the refusal's text
// must carry none of them (B5a: the reason is a code, never the server's message).
func TestOperatorDB_AServerThatRefusesStopsTheBoot(t *testing.T) {
	app, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	sentinel := "zz" + randHex(t, 8)
	for _, tc := range []struct{ name, dsn, sqlstate string }{
		{"the customer DSN asking to become tappa_operator", withParam(app, "role", "tappa_operator"), "42501"},
		{"the customer DSN setting a superuser setting through options",
			withParam(app, "options", "-c log_parameter_max_length=-1"), "42501"},
		{"the same setting as a query parameter", withParam(app, "log_parameter_max_length", "-1"), "42501"},
		{"the owner's DSN asking for a role that does not exist", withParam(owner, "role", sentinel), "22023"},
		{"an unknown setting", withParam(owner, sentinel, "1"), "42704"},
		// 2b: a multi-host DSN whose first host is a closed port. pgx joins the two
		// attempts' errors; the refusal in the second must not hide behind the first.
		{"a closed port, then the customer DSN asking to become tappa_operator",
			withParam(behindAClosedPort(t, app), "role", "tappa_operator"), "42501"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := NewOperatorDB(ctx, &config.Config{OperatorDatabaseURL: tc.dsn})
			if err == nil {
				o.Close()
				t.Fatal("opened")
			}
			if errors.Is(err, ErrOperatorUnreachable) {
				t.Fatalf("a server that refused (%v) is marked unreachable: the boot would go on", err)
			}
			if !strings.Contains(err.Error(), "SQLSTATE "+tc.sqlstate) {
				t.Errorf("the refusal does not name SQLSTATE %s: %v", tc.sqlstate, err)
			}
			for i, text := range renderEverywhere(err) {
				if strings.Contains(text, sentinel) {
					t.Fatalf("rendering #%d of the refusal carries the server's echo of the DSN", i)
				}
			}
		})
	}
	// CONTROL for the echo cases: the server's own message DOES carry the sentinel, so
	// the search above would see it if the reason quoted the server.
	_, err := pgx.Connect(ctx, withParam(owner, "role", sentinel))
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || !strings.Contains(pg.Message, sentinel) {
		t.Fatalf("CONTROL FAILED: the server's message for a missing role does not echo its name (%v)", err)
	}
}

// TestPinLogParameters_LeavesOneSpellingOfTheKey: PostgreSQL's setting names are
// case-insensitive and pgx keeps a DSN's query parameters as map keys of the case they
// were written in. The pin removes every key that is the setting under another case
// before it writes its own -- otherwise both reach the server in map order (B4).
func TestPinLogParameters_LeavesOneSpellingOfTheKey(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://u@127.0.0.1:1/db?LOG_PARAMETER_MAX_LENGTH_ON_ERROR=-1" +
		"&Log_Parameter_Max_Length_On_Error=-1&log_parameter_max_length_on_error=-1&application_name=keep")
	if err != nil {
		t.Fatal(err)
	}
	pinLogParameters(cfg, "DATABASE_URL", nil)
	var keys []string
	for k, v := range cfg.ConnConfig.RuntimeParams {
		if strings.EqualFold(k, logParameterPin) {
			keys = append(keys, k+"="+v)
		}
	}
	if len(keys) != 1 || keys[0] != logParameterPin+"=0" {
		t.Fatalf("after the pin the startup parameters hold %v, want exactly %s=0", keys, logParameterPin)
	}
	if cfg.ConnConfig.RuntimeParams["application_name"] != "keep" {
		t.Error("the pin removed an unrelated startup parameter")
	}
}

// TestPin_ADifferentlyCasedKeyCannotUnpinIt is B4 measured: an upper-case and a mixed-
// case key, thirty opens each, on both pools -- every open succeeds and reads 0. (The
// 1st auditor measured 10 of 30 and 12 of 30 opens refused before the fix: the two
// spellings reached the server in map order.) CONTROL: the upper-case key alone, on a
// raw connection, does set the server's value -- the server reads names case-blind.
func TestPin_ADifferentlyCasedKeyCannotUnpinIt(t *testing.T) {
	app, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	if got := rawSetting(t, ctx, withParam(app, strings.ToUpper(logParameterPin), "-1")); got != "-1" {
		t.Fatalf("CONTROL FAILED: an upper-case key does not reach the server's setting (%q)", got)
	}
	const opens = 30
	for _, key := range []string{strings.ToUpper(logParameterPin), "Log_Parameter_Max_Length_On_Error"} {
		refused := map[string]int{}
		for i := 0; i < opens; i++ {
			d, err := New(ctx, &config.Config{DatabaseURL: withParam(app, key, "-1")})
			if err != nil {
				refused["customer"]++
			} else {
				if got, _, _ := settingOn(t, ctx, d.pool); got != "0" {
					refused["customer"]++
				}
				d.Close()
			}
			o, err := openOperatorDB(ctx, withParam(owner, key, "-1"), asOperator)
			if err != nil {
				refused["operator"]++
			} else {
				if got, _, _ := settingOn(t, ctx, o.pool); got != "0" {
					refused["operator"]++
				}
				o.Close()
			}
		}
		t.Logf("key %s: %d opens per pool, refused or unpinned: customer %d, operator %d", key, opens,
			refused["customer"], refused["operator"])
		if refused["customer"] != 0 || refused["operator"] != 0 {
			t.Errorf("key %s: %v of %d opens per pool refused or unpinned", key, refused, opens)
		}
	}
}

// TestLogParameterPinned_OnlyTheTextZero: the read-back accepts exactly "0" (what the
// server reports for zero, measured) and nothing else -- a size with its unit, a bare
// number, an empty answer, a padded zero (B5b).
func TestLogParameterPinned_OnlyTheTextZero(t *testing.T) {
	if !logParameterPinned("0") {
		t.Fatal(`"0" is refused`)
	}
	for _, got := range []string{"-1", "64B", "1", "1B", "1kB", "", "0B", " 0", "0 ", "00", "-0"} {
		if logParameterPinned(got) {
			t.Errorf("%q is accepted as pinned", got)
		}
	}
}

// TestPin_ASizeIsRefusedLikeUnlimited drives the read-back with the forms a positive
// setting takes on the server ("64B", "1B") on both pools: refused at open, like -1.
func TestPin_ASizeIsRefusedLikeUnlimited(t *testing.T) {
	app, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	for _, v := range []string{"64", "1"} {
		set := func(ctx context.Context, c *pgx.Conn) error {
			_, err := c.Exec(ctx, "SET log_parameter_max_length_on_error = "+v)
			return err
		}
		var np *logParameterNotPinnedError
		if d, err := newDB(ctx, &config.Config{DatabaseURL: app}, set); err == nil || !errors.As(err, &np) {
			if d != nil {
				d.Close()
			}
			t.Errorf("customer pool with the setting at %s: %v, want the read-back's refusal", v, err)
		}
		if o, err := openOperatorDB(ctx, owner, chain(asOperator, set)); err == nil || !errors.As(err, &np) {
			if o != nil {
				o.Close()
			}
			t.Errorf("operator pool with the setting at %s: %v, want the read-back's refusal", v, err)
		}
	}
}

// TestOperatorRoleQuery_SeesTheRestOfADR0021sRole is B9: the attributes ADR 0021 §1
// gives tappa_operator besides privilege and membership -- NOCREATEDB, NOCREATEROLE,
// NOREPLICATION, and no member -- each set inside a rolled-back savepoint and read back
// through the SHIPPED reader as tappa_operator; each one must be seen and refused.
func TestOperatorRoleQuery_SeesTheRestOfADR0021sRole(t *testing.T) {
	ctx, tx := opTx(t)
	read := func(q pgx.Tx) (facts operatorRoleFacts) {
		t.Helper()
		if err := opAs(t, ctx, q, "tappa_operator", func(sp pgx.Tx) (err error) {
			facts, err = readOperatorRoleOn(ctx, sp)
			return err
		}); err != nil {
			t.Fatalf("read the role as tappa_operator: %v", err)
		}
		return facts
	}
	if base := read(tx); base.CreateDB || base.CreateRole || base.Replication || base.HasMembers || operatorRoleRefusal(base) != nil {
		t.Fatalf("tappa_operator as shipped reads %+v; the gate must pass it", base)
	}
	for _, tc := range []struct {
		ddl  string
		seen func(operatorRoleFacts) bool
	}{
		{`ALTER ROLE tappa_operator CREATEDB`, func(f operatorRoleFacts) bool { return f.CreateDB }},
		{`ALTER ROLE tappa_operator CREATEROLE`, func(f operatorRoleFacts) bool { return f.CreateRole }},
		{`ALTER ROLE tappa_operator REPLICATION`, func(f operatorRoleFacts) bool { return f.Replication }},
		{`GRANT tappa_operator TO tappa_app`, func(f operatorRoleFacts) bool { return f.HasMembers }},
	} {
		t.Run(tc.ddl, func(t *testing.T) {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = sp.Rollback(ctx) }()
			if _, err := sp.Exec(ctx, tc.ddl); err != nil {
				t.Fatalf("apply: %v", err)
			}
			f := read(sp)
			if !tc.seen(f) {
				t.Fatalf("the reader does not see %q (%+v)", tc.ddl, f)
			}
			if operatorRoleRefusal(f) == nil {
				t.Fatalf("the gate opens a pool after %q", tc.ddl)
			}
		})
	}
}

// TestNewRefusesASwitchedSessionInProduction is F1: the owner's DSN carrying
// `role=tappa_app` signs in as the superuser and RUNS as tappa_app. Before this round
// the customer pool's gate read only current_user and opened it in production; on that
// connection SET ROLE NONE reached the superuser (measured). Now production refuses it,
// and development still opens it (the gate refuses only in production, as before).
// CONTROL: the switch is real -- SET ROLE NONE on the development pool's connection
// does reach the owner.
func TestNewRefusesASwitchedSessionInProduction(t *testing.T) {
	_, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	switched := withParam(owner, "role", "tappa_app")

	d, err := New(ctx, &config.Config{DatabaseURL: switched, Env: config.EnvProd})
	if err == nil {
		d.Close()
		t.Fatal("New opened a production pool whose session signed in as the owner and runs as tappa_app")
	}
	if !strings.Contains(err.Error(), `session_user "tappa_owner"`) || !strings.Contains(err.Error(), "signed_in_as_another_role=true") {
		t.Errorf("the refusal does not say what it measured: %v", err)
	}

	dev, err := New(ctx, &config.Config{DatabaseURL: switched, Env: config.EnvDev})
	if err != nil {
		t.Fatalf("development refused a pool it opened before this round: %v", err)
	}
	defer dev.Close()
	if f := dev.RoleFacts(); f.User != "tappa_app" || f.Session != "tappa_owner" || !f.Privileged() {
		t.Fatalf("the development pool's facts are %+v; the switch is not measured", f)
	}
	conn, err := dev.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	var cu string
	var super bool
	if _, err := conn.Exec(ctx, `SET ROLE NONE`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `RESET ROLE`) }()
	if err := conn.QueryRow(ctx, `SELECT current_user, (SELECT rolsuper FROM pg_roles WHERE rolname = current_user)`).Scan(&cu, &super); err != nil {
		t.Fatal(err)
	}
	if !super {
		t.Fatalf("CONTROL: SET ROLE NONE reached %q, not a superuser; the scenario is not the measured one", cu)
	}
}

// TestNew_AnUnparseableDSNCarriesNoPassword is F2: a DATABASE_URL that does not parse,
// with its password given as a query parameter, produces an error with no trace of it.
// CONTROL: pgx's own parse error does carry it -- which is why New no longer wraps it.
func TestNew_AnUnparseableDSNCarriesNoPassword(t *testing.T) {
	password := randHex(t, 16)
	dsn := "postgres://tappa_app@127.0.0.1:1/tappa?password=" + url.QueryEscape(password) + "&sslmode=bogus"
	if _, err := pgxpool.ParseConfig(dsn); err == nil || !strings.Contains(err.Error(), password) {
		t.Fatalf("CONTROL: pgx's parse error no longer quotes a query-parameter password (err %v)", err != nil)
	}
	d, err := New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err == nil {
		d.Close()
		t.Fatal("New accepted an unparseable DSN")
	}
	for i, text := range renderEverywhere(err) {
		if strings.Contains(text, password) {
			t.Fatalf("rendering #%d of New's parse error carries the DSN's password", i)
		}
	}
}

// ------------------------------------------------- 2b: one error, many attempts --

// closedAddr is a 127.0.0.1 address nothing listens on: a port the kernel handed out
// and that was closed again.
func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// behindAClosedPort turns a postgres URL into a two-host one whose FIRST host is a
// closed port (pgx's multi-host syntax: host1:port1,host2:port2).
func behindAClosedPort(t *testing.T, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		t.Skipf("the DSN (%d chars) is not a URL with a host this test can rewrite", len(dsn))
	}
	u.Host = closedAddr(t) + "," + u.Host
	return u.String()
}

// selfSignedCert is a server certificate for 127.0.0.1 that no trust store holds.
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fake postgres"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// fakeServer listens on 127.0.0.1 and answers each connection's SSLRequest with reply:
// 'N' declines TLS; 'S' runs a TLS server handshake with cfg. It speaks no more of the
// protocol -- every case it serves fails before the startup message is answered -- and
// it reads until the client hangs up before closing, so a TLS alert it sent is not
// overtaken by a reset.
func fakeServer(t *testing.T, reply byte, cfg *tls.Config) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Logf("close the fake server: %v", err)
		}
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				if err := c.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
					return
				}
				if _, err := io.ReadFull(c, make([]byte, 8)); err != nil {
					return
				}
				if _, err := c.Write([]byte{reply}); err != nil || reply != 'S' {
					return
				}
				tc := tls.Server(c, cfg)
				if err := tc.Handshake(); err != nil {
					_, _ = io.Copy(io.Discard, c)
					return
				}
				_, _ = io.Copy(io.Discard, tc)
			}(c)
		}
	}()
	return ln.Addr().String()
}

// TestConnectFailure_EveryAttemptDecides is 2b on the classifier, with the shapes pgx
// builds (pgconn's ConnectConfig joins every attempt's error with errors.Join): a join is
// unreachability only when EVERY attempt in it is. A refusal the SERVER gave says so; any
// other refusal -- a TLS alert, a certificate this side rejected, a declined TLS request,
// an error of a type the classifier does not know -- says neither "the server refused"
// nor anything from the error's text.
func TestConnectFailure_EveryAttemptDecides(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	pg := func(code string) error {
		return fmt.Errorf("server error: %w", &pgconn.PgError{Code: code, Message: "FAKEmessage"})
	}
	// crypto/tls's shapes: a received alert is a *net.OpError with Op "remote error";
	// one this side sent, "local error"; a certificate it would not accept, a
	// *tls.CertificateVerificationError. A declined SSLRequest is pgconn's errors.New.
	alert := &net.OpError{Op: "remote error", Err: errors.New("FAKEtls: certificate required")}
	local := &net.OpError{Op: "local error", Err: errors.New("FAKElocal")}
	cert := &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}
	declined := errors.New("FAKEserver refused TLS connection")
	join := func(errs ...error) error { return fmt.Errorf("failed to connect: %w", errors.Join(errs...)) }

	for _, tc := range []struct {
		name        string
		err         error
		unreachable bool
		server      bool
		sentinel    error
	}{
		{"a closed port, then 42501", join(refused, pg("42501")), false, true, nil},
		{"42501, then a closed port", join(pg("42501"), refused), false, true, nil},
		{"a closed port, then 28P01 (both in the list)", join(refused, pg("28P01")), true, false, nil},
		{"two closed ports", join(refused, refused), true, false, nil},
		{"a closed port, then a declined TLS request", join(refused, declined), false, false, nil},
		{"a declined TLS request, then a closed port", join(declined, refused), false, false, nil},
		{"a TLS alert alone", fmt.Errorf("failed to receive message: %w", alert), false, false, nil},
		{"a TLS alert, then a closed port", join(alert, refused), false, false, nil},
		{"a closed port, then a local TLS error", join(refused, local), false, false, nil},
		{"a rejected certificate, then a closed port", join(cert, refused), false, false, nil},
		{"a join inside a join", join(refused, errors.Join(refused, pg("42501"))), false, true, nil},
		{"a join inside a join, the inner one mixed", join(refused, errors.Join(refused, declined)), false, false, nil},
		{"a join inside a join, all unreachable", join(refused, errors.Join(refused, pg("08006"))), true, false, nil},
		{"several %w in one error", fmt.Errorf("%w; %w", refused, pg("42704")), false, true, nil},
		{"a name that does not resolve, then a closed port",
			fmt.Errorf("hostname resolving error: %w", errors.Join(&net.DNSError{Err: "no such host", Name: "x"}, refused)),
			true, false, nil},
		{"a closed port, then a deadline: the sentinel is kept",
			join(refused, fmt.Errorf("x: %w", context.DeadlineExceeded)), true, false, context.DeadlineExceeded},
	} {
		got := operatorConnectErr(tc.err)
		if errors.Is(got, ErrOperatorUnreachable) != tc.unreachable {
			t.Errorf("%s: unreachable=%v, want %v (%v)", tc.name, errors.Is(got, ErrOperatorUnreachable), tc.unreachable, got)
			continue
		}
		if tc.sentinel != nil && !errors.Is(got, tc.sentinel) {
			t.Errorf("%s: the context sentinel is lost: %v", tc.name, got)
		}
		if strings.Contains(got.Error(), "FAKE") {
			t.Errorf("%s: the error carries text from the failure itself: %q", tc.name, got)
		}
		if !tc.unreachable {
			if says := strings.Contains(got.Error(), "the server refused"); says != tc.server {
				t.Errorf("%s: says the server refused = %v, want %v: %q", tc.name, says, tc.server, got)
			}
		}
		if errors.Is(operatorStepErr("read its role", tc.err), ErrOperatorUnreachable) {
			t.Errorf("%s after the ping is marked unreachable", tc.name)
		}
	}
}

// TestOperatorDB_AJoinedFailureIsUnreachableOnlyWhenEveryAttemptIs is the 2nd auditor's
// table, driven through the EXPORTED constructor against fake servers on 127.0.0.1 (no
// database needed): pgx tries every host of a multi-host DSN and joins the errors. Each
// mixed case is a refusal whose reason names the attempt that refused; two closed ports
// stay unreachable. The user name is a sentinel (pgx's ConnectError quotes it) and must
// not reach the refusal.
func TestOperatorDB_AJoinedFailureIsUnreachableOnlyWhenEveryAttemptIs(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	cert := selfSignedCert(t)
	declines := fakeServer(t, 'N', nil)
	wantsClientCert := fakeServer(t, 'S', &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAnyClientCert})
	untrusted := fakeServer(t, 'S', &tls.Config{Certificates: []tls.Certificate{cert}})
	closedA, closedB := closedAddr(t), closedAddr(t)
	user := "zz" + randHex(t, 6)
	dsn := func(mode string, hosts ...string) string {
		return "postgres://" + user + "@" + strings.Join(hosts, ",") + "/tappa?sslmode=" + mode
	}
	for _, tc := range []struct {
		name, dsn   string
		unreachable bool
		want        string
	}{
		{"a TLS alert: the server wants a client certificate", dsn("require", wantsClientCert), false,
			"(a TLS alert (remote error))"},
		{"a closed port, then a server that declines TLS", dsn("require", closedA, declines), false,
			"(an error of type *errors.errorString; attempt 2 of 2)"},
		{"a server that declines TLS, then a closed port", dsn("require", declines, closedA), false,
			"(an error of type *errors.errorString; attempt 1 of 2)"},
		{"a certificate this side rejects, then a closed port", dsn("verify-full", untrusted, closedA), false,
			"(this side did not accept the server's TLS certificate; attempt 1 of 2)"},
		{"CONTROL: two closed ports", dsn("require", closedA, closedB), true,
			"(attempt 1: connection refused; attempt 2: connection refused)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := NewOperatorDB(ctx, &config.Config{OperatorDatabaseURL: tc.dsn})
			if err == nil {
				o.Close()
				t.Fatal("opened")
			}
			if errors.Is(err, ErrOperatorUnreachable) != tc.unreachable {
				t.Fatalf("unreachable=%v, want %v: %v", errors.Is(err, ErrOperatorUnreachable), tc.unreachable, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the reason does not name the deciding attempt %q: %v", tc.want, err)
			}
			if !tc.unreachable && strings.Contains(err.Error(), "the server refused") {
				t.Errorf("a refusal no server gave is worded as the server's: %v", err)
			}
			for i, text := range renderEverywhere(err) {
				if strings.Contains(text, user) {
					t.Fatalf("rendering #%d of the error carries the DSN's user name", i)
				}
			}
		})
	}
}

// TestPin_AReadBackThatCannotRunIsRefused is 2b's pin on the read-back's OTHER exit: the
// query itself fails. The hook leaves the connection in an aborted transaction, so the
// read-back's SELECT fails with 25P02; both pools must refuse with THAT error (a mutant
// that let the failed read through would fail later, on another step, with another
// error). The operator pool's refusal is not unreachability -- the server was reached.
func TestPin_AReadBackThatCannotRunIsRefused(t *testing.T) {
	app, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	abort := func(ctx context.Context, c *pgx.Conn) error {
		if _, err := c.Exec(ctx, "BEGIN"); err != nil {
			return err
		}
		if _, err := c.Exec(ctx, "SELECT 1/0"); err == nil {
			return errors.New("PREMISE: a division by zero did not fail, so the transaction is not aborted")
		}
		return nil
	}
	check := func(pool string, err error) {
		t.Helper()
		var read *logParameterReadError
		if !errors.As(err, &read) || read.Code != "25P02" {
			t.Errorf("%s pool: %v, want the read-back's own refusal with SQLSTATE 25P02", pool, err)
		}
		if errors.Is(err, ErrOperatorUnreachable) {
			t.Errorf("%s pool: a read-back that could not run is marked unreachable: %v", pool, err)
		}
	}
	d, err := newDB(ctx, &config.Config{DatabaseURL: app}, abort)
	if err == nil {
		d.Close()
		t.Fatal("customer pool opened on a connection whose read-back could not run")
	}
	check("customer", err)
	o, err := openOperatorDB(ctx, owner, chain(asOperator, abort))
	if err == nil {
		o.Close()
		t.Fatal("operator pool opened on a connection whose read-back could not run")
	}
	check("operator", err)
}

// TestReadLogParameterSQL_IsSchemaQualified: the read-back names pg_catalog's function,
// so no schema on a search_path can answer in its place (2b).
func TestReadLogParameterSQL_IsSchemaQualified(t *testing.T) {
	if !strings.HasPrefix(readLogParameterSQL, "SELECT pg_catalog.current_setting(") {
		t.Fatalf("the read-back is %q; it must call pg_catalog.current_setting by its qualified name", readLogParameterSQL)
	}
}

// TestOperatorDB_PreferAgainstAServerWithoutTLSIsARefusal pins a CONSEQUENCE of 2b's
// rule, measured rather than chosen: with sslmode=prefer (also the default when a DSN
// names no sslmode) pgx tries TLS first and plain second, so against a server without
// TLS (the development server, ssl=off) a wrong password is the join of a declined TLS
// request and 28P01 -- and the declined request is not unreachability, so the boot
// STOPS where the same password with sslmode=disable leaves the product up. The runbook's
// DSN carries sslmode=disable (deploy/README.md, operator surface runbook, step 1).
func TestOperatorDB_PreferAgainstAServerWithoutTLSIsARefusal(t *testing.T) {
	_, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	u, err := url.Parse(owner)
	if err != nil || u.User == nil {
		t.Skipf("DATABASE_MIGRATE_URL (%d chars) is not a URL with a user this test can rewrite", len(owner))
	}
	u.User = url.UserPassword(u.User.Username(), randHex(t, 16))
	q := u.Query()
	q.Set("sslmode", "prefer")
	u.RawQuery = q.Encode()
	o, err := NewOperatorDB(ctx, &config.Config{OperatorDatabaseURL: u.String()})
	if err == nil {
		o.Close()
		t.Fatal("opened with a wrong password")
	}
	// The number of attempts depends on the host's addresses (localhost: ::1 and
	// 127.0.0.1, each tried with TLS before either is tried plain); the first one is
	// always a declined TLS request.
	if errors.Is(err, ErrOperatorUnreachable) || !strings.Contains(err.Error(), "(an error of type *errors.errorString; attempt 1 of ") {
		t.Errorf("sslmode=prefer, server without TLS, wrong password: %v, want a refusal naming the declined TLS attempt", err)
	}
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	o, err = NewOperatorDB(ctx, &config.Config{OperatorDatabaseURL: u.String()})
	if err == nil {
		o.Close()
		t.Fatal("opened with a wrong password")
	}
	if !errors.Is(err, ErrOperatorUnreachable) || !strings.Contains(err.Error(), "SQLSTATE 28P01") {
		t.Errorf("CONTROL: sslmode=disable, wrong password: %v, want ErrOperatorUnreachable naming 28P01", err)
	}

	// 2d (the closing auditor): it is not the password alone. Under sslmode=prefer
	// against this server EVERY server answer in the list is a refusal, because the
	// declined TLS attempt comes first -- measured here for a database that does not
	// exist (3D000; the auditor also measured 57P03 and 53300) -- and only failures
	// that are network-level on EVERY attempt (a closed port) stay unreachable.
	missing, _ := url.Parse(owner)
	missing.Path = "/tappa_missing_" + randHex(t, 4)
	mq := missing.Query()
	mq.Set("sslmode", "prefer")
	missing.RawQuery = mq.Encode()
	o, err = NewOperatorDB(ctx, &config.Config{OperatorDatabaseURL: missing.String()})
	if err == nil {
		o.Close()
		t.Fatal("opened a database that does not exist")
	}
	if errors.Is(err, ErrOperatorUnreachable) {
		t.Errorf("sslmode=prefer, server without TLS, missing database: %v, want a refusal", err)
	}
	closed := url.URL{Scheme: "postgres", User: url.User("tappa_operator"), Host: closedAddr(t), Path: "/tappa",
		RawQuery: "sslmode=prefer"}
	o, err = NewOperatorDB(ctx, &config.Config{OperatorDatabaseURL: closed.String()})
	if err == nil {
		o.Close()
		t.Fatal("opened a closed port")
	}
	if !errors.Is(err, ErrOperatorUnreachable) {
		t.Errorf("sslmode=prefer, closed port: %v, want unreachable on both attempts", err)
	}
}

// ------------------------------------------- 2c: the security auditor's findings --

// execModeNames are pgx's five query exec modes by the value a DSN names them with
// (pgx's ParseConfig: default_query_exec_mode).
var execModeNames = map[pgx.QueryExecMode]string{
	pgx.QueryExecModeCacheStatement: "cache_statement",
	pgx.QueryExecModeCacheDescribe:  "cache_describe",
	pgx.QueryExecModeDescribeExec:   "describe_exec",
	pgx.QueryExecModeExec:           "exec",
	pgx.QueryExecModeSimpleProtocol: "simple_protocol",
}

// queryTextCarries runs one statement with a sentinel ARGUMENT on a raw connection in the
// given mode and reports whether the statement text the server holds -- current_query(),
// the text a failing statement's STATEMENT line logs -- contains the sentinel.
func queryTextCarries(t *testing.T, ctx context.Context, dsn, mode, sentinel string) bool {
	t.Helper()
	c, err := pgx.Connect(ctx, withParam(dsn, "default_query_exec_mode", mode))
	if err != nil {
		t.Fatalf("raw connect in mode %s: %v", mode, err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	var text, echoed string
	if err := c.QueryRow(ctx, `SELECT pg_catalog.current_query(), $1::text`, sentinel).Scan(&text, &echoed); err != nil {
		t.Fatalf("mode %s: %v", mode, err)
	}
	if echoed != sentinel {
		t.Fatalf("mode %s: the argument did not arrive (%q)", mode, echoed)
	}
	return strings.Contains(text, sentinel)
}

// TestQueryExecModes_OnlyTheListedOnesBindOnTheServer measures, on every run, the claim
// boundParameterModes makes: under each of pgx's five modes, does an argument end up in
// the statement TEXT? The listed modes must keep it out and every unlisted mode must put
// it in -- so the list is exactly the measured set, in both directions. The list's VALUE
// is compared with a literal here, in this test's environment; its INITIALIZER's tokens
// are pinned by TestConstructors_BodiesAreTheReviewedOnes (2h: a list built by a call that
// differs in production passed the value comparison).
func TestQueryExecModes_OnlyTheListedOnesBindOnTheServer(t *testing.T) {
	app, _ := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	want := []pgx.QueryExecMode{pgx.QueryExecModeCacheStatement, pgx.QueryExecModeCacheDescribe,
		pgx.QueryExecModeDescribeExec, pgx.QueryExecModeExec}
	if !slices.Equal(boundParameterModes, want) {
		t.Fatalf("boundParameterModes = %v, the measured contract is %v", boundParameterModes, want)
	}
	sentinel := "zz" + randHex(t, 8)
	embedding := 0
	for mode, name := range execModeNames {
		embeds := queryTextCarries(t, ctx, app, name, sentinel)
		t.Logf("mode %s: the argument is in the statement text = %v", name, embeds)
		if embeds {
			embedding++
		}
		if allowed := slices.Contains(boundParameterModes, mode); allowed == embeds {
			t.Errorf("mode %s: allowed=%v but puts the argument into the text=%v", name, allowed, embeds)
		}
	}
	if embedding == 0 {
		t.Fatal("CONTROL FAILED: no mode put the argument into the text; the measurement has gone blind")
	}
}

// TestPin_ADSNCannotChooseClientSideInterpolation is TestPin_ADSNCannotUnpinIt's sibling
// (2c): a DSN carrying default_query_exec_mode=simple_protocol is REFUSED by both pools,
// with a message naming the variable and the mode and nothing from the DSN. pgx reads the
// key and the value case-sensitively, so the two other spellings are refused on other
// paths, which the test also pins: an upper-case KEY is not pgx's and reaches the server
// as an unknown setting (42704); an upper-case VALUE does not parse. CONTROL: on a raw
// connection the same DSN really puts an argument into the statement text.
func TestPin_ADSNCannotChooseClientSideInterpolation(t *testing.T) {
	app, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	sentinel := "zz" + randHex(t, 8)
	if !queryTextCarries(t, ctx, app, "simple_protocol", sentinel) {
		t.Fatal("CONTROL FAILED: simple_protocol on a raw connection does not put the argument into the text")
	}
	for _, tc := range []struct {
		name, key, value, want string
	}{
		{"the documented spelling", "default_query_exec_mode", "simple_protocol",
			"asks for default_query_exec_mode=simple_protocol"},
		{"an upper-case key (not pgx's: the server refuses it)", "DEFAULT_QUERY_EXEC_MODE", "simple_protocol", ""},
		{"an upper-case value (pgx does not parse it)", "default_query_exec_mode", "SIMPLE_PROTOCOL", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := New(ctx, &config.Config{DatabaseURL: withParam(app, tc.key, tc.value)})
			if err == nil {
				d.Close()
				t.Fatal("customer pool opened")
			}
			if tc.want != "" && !strings.Contains(err.Error(), "DATABASE_URL "+tc.want) {
				t.Errorf("customer pool: %v, want the exec-mode refusal", err)
			}
			o, err := openOperatorDB(ctx, withParam(owner, tc.key, tc.value), asOperator)
			if err == nil {
				o.Close()
				t.Fatal("operator pool opened")
			}
			if tc.want != "" && !strings.Contains(err.Error(), "TAPPA_OPERATOR_DATABASE_URL "+tc.want) {
				t.Errorf("operator pool: %v, want the exec-mode refusal", err)
			}
			if errors.Is(err, ErrOperatorUnreachable) {
				t.Errorf("operator pool: a refused configuration is marked unreachable: %v", err)
			}
		})
	}
	// The constructors refuse the mode BEFORE any connection is attempted (2e): with the
	// DSN pointed at a closed port, the answer is still the mode's refusal -- not a failed
	// connection, and for the operator pool not "unreachable" (which would boot the
	// product with the surface down instead of naming the configuration). The
	// per-connection check (pinLogParameters) refuses the same mode later, on a
	// connection; this pins the earlier layer.
	closedHost := func(dsn string) string {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		u.Host = closedAddr(t)
		return withParam(u.String(), "default_query_exec_mode", "simple_protocol")
	}
	var refusedEarly *execModeRefusedError
	if d, err := New(ctx, &config.Config{DatabaseURL: closedHost(app)}); err == nil {
		d.Close()
		t.Fatal("customer pool opened on a closed port")
	} else if !errors.As(err, &refusedEarly) || !strings.HasPrefix(err.Error(), "db: DATABASE_URL asks for") {
		t.Errorf("customer pool, closed port + simple_protocol: %v, want the mode refused before connecting", err)
	}
	if o, err := openOperatorDB(ctx, closedHost(owner), asOperator); err == nil {
		o.Close()
		t.Fatal("operator pool opened on a closed port")
	} else if !errors.As(err, &refusedEarly) || errors.Is(err, ErrOperatorUnreachable) {
		t.Errorf("operator pool, closed port + simple_protocol: %v, want the mode refused before connecting", err)
	}
	// Every allowed mode still opens both pools: the rule refuses a mode, not the key.
	for _, mode := range boundParameterModes {
		d, err := New(ctx, &config.Config{DatabaseURL: withParam(app, "default_query_exec_mode", execModeNames[mode])})
		if err != nil {
			t.Fatalf("customer pool refused the allowed mode %s: %v", execModeNames[mode], err)
		}
		d.Close()
	}
}

// roleGateQueries are the role gates' statements, by name.
var roleGateQueries = map[string]string{"roleFactsQuery": roleFactsQuery, "operatorRoleQuery": operatorRoleQuery}

// TestRoleGateQueries_AreSchemaQualified (2c) pins the text: every catalog relation and
// function the gates read is written pg_catalog.<name>, and no comparison is left to
// search_path -- once every OPERATOR(pg_catalog.=) is taken out, no =, <>, IN or NOT IN
// remains.
func TestRoleGateQueries_AreSchemaQualified(t *testing.T) {
	for name, q := range roleGateQueries {
		for _, catalog := range []string{"pg_roles", "pg_class", "pg_namespace", "pg_auth_members", "pg_has_role"} {
			bare := strings.Count(q, catalog)
			qualified := strings.Count(q, "pg_catalog."+catalog)
			if bare != qualified {
				t.Errorf("%s names %s %d times, %d of them qualified", name, catalog, bare, qualified)
			}
		}
		rest := strings.ReplaceAll(q, "OPERATOR(pg_catalog.=)", "")
		for _, op := range []string{"=", "<>", "!=", " IN ", " IN(", "NOT IN"} {
			if strings.Contains(rest, op) {
				t.Errorf("%s compares with a bare %q, which resolves through search_path", name, op)
			}
		}
	}
}

// TestRoleGateQueries_IgnoreAShadowCatalog drives the auditor's measurement against the
// SHIPPED queries: as the owner, inside a transaction that is rolled back, a schema placed
// before pg_catalog on search_path holds views named pg_roles, pg_class, pg_namespace and
// pg_auth_members that report nothing privileged, a pg_has_role that answers false, and an
// `=` on oid that always matches. Both queries must read exactly what they read without
// it. CONTROL: an unqualified copy of each query, made from the shipped text, DOES read the
// shadow -- so the shadow is live and the comparison is not vacuous.
func TestRoleGateQueries_IgnoreAShadowCatalog(t *testing.T) {
	_, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	c, err := pgx.Connect(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	tx, err := c.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	read := func(q string) []any {
		t.Helper()
		rows, err := tx.Query(ctx, q)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		defer rows.Close()
		var out []any
		for rows.Next() {
			v, err := rows.Values()
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, v...)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	unqualified := func(q string) string {
		q = strings.ReplaceAll(q, "OPERATOR(pg_catalog.=)", "=")
		return strings.ReplaceAll(q, "pg_catalog.", "")
	}
	before := map[string][]any{}
	for name, q := range roleGateQueries {
		before[name] = read(q)
	}
	for _, ddl := range []string{
		`CREATE SCHEMA zz_shadow`,
		`CREATE VIEW zz_shadow.pg_roles AS SELECT oid, rolname, false AS rolsuper, false AS rolbypassrls,
		        false AS rolcreatedb, false AS rolcreaterole, false AS rolreplication FROM pg_catalog.pg_roles`,
		`CREATE VIEW zz_shadow.pg_class AS SELECT * FROM pg_catalog.pg_class WHERE false`,
		`CREATE VIEW zz_shadow.pg_namespace AS SELECT * FROM pg_catalog.pg_namespace WHERE false`,
		`CREATE VIEW zz_shadow.pg_auth_members AS SELECT * FROM pg_catalog.pg_auth_members WHERE false`,
		`CREATE FUNCTION zz_shadow.pg_has_role(name, oid, text) RETURNS boolean LANGUAGE sql AS 'SELECT false'`,
		`CREATE FUNCTION zz_shadow.oid_eq(oid, oid) RETURNS boolean LANGUAGE sql IMMUTABLE AS 'SELECT true'`,
		`CREATE OPERATOR zz_shadow.= (LEFTARG = oid, RIGHTARG = oid, FUNCTION = zz_shadow.oid_eq)`,
		`SET LOCAL search_path = zz_shadow, pg_catalog`,
	} {
		if _, err := tx.Exec(ctx, ddl); err != nil {
			t.Fatalf("shadow: %v", err)
		}
	}
	for name, q := range roleGateQueries {
		if got := read(q); fmt.Sprint(got) != fmt.Sprint(before[name]) {
			t.Errorf("%s reads %v under the shadow catalog, %v without it", name, got, before[name])
		}
		if got := read(unqualified(q)); fmt.Sprint(got) == fmt.Sprint(before[name]) {
			t.Errorf("CONTROL FAILED: the unqualified copy of %s reads %v under the shadow too; the shadow is not live",
				name, got)
		}
	}
}

// TestConnectFailure_AnUnreachableJoinNamesEveryAttempt (2d): when every attempt was
// unreachable the message lists each attempt's code, in order, and nothing else -- not
// the first one alone. A closed port beside a wrong password names both.
func TestConnectFailure_AnUnreachableJoinNamesEveryAttempt(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	wrongPassword := fmt.Errorf("server error: %w", &pgconn.PgError{Code: "28P01", Message: "FAKEmessage"})
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"a closed port, then a wrong password",
			fmt.Errorf("failed to connect: %w", errors.Join(refused, wrongPassword)),
			"cannot connect (attempt 1: connection refused; attempt 2: SQLSTATE 28P01): operator database unreachable"},
		{"three attempts", fmt.Errorf("failed to connect: %w", errors.Join(refused, wrongPassword, refused)),
			"(attempt 1: connection refused; attempt 2: SQLSTATE 28P01; attempt 3: connection refused)"},
		{"one attempt keeps its plain reason", refused, "cannot connect (connection refused): operator database unreachable"},
	} {
		got := operatorConnectErr(tc.err)
		if !errors.Is(got, ErrOperatorUnreachable) || !strings.Contains(got.Error(), tc.want) {
			t.Errorf("%s: %v, want unreachable naming %q", tc.name, got, tc.want)
		}
		if strings.Contains(got.Error(), "FAKE") {
			t.Errorf("%s: the message carries the server's text: %v", tc.name, got)
		}
	}
}

// ---------------------------------------------- 2e: the behaviour, not the text --

// argumentsStayBound acquires three DISTINCT connections from p, holding them all, and
// on each runs one statement with a sentinel ARGUMENT: the statement text the server
// holds (current_query(), what a failing statement's STATEMENT line logs) must not
// contain it. Three, because a mode can differ between the first connection and later
// ones (the pool copies its configuration per new connection).
func argumentsStayBound(t *testing.T, ctx context.Context, name string, p *pgxpool.Pool) {
	t.Helper()
	const conns = 3
	pids := map[uint32]bool{}
	var held []*pgxpool.Conn
	defer func() {
		for _, c := range held {
			c.Release()
		}
	}()
	for i := 0; i < conns; i++ {
		c, err := p.Acquire(ctx)
		if err != nil {
			t.Fatalf("%s pool, connection %d: %v", name, i+1, err)
		}
		held = append(held, c)
		pids[c.Conn().PgConn().PID()] = true
		sentinel := "zz" + randHex(t, 8)
		var text, echoed string
		if err := c.QueryRow(ctx, `SELECT pg_catalog.current_query(), $1::text`, sentinel).Scan(&text, &echoed); err != nil {
			t.Fatalf("%s pool, connection %d: %v", name, i+1, err)
		}
		if echoed != sentinel {
			t.Fatalf("%s pool, connection %d: the argument did not arrive", name, i+1)
		}
		if strings.Contains(text, sentinel) {
			t.Errorf("%s pool, connection %d: the argument is in the statement text the server logs", name, i+1)
		}
	}
	if len(pids) != conns {
		t.Fatalf("%s pool: %d distinct backends for %d held connections; the later connections were not new", name, len(pids), conns)
	}
}

// TestPools_KeepArgumentsOutOfTheStatementText is the BEHAVIOUR the exec-mode rule
// exists for, measured on the pools the two constructors hand back (2e, the 2nd closing
// auditor: two rounds of checks on the configuration's TEXT were each wider than what
// they measured, and a mode changed inside a constructor after its check passed all of
// them). On every connection held, an argument stays out of the statement text.
//
// THE OPERATOR POOL IS OPENED THROUGH ITS TEST HOOK (openOperatorDB with asOperator), and
// that is a measured choice: tappa_operator is NOLOGIN on this server, a password granted
// inside BEGIN ... ROLLBACK is invisible to a new connection (it is not committed), and a
// committed grant would leave a trace if the run died. The property under test is pgx's,
// on this side of the wire -- which mode the pool's connections use -- and does not
// depend on which role signed in. The hook switches the session's role in AfterConnect
// before the production checks run. Within the declarations the source pins name, the
// hook is the one difference between this path and production's
// (TestConstructors_BodiesAreTheReviewedOnes: openOperatorDB's and pinLogParameters'
// bodies token for token; TestConstructors_TheHookReachesOnlyThePin: the hook reaches only
// pinLogParameters, and NewOperatorDB passes nil); what lies outside them is code review's.
//
// THE CUSTOMER POOL IS MEASURED IN BOTH ENVIRONMENTS (2g, the 4th closing auditor: a
// branch on cfg.IsProd() in newDB re-parsed the DSN with simple_protocol and every test
// -- all run in dev -- stayed green). In production env the role gate refuses a
// privileged role, and the development DATABASE_URL signs in as tappa_app, which it
// accepts (role_test.go measures the same role opening in production).
// CONTROL: a pool in simple_protocol mode does put the argument into the text.
func TestPools_KeepArgumentsOutOfTheStatementText(t *testing.T) {
	app, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if !queryTextCarries(t, ctx, app, "simple_protocol", "zz"+randHex(t, 8)) {
		t.Fatal("CONTROL FAILED: simple_protocol does not put the argument into the text; the measurement is blind")
	}
	for _, env := range []string{config.EnvDev, config.EnvProd} {
		d, err := New(ctx, &config.Config{DatabaseURL: app, Env: env})
		if err != nil {
			t.Fatalf("customer pool, env %s: %v", env, err)
		}
		if (env == config.EnvProd) != (&config.Config{Env: env}).IsProd() {
			t.Fatalf("PREMISE: env %s does not read as production=%v", env, env == config.EnvProd)
		}
		argumentsStayBound(t, ctx, "customer ("+env+")", d.pool)
		d.Close()
	}
	o, err := openOperatorDB(ctx, owner, asOperator)
	if err != nil {
		t.Fatalf("operator pool: %v", err)
	}
	defer o.Close()
	argumentsStayBound(t, ctx, "operator", o.pool)
}

// TestPin_AModeChangedAfterTheCheckIsRefusedOnItsConnection: the per-connection layer
// (pinLogParameters' AfterConnect) refuses a connection whose OWN mode interpolates, so --
// WHILE pinLogParameters' AfterConnect remains the pool's hook (2g: replacing the pool's
// configuration after the pin takes the check with it; the constructors' bodies are pinned
// for that reason) -- a mode changed after the constructors' DSN check is refused on its
// connection --
// (a) changed before the pool is built, (b) changed on the pool's configuration after
// its first connection (the pool copies it per new connection, measured: pgxpool's
// NewWithConfig keeps the pointer).
func TestPin_AModeChangedAfterTheCheckIsRefusedOnItsConnection(t *testing.T) {
	app, _ := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pinned := func() *pgxpool.Config {
		t.Helper()
		cfg, err := pgxpool.ParseConfig(app)
		if err != nil {
			t.Fatal(err)
		}
		if err := requireBoundParameters(cfg.ConnConfig, "DATABASE_URL", true); err != nil {
			t.Fatalf("PREMISE: the DSN's own mode is refused: %v", err)
		}
		pinLogParameters(cfg, "DATABASE_URL", nil)
		return cfg
	}
	var refused *execModeRefusedError

	before := pinned()
	before.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	p, err := pgxpool.NewWithConfig(ctx, before)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := p.Ping(ctx); !errors.As(err, &refused) {
		t.Errorf("a mode changed before the pool was built: ping %v, want the per-connection refusal", err)
	} else if msg := refused.Error(); !strings.Contains(msg, "the connection's configuration (the DATABASE_URL pool)") ||
		strings.Contains(msg, "asks for") {
		// 2f (N-4): this mode was set in code, not by the DSN; the message must not
		// blame the DSN.
		t.Errorf("the per-connection refusal blames the DSN for a mode code set: %q", msg)
	}

	after := pinned()
	q, err := pgxpool.NewWithConfig(ctx, after)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	first, err := q.Acquire(ctx)
	if err != nil {
		t.Fatalf("the first connection: %v", err)
	}
	defer first.Release()
	after.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	second, err := q.Acquire(ctx)
	if err == nil {
		second.Release()
		t.Fatal("a second connection opened after the pool's mode was changed to simple_protocol")
	}
	if !errors.As(err, &refused) {
		t.Errorf("the second connection: %v, want the per-connection refusal", err)
	}
}
