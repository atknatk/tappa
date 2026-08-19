// Command rotatekek re-wraps every plaque's per-tag AES key under a NEW KEK, so
// that a leaked TAPPA_TAG_KEK can be retired without touching a single wall.
//
// It is the executable half of the M8-02 acceptance criterion "KEK dönme
// prosedürü yazılı VE yürütülebilir"; the written half is the runbook in
// deploy/README.md ("KEK döndürme"). A runbook that cannot be run is worth
// nothing at the moment it is needed.
//
// ============================================================================
// WHAT THIS ROTATES, AND WHAT IT DOES NOT
// ============================================================================
// It rotates the KEK: the SAME 16-byte per-tag AES key is re-sealed under a new
// 32-byte KEK, so tags.aes_key_ref changes and THE CHIP ON THE WALL DOES NOT.
// Nobody visits a venue.
//
// It does NOT rotate the per-tag AES key itself. That would mean physically
// re-encoding every NTAG 424 DNA chip in the park (ADR 0003 madde 5, "Anahtar
// dondurme" -- madde 3 covers how a key is GENERATED; madde 5 is the one about
// changing one). Nothing in software can change what is burned into a chip, and
// madde 5 names the normative path for a suspect per-tag key as retire + replace,
// leaving in-place ChangeKey OUT OF THE MVP. So "re-encode the park" is a field
// operation the ADR has deliberately deferred, and it is OUT OF SCOPE here. The distinction matters in an incident: a leaked KEK
// is recoverable from a terminal; a leaked PER-TAG key is recoverable only with a
// ladder.
//
// ============================================================================
// WHY IT IS A FILTER AND NOT A DATABASE CLIENT
// ============================================================================
// This program opens no connection, holds no DSN and links no driver. It reads
// (uid, tenant_id, hex(aes_key_ref)) rows on stdin and writes SQL on stdout, in
// the shape test/fixtures/seedkeys already established for the same column and
// for the same reason: tags.aes_key_ref may only be written by tappa_owner
// (migration 00013 revoked UPDATE(aes_key_ref) from tappa_app precisely so a
// stolen application identity cannot silently brick a plaque), and the question
// of WHICH ROLE RUNS is already answered by the psql invocation in the runbook.
// A tool with its own connection would answer it a second time, and the second
// answer is the one that goes wrong at 03:00.
//
// Three further properties fall out of being a filter, and they are the reason
// this shape was chosen rather than merely tolerated:
//
//   - The operator can READ the SQL before applying it. In an incident that is
//     the difference between a procedure and a leap.
//   - The transformation is a PURE FUNCTION of (KEK_old, KEK_new, rows), so every
//     degenerate input below is unit-testable without a database.
//   - There is no partially-written state this program can leave behind: it emits
//     one transaction or it emits a refusal.
//
// The owner DSN's variable name is deliberately NOT written anywhere in this
// file. cmd/tappa/packaging_test.go asserts that string appears in no production
// source outside internal/config (so the serving binary can never be one edit
// away from connecting as the migration role), this file is a production source
// by that test's definition, and a guard that gets widened to admit new code
// stops being a guard. The runbook names it.
//
// ============================================================================
// THE MIXED PARK, AND WHY THIS TOOL IS NOT SUFFICIENT ON ITS OWN
// ============================================================================
// While the rotation runs, some rows are sealed under the new KEK and some under
// the old one. A server process holds ONE KEK for its lifetime, so a server that
// knows only one of them answers 500 to every tap on the other half — §4.6's
// "kayıt asla kaybolmaz" failing at the infrastructure layer, which is exactly
// what an attendance system may not do.
//
// This is NOT closed by writing the whole park in one transaction. Measured on a
// 72 380-row copy of the development park: a single transaction that rewrites
// every aes_key_ref holds its row locks until COMMIT, and the product's own
// replay-guard statement (AdvanceTagCounter, which takes FOR UPDATE on the tag
// row) BLOCKED FOR 10.32 SECONDS behind it. One transaction does not remove the
// window; it moves it from "a growing fraction of taps fail during the run" to
// "every tap on a locked row hangs during the run, and then EVERY tap fails
// between COMMIT and the rollout that hands the server the new KEK".
//
// What closes it is the READ path accepting both: internal/config takes an
// optional TAPPA_TAG_KEK_PREVIOUS and internal/sun tries the primary KEK first,
// then the previous one. With both loaded, the park may be mixed in either
// direction — during the run AND during a rollback — and no tap fails. The
// runbook's ordering (load both, rotate, drop the old) depends on it, and this
// tool is only correct when run inside that ordering.
//
// ============================================================================
// SCOPE: EVERY ROW, INCLUDING retired AND lost
// ============================================================================
// No status filter. §5 line 1 rejects taps on retired and lost plaques, so it is
// tempting to skip them — and it would be wrong for a reason that outlives the
// argument: a lost plaque is IN SOMEBODY'S HAND, and its key is the one that most
// needs to leave the compromised KEK's cover. Beyond that, any exclusion keeps
// the leaked KEK operationally REQUIRED forever, because some row still opens
// only under it. A rotation that does not let you destroy the old KEK is not a
// rotation. The cost of including them is zero: it is the same statement.
//
// ============================================================================
// SECRETS (§4.7)
// ============================================================================
// Neither KEK, no unwrapped per-tag key and no wrapped ref reaches the report on
// stderr. The report carries counts, tenant ids, byte LENGTHS and exit codes.
// Wrapped refs appear on stdout because they ARE the SQL being generated — that
// is the form the column holds, the same boundary test/fixtures/seedkeys states
// for itself.
//
// ⚠️ THIS PARAGRAPH USED TO END "so the runbook says pipe, do not spool", AND THE
// RUNBOOK NO LONGER DOES. Piping straight into psql masked this tool's exit code
// behind psql's (a refusal read as a partial rotation and vice versa), so
// scripts/rotate-kek.sh writes the SQL to a file, gates the exit code, and then
// applies it. The file lives in a mktemp -d (0700) created and removed by that
// script, and the same round's COPY redesign keeps these bytes out of the server
// log — the two changes are why spooling is acceptable now.
//
// The KEKs are read from the ENVIRONMENT, never from argv: argv is visible to
// every process on the host through ps, and to the shell's history file.
package main

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/atknatk/tappa/internal/sun"
)

