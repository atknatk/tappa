package ledger

// anomaly_test.go -- the structural proofs behind the ANOMALIES section (M6-11).
//
// WHAT IS HERE AND WHAT IS NOT. These are the properties a database cannot show: that
// the read path issues no write, that its cap is detectable, that no type in it could
// carry a coordinate, and that the §4.5 net this package owns really does see an
// aggregate FILTER correctly. The behaviour -- which rows produce which counts -- is
// measured against real Postgres in internal/handler/anomalies_db_test.go, because a
// fake reader cannot hold a practice row, a second tenant or a counter gap.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestAnomalies_IssuesOnlySelects is §4.3 made mechanical for this section.
//
// 🔴 IT IS THE PROPERTY THE WHOLE SECTION STANDS ON. `transactions` takes no UPDATE and
// no DELETE (the schema's own trigger refuses them), but INSERT is a different matter:
// a "seen" counter, a cached tally or a materialised summary are all things a reporting
// screen tends to grow, and every one of them would be a page view writing to the
// product's evidence table. This parses anomaly.go, collects every sqlc query it names
// and reads each one's first keyword out of db/queries.
//
// IT READS THE SQL RATHER THAN THE NAME, because "Count…" and "List…" are conventions
// and conventions do not fail builds.
func TestAnomalies_IssuesOnlySelects(t *testing.T) {
	t.Parallel()
	declared := declaredQueries(t)
	named := queriesNamedInLedgerFile(t, "anomaly.go", declared)
	// ANTI-VACUITY: a walk that found nothing would report a clean read path. The floor
	// is one below the eight the file makes today, so deleting one read is not a
	// failure and reading the wrong file is.
	if len(named) < 7 {
		t.Fatalf("anomaly.go names %d store quer(y/ies) (%v); the section's read path names "+
			"more than that, so this scan is reading the wrong file", len(named), named)
	}
	t.Logf("store queries named by anomaly.go: %d (%v)", len(named), named)
	for _, name := range named {
		file, body, ok := findQuery(t, name)
		if !ok {
			t.Errorf("anomaly.go names %q, which no file in db/queries declares", name)
			continue
		}
		if first := anomalyFirstKeyword(body); first != "SELECT" && first != "WITH" {
			t.Errorf("anomaly.go calls %s (%s), whose statement starts with %s. This section "+
				"is a READ of an immutable table: a page view that appended a row would be "+
				"writing to the product's own evidence, and nothing can take it back.",
				name, file, first)
		}
	}
}

// TestAnomalySelectCheck_IsNotVacuous is the negative control for the check above:
// pointed at a statement that DOES write, it must flag it.
func TestAnomalySelectCheck_IsNotVacuous(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, want string }{
		{"\n\nINSERT INTO transactions (id) VALUES ($1)\n", "INSERT"},
		{"  \n  select 1\n", "SELECT"},
		{"-- name: X :many\nWITH a AS (SELECT 1) SELECT * FROM a\n", "WITH"},
		// ⚠️ THE TABLE IN THIS LITERAL IS `employees` AND THAT IS DELIBERATE. The first
		// draft wrote an unconditional counter update on the tags table, which is the
		// §4.4 shape scripts/redline-check.sh's R4 scans for -- and R4 has NO test-file
		// exemption, so `make audit` went red on a string that is only ever compared to
		// the word "UPDATE". (The second draft QUOTED that statement in this comment
		// and went red again: the scan is line-based and reads comments.) Naming a
		// table the scan does not guard keeps this assertion doing its one job without
		// teaching anybody to widen an exemption.
		{"UPDATE employees SET status = 'active'", "UPDATE"},
	} {
		if got := anomalyFirstKeyword(c.in); got != c.want {
			t.Errorf("anomalyFirstKeyword(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// anomalyFirstKeyword returns the first SQL keyword of a query body, upper-cased.
//
// ⚠️ IT SKIPS A LEADING sqlc ANNOTATION. queryBody's marker regexp consumes
// `-- name: X ` and leaves the `:many` that follows it on the first line, so a naive
// reader reports every query as starting with ":MANY" -- three false alarms in the
// sibling copy of this check, and the kind that trains a reader to widen a net until it
// stops seeing anything.
func anomalyFirstKeyword(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if strings.HasPrefix(field, ":") {
				continue
			}
			return strings.ToUpper(field)
		}
	}
	return ""
}

// queriesNamedInLedgerFile is storeQueryNames narrowed to ONE file of this package.
func queriesNamedInLedgerFile(t *testing.T, file string, declared map[string]bool) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
	seen := map[string]bool{}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if declared[sel.Sel.Name] && !seen[sel.Sel.Name] {
			seen[sel.Sel.Name] = true
			out = append(out, sel.Sel.Name)
		}
		return true
	})
	return out
}

