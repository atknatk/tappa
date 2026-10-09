package qrcode

// The symbol's modules (ISO/IEC 18004 §6.3, §7.7-7.9). Coordinates are (x, y): x the
// column from the left, y the row from the top.

type grid struct {
	size  int
	dark  [][]bool // [y][x]
	fixed [][]bool // function modules: not data, not masked
}

func newGrid(ver int) *grid {
	size := 17 + 4*ver
	g := &grid{size: size, dark: make([][]bool, size), fixed: make([][]bool, size)}
	for y := range size {
		g.dark[y] = make([]bool, size)
		g.fixed[y] = make([]bool, size)
	}
	return g
}

func (g *grid) version() int { return (g.size - 17) / 4 }

func (g *grid) set(x, y int, dark bool) {
	g.dark[y][x] = dark
	g.fixed[y][x] = true
}

// drawFunctionPatterns draws the timing patterns, the three finder patterns with their
// separators, the alignment patterns and the version information, and reserves the format
// information (drawFormat writes it once the mask is known). Where an alignment pattern
// meets a timing pattern the two agree, so the drawing order there does not matter.
func (g *grid) drawFunctionPatterns() {
	for i := range g.size {
		g.set(6, i, i%2 == 0)
		g.set(i, 6, i%2 == 0)
	}
	g.drawFinder(3, 3)
	g.drawFinder(g.size-4, 3)
	g.drawFinder(3, g.size-4)
	pos := alignmentPositions(g.version())
	last := len(pos) - 1
	for i, cy := range pos {
		for j, cx := range pos {
			if (i == 0 && j == 0) || (i == 0 && j == last) || (i == last && j == 0) {
				continue // the three corners a finder pattern occupies
			}
			g.drawAlignment(cx, cy)
		}
	}
	g.drawFormat(L, 0) // reserves the area; the real bits come after masking
	g.drawVersion()
}

// drawFinder draws the 7x7 finder pattern centred on (cx, cy) and the light separator
// ring around it, clipped to the symbol.
func (g *grid) drawFinder(cx, cy int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			x, y := cx+dx, cy+dy
			if x < 0 || x >= g.size || y < 0 || y >= g.size {
				continue
			}
			d := max(abs(dx), abs(dy))
			g.set(x, y, d != 2 && d != 4)
		}
	}
}

// drawAlignment draws the 5x5 alignment pattern centred on (cx, cy).
func (g *grid) drawAlignment(cx, cy int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			g.set(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
		}
	}
}

// formatInfo is the 15-bit format information (ISO/IEC 18004 §7.9.1): the level's two
// bits and the mask's three, BCH(15,5) with the generator 0x537, XORed with 0x5412.
func formatInfo(level Level, mask int) int {
	data := level.formatBits()<<3 | mask
	rem := data
	for range 10 {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	return (data<<10 | rem) ^ 0x5412
}

// versionInfo is the 18-bit version information (ISO/IEC 18004 §7.10): the version's six
// bits, BCH(18,6) with the generator 0x1F25.
func versionInfo(ver int) int {
	rem := ver
	for range 12 {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
	}
	return ver<<12 | rem
}

// drawFormat writes the format information twice -- around the top-left finder, and
// split between the bottom-left and top-right finders -- and the dark module beside the
// bottom-left one. Bit 0 is the least significant (Figure 25).
func (g *grid) drawFormat(level Level, mask int) {
	bits := formatInfo(level, mask)
	bit := func(i int) bool { return bits>>i&1 == 1 }
	for i := 0; i <= 5; i++ {
		g.set(8, i, bit(i))
	}
	g.set(8, 7, bit(6))
	g.set(8, 8, bit(7))
	g.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		g.set(14-i, 8, bit(i))
	}
	for i := range 8 {
		g.set(g.size-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		g.set(8, g.size-15+i, bit(i))
	}
	g.set(8, g.size-8, true)
}

// drawVersion writes the version information, from version 7: two 6x3 blocks, above the
// bottom-left finder and left of the top-right one (Figure 27).
func (g *grid) drawVersion() {
	if g.version() < 7 {
		return
	}
	bits := versionInfo(g.version())
	for i := range 18 {
		dark := bits>>i&1 == 1
		a, b := g.size-11+i%3, i/3
		g.set(a, b, dark)
		g.set(b, a, dark)
	}
}

// placeData writes the codewords' bits, most significant first, into the modules that
// are not function modules (§7.7.3): two-module-wide columns from the right edge, upwards
// and downwards in turn, skipping the vertical timing column. Modules left over are the
// remainder bits, light before masking.
func (g *grid) placeData(codewords []byte) {
	i, n := 0, len(codewords)*8
	for right := g.size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		upward := (right+1)&2 == 0
		for vert := range g.size {
			y := vert
			if upward {
				y = g.size - 1 - vert
			}
			for j := range 2 {
				x := right - j
				if g.fixed[y][x] || i >= n {
					continue
				}
				g.dark[y][x] = codewords[i/8]>>(7-i%8)&1 == 1
				i++
			}
		}
	}
}

