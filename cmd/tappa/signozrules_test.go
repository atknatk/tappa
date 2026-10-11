package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/httpx"
)

// signozrules_test.go — T118 / Q28 (a). The seven M8-03 alert rules are installed in
// the cluster's SigNoz as ClickHouse-SQL threshold rules, and
// deploy/observability/signoz-alert-rules.json is their source for the next install.
//
// 🔴 THE FAILURE THESE EXIST FOR IS THE SAME ONE observability_test.go NAMES: A RULE
// KEYED ON A RENAMED FIELD DOES NOT BREAK, IT MATCHES NOTHING. The runbook's
// paste-able block was already pinned to the code; the INSTALLED rule is a second
// spelling of the same filter, in SQL, and nothing held it. A one-sided rename of
// tap.decision would have left SigNoz evaluating a query that returns zero rows for
// ever — "inactive", which is also what a healthy system looks like.
//
// The file is held to three things, each a separate test so a failure names its own
// cause: the code's constants (the names), the runbook's table (one rule per row,
// the same filter in both), and the delivery chain (scoped to this container, no
// address in the tree, no word the healthchecks.io keyword filter reads).

// signozRulesDir holds the SigNoz export. The leak scan reads every file in it, so a
// second export dropped beside the first is covered without editing this test.
var signozRulesDir = filepath.Join(repoRoot, "deploy", "observability")

// signozRulesPath is the file the runbook's reinstall step posts, rule by rule.
var signozRulesPath = filepath.Join(signozRulesDir, "signoz-alert-rules.json")

// signozRulesRel is how the runbook names that file; the alert section must carry it.
const signozRulesRel = "deploy/observability/signoz-alert-rules.json"

// signozChannel is the SigNoz notification channel every rule names. The channel is
// referenced by NAME only: its webhook address is the tappa-signals check's ping URL,
// which is a secret (anyone holding it can send "resolved" and silence an alert).
const signozChannel = "tappa-healthchecks"

// tappaWorkload is both the namespace and the container the rules must be scoped to.
const tappaWorkload = "tappa"

type signozRule struct {
	Alert     string `json:"alert"`
	AlertType string `json:"alertType"`
	RuleType  string `json:"ruleType"`
	Version   string `json:"version"`
	Disabled  bool   `json:"disabled"`
	Condition struct {
		CompositeQuery struct {
			QueryType string `json:"queryType"`
			Queries   []struct {
				Type string `json:"type"`
				Spec struct {
					Name     string `json:"name"`
					Query    string `json:"query"`
					Disabled bool   `json:"disabled"`
				} `json:"spec"`
			} `json:"queries"`
		} `json:"compositeQuery"`
		SelectedQueryName string `json:"selectedQueryName"`
	} `json:"condition"`
	Labels            map[string]string `json:"labels"`
	Annotations       map[string]string `json:"annotations"`
	PreferredChannels []string          `json:"preferredChannels"`
}

