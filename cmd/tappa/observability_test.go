package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/httpx"
)

// observability_test.go — the cross-cutting half of M8-03. These checks span
// packages and a document, so they live where the other repository-wide pins do
// (testnames_test.go, packaging_test.go) rather than in any one package.

// runbookPath is the operator document the alert rules live in.
var runbookPath = filepath.Join(repoRoot, "deploy", "README.md")

// alertSignalMarker is the heading the rules sit under. Finding it is asserted
// separately from finding the names, so "the section was deleted" and "a name
// drifted" are different failures.
const alertSignalMarker = "M8-03 — UYARI KURALLARI"

// TestObservability_AlertSignalNames is what stops an alert from dying silently.
//
// 🔴 THE FAILURE THIS EXISTS FOR IS NOT A CRASH. A monitoring query keyed on a
// field name does not break when the field is renamed — it MATCHES NOTHING, which
// renders as "no rejects, no security attempts, no 5xx" and is indistinguishable
// from a healthy system. There is no runtime signal for it at all; the only place
// it can be caught is here, at build time, by requiring the constant and the
// document to be the same string.
//
// ⚠️ WHAT IT DOES NOT CATCH, COUNTED: renaming a constant AND rewriting the
// document in the same change passes, as it should — that is a deliberate,
// reviewable edit. What it catches is the one-sided change, which is the one
// nobody notices.
func TestObservability_AlertSignalNames(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile(runbookPath)
	if err != nil {
		t.Fatalf("read %s: %v", runbookPath, err)
	}
	doc := string(b)

	if !strings.Contains(doc, alertSignalMarker) {
		t.Fatalf("deploy/README.md no longer contains %q — the alert rules section is gone, "+
			"so the names below are pinned against nothing", alertSignalMarker)
	}

	// name -> the literal every rule must filter on. Written out as literals ON
	// PURPOSE: comparing the constant to itself would pass whatever it was renamed
	// to, and the document is not the only consumer — a query typed into a log tool
	// is the other one, and it cannot be recompiled.
	want := map[string]string{
		"checkin.EventTapDecision":       "tap.decision",
		"checkin.EventTapSecurityAlert":  "tap.security_alert",
		"checkin.LogVerdict":             "verdict",
		"checkin.LogChannel":             "channel",
		"checkin.LogMatchedSid":          "matched_sid",
		"checkin.LogCtrGap":              "ctr_gap",
		"checkin.LogTenantID":            "tenant_id",
		"checkin.LogEmployeeID":          "employee_id",
		"httpx.EventHTTPRequest":         "http.request",
		"httpx.LogRequestIDKey":          "request_id",
		"httpx.LogStatusKey":             "status",
		"httpx.LogRouteKey":              "route",
		"handler.EventReadinessLost":     "readiness.lost",
		"handler.EventReadinessRegained": "readiness.regained",
	}
	got := map[string]string{
		"checkin.EventTapDecision":       checkin.EventTapDecision,
		"checkin.EventTapSecurityAlert":  checkin.EventTapSecurityAlert,
		"checkin.LogVerdict":             checkin.LogVerdict,
		"checkin.LogChannel":             checkin.LogChannel,
		"checkin.LogMatchedSid":          checkin.LogMatchedSid,
		"checkin.LogCtrGap":              checkin.LogCtrGap,
		"checkin.LogTenantID":            checkin.LogTenantID,
		"checkin.LogEmployeeID":          checkin.LogEmployeeID,
		"httpx.EventHTTPRequest":         httpx.EventHTTPRequest,
		"httpx.LogRequestIDKey":          httpx.LogRequestIDKey,
		"httpx.LogStatusKey":             httpx.LogStatusKey,
		"httpx.LogRouteKey":              httpx.LogRouteKey,
		"handler.EventReadinessLost":     handler.EventReadinessLost,
		"handler.EventReadinessRegained": handler.EventReadinessRegained,
	}

	names := make([]string, 0, len(want))
	for n := range want {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		if got[n] != want[n] {
			t.Errorf("%s = %q, want %q — every alert rule in deploy/README.md filters on the literal, "+
				"so renaming the constant makes those rules match nothing", n, got[n], want[n])
			continue
		}
		if !mentionsWholeName(doc, want[n]) {
			t.Errorf("deploy/README.md does not mention %q (%s) as a WHOLE name. Either the "+
				"rule was dropped or the name drifted; an alert nobody can find is not an "+
				"alert", want[n], n)
		}
	}

	// 🔴 AND THE EVENT NAMES ARE CHECKED AGAINST THE PASTE-ABLE BLOCK, NOT THE WHOLE
	// FILE, BECAUSE THE WHOLE-FILE CHECK ABOVE WAS MEASURED AND FOUND WEAK. A
	// mutation renamed the readiness rule's event in the RULES TABLE and this test
	// stayed green: the old literal still appeared three more times elsewhere in the
	// document, so "the document mentions it" was satisfied by prose while the thing
	// an operator copies had silently stopped matching.
	//
	// The block below is what somebody pastes into a log tool. A name that is not in
	// it is not a rule, whatever the prose says.
	//
	// ⚠️ COUNTED LIMIT: only the EVENT names (the msg values) are held to this. The
	// ATTRIBUTE names legitimately live in the table's prose columns — an operator
	// reads "look at employee_id" rather than pasting it — so requiring them here
	// would be requiring a filter that should not exist. Measured on this document:
	// channel, tenant_id, employee_id, request_id and readiness.regained are prose;
	// the four below are filters.
	block := pasteableFilterBlock(t)
	for _, event := range []string{
		checkin.EventTapDecision,
		checkin.EventTapSecurityAlert,
		httpx.EventHTTPRequest,
		handler.EventReadinessLost,
	} {
		if !mentionsWholeName(block, event) {
			t.Errorf("the paste-able filter block in deploy/README.md does not contain %q as a "+
				"WHOLE name. The prose may still name it, but the query an operator actually "+
				"copies no longer matches that event — which renders as a permanently quiet "+
				"alert:\n%s", event, block)
		}
	}
}

