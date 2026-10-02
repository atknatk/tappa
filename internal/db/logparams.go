package db

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// logParameterPin is the one server setting both pools pin on each connection they open
// in today's code (pinLogParameters; measured for a first and a later connection by
// TestPin_AConnectionTheParameterDidNotReachIsRefused) (M10 OP-7; backlog T79 for the
// customer pool, ADR 0021 "Karar verilmedi", condition 1, for the operator's).
//
// 🔴 WHY IT IS PINNED AT ALL. With log_min_error_statement = error (production's
// setting, measured 2026-09-26), a statement that ends in an ERROR is logged with its
// STATEMENT line, and log_parameter_max_length_on_error decides how much of its BOUND
// PARAMETERS go with it: 0 means none, anything else -- a positive length or -1,
// "unlimited" -- means the values. On these pools those values are an operator's raw
// enrollment token, a session token's hash, a password digest, an activation code's
// hash (CLAUDE.md §7's never-log list): every rejected op_* call and every failing
// customer statement would write them to the pod log and to the caller's error
// CONTEXT. And the setting is `user`-context (pg_settings.context, measured): the role
// that holds the DSN may change it in its own session AND write it as its own role
// default (`ALTER ROLE <itself> SET ... = -1` succeeds, measured in OP-5's 4th round),
// where it survives a password rotation and reaches every new pooled connection.
//
// 🔴 WHY A STARTUP PARAMETER, AND WHAT WAS MEASURED (dev Postgres 17.10, 2026-10-01,
// tappa_app given `ALTER ROLE ... IN DATABASE postgres SET log_parameter_max_length_on_error
// = -1` for the duration of the probe, then RESET -- pg_db_role_setting back to 0 rows):
//
//	a connection with no startup parameter                         -1   (the role default)
//	the same, with log_parameter_max_length_on_error=0 at startup    0   (the parameter wins)
//	options='-c ...=-1' AND the parameter 0                          0   (the explicit parameter wins)
//	a DSN carrying ...=-1 as a query parameter, then pinned          0   (the pin overwrites it)
//	the same role connecting to another database                     0   (the role default is per database)
//
// PostgreSQL ranks a client's startup parameter above a role's or a database's
// default, so the value the DSN holder can write does not reach a connection this pool
// opens -- measured: TestPin_TheStartupParameterOverridesARoleDefault re-measures the
// first two rows on every run.
//
// ⚠️ log_parameter_max_length IS NOT PINNED, AND THAT IS MEASURED TOO. It is
// `superuser`-context: a non-superuser connection that names it in its startup packet
// is REFUSED (42501, "permission denied to set parameter"), so pinning it would take
// both pools down; and for the same reason the DSN holder cannot change it either
// (SET and ALTER ROLE ... SET both 42501 as tappa_operator, measured). It only governs
// statement logging (log_statement, log_min_duration_statement, both superuser
// settings and off in production); production runs it at -1, which is also why the
// check below does not require it to be 0 -- that would refuse production.
const logParameterPin = "log_parameter_max_length_on_error"

// readLogParameterSQL reads the pinned setting back on a fresh connection.
// current_setting returns the value with its unit ("64B", "1kB"); zero is exactly "0"
// (measured), so any other text is "not pinned".
//
// The function is SCHEMA-QUALIFIED (2b). An unqualified name resolves through the
// session's search_path, and a schema listed before pg_catalog there could hold a
// current_setting of its own that answers "0". Whether either role could create one is
// not measured here; the qualified name means the read-back does not depend on the
// answer. TestReadLogParameterSQL_IsSchemaQualified pins the text.
const readLogParameterSQL = `SELECT pg_catalog.current_setting('log_parameter_max_length_on_error')`

// logParameterNotPinnedError is a connection on which the pinned setting is not 0. It
// carries the value the server reported -- a size such as "-1" or "64B", never a
// credential -- and nothing from the DSN.
type logParameterNotPinnedError struct{ Got string }

