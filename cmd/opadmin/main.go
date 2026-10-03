// Command opadmin writes the SQL that creates, re-enrolls and disables a platform
// operator account (ADR 0020 §6, M10 OP-9). 00026 grants INSERT on platform_admins to
// no application role and not to tappa_operator (internal/db's
// TestOperator00026_PrivilegeMatrix and TestOperator00026_AppHoldsNothingUnderTheProductionDefaultACL),
// so an account is made by SQL the owner applies, and this command writes that SQL.
//
//	opadmin create    --email ADDRESS --name "DISPLAY NAME" --host OPERATOR_HOST
//	opadmin reset-mfa --id ACCOUNT_ID --email ADDRESS --host OPERATOR_HOST
//	opadmin disable   --id ACCOUNT_ID --email ADDRESS
//
// The runbook is deploy/README.md, "Operator accounts (M10 OP-9)".
//
// ============================================================================
// A FILTER, NOT A DATABASE CLIENT -- cmd/rotatekek's shape, for rotatekek's reason
// ============================================================================
// It writes SQL on stdout; the operator reads it and applies it with psql as
// tappa_owner. Which role runs is answered by the psql invocation in the runbook, and
// the DO block's first statement is a role guard that refuses a role that is neither a
// superuser nor BYPASSRLS: platform_admins has FORCE ROW LEVEL SECURITY and its one
// policy is tappa_operator's SELECT (00026's note on a non-superuser owner says the
// same). TestApply_RefusesARoleRLSWouldFilter measures the guard refusing tappa_app and
// tappa_operator for all three subcommands. tappa_owner is initdb's bootstrap superuser
// on the deployed topology.
//
// The migration role's DSN variable is not named in this file:
// cmd/tappa/packaging_test.go asserts that name appears in no production source
// outside internal/config, and this file is a production source by that test's
// definition (TestDeps_NoEnvironmentAndNoDSNName checks the same here, with ten more
// names).
//
// ============================================================================
// THE ENROLLMENT LINK
// ============================================================================
//
//	https://<operator host>/operator/enroll?id=<account id>#<link secret>
//
// The shape is OP-8's (internal/handler/operator/enroll.go; ADR 0020 §6, OP-8 note):
// the account id travels in the query -- it is not a credential, the page needs it to
// seal the new TOTP key to that account, and op_complete_enrollment matches the id
// AND the secret's hash together -- and the secret travels in the FRAGMENT. Measured
// for this task (deploy/README.md, "Ingress neyi log'lar"): Go's net/http, curl 8.7.1
// and headless Chrome 154 sent the id and neither the fragment nor the secret, in the
// request line and in Chrome's Referer headers.
//
// The host is a flag and follows TAPPA_OPERATOR_HOST's rule (internal/config,
// isDNSHostName): a lower-case DNS name, no scheme, port, path, user part or trailing
// dot -- refused, not normalised. TestHost_IsConfigsRule compares the two function
// bodies printed without comments; a difference turns it red.
//
// ============================================================================
// THE LINK SECRET -- operatorauth's contract, written out here, pinned from the tests
// ============================================================================
// 256 bits from crypto/rand, unpadded base64url (43 characters); the database gets the
// KEYLESS SHA-256 of the secret's text, lower-case hex (ADR 0020 §3: the CLI holds no
// server key; 00026 compares encode(sha256(convert_to(p_token, 'UTF8')), 'hex')).
// That is internal/operatorauth's NewEnrollmentToken and EnrollmentTokenHash, and
// operatorauth's CompleteEnrollment refuses another shape before the database sees it.
//
// WHY A SECOND COPY AND NOT AN IMPORT, MEASURED: internal/operatorauth imports
// internal/db, and `go list -deps ./internal/operatorauth` lists github.com/jackc/pgx/v5
// and database/sql. Importing it would put a PostgreSQL driver into this binary, which
// ADR 0020 §6 rules out. Moving the two functions into a driver-free package would
// edit operatorauth (and the leak and redaction tests that name them) while OP-8 edits
// the same package. So the three standard-library calls are written here, and the
// equality is pinned from this package's TESTS, which may import operatorauth (a test
// binary is not this binary): TestLinkSecret_HashIsOperatorauthsHash and, against
// 00026, TestCreate_TheLinkEnrollsTheAccount, where operatorauth's CompleteEnrollment
// accepts a secret minted here.
//
// ============================================================================
// WHAT LEAVES THIS PROCESS, AND WHERE
// ============================================================================
//   - stdout: the SQL. Its statement texts carry constants, the validated account id
//     and the generation time; the link secret's SHA-256, the email address and the
//     display name are in its one COPY data line, in hex (below)
//     (TestLeak_TheSecretIsOnlyInTheLinkOnStderr: the secret is not on stdout).
//   - stderr: a report, and the link -- once.
//   - THE ORDER (run): every statement but COMMIT; then the report; then COMMIT. A run
//     whose statements could not be written prints no link
//     (TestLeak_AFailedWritePrintsNoLink); a run whose report could not be written
//     writes the poison statement in COMMIT's place, and applying its output commits
//     no account (TestLeak_AFailedReportWithholdsTheCommit;
//     TestApply_AFailureAnywhereLeavesNoRow, "a report that could not be written").
//   - create and reset-mfa refuse to run when stderr is not a terminal: a redirected
//     stderr would put the link in a file or a log. The check reads the file mode:
//     /dev/null is a character device and passes (the link is discarded); a terminal
//     that records itself (script(1), a multiplexer's log) passes too -- deploy/README.md,
//     limit O9-2. TestMain_TheBinaryRefusesARedirectedStderr runs the compiled command.
//
// ============================================================================
// THE SQL -- one transaction, one DO block doing the work
// ============================================================================
// BEGIN · SET LOCAL search_path · SET LOCAL lock_timeout · CREATE TEMP TABLE
// pg_temp.opadmin_in · COPY … FROM STDIN with one data line · DO $opadmin$ … $opadmin$
// · COMMIT. Every read and write of the account is inside the one DO block, which
// PostgreSQL runs as a single statement. The explicit BEGIN … COMMIT is what the two
// SET LOCALs and the ON COMMIT DROP table need; and in psql without ON_ERROR_STOP and
// with ON_ERROR_ROLLBACK off (the runbook passes -X, which skips psqlrc), it makes a
// failure before the COPY, before the DO block or after it abort the transaction
// instead of leaving the DO block committed on its own
// (TestApply_AFailureAnywhereLeavesNoRow). Measured outside that scope, psql 18.4: with
// ON_ERROR_ROLLBACK=on an error before or after the DO block is rolled back to a
// savepoint and the DO block's write commits; the runbook's -v ON_ERROR_STOP=1 stops at
// the first error.
//
// THE HASH, THE ADDRESS AND THE NAME TRAVEL AS COPY DATA, NOT AS STATEMENT TEXT -- WHEN
// PSQL READS THE FILE (round 2; ADR 0020 §5 lists the enrollment token, raw and hashed,
// among what is not logged; cmd/rotatekek's precedent for its wrapped refs). The server
// writes statement TEXT to its log: every statement under log_statement=all (the
// development database runs with it) and the text of a failing statement under
// log_min_error_statement=error (the default, and production's). Round 1 had the hash
// as a literal in the DO block, so each refused application put it there.
//
// Measured with psql 18.4 through a recording relay (m10-platform.md, OP-9 card
// correction, rounds 2 and 3), for the client modes psql -X -v ON_ERROR_STOP=1 with the
// file on stdin, with -f, and with \i: the statements sent carried none of the three
// values; they reached the server only as the one COPY data message, which is not
// statement text. The data line starts with "-- ": when the COPY did not start (an
// earlier error, psql without ON_ERROR_STOP, file on stdin), psql read the line as SQL,
// dropped it as a leading comment and printed "invalid command \." for the terminator
// -- no value sent. OUTSIDE those modes, measured: psql -c "$(cat file)" sends the
// whole file as ONE statement, which fails, and its text -- the hash included -- is
// what log_min_error_statement writes; a GUI query tool is the same class, not
// measured. The runbook and the SQL's own header say: stdin or -f, nothing else.
//
// A COPY error puts the first 100 bytes of the data line into its CONTEXT (measured: a
// data error shows 100 bytes and "..."; a cancel during the COPY reaches the same
// callback -- PostgreSQL's source, not measured), and CONTEXT is logged. copyPadding, a
// fixed 100-byte field before every value, keeps the values out of those bytes
// (TestCopy_ADataErrorShowsThePaddingNotTheValues, TestSQL_TheFirst100BytesOfTheDataLineHoldNoValue).
//
// The repository tests are TestLog_TheValuesReachTheServerOnlyAsCopyData (statement
// texts and error fields, seven refusals) and, for the lock timeout,
// TestApply_ARowHeldElsewhereFailsFastNotForever.
//
// THE ACCOUNT ID IS IN THE STATEMENT TEXT, QUOTED: as '<id>'::uuid and inside the RAISE
// messages. It is not a credential; parse refuses every value that is not the
// lower-case canonical uuid shape (hex digits and four hyphens), so the quoted text
// holds no quote.
//
// THE GENERATION TIME T (rounds 2 and 3, the A-B-A replay). T is this machine's clock
// when the script was generated, written into the DO block. create and reset-mfa refuse
// when the database clock has passed T + 30 minutes, and when T is AHEAD of the
// database clock -- no allowance in the code; refused leads measured down to 250 ms
// (TestScript_GenerationGuards), smaller leads not measured (round 3; round 2 allowed 30
// seconds, and that allowance was a measured replay window -- a refused script applies
// once the database clock has passed T); reset-mfa also refuses when the account's enroll_issued_at is at or after T -- a
// link was issued for it after this script was written. The last guard refuses the
// measured replay (R1 applied and used, R2 applied and used, R1 applied again:
// TestResetMFA_ABAReplayIsRefused, red on round 1's code), and with the first one it
// refuses it for a generating clock 2 s and 10 minutes ahead too
// (TestResetMFA_AClockAheadDoesNotReopenAUsedLink); resetMFAScript says why, and
// deploy/README.md limit O9-5 what is left. The guards for both subcommands:
// TestScript_GenerationGuards.
//
// enroll_issued_at and enroll_expires_at are written from ONE clock_timestamp() read
// inside the SQL (OP-5 card item 27; 00026's warning: the schema's one-hour ceiling is
// measured against the issued-at the writer writes), not from this program's clock:
// the link is valid for 30 minutes from the moment the SQL is APPLIED.
//
// A constraint that refuses the write is reported by NAME and SQLSTATE: the block
// catches integrity_constraint_violation and raises its own message, because
// PostgreSQL's DETAIL line ("Failing row contains (...)", "Key (email)=(...)") would
// echo the row (00026 section 5 for the same reason).
//
// The SQL is built with strings.Builder and no Go format verbs: PL/pgSQL's RAISE has
// its own %, and two formatters over one string is the bug cmd/rotatekek recorded.
//
// ============================================================================
// THE CLAIM, IN THREE PARTS (agent-brief, M10 OP-6/OP-7)
// ============================================================================
// PART I -- this command as shipped, measured:
//   - its dependency closure (go list -deps for linux/amd64, linux/arm64, darwin/amd64,
//     darwin/arm64 and windows/amd64, CGO off) is standard-library packages plus this
//     package, and holds none of database/sql, database/sql/driver, net, net/http,
//     os/exec, plugin: TestDeps_NoDriverInTheClosure;
//   - for create and reset-mfa the link secret is in the stderr report once and, in the
//     eight encodings TestLeak_TheSecretIsOnlyInTheLinkOnStderr lists, in neither stdout
//     nor the default slog/log output at Debug, text and JSON; a failed SQL write prints
//     no link-shaped value (TestLeak_AFailedWritePrintsNoLink); a failed report write
//     leaves the poison statement in COMMIT's place (TestLeak_AFailedReportWithholdsTheCommit);
//     the 47 refusals TestRun_RefusesTheEscapeTable drives print no link-shaped value;
//   - for the seven refusals TestLog_TheValuesReachTheServerOnlyAsCopyData lists and the
//     lock timeout of TestApply_ARowHeldElsewhereFailsFastNotForever, applied statement
//     by statement as psql applies a file, neither a statement text nor a returned error
//     field carries the hash, a hex field of the data line, or the text a field decodes to;
//   - an application with an error injected before the COPY, before, inside or after the
//     DO block, or with the poison in COMMIT's place, leaves no account row, and
//     reset-mfa with an error between its two UPDATEs leaves the account and its session
//     as they were: TestApply_AFailureAnywhereLeavesNoRow;
//   - a link is refused 30 min + 1 s after the application and accepted with 30 s left:
//     TestCreate_AnExpiredLinkIsRefused; the A-B-A replay is refused:
//     TestResetMFA_ABAReplayIsRefused, and with a generating clock 2 s and 10 minutes
//     ahead: TestResetMFA_AClockAheadDoesNotReopenAUsedLink; for create AND reset-mfa,
//     a script 29 minutes old is accepted and one 31 minutes old refused, one 250 ms, 1 s
//     or 20 s ahead of the database clock refused and the same 1-s file accepted once the
//     clock has passed it, and a reset generated before the last issuance refused:
//     TestScript_GenerationGuards; a data line edited in the eight ways
//     TestPayload_OnlyTheOneLineOfTheExpectedShapeIsAccepted lists is refused; a COPY
//     data error's CONTEXT holds the padding and no value:
//     TestCopy_ADataErrorShowsThePaddingNotTheValues; the account's life through operatorauth:
//     TestCreate_TheLinkEnrollsTheAccount, TestResetMFA_KillsTheOldSessionsAndIssuesANewLink,
//     TestResetMFA_RefusesWhatItMustNot, TestDisable_EndsSessionsAndSignIn;
//   - the role guard refuses tappa_app and tappa_operator: TestApply_RefusesARoleRLSWouldFilter;
//     a row held by another transaction ends the script with 55P03 after about 5 s:
//     TestApply_ARowHeldElsewhereFailsFastNotForever; the compiled command refuses a
//     stderr redirected to a file: TestMain_TheBinaryRefusesARedirectedStderr.
//
// PART II -- named pins and exactly what each catches (thirteen):
//   - TestDeps_ImportsAreTheListedOnes: an import set of the non-test .go files here
//     that differs from importAllowList (read by go/parser, build constraints not
//     evaluated, import "C" included);
//   - TestDeps_NoEnvironmentAndNoDSNName: a selector written os.<F> or syscall.<F> for
//     the ten names in envCalls, and the text of the eleven names in dsnNames, in the
//     non-test sources;
//   - TestDeps_StartsNoProcess: a selector written as one of the four names in
//     processCalls in the non-test sources;
//   - TestDeps_NoBuildConstraintOrDirective: a trimmed line starting //go:, // +build or
//     //line in the non-test sources, and on each of the five ports a file the
//     toolchain ignores, a cgo file, or a compiled file list that differs from the
//     files on disk;
//   - TestSQL_IsOneTransactionAroundOneDoBlock: for the three subcommands with its
//     inputs and a fixed time, a statement list other than the seven named in its
//     header, and stdout that differs from the builder's script for the reported values;
//   - TestSQL_TheValuesTravelAsCopyData: for its one input per subcommand, a data line
//     other than "-- ", copyPadding and the hex fields, a statement text holding a hex field or one
//     of its seven raw fragments, a dollar-tag count other than two;
//   - TestSQL_ThirtyMinutesFromOneDatabaseClockRead: enrollTTL other than 30 minutes, a
//     v_now read count other than one in create and reset-mfa, the five frozen-clock
//     names, issuance columns not written from v_now;
//   - TestSQL_ResetRechecksUnderTheRowLock: reset-mfa's SQL without one of its six
//     listed fragments (FOR UPDATE, the three UPDATE conditions, the row-count check,
//     the issue-time guard);
//   - TestSQL_TheFirst100BytesOfTheDataLineHoldNoValue: copyPadding shorter than 100
//     bytes, or a value starting within the first 100 bytes of a data line;
//   - TestLinkSecret_HashIsOperatorauthsHash: a hash differing from
//     operatorauth.EnrollmentTokenHash on its 105 inputs, and a run's COPY data without
//     the hash of its link's secret;
//   - TestLinkSecret_Shape: in 1000 draws, a value that is not 43 canonical base64url
//     characters of 32 bytes in the page's alphabet, or a repeat;
//   - TestHost_IsConfigsRule: an isDNSHostName signature or body that differs from
//     internal/config's, and its 16-case table;
//   - TestRun_AcceptsTheBoundaries: maxEmailBytes other than 254, a 254-byte address or
//     a 200-character name refused.
//
// PART III -- any form not listed above is the subject of code review; no completeness claim.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// linkSecretBytes and linkSecretLen are operatorauth's tokenBytes and tokenLen:
	// 256 bits, and their unpadded base64url length, ceil(256/6) = 43.
	linkSecretBytes = 32
	linkSecretLen   = 43

	// enrollTTL is ADR 0020 §3's link lifetime, as the SQL interval it is written
	// as. The schema's ceiling is one hour (00026, platform_admins_enroll_ttl_ceiling).
	enrollTTL = "30 minutes"

	// scriptMaxAge is how long after its generation a create or reset-mfa script is
	// accepted, by the database clock (the replay guard, round 2: a script file kept
	// around is not a standing licence to issue a link).
	scriptMaxAge = "30 minutes"

	// copyPadding is the first field of the COPY data line, before every value. A COPY
	// error -- a data error, or a cancel arriving during the COPY -- puts the first 100
	// bytes of the line into its CONTEXT (measured, psql 18.4 against PostgreSQL 17.10:
	// 100 bytes and "..."), and CONTEXT goes to the server log. With "-- " and these 100
	// bytes in front, the hash starts at byte 104 (TestCopy_ADataErrorShowsThePaddingNotTheValues).
	// The DO block compares the field for equality, so the line's layout is checked
	// too (TestPayload_OnlyTheOneLineOfTheExpectedShapeIsAccepted).
	copyPadding = "opadmin/copy-data-padding/v1/xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

	// lockTimeout bounds how long THIS script waits for a lock another session holds
	// (cmd/rotatekek's choice (b): fail fast and re-run, rather than queue without a
	// bound -- an op_* call left open by a DSN holder keeps the account's row locked).
	// It bounds this script's waits, not another session's
	// (TestApply_ARowHeldElsewhereFailsFastNotForever: 55P03 after about 5 s).
	lockTimeout = "5s"

	// The email and display-name bounds are 00026's CHECKs, in the units the CHECKs
	// count: char_length counts characters, and validEmail accepts ASCII alone, so an
	// accepted address has as many characters as bytes.
	maxEmailBytes = 254
	maxLocalBytes = 64
	maxNameRunes  = 200

	// dollarTag quotes the DO body; the body is built from constants, the validated
	// id and the generation time (TestSQL_TheValuesTravelAsCopyData counts the tag).
	dollarTag = "$opadmin$"

	// payloadTable holds the one COPY data line of a script for the length of its
	// transaction.
	payloadTable = "pg_temp.opadmin_in"
	copyStmt     = "COPY " + payloadTable + " (payload) FROM STDIN;"
	commitStmt   = "COMMIT;"

	exitOK      = 0
	exitRefused = 1
	exitUsage   = 2
)