const (
	// kekEnv is the KEK rows are re-sealed UNDER: the same variable the server
	// reads into Config.TagKEK. Naming it identically is the point — during a
	// rotation the tool and the server are pointed at ONE secret whose meaning
	// does not change depending on which program reads it.
	kekEnv = "TAPPA_TAG_KEK"
	// prevKEKEnv is the KEK rows are currently sealed under: the same variable
	// the server reads as its fallback. So the Secret that lets the server
	// survive a mixed park is, byte for byte, the Secret this tool needs.
	prevKEKEnv = "TAPPA_TAG_KEK_PREVIOUS"
	// ackEnv is how an operator ACKNOWLEDGES rows that open under neither KEK.
	// It must equal the measured count exactly; see refuse-by-default below.
	ackEnv = "TAPPA_ROTATE_ALLOW_UNOPENABLE"

	// wrappedRefLen is sun's fixed envelope size. Duplicated here as a LENGTH
	// only (never a key), so the report can bucket unopenable rows by shape
	// without importing an unexported constant.
	wrappedRefLen = 44

	// refColumn names the column this tool rewrites. It is a plain constant, and
	// naming it HERE rather than inside an error string is what keeps R7 quiet —
	// this declaration is not a fmt/log/slog call, so the scanner's pattern
	// cannot match it. The name is written out in full: a scanner defeated by a
	// split string literal would be worse than a scanner that fires.
	refColumn = "aes_key_ref"

	// exitRefused is returned when nothing was produced: bad KEKs, malformed
	// input, an empty read, or unacknowledged unopenable rows.
	exitRefused = 1
	// exitIncomplete is returned when SQL WAS produced but the park is not fully
	// rotated: rows opened under neither KEK and the operator acknowledged them.
	// It is a SEPARATE code from success on purpose — the whole failure this
	// program exists to prevent is an operator believing a rotation finished.
	exitIncomplete = 3
)

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

// row is one plaque as read from stdin.
type row struct {
	uid      string // 14 uppercase hex characters (tags.uid is character(14))
	tenantID string // 36-character UUID text
	oldHex   string // lowercase hex of the CURRENT aes_key_ref
	oldRef   []byte // decoded form of oldHex
	newRef   []byte // set only for rows this run re-seals
}

// tally is the outcome of classifying the whole park. Every field is a COUNT or
// a LENGTH; nothing here can hold key material.
type tally struct {
	read       int
	rewrapped  int
	already    int
	unopenable int
	// unopenableByTenant and unopenableByLen make the "could not open" number
	// inspectable without naming a single uid.
	unopenableByTenant map[string]int
	unopenableByLen    map[int]int
}