// nameChar is what can EXTEND an event or attribute name. A match flanked by one of
// these is not the name; it is a longer name that starts or ends with it.
func nameChar(r byte) bool {
	return r == '_' || r == '.' || r == '-' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// mentionsWholeName reports whether text contains name as a complete token.
//
// 🔴 IT REPLACES strings.Contains, AND THE REPLACEMENT WAS MEASURED RATHER THAN
// REASONED (backlog T54 item 1, closed M8-04 FAZ B3). Renaming the event in
// deploy/README.md from `tap.decision` to `tap.decisionMUT` — six occurrences,
// INCLUDING the paste-able filter block an operator copies — left this whole test
// GREEN, because "tap.decisionMUT" contains "tap.decision". The rename that a
// substring check DOES catch is one that shortens or alters the stem
// (`tap.verdictline` fails); the one it misses is a suffix, which is exactly the
// shape a typo and a half-finished rename take.
//
// The constant-versus-literal comparison above is unaffected either way: renaming
// the Go constant fails on equality. This is about the DOCUMENT half, where the
// only reader is a human copying a filter into a log tool.
//
// ⚠️ COUNTED LIMIT: this is a token check, not a parse. The document could still
// quote a name inside a sentence that says the opposite ("we no longer emit
// tap.decision") and satisfy it. What it holds is the one-sided RENAME, which is
// the failure the alert rules actually suffer.
func mentionsWholeName(text, name string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], name)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(name)
		beforeOK := start == 0 || !nameChar(text[start-1])
		afterOK := end == len(text) || !nameChar(text[end])
		if beforeOK && afterOK {
			return true
		}
		i = start + 1
	}
}

