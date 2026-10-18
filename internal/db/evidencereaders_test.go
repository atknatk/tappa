package db

// evidencereaders_test.go -- M11 API-1c, the second review's ORTA-A (K-39): every statement of
// db/queries that reads transactions' evidence or decision columns is classified -- does it keep a
// row taken at a holding PEER's venue from describing the peer's network and coordinate, and how.
// WHAT IT CATCHES, AND WHAT IT DOES NOT (the third review's D-3, stated rather than implied): a
// statement in db/queries that names `transactions` and one of evidenceColumnRE's columns or a
// whole-row read -- `*` in any SELECT form (ALL, DISTINCT, DISTINCT ON), `alias.*`, `RETURNING *`,
// sqlc.embed, or the table's bare name or alias (`SELECT t`, `to_jsonb(t)`, `row_to_json(t)`;
// readsWholeRow, the fourth review's D-e) -- is red until somebody files it. It does not see SQL outside db/queries (no Go
// file writes SQL by hand today; the holding_* functions live in migrations and are proven by
// their own catalog tests), a reader that reaches the table under another name (a view), or
// whether a counted filter sits on the RIGHT figure -- `must` is a fragment counted, not parsed;
// the one-filter-dropped control and the anomaly tests' SQL mutants carry that half.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// evidenceReaderClass is how one statement treats a row at a peer's venue.
type evidenceReaderClass struct {
	// how: "sql-filter" (the evidence figures carry site_tenant_id IS NULL / the partner fold),
	// "excluded" (the statement's own predicate leaves site rows out), "go-blank" (the rows are
	// returned with their site ids and internal/domain/ledger blanks the evidence -- site.go),
	// "not-rendered" (a decision input or a writer, never on a screen), "residue" (named in ADR
	// 0027 §13 item 5).
	how string
	// must is a fragment the statement must carry for the class to hold ("" for none), and
	// times how often, exactly (0: at least once) -- for a filtered statement it is the number of
	// figures that carry the filter, so dropping it from ONE of them is red (the third review's D-3:
	// "somewhere, once" let a statement lose all but one filter and stay green).
	must  string
	times int
	why   string
}

// evidenceReaders is the inventory (2026-10-14).
var evidenceReaders = map[string]evidenceReaderClass{
	"CountAnomalySignals": {"sql-filter", "site_tenant_id IS NULL", 4,
		"judged, distance_only, outside_venue, outside_venue_on_network own venues only; partner rows counted apart (site_tenant_id IS NOT NULL, twice)"},
	"ListAnomalyPeople": {"sql-filter", "site_tenant_id IS NULL", 6,
		"distance_only and outside_venue in the select, the HAVING and the ORDER, own venues only"},
	"ListAnomalyVenues": {"sql-filter", "site_tenant_id IS NULL", 3,
		"the two evidence figures and the ORDER own venues only; the partner venue is its own row"},
	"ListAnomalyPolicySids": {"excluded", "site_tenant_id IS NULL", 1,
		"partner rows are not in the breakdown; ledger appends one unnamed bucket, last, from the totals"},
	"ListAnomalyPlaques": {"excluded", "tag_uid IS NOT NULL", 0,
		"a site row's tag_uid is NULL (K-30): the peer's plaque is never the employer's anomaly"},
	"ListPanelTransactions": {"go-blank", "site_location_id", 0,
		"ledger.nameSiteRecords blanks ip/gps/trust and replaces an evidence rule's note (matched_sid read for that)"},
	"ListFlaggedForReview": {"go-blank", "site_location_id", 0, "the same mapping as the day list (review queue)"},
	"ListWorkedShiftEvents": {"residue", "", 0,
		"verdict decides the hours state only; it is not printed as evidence (§13 item 5: the verdict residue)"},
	"CountPendingFlagged":           {"residue", "", 0, "a count of flags to decide; a flag's existence is the verdict residue (§13 item 5)"},
	"GetLastOpenTransaction":        {"not-rendered", "", 0, "the tap decision's input (direction chain), never shown"},
	"GetLastTransactionForEmployee": {"not-rendered", "", 0, "the tap decision's input (debounce), never shown"},
	"InsertTransaction":             {"not-rendered", "", 0, "the writer"},
	"InsertManualTransaction":       {"not-rendered", "", 0, "the writer (manual)"},
	"InsertManualSiteTransaction":   {"not-rendered", "", 0, "the writer (manual at a peer's venue)"},
	"InsertTransactionReview":       {"not-rendered", "", 0, "the review writer reads the decided row's verdict to refuse a non-flag"},
	"GetTapOutcome": {"go-blank", "site_location_id", 0,
		"the sandbox simulation's answer (M11 API-11): internal/sandbox's tapBodyOf blanks trust and the rule of a row at a partner venue; the verdict is the residue"},
}

// evidenceColumnRE names the columns a K-39 leak would be computed from.
// The evidence itself (source_ip, gps_lat, gps_lng -- and gps_distance_m should a column of that
// name ever exist; today the distance lives in policy_context), the decision built on it, the chip
// facts (sun_valid, ctr), and a read of the whole row (SELECT *, alias.*, RETURNING *, sqlc.embed),
// which carries all of them under no column name (the third review's D-3).
var evidenceColumnRE = regexp.MustCompile(`(?i)\b(ip_match|gps_match|gps_distance_m|gps_lat|gps_lng|source_ip|sun_valid|ctr|trust|matched_sid|note|policy_context|policy_layer|verdict)\b` +
	`|\bSELECT\s+(?:ALL\s+|DISTINCT\s+(?:ON\s*\([^)]*\)\s*)?)?\*|\b\w+\.\*|\bRETURNING\s+\*|\bsqlc\.embed\s*\(`)