// run is main's testable body. It writes SQL to out and the human report to
// errOut, and returns the process exit code. getenv is injected so the degenerate
// environments are exercised by tests rather than by an operator (§7: explicit
// dependencies, no package-level state).
func run(in io.Reader, out, errOut io.Writer, getenv func(string) string) int {
	newKEK, oldKEK, err := loadKEKs(getenv)
	if err != nil {
		return refuse(out, errOut, err)
	}

	rows, err := readRows(in)
	if err != nil {
		return refuse(out, errOut, err)
	}
	if len(rows) == 0 {
		// A HEALTHY SYSTEM IS EMPTY AND A BROKEN ONE IS MISSING, and this is the
		// place those two look identical. Zero rows is what a genuinely empty
		// park looks like AND what tags looks like to a role RLS is filtering:
		// measured on the development database, tappa_app with no app.tenant_id
		// set SELECTs 0 of 72 380 rows, silently and without an error. Answering
		// "0/0 rotated, done" to either is fail-open, so neither is accepted.
		return refuse(out, errOut, fmt.Errorf(
			"read 0 rows on stdin. Either the park really is empty, or the reading role could not SEE it: "+
				"tags has FORCE ROW LEVEL SECURITY, so a connection that is not tappa_owner (or has no "+
				"app.tenant_id set) selects zero rows with no error at all. Check which role the COPY ran as"))
	}

	t, err := classify(rows, newKEK, oldKEK)
	if err != nil {
		return refuse(out, errOut, err)
	}

	// REFUSE BY DEFAULT ON ROWS THAT OPEN UNDER NEITHER KEK. Silently skipping
	// them is the exact shape of §4.6: the operator would read "33 318 re-wrapped"
	// and believe the rotation finished while rows stayed under the leaked KEK.
	//
	// The acknowledgement is an EXACT count, so a run cannot be waved through by a
	// stale number. It is a SPEED BUMP AND NOT A RECORD — this comment claimed
	// "and a record" long after deploy/README.md had corrected the same sentence
	// in itself: nothing here writes an audit_log row, a file, or any DB trace, so
	// after step 3 the product cannot answer "how many rows stayed under the
	// leaked KEK". That gap is a named counted limit in the runbook.
	ack, ackSet, err := readAck(getenv)
	if err != nil {
		return refuse(out, errOut, err)
	}
	if t.unopenable > 0 && (!ackSet || ack != t.unopenable) {
		writeReport(errOut, t)
		got := "unset"
		if ackSet {
			got = strconv.Itoa(ack)
		}
		return refuse(out, errOut, fmt.Errorf(
			"%d of %d rows open under NEITHER KEK and stay sealed under the old one. Nothing was written. "+
				"Read the breakdown above, then re-run with %s=%d to accept exactly that many (currently %s)",
			t.unopenable, t.read, ackEnv, t.unopenable, got))
	}

	if err := writeSQL(out, rows, t); err != nil {
		// A rotation whose SQL did not reach the pipe is not a partial success; it
		// is a refusal that happened late.
		return refuse(out, errOut, err)
	}
	writeReport(errOut, t)
	if t.unopenable > 0 {
		fmt.Fprintf(errOut, "\nEXIT %d: SQL was written, but %d rows remain under the old KEK. "+
			"The old KEK CANNOT be destroyed yet.\n", exitIncomplete, t.unopenable)
		return exitIncomplete
	}
	fmt.Fprintf(errOut, "\nEXIT 0: every row read is accounted for under %s.\n", kekEnv)
	return 0
}

// refuse writes the reason to stderr and puts a POISON statement on stdout.
//
// Emitting nothing would be fail-closed only if the operator remembered
// `set -o pipefail`; without it `rotatekek | psql` reports psql's exit status,
// psql given empty input succeeds, and a refusal reads as a successful rotation.
// So the refusal is carried IN THE SQL STREAM, where pipe semantics cannot lose
// it: any psql that receives this aborts.
func refuse(out, errOut io.Writer, err error) int {
	fmt.Fprintf(errOut, "rotatekek: REFUSED: %v\n", err)
	// If even the poison cannot be written, say so on stderr — the operator's only
	// remaining channel. The exit code is unchanged: a refusal is a refusal.
	if _, werr := fmt.Fprintln(out, "DO $$ BEGIN RAISE EXCEPTION "+
		"'rotatekek refused and wrote no rotation SQL; read its stderr. Nothing was changed.'; END $$;"); werr != nil {
		fmt.Fprintf(errOut, "rotatekek: and the refusal could not be written to stdout either: %v\n", werr)
	}
	return exitRefused
}

// loadKEKs reads and validates both KEKs. Errors name the VARIABLE and the
// failure, never the value (§4.7) — the same discipline internal/sun/keys.go
// applies to lengths.
func loadKEKs(getenv func(string) string) (newKEK, oldKEK []byte, err error) {
	newKEK, err = decodeKEK(getenv, kekEnv)
	if err != nil {
		return nil, nil, err
	}
	oldKEK, err = decodeKEK(getenv, prevKEKEnv)
	if err != nil {
		return nil, nil, err
	}
	// IDENTICAL KEKS ARE REFUSED RATHER THAN TREATED AS A NO-OP. Re-sealing every
	// row under the key it already uses would "succeed" and rotate nothing, and
	// the only realistic way to get here is an operator who pasted one value into
	// both variables — the single mistake that would leave a leaked KEK live while
	// every report says the rotation completed. The comparison is on the DECODED
	// bytes, so two spellings of the same key (padding, whitespace) are caught too.
	if string(newKEK) == string(oldKEK) {
		return nil, nil, fmt.Errorf("%s and %s decode to the SAME key, so this would re-seal the park under "+
			"the key it already uses and rotate nothing. Generate a new one: openssl rand -base64 32", kekEnv, prevKEKEnv)
	}
	return newKEK, oldKEK, nil
}