// subcommand is one parsed and validated invocation.
type subcommand struct {
	name        string // create | reset-mfa | disable
	id          string // canonical lower-case uuid; generated for create
	email       string
	displayName string // create only
	host        string // create and reset-mfa
	generated   time.Time
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, stderrIsTerminal(), time.Now()))
}

// stderrIsTerminal reports whether stderr is a character device -- a terminal, or
// /dev/null -- from the descriptor's mode (TestMain_StderrIsTerminalReadsTheMode; the
// compiled command: TestMain_TheBinaryRefusesARedirectedStderr).
func stderrIsTerminal() bool {
	fi, err := os.Stderr.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// run is main's testable body: SQL to stdout, the report and the link to stderr, the
// exit code returned. terminal is whether stderr is a terminal and now is the
// generation time (main's facts, injected so both arms of each are tested).
func run(args []string, stdout, stderr io.Writer, terminal bool, now time.Time) int {
	cmd, code, err := parse(args, stderr)
	if err != nil {
		return refuse(stdout, stderr, code, err)
	}
	cmd.generated = now.UTC()

	var secret string
	if cmd.name == "create" || cmd.name == "reset-mfa" {
		if !terminal {
			return refuse(stdout, stderr, exitRefused, errors.New(
				"stderr is not a terminal. The enrollment link is printed once, on stderr, and a redirected "+
					"stderr would put it into a file or a log. Run this with stderr on the terminal and only "+
					"stdout redirected (deploy/README.md, \"Operator accounts (M10 OP-9)\")"))
		}
		if secret, err = newLinkSecret(); err != nil {
			return refuse(stdout, stderr, exitRefused, err)
		}
	}
	if cmd.name == "create" {
		if cmd.id, err = newAccountID(); err != nil {
			return refuse(stdout, stderr, exitRefused, err)
		}
	}

	var s script
	switch cmd.name {
	case "create":
		s = createScript(cmd, linkSecretHash(secret))
	case "reset-mfa":
		s = resetMFAScript(cmd, linkSecretHash(secret))
	default:
		s = disableScript(cmd)
	}

	// THE ORDER IS: every statement but COMMIT, then the report with the link, then
	// COMMIT. A link is printed only after the statements that store its hash were
	// written in full (a link whose hash is not in the database opens no account), and
	// COMMIT is written only after the link reached stderr: if the report cannot be
	// written, the poison statement takes COMMIT's place, so applying what stdout holds
	// commits no account whose link this run did not deliver
	// (TestLeak_AFailedReportWithholdsTheCommit).
	body := s.body()
	if _, err := io.WriteString(stdout, body); err != nil {
		return refuse(stdout, stderr, exitRefused, errors.New("writing the SQL to stdout failed after "+
			strconv.Itoa(len(body))+" bytes were prepared; what reached the destination (if anything) is a "+
			"truncated script, so do not apply it. No link was printed. ("+err.Error()+")"))
	}
	if _, err := io.WriteString(stderr, report(cmd, secret)); err != nil {
		return refuse(stdout, stderr, exitRefused, errors.New("the report could not be written to stderr, "+
			"so the SQL on stdout ends in the refusal statement instead of COMMIT and applies nothing"))
	}
	if _, err := io.WriteString(stdout, commitStmt+"\n"); err != nil {
		return refuse(stdout, stderr, exitRefused, errors.New("writing the final COMMIT to stdout failed; "+
			"the script there ends without COMMIT. Generate it again"))
	}
	return exitOK
}

// refuse prints the reason on stderr and a POISON statement on stdout. An empty stdout
// piped into psql succeeds; the poison makes psql print an ERROR, and with
// ON_ERROR_STOP=1 (the runbook's flag) exit 3 -- without it psql exits 0 (measured,
// deploy/README.md). Where it follows statements of this program's own transaction
// (run's report path), it stands in place of COMMIT.
func refuse(stdout, stderr io.Writer, code int, err error) int {
	_, _ = io.WriteString(stderr, "opadmin: REFUSED: "+err.Error()+"\n")
	if _, werr := io.WriteString(stdout, poisonStmt+"\n"); werr != nil {
		_, _ = io.WriteString(stderr, "opadmin: and the refusal could not be written to stdout either\n")
	}
	return code
}

const poisonStmt = "DO $$ BEGIN RAISE EXCEPTION " +
	"'opadmin refused: this script commits no change. Read the stderr of the opadmin run.'; END $$;"

// ------------------------------------------------------------------ parsing --

const usageText = `usage:
  opadmin create    --email ADDRESS --name "DISPLAY NAME" --host OPERATOR_HOST
  opadmin reset-mfa --id ACCOUNT_ID --email ADDRESS --host OPERATOR_HOST
  opadmin disable   --id ACCOUNT_ID --email ADDRESS

Writes SQL on stdout for psql as tappa_owner; create and reset-mfa print the
enrollment link once on stderr. Runbook: deploy/README.md, "Operator accounts (M10 OP-9)".
`

// onceFlag is a string flag that refuses a second value: `--email a --email b` would
// otherwise keep the last one silently.
type onceFlag struct {
	v   string
	set bool
}

func (f *onceFlag) String() string { return f.v }

func (f *onceFlag) Set(s string) error {
	if f.set {
		return errors.New("given more than once")
	}
	f.v, f.set = s, true
	return nil
}

func parse(args []string, stderr io.Writer) (subcommand, int, error) {
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, usageText)
		return subcommand{}, exitUsage, errors.New("no subcommand")
	}
	cmd := subcommand{name: args[0]}
	var wants []string
	switch cmd.name {
	case "create":
		wants = []string{"email", "name", "host"}
	case "reset-mfa":
		wants = []string{"id", "email", "host"}
	case "disable":
		wants = []string{"id", "email"}
	case "-h", "-help", "--help", "help":
		_, _ = io.WriteString(stderr, usageText)
		return subcommand{}, exitUsage, errors.New("help requested; nothing was produced")
	default:
		_, _ = io.WriteString(stderr, usageText)
		return subcommand{}, exitUsage, errors.New("unknown subcommand (want create, reset-mfa or disable)")
	}

	fs := flag.NewFlagSet("opadmin "+cmd.name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _, _ = io.WriteString(stderr, usageText) }
	vals := map[string]*onceFlag{}
	for _, name := range wants {
		vals[name] = &onceFlag{}
		fs.Var(vals[name], name, "")
	}
	if err := fs.Parse(args[1:]); err != nil {
		return subcommand{}, exitUsage, errors.New("the flags were not accepted (see above)")
	}
	if fs.NArg() > 0 {
		return subcommand{}, exitUsage, errors.New("unexpected argument after the flags; every value is a --flag")
	}
	for _, name := range wants {
		if !vals[name].set {
			return subcommand{}, exitUsage, errors.New("--" + name + " is required for " + cmd.name)
		}
	}

	var err error
	if v := vals["email"]; v != nil {
		if err = validEmail(v.v); err != nil {
			return subcommand{}, exitRefused, err
		}
		cmd.email = v.v
	}
	if v := vals["name"]; v != nil {
		if err = validDisplayName(v.v); err != nil {
			return subcommand{}, exitRefused, err
		}
		cmd.displayName = v.v
	}
	if v := vals["host"]; v != nil {
		if !isDNSHostName(v.v) {
			return subcommand{}, exitRefused, errors.New("--host must be the operator host exactly as " +
				"TAPPA_OPERATOR_HOST holds it: a lower-case DNS name such as ops.taptime.mt, with no scheme, " +
				"port, path, user part or trailing dot")
		}
		cmd.host = v.v
	}
	if v := vals["id"]; v != nil {
		if !isCanonicalUUID(v.v) {
			return subcommand{}, exitRefused, errors.New("--id must be the account id as PostgreSQL prints " +
				"it: 36 characters, lower-case hex in 8-4-4-4-12 groups")
		}
		cmd.id = v.v
	}
	return cmd, exitOK, nil
}

