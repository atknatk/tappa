package qrcode

// The version and level tables (ISO/IEC 18004 Table 9), in the compact form: per level
// and version, the error correction codewords of each block and the number of blocks. A
// version's codeword total comes from numRawDataModules, so the block lengths follow: the
// blocks share the total as evenly as whole codewords allow (interleave). Index 0 is
// unused. Held to the standard by the reference matrices (testdata/sweep.txt: every
// version at every level) and to an independent copy of Table 9 for versions 1-10
// (read_test.go).

var eccPerBlock = [4][41]int{
	L: {0, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28, 30, 28, 28, 28, 28, 30, 30, 26, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
	M: {0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, 26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28},
	Q: {0, 13, 22, 18, 26, 18, 24, 18, 22, 20, 24, 28, 26, 24, 20, 30, 24, 28, 28, 26, 30, 28, 30, 30, 30, 30, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
	H: {0, 17, 28, 22, 16, 22, 28, 26, 26, 24, 28, 24, 28, 22, 24, 24, 30, 28, 28, 26, 28, 30, 24, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
}

var numBlocks = [4][41]int{
	L: {0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8, 8, 9, 9, 10, 12, 12, 12, 13, 14, 15, 16, 17, 18, 19, 19, 20, 21, 22, 24, 25},
	M: {0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16, 17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49},
	Q: {0, 1, 1, 2, 2, 4, 4, 6, 6, 8, 8, 8, 10, 12, 16, 12, 17, 16, 18, 21, 20, 23, 23, 25, 27, 29, 34, 34, 35, 38, 40, 43, 45, 48, 51, 53, 56, 59, 62, 65, 68},
	H: {0, 1, 1, 2, 4, 4, 4, 5, 6, 8, 8, 11, 11, 16, 16, 18, 16, 19, 21, 25, 25, 25, 34, 30, 32, 35, 37, 40, 42, 45, 48, 51, 54, 57, 60, 63, 66, 70, 74, 77, 81},
}

// numRawDataModules is the number of modules a version-ver symbol leaves for codewords
// and remainder bits: the square, less the three finder patterns with their separators,
// the format information and the dark module, the timing patterns, the alignment patterns
// that do not overlap a finder, and from version 7 the two version information blocks
// (36), in closed form. Divided by 8 it is Table 9's codeword total; the remainder is the
// version's remainder bits (0, 3, 4 or 7).
func numRawDataModules(ver int) int {
	n := (16*ver+128)*ver + 64
	if ver >= 2 {
		align := ver/7 + 2
		n -= (25*align-10)*align - 55
		if ver >= 7 {
			n -= 36
		}
	}
	return n
}

// numDataCodewords is the version's codeword total less its error correction codewords.
func numDataCodewords(ver int, level Level) int {
	return numRawDataModules(ver)/8 - eccPerBlock[level][ver]*numBlocks[level][ver]
}

// alignmentPositions is the row and column coordinates of the alignment patterns' centres
// (ISO/IEC 18004 Annex E): 6, then evenly spaced, even steps ending at size-7. Version 1
// has none. The closed form reproduces Table E.1, version 32's irregular step included
// (TestAlignmentPositions_AreTableE1).
func alignmentPositions(ver int) []int {
	if ver == 1 {
		return nil
	}
	n := ver/7 + 2
	step := (ver*8 + n*3 + 5) / (n*4 - 4) * 2
	out := make([]int, n)
	out[0] = 6
	for i, pos := n-1, 17+4*ver-7; i >= 1; i, pos = i-1, pos-step {
		out[i] = pos
	}
	return out
}
