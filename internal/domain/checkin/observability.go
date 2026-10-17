package checkin

import (
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/domain/tap"
)

// The ALERT INTERFACE of the tap decision engine (M8-03).
//
// 🔴 THESE STRINGS ARE A CONTRACT WITH deploy/README.md, NOT INTERNAL SPELLING.
// Most of the M8-03 alert rules are queries over the events below (which rule reads
// which is cmd/tappa's TestSignozRules_FilterOnThePinnedNames), and
// a renamed field does not break such a query — it makes it match nothing. The
// failure mode is an alert that reads "no rejects, no security attempts, healthy"
// forever, which is indistinguishable from the good state and therefore worse than
// no alert at all.
//
// TestObservability_AlertSignalNames (cmd/tappa) is what stops that: it asserts
// each constant below is the exact literal the rule in deploy/README.md filters on,
// so renaming one without rewriting the rule fails the build.
//
// ⚠️ COUNTED LIMIT: that test pins the NAMES, not the emission. Deleting the
// s.log.Log call in Record leaves every name below in the source and the pin
// green — it is TestSeedDB_ATapEmitsTheDecisionSignal (internal/handler) that
// drives a real tap and fails, and that one needs a database. Two tests because
// they catch different halves; neither replaces the other. EventPolicyFallback's
// emission is held in this package and without one:
// TestPolicyFallback_EachForTenantPathNamesItsReason drives all four paths.
const (
	// EventTapDecision is emitted once per DECIDED tap — the reject-rate, the
	// flag-queue and the ctr-gap signals are all filters over it.
	EventTapDecision = "tap.decision"

	// EventTapSecurityAlert is emitted for ANY non-empty policy.SecurityAlert, and
	// there are TWO of those — this comment named only one for a round, and so did
	// the runbook rule that reads it:
	//
	//   - §5 row 4, sys:employee-deactivated -> AlertDeactivatedEmployeeTapped: a
	//     live session on a deactivated account touched a plaque. Somebody who left
	//     is still using their phone, or a session was stolen.
	//   - §5 row 1, sys:tag-not-active on a tag whose status is `lost` ->
	//     AlertLostTagTapped: a plaque REPORTED LOST was tapped. A different
	//     incident with a different response — the plaque is off its wall.
	//
	// `retired` and `unassigned` do NOT alert; that is routine lifecycle
	// (internal/policy/guardrails.go). matched_sid on the accompanying
	// EventTapDecision record is what tells an operator which path fired, and
	// deploy/README.md's rule 3 now says so.
	//
	// ⚠️ ONLY THE FIRST PATH WAS DRIVEN BY A TEST, WHICH IS HOW THE HALF-DESCRIPTION
	// SURVIVED. Both are now: TestSeedDB_ADeactivatedAccountTapRaisesTheSecurityEvent
	// and TestSeedDB_ALostPlaqueTapRaisesTheSameSecurityEvent (internal/handler).
	//
	// 🔴 IT IS DEFINED AS ActionTapSecurityAlert RATHER THAN AS A SECOND LITERAL,
	// and the equality is the point: the log line and the audit_log row name ONE
	// act. An operator who sees this event in the log searches the trail for the
	// same string and finds the evidence; two strings that merely happened to look
	// alike would drift the first time one of them was reworded, and the drift
	// would be silent on both sides.
	EventTapSecurityAlert = ActionTapSecurityAlert

	// EventPolicyFallback is emitted when a tap is decided on the GUARDRAILS ALONE
	// because forTenant could not assemble the tenant's policy set (policyset.go).
	// No baseline statement can match, so an `ok` lands on the fail-to-review default
	// and is usually recorded as a `flag` for a manager (§4.6) — unless the database
	// is unreachable altogether, when the WithTenant that records the tap fails too
	// and the person is told to tap again.
	//
	// 🔴 IT IS AN EVENT NAME AND NOT THE SENTENCE IT REPLACED (T122). The four paths
	// used to write four sentences ending "; deciding on guardrails alone", and an
	// alert could only have matched that tail with a LIKE: free text, which a reworded
	// log line breaks without breaking anything else. deploy/README.md's M8-03 rule 8
	// matches msg against this constant, and which path fired is LogReason, from the
	// closed set below.
	//
	// Since T121 (f2c7771) no harmless source of this line is known — concurrent
	// first taps were the last one — so a single occurrence is worth a person's look.
	// A request whose own context has ended (client gone, request timeout) does not
	// emit it: that tap is not recorded at all, and fallback writes a non-paging WARN
	// line instead.
	//
	// ⚠️ stored() — the panel's StoredSet read — does NOT emit it. Its guardrails-alone
	// answer is an authorisation REFUSAL (fail-closed), and for a business no tap has
	// provisioned yet it is the expected state, not an incident.
	EventPolicyFallback = "policy.guardrails_alone"
)