// decodeKEK turns one base64 environment variable into 32 raw bytes.
func decodeKEK(getenv func(string) string, name string) ([]byte, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return nil, fmt.Errorf("%s is empty. Both KEKs are required: %s is the key rows are sealed under "+
			"TODAY, %s is the key they are being moved TO. Export them from a file — never on the command "+
			"line, where ps and the shell history can read them", name, prevKEKEnv, kekEnv)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: not valid base64", name)
	}
	if len(key) != 32 {
		// 32 is stated rather than derived: sun.Wrap would reject anything else
		// anyway, but it would do so 33 000 times instead of once, and the
		// operator would be reading a per-row error for an environment mistake.
		return nil, fmt.Errorf("%s: want 32 bytes, got %d", name, len(key))
	}
	return key, nil
}

// readAck parses the acknowledgement variable. An unparseable or negative value
// is an error rather than "treat as unset": a typo must not silently become the
// stricter default and then look like a different failure.
func readAck(getenv func(string) string) (n int, set bool, err error) {
	raw := strings.TrimSpace(getenv(ackEnv))
	if raw == "" {
		return 0, false, nil
	}
	n, convErr := strconv.Atoi(raw)
	if convErr != nil || n < 0 {
		// 🔴 THE VALUE IS NOT ECHOED, ONLY ITS LENGTH. This used to print %q of the
		// raw environment value, and it was the ONE place in this file that broke
		// the rule the rest of it states twice: errors carry lengths, never
		// contents. Measured: exporting a KEK into this variable by mistake put
		// the whole base64 key on stderr — and the runbook tells the operator to
		// KEEP that stderr as the only record of the run.
		return 0, false, fmt.Errorf("%s must be a non-negative whole number (the exact count of rows that "+
			"open under neither KEK); got a %d-character value that is not one", ackEnv, len(raw))
	}
	return n, true, nil
}

// readRows parses the COPY stream: uid \t tenant_id \t hex(aes_key_ref).
//
// Every field is validated to a SHAPE before it is used, because these values are
// interpolated into SQL further down (writeSQL states that boundary). A row this
// function accepts cannot carry a quote, a backslash or a newline.
func readRows(in io.Reader) ([]row, error) {
	sc := bufio.NewScanner(in)
	// NO sc.Buffer CALL, and the reason is a MEASUREMENT that contradicted the
	// comment that used to stand here. That comment raised the token ceiling to
	// 1 MB and said an over-long line would otherwise be "silently truncated into
	// a valid-looking one, which is how bufio.Scanner fails by default". Measured
	// on this Go: an over-long line makes Scan stop and Err return
	// "bufio.Scanner: token too long" — WITH the raised ceiling and WITHOUT it,
	// identically. Scanner does not truncate silently; it reports.
	//
	// So the ceiling was defending against nothing (a legal line is uid + tenant +
	// 88 hex characters, comfortably under 200 bytes, and the default is 64 KB),
	// and what actually stops a truncated read from passing as a short park is the
	// Err() check at the bottom of this function — which is pinned by
	// TestReadRows_TruncatedStreamIsNotAShortPark.

	var rows []row
	seen := make(map[string]int)
	for line := 1; sc.Scan(); line++ {
		text := sc.Text()
		if text == "" {
			return nil, fmt.Errorf("line %d is empty; expected uid<TAB>tenant_id<TAB>hex of %s", line, refColumn)
		}
		f := strings.Split(text, "\t")
		if len(f) != 3 {
			return nil, fmt.Errorf("line %d has %d tab-separated fields, want 3 (uid, tenant_id, hex of %s)",
				line, len(f), refColumn)
		}
		r := row{uid: f[0], tenantID: f[1], oldHex: f[2]}
		if err := validUID(r.uid); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if err := validUUID(r.tenantID); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		ref, err := hex.DecodeString(r.oldHex)
		if err != nil {
			// The message names the FIELD, not the bytes: a wrapped ref is not a
			// secret, but printing input back verbatim is how a log grows one.
			//
			// The column is named through a constant rather than inline, and that
			// is not style: scripts/redline-check.sh R7 fails any line matching
			// `(slog|log|fmt)\.[A-Za-z]+\(` followed by `aes_?key` before the
			// closing paren, case-insensitively. Writing the name inside this
			// fmt.Errorf would trip it. seedkeys documents the same scanner
			// escaping a line only "by accident of formatting"; a constant is not
			// an accident.
			return nil, fmt.Errorf("line %d: %s is not hex", line, refColumn)
		}
		r.oldRef = ref
		// DUPLICATE UIDS ARE REFUSED. uid is the primary key, so a duplicate
		// cannot come from the database; it means the input was concatenated,
		// replayed or hand-edited. It matters beyond hygiene: the emitted
		// statement joins on uid, and two rows claiming one uid make WHICH
		// wrapped value wins undefined.
		if prev, dup := seen[r.uid]; dup {
			return nil, fmt.Errorf("line %d: uid already seen on line %d; the input is not a clean read of tags", line, prev)
		}
		seen[r.uid] = line
		rows = append(rows, r)
	}
	if err := sc.Err(); err != nil {
		// A read that DIES mid-stream must never look like a read that ENDED.
		// This is the "database connection dropped mid-run" path: psql's COPY is
		// cut short, the scanner reports the error, and the tool refuses instead
		// of rotating the prefix it happened to receive and calling it the park.
		return nil, fmt.Errorf("reading stdin failed after %d rows, so the input is a TRUNCATED read of the "+
			"park and not the park: %w", len(rows), err)
	}
	return rows, nil
}