// ---------------------------------------------------------------- validation --

// validEmail accepts an ASCII address: a dot-atom local part (RFC 5322 atext and
// dots) and a domain of DNS labels, in at most 254 bytes. ASCII only, deliberately:
// the address is a sign-in identifier, and a look-alike letter from another script
// would be a second spelling of an operator's name. Messages name the rule, not the
// value.
func validEmail(s string) error {
	if len(s) == 0 || len(s) > maxEmailBytes {
		return errors.New("--email must be 1 to " + strconv.Itoa(maxEmailBytes) + " bytes")
	}
	at := strings.IndexByte(s, '@')
	if at < 0 || strings.IndexByte(s[at+1:], '@') >= 0 {
		return errors.New("--email must contain exactly one @")
	}
	local, domain := s[:at], s[at+1:]
	if len(local) == 0 || len(local) > maxLocalBytes {
		return errors.New("--email: the part before @ must be 1 to " + strconv.Itoa(maxLocalBytes) + " bytes")
	}
	for _, label := range strings.Split(local, ".") {
		if label == "" {
			return errors.New("--email: the part before @ may not start or end with a dot or hold two in a row")
		}
		for i := 0; i < len(label); i++ {
			if !isAtext(label[i]) {
				return errors.New("--email: the part before @ may hold only letters, digits and " +
					"!#$%&'*+-/=?^_`{|}~ (ASCII); quoted forms are not accepted")
			}
		}
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("--email: the domain must be dot-separated labels of 1 to 63 characters, " +
				"none starting or ending with a hyphen")
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return errors.New("--email: the domain may hold only ASCII letters, digits, hyphens and dots")
			}
		}
	}
	return nil
}

