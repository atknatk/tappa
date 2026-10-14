package db

// evidencereaders_test.go -- M11 API-1c, the second review's ORTA-A (K-39): every statement of
// db/queries that reads transactions' evidence or decision columns is classified -- does it keep a
// row taken at a holding PEER's venue from describing the peer's network and coordinate, and how.
// A new reader is red until somebody files it.

import (
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
	// must is a fragment the statement must carry for the class to hold ("" for none).
	must string
	why  string
}

// evidenceReaders is the inventory (2026-10-14).
var evidenceReaders = map[string]evidenceReaderClass{
	"CountAnomalySignals":           {"sql-filter", "site_tenant_id IS NULL", "judged, distance_only, outside_venue(_on_network) own venues only; partner rows counted apart"},
	"ListAnomalyPeople":             {"sql-filter", "site_tenant_id IS NULL", "distance_only and outside_venue (select, HAVING, ORDER) own venues only"},
	"ListAnomalyVenues":             {"sql-filter", "site_tenant_id IS NULL", "the two evidence figures own venues only; the partner venue is its own row"},
	"ListAnomalyPolicySids":         {"sql-filter", "site_tenant_id IS NOT NULL", "a partner row is marked; ledger folds them into one unnamed bucket"},
	"ListAnomalyPlaques":            {"excluded", "tag_uid IS NOT NULL", "a site row's tag_uid is NULL (K-30): the peer's plaque is never the employer's anomaly"},
	"ListPanelTransactions":         {"go-blank", "site_location_id", "ledger.nameSiteRecords blanks ip/gps/trust and replaces an evidence rule's note"},
	"ListFlaggedForReview":          {"go-blank", "site_location_id", "the same mapping as the day list (review queue)"},
	"ListWorkedShiftEvents":         {"residue", "", "verdict decides the hours state only; it is not printed as evidence (§13 item 5: the verdict residue)"},
	"CountPendingFlagged":           {"residue", "", "a count of flags to decide; a flag's existence is the verdict residue (§13 item 5)"},
	"GetLastOpenTransaction":        {"not-rendered", "", "the tap decision's input (direction chain), never shown"},
	"GetLastTransactionForEmployee": {"not-rendered", "", "the tap decision's input (debounce), never shown"},
	"InsertTransaction":             {"not-rendered", "", "the writer"},
	"InsertManualTransaction":       {"not-rendered", "", "the writer (manual)"},
	"InsertManualSiteTransaction":   {"not-rendered", "", "the writer (manual at a peer's venue)"},
	"InsertTransactionReview":       {"not-rendered", "", "the review writer reads the decided row's verdict to refuse a non-flag"},
}

// evidenceColumnRE names the columns a K-39 leak would be computed from.
var evidenceColumnRE = regexp.MustCompile(`(?i)\b(ip_match|gps_match|gps_distance_m|trust|matched_sid|note|policy_context|policy_layer|verdict)\b`)

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
			if c.must != "" && !strings.Contains(stmt, c.must) {
				out = append(out, name+": filed as "+c.how+" but does not carry "+c.must)
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
	control := src.String() + "\n-- name: ZZProbe :many\nSELECT t.ip_match FROM transactions t WHERE t.tenant_id = @tenant_id;\n"
	if got := check(control); len(got) != 1 || !strings.HasPrefix(got[0], "ZZProbe") {
		t.Errorf("CONTROL: a new reader of ip_match was not caught: %v", got)
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
		if regexp.MustCompile(`(?i)\btransactions\b`).MatchString(stmt) && evidenceColumnRE.MatchString(stmt) {
			out[src[m[2]:m[3]]] = stmt
		}
	}
	return out
}