// pasteableFilterBlock returns the fenced block of copy-and-paste filters inside
// the alert rules section.
func pasteableFilterBlock(t *testing.T) string {
	t.Helper()
	const opener = "# 1 — reject"
	section := alertRulesSection(t)
	i := strings.Index(section, opener)
	if i < 0 {
		t.Fatalf("deploy/README.md's alert section no longer contains the paste-able filter block "+
			"(looked for %q); the rules may still be described in prose, but nothing is copyable", opener)
	}
	j := strings.Index(section[i:], "```")
	if j < 0 {
		t.Fatal("the paste-able filter block is not closed by a fence")
	}
	return section[i : i+j]
}

// TestObservability_LogFormatIsSelectableAndClosed pins the two halves of the
// TAPPA_LOG_FORMAT decision: json really produces JSON (the collector reads it),
// and an unknown value is a REFUSAL rather than a silent fall back to text.
//
// The second half is the one worth a test. parseLevel falls back to info on a
// typo, which is right for a verbosity knob; the same shape here would give a
// production deployment logfmt again, the collector would index no fields, and
// every rule in deploy/README.md would match zero rows — which reads exactly like
// a quiet, healthy system.
func TestObservability_LogFormatIsSelectableAndClosed(t *testing.T) {
	// NOT t.Parallel: the closed-set half drives config.Load through t.Setenv.
	var buf bytes.Buffer
	slog.New(logHandler(&buf, &config.Config{LogFormat: config.LogFormatJSON, LogLevel: "info"})).
		Info("probe", "k", "v")
	m := map[string]any{}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m); err != nil {
		t.Fatalf("json format did not produce JSON: %q: %v", buf.String(), err)
	}
	if m["k"] != "v" {
		t.Fatalf("json record lost its attribute: %v", m)
	}

	buf.Reset()
	slog.New(logHandler(&buf, &config.Config{LogFormat: config.LogFormatText, LogLevel: "info"})).
		Info("probe", "k", "v")
	if !strings.Contains(buf.String(), "k=v") {
		t.Fatalf("text format did not produce logfmt: %q", buf.String())
	}

	// The closed set. config.Load is the gate; logHandler has no unknown branch
	// precisely because Load refuses first.
	t.Setenv("TAPPA_LOG_FORMAT", "jsn")
	for _, k := range requiredEnvForLoad() {
		t.Setenv(k.name, k.value)
	}
	_, err := config.Load()
	if err == nil {
		t.Fatal("config.Load accepted TAPPA_LOG_FORMAT=jsn; a typo must refuse the boot, not " +
			"silently ship logfmt to a collector that indexes no fields")
	}
	if !strings.Contains(err.Error(), "TAPPA_LOG_FORMAT") {
		t.Fatalf("the refusal does not name the variable: %v", err)
	}
}

type envPair struct{ name, value string }

// requiredEnvForLoad is the minimum config.Load needs to get far enough to
// report the log-format error rather than only the missing-secret errors. Load
// accumulates every error, so this exists to keep the assertion above honest
// about WHICH error it found rather than to make Load succeed.
func requiredEnvForLoad() []envPair {
	return []envPair{
		{"TAPPA_ENV", "dev"},
		{"DATABASE_URL", "postgres://probe@127.0.0.1:1/probe"},
		{"TAPPA_RETENTION_YEARS", "2"},
	}
}