func isAtext(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("!#$%&'*+-/=?^_`{|}~", c) >= 0
}

// validDisplayName accepts 1 to 200 characters of valid UTF-8 drawn from letters,
// marks, numbers, punctuation, symbols and the ASCII space, with no space at either
// end. Refused rather than cleaned: control characters (a terminal escape in the
// report or in a psql NOTICE), format characters (bidirectional overrides, zero-width
// characters: a name that renders as someone else's) and every other separator.
func validDisplayName(s string) error {
	if !utf8.ValidString(s) {
		return errors.New("--name is not valid UTF-8")
	}
	n := utf8.RuneCountInString(s)
	if n == 0 || n > maxNameRunes {
		return errors.New("--name must be 1 to " + strconv.Itoa(maxNameRunes) + " characters")
	}
	if s[0] == ' ' || s[len(s)-1] == ' ' {
		return errors.New("--name may not start or end with a space")
	}
	for _, r := range s {
		if r == ' ' || unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S) {
			continue
		}
		return errors.New("--name may hold only letters, marks, digits, punctuation, symbols and plain " +
			"spaces; control, format and other separator characters are refused")
	}
	return nil
}

// isCanonicalUUID is PostgreSQL's output form of a uuid: lower-case hex, 8-4-4-4-12.
// One spelling, because the value goes into a link and into the SQL as it is.
func isCanonicalUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// isDNSHostName is internal/config's rule for TAPPA_OPERATOR_HOST, body for body
// (TestHost_IsConfigsRule compares the two): 1-253 bytes, dot-separated labels of
// 1-63 bytes drawn from [a-z0-9-], none starting or ending with a hyphen.
func isDNSHostName(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			b := label[i]
			if (b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '-' {
				return false
			}
		}
	}
	return true
}