func validUID(s string) error {
	if len(s) != 14 {
		return fmt.Errorf("uid must be 14 hex characters, got %d", len(s))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f') {
			continue
		}
		return fmt.Errorf("uid is not hex")
	}
	return nil
}

func validUUID(s string) error {
	if len(s) != 36 {
		return fmt.Errorf("tenant_id must be a 36-character uuid, got %d characters", len(s))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return fmt.Errorf("tenant_id is not a uuid")
			}
			continue
		}
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return fmt.Errorf("tenant_id is not a uuid")
	}
	return nil
}

// classify decides, for every row, whether it must be re-sealed, is already done,
// or opens under neither KEK. It mutates rows[i].newRef for the first group.
//
// THE ORDER OF THE TWO ATTEMPTS IS LOAD-BEARING AND IT IS "NEW FIRST".
// Nothing in the schema records which KEK a row is sealed under (there is no
// version column, by design — see the runbook), so the only way to find out is to
// try. Trying the NEW key first makes a re-run of an interrupted rotation cheap
// and, more importantly, makes the tool IDEMPOTENT: a row already re-sealed is
// recognised as done instead of being re-sealed a second time under a fresh
// nonce. An operator whose run was killed half way just runs it again.
func classify(rows []row, newKEK, oldKEK []byte) (tally, error) {
	t := tally{
		read:               len(rows),
		unopenableByTenant: map[string]int{},
		unopenableByLen:    map[int]int{},
	}
	for i := range rows {
		r := &rows[i]
		uidBytes, err := hex.DecodeString(r.uid)
		if err != nil || len(uidBytes) != 7 {
			// validUID already guarantees this cannot happen; kept as defence
			// rather than a comment claiming it cannot (§7 — surfaced, not
			// swallowed).
			return tally{}, fmt.Errorf("uid does not decode to 7 bytes; the input was validated and still got here")
		}

		// Already under the new KEK? Then this row is DONE.
		if opened, _ := withUnwrapped(newKEK, uidBytes, r.oldRef, noop); opened {
			t.already++
			continue
		}

		var ref []byte
		opened, err := withUnwrapped(oldKEK, uidBytes, r.oldRef, func(plain []byte) error {
			var e error
			ref, e = sun.Wrap(newKEK, uidBytes, plain)
			return e
		})
		if err != nil {
			return tally{}, fmt.Errorf("re-sealing a row failed: %w", err)
		}
		if !opened {
			// Opens under neither. NOT skipped silently: counted, bucketed and
			// surfaced. GCM cannot tell a wrong KEK from a corrupt ref from a ref
			// moved to another row, by design, so the tool does not guess either —
			// it reports the LENGTH, which is the one thing that distinguishes
			// "wrong key" from "not an envelope at all".
			t.unopenable++
			t.unopenableByTenant[r.tenantID]++
			t.unopenableByLen[len(r.oldRef)]++
			continue
		}
		r.newRef = ref
		t.rewrapped++
	}
	return t, nil
}