func (e *logParameterNotPinnedError) Error() string {
	return fmt.Sprintf("db: %s is %q on a new connection, not 0. Every statement this pool runs that ends in an "+
		"error would put its bound parameters into the server log and the caller's error context (CLAUDE.md §7). "+
		"The pool pins it to 0 as a connection startup parameter, which outranks a role or database default; a "+
		"value other than 0 here means the startup parameter did not take effect as sent -- for example behind a "+
		"connection pooler that does not forward startup parameters -- and the pool refuses the connection rather "+
		"than run on it", logParameterPin, e.Got)
}

// boundParameterModes are the pgx query exec modes a pool may run in: the ones that send
// a statement's arguments to the server as BOUND PARAMETERS, so the statement text the
// server logs on an error carries $1, $2 ... and the arguments are governed by the pinned
// setting above. Measured on every run (TestQueryExecModes_OnlyTheListedOnesBindOnTheServer:
// current_query() under each of pgx's five modes, with a sentinel argument): these four
// keep the sentinel out of the text; simple_protocol puts it in.
//
// 🔴 WHY THIS IS PART OF THE PIN (2c, the security auditor's finding, measured): pgx
// reads `default_query_exec_mode` from the DSN ITSELF and does not send it to the server
// (pgx's ParseConfig, read), so the read-back below does not see it. With `?default_query_exec_mode=simple_protocol`
// both pools opened, the read-back answered "0", and every argument was interpolated
// into the SQL text on this side -- the text a failing statement's STATEMENT line
// writes to the pod log under production's log_min_error_statement = error (a session
// hash, a raw enrollment token, a code's hash: CLAUDE.md §7). So a pool refuses a
// configuration whose mode is not on this list, and does not rewrite it (the
// operator host's precedent: one accepted form, refused rather than normalised).
// pgx matches the key and the value case-sensitively (measured: an upper-case key is
// not read by pgx and reaches the server as an unknown setting, 42704; an upper-case
// value is a parse error), so the parsed mode is the one fact to check.
//
// THE CLAIM, IN THREE PARTS AND ONLY THREE (2h, the orchestrator's decision; agent-brief
// M8-02 FAZ C. Five closing rounds each found a place outside the pins, because the text
// read as a guarantee against FUTURE, ARBITRARY code changes, which no test can give).
//
// PART I -- TODAY'S SHIPPED CODE, MEASURED. Every connection of the two pools runs in a
// mode that sends arguments as bound parameters, with log_parameter_max_length_on_error
// at zero. Measured by:
//   - TestPools_KeepArgumentsOutOfTheStatementText: three connections of each pool (the
//     customer pool in dev AND production env, the operator pool through its test hook),
//     no argument in current_query();
//   - TestPin_ADSNCannotChooseClientSideInterpolation: a DSN asking for simple_protocol
//     is refused by both constructors, before connecting;
//   - TestPin_AModeChangedAfterTheCheckIsRefusedOnItsConnection: the per-connection check
//     refuses a connection whose own mode interpolates;
//   - TestQueryExecModes_OnlyTheListedOnesBindOnTheServer: which modes interpolate;
//   - the pin and its read-back: TestPin_TheStartupParameterOverridesARoleDefault,
//     TestPin_ADSNCannotUnpinIt, TestPin_ADifferentlyCasedKeyCannotUnpinIt,
//     TestPin_AConnectionTheParameterDidNotReachIsRefused,
//     TestPin_AReadBackThatCannotRunIsRefused, TestLogParameterPinned_OnlyTheTextZero.
//
// PART II -- NAMED PINS AND WIRES, AND EXACTLY WHAT EACH CATCHES:
//   - TestConstructors_BodiesAreTheReviewedOnes: the tokens of the bodies of newDB,
//     openOperatorDB, pinLogParameters and requireBoundParameters, and of this list's
//     initializer;
//   - TestConstructors_TheHookReachesOnlyThePin: New's and NewOperatorDB's bodies as
//     written, and the hook used only as pinLogParameters' third argument;
//   - TestProductCode_ExecModeWireAndConnectWire: the MODE wire (the spellings in
//     execModeEscapes; its allowed region is a PACKAGE-LEVEL declaration named
//     boundParameterModes or requireBoundParameters in logparams.go, by the file's own
//     position -- a method of that name, or a //line directive naming that file, is not
//     in it -- with exactly 16 allowed uses), the CONNECT wire (connectFuncs outside the
//     two constructors), the POOLCFG wire (the written forms listed at poolConfigHooks);
//   - closed structural rules: cmd/tappa's TestBinary_LinksNoTestCode (the binary's
//     dependency closure for linux/amd64 with CGO_ENABLED=0 holds no testing package and
//     no pgxtest) and TestProductCode_CarriesNoBuildConstraint (the constraint kinds
//     listed there).
//
// PART III -- NO COMPLETENESS CLAIM: any code change the pins and wires above do not list
// -- unpinned helpers (requireLogParametersPinned, logParameterPinned, readRole, ...),
// imports, other initializers, behaviour conditional on the environment, reflect and
// unsafe, and a mode chosen per call (the same class as SQL built in code: CLAUDE.md §6
// and operator.go's bound-parameter pin) -- is the subject of code review.
var boundParameterModes = []pgx.QueryExecMode{
	pgx.QueryExecModeCacheStatement,
	pgx.QueryExecModeCacheDescribe,
	pgx.QueryExecModeDescribeExec,
	pgx.QueryExecModeExec,
}

