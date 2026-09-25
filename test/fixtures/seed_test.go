package fixtures

// seed_test.go — the drift check between seed.sql's plaque INSERT, its encoded_at
// stamp and SeedTags (M10 F0-6, second round, B3).
//
// 🔴 A NEW DEMO PLAQUE HAS THREE HOMES NOW, AND ONLY ONE OF THEM FAILS LOUDLY ON ITS
// OWN. Forgetting SeedTags is caught by seedkeys' drift guard at `make seed`. The
// encoded_at UPDATE below the INSERT has no such guard: a plaque missing from its uid
// list is loaded without a stamp, so the demo panel shows it as "cannot go on a wall"
// or "needs replacing" and its card refuses to mount it — a demo quietly teaching its
// audience to ignore the one banner that must never be ignored. This reads seed.sql
// as TEXT, with no database, so the check runs on every `go test` rather than only
// against a freshly seeded one.
//
// ⚠️ IT READS THE SQL THE DATABASE READS, NOT THE FILE'S BYTES (third round). The
// first version matched uids anywhere between the statement's first and last line,
// so a uid commented out of the stamp list (`-- '04…',`) still counted as stamped —
// a green check over a plaque the database would load unstamped. Comments are now
// stripped the way PostgreSQL does it — outside string literals only — before
// anything is matched, and the statement shapes are pinned to exactly one each.

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// seedUID is a canonical plaque uid as seed.sql writes it: a quoted literal.
var seedUID = regexp.MustCompile(`'([0-9A-F]{14})'`)

// firstOfTuple is a uid at the START of a VALUES tuple — the row's own uid, never
// the replaced_by that sits at the end of the retired plaque's row.
var firstOfTuple = regexp.MustCompile(`\(\s*'([0-9A-F]{14})'`)

// The two statements, matched on comment-free SQL. Case and whitespace are free
// because SQL's are; `tags\b` keeps a future `tags_something` table out.
var (
	insertTags = regexp.MustCompile(`(?is)\binsert\s+into\s+tags\b(.*?)\bon\s+conflict\s*\(\s*uid\s*\)\s*do\s+nothing\s*;`)
	stampTags  = regexp.MustCompile(`(?is)\bupdate\s+tags\s+set\s+encoded_at\b(.*?);`)
	// anyInsertTags / anyStampTags count EVERY statement of each kind, so a second
	// block — which the two patterns above would silently ignore — is a failure
	// rather than a plaque nobody checked.
	anyInsertTags = regexp.MustCompile(`(?i)\binsert\s+into\s+tags\b`)
	anyStampTags  = regexp.MustCompile(`(?i)\bupdate\s+tags\s+set\s+encoded_at\b`)
)

func TestSeedSQL_EveryLoadedPlaqueIsStampedAndListed(t *testing.T) {
	raw, err := os.ReadFile("seed.sql")
	if err != nil {
		t.Fatalf("read seed.sql: %v", err)
	}
	// The stripper tracks single quotes only; a dollar-quoted body could hold an
	// unbalanced one and flip it. seed.sql has none — pinned rather than assumed.
	if strings.Contains(string(raw), "$$") {
		t.Fatal("seed.sql contains a dollar-quoted body; stripSQLComments does not " +
			"understand one, so this check cannot trust what it strips")
	}
	src := stripSQLComments(string(raw))

	// 🔴 ONE BLOCK OF EACH, PINNED. The check below compares one INSERT with one
	// UPDATE; a second of either would be a plaque (or a stamp) it never looked at.
	// If seed.sql ever genuinely needs a second block, this is the line to change —
	// deliberately, together with the parsing below.
	if n := len(anyInsertTags.FindAllString(src, -1)); n != 1 {
		t.Fatalf("seed.sql has %d `INSERT INTO tags` statements, want exactly 1 — this "+
			"check reads one block and would miss the plaques in any other", n)
	}
	if n := len(anyStampTags.FindAllString(src, -1)); n != 1 {
		t.Fatalf("seed.sql has %d `UPDATE tags SET encoded_at` statements, want exactly 1", n)
	}
	insert := insertTags.FindStringSubmatch(src)
	if insert == nil {
		t.Fatal("seed.sql's tags INSERT does not end in ON CONFLICT (uid) DO NOTHING; " +
			"the idempotency the file promises is gone, or the pattern is")
	}
	stamp := stampTags.FindStringSubmatch(src)
	if stamp == nil {
		t.Fatal("seed.sql has no terminated `UPDATE tags SET encoded_at … ;`")
	}

	loaded := uids(firstOfTuple, insert[1])
	stamped := uids(seedUID, stamp[1])
	listed := make([]string, 0, len(SeedTags))
	for _, s := range SeedTags {
		listed = append(listed, s.UID)
	}
	sort.Strings(listed)

	// ANTI-VACUITY: a scan that found nothing agrees with everything.
	if len(loaded) < 2 {
		t.Fatalf("found %d plaque uid(s) in the tags INSERT; the scan is not reading it", len(loaded))
	}
	if strings.Join(loaded, ",") != strings.Join(stamped, ",") {
		t.Errorf("seed.sql loads %v but stamps %v as encoded — a plaque loaded and not "+
			"stamped shows on the demo panel as unmountable", loaded, stamped)
	}
	if strings.Join(loaded, ",") != strings.Join(listed, ",") {
		t.Errorf("seed.sql loads %v but fixtures.SeedTags lists %v", loaded, listed)
	}
}

// TestStripSQLComments_KeepsWhatPostgresKeeps is the stripper's own control. A
// stripper that removed too little is the defect it was written for; one that
// removed too much (the inside of a string literal) would hide a real uid.
func TestStripSQLComments_KeepsWhatPostgresKeeps(t *testing.T) {
	for in, want := range map[string]string{
		"a -- gone\nb":                   "a \nb",
		"a /* gone\n still gone */ b":    "a  b",
		"'--kept' -- gone":               "'--kept' ",
		"'it''s -- kept' x":              "'it''s -- kept' x",
		"'/* kept */' /* gone */":        "'/* kept */' ",
		"  -- '04AC7E55000901',\n'ok'":   "  \n'ok'",
		"x -- no newline at the end":     "x ",
		"/* unterminated comment to EOF": "",
	} {
		if got := stripSQLComments(in); got != want {
			t.Errorf("stripSQLComments(%q) = %q, want %q", in, got, want)
		}
	}
}

// stripSQLComments removes `--` line comments and `/* */` block comments OUTSIDE
// single-quoted string literals, the way PostgreSQL's lexer treats them (a doubled
// quote is an escaped quote inside a literal). A line comment keeps its terminating
// newline so line structure survives. NOT handled, and each is either pinned or
// fails loudly: nested block comments (seed.sql writes none; an unterminated one
// swallows the rest of the input, which fails the statement checks) and
// dollar-quoted bodies (the test refuses a seed.sql that contains one).
func stripSQLComments(src string) string {
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case inQuote:
			b.WriteByte(c)
			if c == '\'' {
				if i+1 < len(src) && src[i+1] == '\'' {
					b.WriteByte('\'')
					i++
				} else {
					inQuote = false
				}
			}
		case c == '\'':
			inQuote = true
			b.WriteByte(c)
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				b.WriteByte('\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += 2 + end + 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// uids returns the sorted, de-duplicated first capture group of every match.
func uids(re *regexp.Regexp, text string) []string {
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		seen[m[1]] = true
	}
	out := make([]string, 0, len(seen))
	for u := range seen {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}