// number is the rule's position in the runbook table, read from its name.
func (r signozRule) number() int {
	m := ruleNamePattern.FindStringSubmatch(r.Alert)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// query is the rule's single ClickHouse statement ("" when the shape is wrong; the
// shape test reports why).
func (r signozRule) query() string {
	q := r.Condition.CompositeQuery.Queries
	if len(q) != 1 {
		return ""
	}
	return q[0].Spec.Query
}

var (
	// ruleNamePattern: every rule is called "tappa <row>: <what>", so the row it
	// implements is readable in SigNoz's list and in the notification.
	ruleNamePattern = regexp.MustCompile(`^tappa ([0-9]+): \S`)
	// anyExtract is every JSON extraction in a query; bodyExtract is the subset this
	// test understands. A difference means a shape nobody taught it to read.
	anyExtract  = regexp.MustCompile(`JSONExtract[A-Za-z0-9]*\(`)
	bodyExtract = regexp.MustCompile(`JSONExtract(String|Int)\(\s*body\s*,\s*'([^']*)'\s*\)`)
	bodyStrEq   = regexp.MustCompile(`JSONExtractString\(\s*body\s*,\s*'([^']*)'\s*\)\s*=\s*'([^']*)'`)
	bodyIntCmp  = regexp.MustCompile(`JSONExtractInt\(\s*body\s*,\s*'([^']*)'\s*\)\s*(>=|<=|!=|>|<|=)\s*(-?[0-9]+)`)
	nsFilter    = regexp.MustCompile(`resources_string\['k8s\.namespace\.name'\]\s*=\s*'([^']*)'`)
	ctFilter    = regexp.MustCompile(`resources_string\['k8s\.container\.name'\]\s*=\s*'([^']*)'`)
	// ruleRow is one row of the runbook's rules table: "| <n> | ...".
	ruleRow = regexp.MustCompile(`(?m)^\| ([0-9]+) \| (.+)$`)
	// ruleCitation is how a description points back at its row.
	ruleCitation = regexp.MustCompile(`M8-03 rule ([0-9]+)\b`)
)

func readSignozRules(t *testing.T) []signozRule {
	t.Helper()
	b, err := os.ReadFile(signozRulesPath)
	if err != nil {
		t.Fatalf("read %s: %v", signozRulesPath, err)
	}
	var rules []signozRule
	if err := json.Unmarshal(b, &rules); err != nil {
		t.Fatalf("%s is not a JSON array of rules: %v", signozRulesRel, err)
	}
	return rules
}

// strEq is one `JSONExtractString(body,'field') = 'value'` filter.
type strEq struct{ field, value string }

// ruleTableHeader opens the rules table inside the alert section.
const ruleTableHeader = "| # | Sinyal |"

// runbookRuleRows returns the rules table of the alert section, row number -> row text.
// Only the table that opens with ruleTableHeader is read, up to its first non-table
// line: other tables in the section are not rules.
func runbookRuleRows(t *testing.T) map[int]string {
	t.Helper()
	section := alertRulesSection(t)
	i := strings.Index(section, ruleTableHeader)
	if i < 0 {
		t.Fatalf("the runbook's alert section has no rules table (looked for %q)", ruleTableHeader)
	}
	var table []string
	for _, ln := range strings.Split(section[i:], "\n") {
		if !strings.HasPrefix(ln, "|") {
			break
		}
		table = append(table, ln)
	}
	rows := map[int]string{}
	for _, m := range ruleRow.FindAllStringSubmatch(strings.Join(table, "\n"), -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("unparsable row number %q", m[1])
		}
		if _, dup := rows[n]; dup {
			t.Errorf("the runbook's rules table has row %d twice", n)
		}
		rows[n] = m[2]
	}
	return rows
}