// TestAnomalyReadLimit_MakesTheCapVISIBLE is the twin of
// TestReadLimit_MakesTruncationDETECTABLE, and it exists for the same reason.
//
// 🔴 IF THE QUERY ASKED FOR EXACTLY AnomalyRowCap ROWS, a full page and a shortened one
// would be the same length, every "…and there are more" flag would be false forever,
// and a listing that stopped halfway would present itself as the whole answer. That is
// a one-character edit away, and no behavioural test on this section would notice: a
// page with fifty rows looks right either way.
func TestAnomalyReadLimit_MakesTheCapVISIBLE(t *testing.T) {
	t.Parallel()
	if got := anomalyReadLimit(); int(got) <= AnomalyRowCap {
		t.Fatalf("anomalyReadLimit() = %d and the cap is %d. The read must ask for MORE "+
			"rows than it can use, or a shortened list is indistinguishable from a "+
			"complete one.", got, AnomalyRowCap)
	}
	// The pair, exercised at the boundary rather than argued about.
	for _, c := range []struct {
		name     string
		rows     int
		wantKept int
		wantMore bool
	}{
		{"one short of the cap", AnomalyRowCap - 1, AnomalyRowCap - 1, false},
		{"exactly the cap", AnomalyRowCap, AnomalyRowCap, false},
		{"the limit, i.e. one past the cap", AnomalyRowCap + 1, AnomalyRowCap, true},
	} {
		rows := make([]int, c.rows)
		kept, more := trimAnomalyRows(rows)
		if len(kept) != c.wantKept || more != c.wantMore {
			t.Errorf("%s: trimAnomalyRows(%d rows) kept %d, more=%v; want %d, more=%v",
				c.name, c.rows, len(kept), more, c.wantKept, c.wantMore)
		}
	}
}

// TestWhereBlocks_SkipsAggregateFilterAndStillSeesTheRealClause is the control for the
// narrowing whereBlocks gained in M6-11, in BOTH directions.
//
// 🔴 THE SECOND HALF IS THE LOAD-BEARING ONE. Teaching a net to ignore a construct is
// how nets get deleted one exception at a time, so the case where the ONLY tenant
// predicate hides inside a FILTER must still be refused -- otherwise the "fix" would
// have opened exactly the hole it was meant to leave closed.
func TestWhereBlocks_SkipsAggregateFilterAndStillSeesTheRealClause(t *testing.T) {
	t.Parallel()

	// (1) The shape M6-11 ships: aggregate filters plus one scoped WHERE.
	good := `SELECT count(*) FILTER (WHERE t.practice) AS a,
	                count(*) FILTER (
	                    WHERE t.channel = 'manual'
	                ) AS b
	         FROM transactions t
	         WHERE t.tenant_id = @tenant_id AND t.occurred_at >= @from_at`
	blocks := whereBlocks(good)
	if len(blocks) != 1 {
		t.Fatalf("whereBlocks read %d clause(s) from a statement with one WHERE and two "+
			"aggregate FILTERs; an aggregate's row filter is not a query's scope", len(blocks))
	}
	if !scopedByTenant(blocks[0].where, blocks[0].column) {
		t.Errorf("the statement's real WHERE was not recognised as scoped: %q", blocks[0].where)
	}

	// (2) THE HOLE THAT MUST STAY CLOSED: the tenant predicate ONLY inside a FILTER.
	bad := `SELECT count(*) FILTER (WHERE t.tenant_id = @tenant_id) AS a
	        FROM transactions t
	        WHERE t.occurred_at >= @from_at`
	blocks = whereBlocks(bad)
	if len(blocks) != 1 {
		t.Fatalf("whereBlocks read %d clause(s) from the unscoped shape, want 1", len(blocks))
	}
	if scopedByTenant(blocks[0].where, blocks[0].column) {
		t.Errorf("a query whose ONLY tenant predicate sits inside an aggregate FILTER was " +
			"accepted as scoped. The FILTER chooses which already-visible rows an " +
			"aggregate counts; it cannot scope the statement.")
	}

	// (3) And a CTE's WHERE is still read, so the narrowing did not swallow a block.
	both := `WITH w AS (
	             SELECT count(*) FILTER (WHERE x) AS n FROM transactions t
	             WHERE t.tenant_id = @tenant_id
	         )
	         SELECT n FROM w WHERE n > 0`
	if got := len(whereBlocks(both)); got != 2 {
		t.Errorf("whereBlocks read %d clause(s) from a CTE plus a main query, want 2", got)
	}
}