// TestObservability_EveryLoggerCallSiteIsSpelledLog closes the pinning gap
// backlog T51 measured and named.
//
// 🔴 WHAT T51 FOUND: redline-check.sh's R7/R7b/R7c only see a log call whose
// RECEIVER is spelled log, slog or fmt. Seven ordinary spellings escape it —
// a.logger.Error, `lg := a.log; lg.Error`, a method value, a slice or map
// element, an embedded field, an interface variable. The nets were correct on
// this tree BY ACCIDENT: every logger call site happened to be written x.log, and
// NOTHING HELD THAT. One `a.logger` would have switched three §4.7 rules off for
// a whole file, silently, with every scan still green.
//
// This does not fix the nets — the real answer is type-level redaction of the
// logger itself, which T51 measured at 224 call sites in internal/handler alone
// and is M8-04's problem. It makes the accident into an invariant: the moment
// somebody writes a differently-named logger, they are told, here, that they have
// just left the scanner's field of view.
func TestObservability_EveryLoggerCallSiteIsSpelledLog(t *testing.T) {
	t.Parallel()

	type offence struct {
		file, recv, method string
		line               int
	}
	var offences []offence
	total := 0

	logMethods := map[string]bool{
		"Info": true, "Warn": true, "Error": true, "Debug": true, "Log": true,
		"InfoContext": true, "WarnContext": true, "ErrorContext": true,
		"DebugContext": true, "LogAttrs": true,
	}

	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			rel, rerr := filepath.Rel(repoRoot, path)
			if rerr == nil && skipScanDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.HasSuffix(path, "_templ.go") || strings.Contains(path, "/internal/store/") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // not our business; the compiler owns syntax
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !logMethods[sel.Sel.Name] {
				return true
			}
			recv := receiverText(sel.X)
			// Only judge things that ARE loggers. A method called Log or Error on
			// something else (an error value's Error()) is not one.
			if !isLoggerReceiver(recv) {
				return true
			}
			total++
			if !receiverIsSpelledLog(recv) {
				rel, _ := filepath.Rel(repoRoot, path)
				offences = append(offences, offence{
					file: rel, line: fset.Position(call.Pos()).Line,
					recv: recv, method: sel.Sel.Name,
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	if total == 0 {
		t.Fatal("found no logger call sites at all — the scan is broken, not the tree")
	}
	t.Logf("logger call sites examined: %d", total)

	for _, o := range offences {
		t.Errorf("%s:%d — the logger is reached as %q, so %s(...) is INVISIBLE to R7/R7b/R7c "+
			"in scripts/redline-check.sh (their patterns require the receiver to read log./slog./fmt.). "+
			"Rename the receiver to `log`, or bind it first (`log := %s`). Backlog T51 has the measurement.",
			o.file, o.line, o.recv, o.method, o.recv)
	}
}

// receiverText renders a selector's receiver expression as source-ish text.
func receiverText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return receiverText(v.X) + "." + v.Sel.Name
	case *ast.CallExpr:
		return receiverText(v.Fun) + "()"
	case *ast.IndexExpr:
		return receiverText(v.X) + "[…]"
	case *ast.StarExpr:
		return "*" + receiverText(v.X)
	case *ast.ParenExpr:
		return "(" + receiverText(v.X) + ")"
	}
	return "?"
}

// isLoggerReceiver decides whether an expression names something logger-shaped.
//
// ⚠️ IT IS A NAME HEURISTIC, AND THAT IS THE SAME DEPENDENCY THE RULES IT GUARDS
// HAVE. A logger held under a name with neither "log" nor "slog" in it — say
// `a.out` — is invisible to this test as well as to R7b, so this test does not
// claim to find such a thing. It claims something narrower and true: of the
// loggers that CAN be recognised, all of them are spelled the way the scanner
// needs. Closing the wider hole is the type-level change (T51, M8-04).
func isLoggerReceiver(recv string) bool {
	low := strings.ToLower(recv)
	return strings.Contains(low, "log")
}

// receiverIsSpelledLog reports whether R7/R7b/R7c can see through this receiver:
// the text immediately before the method call must be `log`, `slog` or `fmt`.
func receiverIsSpelledLog(recv string) bool {
	tail := recv
	if i := strings.LastIndex(recv, "."); i >= 0 {
		tail = recv[i+1:]
	}
	switch tail {
	case "log", "slog", "fmt":
		return true
	}
	return false
}

// TestObservability_ProductionConfigSelectsTheMachineReadableFormat.
//
// 🔴 THE FAILURE THIS PINS IS TOTAL AND SILENT. Every alert rule in
// deploy/README.md filters on a FIELD (verdict, status, ctr_gap). The collector
// that reads this deployment's logs — measured 2026-08-19, the signoz
// k8s-infra-otel-agent DaemonSet, whose exclude list does not name this namespace —
// runs exactly one filelog operator, `container`, and no logfmt parser. So with
// TAPPA_LOG_FORMAT=text the body arrives as one opaque string, all six rules match
// zero rows, and the dashboard reads "no rejects, no security attempts, no 5xx".
// That is indistinguishable from a healthy system and nothing anywhere reports it.
//
// Flipping one word in a ConfigMap turns off the whole of M8-03's criterion 3.
// This is the only thing in the tree that notices.
func TestObservability_ProductionConfigSelectsTheMachineReadableFormat(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRoot, "deploy", "k8s", "05-config.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// Ignore comment lines: this file explains the decision at length and the word
	// "text" appears in that prose.
	var setting string
	for _, ln := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, "TAPPA_LOG_FORMAT:") {
			continue
		}
		setting = strings.TrimSpace(strings.TrimPrefix(trimmed, "TAPPA_LOG_FORMAT:"))
	}
	if setting == "" {
		t.Fatalf("deploy/k8s/05-config.yaml sets no TAPPA_LOG_FORMAT, so the deployment falls back "+
			"to %q and every alert rule in deploy/README.md matches nothing", config.LogFormatText)
	}
	if got := strings.Trim(setting, `"'`); got != config.LogFormatJSON {
		t.Fatalf("TAPPA_LOG_FORMAT = %q in the production ConfigMap, want %q. The collector reading "+
			"this namespace has no logfmt parser, so anything else silently disables all six M8-03 "+
			"alert signals while looking healthy.", got, config.LogFormatJSON)
	}
}