// TestSignozRules_OneRuleForEachRunbookRow: the export holds exactly the rules the
// runbook's table lists, one per row, in order, and each rule's filters are the ones
// that row publishes.
//
// 🔴 THE ROW CHECK IS WHAT MAKES "SEVEN" MEAN SOMETHING. A count alone passes a
// file whose rule 4 filters on a different threshold than the table tells the
// operator, or whose rule 2 forgot its verdict filter. So every equality and every
// numeric comparison the SQL makes on the log body must appear, as the table writes
// it, in that rule's own row — and every body field it reads must be named there.
//
// ⚠️ COUNTED LIMIT: the direction is rule -> row. A row may say MORE than its rule
// (rule 1's "at least 5", rule 5's "1% of requests"): those are the documented
// simplifications in the section's delivery box, and the test does not pretend the
// SQL encodes them.
func TestSignozRules_OneRuleForEachRunbookRow(t *testing.T) {
	t.Parallel()

	rules := readSignozRules(t)
	rows := runbookRuleRows(t)
	if len(rows) < 7 {
		t.Fatalf("found only %d rows in the runbook's rules table; the scan is broken, not the table", len(rows))
	}
	if len(rules) != len(rows) {
		t.Errorf("%s holds %d rules but the runbook's table has %d rows. A row with no installed "+
			"rule is an alert nobody receives; a rule with no row is one nobody can look up.",
			signozRulesRel, len(rules), len(rows))
	}
	for i, r := range rules {
		n := r.number()
		if n != i+1 {
			t.Errorf("rule %d is named %q; want the name to start %q so position, name and table row agree",
				i+1, r.Alert, fmt.Sprintf("tappa %d: ", i+1))
			continue
		}
		if r.Annotations["summary"] != r.Alert {
			t.Errorf("rule %d: summary %q differs from the rule name %q", n, r.Annotations["summary"], r.Alert)
		}
		cites := ruleCitation.FindAllStringSubmatch(r.Annotations["description"], -1)
		if len(cites) == 0 {
			t.Errorf("rule %d: the description does not point at its runbook row (want %q)", n,
				fmt.Sprintf("M8-03 rule %d", n))
		}
		for _, c := range cites {
			if c[1] != strconv.Itoa(n) {
				t.Errorf("rule %d: the description points at M8-03 rule %s", n, c[1])
			}
		}

		row, ok := rows[n]
		if !ok {
			t.Errorf("rule %d (%q) has no row %d in the runbook's rules table", n, r.Alert, n)
			continue
		}
		q := r.query()
		for _, m := range bodyStrEq.FindAllStringSubmatch(q, -1) {
			field, value := m[1], m[2]
			want := "`" + field + ` = "` + value + "\"`"
			if field == slog.MessageKey {
				// The table's event column carries the msg value on its own.
				want = "`" + value + "`"
			}
			if !strings.Contains(row, want) {
				t.Errorf("rule %d filters on %s = '%s', and its runbook row does not say %s:\n%s",
					n, field, value, want, row)
			}
		}
		for _, m := range bodyIntCmp.FindAllStringSubmatch(q, -1) {
			want := "`" + m[1] + " " + m[2] + " " + m[3] + "`"
			if !strings.Contains(row, want) {
				t.Errorf("rule %d compares %s %s %s, and its runbook row does not say %s:\n%s",
					n, m[1], m[2], m[3], want, row)
			}
		}
		for _, m := range bodyExtract.FindAllStringSubmatch(q, -1) {
			if m[2] == slog.MessageKey {
				continue
			}
			if !mentionsWholeName(row, m[2]) {
				t.Errorf("rule %d reads the body field %q, which its runbook row never names:\n%s", n, m[2], row)
			}
		}
	}

	// The section must say where the installed rules live and which channel they use,
	// or the next install starts from the paste-able block and a guess.
	section := alertRulesSection(t)
	for _, must := range []string{signozRulesRel, "`" + signozChannel + "`"} {
		if !strings.Contains(section, must) {
			t.Errorf("the runbook's alert section does not mention %s", must)
		}
	}
}