// ------------------------------------------------------- secret, hash and id --

// newLinkSecret draws linkSecretBytes from crypto/rand as unpadded base64url --
// operatorauth's randomToken.
func newLinkSecret() (string, error) {
	b := make([]byte, linkSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("reading randomness failed")
	}
	v := base64.RawURLEncoding.EncodeToString(b)
	clear(b)
	return v, nil
}

// linkSecretHash is operatorauth's EnrollmentTokenHash: lower-case hex of a keyless
// SHA-256 over the secret's TEXT, the bytes 00026's convert_to(p_token, 'UTF8')
// hashes.
func linkSecretHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// newAccountID is a random (version 4, RFC 9562) uuid in canonical form. The id is
// this program's because the link must carry it before the row exists (ADR 0020 §6).
func newAccountID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errors.New("reading randomness failed")
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}

// enrollLink is the link OP-8's page reads.
func enrollLink(host, id, secret string) string {
	return "https://" + host + "/operator/enroll?id=" + id + "#" + secret
}

// ---------------------------------------------------------------------- SQL --

// script is the generated SQL: a comment header and the top-level statements before
// COMMIT, in order. The COPY entry carries its data line and the \. terminator. The
// tests apply stmts one by one, the way psql does.
type script struct {
	sub   string
	id    string
	stmts []string
}