// alertSectionEnd is the heading that closes the alert-rules section. The scan
// below is bounded by these two markers rather than reading the whole file, and
// that boundary is a DECISION with a measurement behind it — see the counted limit
// in TestObservability_EverySidInTheAlertRulesExists.
const alertSectionEnd = "### §4.7"

// alertRulesSection returns the slice of the runbook the alert rules live in.
func alertRulesSection(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(runbookPath)
	if err != nil {
		t.Fatalf("read %s: %v", runbookPath, err)
	}
	doc := string(b)
	start := strings.Index(doc, alertSignalMarker)
	if start < 0 {
		t.Fatalf("deploy/README.md no longer contains %q", alertSignalMarker)
	}
	end := strings.Index(doc[start:], alertSectionEnd)
	if end < 0 {
		t.Fatalf("deploy/README.md no longer contains %q after the alert rules; the scan below "+
			"cannot know where the section stops", alertSectionEnd)
	}
	return doc[start : start+end]
}

// sidPattern matches a policy sid as this repository spells them.
var sidPattern = regexp.MustCompile(`\b(sys|base|t):[a-z0-9-]+`)

// TestObservability_EverySidInTheAlertRulesExists.
//
// 🔴 THE DEFECT THIS EXISTS FOR SHIPPED, IN THE ALERT TABLE, IN THE ROUND THAT
// WROTE THE TABLE. Rule 1's "first place to look" column told the operator to
// filter on matched_sid = "sys:tag-lost". That sid does not exist and never has:
// the guardrail is sys:tag-not-active (internal/policy/guardrails.go), renamed when
// migration 00013 added a fourth tag status. Pasting the suggested filter returns
// ZERO ROWS — which is the exact failure the top of that section warns about in its
// own words, "a renamed field does not break a query, it makes it match nothing".
//
// 🔴 SO FIXING THE STRING WAS NOT THE FIX. TestObservability_AlertSignalNames pins
// every literal in its `want` map and not one of them is a sid, because sids are
// DATA (a column value) rather than field names, and nothing was checking data. This
// walks every sid the section names and requires it to be a string literal in
// internal/policy.
//
// ⚠️ THE SENTENCE ABOVE USED TO SAY "twelve literals" AND THE MAP HELD FOURTEEN. A
// count of something in the same file is a live number, and this repository has
// measured the same rot four times now: it belongs in an assertion that recomputes
// it, never in prose beside it. So the count is gone rather than corrected.
//
// ⚠️ COUNTED LIMIT — SCOPE IS THE ALERT SECTION, NOT THE FILE. deploy/README.md's
// own limit 23 records that this document's citations are not mechanically checked,
// and it PROVES that by keeping a deliberately fabricated policy name
// (base:qr-requires-gps) in its text as evidence. Scanning the whole file would
// have to exempt that line, and an exemption list is the thing that rots. The
// section boundary is the honest scope: it is where a paste-able operator filter
// lives, which is where a wrong sid costs somebody an outage.
//
// ⚠️ SECOND LIMIT: this proves the sid EXISTS, not that it is the RIGHT one for the
// rule. sys:sun-invalid under rule 2 would pass. A wrong-but-real sid is a review
// problem; a non-existent one is a silent one, and only the second is closed here.
func TestObservability_EverySidInTheAlertRulesExists(t *testing.T) {
	t.Parallel()

	declared := declaredSids(t)
	if len(declared) < 10 {
		t.Fatalf("only %d sids found in internal/policy; the scan is broken, not the tree", len(declared))
	}

	section := alertRulesSection(t)
	found := sidPattern.FindAllString(section, -1)
	if len(found) == 0 {
		t.Fatal("the alert rules section names no sid at all — rule 1, 2 and 4 all point at " +
			"matched_sid, so this scan is looking at the wrong slice of the document")
	}

	seen := map[string]bool{}
	for _, sid := range found {
		if seen[sid] {
			continue
		}
		seen[sid] = true
		if declared[sid] {
			continue
		}
		t.Errorf("deploy/README.md's alert rules name the policy sid %q, and NOTHING in "+
			"internal/policy declares it. An operator pasting that filter gets zero rows and reads "+
			"it as a healthy system. Declared sids: %v", sid, sortedKeys(declared))
	}
	t.Logf("sids named in the alert rules: %d distinct, all resolved", len(seen))
}