// TestSignozRules_FilterOnThePinnedNames: every body field and every msg value the
// installed rules filter on is a constant TestObservability_AlertSignalNames already
// pins to its literal, and each rule filters on the event its row is about.
//
// 🔴 NO NEW LIST. The names come from alertSignalConstants(), the map that test
// compares against hand-written literals, plus slog's own msg/level keys. A field the
// pin does not hold cannot be filtered on here without first being pinned there.
func TestSignozRules_FilterOnThePinnedNames(t *testing.T) {
	t.Parallel()

	pinned := map[string]bool{}
	for _, v := range alertSignalConstants() {
		pinned[v] = true
	}

	// Per rule: the exact body equalities and the exact set of body fields read.
	type expect struct {
		eqs    []strEq
		fields []string
	}
	msg := slog.MessageKey
	want := map[int]expect{
		1: {
			eqs:    []strEq{{msg, checkin.EventTapDecision}, {checkin.LogVerdict, string(tap.VerdictReject)}},
			fields: []string{msg, checkin.LogVerdict},
		},
		2: {
			eqs:    []strEq{{msg, checkin.EventTapDecision}, {checkin.LogVerdict, string(tap.VerdictFlag)}},
			fields: []string{msg, checkin.LogVerdict, checkin.LogMatchedSid},
		},
		3: {
			eqs:    []strEq{{msg, checkin.EventTapSecurityAlert}},
			fields: []string{msg},
		},
		4: {
			eqs:    []strEq{{msg, checkin.EventTapDecision}},
			fields: []string{msg, checkin.LogCtrGap},
		},
		5: {
			// Rule 5 reads the record's level, not its status: the access log writes
			// a 5xx at ERROR, and the level is slog's own key.
			eqs:    []strEq{{msg, httpx.EventHTTPRequest}, {slog.LevelKey, slog.LevelError.String()}},
			fields: []string{msg, slog.LevelKey},
		},
		6: {
			eqs:    []strEq{{msg, handler.EventReadinessLost}},
			fields: []string{msg},
		},
		7: {
			// Rule 7 has no event: the start-up line's msg is a sentence, the filter is
			// the attribute (cmd/tappa/operator.go).
			eqs:    []strEq{{operatorSurfaceKey, operatorSurfaceUnavailable}},
			fields: []string{operatorSurfaceKey},
		},
	}

	// The expectations above are themselves held to the pin, so this test cannot
	// drift into filtering on a name the pin does not cover.
	for n, e := range want {
		for _, f := range e.fields {
			if f != slog.MessageKey && f != slog.LevelKey && !pinned[f] {
				t.Fatalf("expectation for rule %d reads %q, which alertSignalConstants does not pin", n, f)
			}
		}
		for _, eq := range e.eqs {
			if eq.field == slog.MessageKey && !pinned[eq.value] {
				t.Fatalf("expectation for rule %d filters msg on %q, which alertSignalConstants does not pin", n, eq.value)
			}
		}
	}

	rules := readSignozRules(t)
	seen := map[int]bool{}
	for _, r := range rules {
		n := r.number()
		e, ok := want[n]
		if !ok {
			t.Errorf("rule %q has no expectation here; a new rule is added to the runbook table, the pin "+
				"and this map in the same change", r.Alert)
			continue
		}
		seen[n] = true
		q := r.query()

		if a, b := len(anyExtract.FindAllString(q, -1)), len(bodyExtract.FindAllString(q, -1)); a != b {
			t.Errorf("rule %d: %d JSON extractions, %d of them the body shape this test reads "+
				"(JSONExtractString/JSONExtractInt(body,'field')). Teach the test the new shape before "+
				"shipping it, or an unread field escapes the pin:\n%s", n, a, b, q)
		}

		var gotFields []string
		for _, m := range bodyExtract.FindAllStringSubmatch(q, -1) {
			gotFields = append(gotFields, m[2])
		}
		if g, w := uniqueSorted(gotFields), uniqueSorted(e.fields); g != w {
			t.Errorf("rule %d (%q) reads body fields %s, want %s. A field that is not the constant's "+
				"literal matches no record, and that reads as a quiet system.", n, r.Alert, g, w)
		}

		var gotEqs []string
		for _, m := range bodyStrEq.FindAllStringSubmatch(q, -1) {
			gotEqs = append(gotEqs, m[1]+"="+m[2])
		}
		var wantEqs []string
		for _, eq := range e.eqs {
			wantEqs = append(wantEqs, eq.field+"="+eq.value)
		}
		if g, w := uniqueSorted(gotEqs), uniqueSorted(wantEqs); g != w {
			t.Errorf("rule %d (%q) filters %s, want %s", n, r.Alert, g, w)
		}
	}
	for n := range want {
		if !seen[n] {
			t.Errorf("rule %d is missing from %s", n, signozRulesRel)
		}
	}
}

// TestSignozRules_EveryQueryIsScopedToTappa: each rule is a live, enabled ClickHouse
// query over SigNoz's log table, bounded by the evaluation window and scoped to this
// product's namespace and container.
//
// 🔴 THE NAMESPACE FILTER IS NOT DECORATION. The SigNoz in this cluster collects every
// namespace's logs; msg values like http.request are generic enough that another
// workload's 5xx would page Tappa's operator. And without the window variables the
// query scans the whole log table on every evaluation.
func TestSignozRules_EveryQueryIsScopedToTappa(t *testing.T) {
	t.Parallel()

	rules := readSignozRules(t)
	if len(rules) == 0 {
		t.Fatal("no rules read; the scan is broken, not the file")
	}
	for _, r := range rules {
		name := r.Alert
		if r.Version != "v5" {
			t.Errorf("%q: version %q; this SigNoz (v0.131) refuses anything but v5 (measured: v4 refused)", name, r.Version)
		}
		if r.AlertType != "LOGS_BASED_ALERT" || r.RuleType != "threshold_rule" {
			t.Errorf("%q: alertType %q / ruleType %q, want LOGS_BASED_ALERT / threshold_rule", name, r.AlertType, r.RuleType)
		}
		if r.Disabled {
			t.Errorf("%q is disabled; a reinstall would bring it back silent", name)
		}
		cq := r.Condition.CompositeQuery
		if len(cq.Queries) != 1 {
			t.Errorf("%q has %d queries, want exactly 1", name, len(cq.Queries))
			continue
		}
		qq := cq.Queries[0]
		if qq.Type != "clickhouse_sql" || cq.QueryType != "clickhouse_sql" {
			t.Errorf("%q: query type %q / %q, want clickhouse_sql", name, qq.Type, cq.QueryType)
		}
		if qq.Spec.Disabled {
			t.Errorf("%q: its only query is disabled", name)
		}
		if qq.Spec.Name != r.Condition.SelectedQueryName {
			t.Errorf("%q: the condition selects %q but the query is %q", name, r.Condition.SelectedQueryName, qq.Spec.Name)
		}
		q := qq.Spec.Query
		for _, must := range []string{"signoz_logs.distributed_logs_v2", "{{.start_timestamp_nano}}", "{{.end_timestamp_nano}}"} {
			if !strings.Contains(q, must) {
				t.Errorf("%q: the query does not contain %s", name, must)
			}
		}
		for _, f := range []struct {
			label string
			re    *regexp.Regexp
		}{{"k8s.namespace.name", nsFilter}, {"k8s.container.name", ctFilter}} {
			ms := f.re.FindAllStringSubmatch(q, -1)
			if len(ms) == 0 {
				t.Errorf("%q: the query has no %s = '%s' filter, so it counts every workload's logs:\n%s",
					name, f.label, tappaWorkload, q)
			}
			for _, m := range ms {
				if m[1] != tappaWorkload {
					t.Errorf("%q: %s = '%s', want '%s'", name, f.label, m[1], tappaWorkload)
				}
			}
		}
	}
}

