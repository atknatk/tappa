package qrcode

// Reed-Solomon error correction over GF(2^8) with the field polynomial
// x^8 + x^4 + x^3 + x^2 + 1 (0x11D) and the generator (x - a^0)(x - a^1)...(x - a^(n-1)),
// a = 2 (ISO/IEC 18004 §7.5.2, Annex A). The error correction codewords of a block are
// the remainder of the block's data, times x^n, divided by the generator.

// gfMul multiplies in GF(2^8) mod 0x11D, shift-and-add -- no tables, so the decoder's
// log/antilog tables in read_test.go are an independent second computation.
func gfMul(x, y byte) byte {
	z := 0
	for i := 7; i >= 0; i-- {
		z = (z << 1) ^ ((z >> 7) * 0x11D)
		z ^= int(y>>i&1) * int(x)
	}
	return byte(z)
}

// rsDivisor is the generator polynomial of degree n, its coefficients from x^(n-1) down to
// x^0; the leading x^n's 1 is implied.
func rsDivisor(n int) []byte {
	out := make([]byte, n)
	out[n-1] = 1 // start from the polynomial 1, kept in the lowest coefficient
	root := byte(1)
	for range n {
		// Multiply the product so far by (x - root), which is (x + root) in GF(2^8).
		for j := range out {
			out[j] = gfMul(out[j], root)
			if j+1 < n {
				out[j] ^= out[j+1]
			}
		}
		root = gfMul(root, 2)
	}
	return out
}

// rsRemainder is data(x) * x^n mod the generator: n = len(divisor) codewords.
func rsRemainder(data, divisor []byte) []byte {
	out := make([]byte, len(divisor))
	for _, b := range data {
		factor := b ^ out[0]
		copy(out, out[1:])
		out[len(out)-1] = 0
		for i, c := range divisor {
			out[i] ^= gfMul(c, factor)
		}
	}
	return out
}