// TestAnomalyTypes_CarryNothingAFigureCouldHide is §4.7 asserted STRUCTURALLY, and it
// is deliberately stronger than the name-based net the reports section uses.
//
// 🔴 A NAME LIST CANNOT SEE A NEUTRALLY NAMED FIELD. `Detail`, `Extra`, `Meta`,
// `Payload` -- none of them matches any §4.7 token, and any of them typed as
// `map[string]any` or `json.RawMessage` would carry a whole policy snapshot onto a
// screen. That is not hypothetical here: three of this section's signals ARE read out
// of policy_context, and the only reason no snapshot reaches Go is that the counting
// happens inside the query. So this walks the type graph and refuses, in addition to
// the names, any field of an OPEN type -- interface, map, slice-of-byte, or a struct
// this package does not control.
//
// 🔴 IT ALSO REFUSES A FLOAT. A coordinate is a float; a distance in metres is a float.
// Every quantity this section reports is a COUNT, so a float field here would be a
// field with nothing legitimate to hold -- and the one thing it could hold is the
// distance §4.7 keeps off a screen.
//
// THE GRAPH IS DUMPED ON EVERY RUN (t.Log), because a wall nobody can see the shape of
// is a wall nobody notices moving.
func TestAnomalyTypes_CarryNothingAFigureCouldHide(t *testing.T) {
	t.Parallel()
	var dump []string
	fields := 0
	seen := map[reflect.Type]bool{}

	var walk func(reflect.Type, string)
	walk = func(rt reflect.Type, path string) {
		for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
			rt = rt.Elem()
		}
		// 🔴 THE WALK STOPS AT A LEAF THIS SECTION DOES NOT OWN. Descending into
		// time.Time and time.Location reports their unexported internals -- a wall
		// firing on the standard library is a wall nobody keeps.
		if anomalyLeafType(rt) || rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			fields++
			dump = append(dump, path+rt.Name()+"."+f.Name+" "+f.Type.String())
			if why := anomalyBannedName(f.Name); why != "" {
				t.Errorf("%s%s.%s is a field a coordinate, an address or a secret could "+
					"travel in (%s). §4.7: no query selects one, so a field for one would "+
					"have nothing to fill it from -- and a reader would fill it.",
					path, rt.Name(), f.Name, why)
			}
			if why := anomalyBannedKind(f.Type); why != "" {
				t.Errorf("%s%s.%s is of type %s, which %s. Every quantity this section "+
					"reports is a count, a name or one instant; an open type is where a "+
					"policy snapshot would arrive under a harmless name.",
					path, rt.Name(), f.Name, f.Type, why)
			}
			walk(f.Type, path+rt.Name()+".")
		}
	}
	walk(reflect.TypeOf(AnomalyScreen{}), "")

	// ANTI-VACUITY: a walker that visited nothing would pass forever. The types held
	// well over forty fields on the day this was written; the floor is under that so a
	// deletion is not a failure, and well over zero so reading the wrong type is.
	if fields < 30 {
		t.Fatalf("the walk saw %d field(s); AnomalyScreen and its rows hold more, so it is "+
			"reading the wrong type", fields)
	}
	t.Logf("§4.7 type graph, %d fields:\n%s", fields, strings.Join(dump, "\n"))
}

// anomalyLeafType names the three types the walk treats as opaque leaves.
//
// 🔴 NAMED, NOT INFERRED, and the list is deliberately three long. time.Time is one
// instant, time.Location is the zone the screen renders in, uuid.UUID is one identity
// this tenant already owns -- and every one of them is a struct or an array whose
// INSIDES belong to somebody else. A rule like "stop at any standard-library type"
// would also stop at whatever the next edit imports.
func anomalyLeafType(rt reflect.Type) bool {
	switch rt.String() {
	case "time.Time", "time.Location", "uuid.UUID":
		return true
	}
	return false
}