// requireBoundParameters refuses a connection configuration whose default query exec
// mode is not in boundParameterModes. variable names the pool's environment variable;
// fromDSN says whether cfg is the DSN as parsed (the constructors' check) or a
// connection's own configuration (the per-connection check), so the message does not
// blame the DSN for a mode that code set (2f). The message carries the mode's name (one
// of pgx's five) and nothing from the DSN.
func requireBoundParameters(cfg *pgx.ConnConfig, variable string, fromDSN bool) error {
	if slices.Contains(boundParameterModes, cfg.DefaultQueryExecMode) {
		return nil
	}
	return &execModeRefusedError{variable: variable, fromDSN: fromDSN,
		mode: strings.ReplaceAll(cfg.DefaultQueryExecMode.String(), " ", "_")}
}

// execModeRefusedError is requireBoundParameters' refusal: a TYPE, so the operator pool
// passes it through intact when the per-connection check refuses inside its ping (as it
// does the pin's two refusals) instead of classifying it as an unrecognised error.
type execModeRefusedError struct {
	variable, mode string
	fromDSN        bool
}

func (e *execModeRefusedError) Error() string {
	const why = "which makes pgx write every statement's arguments into its SQL text on this side: a statement " +
		"that fails would put them into the server log with its text, past the %s pin. This pool accepts only " +
		"modes that send arguments as bound parameters (cache_statement, the default; cache_describe; " +
		"describe_exec; exec)"
	if e.fromDSN {
		return fmt.Sprintf("db: %s asks for default_query_exec_mode=%s, "+why+"; remove the parameter",
			e.variable, e.mode, logParameterPin)
	}
	return fmt.Sprintf("db: the connection's configuration (the %s pool) is in query exec mode %s, "+why+
		"; the DSN was checked when the pool was built, so the mode was set after that, in code",
		e.variable, e.mode, logParameterPin)
}

// logParameterReadError is a connection on which READING the pinned setting failed with
// a server error. It carries the SQLSTATE and nothing else (the server's message can
// echo what the session sent), and it is a refusal: the server was reached, and a check
// that could not run has not passed (2b; TestPin_AReadBackThatCannotRunIsRefused).
type logParameterReadError struct{ Code string }

func (e *logParameterReadError) Error() string {
	return fmt.Sprintf("db: reading %s on a new connection failed (SQLSTATE %s), so the connection is refused: a "+
		"check that could not run has not passed", logParameterPin, e.Code)
}

