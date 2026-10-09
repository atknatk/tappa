package qrcode

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// THE REFERENCE. testdata/ holds what libqrencode 4.1.1's command-line tool drew
// (testdata/generate.sh; the tests do not run it, CI has no qrencode): named vectors as
// full matrices, and a sweep of every version at every level as a sha256 per matrix.
//
// MASKS. A symbol is valid under any of the eight masks; which one an encoder picks is its
// penalty arithmetic's. So every comparison is made at the REFERENCE's mask (read from the
// format information it drew, forced with encode): that matrix must equal the reference's
// module for module. Whether Encode's own choice is the reference's -- in 139 of 166
// cases, the rest explained by the two encoders' readings of the penalty rules -- is
// TestEncode_TheMaskChoiceDiffersFromTheReferenceOnlyInThePenaltyReading's, and that the
// choice is the lowest of this package's penalty scores is
// TestEncode_ChoosesTheLowestPenaltyMask's.

// ascii draws a matrix the way qrencode -t ASCII -m 0 does: "##" dark, "  " light, a row
// per line.
func ascii(m [][]bool) string {
	var b strings.Builder
	for _, row := range m {
		for _, dark := range row {
			if dark {
				b.WriteString("##")
			} else {
				b.WriteString("  ")
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// parseASCII is ascii's inverse; a malformed reference is a red test.
func parseASCII(t *testing.T, text string) [][]bool {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	m := make([][]bool, len(lines))
	for y, line := range lines {
		if len(line) != 2*len(lines) {
			t.Fatalf("reference row %d is %d characters wide; a %d-row matrix has %d", y, len(line), len(lines), 2*len(lines))
		}
		m[y] = make([]bool, len(lines))
		for x := range m[y] {
			switch line[2*x : 2*x+2] {
			case "##":
				m[y][x] = true
			case "  ":
			default:
				t.Fatalf("reference row %d, module %d is %q", y, x, line[2*x:2*x+2])
			}
		}
	}
	return m
}

// formatOf reads the level and the mask a matrix's first format information copy names,
// at the positions drawFormat writes (the second copy and the BCH check are read_test.go's).
func formatOf(m [][]bool) (Level, int) {
	bits := 0
	put := func(i int, dark bool) {
		if dark {
			bits |= 1 << i
		}
	}
	for i := 0; i <= 5; i++ {
		put(i, m[i][8])
	}
	put(6, m[7][8])
	put(7, m[8][8])
	put(8, m[8][7])
	for i := 9; i < 15; i++ {
		put(i, m[8][14-i])
	}
	data := (bits ^ 0x5412) >> 10
	level := map[int]Level{1: L, 0: M, 3: Q, 2: H}[data>>3]
	return level, data & 7
}

// TestEncode_MatchesTheReferenceVectors: each named vector, encoded at its level, is the
// reference's version, and at the reference's mask it is the reference's matrix, module
// for module. Cases: a short ASCII text at L, M, Q and H (version 1); a 112-byte text at M
// (version 7: the version information blocks); an otpauth:// URI of the shape the
// enrollment page encodes, with an invented key, at M (version 8).
func TestEncode_MatchesTheReferenceVectors(t *testing.T) {
	tests := []struct {
		name    string
		in, ref string
		level   Level
		version int
	}{
		{"short ASCII, L", "short.in", "short-L.txt", L, 1},
		{"short ASCII, M", "short.in", "short-M.txt", M, 1},
		{"short ASCII, Q", "short.in", "short-Q.txt", Q, 1},
		{"short ASCII, H", "short.in", "short-H.txt", H, 1},
		{"version 7, M", "v7.in", "v7-M.txt", M, 7},
		{"otpauth URI, M", "otpauth.in", "otpauth-M.txt", M, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, ref := readFile(t, tt.in), string(readFile(t, tt.ref))
			refLevel, refMask := formatOf(parseASCII(t, ref))
			if refLevel != tt.level {
				t.Fatalf("PREMISE: the reference is level %s, the case says %s", refLevel, tt.level)
			}
			auto, err := Encode(data, tt.level)
			if err != nil {
				t.Fatal(err)
			}
			if auto.Version() != tt.version || auto.Level() != tt.level {
				t.Fatalf("Encode chose version %d level %s, want %d %s", auto.Version(), auto.Level(), tt.version, tt.level)
			}
			c, err := encode(data, tt.level, MinVersion, refMask)
			if err != nil {
				t.Fatal(err)
			}
			if got := ascii(c.modules); got != ref {
				t.Errorf("at the reference's mask %d the matrix differs from the reference:\ngot\n%s\nwant\n%s", refMask, got, ref)
			}
			if auto.Mask() == refMask && ascii(auto.modules) != ref {
				t.Error("Encode chose the reference's mask and still drew another matrix")
			}
		})
	}
}

var levels = []Level{L, M, Q, H}

// sweepInput is generate.sh's input formula: byte i is
// ALPHA[(37*i + 11*version + 5*level) mod 64], level 0-3 for L M Q H.
func sweepInput(version int, level Level, n int) []byte {
	const alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	out := make([]byte, n)
	for i := range out {
		out[i] = alpha[(37*i+11*version+5*int(level))%64]
	}
	return out
}

type sweepCase struct {
	version, bytes, mask int
	level                Level
	sha                  string
}

func readSweep(t *testing.T) []sweepCase {
	t.Helper()
	var out []sweepCase
	sc := bufio.NewScanner(bytes.NewReader(readFile(t, "sweep.txt")))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 5 {
			t.Fatalf("sweep.txt line %q has %d fields", line, len(f))
		}
		var c sweepCase
		var err error
		if c.version, err = strconv.Atoi(f[0]); err != nil {
			t.Fatal(err)
		}
		lv := strings.Index("LMQH", f[1])
		if len(f[1]) != 1 || lv < 0 {
			t.Fatalf("sweep.txt level %q", f[1])
		}
		c.level = Level(lv)
		if c.bytes, err = strconv.Atoi(f[2]); err != nil {
			t.Fatal(err)
		}
		if c.mask, err = strconv.Atoi(f[3]); err != nil {
			t.Fatal(err)
		}
		c.sha = f[4]
		out = append(out, c)
	}
	if len(out) != 160 {
		t.Fatalf("PREMISE: sweep.txt has %d case(s); every version at every level is 160", len(out))
	}
	return out
}

func shaOf(m [][]bool) string {
	s := sha256.Sum256([]byte(ascii(m)))
	return hex.EncodeToString(s[:])
}

// TestEncode_MatchesTheReferenceSweep: for every version 1-40 at every level, an input as
// long as the reference found the version holds (its --strict-version accepting the length
// and refusing one byte more) is exactly this package's capacity, Encode puts it in that
// version, and at the reference's mask the matrix hashes to the reference's.
func TestEncode_MatchesTheReferenceSweep(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range readSweep(t) {
		key := fmt.Sprintf("v%d-%s", c.version, c.level)
		seen[key] = true
		if got := capacity(c.version, c.level); got != c.bytes {
			t.Errorf("%s: capacity %d bytes, the reference holds %d", key, got, c.bytes)
			continue
		}
		data := sweepInput(c.version, c.level, c.bytes)
		auto, err := Encode(data, c.level)
		if err != nil {
			t.Errorf("%s: %v", key, err)
			continue
		}
		if auto.Version() != c.version {
			t.Errorf("%s: Encode chose version %d", key, auto.Version())
		}
		forced, err := encode(data, c.level, c.version, c.mask)
		if err != nil {
			t.Errorf("%s: %v", key, err)
			continue
		}
		if got := shaOf(forced.modules); got != c.sha {
			t.Errorf("%s: at the reference's mask %d the matrix hashes to %s, the reference to %s", key, c.mask, got, c.sha)
		}
	}
	for _, l := range levels {
		for v := MinVersion; v <= MaxVersion; v++ {
			if !seen[fmt.Sprintf("v%d-%s", v, l)] {
				t.Errorf("sweep.txt has no case for version %d level %s", v, l)
			}
		}
	}
}

type maskCase struct {
	name  string
	data  []byte
	level Level
	mask  int // the reference's
}

// maskCases are the sweep's 160 cases and the six named vectors, with the mask the
// reference chose for each.
func maskCases(t *testing.T) []maskCase {
	t.Helper()
	var cases []maskCase
	for _, c := range readSweep(t) {
		cases = append(cases, maskCase{fmt.Sprintf("v%d-%s", c.version, c.level), sweepInput(c.version, c.level, c.bytes), c.level, c.mask})
	}
	for _, v := range []struct {
		in, ref string
		level   Level
	}{
		{"short.in", "short-L.txt", L}, {"short.in", "short-M.txt", M}, {"short.in", "short-Q.txt", Q},
		{"short.in", "short-H.txt", H}, {"v7.in", "v7-M.txt", M}, {"otpauth.in", "otpauth-M.txt", M},
	} {
		_, mask := formatOf(parseASCII(t, string(readFile(t, v.ref))))
		cases = append(cases, maskCase{v.ref, readFile(t, v.in), v.level, mask})
	}
	if len(cases) != 166 {
		t.Fatalf("PREMISE: %d case(s), 160 + 6 expected", len(cases))
	}
	return cases
}

// qrencodePenalty is a MODEL of the reference's penalty -- libqrencode 4.1.1's mask
// scoring, written from its mask.c as this test reads it, not run. It differs from
// penalty in two readings of §7.8.3, both of the standard's words rather than of its
// arithmetic:
//
//	N3  a 1:1:3:1:1 run is scored at most ONCE, when either side has four light modules
//	    or the line's edge; penalty scores it once PER light side ("preceded or followed
//	    by" read as two patterns), so twice when both sides are light;
//	N4  the dark share is ROUNDED to a whole percent before the 5% bands, so 54.6%
//	    scores k = 1; penalty compares the exact share, k = 0.
//
// N1 and N2 are the same. Measured: choosing by this model reproduces the reference's
// mask in all 166 cases (TestEncode_TheMaskChoiceDiffersFromTheReferenceOnlyInThePenaltyReading).
func qrencodePenalty(m [][]bool) int {
	size := len(m)
	score := 0
	// line scores N1 and N3 on one row or column from its run lengths; a line that starts
	// dark gets a placeholder light run of -1 first, so the odd indices are the dark runs.
	line := func(get func(i int) bool) int {
		var rl []int
		if get(0) {
			rl = append(rl, -1)
		}
		rl = append(rl, 1)
		prev := get(0)
		for i := 1; i < size; i++ {
			if get(i) != prev {
				rl = append(rl, 1)
				prev = get(i)
			} else {
				rl[len(rl)-1]++
			}
		}
		s, n := 0, len(rl)
		for i := range n {
			if rl[i] >= 5 {
				s += 3 + rl[i] - 5
			}
			if i&1 == 1 && i >= 3 && i < n-2 && rl[i]%3 == 0 {
				f := rl[i] / 3
				if rl[i-2] == f && rl[i-1] == f && rl[i+1] == f && rl[i+2] == f {
					if i == 3 || rl[i-3] >= 4*f {
						s += 40
					} else if i+4 >= n || rl[i+3] >= 4*f {
						s += 40
					}
				}
			}
		}
		return s
	}
	for a := range size {
		score += line(func(i int) bool { return m[a][i] })
		score += line(func(i int) bool { return m[i][a] })
	}
	for y := 1; y < size; y++ {
		for x := 1; x < size; x++ {
			c := m[y][x]
			if c == m[y][x-1] && c == m[y-1][x] && c == m[y-1][x-1] {
				score += 3
			}
		}
	}
	blacks, w2 := 0, size*size
	for _, row := range m {
		for _, c := range row {
			if c {
				blacks++
			}
		}
	}
	bratio := (200*blacks + w2) / w2 / 2
	return score + abs(bratio-50)/5*10
}

// TestEncode_TheMaskChoiceDiffersFromTheReferenceOnlyInThePenaltyReading -- why Encode's
// mask is not always the reference's, measured on the sweep's 160 cases and the six named
// vectors:
//
//   - choosing among the eight forced symbols by qrencodePenalty (the model of the
//     reference's two readings) gives the reference's mask in ALL 166 cases -- so where
//     the choices differ, the symbols compared are the same and only the scoring differs;
//   - Encode's own choice is the reference's in 139 of the 166 (a pinned count: a penalty
//     rule dropped or re-weighted moves it). The 27 others are valid symbols -- the
//     standard admits any mask -- and are the lowest of penalty's scores
//     (TestEncode_ChoosesTheLowestPenaltyMask). Their matrices at the reference's mask equal
//     the reference's (TestEncode_MatchesTheReferenceSweep, ..._MatchesTheReferenceVectors).
//
// Why penalty keeps its reading rather than the reference's: the reference's N4 rounds
// before it compares, and its N3 makes "preceded or followed" one pattern -- both
// defensible, neither the only reading; a reader scans either symbol.
func TestEncode_TheMaskChoiceDiffersFromTheReferenceOnlyInThePenaltyReading(t *testing.T) {
	const wantOwnAgree = 139
	model, own := 0, 0
	var differ []string
	cases := maskCases(t)
	for _, c := range cases {
		auto, err := Encode(c.data, c.level)
		if err != nil {
			t.Fatal(err)
		}
		if auto.Mask() == c.mask {
			own++
		} else {
			differ = append(differ, fmt.Sprintf("%s (Encode %d, reference %d)", c.name, auto.Mask(), c.mask))
		}
		best, bestScore := -1, 0
		for m := range 8 {
			f, err := encode(c.data, c.level, auto.Version(), m)
			if err != nil {
				t.Fatal(err)
			}
			if s := qrencodePenalty(f.modules); best < 0 || s < bestScore {
				best, bestScore = m, s
			}
		}
		if best == c.mask {
			model++
		} else {
			t.Errorf("%s: the model of the reference's reading chooses mask %d, the reference chose %d", c.name, best, c.mask)
		}
	}
	if model != len(cases) {
		t.Errorf("the model reproduces %d of %d reference choices; the comment says all", model, len(cases))
	}
	if own != wantOwnAgree {
		t.Errorf("Encode's mask is the reference's in %d of %d cases, pinned at %d; differing: %v", own, len(cases), wantOwnAgree, differ)
	}
	t.Logf("Encode agrees with the reference's mask in %d of %d; the model in %d; differing: %s", own, len(cases), model, strings.Join(differ, ", "))
}

// TestEncode_ChoosesTheLowestPenaltyMask: on every sweep case and named vector, the mask
// Encode chose scores, as the whole symbol it drew, the lowest penalty of the eight forced
// masks, and no lower-numbered mask scores as low (the tie rule).
func TestEncode_ChoosesTheLowestPenaltyMask(t *testing.T) {
	check := func(name string, data []byte, level Level) {
		auto, err := Encode(data, level)
		if err != nil {
			t.Fatal(err)
		}
		var scores [8]int
		for m := range 8 {
			c, err := encode(data, level, auto.Version(), m)
			if err != nil {
				t.Fatal(err)
			}
			scores[m] = penalty(c.modules)
		}
		for m := range 8 {
			if scores[m] < scores[auto.Mask()] || (scores[m] == scores[auto.Mask()] && m < auto.Mask()) {
				t.Errorf("%s: Encode chose mask %d (penalty %d), mask %d scores %d", name, auto.Mask(), scores[auto.Mask()], m, scores[m])
			}
		}
	}
	for _, c := range readSweep(t) {
		check(fmt.Sprintf("v%d-%s", c.version, c.level), sweepInput(c.version, c.level, c.bytes), c.level)
	}
	for _, in := range []string{"short.in", "v7.in", "otpauth.in"} {
		for _, l := range levels {
			if _, err := Encode(readFile(t, in), l); err == nil {
				check(in+"-"+l.String(), readFile(t, in), l)
			}
		}
	}
}

// TestPenalty_ScoresEachRuleByTheStandard holds the penalty to hand-counted inputs, one
// rule at a time, so a rule dropped or mis-weighted is red here and not only through a
// changed mask choice: whole matrices for N2 and N4 (and N1 on a uniform square), single
// lines through the run scanner for N1 and N3. Each count is worked in the case's comment.
func TestPenalty_ScoresEachRuleByTheStandard(t *testing.T) {
	parse := func(rows ...string) [][]bool {
		m := make([][]bool, len(rows))
		for y, r := range rows {
			m[y] = make([]bool, len(r))
			for x := range r {
				m[y][x] = r[x] == '#'
			}
		}
		return m
	}
	squares := []struct {
		name string
		m    [][]bool
		want int
	}{
		// 2x2 checkerboard: no run of 5, no one-colour 2x2 block, exactly half dark. 0.
		{"checkerboard", parse("#.", ".#"), 0},
		// 2x2 all dark: N2 one block (3); 100% dark, k = 9 (90). 93.
		{"all dark 2x2", parse("##", "##"), 3 + 90},
		// 5x5 all light: N1 five rows and five columns, each one run of 5 (10 x 3 = 30); N2
		// sixteen blocks (48); N4 0% dark, k = 9 (90); no dark run, so no N3. 168.
		{"all light 5x5", parse(".....", ".....", ".....", ".....", "....."), 30 + 48 + 90},
		// 5x5 with 15 dark (60%): every row "###.." or ".###." mixes, so no N1 or N3; N2
		// counts the one-colour 2x2 blocks: rows 0-1 "###.." over "###.." give 2 dark and
		// 1 light; rows 1-2 "###.." over ".###." give 1 dark; rows 2-3 ".###." twice give 2
		// dark; rows 3-4 ".###." over "###.." give 1 dark: 7 blocks (21). N4: 60% is on the
		// edge of 55-60%, the lower k = 1 (10). Its columns: 0 "##..#", 1 "#####" -- a run
		// of 5 (3) --, 2 "#####" (3), 3 "..##.", 4 "....." -- a light run of 5 (3); no
		// line holds five runs, so no 1:1:3:1:1. 21 + 10 + 9 = 40.
		{"60 percent dark", parse("###..", "###..", ".###.", ".###.", "###.."), 21 + 10 + 9},
	}
	for _, tt := range squares {
		if got := penalty(tt.m); got != tt.want {
			t.Errorf("%s: penalty %d, want %d", tt.name, got, tt.want)
		}
	}
	lines := []struct {
		name, line string
		want       int
	}{
		// Seven dark: N1 3 at the fifth module, 1 at the sixth and seventh. 5.
		{"a run of seven", "#######", 5},
		// 1:1:3:1:1 with three light before it -- plus the light border, eleven modules,
		// fourteen -- and one after, plus the border, twelve: four light on each side
		// counts the pattern twice (N3 2 x 40). No run of five. 80.
		{"finder-like, light both sides", "...#.###.#.", 80},
		// The same pattern with a dark module either side: no four-module light side. 0.
		{"finder-like, dark both sides", "#.#.###.#.#", 0},
	}
	for _, tt := range lines {
		r := runScanner{size: len(tt.line)}
		got := 0
		for i := range tt.line {
			got += r.push(tt.line[i] == '#')
		}
		got += r.end()
		if got != tt.want {
			t.Errorf("%s: %d, want %d", tt.name, got, tt.want)
		}
	}
}

// TestEncode_OneByteMoreTakesTheNextVersion: an input one byte longer than a version holds
// at a level is put in the next version -- and past version 40 it is refused with
// ErrTooLong.
func TestEncode_OneByteMoreTakesTheNextVersion(t *testing.T) {
	for _, l := range levels {
		for v := MinVersion; v <= MaxVersion; v++ {
			c, err := Encode(make([]byte, capacity(v, l)+1), l)
			if v == MaxVersion {
				if !errors.Is(err, ErrTooLong) {
					t.Errorf("level %s: one byte past version 40 gave %v, want ErrTooLong", l, err)
				}
				continue
			}
			if err != nil || c.Version() != v+1 {
				t.Errorf("level %s: one byte past version %d gave %v, %v", l, v, c, err)
			}
		}
	}
	if c, err := Encode(nil, M); err != nil || c.Version() != 1 {
		t.Errorf("empty data gave %v, %v; want a version 1 symbol", c, err)
	}
}

// TestEncode_TheRefusalCarriesNoInput: the error for data too long, and for an unknown
// level, names no byte run of the input -- the input of this package's caller is a TOTP
// secret's URI (package comment).
func TestEncode_TheRefusalCarriesNoInput(t *testing.T) {
	marker := "otpauth://totp/x?secret=FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE&"
	long := []byte(strings.Repeat(marker, 2953/len(marker)+2))
	for _, l := range levels {
		_, err := Encode(long, l)
		if !errors.Is(err, ErrTooLong) {
			t.Fatalf("level %s: %d bytes gave %v, want ErrTooLong", l, len(long), err)
		}
		for i := 0; i+4 <= len(marker); i++ {
			if run := marker[i : i+4]; strings.Contains(err.Error(), run) {
				t.Errorf("level %s: the error %q carries the input's run %q", l, err, run)
			}
		}
	}
	if _, err := Encode([]byte(marker), Level(4)); err == nil || strings.Contains(err.Error(), "FAKE") {
		t.Errorf("an unknown level gave %v", err)
	}
}

// TestAlignmentPositions_AreTableE1 holds the closed form to ISO/IEC 18004 Table E.1,
// typed from the table.
func TestAlignmentPositions_AreTableE1(t *testing.T) {
	table := map[int][]int{
		1: nil, 2: {6, 18}, 3: {6, 22}, 4: {6, 26}, 5: {6, 30}, 6: {6, 34},
		7: {6, 22, 38}, 8: {6, 24, 42}, 9: {6, 26, 46}, 10: {6, 28, 50}, 11: {6, 30, 54},
		12: {6, 32, 58}, 13: {6, 34, 62}, 14: {6, 26, 46, 66}, 15: {6, 26, 48, 70},
		16: {6, 26, 50, 74}, 17: {6, 30, 54, 78}, 18: {6, 30, 56, 82}, 19: {6, 30, 58, 86},
		20: {6, 34, 62, 90}, 21: {6, 28, 50, 72, 94}, 22: {6, 26, 50, 74, 98},
		23: {6, 30, 54, 78, 102}, 24: {6, 28, 54, 80, 106}, 25: {6, 32, 58, 84, 110},
		26: {6, 30, 58, 86, 114}, 27: {6, 34, 62, 90, 118}, 28: {6, 26, 50, 74, 98, 122},
		29: {6, 30, 54, 78, 102, 126}, 30: {6, 26, 52, 78, 104, 130},
		31: {6, 30, 56, 82, 108, 134}, 32: {6, 34, 60, 86, 112, 138},
		33: {6, 30, 58, 86, 114, 142}, 34: {6, 34, 62, 90, 118, 146},
		35: {6, 30, 54, 78, 102, 126, 150}, 36: {6, 24, 50, 76, 102, 128, 154},
		37: {6, 28, 54, 80, 106, 132, 158}, 38: {6, 32, 58, 84, 110, 136, 162},
		39: {6, 26, 54, 82, 110, 138, 166}, 40: {6, 30, 58, 86, 114, 142, 170},
	}
	for v := MinVersion; v <= MaxVersion; v++ {
		if got := alignmentPositions(v); fmt.Sprint(got) != fmt.Sprint(table[v]) {
			t.Errorf("version %d: %v, Table E.1 %v", v, got, table[v])
		}
	}
}

// TestFormatAndVersionInfo_AreTheStandardsCodewords holds formatInfo to the 32 format
// information sequences of ISO/IEC 18004 Table C.1 and versionInfo to the 34 of Table D.1,
// typed from the tables.
func TestFormatAndVersionInfo_AreTheStandardsCodewords(t *testing.T) {
	format := map[Level][8]string{
		L: {"111011111000100", "111001011110011", "111110110101010", "111100010011101",
			"110011000101111", "110001100011000", "110110001000001", "110100101110110"},
		M: {"101010000010010", "101000100100101", "101111001111100", "101101101001011",
			"100010111111001", "100000011001110", "100111110010111", "100101010100000"},
		Q: {"011010101011111", "011000001101000", "011111100110001", "011101000000110",
			"010010010110100", "010000110000011", "010111011011010", "010101111101101"},
		H: {"001011010001001", "001001110111110", "001110011100111", "001100111010000",
			"000011101100010", "000001001010101", "000110100001100", "000100000111011"},
	}
	for _, l := range levels {
		for m := range 8 {
			if got := fmt.Sprintf("%015b", formatInfo(l, m)); got != format[l][m] {
				t.Errorf("format information, level %s mask %d: %s, Table C.1 %s", l, m, got, format[l][m])
			}
		}
	}
	version := []int{0x07C94, 0x085BC, 0x09A99, 0x0A4D3, 0x0BBF6, 0x0C762, 0x0D847, 0x0E60D,
		0x0F928, 0x10B78, 0x1145D, 0x12A17, 0x13532, 0x149A6, 0x15683, 0x168C9, 0x177EC,
		0x18EC4, 0x191E1, 0x1AFAB, 0x1B08E, 0x1CC1A, 0x1D33F, 0x1ED75, 0x1F250, 0x209D5,
		0x216F0, 0x228BA, 0x2379F, 0x24B0B, 0x2542E, 0x26A64, 0x27541, 0x28C69}
	for i, want := range version {
		if got := versionInfo(7 + i); got != want {
			t.Errorf("version information, version %d: %05X, Table D.1 %05X", 7+i, got, want)
		}
	}
}

// TestBitmap_AddsTheQuietZone: Bitmap(QuietZone) is the symbol with four light modules on
// every side, and a fresh matrix -- writing to it leaves the Code as it was.
func TestBitmap_AddsTheQuietZone(t *testing.T) {
	c, err := Encode(readFile(t, "otpauth.in"), M)
	if err != nil {
		t.Fatal(err)
	}
	b := c.Bitmap(QuietZone)
	if len(b) != c.Size()+8 || QuietZone != 4 {
		t.Fatalf("Bitmap(%d) is %d wide for a %d-module symbol", QuietZone, len(b), c.Size())
	}
	for y := range b {
		for x := range b[y] {
			inside := x >= 4 && y >= 4 && x < 4+c.Size() && y < 4+c.Size()
			if want := inside && c.modules[y-4][x-4]; b[y][x] != want {
				t.Fatalf("Bitmap module (%d, %d) = %v, want %v", x, y, b[y][x], want)
			}
		}
	}
	before := ascii(c.modules)
	for y := range b {
		for x := range b[y] {
			b[y][x] = !b[y][x]
		}
	}
	if ascii(c.modules) != before {
		t.Error("writing to the bitmap changed the Code")
	}
}