// anomalyBannedKind reports why a field's TYPE is refused, or "" for an acceptable one.
//
// THE ALLOWED SET IS TINY AND NAMED, which is the opposite of a deny list: a type that
// is not on it is refused whatever it is called. The three leaves are allowed because
// the section legitimately carries one instant, one zone and one identity per row --
// and a uuid.UUID is a [16]byte, so it has to be named BEFORE the byte-array rule that
// exists to refuse an encoded payload.
func anomalyBannedKind(rt reflect.Type) string {
	if anomalyLeafType(rt) {
		return ""
	}
	switch rt.Kind() {
	case reflect.String, reflect.Bool, reflect.Int:
		return ""
	case reflect.Interface:
		return "is an interface, so anything at all fits in it"
	case reflect.Map:
		return "is a map, which is how a whole policy snapshot arrives under one name"
	case reflect.Float32, reflect.Float64:
		return "is a float, and the only floats in this product's tap path are a " +
			"coordinate and a distance"
	case reflect.Ptr:
		return anomalyBannedKind(rt.Elem())
	case reflect.Slice, reflect.Array:
		if rt.Elem().Kind() == reflect.Uint8 {
			return "is a byte slice, which carries anything a coordinate could be encoded as"
		}
		return anomalyBannedKind(rt.Elem())
	case reflect.Struct:
		switch rt.String() {
		case "ledger.Period", "ledger.Date",
			"ledger.AnomalyTotals", "ledger.MaskedOpenTally", "ledger.Anomalies",
			"ledger.AnomalyPerson", "ledger.AnomalyVenue", "ledger.AnomalyPlaque",
			"ledger.AnomalyPair", "ledger.AnomalyOpen", "ledger.AnomalyPolicyRule",
			"ledger.AnomalyScreen":
			return ""
		}
		return "is a struct type this section does not own, so what it can hold is not " +
			"this file's decision"
	default:
		return "is of a kind this section has no use for (" + rt.Kind().String() + ")"
	}
}

// anomalyBannedName is the name half of the wall. It is the same idea as the reports
// section's matcher and is kept independent on purpose: two nets written from the same
// rule catch different mistakes, and a shared one is a shared blind spot.
func anomalyBannedName(name string) string {
	low := strings.ToLower(name)
	for _, tok := range []string{
		"latitude", "longitude", "lng", "gps", "coord", "geoloc", "addr",
		"token", "secret", "credential", "cmac", "password", "invite", "hash",
		"nonce", "bearer", "snapshot",
	} {
		if strings.Contains(low, tok) {
			return "the name contains " + tok
		}
	}
	for _, word := range anomalyCamelWords(name) {
		switch word {
		case "lat", "ip", "key", "code", "where", "geo", "salt", "context":
			return "the name carries the word " + word
		}
	}
	return ""
}

// anomalyCamelWords splits a Go field name into lower-cased words, keeping acronyms
// whole: TagUID -> tag, uid; OutsideVenue -> outside, venue.
func anomalyCamelWords(name string) []string {
	runes := []rune(name)
	var out []string
	start := 0
	for i := 1; i < len(runes); i++ {
		lowerToUpper := isLowerRune(runes[i-1]) && isUpperRune(runes[i])
		acronymEnd := i+1 < len(runes) && isUpperRune(runes[i-1]) &&
			isUpperRune(runes[i]) && isLowerRune(runes[i+1])
		if lowerToUpper || acronymEnd {
			out = append(out, strings.ToLower(string(runes[start:i])))
			start = i
		}
	}
	return append(out, strings.ToLower(string(runes[start:])))
}

func isLowerRune(r rune) bool { return r >= 'a' && r <= 'z' }
func isUpperRune(r rune) bool { return r >= 'A' && r <= 'Z' }