// transactionsAliasRE finds the names a statement gives transactions (the table itself and its
// alias), so a read of the BARE name -- `SELECT t`, `to_jsonb(t)`, `row_to_json(t)`, a row
// constructor -- is caught as the whole-row read it is (the fourth review's D-e).
var transactionsAliasRE = regexp.MustCompile(`(?i)\btransactions\b(?:\s+(?:AS\s+)?(\w+))?`)

// sqlWords are the words that can follow a table name and are not an alias.
var sqlWords = map[string]bool{"where": true, "join": true, "left": true, "right": true, "inner": true, "full": true,
	"cross": true, "on": true, "using": true, "group": true, "order": true, "limit": true, "set": true, "values": true,
	"returning": true, "union": true, "except": true, "intersect": true, "for": true, "having": true, "window": true,
	"natural": true, "lateral": true, "default": true, "select": true}

// readsWholeRow: some name of transactions in stmt is used bare -- not followed by a dot, and not
// where the statement names the table or declares the alias.
func readsWholeRow(stmt string) bool {
	decl := transactionsAliasRE.FindAllStringSubmatchIndex(stmt, -1)
	names := map[string]bool{"transactions": true}
	declared := map[int]bool{}
	for _, m := range decl {
		declared[m[0]] = true
		if m[2] >= 0 && !sqlWords[strings.ToLower(stmt[m[2]:m[3]])] {
			names[strings.ToLower(stmt[m[2]:m[3]])] = true
			declared[m[2]] = true
		}
	}
	for name := range names {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
		for _, at := range re.FindAllStringIndex(stmt, -1) {
			if declared[at[0]] || (at[1] < len(stmt) && stmt[at[1]] == '.') {
				continue
			}
			return true
		}
	}
	return false
}

// TestEvidenceReaders_EveryOneIsClassified: the statements of db/queries that name transactions and
// one of evidenceColumnRE's columns are exactly evidenceReaders' keys, and each carries the
// fragment its class rests on. CONTROL: a statement added to a copy of the scan input is caught.
func TestEvidenceReaders_EveryOneIsClassified(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "db", "queries", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("read db/queries: %v", err)
	}
	var src strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
		src.WriteString("\n")
	}
	check := func(all string) []string {
		got := evidenceCandidates(all)
		var out []string
		for name, stmt := range got {
			c, ok := evidenceReaders[name]
			if !ok {
				out = append(out, name+": reads transactions' evidence or decision columns and is not in evidenceReaders -- "+
					"decide what a row at a partner venue does there (K-39) and file it")
				continue
			}
			switch n := strings.Count(stmt, c.must); {
			case c.must == "":
			case c.times == 0 && n == 0:
				out = append(out, name+": filed as "+c.how+" but does not carry "+c.must)
			case c.times > 0 && n != c.times:
				out = append(out, fmt.Sprintf("%s: filed as %s with %q %d times, the statement carries it %d times", name, c.how, c.must, c.times, n))
			}
		}
		for name := range evidenceReaders {
			if _, ok := got[name]; !ok {
				out = append(out, name+": in evidenceReaders but the scan no longer finds it")
			}
		}
		sort.Strings(out)
		return out
	}
	for _, f := range check(src.String()) {
		t.Error(f)
	}
	if len(evidenceReaders) < 15 {
		t.Fatalf("anti-vacuity: %d readers", len(evidenceReaders))
	}
	for _, probe := range []string{"t.ip_match", "t.source_ip", "t.gps_lat", "t.sun_valid", "t.ctr", "t.*", "*", "sqlc.embed(t)",
		"t", "to_jsonb(t)", "row_to_json(t)", "ALL *", "DISTINCT ON (t.id) *", "t.id, t"} {
		control := src.String() + "\n-- name: ZZProbe :many\nSELECT " + probe + " FROM transactions t WHERE t.tenant_id = @tenant_id;\n"
		if got := check(control); len(got) != 1 || !strings.HasPrefix(got[0], "ZZProbe") {
			t.Errorf("CONTROL: a new reader of %s was not caught: %v", probe, got)
		}
	}
	dropped := strings.Replace(src.String(), "count(*) FILTER (WHERE t.gps_match AND NOT t.ip_match AND t.site_tenant_id IS NULL) AS distance_only",
		"count(*) FILTER (WHERE t.gps_match AND NOT t.ip_match) AS distance_only", 1)
	if dropped == src.String() || len(check(dropped)) == 0 {
		t.Error("CONTROL: one filter taken out of one figure was not caught")
	}
}

// evidenceCandidates splits the queries into named statements (comments dropped) and keeps those
// that name transactions and an evidence column.
func evidenceCandidates(src string) map[string]string {
	nameRE := regexp.MustCompile(`(?m)^-- name: (\w+)`)
	out := map[string]string{}
	idx := nameRE.FindAllStringSubmatchIndex(src, -1)
	for i, m := range idx {
		end := len(src)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		var body strings.Builder
		for _, line := range strings.Split(src[m[1]:end], "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				body.WriteString(line + "\n")
			}
		}
		stmt := body.String()
		if regexp.MustCompile(`(?i)\btransactions\b`).MatchString(stmt) && (evidenceColumnRE.MatchString(stmt) || readsWholeRow(stmt)) {
			out[src[m[2]:m[3]]] = stmt
		}
	}
	return out
}