// writeSQL emits ONE transaction: two post-conditions and one set-based UPDATE.
//
// WHY ONE STATEMENT RATHER THAN N. Measured on a 72 380-row copy of the
// development park: a transaction holding row locks on tags blocks the product's
// replay-guard statement for as long as it stays open (10.32 s in the probe). The
// lock window is therefore minimised, not the transaction count: one set-based
// UPDATE holds locks for the duration of one statement instead of the duration of
// thirty thousand round trips.
//
// WHY THE VALUES CARRY THE OLD REF TOO. The join requires aes_key_ref to still BE
// what was read. A row somebody changed between the COPY and this statement
// simply does not match, the affected count comes up short, and post-condition 2
// aborts the transaction. Without it, a concurrent change would be overwritten by
// a value computed from a stale read.
//
// INTERPOLATION BOUNDARY, stated rather than assumed (the same boundary
// test/fixtures/seedkeys writes for itself): this program has no driver and thus
// no bind parameters, so the statement is built by formatting. What makes that
// safe is that every value was constrained to a SHAPE before arriving here — uid
// through validUID (14 hex characters), tenant_id through validUUID (hex and
// hyphens), and both refs through hex.EncodeToString of bytes this program
// produced. None of the four can contain a quote, a backslash or a newline. THE
// MOMENT any of them comes from somewhere else, this reasoning is void and the
// answer is a driver with bind parameters, not quoting added here.
// NOTE ON HOW THIS FUNCTION IS WRITTEN: it builds the statement with
// strings.Builder and strconv, and uses NO Go format verbs. That is a correctness
// requirement, not a style choice. PL/pgSQL's RAISE uses `%` as its own
// placeholder and `%%` as a literal percent, so a Go format string carrying
// RAISE text has two substitution languages fighting over one character — the
// first draft of this function shipped two bugs from exactly that (`% matched`
// became Go's `%!m(MISSING)`, and a doubled `%%` left RAISE with one placeholder
// and two arguments, which is a runtime error in the emitted SQL). Keeping Go's
// formatter out of the SQL removes the collision entirely.
func writeSQL(out io.Writer, rows []row, t tally) error {
	var b strings.Builder
	b.WriteString("-- GENERATED by cmd/rotatekek — do not edit, do not commit, do not keep.\n")
	b.WriteString("-- Re-seals per-tag AES keys under a new KEK. The chips on the walls are unchanged.\n")
	b.WriteString("-- read " + strconv.Itoa(t.read) +
		" rows · re-sealing " + strconv.Itoa(t.rewrapped) +
		" · already done " + strconv.Itoa(t.already) +
		" · opens under neither KEK " + strconv.Itoa(t.unopenable) + "\n")
	b.WriteString("BEGIN;\n")
	// 🔴 A MECHANICAL CEILING ON HOW LONG *THIS ROTATION* WILL WAIT FOR ITS OWN
	// LOCKS — NOT ON HOW LONG A TAP IS HELD BEHIND IT.
	//
	// ⚠️ THE HEADING ABOVE USED TO CLAIM THE SECOND THING AND IT WAS FALSE
	// (backlog T47, closed 2026-08-19). lock_timeout bounds only the waiting of the
	// session that SETS it. This rotation holds row locks on `tags`; the product's
	// own replay guard (AdvanceTagCounter) takes FOR UPDATE on the same rows, so a
	// tap queues behind us and lock_timeout does nothing for that tap. Measured, in
	// the round that produced this correction: with the rotation holding, a live tap
	// waited 10.07 s, while the positive control — rotation as the WAITING side —
	// was cancelled at 5.05 s, i.e. by this very setting. Earlier rounds measured
	// tap waits of 0.76 s to 13.19 s; the wait is a property of machine load.
	//
	// SO THE HONEST STATEMENT IS: there is NO ceiling on how long a tap can be held
	// (statement_timeout is 0 on the server and is set at no layer), and this line
	// buys something narrower and still worth having — the rotation FAILS FAST
	// rather than queueing indefinitely for locks it cannot get.
	//
	// TWO READINGS: (a) no timeout — the rotation always finishes, but a tap can be
	// held for an unbounded time and the person at the plaque cannot know; (b) a
	// timeout — the rotation ABORTS if it cannot take its locks quickly, nothing is
	// written (one transaction, guards first), and the operator re-runs.
	//
	// (b) is chosen: an attendance record never taken is worse than a rotation
	// retried, and retrying is free because the tool is idempotent. SET LOCAL so it
	// dies with the transaction and cannot leak into a pooled session (§6).
	b.WriteString("SET LOCAL lock_timeout = '5s';\n")

	// ---------------------------------------------------------------- guards --
	// EVERY PRECONDITION RUNS BEFORE A SINGLE WRAPPED REF IS TRANSMITTED. That
	// ordering is a §4.7 decision, not tidiness: the park-size mismatch is the
	// abort the runbook treats as routine ("re-read and re-run"), and putting it
	// ahead of the data means the common failure never puts a ref on the wire at
	// all.
	b.WriteString("DO $guard$\nDECLARE\n    park_now bigint;\n    bypasses boolean;\nBEGIN\n")
	b.WriteString("    IF NULLIF(current_setting('app.tenant_id', true), '') IS NOT NULL THEN\n")
	b.WriteString("        RAISE EXCEPTION 'rotatekek: this session has app.tenant_id set, so every statement below " +
		"is scoped to ONE tenant and a park-wide rotation is impossible. Nothing written; reconnect without it.';\n")
	b.WriteString("    END IF;\n")
	b.WriteString("    SELECT rolsuper OR rolbypassrls INTO bypasses FROM pg_roles WHERE rolname = current_user;\n")
	b.WriteString("    IF bypasses IS NOT TRUE THEN\n")
	b.WriteString("        RAISE EXCEPTION 'rotatekek: % neither is a superuser nor has BYPASSRLS, and tags has " +
		"FORCE ROW LEVEL SECURITY, so this session cannot see or write the whole park -- it would rotate only " +
		"the rows its policy admits and report success. Nothing written.', current_user;\n")
	b.WriteString("    END IF;\n")
	b.WriteString("    SELECT count(*) INTO park_now FROM tags;\n")
	b.WriteString("    IF park_now <> " + strconv.Itoa(t.read) + " THEN\n")
	b.WriteString("        RAISE EXCEPTION 'rotatekek: the input did not cover the park: " +
		strconv.Itoa(t.read) + " rows were read but tags holds %. Either the COPY ran as a role RLS " +
		"filters, or the park changed under the run. Nothing written; re-read and re-run.', park_now;\n")
	b.WriteString("    END IF;\n")
	b.WriteString("END\n$guard$;\n")

	if t.rewrapped == 0 {
		b.WriteString("DO $rotate$ BEGIN RAISE NOTICE " +
			"'rotatekek: nothing to re-seal; every row read is already under the new KEK.'; END $rotate$;\n")
		b.WriteString("COMMIT;\n")
		return writeAll(out, b.String())
	}

	// ------------------------------------------------------- data, then work --
	// 🔴 THE WRAPPED REFS TRAVEL AS *COPY DATA*, NOT AS PART OF A STATEMENT, AND
	// THAT IS THE WHOLE POINT OF THIS SHAPE.
	//
	// The first version inlined all of them in a VALUES list inside the DO block,
	// which made the rotation ONE ~19 MB statement. Postgres logs the FULL TEXT of
	// a statement that raises an error (log_min_error_statement defaults to
	// 'error'), so every failed rotation wrote the whole park's wrapped refs —
	// including the ones under the NEW KEK — into the server log. Measured on a
	// 20-row probe with log_statement='none' and log_min_duration_statement=-1,
	// i.e. with the runbook's own precondition satisfied: 40 distinct 88-hex refs
	// in the log, 20 old and 20 new.
	//
	// That is worse than the log_statement='all' exposure the runbook already
	// warns about: refs under the OLD key stop mattering once the rotation
	// completes, but refs under the NEW key stay sensitive for the life of the
	// park. And aborts are ROUTINE here — the runbook tells the operator to re-read
	// and re-run.
	//
	// Closing it by adding a third value to a precondition would be a CONFIGURATION
	// claim: it holds only while an operator, and a managed provider, keep it true.
	// COPY moves the payload out of statement text entirely, so the strongest
	// statement any error can log is the small block below. The temp table is also
	// not WAL-logged, which keeps the same bytes out of backups and replicas.
	b.WriteString("CREATE TEMP TABLE rotatekek_in (\n")
	b.WriteString("    uid        text NOT NULL,\n")
	b.WriteString("    tenant_id  text NOT NULL,\n")
	b.WriteString("    oldref_hex text NOT NULL,\n")
	b.WriteString("    newref_hex text NOT NULL\n")
	b.WriteString(") ON COMMIT DROP;\n")
	// Hex TEXT columns rather than bytea: COPY's text format treats backslash as
	// an escape, so bytea would need '\\x…' doubling. Plain hex has no character
	// COPY must escape, so no row can be mangled by a quoting rule.
	b.WriteString("COPY rotatekek_in (uid, tenant_id, oldref_hex, newref_hex) FROM STDIN;\n")
	for i := range rows {
		r := &rows[i]
		if r.newRef == nil {
			continue
		}
		b.WriteString(r.uid + "\t" + r.tenantID + "\t" + r.oldHex + "\t" + hex.EncodeToString(r.newRef) + "\n")
	}
	b.WriteString("\\.\n")

	b.WriteString("DO $rotate$\nDECLARE\n    applied bigint;\nBEGIN\n")
	b.WriteString("    WITH upd AS (\n")
	b.WriteString("        UPDATE tags t SET " + refColumn + " = decode(v.newref_hex, 'hex')\n")
	b.WriteString("          FROM rotatekek_in v\n")
	b.WriteString("         WHERE t.uid = v.uid AND t.tenant_id = v.tenant_id::uuid\n")
	b.WriteString("           AND t." + refColumn + " = decode(v.oldref_hex, 'hex')\n")
	b.WriteString("        RETURNING 1\n")
	b.WriteString("    )\n")
	b.WriteString("    SELECT count(*) INTO applied FROM upd;\n")
	b.WriteString("    IF applied <> " + strconv.Itoa(t.rewrapped) + " THEN\n")
	b.WriteString("        RAISE EXCEPTION 'rotatekek: planned to re-seal " +
		strconv.Itoa(t.rewrapped) + " rows but % matched. A row changed between the read and this " +
		"write. Nothing written; re-read and re-run.', applied;\n")
	b.WriteString("    END IF;\n")
	b.WriteString("    RAISE NOTICE 'rotatekek: re-sealed % rows under the new KEK.', applied;\n")
	b.WriteString("END\n$rotate$;\n")
	b.WriteString("COMMIT;\n")
	return writeAll(out, b.String())
}