// TestAnomalyWalls_RefuseWhatTheyExistToRefuse is the control for BOTH matchers, in
// both directions. Without the accept half, either would be satisfied by a matcher that
// refuses everything -- which is a net somebody deletes rather than works past.
func TestAnomalyWalls_RefuseWhatTheyExistToRefuse(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"GPS", "GpsLat", "Lat", "Longitude", "Coords", "SourceIP", "RemoteAddr",
		"PolicyContext", "SessionToken", "InviteCode", "AesKey", "Cmac", "TokenHash",
		"PasswordHash", "Where", "Geoloc", "Snapshot",
	} {
		if anomalyBannedName(name) == "" {
			t.Errorf("the §4.7 name wall ACCEPTS the field name %q", name)
		}
	}
	for _, name := range []string{
		// Every field name this section really uses is required to pass. These are
		// checked against the live type graph on every run by the walk above; naming
		// them here as well is what turns "it happens to pass" into "it must".
		"Records", "Judged", "Unanswerable", "DistanceOnly", "OutsideVenue",
		"OutsideVenueOnNetwork", "CounterGaps", "OtherVenue", "Training", "ManagerTyped",
		"TagUID", "LocationName", "LargestGap", "Together", "Days", "ClosestSeconds",
		"StillOpen", "ClosedLater", "SID", "Layer", "WasMaskable", "MorePlaques",
		"TogetherMinDays", "TenantName", "Queried", "Period", "Zone", "EmployeeID",
	} {
		if why := anomalyBannedName(name); why != "" {
			t.Errorf("the §4.7 name wall REFUSES %q, a name this section really uses (%s); "+
				"a wall that refuses everything is one somebody deletes rather than fixes",
				name, why)
		}
	}
	// The type half, in both directions.
	for _, c := range []struct {
		rt   reflect.Type
		want bool // want refused
	}{
		{reflect.TypeOf(""), false},
		{reflect.TypeOf(0), false},
		{reflect.TypeOf(true), false},
		{reflect.TypeOf(time.Time{}), false},
		{reflect.TypeOf(uuid.UUID{}), false},
		{reflect.TypeOf(time.UTC), false},
		{reflect.TypeOf(map[string]any{}), true},
		{reflect.TypeOf([]byte(nil)), true},
		{reflect.TypeOf(float64(0)), true},
		{reflect.TypeOf(struct{ X int }{}), true},
	} {
		refused := anomalyBannedKind(c.rt) != ""
		if refused != c.want {
			t.Errorf("anomalyBannedKind(%s) refused=%v, want %v", c.rt, refused, c.want)
		}
	}
	// AND THE SPLITTER BOTH MODES STAND ON.
	for _, c := range []struct {
		in   string
		want string
	}{
		{"TagUID", "tag,uid"},
		{"OutsideVenueOnNetwork", "outside,venue,on,network"},
		{"SID", "sid"},
		{"DistanceOnly", "distance,only"},
	} {
		if got := strings.Join(anomalyCamelWords(c.in), ","); got != c.want {
			t.Errorf("anomalyCamelWords(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAnomalyThresholds_ArePrintableAndNotZero pins the two constants the pair listing
// filters by, because they are the difference between a finding and a queue.
//
// 🔴 §5 CALLS A QUEUE NORMAL -- "different people may touch the same plaque one after
// another" -- so a min-days threshold of 1 would list every busy shift start in the
// business as if it were a signal. A zero window would list nothing at all, which is
// worse: a screen that quietly reports no pairs because its threshold is broken looks
// exactly like a screen reporting a clean week.
func TestAnomalyThresholds_ArePrintableAndNotZero(t *testing.T) {
	t.Parallel()
	if TogetherWithin <= 0 {
		t.Fatalf("TogetherWithin is %v; a non-positive window matches nothing and the "+
			"screen would report a clean week it never measured", TogetherWithin)
	}
	if TogetherWithin >= 60*time.Second {
		t.Errorf("TogetherWithin is %v, which is at or past §5's own per-person debounce "+
			"of 60 s. Past that the list stops describing 'arrived together' and starts "+
			"describing 'were both on shift'.", TogetherWithin)
	}
	if TogetherMinDays < 2 {
		t.Errorf("TogetherMinDays is %d. §5 says a queue at one plaque is ordinary, so a "+
			"single day's burst is not a signal -- at 1 this listing reports every busy "+
			"shift start in the business.", TogetherMinDays)
	}
	if int(TogetherWithin/time.Second) == 0 {
		t.Error("the window rounds to 0 whole seconds, so the sentence the screen prints " +
			"would say 'within 0 seconds'")
	}
}