// The attribute keys of the events above.
const (
	LogVerdict     = "verdict"
	LogChannel     = "channel"
	LogMatchedSid  = "matched_sid"
	LogPolicyLayer = "policy_layer"
	LogTrust       = "trust"
	LogCtrGap      = "ctr_gap"
	LogPractice    = "practice"
	LogTenantID    = "tenant_id"
	LogEmployeeID  = "employee_id"
	LogTagUID      = "tag_uid"

	// LogReason is which forTenant path an EventPolicyFallback line came from.
	LogReason = "reason"
	// LogSQLState and LogConstraint are a PostgreSQL error reduced to what a log may
	// carry (pgErrorAttrs).
	LogSQLState   = "sqlstate"
	LogConstraint = "constraint"
)

// The closed set of LogReason values on EventPolicyFallback, one per path in
// forTenant that answers with the guardrails alone. Closed so the runbook row can
// say what each one means and a responder can group on it.
const (
	// FallbackLoadFailed: the tenant's stored policy layer could not be read.
	FallbackLoadFailed = "load_failed"
	// FallbackMaterialiseFailed: the baseline was missing and writing it failed.
	FallbackMaterialiseFailed = "materialise_failed"
	// FallbackReloadFailed: writing succeeded, reading it back did not.
	FallbackReloadFailed = "reload_failed"
	// FallbackIncomplete: written and read back, and still not all there — most often
	// a stored document this release cannot parse (db/queries/policies.sql names the
	// other shape: a policy_versions row holding the derived id for another policy).
	FallbackIncomplete = "incomplete_after_materialise"
)

// pgErrorAttrs is what a log line may say about a PostgreSQL error in err's chain:
// its SQLSTATE and the name of the constraint it hit. Nothing else, and nil when the
// chain holds no *pgconn.PgError.
//
// 🔴 NOT Detail, NOT Where, NOT Hint — NOT ANY OTHER FIELD. Detail carries ROW
// VALUES: a unique violation reads "Key (id)=(…) already exists", a check violation
// "Failing row contains (…)". Where and InternalQuery can quote SQL. Code and
// ConstraintName are schema facts — they come from the migrations in db/migrations,
// never from a row — and they are what tells a 23505 on a unique index from a 23503
// on a foreign key, which a sentence alone did not. (internal/db's operator file goes
// further and drops ConstraintName as well — its rule (c), for 00026's functions; on
// this path the T121 review asked for the name, and it is public DDL.)
//
// MEASURED (TestPolicySetDB_AFallbackCarriesTheRealSQLStateAndNotTheKey): for
// tappa_app the server sends NO Detail on such a collision — row-level security makes
// Postgres omit the key description — so on today's roles this is defence in depth.
// It is kept unconditional because which role and which table an error came from is
// not something a log helper should have to know.
//
// BOTH KEYS OR NEITHER: constraint is "" when the server named none, so the presence
// of the pair means exactly one thing (a PgError was behind this line) and a reader
// never has to interpret one key without the other.
//
// ⚠️ COUNTED LIMIT: the line's own "err" attribute is err.Error(), which for a
// PgError is Severity + Message + SQLSTATE (pgconn). Detail is not in it, but the
// server's Message is, and for a few classes it quotes input (22P02 names the text
// it could not parse). That was true of these lines before this helper existed and
// it is not widened here.
func pgErrorAttrs(err error) []any {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg == nil {
		return nil
	}
	return []any{LogSQLState, pg.Code, LogConstraint, pg.ConstraintName}
}

// decisionLevel maps a verdict onto a log level.
//
// reject and flag are WARN because both are outcomes a person is standing in
// front of a plaque experiencing: one was refused, the other is waiting on a
// manager. ok and ignored are INFO.
//
// 🔴 NOTHING IS ERROR, DELIBERATELY — AND THE REASON WAS RESTATED AFTER BEING
// MEASURED WRONG. This comment used to say that an ERROR verdict "would make the 5xx
// rule fire on a person tapping a retired plaque". It would not: the rule that
// shipped is
//
//	body.msg = "http.request" AND body.status >= 500
//
// so it keys on the EVENT and the STATUS and never looks at the level. A
// tap.decision record cannot enter it whatever level it carries.
//
// The real cost is coarser and slower. level=ERROR is the one filter an operator can
// type without knowing this product's event names, and httpx.AccessLog gives it a
// single meaning: THIS SERVER IS BROKEN. A rejected tap is the product working
// correctly, so moving it to ERROR does not misfire an alert — it fills the
// operator's broadest filter with routine traffic until the genuine 5xx lines are
// invisible in it. Nothing reports that, which is exactly why it needed a test:
// TestDecisionLevel_NothingIsError (this package) pins every arm, including the
// default one.
func decisionLevel(v tap.Verdict) slog.Level {
	switch v {
	case tap.VerdictReject, tap.VerdictFlag:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