// writeAll writes the SQL and REPORTS a failed write instead of swallowing it.
//
// 🔴 THE SWALLOWED VERSION CONTRADICTED THIS FILE'S OWN DOCTRINE. refuse() exists
// because "emitting nothing" is not fail-closed — an operator without
// `set -o pipefail` sees psql succeed on empty input and reads a refusal as a
// rotation. The success path had exactly that hole: the write result was discarded
// with `_, _ =`, so a write that failed still printed "EXIT 0: every row read is
// accounted for" and exited 0.
//
// ⚠️ THE SHELL REPRO ORIGINALLY QUOTED HERE DOES NOT PRODUCE THAT FAILURE, and
// this comment used to assert it did. Measured: `rotatekek ... 1>&-` exits 0 and
// writes its SQL — Go's runtime opens /dev/null over any closed standard
// descriptor at start-up, so the write SUCCEEDS. The hole was real; that way of
// showing it was not. It is exercised by a failing io.Writer in
// TestRun_AFailedWriteIsARefusalNotASuccess, and the real-world shape that hits it
// is a broken pipe (EPIPE), which is fail-closed either way.
func writeAll(out io.Writer, sql string) error {
	if _, err := io.WriteString(out, sql); err != nil {
		return fmt.Errorf("writing the rotation SQL failed after %d bytes were prepared, so what reached "+
			"the database (if anything) is a TRUNCATED transaction: %w", len(sql), err)
	}
	return nil
}