// body is everything run writes before the report: the header and every statement
// but COMMIT.
func (s script) body() string {
	var b strings.Builder
	b.WriteString("-- GENERATED by cmd/opadmin " + s.sub + " (M10 OP-9, ADR 0020 §6) for operator account " + s.id + ".\n")
	b.WriteString("-- Review it, then apply it ONCE as tappa_owner with psql -X -v ON_ERROR_STOP=1 reading\n")
	b.WriteString("-- this file (< file, or -f file) -- not psql -c, which sends it as one statement text\n")
	b.WriteString("-- (measured), and not a GUI query tool (not measured). Runbook: deploy/README.md,\n")
	b.WriteString("-- \"Operator accounts (M10 OP-9)\". One transaction. The values travel as the one COPY\n")
	b.WriteString("-- data line below, in hex; the stderr report shows them readable.\n")
	if s.sub != "disable" {
		b.WriteString("-- The enrollment link is not in this file; the data line holds the SHA-256 of its secret.\n")
	}
	for _, st := range s.stmts {
		b.WriteString(st)
		b.WriteString("\n")
	}
	return b.String()
}

// String is the whole script as run writes it when the report reached stderr.
func (s script) String() string { return s.body() + commitStmt + "\n" }

// envelope wraps one DO body and one data line into the statements every subcommand
// emits. The data line is "-- ", copyPadding and the fields in hex, space-separated:
// COPY reads it as the one column; psql 18.4, reading the line as SQL because the COPY did not
// start (an earlier error, psql without ON_ERROR_STOP), dropped it as a leading
// comment -- measured (m10-platform.md, OP-9 card correction, round 2).
func envelope(sub, id string, fields []string, do string) script {
	return script{sub: sub, id: id, stmts: []string{
		"BEGIN;",
		"SET LOCAL search_path = pg_catalog, pg_temp;",
		"SET LOCAL lock_timeout = '" + lockTimeout + "';",
		"CREATE TEMP TABLE " + payloadTable + " (payload text NOT NULL) ON COMMIT DROP;",
		copyStmt + "\n-- " + copyPadding + " " + strings.Join(fields, " ") + "\n\\.",
		"DO " + dollarTag + "\n" + do + dollarTag + ";",
	}}
}

func hexOf(s string) string { return hex.EncodeToString([]byte(s)) }

// sqlTime is t as a timestamptz literal (UTC, microseconds).
func sqlTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000Z") }

// declare is the DECLARE block every DO body opens with.
func declare(vars ...string) string {
	var b strings.Builder
	b.WriteString("DECLARE\n")
	for _, v := range append([]string{"v_rows bigint", "v_payload text", "v_f text[]", "v_constraint text", "v_state text"}, vars...) {
		b.WriteString("    " + v + ";\n")
	}
	return b.String()
}

// roleGuard refuses a session that RLS would filter: platform_admins has FORCE ROW
// LEVEL SECURITY and its one policy is tappa_operator's SELECT (00026's note on a
// non-superuser owner says the same). TestApply_RefusesARoleRLSWouldFilter drives it
// with tappa_app and tappa_operator.
func roleGuard(sub string) string {
	return "    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles AS r\n" +
		"                    WHERE r.rolname = current_user AND (r.rolsuper OR r.rolbypassrls)) THEN\n" +
		"        RAISE EXCEPTION 'opadmin " + sub + ": % is neither a superuser nor BYPASSRLS. platform_admins has " +
		"FORCE ROW LEVEL SECURITY and is written by tappa_owner alone (ADR 0020 §6): apply this as tappa_owner. " +
		"Nothing was changed.', current_user;\n" +
		"    END IF;\n"
}

// readPayload reads the COPY data line into v_f and refuses anything but the one line
// of the expected shape: v_f[1] is the "--" marker, v_f[2] is copyPadding, and the hex
// fields follow from v_f[3], matching patterns in order.
func readPayload(sub string, patterns ...string) string {
	var b strings.Builder
	b.WriteString("    SELECT pg_catalog.count(*), pg_catalog.min(i.payload) INTO v_rows, v_payload FROM " + payloadTable + " AS i;\n")
	b.WriteString("    v_f := pg_catalog.string_to_array(v_payload, ' ');\n")
	b.WriteString("    IF v_rows <> 1 OR COALESCE(pg_catalog.cardinality(v_f), 0) <> " + strconv.Itoa(len(patterns)+2) +
		" OR v_f[1] IS DISTINCT FROM '--'\n       OR v_f[2] IS DISTINCT FROM '" + copyPadding + "'")
	for i, p := range patterns {
		b.WriteString("\n       OR NOT COALESCE(v_f[" + strconv.Itoa(i+3) + "] ~ '" + p + "', false)")
	}
	b.WriteString(" THEN\n")
	b.WriteString("        RAISE EXCEPTION 'opadmin " + sub + ": the COPY data is not the one line this script carries. " +
		"Nothing was changed.';\n")
	b.WriteString("    END IF;\n")
	return b.String()
}

const (
	hashPattern = "^[0-9a-f]{64}$"
	textPattern = "^([0-9a-f]{2})+$"
)

func decoded(field int) string {
	return "pg_catalog.convert_from(pg_catalog.decode(v_f[" + strconv.Itoa(field) + "], 'hex'), 'UTF8')"
}

// generationGuards refuse a create or reset-mfa script whose generation time is AHEAD
// of the database clock, and one older than scriptMaxAge. No allowance (round 3; refused
// leads measured down to 250 ms, TestScript_GenerationGuards):
// an accepted script then has a generation time at or before its own issuance, which
// is what lets the replay guard in resetMFAScript refuse a replay from a generating
// clock ahead of the database's (measured 2 s and 10 minutes ahead:
// TestResetMFA_AClockAheadDoesNotReopenAUsedLink). A script refused for being ahead
// applies once the database clock has passed its generation time -- the same file,
// applied again (TestScript_GenerationGuards).
func generationGuards(sub string) string {
	return "    IF v_generated > pg_catalog.clock_timestamp() THEN\n" +
		"        RAISE EXCEPTION 'opadmin " + sub + ": this script carries a generation time % that is % ahead of " +
		"the database clock; apply it again once the database clock has passed it, or fix the clock of the machine " +
		"that ran opadmin. Nothing was changed.', v_generated, v_generated - pg_catalog.clock_timestamp();\n" +
		"    END IF;\n" +
		"    IF pg_catalog.clock_timestamp() > v_generated + interval '" + scriptMaxAge + "' THEN\n" +
		"        RAISE EXCEPTION 'opadmin " + sub + ": this script was generated at % and is more than " + scriptMaxAge +
		" old; generate a new one. Nothing was changed.', v_generated;\n" +
		"    END IF;\n"
}