// declaredSids collects every sid-shaped STRING LITERAL in internal/policy.
//
// Literals rather than a grep, on purpose: a grep would also accept a sid that only
// appears in a COMMENT, and a comment naming a policy that was deleted is exactly
// the kind of stale text this test is here to refuse.
func declaredSids(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	dir := filepath.Join(repoRoot, "internal", "policy")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", e.Name(), perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			// The whole literal must BE a sid. The length guard is not decoration:
			// FindString("") returns "", so without it every empty string literal in
			// the package joins the set and the assertion below accepts "".
			if v != "" && sidPattern.FindString(v) == v {
				out[v] = true
			}
			return true
		})
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestObservability_TheShippedLogLevelAdmitsTheSignals.
//
// 🔴 TWO OF THE FIVE SIGNALS ARE SILENTLY LEVEL-DEPENDENT AND NOTHING SAID SO.
// internal/domain/checkin.decisionLevel puts `ok` and `ignored` verdicts at INFO
// and only `reject`/`flag` at WARN. Signal 1 is a RATIO whose denominator is the
// total number of decisions, so at TAPPA_LOG_LEVEL=warn the denominator collapses
// to the rejects alone and the rate reads 100% for ever. Signal 4 keys on ctr_gap,
// which rides mostly on `ok` taps, so it disappears outright.
//
// TAPPA_LOG_FORMAT already has a closed-set test for the same class of failure
// (a ConfigMap word turning off criterion 3 while looking healthy). The level had
// none, and it is a FALL-BACK rather than a closed set — parseLevel accepts a typo
// and quietly means info — so the only thing that can catch a deliberate `warn` is
// a test that reads the shipped value.
func TestObservability_TheShippedLogLevelAdmitsTheSignals(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "05-config.yaml"))
	if err != nil {
		t.Fatalf("read 05-config.yaml: %v", err)
	}
	m := regexp.MustCompile(`(?m)^\s+TAPPA_LOG_LEVEL:\s*"?([a-zA-Z]+)"?`).FindStringSubmatch(string(b))
	if m == nil {
		t.Fatal("05-config.yaml sets no TAPPA_LOG_LEVEL, so this test cannot know what ships")
	}
	shipped := parseLevel(m[1])
	t.Logf("shipped TAPPA_LOG_LEVEL=%q -> %v", m[1], shipped)

	if shipped > slog.LevelInfo {
		t.Fatalf("TAPPA_LOG_LEVEL=%q parses to %v, which is above INFO. The `ok` and `ignored` tap "+
			"decisions are INFO records, so alert signal 1 loses its denominator (the reject rate "+
			"then reads 100%% for ever) and signal 4 (ctr_gap) vanishes entirely. Neither failure "+
			"is visible on a dashboard — both look like a quiet, healthy system.", m[1], shipped)
	}

	// Positive control through the real handler builder, so this cannot pass on the
	// strength of the constant alone.
	var buf bytes.Buffer
	slog.New(logHandler(&buf, &config.Config{LogFormat: config.LogFormatJSON, LogLevel: m[1]})).
		Info(checkin.EventTapDecision, checkin.LogVerdict, "ok", checkin.LogCtrGap, 0)
	if !strings.Contains(buf.String(), checkin.EventTapDecision) {
		t.Fatalf("an INFO %s record did not survive the shipped level: %q",
			checkin.EventTapDecision, buf.String())
	}
}