// maskBit is data mask pattern m's condition at column x, row y (§7.8.2, Table 10, with
// the table's i the row and j the column).
func maskBit(m, x, y int) bool {
	switch m {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (x/3+y/2)%2 == 0
	case 5:
		return x*y%2+x*y%3 == 0
	case 6:
		return (x*y%2+x*y%3)%2 == 0
	case 7:
		return ((x+y)%2+x*y%3)%2 == 0
	}
	return false
}

// applyMask inverts every data module where pattern m's condition holds; applying it
// twice restores the modules.
func (g *grid) applyMask(m int) {
	for y := range g.size {
		for x := range g.size {
			if !g.fixed[y][x] && maskBit(m, x, y) {
				g.dark[y][x] = !g.dark[y][x]
			}
		}
	}
}

// bestMask is the mask whose symbol -- format information included -- scores the lowest
// penalty, the lowest-numbered on a tie. It leaves the data unmasked.
func (g *grid) bestMask(level Level) int {
	best, bestScore := 0, -1
	for m := range 8 {
		g.applyMask(m)
		g.drawFormat(level, m)
		if s := penalty(g.dark); bestScore < 0 || s < bestScore {
			best, bestScore = m, s
		}
		g.applyMask(m)
	}
	return best
}

// The penalty weights (§7.8.3, Table 11).
const (
	penaltyN1 = 3
	penaltyN2 = 3
	penaltyN3 = 40
	penaltyN4 = 10
)

// penalty is the score of §7.8.3 the mask is chosen by:
//
//	N1  a run of five or more same-coloured modules in a row or column: 3, plus 1 for
//	    each module past five;
//	N2  each 2x2 block of one colour (overlapping blocks counted): 3;
//	N3  each 1:1:3:1:1 dark-light-dark-light-dark run in a row or column with four light
//	    modules on one side of it -- the area outside the symbol counts as light: 40;
//	N4  10 x k, k the smallest whole number with the dark modules' share within
//	    50 +- 5(k+1)% -- the standard's bands, a share on a band's edge taking the
//	    lower k.
func penalty(m [][]bool) int {
	size := len(m)
	score := 0
	for _, horizontal := range []bool{true, false} {
		for a := range size {
			run := runScanner{size: size}
			for b := range size {
				x, y := b, a
				if !horizontal {
					x, y = a, b
				}
				score += run.push(m[y][x])
			}
			score += run.end()
		}
	}
	for y := 0; y < size-1; y++ {
		for x := 0; x < size-1; x++ {
			c := m[y][x]
			if c == m[y][x+1] && c == m[y+1][x] && c == m[y+1][x+1] {
				score += penaltyN2
			}
		}
	}
	dark := 0
	for _, row := range m {
		for _, c := range row {
			if c {
				dark++
			}
		}
	}
	total := size * size
	// The smallest k >= 0 with |dark/total - 1/2| <= (k+1) * 5%, in integers.
	k := max((abs(dark*20-total*10)+total-1)/total-1, 0)
	return score + k*penaltyN4
}

// runScanner scores one row or column for N1 and N3 as its modules are pushed. history
// holds the last seven run lengths, newest first; the light area before the line is
// added to its first light run and the light area after it to its last.
type runScanner struct {
	size    int
	color   bool // the current run's colour; a line starts in a light run of length 0
	length  int
	history [7]int
}

func (r *runScanner) push(dark bool) int {
	if dark == r.color {
		r.length++
		switch {
		case r.length == 5:
			return penaltyN1
		case r.length > 5:
			return 1
		}
		return 0
	}
	r.addHistory(r.length)
	score := 0
	if !r.color {
		score = r.finderLike() * penaltyN3
	}
	r.color, r.length = dark, 1
	return score
}

func (r *runScanner) end() int {
	if r.color { // close the dark run, then the light area after the line is a run
		r.addHistory(r.length)
		r.length = 0
	}
	r.length += r.size
	r.addHistory(r.length)
	return r.finderLike() * penaltyN3
}

func (r *runScanner) addHistory(length int) {
	if r.history[0] == 0 {
		length += r.size // the light area before the line
	}
	copy(r.history[1:], r.history[:6])
	r.history[0] = length
}

// finderLike counts the 1:1:3:1:1 patterns the newest light run closes: history[1..5]
// are the dark-light-dark-light-dark runs, history[0] the light run after them and
// history[6] the light run before; one side must be at least four times the unit.
func (r *runScanner) finderLike() int {
	h := r.history
	n := h[1]
	core := n > 0 && h[2] == n && h[3] == n*3 && h[4] == n && h[5] == n
	count := 0
	if core && h[0] >= n*4 && h[6] >= n {
		count++
	}
	if core && h[6] >= n*4 && h[0] >= n {
		count++
	}
	return count
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
