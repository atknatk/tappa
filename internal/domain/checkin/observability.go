package checkin

import (
	"log/slog"

	"github.com/atknatk/tappa/internal/domain/tap"
)

// The ALERT INTERFACE of the tap decision engine (M8-03).
//
// 🔴 THESE STRINGS ARE A CONTRACT WITH deploy/README.md, NOT INTERNAL SPELLING.
// Four of the six M8-03 alert signals are queries over the two events below, and
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
// they catch different halves; neither replaces the other.
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
)

// The attribute keys of the two events above.
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
)

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