// limitsHeading opens the runbook's numbered list of accepted limits.
const limitsHeading = "## Kabul edilmiş sınırlar"

// limitItem matches the ordinal that opens one item of that list. It is anchored
// at column zero because every continuation line in the list is indented.
var limitItem = regexp.MustCompile(`(?m)^(\d+)\. \*\*`)

// TestRunbook_AcceptedLimitsAreNumberedOnce.
//
// 🔴 THE DEFECT THIS EXISTS FOR SHIPPED, AND IT LANDED ON THE ONE ITEM THAT SAYS
// CITATIONS ARE NOT CHECKED. The list had run to 23; a round appended three more
// and numbered them 17, 18, 19 — numbers already taken. The cost was not
// cosmetic and it was measured: docs/plan/m8-deploy-pilot.md cites "limit 19" in
// two places meaning two DIFFERENT items, so an operator following either citation
// arrives somewhere the sentence does not describe.
//
// Keeping the numbering unique is the cheapest possible guard: nothing here reads
// the CONTENT of an item, only that the ordinals are 1..N exactly once each, which
// is a property a wrong append always violates and a correct one never does.
//
// ⚠️ COUNTED LIMIT: this cannot tell whether a CITATION points at the right item.
// deploy/README.md's own limit about unverified citations still stands; this closes
// the narrower thing that made those citations ambiguous rather than merely
// unverified.
func TestRunbook_AcceptedLimitsAreNumberedOnce(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile(runbookPath)
	if err != nil {
		t.Fatalf("read %s: %v", runbookPath, err)
	}
	doc := string(b)
	start := strings.Index(doc, limitsHeading)
	if start < 0 {
		t.Fatalf("deploy/README.md no longer has a %q section", limitsHeading)
	}

	matches := limitItem.FindAllStringSubmatch(doc[start:], -1)
	if len(matches) < 10 {
		t.Fatalf("found only %d numbered limits; the scan is broken, not the document", len(matches))
	}

	seen := map[int]int{}
	var order []int
	for _, m := range matches {
		n, cerr := strconv.Atoi(m[1])
		if cerr != nil {
			t.Fatalf("unparsable ordinal %q", m[1])
		}
		seen[n]++
		order = append(order, n)
	}
	for n, count := range seen {
		if count > 1 {
			t.Errorf("limit %d appears %d times. A citation that says \"limit %d\" now names two "+
				"different things, and docs/plan/m8-deploy-pilot.md already contains two such "+
				"citations pointing at different items. Full order: %v", n, count, n, order)
		}
	}
	for i, n := range order {
		if n != i+1 {
			t.Errorf("the %d. limit is numbered %d; the list must read 1..%d in order so a "+
				"citation resolves to exactly one item. Full order: %v", i+1, n, len(order), order)
			break
		}
	}
}