// constraintHandler turns a constraint's refusal into its name and SQLSTATE. The
// DETAIL line PostgreSQL would print echoes the row.
func constraintHandler(sub string) string {
	return "    EXCEPTION WHEN integrity_constraint_violation THEN\n" +
		"        GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME, v_state = RETURNED_SQLSTATE;\n" +
		"        RAISE EXCEPTION 'opadmin " + sub + ": refused by constraint % (SQLSTATE %). Nothing was changed.', " +
		"v_constraint, v_state;\n"
}

// createScript writes a PENDING account with a fresh link secret's hash.
func createScript(c subcommand, hash string) script {
	var b strings.Builder
	b.WriteString(declare("v_hash text", "v_email text", "v_name text", "v_now timestamptz",
		"v_generated timestamptz := '"+sqlTime(c.generated)+"'"))
	b.WriteString("BEGIN\n")
	b.WriteString(roleGuard(c.name))
	b.WriteString(readPayload(c.name, hashPattern, textPattern, textPattern))
	b.WriteString("    v_hash := v_f[3];\n")
	b.WriteString("    v_email := " + decoded(4) + ";\n")
	b.WriteString("    v_name := " + decoded(5) + ";\n")
	b.WriteString(generationGuards(c.name))
	b.WriteString("    v_now := pg_catalog.clock_timestamp();\n")
	b.WriteString("    BEGIN\n")
	b.WriteString("        INSERT INTO public.platform_admins\n")
	b.WriteString("               (id, email, display_name, status,\n")
	b.WriteString("                enroll_token_hash, enroll_issued_at, enroll_expires_at)\n")
	b.WriteString("        VALUES ('" + c.id + "'::uuid, v_email::public.citext, v_name, 'pending',\n")
	b.WriteString("                v_hash, v_now, v_now + interval '" + enrollTTL + "');\n")
	b.WriteString(constraintHandler(c.name))
	b.WriteString("    END;\n")
	b.WriteString("    RAISE NOTICE 'opadmin create: pending operator account % written for % (%); its enrollment link " +
		"works once, until % (the database clock)', '" + c.id + "', v_email, v_name, v_now + interval '" + enrollTTL + "';\n")
	b.WriteString("END\n")
	return envelope(c.name, c.id, []string{hash, hexOf(c.email), hexOf(c.displayName)}, b.String())
}

// targetRow locks the ONE account named by BOTH the id and the address, or refuses.
// Two identifiers on purpose: a mistyped id that happens to be another operator's is
// refused because the address does not match it.
func targetRow(c subcommand, into string) string {
	return "    SELECT " + into + "\n" +
		"      FROM public.platform_admins AS a\n" +
		"     WHERE a.id = '" + c.id + "'::uuid\n" +
		"       AND a.email OPERATOR(public.=) v_email::public.citext\n" +
		"       FOR UPDATE;\n" +
		"    IF NOT FOUND THEN\n" +
		"        RAISE EXCEPTION 'opadmin " + c.name + ": no operator account has BOTH id " + c.id +
		" and the email address given. Nothing was changed.';\n" +
		"    END IF;\n"
}

// revokeSessions ends the account's sessions that are not revoked yet, in one
// statement; a session revoked earlier keeps its revoked_at
// (TestDisable_EndsSessionsAndSignIn).
func revokeSessions(c subcommand) string {
	return "        UPDATE public.platform_sessions AS s\n" +
		"           SET revoked_at = pg_catalog.clock_timestamp()\n" +
		"         WHERE s.admin_id = '" + c.id + "'::uuid\n" +
		"           AND s.revoked_at IS NULL;\n" +
		"        GET DIAGNOSTICS v_revoked = ROW_COUNT;\n"
}