// pinLogParameters sets the startup parameter on cfg and installs, as the pool's
// AfterConnect, the check that reads it back on each connection the pool opens (measured
// for the first and a later connection: TestPin_AConnectionTheParameterDidNotReachIsRefused).
//
// THE CHECK RUNS PER CONNECTION, NOT ONCE AT BOOT, and the boot refusal is its first
// run: New and NewOperatorDB ping before handing a pool back, the ping opens the first
// connection, and a refused connection fails the ping. A later connection (pool growth,
// a replaced idle or expired connection) is checked the same way while this stays the
// pool's AfterConnect (measured: TestPin_AConnectionTheParameterDidNotReachIsRefused), so
// a topology change after start-up -- the pooler case above -- refuses connections
// loudly instead of serving on them silently. Its cost is one round trip per NEW
// connection, not per statement.
//
// before runs first on each connection. Production passes nil; this package's tests
// use it to impersonate a NOLOGIN role or to change the setting after startup.
//
// The same per-connection check refuses a connection whose OWN query exec mode is not
// in boundParameterModes (2e; TestPin_AModeChangedAfterTheCheckIsRefusedOnItsConnection),
// as long as it is the pool's AfterConnect: code that replaces or bypasses the hook is
// outside it (PART III of the claim at boundParameterModes). variable names the
// environment variable in the message.
//
// The pin REPLACES whatever the DSN brought for the same setting, in ANY CASE: pgx turns
// an unknown query parameter into a startup parameter and keeps the case it was written
// in, and PostgreSQL reads setting names case-blind. So `?log_parameter_max_length_on_error=-1`
// was overwritten from the first round, but `?LOG_PARAMETER_MAX_LENGTH_ON_ERROR=-1` stayed
// a second map key, both reached the server in map order, and the last one won (2nd
// round, measured: of 30 opens per pool, 7-11 refused or unpinned -- the 1st auditor saw
// 10 and 12). Every key equal to the setting's name case-insensitively is deleted first
// (TestPinLogParameters_LeavesOneSpellingOfTheKey, TestPin_ADifferentlyCasedKeyCannotUnpinIt:
// 0 of 30, both pools, both spellings). An `options=-c ...` value needs nothing: the
// server applies the explicit startup parameters after `options` (measured).
func pinLogParameters(cfg *pgxpool.Config, variable string, before func(context.Context, *pgx.Conn) error) {
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	for k := range cfg.ConnConfig.RuntimeParams {
		if strings.EqualFold(k, logParameterPin) {
			delete(cfg.ConnConfig.RuntimeParams, k)
		}
	}
	cfg.ConnConfig.RuntimeParams[logParameterPin] = "0"
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		if before != nil {
			if err := before(ctx, c); err != nil {
				return err
			}
		}
		if err := requireBoundParameters(c.Config(), variable, false); err != nil {
			return err
		}
		return requireLogParametersPinned(ctx, c)
	}
}

// requireLogParametersPinned reads the setting on c and refuses anything but "0". A
// read that fails with a server error is a refusal (logParameterReadError); any other
// failure -- the network, the dial's deadline -- is wrapped, so the operator pool
// classifies it like any other failure during its ping.
func requireLogParametersPinned(ctx context.Context, c *pgx.Conn) error {
	var got string
	if err := c.QueryRow(ctx, readLogParameterSQL).Scan(&got); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			return &logParameterReadError{Code: pg.Code}
		}
		return fmt.Errorf("db: reading %s on a new connection: %w", logParameterPin, err)
	}
	if !logParameterPinned(got) {
		return &logParameterNotPinnedError{Got: got}
	}
	return nil
}

// logParameterPinned is the read-back's one accepted answer: the exact text "0", which
// is what the server reports for zero (measured; a size comes with its unit -- "64B",
// "1kB" -- and unlimited is "-1"). Anything else, an empty answer included, is refused
// (TestLogParameterPinned_OnlyTheTextZero).
func logParameterPinned(got string) bool { return got == "0" }