// TestSignozRules_CarryNoAddressAndNoDeliveryKeyword: the export is safe to commit
// and safe to deliver.
//
// 🔴 TWO DIFFERENT FAILURES, ONE SCAN. (1) The delivery address is the tappa-signals
// ping URL, a secret: whoever has it can post "resolved" and close any alert. The
// channel is named, never addressed, and no file here may carry an hc-ping address or
// any URL at all. (2) healthchecks.io reads the webhook BODY for its keywords —
// failure "firing", success "resolved" — and the body carries each rule's name and
// annotations. A description containing either word turns a notification into the
// opposite signal (or a firing alert's own text into a "down" that never clears), so
// neither word may appear anywhere in the file, in any case.
//
// Also: no server-assigned fields. An id, state or timestamp in the source would make
// the reinstall post a stale identity.
func TestSignozRules_CarryNoAddressAndNoDeliveryKeyword(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(signozRulesDir)
	if err != nil {
		t.Fatalf("read %s: %v", signozRulesDir, err)
	}
	files := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files++
		path := filepath.Join(signozRulesDir, e.Name())
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read %s: %v", path, rerr)
		}
		low := strings.ToLower(string(b))
		for _, bad := range []string{"hc-ping", "://", "firing", "resolved"} {
			if strings.Contains(low, bad) {
				t.Errorf("deploy/observability/%s contains %q. An address here is a secret in a public "+
					"repository; the words firing/resolved are the healthchecks.io keywords and would "+
					"invert the signal they travel in.", e.Name(), bad)
			}
		}
	}
	if files == 0 {
		t.Fatal("deploy/observability holds no file; the scan is broken, not the tree")
	}

	b, err := os.ReadFile(signozRulesPath)
	if err != nil {
		t.Fatalf("read %s: %v", signozRulesPath, err)
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("%s: %v", signozRulesRel, err)
	}
	serverOwned := []string{"id", "state", "createAt", "updateAt", "createBy", "updateBy"}
	for i, r := range raw {
		for _, k := range serverOwned {
			if _, ok := r[k]; ok {
				t.Errorf("rule %d carries the server-assigned field %q; strip it from the export", i+1, k)
			}
		}
	}
	for _, r := range readSignozRules(t) {
		if len(r.PreferredChannels) != 1 || r.PreferredChannels[0] != signozChannel {
			t.Errorf("%q delivers to %v, want exactly [%s] — the only channel that reaches the "+
				"tappa-signals check", r.Alert, r.PreferredChannels, signozChannel)
		}
	}
}

func uniqueSorted(xs []string) string {
	set := map[string]bool{}
	for _, x := range xs {
		set[x] = true
	}
	out := make([]string, 0, len(set))
	for x := range set {
		out = append(out, x)
	}
	sort.Strings(out)
	return "[" + strings.Join(out, " ") + "]"
}