// writeReport prints the human-readable outcome. It contains counts, tenant ids
// and byte lengths — never a uid, never a ref, never a key (§4.7). The tenant
// breakdown is what makes "39 062 rows did not open" actionable: one tenant means
// a bad load, every tenant means a wrong KEK.
func writeReport(errOut io.Writer, t tally) {
	fmt.Fprintf(errOut, "rotatekek report\n")
	fmt.Fprintf(errOut, "  rows read from stdin ............ %d\n", t.read)
	fmt.Fprintf(errOut, "  re-sealed under the new KEK ..... %d\n", t.rewrapped)
	fmt.Fprintf(errOut, "  already under the new KEK ....... %d\n", t.already)
	fmt.Fprintf(errOut, "  open under NEITHER KEK .......... %d\n", t.unopenable)
	if t.unopenable == 0 {
		return
	}
	fmt.Fprintf(errOut, "\n  Rows that open under neither KEK stay sealed under the OLD one.\n")
	fmt.Fprintf(errOut, "  By wrapped length (a %d-byte value is a well-formed envelope that did not\n", wrappedRefLen)
	fmt.Fprintf(errOut, "  authenticate; any other length was never an envelope at all):\n")
	lens := make([]int, 0, len(t.unopenableByLen))
	for l := range t.unopenableByLen {
		lens = append(lens, l)
	}
	sort.Ints(lens)
	for _, l := range lens {
		note := "  <- not an envelope"
		if l == wrappedRefLen {
			note = "  <- envelope, wrong key or corrupt"
		}
		fmt.Fprintf(errOut, "    %5d bytes  %8d rows%s\n", l, t.unopenableByLen[l], note)
	}
	type tc struct {
		tenant string
		n      int
	}
	list := make([]tc, 0, len(t.unopenableByTenant))
	for k, v := range t.unopenableByTenant {
		list = append(list, tc{k, v})
	}
	// Sorted by count then tenant id, so two runs over the same park produce the
	// same report and a diff means the park moved, not that a map iterated.
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].tenant < list[j].tenant
	})
	fmt.Fprintf(errOut, "  Across %d tenants. Worst %d:\n", len(list), min(len(list), 20))
	for i, e := range list {
		if i == 20 {
			break
		}
		fmt.Fprintf(errOut, "    %s  %d\n", e.tenant, e.n)
	}
	fmt.Fprintf(errOut, "  (uids are deliberately not listed; query tags directly if you need them)\n")
}

// noop is the callback for "open it only to learn whether it opens".
func noop([]byte) error { return nil }

// withUnwrapped opens ref under kek and hands the PLAINTEXT per-tag key to fn for
// the duration of that call only, wiping it before returning on every path.
//
// 🔴 THE ZEROING IS STRUCTURAL RATHER THAN A LINE AT EACH CALL SITE, and that is
// the whole reason this function exists. §4.7 says the unwrapped key lives for one
// operation and is then wiped; the previous shape said that with a `sun.Zero(key)`
// at each of two call sites, and DELETING EITHER ONE CHANGED NO OBSERVABLE
// BEHAVIOUR — a declared discipline that nothing held. Here the wipe is a defer
// inside the only function that ever holds a plaintext key, so a caller cannot
// forget it and a test can watch it happen (the buffer fn saw is all zeroes once
// this returns).
//
// It reports (false, nil) when the ref does not authenticate — an ordinary
// outcome, not an error — and (false, err) only when fn itself fails.
func withUnwrapped(kek, uid, ref []byte, fn func([]byte) error) (bool, error) {
	key, err := sun.Unwrap(kek, uid, ref)
	if err != nil {
		return false, nil
	}
	defer sun.Zero(key)
	if err := fn(key); err != nil {
		return false, err
	}
	return true, nil
}