// resetMFAScript returns an account to PENDING with a new link (ADR 0020 §3, §6): the
// TOTP envelope and the password digest are removed (enrollment writes both again),
// the lock counter is reset, the live sessions are revoked, and the new hash and an
// unused stamp are written in the SAME statement (OP-5 card item 18:
// platform_admins_pending_token_unused refuses anything else).
//
// REFUSED, each by its own message: a disabled account (re-admitting a person is not
// an MFA reset); the account whose hash already IS this script's (an immediate second
// application); and -- the replay guard -- an account whose enroll_issued_at is at or
// after this script's generation time: a link was issued for it after this script was
// written, so applying it would issue an older script's hash over a newer one. That is
// the A-B-A replay (R1 applied and used, R2 applied and used, R1 applied again), which
// the hash comparison alone let through and which would re-open R1's used link
// (TestResetMFA_ABAReplayIsRefused, red before this guard).
//
// THE GUARD COMPARES TWO CLOCKS: the generation time comes from the machine that ran
// opadmin, enroll_issued_at from the database. generationGuards refuses a generation
// time ahead of the database clock, so an accepted R1 has T1 at or before its own
// issuance A1, and an issuance after it carries a timestamp at or after A1 by the same
// database clock -- at or after T1 -- which this guard refuses
// (TestResetMFA_AClockAheadDoesNotReopenAUsedLink). What is left is the database clock
// itself stepping backwards between two issuances, not measured (deploy/README.md,
// limit O9-5, which also gives the generating clock BEHIND the database's).
//
// The pre-checks run under FOR UPDATE, and the UPDATE repeats the status, hash and
// issue-time conditions in its WHERE and requires one row, so a check and the write
// see the same row (TestSQL_ResetRechecksUnderTheRowLock).
func resetMFAScript(c subcommand, hash string) script {
	var b strings.Builder
	b.WriteString(declare("v_hash text", "v_email text", "v_status text", "v_cur_hash text", "v_issued timestamptz",
		"v_now timestamptz", "v_revoked bigint", "v_generated timestamptz := '"+sqlTime(c.generated)+"'"))
	b.WriteString("BEGIN\n")
	b.WriteString(roleGuard(c.name))
	b.WriteString(readPayload(c.name, hashPattern, textPattern))
	b.WriteString("    v_hash := v_f[3];\n")
	b.WriteString("    v_email := " + decoded(4) + ";\n")
	b.WriteString(generationGuards(c.name))
	b.WriteString(targetRow(c, "a.status, a.enroll_token_hash, a.enroll_issued_at INTO v_status, v_cur_hash, v_issued"))
	b.WriteString("    IF v_status = 'disabled' THEN\n")
	b.WriteString("        RAISE EXCEPTION 'opadmin reset-mfa: account " + c.id + " is disabled; an MFA reset does not " +
		"re-admit a disabled operator. Nothing was changed.';\n")
	b.WriteString("    END IF;\n")
	b.WriteString("    IF v_cur_hash IS NOT DISTINCT FROM v_hash THEN\n")
	b.WriteString("        RAISE EXCEPTION 'opadmin reset-mfa: this script was already applied to account " + c.id +
		"; applying it again would re-open its link. Generate a new one. Nothing was changed.';\n")
	b.WriteString("    END IF;\n")
	b.WriteString("    IF v_issued IS NOT NULL AND v_issued >= v_generated THEN\n")
	b.WriteString("        RAISE EXCEPTION 'opadmin reset-mfa: an enrollment link was issued for account " + c.id +
		" at % (the database clock), not before this script was generated (%); applying it would issue an older " +
		"script over a newer one. Generate a new one. Nothing was changed.', v_issued, v_generated;\n")
	b.WriteString("    END IF;\n")
	b.WriteString("    v_now := pg_catalog.clock_timestamp();\n")
	b.WriteString("    BEGIN\n")
	b.WriteString("        UPDATE public.platform_admins AS a\n")
	b.WriteString("           SET status             = 'pending',\n")
	b.WriteString("               password_hash      = NULL,\n")
	b.WriteString("               totp_secret_sealed = NULL,\n")
	b.WriteString("               totp_failures      = 0,\n")
	b.WriteString("               totp_locked_until  = NULL,\n")
	b.WriteString("               enroll_token_hash  = v_hash,\n")
	b.WriteString("               enroll_issued_at   = v_now,\n")
	b.WriteString("               enroll_expires_at  = v_now + interval '" + enrollTTL + "',\n")
	b.WriteString("               enroll_used_at     = NULL\n")
	b.WriteString("         WHERE a.id = '" + c.id + "'::uuid\n")
	b.WriteString("           AND a.status <> 'disabled'\n")
	b.WriteString("           AND a.enroll_token_hash IS DISTINCT FROM v_hash\n")
	b.WriteString("           AND (a.enroll_issued_at IS NULL OR a.enroll_issued_at < v_generated);\n")
	b.WriteString("        GET DIAGNOSTICS v_rows = ROW_COUNT;\n")
	b.WriteString("        IF v_rows <> 1 THEN\n")
	b.WriteString("            RAISE EXCEPTION 'opadmin reset-mfa: account " + c.id + " changed under this script; " +
		"generate a new one. Nothing was changed.';\n")
	b.WriteString("        END IF;\n")
	b.WriteString(revokeSessions(c))
	b.WriteString(constraintHandler(c.name))
	b.WriteString("    END;\n")
	b.WriteString("    RAISE NOTICE 'opadmin reset-mfa: account % was %, is now pending; % live session(s) revoked; " +
		"its new enrollment link works once, until % (the database clock)', '" + c.id + "', v_status, v_revoked, " +
		"v_now + interval '" + enrollTTL + "';\n")
	b.WriteString("END\n")
	return envelope(c.name, c.id, []string{hash, hexOf(c.email)}, b.String())
}

// disableScript closes an account and revokes its live sessions. Applied to an account
// that is already disabled it leaves the status as it is, and its NOTICE names the
// status it found (TestDisable_EndsSessionsAndSignIn applies it twice). It carries no
// generation guard: it issues no link.
func disableScript(c subcommand) script {
	var b strings.Builder
	b.WriteString(declare("v_email text", "v_status text", "v_revoked bigint"))
	b.WriteString("BEGIN\n")
	b.WriteString(roleGuard(c.name))
	b.WriteString(readPayload(c.name, textPattern))
	b.WriteString("    v_email := " + decoded(3) + ";\n")
	b.WriteString(targetRow(c, "a.status INTO v_status"))
	b.WriteString("    BEGIN\n")
	b.WriteString("        UPDATE public.platform_admins AS a\n")
	b.WriteString("           SET status = 'disabled'\n")
	b.WriteString("         WHERE a.id = '" + c.id + "'::uuid;\n")
	b.WriteString(revokeSessions(c))
	b.WriteString(constraintHandler(c.name))
	b.WriteString("    END;\n")
	b.WriteString("    RAISE NOTICE 'opadmin disable: account % was %, is now disabled; % live session(s) revoked', '" +
		c.id + "', v_status, v_revoked;\n")
	b.WriteString("END\n")
	return envelope(c.name, c.id, []string{hexOf(c.email)}, b.String())
}

// ------------------------------------------------------------------- report --

// report is the stderr text: what the SQL will do, and for create and reset-mfa the
// link, which carries the link secret (TestLeak_TheSecretIsOnlyInTheLinkOnStderr
// measures stdout and the default loggers for it). The email address and display
// name are the operator's own input echoed to the operator's terminal (the SQL holds
// them as hex); this is a command's report, not a process log.
func report(c subcommand, secret string) string {
	var b strings.Builder
	b.WriteString("opadmin " + c.name + ": SQL written to stdout. Nothing has changed yet.\n\n")
	b.WriteString("  account id   " + c.id + "\n")
	b.WriteString("  email        " + c.email + "\n")
	if c.displayName != "" {
		b.WriteString("  name         " + c.displayName + "\n")
	}
	switch c.name {
	case "create":
		b.WriteString("  becomes      pending, until the link below is used\n")
	case "reset-mfa":
		b.WriteString("  becomes      pending: TOTP key and password removed, lock counter reset, live sessions revoked\n")
	case "disable":
		b.WriteString("  becomes      disabled, live sessions revoked\n")
	}
	b.WriteString("\nApply it as tappa_owner: deploy/README.md, \"Operator accounts (M10 OP-9)\".\n")
	if secret == "" {
		return b.String()
	}
	b.WriteString("Apply it within " + scriptMaxAge + " of now; the database refuses it after that.\n")
	b.WriteString("\nThe enrollment link works ONCE, for " + enrollTTL + " from the moment the SQL is applied\n")
	b.WriteString("(the database clock). It is printed here once; the SQL holds the SHA-256 of its\n")
	b.WriteString("secret, not the secret. Hand it to the operator over a channel that keeps no\n")
	b.WriteString("copy, then clear this terminal's scrollback:\n\n")
	b.WriteString(enrollLink(c.host, c.id, secret) + "\n\n")
	return b.String()
}
